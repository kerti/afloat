package conformance_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kerti/afloat/conformance"
)

// backend is one running server under test. Both are black boxes here: the
// harness knows a base URL and nothing else, which is the whole point.
type backend struct {
	name    string
	baseURL string
}

// pairs is where each profile's two backends answer by default, and the
// variables that move them. scripts/conformance.sh boots one pair per profile.
var pairs = map[string][2]struct{ name, envKey, fallback string }{
	conformance.ProfileDefault: {
		{"go", "AFLOAT_GO_BASE_URL", "http://localhost:5182/api"},
		{"kotlin", "AFLOAT_KOTLIN_BASE_URL", "http://localhost:5183/api"},
	},
	conformance.ProfileLocalDisabled: {
		{"go", "AFLOAT_GO_LOCAL_DISABLED_BASE_URL", "http://localhost:5186/api"},
		{"kotlin", "AFLOAT_KOTLIN_LOCAL_DISABLED_BASE_URL", "http://localhost:5187/api"},
	},
}

// perResponseHeaders carry a value minted per response, so two answers from
// the SAME backend differ on them by construction. Only same_as skips them;
// parity between backends is the permitted list's business.
var perResponseHeaders = []string{"Date", "X-Request-Id"}

const (
	casesDir      = "cases"
	permittedFile = "permitted-differences.yaml"
	readinessPath = "/health"

	// Short when the run is optional, because the common case is a developer
	// running `go test ./...` with no backends up and wanting the skip
	// immediately. Long when it is required, because then something is
	// supposed to be starting and the wait is the point.
	readinessLimitOptional = 2 * time.Second
	readinessLimitRequired = 30 * time.Second
)

// response is everything observable about one answer. Captured whole, because
// failure mode 2 compares the parts no case asserts.
type response struct {
	status  int
	headers http.Header
	body    []byte
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TestConformance is the whole harness. One test function, subtests per case
// per mode, so a failure names the case and which kind of failure it is.
func TestConformance(t *testing.T) {
	// Same shape as AFLOAT_REQUIRE_TEST_DB in both backends' suites: a machine
	// that has the backends running must never report green on a run that never
	// happened, and a machine that does not must not fail for it.
	required := os.Getenv("AFLOAT_REQUIRE_CONFORMANCE") == "1"
	// `go test -short` runs the case-file and allowlist tests and nothing that
	// needs a server. That is what `make check` uses: the committed files are
	// validated by the pre-push gate without the gate ever depending on two
	// backends being up, or quietly running the whole suite when they happen to
	// be. AFLOAT_REQUIRE_CONFORMANCE wins, so a required run cannot be
	// short-circuited by a stray flag.
	if testing.Short() && !required {
		t.Skip("skipping: -short runs no case that needs a running backend")
	}

	permitted, err := conformance.LoadPermitted(permittedFile)
	if err != nil {
		t.Fatalf("loading %s: %v", permittedFile, err)
	}
	cases, err := conformance.Load(casesDir)
	if err != nil {
		t.Fatalf("loading cases: %v", err)
	}
	if err := conformance.CheckPermits(cases, permitted); err != nil {
		t.Fatal(err)
	}
	if prov := permitted.Provisional(); len(prov) > 0 {
		names := make([]string, 0, len(prov))
		for _, p := range prov {
			names = append(names, fmt.Sprintf("%s%s (%s)", p.Header, p.Name, p.Issue))
		}
		sort.Strings(names)
		t.Logf("%d permitted difference(s) are PROVISIONAL, pending a decision: %s",
			len(prov), strings.Join(names, ", "))
	}

	// Only the profiles some case uses have to be up: a pair nothing runs
	// against is not a reason to skip, or to fail.
	limit := readinessLimitOptional
	if required {
		limit = readinessLimitRequired
	}
	backendsFor := map[string][]backend{}
	for _, c := range cases {
		profile := c.ProfileOf()
		if _, done := backendsFor[profile]; done {
			continue
		}
		var bs []backend
		for _, p := range pairs[profile] {
			bs = append(bs, backend{name: p.name, baseURL: env(p.envKey, p.fallback)})
		}
		for _, b := range bs {
			if err := waitReady(b, limit); err != nil {
				if required {
					t.Fatalf("AFLOAT_REQUIRE_CONFORMANCE=1 but the %s backend (%s profile) is not answering at %s: %v",
						b.name, profile, b.baseURL, err)
				}
				t.Skipf("skipping: the %s backend (%s profile) is not answering at %s (%v). "+
					"Run `make conformance`, or set AFLOAT_REQUIRE_CONFORMANCE=1 to make this a failure.",
					b.name, profile, b.baseURL, err)
			}
		}
		backendsFor[profile] = bs
	}
	t.Logf("%d case(s) from %s, across %d profile(s)", len(cases), casesDir, len(backendsFor))

	for _, c := range cases {
		backends := backendsFor[c.ProfileOf()]
		t.Run(c.Name, func(t *testing.T) {
			answers := make(map[string]response, len(backends))

			// Failure mode 1: a backend disagrees with the expected answer.
			// That backend is wrong, and the case names which one.
			for _, b := range backends {
				b := b
				resp, err := do(b, c.Request)
				if err != nil {
					t.Fatalf("%s: %v", b.name, err)
				}
				answers[b.name] = resp
				var ref *response
				if c.Expect.SameAs != nil {
					r, err := do(b, *c.Expect.SameAs)
					if err != nil {
						t.Fatalf("%s same_as: %v", b.name, err)
					}
					ref = &r
				}
				t.Run("expected/"+b.name, func(t *testing.T) {
					assertExpected(t, c, resp)
					if ref != nil {
						assertSameAs(t, c, resp, *ref)
					}
				})
			}

			// Failure mode 2: the two agree with the case and still differ on
			// something no case asserts. That is not a pass - it means the case
			// file is incomplete, which is the finding this harness exists for.
			t.Run("parity", func(t *testing.T) {
				assertParity(t, c, permitted, answers["go"], answers["kotlin"])
			})
		})
	}
}

func waitReady(b backend, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	var last error
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, b.baseURL+readinessPath, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		last = err
		time.Sleep(100 * time.Millisecond)
	}
	return last
}

