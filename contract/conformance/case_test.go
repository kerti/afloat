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
			name: "path and raw_target at once",
			yaml: "- name: x\n  request: {method: GET, path: /health, raw_target: /health}\n  expect: {status: 200}\n",
			want: "mutually exclusive",
		},
		{
			name: "raw_target without a leading slash",
			yaml: "- name: x\n  request: {method: GET, raw_target: health}\n  expect: {status: 200}\n",
			want: "raw_target must start with /",
		},
		{
			name: "headers_hex with invalid hex",
			yaml: "- name: x\n  request: {method: GET, path: /health, headers_hex: {User-Agent: \"zz\"}}\n  expect: {status: 200}\n",
			want: "not valid hex",
		},
		{
			name: "headers_hex duplicating a headers name",
			yaml: "- name: x\n  request: {method: GET, path: /health, headers: {User-Agent: x}, headers_hex: {User-Agent: \"78\"}}\n  expect: {status: 200}\n",
			want: "also appears in",
		},
		{
			name: "headers_hex duplicating a headers name, case-insensitively",
			yaml: "- name: x\n  request: {method: GET, path: /health, headers: {user-agent: x}, headers_hex: {User-Agent: \"78\"}}\n  expect: {status: 200}\n",
			want: "also appears in",
		},
		{
			name: "headers_hex naming Host",
			yaml: "- name: x\n  request: {method: GET, path: /health, headers_hex: {Host: \"78\"}}\n  expect: {status: 200}\n",
			want: "Host has no raw-bytes equivalent",
		},
		{
			name: "no status",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {}\n",
			want: "expect.status is required",
		},
		{
			name: "status and status_by_backend at once",
			yaml: "- name: x\n  permit: [p]\n  request: {method: GET, path: /health}\n  expect: {status: 200, status_by_backend: {go: 404, kotlin: 400}}\n",
			want: "mutually exclusive",
		},
		{
			name: "status_by_backend missing a backend",
			yaml: "- name: x\n  permit: [p]\n  request: {method: GET, path: /health}\n  expect: {status_by_backend: {go: 404}}\n",
			want: "missing \"kotlin\"",
		},
		{
			name: "status_by_backend with no permit",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {status_by_backend: {go: 404, kotlin: 400}}\n",
			want: "needs a case-scoped permit entry",
		},
		{
			// S4 / review finding: a permit on GET /health with
			// status_by_backend {go: 200, kotlin: 200} is nonsense - the two
			// backends do not differ at all, so this is not a status
			// permitted to differ, and belongs in expect.status instead.
			name: "status_by_backend with the same status for both backends",
			yaml: "- name: x\n  permit: [p]\n  request: {method: GET, path: /health}\n  expect: {status_by_backend: {go: 200, kotlin: 200}}\n",
			want: "same status for both backends",
		},
		{
			name: "status_by_backend with an unknown backend",
			yaml: "- name: x\n  permit: [p]\n  request: {method: GET, path: /health}\n  expect: {status_by_backend: {go: 404, kotlin: 400, postgres: 500}}\n",
			want: "unknown backend \"postgres\"",
		},
		{
			name: "same_as_for with an unknown backend",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {status: 200, same_as_for: {postgres: {method: GET, path: /nowhere}}}\n",
			want: "unknown backend \"postgres\"",
		},
		{
			// S-A: a same_as identical to the case's own request only ever
			// compares an answer to itself - always equal, asserting nothing.
			name: "same_as identical to request",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {status: 200, same_as: {method: GET, path: /health}}\n",
			want: "identical to request",
		},
		{
			name: "same_as_for identical to request",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {status: 200, same_as_for: {go: {method: GET, path: /health}}}\n",
			want: "identical to request",
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
		{
			name: "unknown profile",
			yaml: "- name: x\n  profile: local-off\n  request: {method: GET, path: /health}\n  expect: {status: 200}\n",
			want: "unknown profile",
		},
		{
			name: "same_as with no method",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {status: 404, same_as: {path: /nowhere}}\n",
			want: "same_as.method is required",
		},
		{
			name: "same_as path includes the api base",
			yaml: "- name: x\n  request: {method: GET, path: /health}\n  expect: {status: 404, same_as: {method: GET, path: api/nowhere}}\n",
			want: "same_as.path must start with /",
		},
		{
			name: "given step with neither request nor sql",
			yaml: "- name: x\n  given: [{status: 204}]\n  request: {method: GET, path: /me}\n  expect: {status: 200}\n",
			want: "none of request, sql or outage",
		},
		{
			name: "given step that is both request and sql",
			yaml: "- name: x\n  given: [{request: {method: GET, path: /me}, status: 200, sql: SELECT 1}]\n  request: {method: GET, path: /me}\n  expect: {status: 200}\n",
			want: "not several",
		},
		{
			// A setup login that silently failed would make every assertion
			// after it about the wrong state.
			name: "given request with no status",
			yaml: "- name: x\n  given: [{request: {method: GET, path: /me}}]\n  request: {method: GET, path: /me}\n  expect: {status: 200}\n",
			want: "given[0].status is required",
		},
		{
			name: "given request path includes the api base",
			yaml: "- name: x\n  given: [{request: {method: GET, path: api/me}, status: 200}]\n  request: {method: GET, path: /me}\n  expect: {status: 200}\n",
			want: "given[0].request.path must start with /",
		},
		{
			name: "status on an sql step",
			yaml: "- name: x\n  given: [{sql: SELECT 1, status: 200}]\n  request: {method: GET, path: /me}\n  expect: {status: 200}\n",
			want: "belongs to a request step",
		},
		{
			name: "outage before another step",
			yaml: "- name: x\n  given: [{outage: true}, {sql: SELECT 1}]\n  request: {method: GET, path: /me}\n  expect: {status: 200}\n",
			want: "must be the last given step",
		},
		{
			name: "cookie with neither value nor pattern",
			yaml: "- name: x\n  request: {method: GET, path: /me}\n  expect: {status: 200, cookies: {s: {path: /, max_age: 0, http_only: true, secure: false, same_site: Lax, expires: past}}}\n",
			want: "one of value or value_pattern is required",
		},
		{
			name: "cookie with both value and pattern",
			yaml: "- name: x\n  request: {method: GET, path: /me}\n  expect: {status: 200, cookies: {s: {value: a, value_pattern: a, path: /, max_age: 0, http_only: true, secure: false, same_site: Lax, expires: past}}}\n",
			want: "mutually exclusive",
		},
		{
			// Every attribute is pinned, or a case quietly stops asserting one.
			name: "cookie missing attributes",
			yaml: "- name: x\n  request: {method: GET, path: /me}\n  expect: {status: 200, cookies: {s: {value: a, expires: past}}}\n",
			want: "missing: path, max_age, http_only, secure, same_site",
		},
		{
			name: "cookie with an unknown expires relation",
			yaml: "- name: x\n  request: {method: GET, path: /me}\n  expect: {status: 200, cookies: {s: {value: a, path: /, max_age: 0, http_only: true, secure: false, same_site: Lax, expires: soon}}}\n",
			want: "expires must be",
		},
		{
			name: "cookie with a bad pattern",
			yaml: "- name: x\n  request: {method: GET, path: /me}\n  expect: {status: 200, cookies: {s: {value_pattern: \"[\", path: /, max_age: 0, http_only: true, secure: false, same_site: Lax, expires: past}}}\n",
			want: "value_pattern",
		},
		{
			name: "pad with no token in the body",
			yaml: "- name: x\n  request: {method: POST, path: /auth/logout, body: abc, pad: {with: a, count: 3}}\n  expect: {status: 204}\n",
			want: "exactly one {pad}",
		},
		{
			name: "pad with no count",
			yaml: "- name: x\n  request: {method: POST, path: /auth/logout, body: \"{pad}\", pad: {with: a}}\n  expect: {status: 204}\n",
			want: "positive `count`",
		},
		{
			name: "rows with no sql",
			yaml: "- name: x\n  request: {method: GET, path: /me}\n  expect: {status: 200, rows: [{rows: []}]}\n",
			want: "expect.rows[0].sql is required",
		},
		{
			// An omitted list must not read as "asserts no rows".
			name: "rows with no expected rows",
			yaml: "- name: x\n  request: {method: GET, path: /me}\n  expect: {status: 200, rows: [{sql: SELECT 1}]}\n",
			want: "write `rows: []`",
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
	permitted, err := conformance.LoadPermitted("permitted-differences.yaml")
	if err != nil {
		t.Fatalf("permitted-differences.yaml: %v", err)
	}
	if err := conformance.CheckPermits(cases, permitted); err != nil {
		t.Fatal(err)
	}
}

// S-B: SameAsRequest's own three cases, each of which a mutation that
// disabled the SameAsFor lookup (e.g. changing its `ok` check to always
// fail) would break - the third case is the one that would still pass
// against such a mutation on its own, which is why the first two matter.
func TestSameAsRequestPrefersSameAsForOverSameAs(t *testing.T) {
	sameAs := &conformance.Request{Method: "GET", Path: "/same-as"}
	sameAsForGo := &conformance.Request{Method: "GET", Path: "/same-as-for-go"}
	e := conformance.Expect{
		SameAs:    sameAs,
		SameAsFor: map[string]*conformance.Request{"go": sameAsForGo},
	}

	if got := e.SameAsRequest("go"); got != sameAsForGo {
		t.Errorf("SameAsRequest(go) = %v, want the same_as_for entry (%v), not same_as (%v)", got, sameAsForGo, sameAs)
	}
	if got := e.SameAsRequest("kotlin"); got != sameAs {
		t.Errorf("SameAsRequest(kotlin) = %v, want the same_as fallback (%v), since same_as_for has no kotlin entry", got, sameAs)
	}

	empty := conformance.Expect{}
	if got := empty.SameAsRequest("go"); got != nil {
		t.Errorf("SameAsRequest(go) on an Expect with neither set = %v, want nil", got)
	}
}

func TestSentBodyExpandsThePad(t *testing.T) {
	r := conformance.Request{Body: `{"p":"{pad}"}`, Pad: &conformance.Pad{With: "ab", Count: 3}}
	if got := r.SentBody(); got != `{"p":"ababab"}` {
		t.Fatalf("SentBody() = %q", got)
	}
	if got := (conformance.Request{Body: "{pad}"}).SentBody(); got != "{pad}" {
		t.Fatalf("with no pad, SentBody() = %q, want the body verbatim", got)
	}
}
