package conformance_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kerti/afloat/conformance"
)

// server is one running backend under test, as a black box: the harness knows
// a base URL and the database it writes to, and nothing else, which is the
// whole point.
type server struct {
	profile string
	baseURL string
	db      *conformance.DB
}

// database is where the backend's database is. Every profile's server shares
// it: scripts/conformance.sh boots them all against the one database.
var database = struct{ envKey, fallback string }{
	"AFLOAT_DATABASE_URL", "postgres://afloat:afloat@localhost:5184/afloat",
}

// servers is where each profile's server answers by default, and the
// variable that moves it. scripts/conformance.sh boots one per profile.
var servers = map[string]struct{ envKey, fallback string }{
	conformance.ProfileDefault:       {"AFLOAT_BASE_URL", "http://localhost:5182/api"},
	conformance.ProfileLocalDisabled: {"AFLOAT_LOCAL_DISABLED_BASE_URL", "http://localhost:5186/api"},
}

// perResponseHeaders carry a value minted per response, so two answers from
// the same server differ on them by construction. same_as skips them.
var perResponseHeaders = []string{"Date", "X-Request-Id"}

const (
	casesDir      = "cases"
	seedFile      = "fixtures/seed.sql"
	readinessPath = "/health"

	// Short when the run is optional, because the common case is a developer
	// running `go test ./...` with no backend up and wanting the skip
	// immediately. Long when it is required, because then something is
	// supposed to be starting and the wait is the point.
	readinessLimitOptional = 2 * time.Second

	// Longer than any pool's wait for a connection, so an outage case sees
	// the backend's answer rather than the harness giving up first.
	requestTimeout         = 45 * time.Second
	readinessLimitRequired = 30 * time.Second
)

// response is everything observable about one answer.
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

// TestConformance is the whole harness. One test function, a subtest per
// case, so a failure names the case.
func TestConformance(t *testing.T) {
	// Same shape as AFLOAT_REQUIRE_TEST_DB in the backend's suite: a machine
	// that has the backend running must never report green on a run that never
	// happened, and a machine that does not must not fail for it.
	required := os.Getenv("AFLOAT_REQUIRE_CONFORMANCE") == "1"
	// `go test -short` runs the case-file tests and nothing that needs a
	// server. That is what `make check` uses: the committed files are validated
	// by the pre-push gate without the gate ever depending on a backend being
	// up, or quietly running the whole suite when one happens to be. AFLOAT_REQUIRE_CONFORMANCE wins, so a required run cannot be
	// short-circuited by a stray flag.
	if testing.Short() && !required {
		t.Skip("skipping: -short runs no case that needs a running backend")
	}

	cases, err := conformance.Load(casesDir)
	if err != nil {
		t.Fatalf("loading cases: %v", err)
	}

	// Only the profiles some case uses have to be up: a server nothing runs
	// against is not a reason to skip, or to fail.
	limit := readinessLimitOptional
	if required {
		limit = readinessLimitRequired
	}
	ctx := context.Background()
	db, err := conformance.OpenDB(ctx, env(database.envKey, database.fallback))
	if err != nil {
		if required {
			t.Fatalf("AFLOAT_REQUIRE_CONFORMANCE=1 but the database is not reachable at %s: %v", database.envKey, err)
		}
		t.Skipf("skipping: the database is not reachable (%v). "+
			"Run `make conformance`, or set AFLOAT_REQUIRE_CONFORMANCE=1 to make this a failure.", err)
	}
	t.Cleanup(func() { db.Close(ctx) })

	serverFor := map[string]server{}
	for _, c := range cases {
		profile := c.ProfileOf()
		if _, done := serverFor[profile]; done {
			continue
		}
		s := server{profile: profile, baseURL: env(servers[profile].envKey, servers[profile].fallback), db: db}
		if err := waitReady(s, limit); err != nil {
			if required {
				t.Fatalf("AFLOAT_REQUIRE_CONFORMANCE=1 but the %s server is not answering at %s: %v",
					profile, s.baseURL, err)
			}
			t.Skipf("skipping: the %s server is not answering at %s (%v). "+
				"Run `make conformance`, or set AFLOAT_REQUIRE_CONFORMANCE=1 to make this a failure.",
				profile, s.baseURL, err)
		}
		serverFor[profile] = s
	}
	t.Logf("%d case(s) from %s, across %d profile(s)", len(cases), casesDir, len(serverFor))

	for _, c := range cases {
		s := serverFor[c.ProfileOf()]
		t.Run(c.Name, func(t *testing.T) {
			r, err := runCase(ctx, s, c)
			if err != nil {
				t.Fatal(err)
			}
			assertExpected(t, c, r.resp)
			if r.ref != nil {
				assertSameAs(t, c, r.resp, *r.ref)
			}
			assertRows(t, c, r.rows)
		})
	}
}