func do(b backend, r conformance.Request) (response, error) {
	var body io.Reader
	if r.Body != "" {
		body = strings.NewReader(r.Body)
	}
	req, err := http.NewRequest(r.Method, b.baseURL+r.Path, body)
	if err != nil {
		return response{}, err
	}
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}

	// Redirects are an observable answer, not something to resolve on the
	// client's behalf: a backend that 302s where the other 200s is a finding.
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return response{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return response{}, err
	}
	return response{status: resp.StatusCode, headers: resp.Header, body: raw}, nil
}

func assertExpected(t *testing.T, c conformance.Case, got response) {
	t.Helper()

	if got.status != c.Expect.Status {
		t.Errorf("status: want %d, got %d\nbody: %s", c.Expect.Status, got.status, truncate(got.body))
	}

	for name, want := range c.Expect.Headers {
		if have := got.headers.Get(name); have != want {
			t.Errorf("header %s: want %q, got %q", name, want, have)
		}
	}

	switch {
	case c.Expect.BodyEmpty:
		if len(got.body) != 0 {
			t.Errorf("body: want empty, got %s", truncate(got.body))
		}
	case c.Expect.BodyRaw != "":
		if string(got.body) != c.Expect.BodyRaw {
			t.Errorf("body: want %q, got %q", c.Expect.BodyRaw, string(got.body))
		}
	case c.Expect.BodyJSON != nil:
		var have any
		if err := json.Unmarshal(got.body, &have); err != nil {
			t.Errorf("body: not JSON (%v): %s", err, truncate(got.body))
			return
		}
		// Round-trip the expectation through JSON so YAML's int/float and
		// map[string]any typing match the decoder's, rather than failing on a
		// difference that is only in Go's type system.
		want := normalise(t, c.Expect.BodyJSON)
		if !reflect.DeepEqual(have, want) {
			t.Errorf("body: want %v, got %v", want, have)
		}
	}
}

