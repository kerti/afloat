package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/system"
)

type fakeQuerier struct{ db.Querier }

func (fakeQuerier) Ping(context.Context) (int32, error) { return 1, nil }

// testHandlerTimeout is what production gets by default (config.Config's
// HTTP_WRITE_TIMEOUT envDefault) — long enough that no test in this file
// competes with it. TestHandlerTimeoutReadsConfig is the one test that
// overrides it.
const testHandlerTimeout = 60 * time.Second

func newTestServer() http.Handler {
	q := fakeQuerier{}
	return New(Deps{
		System: system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: true}),
		Auth: auth.New(auth.Deps{
			Querier:            q,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
		HandlerTimeout: testHandlerTimeout,
	})
}

// The contract's servers entry is /api, so every route lives under that prefix.
// A route answering at the bare path would mean the client and the server
// disagree about where the API is.
func TestRoutesAreMountedUnderAPI(t *testing.T) {
	srv := newTestServer()

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/health", http.StatusOK},
		{http.MethodGet, "/api/auth/methods", http.StatusOK},
		{http.MethodGet, "/api/me", http.StatusUnauthorized},
		{http.MethodPost, "/api/auth/logout", http.StatusNoContent},
		{http.MethodPost, "/api/auth/local/login", http.StatusBadRequest}, // no body
		{http.MethodGet, "/health", http.StatusNotFound},
		{http.MethodGet, "/api/nope", http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}

// An unauthenticated request must produce the shared envelope, not a bare
// status or a plain-text body: the frontend localises by code and has nothing
// to show otherwise.
func TestErrorsUseTheSharedEnvelope(t *testing.T) {
	srv := newTestServer()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body["code"] != string(api.UNAUTHORIZED) {
		t.Errorf("code = %v, want UNAUTHORIZED", body["code"])
	}
	// No message field, ever: the frontend owns every user-facing string, and a
	// second home for copy is how the two drift (BOOTSTRAP.md §5.2).
	if _, present := body["message"]; present {
		t.Error("envelope carries a message field; the frontend owns copy")
	}
}

// Balances caps request bodies on its file-upload handlers only, leaving an
// unbounded decode everywhere else. This is that gap closed.
//
// The assertion is the envelope, not merely "not 200": the oversized body is
// refused by MaxBytesReader mid-read, outside any handler, where the default
// answer was http.Error — text/plain carrying an English message. The
// spec-validating middleware is the reader now (openapi_validate.go); before
// it, the generated decode was, and errors.go had to wire it.
func TestBodyIsCapped(t *testing.T) {
	srv := newTestServer()

	huge := strings.NewReader(`{"email":"` + strings.Repeat("a", 2<<20) + `"}`)
	req := httptest.NewRequest(http.MethodPost, localLoginPath, huge)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an oversized body", rec.Code)
	}
	assertEnvelope(t, rec, string(api.INVALIDJSONBODY))
}

// A body that did not decode is INVALID_JSON_BODY, in the envelope. The
// spec-validating middleware rejects it before any handler runs, so nothing in
// internal/auth can answer this path — it is the server's to wire, and its
// default was a plain-text message (PRD N7, non-negotiable 7).
func TestMalformedBodyIsTheEnvelope(t *testing.T) {
	srv := newTestServer()

	for _, tc := range []struct{ name, body string }{
		{"truncated", `{"email": `},
		{"not an object", `["email"]`},
		{"empty", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, localLoginPath, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			assertEnvelope(t, rec, string(api.INVALIDJSONBODY))
		})
	}
}

// assertEnvelope checks the one shape both backends emit: application/json,
// a `code`, and no `message` anywhere.
func assertEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json (body %q)", ct, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body["code"] != wantCode {
		t.Errorf("code = %v, want %s", body["code"], wantCode)
	}
	if _, present := body["message"]; present {
		t.Error("envelope carries a message field; the frontend owns copy")
	}
}

// A panicking handler must not take the process down and drop every other
// in-flight request with it.
func TestPanicIsRecovered(t *testing.T) {
	srv := New(Deps{
		System:         system.New(system.Deps{Querier: panicQuerier{}, LocalEnabled: true}),
		Auth:           auth.New(auth.Deps{Querier: panicQuerier{}}),
		HandlerTimeout: testHandlerTimeout,
	})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 from the recoverer", rec.Code)
	}
	assertEnvelope(t, rec, string(api.INTERNAL))
}

type panicQuerier struct{ db.Querier }

func (panicQuerier) Ping(context.Context) (int32, error) { panic("boom") }

