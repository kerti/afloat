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
		{"no header", "- why: because\n", "no header"},
		{"no why", "- header: Date\n", "has no `why`"},
		{"provisional with no issue", "- header: X-Request-Id\n  why: pending\n  provisional: true\n", "cites no issue"},
		{"duplicate", "- header: Date\n  why: a\n- header: date\n  why: b\n", "duplicate entry"},
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
