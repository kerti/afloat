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
func TestBodyIsCapped(t *testing.T) {
	srv := newTestServer()

	huge := strings.NewReader(`{"email":"` + strings.Repeat("a", 2<<20) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", huge)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("an oversized body was accepted (status %d)", rec.Code)
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
}

type panicQuerier struct{ db.Querier }

func (panicQuerier) Ping(context.Context) (int32, error) { panic("boom") }