// The full pinned set (#26), asserted on a representative response. Not
// Strict-Transport-Security, in either backend: whatever terminates TLS owns
// it (BOOTSTRAP.md §5.2).
func TestSecurityHeadersOnHealth(t *testing.T) {
	srv := newTestServer()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Cache-Control":          "no-cache, no-store, max-age=0, must-revalidate",
		"Pragma":                 "no-cache",
		"Expires":                "0",
		"X-XSS-Protection":       "0",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want unset — the TLS terminator owns HSTS, not either backend", got)
	}
}

var mintedRequestID = regexp.MustCompile(`^[0-9a-f]{16}$`)

// The inbound rule Kotlin's RequestLogFilter applies, byte for byte: keep
// ASCII letters, digits, '-' and '_', cut to 64, mint if nothing survives.
func TestRequestIDFromInbound(t *testing.T) {
	cases := []struct {
		name, inbound, want string // want "" means a freshly minted id
	}{
		{"clean id is echoed", "caller-supplied_ID-42", "caller-supplied_ID-42"},
		{"disallowed ASCII is dropped", "ab<c>d e;f/g.h:i", "abcdefghi"},
		{"non-ASCII is dropped, not just decoded", "abcé字1", "abc1"},
		{"cut to 64 after filtering", "<" + strings.Repeat("a", 70), strings.Repeat("a", 64)},
		{"nothing survives, so one is minted", "<>;/.:", ""},
		{"absent, so one is minted", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var inContext string
			h := requestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				inContext = requestIDFrom(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.inbound != "" {
				req.Header.Set("X-Request-Id", tc.inbound)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			got := rec.Header().Get("X-Request-Id")
			switch {
			case tc.want == "" && !mintedRequestID.MatchString(got):
				t.Errorf("X-Request-Id = %q, want a minted id (16 lowercase hex)", got)
			case tc.want != "" && got != tc.want:
				t.Errorf("X-Request-Id = %q, want %q", got, tc.want)
			}
			// requestLogger reads the context; the response must not name a
			// different request than the log line does.
			if inContext != got {
				t.Errorf("context id = %q, response header = %q; they must be the same id", inContext, got)
			}
		})
	}
}

// Through the whole router, so a later middleware that rewrote or dropped the
// header would be caught — including on a response the router answers itself.
func TestRequestIDOnEveryResponse(t *testing.T) {
	srv := newTestServer()

	for _, path := range []string{"/api/health", "/api/genuinely-unregistered"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Request-Id", "from-the-proxy")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if got := rec.Header().Get("X-Request-Id"); got != "from-the-proxy" {
			t.Errorf("%s: X-Request-Id = %q, want the inbound id echoed", path, got)
		}
	}
}

// slowQuerier's Ping respects context cancellation the way pgx does against a
// real deadline, standing in for a handler slow enough to hit HandlerTimeout.
type slowQuerier struct {
	db.Querier
	delay time.Duration
}

func (s slowQuerier) Ping(ctx context.Context) (int32, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(s.delay):
		return 1, nil
	}
}

// middleware.Timeout used to read a bare `30 * time.Second` literal (#30).
// HandlerTimeout is set here to 20ms against a handler that would otherwise
// take 2s: if the literal ever creeps back in, ctx would not cancel until
// 30s and this test would time out waiting on the 2s Ping instead of
// returning in well under a second.
func TestHandlerTimeoutReadsConfig(t *testing.T) {
	q := slowQuerier{delay: 2 * time.Second}
	srv := New(Deps{
		System:         system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: true}),
		Auth:           auth.New(auth.Deps{Querier: q}),
		HandlerTimeout: 20 * time.Millisecond,
	})

	started := time.Now()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	elapsed := time.Since(started)

	if elapsed > 500*time.Millisecond {
		t.Errorf("handler returned after %s, want it cut off near the configured 20ms HandlerTimeout (not left to run the full 2s, and not bound to the old 30s literal)", elapsed)
	}
	// GetHealth answers a cancelled Ping the same way it answers any other
	// Ping error: 503, degraded, in its own JSON shape (system.go) — reached
	// because Ping observed ctx.Done() and returned, not because
	// middleware.Timeout's own deferred write raced it.
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// spyQuerier's every unstubbed method panics (db.Querier is embedded nil),
// which is deliberate: if disabledLocalLogin404 ever let a request through
// to the handler, the test fails loudly on that panic rather than passing by
// accident. CreateSession is the one method it does stub, so a session issued
// in spite of the gate is a recorded fact, not a guess.
type spyQuerier struct {
	db.Querier
	sessionCreated bool
}

func (q *spyQuerier) CreateSession(_ context.Context, _ db.CreateSessionParams) (db.Session, error) {
	q.sessionCreated = true
	return db.Session{}, nil
}

