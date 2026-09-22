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
		System: system.New(system.Deps{Querier: panicQuerier{}, LocalEnabled: true}),
		Auth:   auth.New(auth.Deps{Querier: panicQuerier{}}),
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
	})

	body := `{"email":"a@example.com","password":"whatever-it-does-not-matter"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	// GET too: with a method-scoped gate chi answers 405 here, which tells a
	// prober the route exists.
	reqGet := httptest.NewRequest(http.MethodGet, "/api/auth/local/login", nil)
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
		t.Errorf("Content-Type = %q, want %q (same as an unmatched route)", ct, wantCt)
	}
	if rec.Body.String() != unmatched.Body.String() {
		t.Errorf("POST body = %q, want %q (same as an unmatched route)", rec.Body.String(), unmatched.Body.String())
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
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/local/login", nil))

	if rec.Code == http.StatusNotFound {
		t.Error("status = 404 with the local provider enabled; the route must still exist")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (no body), not a gate-related change", rec.Code)
	}
}
