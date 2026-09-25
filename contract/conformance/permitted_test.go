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

	header, body := set.ForCase(conformance.Case{Permit: []string{"unmatched-404"}})
	if !header("content-type") || !body {
		t.Errorf("citing case: header=%v body=%v, want both true", header("content-type"), body)
	}

	header, body = set.ForCase(conformance.Case{})
	if header("Content-Type") || body {
		t.Errorf("non-citing case: header=%v body=%v, want both false", header("Content-Type"), body)
	}
	if set.Allows("Content-Type") {
		t.Error("a scoped header leaked into the global Allows")
	}
}

func TestCheckPermits(t *testing.T) {
	set, err := conformance.LoadPermitted(writePermitted(t, scopedEntry))
	if err != nil {
		t.Fatal(err)
	}
	sameAs := &conformance.Request{Method: "GET", Path: "/nowhere"}
	withSameAs := conformance.Case{Name: "a", Permit: []string{"unmatched-404"}}
	withSameAs.Expect.SameAs = sameAs

	for _, tc := range []struct {
		name  string
		cases []conformance.Case
		want  string
	}{
		{"cites an undefined entry", []conformance.Case{withSameAs, {Name: "b", Permit: []string{"nope"}}}, "does not define"},
		{"body permitted, body unpinned", []conformance.Case{{Name: "a", Permit: []string{"unmatched-404"}}}, "asserts nothing about the body"},
		{"entry cited by nothing", []conformance.Case{{Name: "a"}}, "cited by no case"},
		{"valid", []conformance.Case{withSameAs}, ""},
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
