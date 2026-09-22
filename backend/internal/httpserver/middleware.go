package httpserver

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

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
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

// localLoginPath is POST /api/auth/local/login's full mounted path — the one
// route disabledLocalLogin404 gates. It is spelled out here rather than
// derived from the generated api package, which has no constant for it.
const localLoginPath = "/api/auth/local/login"

// disabledLocalLogin404 makes POST /api/auth/local/login answer a bare 404
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
			if r.Method == http.MethodPost && r.URL.Path == localLoginPath {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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
