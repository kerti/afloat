package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
// refused by MaxBytesReader mid-decode, which is one of the generated server's
// escape hatches and answered http.Error — text/plain carrying an English
// message — until errors.go wired it.
func TestBodyIsCapped(t *testing.T) {
	srv := newTestServer()

	huge := strings.NewReader(`{"email":"` + strings.Repeat("a", 2<<20) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", huge)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an oversized body", rec.Code)
	}
	assertEnvelope(t, rec, string(api.INVALIDJSONBODY))
}

// A body that did not decode is INVALID_JSON_BODY, in the envelope. The
// generated strict handler decodes before any handler runs, so nothing in
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
			req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(tc.body))
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
// Strict-Transport-Security: that one is Tomcat's HTTPS-only behaviour, a
// documented deliberate difference from Kotlin, not something Go sends.
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
	if got := rec.Header().Get("X-Request-Id"); got == "" {
		t.Error("X-Request-Id is empty; chi's middleware.RequestID mints one and this must echo it")
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want unset — that is Tomcat's HTTPS-only behaviour, not Go's", got)
	}
}

// An incoming X-Request-Id is echoed, not overwritten: chi's middleware.
// RequestID already prefers it (request_id.go), and this only has to not
// lose what RequestID put in context.
func TestSecurityHeadersEchoesIncomingRequestID(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-Request-Id", "caller-supplied-id")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "caller-supplied-id" {
		t.Errorf("X-Request-Id = %q, want the incoming id echoed back", got)
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
