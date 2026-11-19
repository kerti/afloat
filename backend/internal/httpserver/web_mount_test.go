package httpserver

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/system"
	"github.com/kerti/afloat/backend/internal/web"
)

func newTestServerWithWeb(t *testing.T) http.Handler {
	t.Helper()
	q := fakeQuerier{}
	h := web.NewHandler(fstest.MapFS{
		"index.html":          {Data: []byte("<!doctype html><title>Afloat</title>")},
		"assets/index-abc.js": {Data: []byte("console.log(1)")},
	})
	if h == nil {
		t.Fatal("web.NewHandler returned nil for a build with index.html")
	}
	return New(Deps{
		System:         system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: true}),
		Auth:           auth.New(auth.Deps{Querier: q, SessionTTL: time.Hour, SessionMaxLifetime: 2 * time.Hour}),
		Web:            h,
		HandlerTimeout: testHandlerTimeout,
	})
}

// BOOTSTRAP.md §8: the frontend is served outside /api, and the API keeps
// every answer it gave before a frontend was mounted.
func TestWebMount(t *testing.T) {
	srv := newTestServerWithWeb(t)
	serve := func(method, target, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	t.Run("a client route is the shell, with the fixed security headers", func(t *testing.T) {
		rec := serve("GET", "/login", "")
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>Afloat</title>") {
			t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
		}
		for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "X-Request-Id"} {
			if rec.Header().Get(h) == "" {
				t.Errorf("%s missing on a static answer", h)
			}
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache (the shell is revalidated)", got)
		}
		if rec.Header().Get("Pragma") != "" || rec.Header().Get("Expires") != "" {
			t.Errorf("the API's no-store companions leaked onto a static file: Pragma %q, Expires %q",
				rec.Header().Get("Pragma"), rec.Header().Get("Expires"))
		}
	})

	// fakeQuerier panics on any query, so a 200 here proves the session
	// middleware never looked up the cookie for a static file.
	t.Run("a static file never touches the session store", func(t *testing.T) {
		rec := serve("GET", "/assets/index-abc.js", "afloat_session=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		if rec.Code != 200 {
			t.Fatalf("status %d: a static request reached the session middleware", rec.Code)
		}
	})

	t.Run("the API still answers /api, never with the shell", func(t *testing.T) {
		rec := serve("GET", "/api/health", "")
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
			t.Fatalf("health: status %d, body %q", rec.Code, rec.Body.String())
		}
		rec = serve("GET", "/api/no-such-route", "")
		if rec.Code != 404 || strings.Contains(rec.Body.String(), "<title>") {
			t.Fatalf("an unregistered API path answered %d with %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("/api itself is never the shell", func(t *testing.T) {
		for _, target := range []string{"/api", "/api/"} {
			if rec := serve("GET", target, ""); rec.Code != 404 || strings.Contains(rec.Body.String(), "<title>") {
				t.Errorf("%s answered %d with %q", target, rec.Code, rec.Body.String())
			}
		}
	})

	// Outside /api only GET and HEAD are routed, so a write there is chi's
	// 405 naming exactly those two - never the shell, and never reaching the
	// API's CSRF guard or session lookup (fakeQuerier would panic).
	t.Run("a write outside /api is a 405, not the shell", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/login", strings.NewReader("x"))
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Cookie", "afloat_session=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != 405 || strings.Contains(rec.Body.String(), "<title>") {
			t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
		}
		// chi sends one Allow header per method, in map order: compare as a set.
		allow := rec.Header().Values("Allow")
		slices.Sort(allow)
		if got := strings.Join(allow, ", "); got != "GET, HEAD" {
			t.Errorf("Allow = %q, want GET and HEAD", got)
		}
	})

	t.Run("OPTIONS stays the flat 405 outside the API too", func(t *testing.T) {
		if rec := serve("OPTIONS", "/login", ""); rec.Code != 405 {
			t.Fatalf("status %d, want 405", rec.Code)
		}
	})
}

// Without a frontend build, a path outside the API is the router's plain 404,
// exactly as before Web existed.
func TestNoWebMeansNoShell(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestServer().ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	if rec.Code != 404 {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}
