package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/oapi-codegen/runtime"
)

// A path parameter is percent-decoded exactly once (#65).
//
// The contract has no path parameter yet, so this binds the way generated code
// does. The call below is oapi-codegen v2.8.0's chi-middleware.tmpl, verbatim
// but for the names: ValueIsUnescaped tells the runtime that chi already
// decoded the value, which it did whenever RawPath is empty - and
// routeOnDecodedPath empties it on every path but one with an encoded slash.
// Before runtime v1.9.0 (#89) the binder decoded unconditionally, a second
// time. Step 9's first {id} route takes over with conformance cases.
func TestPathParametersAreDecodedOnce(t *testing.T) {
	api := chi.NewRouter()
	api.Get("/things/{id}", func(w http.ResponseWriter, r *http.Request) {
		var id string
		err := runtime.BindStyledParameterWithOptions("simple", "id", chi.URLParam(r, "id"), &id,
			runtime.BindStyledParameterOptions{ParamLocation: runtime.ParamLocationPath, Explode: false, Required: true, Type: "string", Format: "", ValueIsUnescaped: r.URL.RawPath == ""})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(id))
	})
	// The same shape as New: the middleware on an outer router, the routes
	// under a mount.
	outer := chi.NewRouter()
	outer.Use(routeOnDecodedPath)
	outer.Mount(basePath, api)

	for _, tc := range []struct{ path, want string }{
		{"/things/abc", "abc"},
		{"/things/%61bc", "abc"},
		{"/things/%2541", "%41"},
		{"/things/%25", "%"},
		{"/things/100%25", "100%"},
		{"/things/%252F", "%2F"},
		{"/things/caf%C3%A9", "café"},
		// An encoded slash keeps chi on the raw path; the binder then decodes
		// it once, so the separator stays inside the value.
		{"/things/abc%2Fdef", "abc/def"},
		{"/things/%2541%2F", "%41/"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			outer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, basePath+tc.path, nil))
			if rec.Code != http.StatusOK || rec.Body.String() != tc.want {
				t.Errorf("got %d %q, want 200 %q", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}
