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
	if localLoginPath != basePath+"/auth/local/login" {
		t.Errorf("localLoginPath = %q, not under basePath %q", localLoginPath, basePath)
	}
}
