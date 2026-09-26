package conformance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kerti/afloat/conformance"
)

func writePermitted(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "permitted.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The allowlist is the one file where a careless edit turns a real divergence
// green, so every rule that keeps an entry honest is worth a test.
func TestLoadPermittedRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want string
	}{
		{"no header", "- why: because\n", "none of `header`, `name` or `column`"},
		{"no why", "- header: Date\n", "has no `why`"},
		{"provisional with no issue", "- header: X-Request-Id\n  why: pending\n  provisional: true\n", "cites no issue"},
		{"duplicate", "- header: Date\n  why: a\n- header: date\n  why: b\n", "duplicate entry"},
		{"global and scoped at once", "- header: Date\n  name: x\n  why: a\n", "not several"},
		{"global with a body", "- header: Date\n  body: true\n  why: a\n", "needs a `name`"},
		{"scoped with no issue", "- name: x\n  body: true\n  why: a\n", "case-scoped but cites no issue"},
		{"scoped permitting nothing", "- name: x\n  issue: \"24\"\n  why: a\n", "permits nothing"},
		{"column not written table.column", "- column: sessions\n  why: a\n", "table.column"},
		{"column with a body", "- column: sessions.id\n  body: true\n  why: a\n", "needs a `name`"},
		{"column and header at once", "- column: sessions.id\n  header: Date\n  why: a\n", "not several"},
		{"duplicate column", "- column: sessions.id\n  why: a\n- column: sessions.id\n  why: b\n", "duplicate entry for column"},
		{"duplicate name", "- name: x\n  issue: \"24\"\n  body: true\n  why: a\n- name: x\n  issue: \"24\"\n  body: true\n  why: b\n", "duplicate entry named"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := conformance.LoadPermitted(writePermitted(t, tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPermittedMatchingIsCaseInsensitive(t *testing.T) {
	set, err := conformance.LoadPermitted(writePermitted(t, "- header: X-Request-Id\n  why: a\n  issue: \"26\"\n  provisional: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"X-Request-Id", "x-request-id", "X-REQUEST-ID"} {
		if !set.Allows(name) {
			t.Errorf("Allows(%q) = false, want true", name)
		}
	}
	if set.Allows("Date") {
		t.Error("Allows(\"Date\") = true, want false")
	}
	if got := set.Provisional(); len(got) != 1 {
		t.Errorf("Provisional() returned %d entries, want 1", len(got))
	}
}

const scopedEntry = "- name: unmatched-404\n  issue: \"24\"\n  headers: [Content-Type]\n  body: true\n  why: a\n"

// A case-scoped entry widens only the cases that cite it. Leaking to every
// case would make it a global exemption with extra steps.
func TestScopedPermitAppliesOnlyToCitingCases(t *testing.T) {
	set, err := conformance.LoadPermitted(writePermitted(t, scopedEntry))
	if err != nil {
		t.Fatal(err)
	}

	header, body, status := set.ForCase(conformance.Case{Permit: []string{"unmatched-404"}})
	if !header("content-type") || !body || status {
		t.Errorf("citing case: header=%v body=%v status=%v, want header and body true, status false", header("content-type"), body, status)
	}

	header, body, status = set.ForCase(conformance.Case{})
	if header("Content-Type") || body || status {
		t.Errorf("non-citing case: header=%v body=%v status=%v, want all false", header("Content-Type"), body, status)
	}
	if set.Allows("Content-Type") {
		t.Error("a scoped header leaked into the global Allows")
	}
}

// #64: a status-only case-scoped entry is the same shape, so it gets the
// same isolation guarantee headers and body already have.
func TestScopedPermitStatusAppliesOnlyToCitingCases(t *testing.T) {
	const entry = "- name: malformed-path-connector-400\n  issue: \"64\"\n  status: true\n  why: a\n"
	set, err := conformance.LoadPermitted(writePermitted(t, entry))
	if err != nil {
		t.Fatal(err)
	}

	_, _, status := set.ForCase(conformance.Case{Permit: []string{"malformed-path-connector-400"}})
	if !status {
		t.Error("citing case: status = false, want true")
	}
	_, _, status = set.ForCase(conformance.Case{})
	if status {
		t.Error("non-citing case: status = true, want false")
	}
}

func TestCheckPermits(t *testing.T) {
	set, err := conformance.LoadPermitted(writePermitted(t, scopedEntry+
		"- name: status-diff\n  issue: \"64\"\n  status: true\n  why: a\n"+
		"- name: status-and-body-diff\n  issue: \"64\"\n  status: true\n  body: true\n  why: a\n"))
	if err != nil {
		t.Fatal(err)
	}
	sameAs := &conformance.Request{Method: "GET", Path: "/nowhere"}
	withSameAs := conformance.Case{Name: "a", Permit: []string{"unmatched-404"}}
	withSameAs.Expect.SameAs = sameAs

	withStatusByBackend := conformance.Case{Name: "s", Permit: []string{"status-diff"}}
	withStatusByBackend.Expect.StatusByBackend = map[string]int{"go": 404, "kotlin": 400}

	statusPermittedButUnpinned := conformance.Case{Name: "s2", Permit: []string{"status-diff"}}
	statusPermittedButUnpinned.Expect.Status = 404

	statusPinnedButUnpermitted := conformance.Case{Name: "s3", Permit: []string{"unmatched-404"}}
	statusPinnedButUnpermitted.Expect.SameAs = sameAs
	statusPinnedButUnpermitted.Expect.StatusByBackend = map[string]int{"go": 404, "kotlin": 400}

	// The strict rule: status_by_backend alone does NOT excuse a
	// permitted body difference from being pinned some other way.
	statusByBackendAloneIsNotEnough := conformance.Case{Name: "s4", Permit: []string{"status-and-body-diff"}}
	statusByBackendAloneIsNotEnough.Expect.StatusByBackend = map[string]int{"go": 404, "kotlin": 400}

	// #64's own shape: same_as_for pins Go's side (its malformed-path answer
	// IS its own unregistered-path 404), and that plus the status already
	// pinned is enough - no byte-exact pin of Kotlin's framework-rendered
	// page demanded on top, which would be the maintenance burden the ruling
	// rejected a Tomcat valve for.
	statusAndBodyPinnedViaSameAsFor := conformance.Case{Name: "s5", Permit: []string{"status-and-body-diff"}}
	statusAndBodyPinnedViaSameAsFor.Expect.StatusByBackend = map[string]int{"go": 404, "kotlin": 400}
	statusAndBodyPinnedViaSameAsFor.Expect.SameAsFor = map[string]*conformance.Request{"go": sameAs}

	// S-A: same_as_for as the ONLY body pin must carry the go key - Kotlin's
	// side is the framework-rendered one nobody wants pinned byte-for-byte.
	kotlinOnlySameAsFor := &conformance.Request{Method: "GET", Path: "/kotlin-side"}
	sameAsForKotlinOnly := conformance.Case{Name: "s6", Permit: []string{"status-and-body-diff"}}
	sameAsForKotlinOnly.Expect.StatusByBackend = map[string]int{"go": 404, "kotlin": 400}
	sameAsForKotlinOnly.Expect.SameAsFor = map[string]*conformance.Request{"kotlin": kotlinOnlySameAsFor}

	for _, tc := range []struct {
		name  string
		cases []conformance.Case
		want  string
	}{
		{"cites an undefined entry", []conformance.Case{withSameAs, {Name: "b", Permit: []string{"nope"}}}, "does not define"},
		{"body permitted, body unpinned", []conformance.Case{{Name: "a", Permit: []string{"unmatched-404"}}}, "asserts nothing about the body"},
		{"entry cited by nothing", []conformance.Case{{Name: "a"}}, "cited by no case"},
		{"status permitted, status unpinned", []conformance.Case{withStatusByBackend, statusPermittedButUnpinned}, "expect.status pins one value"},
		{"status pinned, status unpermitted", []conformance.Case{withStatusByBackend, statusPinnedButUnpermitted}, "permits no case-scoped entry with `status: true`"},
		{
			"status_by_backend alone does not excuse a permitted body difference",
			[]conformance.Case{withSameAs, withStatusByBackend, statusByBackendAloneIsNotEnough},
			"asserts nothing about the body",
		},
		{
			"same_as_for pins the body status_by_backend alone could not, and every entry is cited",
			[]conformance.Case{withSameAs, withStatusByBackend, statusAndBodyPinnedViaSameAsFor},
			"",
		},
		{
			"same_as_for as the only body pin with no go key is rejected (S-A)",
			[]conformance.Case{withSameAs, withStatusByBackend, sameAsForKotlinOnly},
			"same_as_for has no \"go\" key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := conformance.CheckPermits(tc.cases, set)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

func TestMasksColumnIsExact(t *testing.T) {
	set, err := conformance.LoadPermitted(writePermitted(t, "- column: sessions.id\n  why: a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !set.MasksColumn("sessions", "id") {
		t.Error("MasksColumn(sessions, id) = false, want true")
	}
	for _, tc := range [][2]string{{"users", "id"}, {"sessions", "user_id"}} {
		if set.MasksColumn(tc[0], tc[1]) {
			t.Errorf("MasksColumn(%s, %s) = true, want false", tc[0], tc[1])
		}
	}
}
