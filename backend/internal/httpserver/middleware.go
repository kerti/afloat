package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/kerti/afloat/backend/internal/httperr"
)

// recoverer replaces chi's middleware.Recoverer, which answers a handler
// panic with a bare 500 — no body, no Content-Type — leaving the frontend
// nothing to dispatch on (issue #29; #19 closed the other two escape
// hatches the generated server has). It logs the panic and stack at error
// level, then writes the shared envelope. It never re-panics: that would
// drop the in-flight request, exactly what recovering exists to prevent.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				slog.Error("panic recovered",
					"path", r.URL.Path,
					"panic", rvr,
					"stack", string(debug.Stack()),
				)
				httperr.Write(w, http.StatusInternalServerError, httperr.CodeInternal, nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestLogger records method, path, status and duration — and nothing else.
//
// Never the body, never query values, never the session cookie. PRD N6 forbids
// telemetry of any kind, and a log line carrying an Expense description is
// telemetry that happens to be written to disk (docs/adr/go/0003).
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		started := time.Now()

		next.ServeHTTP(ww, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
			"request_id", requestIDFrom(r.Context()),
		)
	})
}

// localLoginPath is the login route's full mounted path — the one route
// disabledLocalLogin404 gates. The prefix is the spec's (basePath); the rest is
// spelled out, because the generated api package has no constant for it.
var localLoginPath = basePath + "/auth/local/login"

// disabledLocalLogin404 makes /api/auth/local/login answer a bare 404
// when the local provider is off, matching how GET /api/auth/methods already
// reports it (issue #24). When enabled is true this is a no-op: the returned
// middleware is next itself, so the default configuration's request path is
// unchanged.
func disabledLocalLogin404(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == localLoginPath {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// routeOnDecodedPath makes chi route on the decoded path. chi prefers
// r.URL.RawPath whenever the URL parser kept one, which it does for any
// percent-encoding other than its own - so /api/auth/local/%6Cogin was a 404
// here and the login route on Kotlin, though RFC 3986 §6.2.2.2 makes %6C and l
// the same resource (#56). Clearing RawPath on a copy of the request leaves
// chi only the decoded Path.
//
// Except for an encoded slash: decoded, %2F would become a separator and
// /api/auth%2Flocal/login would reach login. Spring's firewall refuses that
// path, and so does chi, which routes it raw and finds nothing.
func routeOnDecodedPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawPath == "" || strings.Contains(strings.ToLower(r.URL.RawPath), "%2f") {
			next.ServeHTTP(w, r)
			return
		}
		decoded := r.Clone(r.Context())
		decoded.URL.RawPath = ""
		next.ServeHTTP(w, decoded)
	})
}

// maxBodyBytes caps the request body before any handler decodes it, so an
// oversized or endless body is refused rather than buffered.
func maxBodyBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// headAsGet answers HEAD by running the GET handler and discarding the
// body (RFC 9110 §9.1): the same status and headers as GET, nothing written.
//
// Not chi's middleware.GetHead, which rewrites only rctx.RouteMethod — the
// field chi's own tree lookup keys off — and leaves r.Method as HEAD. Every
// layer downstream of routing that inspects the method disagrees with what
// got matched, and one of them does: oapi-codegen's per-operation
// spec-validating middleware (openapi_validate.go) reads r.Method directly
// and refused a HEAD against a GET-only operation with "method not allowed"
// before a single handler ran. So this rewrites both: rctx.RouteMethod for
// this mounted router's own routing (its tree lookup would otherwise still
// match against HEAD — chi shares one *Context across a Mount, and the outer
// router already set that field during its own pass before this one runs),
// and a cloned request's Method for everything downstream of routing.
// Discarding only the bytes a ResponseWriter is asked to write keeps the
// rest: the httptest.Recorder output for HEAD /api/health is byte-identical
// to GET's but for the body.
//
// Mounted on the API's own base router (server.go), inside the mount at
// basePath: its look-ahead is chi's ordinary routing, on that router's own
// tree of the contract's actual operations, so a HEAD to a route the
// contract does not register as GET still 405s like any other wrong method.
func headAsGet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		// chi's own routing (mux.routeHTTP) keys off rctx.RouteMethod, not
		// r.Method, and that field is already set — to HEAD — by the outer
		// router's own routeHTTP pass before this mounted router ever runs
		// (chi shares one *Context across a Mount). Changing only the
		// request's Method, as below, would leave this router's tree lookup
		// still matching against the stale HEAD, same as chi's own
		// middleware.GetHead would if it ran here instead of at the mux's
		// own top level.
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			rctx.RouteMethod = http.MethodGet
		}
		r2 := r.Clone(r.Context())
		r2.Method = http.MethodGet
		next.ServeHTTP(headDiscardingWriter{w}, r2)
	})
}

// headDiscardingWriter passes every header and status write through
// unchanged and drops only the body: what HEAD asks for.
type headDiscardingWriter struct{ http.ResponseWriter }

func (headDiscardingWriter) Write(b []byte) (int, error) { return len(b), nil }

// optionsRefused answers every OPTIONS request with a flat 405: empty body,
// no Allow header naming the route's methods (#66). Afloat is same-origin
// with no CORS, so no client sends OPTIONS, and answering it identically on
// every path - registered, unregistered, or the disabled-login route - is
// what keeps the disabled route indistinguishable from one never registered
// at all (#24's rule "on every method"), without a second gate that could
// drift from disabledLocalLogin404's. Mounted ahead of routing entirely, so
// chi's own method-not-allowed handling (which would add Allow: GET, HEAD for
// a route it does find) never runs for this method.
func optionsRefused(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestIDHeader carries the correlation id in both directions: honoured
// inbound, always written outbound.
const requestIDHeader = "X-Request-Id"

// maxRequestIDLength caps an honoured inbound id, after sanitising.
const maxRequestIDLength = 64

type requestIDKey struct{}

// requestID replaces chi's middleware.RequestID, which echoes an inbound id
// unfiltered — the header is attacker-controlled in a self-hosted deployment
// with nothing in front, and it lands in every log line — and mints ids of
// the form host/prefix-000001, which leak the hostname and count requests.
//
// Matches Kotlin's RequestLogFilter byte for byte (BOOTSTRAP.md §5.2, #26):
// an inbound id keeps only ASCII letters, digits, '-' and '_', is then cut to
// 64 bytes, and is minted afresh if nothing survives. A minted id is 8 random
// bytes as 16 lowercase hex characters.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitiseRequestID(r.Header.Get(requestIDHeader))
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func sanitiseRequestID(supplied string) string {
	b := make([]byte, 0, min(len(supplied), maxRequestIDLength))
	for i := 0; i < len(supplied) && len(b) < maxRequestIDLength; i++ {
		c := supplied[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			b = append(b, c)
		}
	}
	return string(b)
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error (Go 1.24+).
	return hex.EncodeToString(b[:])
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// securityHeaders writes the fixed set BOOTSTRAP.md §5.2 pins, matching what
// Kotlin sends via Spring Security's defaults (issue #26). The values never
// depend on the request, so every response carries the same six.
//
// Strict-Transport-Security is not among them, in either backend: HSTS is
// policy about the operator's domain, and whatever terminates TLS owns it.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
		h.Set("Pragma", "no-cache")
		h.Set("Expires", "0")
		h.Set("X-XSS-Protection", "0")
		next.ServeHTTP(w, r)
	})
}
