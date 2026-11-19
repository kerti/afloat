package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func build() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>Afloat</title>")},
		"sw.js":                {Data: []byte("self.addEventListener('install',()=>{})")},
		"manifest.webmanifest": {Data: []byte(`{"name":"Afloat"}`)},
		"icon.svg":             {Data: []byte("<svg/>")},
		"assets/index-abc.js":  {Data: []byte("console.log(1)")},
	}
}

func get(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestNewHandlerWithoutABuildIsNil(t *testing.T) {
	if NewHandler(fstest.MapFS{".gitkeep": {}}) != nil {
		t.Fatal("a build with no index.html must give a nil handler, so the server serves the API alone")
	}
}

func TestSPAServing(t *testing.T) {
	h := NewHandler(build())
	for _, tc := range []struct {
		name, method, target string
		status               int
		body, cacheControl   string
		contentType          string
	}{
		{"root is the shell", "GET", "/", 200, "<!doctype html><title>Afloat</title>", "no-cache", "text/html; charset=utf-8"},
		{"a client route is the shell", "GET", "/login", 200, "<!doctype html><title>Afloat</title>", "no-cache", "text/html; charset=utf-8"},
		{"a nested client route is the shell", "GET", "/budgets/2026-11", 200, "<!doctype html><title>Afloat</title>", "no-cache", "text/html; charset=utf-8"},
		{"index.html by name", "GET", "/index.html", 200, "<!doctype html><title>Afloat</title>", "no-cache", "text/html; charset=utf-8"},
		{"the service worker is itself, revalidated", "GET", "/sw.js", 200, "self.addEventListener('install',()=>{})", "no-cache", "text/javascript; charset=utf-8"},
		{"the manifest is revalidated", "GET", "/manifest.webmanifest", 200, `{"name":"Afloat"}`, "no-cache", "application/manifest+json"},
		{"a hashed asset is cached for a year", "GET", "/assets/index-abc.js", 200, "console.log(1)", "public, max-age=31536000, immutable", "text/javascript; charset=utf-8"},
		{"a missing asset is a 404, never the shell", "GET", "/assets/index-old.js", 404, "", "", ""},
		{"a missing file with an extension is a 404", "GET", "/favicon.ico", 404, "", "", ""},
		{"/api is never the shell", "GET", "/api", 404, "", "", ""},
		{"a path under /api is never the shell", "GET", "/api/no-such-route", 404, "", "", ""},
		{"a dot segment cannot climb out", "GET", "/../index.html", 200, "<!doctype html><title>Afloat</title>", "no-cache", "text/html; charset=utf-8"},
		{"a directory is the shell, never a listing", "GET", "/assets", 200, "<!doctype html><title>Afloat</title>", "no-cache", "text/html; charset=utf-8"},
		{"a directory with a slash is the shell too", "GET", "/assets/", 200, "<!doctype html><title>Afloat</title>", "no-cache", "text/html; charset=utf-8"},
		{"HEAD answers like GET with no body", "HEAD", "/login", 200, "", "no-cache", "text/html; charset=utf-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(t, h, tc.method, tc.target)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status != 200 {
				return
			}
			if got := rec.Body.String(); got != tc.body {
				t.Errorf("body = %q, want %q", got, tc.body)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.cacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, tc.cacheControl)
			}
			if tc.contentType != "" {
				if got := rec.Header().Get("Content-Type"); got != tc.contentType {
					t.Errorf("Content-Type = %q, want %q", got, tc.contentType)
				}
			}
		})
	}
}
