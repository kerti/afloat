package conformance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
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
// harness knows a base URL and the database the backend writes to, and
// nothing else, which is the whole point.
type backend struct {
	name    string
	baseURL string
	db      *conformance.DB
}

// databases is where each backend's own database is. Keyed by backend, not by
// profile: a second profile's pair shares its default twin's database.
var databases = map[string]struct{ envKey, fallback string }{
	"go":     {"AFLOAT_GO_DATABASE_URL", "postgres://afloat:afloat@localhost:5184/afloat_go"},
	"kotlin": {"AFLOAT_KOTLIN_DATABASE_URL", "postgres://afloat:afloat@localhost:5184/afloat_kotlin"},
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
	seedFile      = "fixtures/seed.sql"
	readinessPath = "/health"

	// Short when the run is optional, because the common case is a developer
	// running `go test ./...` with no backends up and wanting the skip
	// immediately. Long when it is required, because then something is
	// supposed to be starting and the wait is the point.
	readinessLimitOptional = 2 * time.Second

	// Longer than any pool's wait for a connection, so an outage case sees
	// the backend's answer rather than the harness giving up first.
	requestTimeout         = 45 * time.Second
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
	ctx := context.Background()
	dbs := map[string]*conformance.DB{}
	for name, d := range databases {
		url := env(d.envKey, d.fallback)
		db, err := conformance.OpenDB(ctx, url)
		if err != nil {
			if required {
				t.Fatalf("AFLOAT_REQUIRE_CONFORMANCE=1 but the %s database is not reachable at %s: %v", name, d.envKey, err)
			}
			t.Skipf("skipping: the %s database is not reachable (%v). "+
				"Run `make conformance`, or set AFLOAT_REQUIRE_CONFORMANCE=1 to make this a failure.", name, err)
		}
		t.Cleanup(func() { db.Close(ctx) })
		dbs[name] = db
	}

	backendsFor := map[string][]backend{}
	for _, c := range cases {
		profile := c.ProfileOf()
		if _, done := backendsFor[profile]; done {
			continue
		}
		var bs []backend
		for _, p := range pairs[profile] {
			bs = append(bs, backend{name: p.name, baseURL: env(p.envKey, p.fallback), db: dbs[p.name]})
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
			runs := make(map[string]run, len(backends))

			// Failure mode 1: a backend disagrees with the expected answer.
			// That backend is wrong, and the case names which one.
			for _, b := range backends {
				r, err := runCase(ctx, b, c, permitted)
				if err != nil {
					t.Fatalf("%s: %v", b.name, err)
				}
				runs[b.name] = r
				t.Run("expected/"+b.name, func(t *testing.T) {
					assertExpected(t, c, r.resp, b.name)
					if r.ref != nil {
						assertSameAs(t, c, r.resp, *r.ref)
					}
					assertRows(t, c, r.rows)
				})
			}

			// Failure mode 2: the two agree with the case and still differ on
			// something no case asserts - in the answer, or in the rows they
			// left behind. That is not a pass: it means the case file is
			// incomplete, which is the finding this harness exists for.
			t.Run("parity", func(t *testing.T) {
				assertParity(t, c, permitted, runs["go"].resp, runs["kotlin"].resp)
				for _, d := range conformance.SnapshotParity(runs["go"].state, runs["kotlin"].state) {
					t.Errorf("persisted state differs between backends: %s", d)
				}
			})
		})
	}
}

// run is everything one backend did for one case.
type run struct {
	resp  response
	ref   *response // the same_as answer, when the case has one
	rows  [][]map[string]any
	state conformance.Snapshot
}

// runCase drives one backend through one case from a freshly seeded database:
// the given steps, the request, the rows and the snapshot, and only then the
// same_as request, whose own writes are nobody's business. An error here is a
// setup failure - a given step that did not land - and names the step.
func runCase(ctx context.Context, b backend, c conformance.Case, permitted *conformance.PermittedSet) (run, error) {
	if err := b.db.Reset(ctx, seedFile); err != nil {
		return run{}, fmt.Errorf("resetting the database: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return run{}, err
	}
	// Redirects are an observable answer, not something to resolve on the
	// client's behalf: a backend that 302s where the other 200s is a finding.
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
		// must not start while this backend is still recovering, or it
		// would fail for a reason that belongs to this one.
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
	if r.state, err = b.db.Snapshot(ctx, permitted.MasksColumn); err != nil {
		return run{}, fmt.Errorf("snapshot: %w", err)
	}
	if c.Expect.SameAs != nil {
		ref, err := do(client, b, *c.Expect.SameAs)
		if err != nil {
			return run{}, fmt.Errorf("same_as: %w", err)
		}
		r.ref = &ref
	}
	return r, nil
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

// waitHealthy waits for /health to answer 200, not merely to answer.
func waitHealthy(b backend, limit time.Duration) error {
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

func do(client *http.Client, b backend, r conformance.Request) (response, error) {
	var body io.Reader
	if sent := r.SentBody(); sent != "" {
		body = strings.NewReader(sent)
	}
	var req *http.Request
	var err error
	if r.RawTarget != "" {
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
	} else {
		if req, err = http.NewRequest(r.Method, b.baseURL+r.Path, body); err != nil {
			return response{}, err
		}
	}
	for k, v := range r.Headers {
		// Host is the request's, not a header's, in net/http. Setting it
		// lets a case that compares Origin with Host mean the same thing on
		// two backends listening on two ports.
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
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

func assertExpected(t *testing.T, c conformance.Case, got response, backend string) {
	t.Helper()

	if want := c.Expect.StatusFor(backend); got.status != want {
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

	allowsHeader, allowsBody, allowsStatus := permitted.ForCase(c)

	if !allowsStatus && goResp.status != ktResp.status {
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
		if allowsHeader(n) {
			continue
		}
		if strings.EqualFold(n, "Set-Cookie") {
			continue // compared attribute by attribute below
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

	if !allowsHeader("Set-Cookie") {
		goCookies, gErr := conformance.ParseSetCookies(goResp.headers)
		ktCookies, kErr := conformance.ParseSetCookies(ktResp.headers)
		if gErr != nil || kErr != nil {
			t.Errorf("Set-Cookie: go %v, kotlin %v", gErr, kErr)
		} else {
			goDate, _ := http.ParseTime(goResp.headers.Get("Date"))
			ktDate, _ := http.ParseTime(ktResp.headers.Get("Date"))
			for _, d := range conformance.CookieParity(goCookies, ktCookies, goDate, ktDate) {
				t.Errorf("Set-Cookie differs: %s\n  go:     %q\n  kotlin: %q", d,
					goResp.headers.Values("Set-Cookie"), ktResp.headers.Values("Set-Cookie"))
			}
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
