package conformance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kerti/afloat/conformance"
)

// The case format is hand-written YAML, so the failure it is most likely to
// have is a file that parses and asserts nothing. These prove the loader
// rejects that, and they run without either backend.

func writeCases(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cases.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadRejectsUnusableCases(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "no name",
			yaml: "- request: {method: GET, path: /health}\n  expect: {status: 200}\n",
			want: "no name",
		},
		{
			name: "no method",
			yaml: "- name: x\n  request: {path: /health}\n  expect: {status: 200}\n",
			want: "request.method is required",
		},
		{
			name: "path includes the api base",
			yaml: "- name: x\n  request: {method: GET, path: api/health}\n  expect: {status: 200}\n",
			want: "must start with /",
		},
		{
			name: "no status",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {}\n",
			want: "expect.status is required",
		},
		{
			name: "two body assertions",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {status: 200, body_raw: \"a\", body_json: {b: 1}}\n",
			want: "mutually exclusive",
		},
		{
			name: "empty body and a body assertion",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {status: 204, body_empty: true, body_raw: \"a\"}\n",
			want: "cannot be combined",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := conformance.Load(writeCases(t, tc.yaml))
			if err == nil {
				t.Fatalf("want an error mentioning %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

// Case names are how a failure is reported and how a run is read, so two cases
// sharing one is a silent loss of a result.
func TestLoadRejectsDuplicateNames(t *testing.T) {
	dir := writeCases(t, "- name: same\n  request: {method: GET, path: /health}\n  expect: {status: 200}\n"+
		"- name: same\n  request: {method: GET, path: /health}\n  expect: {status: 200}\n")
	if _, err := conformance.Load(dir); err == nil || !strings.Contains(err.Error(), "duplicate case name") {
		t.Fatalf("want a duplicate-name error, got %v", err)
	}
}

// An empty cases directory must not read as a green run of zero cases.
func TestLoadRejectsAnEmptyDirectory(t *testing.T) {
	if _, err := conformance.Load(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no case files") {
		t.Fatalf("want a no-cases error, got %v", err)
	}
}

// The committed cases and allowlist must always load, whether or not a backend
// is up - otherwise a typo in either is found only on a machine running both.
func TestCommittedFilesLoad(t *testing.T) {
	cases, err := conformance.Load("cases")
	if err != nil {
		t.Fatalf("cases/: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("cases/ loaded zero cases")
	}
	if _, err := conformance.LoadPermitted("permitted-differences.yaml"); err != nil {
		t.Fatalf("permitted-differences.yaml: %v", err)
	}
}