// run is everything the server did for one case.
type run struct {
	resp response
	ref  *response // the same_as answer, when the case has one
	rows [][]map[string]any
}

// runCase drives the server through one case from a freshly seeded database:
// the given steps, the request and the rows, and only then the same_as
// request, whose own writes are nobody's business. An error here is a setup
// failure - a given step that did not land - and names the step.
func runCase(ctx context.Context, b server, c conformance.Case) (run, error) {
	if err := b.db.Reset(ctx, seedFile); err != nil {
		return run{}, fmt.Errorf("resetting the database: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return run{}, err
	}
	// Redirects are an observable answer, not something to resolve on the
	// client's behalf: a 302 where the case expects a 200 is a finding.
	client := &http.Client{
		Jar:           jar,
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for i, g := range c.Given {
		if g.Outage {
			if err := b.db.Outage(ctx); err != nil {
				return run{}, fmt.Errorf("given[%d] outage: %w", i, err)
			}
			// Restored before the rows are read, and on every early return.
			defer func() { _ = b.db.Restore(ctx) }()
			continue
		}
		if g.SQL != "" {
			if err := b.db.Exec(ctx, g.SQL); err != nil {
				return run{}, fmt.Errorf("given[%d] sql: %w", i, err)
			}
			continue
		}
		resp, err := do(client, b, *g.Request)
		if err != nil {
			return run{}, fmt.Errorf("given[%d] %s %s: %w", i, g.Request.Method, g.Request.Path, err)
		}
		if resp.status != g.Status {
			return run{}, fmt.Errorf("given[%d] %s %s: status %d, want %d; body %s",
				i, g.Request.Method, g.Request.Path, resp.status, g.Status, truncate(resp.body))
		}
	}

	var r run
	if r.resp, err = do(client, b, c.Request); err != nil {
		return run{}, err
	}
	if err := b.db.Restore(ctx); err != nil {
		return run{}, fmt.Errorf("ending the outage: %w", err)
	}
	if slices.ContainsFunc(c.Given, func(g conformance.Step) bool { return g.Outage }) {
		// A pool rebuilds its connections in its own time. The next case
		// must not start while the server is still recovering, or it would
		// fail for a reason that belongs to this one.
		if err := waitHealthy(b, readinessLimitRequired); err != nil {
			return run{}, fmt.Errorf("not healthy after the outage ended: %w", err)
		}
	}
	for i, q := range c.Expect.Rows {
		got, err := b.db.Query(ctx, q.SQL)
		if err != nil {
			return run{}, fmt.Errorf("expect.rows[%d]: %w", i, err)
		}
		r.rows = append(r.rows, got)
	}
	if sameAs := c.Expect.SameAs; sameAs != nil {
		ref, err := do(client, b, *sameAs)
		if err != nil {
			return run{}, fmt.Errorf("same_as: %w", err)
		}
		r.ref = &ref
	}
	return r, nil
}

func waitReady(b server, limit time.Duration) error {
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

// waitHealthy waits for /health to answer 200, not merely to answer.
func waitHealthy(b server, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	status := 0
	for time.Now().Before(deadline) {
		resp, err := http.Get(b.baseURL + readinessPath)
		if err == nil {
			status = resp.StatusCode
			_ = resp.Body.Close()
			if status == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("/health still %d after %s", status, limit)
}

func do(client *http.Client, b server, r conformance.Request) (response, error) {
	var body io.Reader
	if sent := r.SentBody(); sent != "" {
		body = strings.NewReader(sent)
	}
	var req *http.Request
	var err error
	switch {
	case strings.HasPrefix(r.RawTarget, "*") || strings.Contains(r.RawTarget, "://"):
		// The asterisk-form (RFC 9110 §7.1's "*", or a ruling #70 shape such
		// as "*?x=1") and the absolute-form (a full URI, e.g. "http://x*"):
		// neither is under the API base, so RawTarget is sent exactly as
		// given, with no base path prefixed.
		if req, err = http.NewRequest(r.Method, b.baseURL, body); err != nil {
			return response{}, err
		}
		req.URL.Opaque = r.RawTarget
	case r.RawTarget != "":
		// URL.Opaque is sent as the request line's target verbatim, with no
		// escaping and no re-parse: what RawTarget exists for (#64) - a byte
		// url.Parse would otherwise normalise, such as %5C's un-encoded form,
		// which a Path-based request can never express because
		// http.NewRequest necessarily builds and re-serialises a full URL.
		base, perr := url.Parse(b.baseURL)
		if perr != nil {
			return response{}, perr
		}
		if req, err = http.NewRequest(r.Method, b.baseURL, body); err != nil {
			return response{}, err
		}
		req.URL.Opaque = base.Path + r.RawTarget
	default:
		if req, err = http.NewRequest(r.Method, b.baseURL+r.Path, body); err != nil {
			return response{}, err
		}
	}
	for k, v := range r.Headers {
		// Host is the request's, not a header's, in net/http. Setting it
		// lets a case that compares Origin with Host mean the same thing on
		// whichever port the server listens on.
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	for k, hexValue := range r.HeadersHex {
		// Sent as the exact decoded bytes, bypassing Header.Set's string
		// handling: a YAML string is Unicode text and cannot hold an invalid
		// UTF-8 byte directly (#70), so cases that need one spell it as hex
		// instead. This client's own check still applies even writing the map
		// directly - verified: it refuses NUL, 0x01, 0x1B, 0x7F and a bare CR
		// or LF, and allows everything else, HTAB (0x09) and any C1/UTF-8 byte
		// (0x80-0xFF) included - so a case naming one of the refused bytes can
		// only be exercised with a raw socket, not through this harness.
		decoded, herr := hex.DecodeString(hexValue)
		if herr != nil {
			return response{}, fmt.Errorf("headers_hex[%s]: %w", k, herr)
		}
		req.Header[http.CanonicalHeaderKey(k)] = []string{string(decoded)}
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

	if want := c.Expect.Status; got.status != want {
		t.Errorf("status: want %d, got %d\nbody: %s", want, got.status, truncate(got.body))
	}

	for name, want := range c.Expect.Headers {
		if have := got.headers.Get(name); have != want {
			t.Errorf("header %s: want %q, got %q", name, want, have)
		}
	}

	if c.Expect.Cookies != nil {
		assertCookies(t, c, got)
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

// assertCookies holds every Set-Cookie to the case: each named cookie set
// exactly once and matching attribute by attribute, and nothing else set.
func assertCookies(t *testing.T, c conformance.Case, got response) {
	t.Helper()
	cookies, err := conformance.ParseSetCookies(got.headers)
	if err != nil {
		t.Error(err)
		return
	}
	date, _ := http.ParseTime(got.headers.Get("Date"))
	seen := map[string]int{}
	for _, ck := range cookies {
		seen[ck.Name]++
		want, ok := c.Expect.Cookies[ck.Name]
		if !ok {
			t.Errorf("cookie %s is set and the case does not expect it: %s", ck.Name, ck.Raw)
			continue
		}
		for _, bad := range conformance.CheckCookie(ck, want, date) {
			t.Errorf("cookie %s: %s\n  Set-Cookie: %s", ck.Name, bad, ck.Raw)
		}
	}
	for name := range c.Expect.Cookies {
		if n := seen[name]; n != 1 {
			t.Errorf("cookie %s: set %d time(s), want exactly once", name, n)
		}
	}
}

// assertRows holds each expect.rows query's result to the rows the case
// lists, in order.
func assertRows(t *testing.T, c conformance.Case, got [][]map[string]any) {
	t.Helper()
	for i, q := range c.Expect.Rows {
		want := normalise(t, q.Rows)
		have := normalise(t, got[i])
		if !reflect.DeepEqual(have, want) {
			t.Errorf("rows[%d] %s\n  want: %v\n  got:  %v", i, strings.TrimSpace(q.SQL), want, have)
		}
	}
}

// assertSameAs holds the answer to the server's own answer for the same_as
// request: status, every header but the per-response ones, and the body bytes.
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
