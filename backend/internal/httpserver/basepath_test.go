package httpserver

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// What counts as a base path (#17): the spec's servers[0].url is where every
// route mounts, so anything chi cannot mount at must stop the process at boot
// rather than serve the API somewhere the client does not look.
func TestBasePathOf(t *testing.T) {
	for _, tc := range []struct {
		name    string
		servers openapi3.Servers
		want    string
		wantErr string
	}{
		{"the contract's own", openapi3.Servers{{URL: "/api"}}, "/api", ""},
		{"a deeper prefix", openapi3.Servers{{URL: "/afloat/api"}}, "/afloat/api", ""},
		{"no servers entry", nil, "", "no servers entry"},
		{"a host", openapi3.Servers{{URL: "https://example.com/api"}}, "", "not a base path"},
		{"protocol-relative", openapi3.Servers{{URL: "//example.com/api"}}, "", "not a base path"},
		{"relative", openapi3.Servers{{URL: "api"}}, "", "not a base path"},
		{"a trailing slash", openapi3.Servers{{URL: "/api/"}}, "", "not a base path"},
		{"the root", openapi3.Servers{{URL: "/"}}, "", "not a base path"},
		{"a server variable", openapi3.Servers{{URL: "/{version}"}}, "", "not a base path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := basePathOf(&openapi3.T{Servers: tc.servers})
			switch {
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("want an error mentioning %q, got %q, %v", tc.wantErr, got, err)
			case tc.wantErr == "" && (err != nil || got != tc.want):
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// The value in use is the contract file's, and the login gate is built from it,
// so the gate cannot guard a path the router no longer serves. Read from
// contract/openapi.yaml itself, not the embedded copy: that the two match is
// check-generated.sh's to prove, and this must not pass on a stale embed.
func TestBasePathIsTheContracts(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile("../../../contract/openapi.yaml")
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	want, err := basePathOf(spec)
	if err != nil {
		t.Fatalf("contract: %v", err)
	}
	if basePath != want {
		t.Errorf("basePath = %q; contract/openapi.yaml's servers[0].url is %q", basePath, want)
	}
	for path := range localPaths {
		if !strings.HasPrefix(path, basePath+"/auth/local/") {
			t.Errorf("gated path %q is not under basePath %q's /auth/local/", path, basePath)
		}
	}
}

// Every route the contract puts under /auth/local/ is behind the disabled-local
// gate, and the gate guards nothing the contract does not declare. The gate is
// a hand-kept list (the generated router mounts every operation
// unconditionally, so it cannot be skipped per route), and a path left off it
// stays live with local sign-in off: this is what catches one.
func TestEveryLocalRouteIsGated(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile("../../../contract/openapi.yaml")
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	declared := map[string]bool{}
	for path := range spec.Paths.Map() {
		if strings.HasPrefix(path, "/auth/local/") {
			declared[basePath+path] = true
		}
	}
	if len(declared) == 0 {
		t.Fatal("the contract declares no /auth/local/ route; the test reads the wrong thing")
	}
	for path := range declared {
		if _, gated := localPaths[path]; !gated {
			t.Errorf("%s is in the contract under /auth/local/ but not behind disabledLocalAuth404", path)
		}
	}
	for path := range localPaths {
		if !declared[path] {
			t.Errorf("%s is gated but the contract declares no such route", path)
		}
	}
}