// assertSameAs holds one backend's answer to its own answer for the same_as
// request: status, every header but the per-response ones, and the body bytes.
// It runs under expected/<backend>, because a mismatch means that backend is
// wrong, whatever the other one does.
func assertSameAs(t *testing.T, c conformance.Case, got, ref response) {
	t.Helper()
	s := c.Expect.SameAs

	if got.status != ref.status {
		t.Errorf("same_as %s %s: status %d, but that request answers %d", s.Method, s.Path, got.status, ref.status)
	}
	names := map[string]struct{}{}
	for n := range got.headers {
		names[n] = struct{}{}
	}
	for n := range ref.headers {
		names[n] = struct{}{}
	}
	for n := range names {
		if slices.ContainsFunc(perResponseHeaders, func(h string) bool { return strings.EqualFold(h, n) }) {
			continue
		}
		if g, r := got.headers.Values(n), ref.headers.Values(n); !reflect.DeepEqual(g, r) {
			t.Errorf("same_as %s %s: header %s is %q, but that request answers %q",
				s.Method, s.Path, n, strings.Join(g, ", "), strings.Join(r, ", "))
		}
	}
	if string(got.body) != string(ref.body) {
		t.Errorf("same_as %s %s: body %s, but that request answers %s",
			s.Method, s.Path, truncate(got.body), truncate(ref.body))
	}
}

// assertParity is failure mode 2. It compares the FULL answers and reports
// anything that differs and is neither asserted by the case nor on the
// permitted-difference list.
func assertParity(t *testing.T, c conformance.Case, permitted *conformance.PermittedSet, goResp, ktResp response) {
	t.Helper()

	if goResp.status != ktResp.status {
		t.Errorf("status differs between backends: go %d, kotlin %d", goResp.status, ktResp.status)
	}

	names := map[string]struct{}{}
	for n := range goResp.headers {
		names[n] = struct{}{}
	}
	for n := range ktResp.headers {
		names[n] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	allowsHeader, allowsBody := permitted.ForCase(c)
	for _, n := range sorted {
		if allowsHeader(n) {
			continue
		}
		g, k := goResp.headers.Values(n), ktResp.headers.Values(n)
		if reflect.DeepEqual(g, k) {
			continue
		}
		switch {
		case len(g) == 0:
			t.Errorf("header %s: kotlin sends %q, go sends nothing. "+
				"Either both should, or %s belongs on the permitted-difference list with the issue that decided it.",
				n, strings.Join(k, ", "), n)
		case len(k) == 0:
			t.Errorf("header %s: go sends %q, kotlin sends nothing. "+
				"Either both should, or %s belongs on the permitted-difference list with the issue that decided it.",
				n, strings.Join(g, ", "), n)
		default:
			t.Errorf("header %s differs: go %q, kotlin %q", n, strings.Join(g, ", "), strings.Join(k, ", "))
		}
	}

	if !allowsBody && !equalBodies(goResp.body, ktResp.body) {
		t.Errorf("body differs between backends\n  go:     %s\n  kotlin: %s",
			truncate(goResp.body), truncate(ktResp.body))
	}
}

// equalBodies compares bytes, falling back to a JSON comparison only when BOTH
// parse. Byte equality is the standard; key order is the one difference that is
// not worth hand-rolling a serialiser in either backend to remove, and a case
// that needs byte-exactness says so with expect.body_raw.
func equalBodies(a, b []byte) bool {
	if string(a) == string(b) {
		return true
	}
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

func normalise(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("expectation is not JSON-encodable: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("expectation did not round-trip: %v", err)
	}
	return out
}

func truncate(b []byte) string {
	const limit = 512
	if len(b) == 0 {
		return "(empty)"
	}
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + fmt.Sprintf("... (%d bytes)", len(b))
}
