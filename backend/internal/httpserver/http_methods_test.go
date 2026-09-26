package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/system"
)

// #66: HEAD answers like GET (RFC 9110 §9.1), same status and headers, empty
// body.
func TestHeadAnswersLikeGet(t *testing.T) {
	srv := newTestServer()

	get := httptest.NewRecorder()
	srv.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	head := httptest.NewRecorder()
	srv.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/api/health", nil))

	if head.Code != get.Code {
		t.Errorf("HEAD status = %d, want %d (GET's)", head.Code, get.Code)
	}
	if ct := head.Header().Get("Content-Type"); ct != get.Header().Get("Content-Type") {
		t.Errorf("HEAD Content-Type = %q, want %q (GET's)", ct, get.Header().Get("Content-Type"))
	}
	if head.Body.Len() != 0 {
		t.Errorf("HEAD body = %q, want empty", head.Body.String())
	}
}

// A POST-only route answers HEAD the way it answers any other wrong method:
// 405, not a 200 with an empty GET body that was never there to give.
func TestHeadOnAPostOnlyRouteIsMethodNotAllowed(t *testing.T) {
	srv := newTestServer()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/api/auth/logout", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("HEAD /api/auth/logout = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// #66: OPTIONS is a flat, uninformative 405 everywhere - registered route,
// unregistered path, and the disabled-login gate alike - so none of the three
// can be told apart by the one header (Allow) that would otherwise vary.
func TestOptionsIsAlwaysMethodNotAllowedWithNoAllowHeader(t *testing.T) {
	srv := newTestServer()

	for _, path := range []string{"/api/health", "/api/nope", "/api/auth/local/login"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, path, nil))

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("OPTIONS %s = %d, want %d", path, rec.Code, http.StatusMethodNotAllowed)
		}
		if allow := rec.Header().Get("Allow"); allow != "" {
			t.Errorf("OPTIONS %s: Allow = %q, want no Allow header", path, allow)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("OPTIONS %s: body = %q, want empty", path, rec.Body.String())
		}
	}
}

// The disabled-login gate must answer OPTIONS exactly like an unregistered
// path, the second-review finding on #66: with the provider off, distinguishing
// the two would tell a prober the route exists.
func TestOptionsOnDisabledLoginMatchesUnregisteredPath(t *testing.T) {
	q := fakeQuerier{}
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

	disabled := httptest.NewRecorder()
	srv.ServeHTTP(disabled, httptest.NewRequest(http.MethodOptions, "/api/auth/local/login", nil))

	unregistered := httptest.NewRecorder()
	srv.ServeHTTP(unregistered, httptest.NewRequest(http.MethodOptions, "/api/no-such-route", nil))

	if disabled.Code != unregistered.Code {
		t.Errorf("disabled-login OPTIONS = %d, unregistered-path OPTIONS = %d, want equal", disabled.Code, unregistered.Code)
	}
	if disabled.Header().Get("Allow") != unregistered.Header().Get("Allow") {
		t.Errorf("Allow headers differ: disabled-login %q, unregistered %q",
			disabled.Header().Get("Allow"), unregistered.Header().Get("Allow"))
	}
}
