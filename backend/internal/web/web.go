// Package web serves the built frontend from the binary (BOOTSTRAP.md §8).
//
// //go:embed cannot reach a parent directory, so the build copies
// frontend/dist into dist/ here (`make frontend-build`), as the migrations
// live in the package that embeds them. dist/ is build output and gitignored
// apart from a .gitkeep, which keeps the embed pattern valid in a checkout
// with no frontend built: Handler then returns nil and the server serves the
// API alone, which is what `make check` and the conformance suite exercise.
package web

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var embedded embed.FS

// Handler serves the embedded build, or returns nil when there is none.
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil
	}
	return NewHandler(sub)
}

// NewHandler serves an SPA build from fsys, or returns nil when fsys has no
// index.html. Split from Handler so tests can hand it an fstest.MapFS.
//
// The rules (BOOTSTRAP.md §8):
//   - a file that exists is served as itself;
//   - a path with no file and no extension is a client-side route, and gets
//     index.html - except under /api, which the API owns, so a typo there
//     never comes back as an HTML page with status 200;
//   - a missing path WITH an extension (a stale /assets/old.js, a missing
//     /sw.js) is a 404, never index.html served as JavaScript;
//   - index.html, the service worker and the manifest are revalidated on
//     every load, or an installed PWA keeps running the old shell; the
//     content-hashed files under /assets are cached for a year.
func NewHandler(fsys fs.FS) http.Handler {
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "api" || strings.HasPrefix(name, "api/") {
			http.NotFound(w, r)
			return
		}
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(fsys, name); err != nil || info.IsDir() {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}
		serveFile(w, r, fsys, name)
	})
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	f, err := fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	// Overrides the API's no-store default (httpserver.securityHeaders): a
	// static file is revalidated or, when content-hashed, kept for a year.
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	// Go's MIME table has no .webmanifest, and text/plain is not a manifest.
	if path.Ext(name) == ".webmanifest" {
		w.Header().Set("Content-Type", "application/manifest+json")
	}
	w.Header().Del("Pragma")
	w.Header().Del("Expires")
	// The embedded files carry no modification time, so ETag-less
	// revalidation falls back to a full response: correct, and the files are
	// small. http.ServeContent picks the Content-Type from the extension.
	http.ServeContent(w, r, name, time.Time{}, rs)
}
