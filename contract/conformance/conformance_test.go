package conformance_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
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

const (
	defaultGoBaseURL     = "http://localhost:5182/api"
	defaultKotlinBaseURL = "http://localhost:5183/api"

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
	backends := []backend{
		{name: "go", baseURL: env("AFLOAT_GO_BASE_URL", defaultGoBaseURL)},
		{name: "kotlin", baseURL: env("AFLOAT_KOTLIN_BASE_URL", defaultKotlinBaseURL)},
	}

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

	limit := readinessLimitOptional
	if required {
		limit = readinessLimitRequired
	}
	for _, b := range backends {
		if err := waitReady(b, limit); err != nil {
			if required {
				t.Fatalf("AFLOAT_REQUIRE_CONFORMANCE=1 but the %s backend is not answering at %s: %v",
					b.name, b.baseURL, err)
			}
			t.Skipf("skipping: the %s backend is not answering at %s (%v). "+
				"Run `make conformance`, or set AFLOAT_REQUIRE_CONFORMANCE=1 to make this a failure.",
				b.name, b.baseURL, err)
		}
	}

	permitted, err := conformance.LoadPermitted(permittedFile)
	if err != nil {
		t.Fatalf("loading %s: %v", permittedFile, err)
	}
	if prov := permitted.Provisional(); len(prov) > 0 {
		names := make([]string, 0, len(prov))
		for _, p := range prov {
			names = append(names, fmt.Sprintf("%s (%s)", p.Header, p.Issue))
		}
		sort.Strings(names)
		t.Logf("%d permitted difference(s) are PROVISIONAL, pending a decision: %s",
			len(prov), strings.Join(names, ", "))
	}

	cases, err := conformance.Load(casesDir)
	if err != nil {
		t.Fatalf("loading cases: %v", err)
	}
	t.Logf("%d case(s) from %s, against %d backends", len(cases), casesDir, len(backends))

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			answers := make(map[string]response, len(backends))

			// Failure mode 1: a backend disagrees with the expected answer.
			// That backend is wrong, and the case names which one.
			for _, b := range backends {
				b := b
				resp, err := do(b, c)
				if err != nil {
					t.Fatalf("%s: %v", b.name, err)
				}
				answers[b.name] = resp
				t.Run("expected/"+b.name, func(t *testing.T) {
					assertExpected(t, c, resp)
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

func do(b backend, c conformance.Case) (response, error) {
	var body io.Reader
	if c.Request.Body != "" {
		body = strings.NewReader(c.Request.Body)
	}
	req, err := http.NewRequest(c.Request.Method, b.baseURL+c.Request.Path, body)
	if err != nil {
		return response{}, err
	}
	for k, v := range c.Request.Headers {
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

	for _, n := range sorted {
		if permitted.Allows(n) {
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

	if !equalBodies(goResp.body, ktResp.body) {
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