// issue #24 (Go side): with the local provider off, the login route must not
// exist — a bare 404 indistinguishable from any other unmatched path, not a
// 401/403 answered from inside the handler — and /auth/methods must still
// report it accurately.
func TestLocalLoginDisabledIsABareNotFound(t *testing.T) {
	q := &spyQuerier{}
	srv := New(Deps{
		System: system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: false}),
		Auth: auth.New(auth.Deps{
			Querier:            q,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
		HandlerTimeout: testHandlerTimeout,
	})

	body := `{"email":"a@example.com","password":"whatever-it-does-not-matter"}`
	req := httptest.NewRequest(http.MethodPost, localLoginPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("POST status = %d, want 404", rec.Code)
	}

	// GET too: with a method-scoped gate chi answers 405 here, which tells a
	// prober the route exists.
	reqGet := httptest.NewRequest(http.MethodGet, localLoginPath, nil)
	recGet := httptest.NewRecorder()
	srv.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusNotFound {
		t.Errorf("GET status = %d, want 404", recGet.Code)
	}

	// Not just any 404: the same one chi hands back for a path that was never
	// registered — proof this isn't an error envelope wearing a 404, and that
	// the route genuinely does not exist rather than existing-but-refusing.
	unmatched := httptest.NewRecorder()

	srv.ServeHTTP(unmatched, httptest.NewRequest(http.MethodGet, "/api/genuinely-unregistered", nil))
	if ct, wantCt := rec.Header().Get("Content-Type"), unmatched.Header().Get("Content-Type"); ct != wantCt {
		t.Errorf("POST Content-Type = %q, want %q (same as an unmatched route)", ct, wantCt)
	}
	if rec.Body.String() != unmatched.Body.String() {
		t.Errorf("POST body = %q, want %q (same as an unmatched route)", rec.Body.String(), unmatched.Body.String())
	}

	if ct, wantCt := recGet.Header().Get("Content-Type"), unmatched.Header().Get("Content-Type"); ct != wantCt {
		t.Errorf("GET Content-Type = %q, want %q (same as an unmatched route)", ct, wantCt)
	}
	if recGet.Body.String() != unmatched.Body.String() {
		t.Errorf("GET body = %q, want %q (same as an unmatched route)", recGet.Body.String(), unmatched.Body.String())
	}

	if q.sessionCreated {
		t.Error("a session was created even though the route should not exist")
	}

	// /auth/methods is the single source of truth for which providers exist,
	// and gating the route must not drift from what it reports.
	methodsRec := httptest.NewRecorder()
	srv.ServeHTTP(methodsRec, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))
	var methods struct {
		Local  bool `json:"local"`
		Google bool `json:"google"`
	}
	if err := json.Unmarshal(methodsRec.Body.Bytes(), &methods); err != nil {
		t.Fatalf("auth/methods body is not JSON: %v (%s)", err, methodsRec.Body.String())
	}
	if methods.Local {
		t.Error("auth/methods reports local: true while the route is gated off")
	}
}

// The mirror of the case above: with the default configuration (local
// enabled), gating this route must be a no-op — LocalLogin behaves exactly as
// it did before #24.
func TestLocalLoginEnabledIsUnaffected(t *testing.T) {
	srv := newTestServer()

	// No body: the existing TestRoutesAreMountedUnderAPI already covers this
	// exact case (400, not 404), reasserted here as the explicit "default
	// config is unaffected" contrast to the disabled case above.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, localLoginPath, nil))

	if rec.Code == http.StatusNotFound {
		t.Error("status = 404 with the local provider enabled; the route must still exist")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (no body), not a gate-related change", rec.Code)
	}
}

// The body cap applies to the bytes a handler reads (#56). Logout reads none,
// so an oversize body on it is never met. It was: kin-openapi's security check
// read the whole body before calling even a no-op AuthenticationFunc, the read
// hit maxBodyBytes, and the failure came back as a 401.
func TestAnOversizeBodyOnARouteThatReadsNoneIsIgnored(t *testing.T) {
	srv := newTestServer()
	body := strings.Repeat("a", 3<<19) // 1.5 MiB, past the 1 MiB cap

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
}

// A percent-encoded path is its decoded route; an encoded slash is not a
// separator (#56).
func TestRoutesOnTheDecodedPath(t *testing.T) {
	srv := newTestServer()
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/h%65alth", http.StatusOK},
		{"/api/%68ealth", http.StatusOK},
		{"/api/auth/%6Dethods", http.StatusOK},
		{"/api/auth%2Fmethods", http.StatusNotFound},
		{"/api/auth%2fmethods", http.StatusNotFound},
		{"/api/h%25ealth", http.StatusNotFound},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
