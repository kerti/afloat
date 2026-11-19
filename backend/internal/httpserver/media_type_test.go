package httpserver

import (
	"net/http"
	"testing"
)

// canonicalMediaType's own rule, independent of kin-openapi: a valid media
// type has its type and subtype lowercased (ASCII only) and is otherwise left
// alone; anything that is not token "/" token is dropped, so a body route
// refuses it as undeclared. From v0.150.0 kin-openapi folds Unicode itself,
// so the drop is what keeps "applİcation/json" from reading as JSON (#89).
func TestCanonicalMediaType(t *testing.T) {
	for _, tc := range []struct {
		in, want string // want "" means the header is dropped
	}{
		{"application/json", "application/json"},
		{"Application/JSON", "application/json"},
		{" application/json ; charset=utf-8", "application/json; charset=utf-8"},
		{"application/vnd.api+json", "application/vnd.api+json"},
		{"*/*", "*/*"},
		{"applİcation/json", ""},
		{"application/jſon", ""}, // U+017F folds to "s" under Unicode rules
		{"application", ""},
		{"application/", ""},
		{"/json", ""},
		{"application json", ""},
		{"application/json/x", ""},
		{"   ", ""},
		{"application/json;", "application/json;"},
		// Every RFC 9110 §5.6.2 tchar punctuation byte is kept...
		{"a!#$%&'*+-.^_`|~/b", "a!#$%&'*+-.^_`|~/b"},
		// ...and ASCII punctuation outside tchar is not.
		{"applic(ation/json", ""},
		{"application/js@on", ""},
		{"application/json,text/plain", ""},
		{`"application/json"`, ""},
		{"application/js\x7fon", ""},
	} {
		h := http.Header{"Content-Type": {tc.in}}
		canonicalMediaType(h)
		got, present := h.Get("Content-Type"), len(h.Values("Content-Type")) > 0
		switch {
		case tc.want == "" && present:
			t.Errorf("%q: kept as %q, want the header dropped", tc.in, got)
		case tc.want != "" && got != tc.want:
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}
