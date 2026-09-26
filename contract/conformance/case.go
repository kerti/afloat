// Package conformance drives both Afloat backends over HTTP and asserts that
// they answer identically.
//
// It is a test-only package: the runner lives in conformance_test.go. The types
// here describe the case-file format, which is the vocabulary the whole harness
// is written in.
package conformance

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Case is one request and the answer both backends must give.
//
// Deliberately not generated from contract/openapi.yaml. The spec says what a
// response may look like; a case says what it IS, including the parts the spec
// cannot express - which status a specific bad body produces, what the error
// envelope's code is, what the cookie's attributes are. Generating these would
// re-derive the ambiguity the harness exists to remove.
type Case struct {
	// Name is what a failure is reported as. Required, and unique across files.
	Name string `yaml:"name"`

	// Issue optionally records the decision this case pins, so a case that
	// encodes a milestone ruling says which one.
	Issue string `yaml:"issue"`

	// Profile names the server configuration the case runs against; empty
	// means ProfileDefault. A case runs against its own profile only, so a
	// login case is never also run against a pair where login is off.
	Profile string `yaml:"profile"`

	// Permit cites case-scoped entries in permitted-differences.yaml by name.
	// A difference that is legitimate on one answer - Go's and Spring's own
	// unregistered-path 404 - must not become invisible on every other.
	Permit []string `yaml:"permit"`

	// Given runs before Request, on the same backend and the same cookie jar,
	// after the database is reset to the seed. It is how a case reaches a
	// state - signed in, throttled, a session near its expiry - without
	// asserting on the way there, beyond each request step's status.
	Given []Step `yaml:"given"`

	Request Request `yaml:"request"`
	Expect  Expect  `yaml:"expect"`

	// SourceFile is set by Load, for error messages. Not part of the format.
	SourceFile string `yaml:"-"`
}

type Request struct {
	Method string `yaml:"method"`
	// Path is relative to each backend's base URL, so it excludes /api.
	Path string `yaml:"path"`
	// RawTarget is Path's alternative for a case that needs bytes on the wire
	// url.Parse would otherwise normalise out of existence - a literal
	// backslash, say, which Go's own http.Client silently percent-encodes to
	// %5C before RawTarget existed, so a case naming one never actually
	// reached either backend's router (#64). Sent verbatim as the request
	// line's target, via URL.Opaque, after the base path: never escaped,
	// never re-parsed. Relative to each backend's base URL, so it excludes
	// /api, same as Path, and takes its leading slash the same way.
	RawTarget string            `yaml:"raw_target"`
	Headers   map[string]string `yaml:"headers"`
	// HeadersHex is Headers' alternative for a value that is not valid UTF-8 -
	// a YAML string is Unicode text, so it cannot hold arbitrary bytes, and a
	// `\xHH` escape in YAML means the Unicode code point U+00HH, not the raw
	// byte (#70: proving sessions.user_agent's invalid-UTF-8 rows needs the
	// raw byte on the wire, such as a lone 0x85). Each value is hex, decoded
	// and sent as the header's exact bytes. validate() rejects a name also
	// set in Headers (case-insensitively: HTTP header names are) and rejects
	// Host outright, which Headers gives its own connection-level meaning to
	// (do() sets req.Host, never a literal Host header) that raw bytes have
	// no equivalent for.
	HeadersHex map[string]string `yaml:"headers_hex"`
	// Body is sent verbatim. A string rather than a map, because some cases
	// exist precisely to send bytes that are not valid JSON.
	Body string `yaml:"body"`
	// Pad expands the one `{pad}` in Body, for a body too large to write
	// out: the 1 MiB cap's cases send one and a half.
	Pad *Pad `yaml:"pad"`
}

type Pad struct {
	With  string `yaml:"with"`
	Count int    `yaml:"count"`
}

const padToken = "{pad}"

// SentBody is the body as it goes on the wire.
func (r Request) SentBody() string {
	if r.Pad == nil {
		return r.Body
	}
	return strings.Replace(r.Body, padToken, strings.Repeat(r.Pad.With, r.Pad.Count), 1)
}

// Step is one thing Given does: a request whose status must match, or SQL run
// against that backend's own database. Exactly one of the two.
type Step struct {
	Request *Request `yaml:"request"`
	// Status is required on a request step: a setup login that silently
	// failed would otherwise make every assertion after it about the wrong
	// state.
	Status int `yaml:"status"`

	// SQL moves persisted state where no request can, such as a session's
	// expiry into the past. Run as one statement batch, no results read.
	SQL string `yaml:"sql"`

	// Outage takes the backend's database away - every connection it holds
	// terminated, every new one refused - until the case's request has been
	// answered. It is how a case reaches #23 and #27: what a backend says
	// when the database, not the caller, is the problem.
	Outage bool `yaml:"outage"`
}

// Expect is the asserted answer. Anything NOT named here is still compared
// between the two backends - that is failure mode 2, the one that catches the
// divergence nobody thought to assert.
type Expect struct {
	Status int `yaml:"status"`

	// StatusByBackend overrides Status per backend ("go"/"kotlin"), for a case
	// whose two backends are permitted to answer different statuses outright
	// (#64: Tomcat's connector-level 400 on a malformed path Go's router
	// simply has no route for). Never to paper over a bug: a case that sets
	// this must also cite, under `permit`, a case-scoped entry with
	// `status: true` - CheckPermits enforces the pairing, the same way a
	// body-permitting entry requires a body assertion. Exactly one of Status
	// or StatusByBackend is set, and the map must name every backend the
	// case's profile runs, or one backend's expectation goes unstated by
	// omission.
	StatusByBackend map[string]int `yaml:"status_by_backend"`

	// Headers are compared exactly, by value, case-insensitively on the name.
	Headers map[string]string `yaml:"headers"`

	// BodyJSON is compared as parsed JSON, so key order and whitespace do not
	// matter. Use BodyRaw when the bytes themselves are the assertion.
	BodyJSON any    `yaml:"body_json"`
	BodyRaw  string `yaml:"body_raw"`

	// BodyEmpty asserts a zero-length body, which is distinct from "no
	// assertion" and is what every 204 in this contract requires.
	BodyEmpty bool `yaml:"body_empty"`

	// SameAs is a second request, sent to the same backend, whose whole answer
	// this one must equal. It asserts a relation rather than bytes: "answers
	// exactly like an unregistered path" (#24) holds on each backend even where
	// the two backends' unregistered-path answers are permitted to differ.
	SameAs *Request `yaml:"same_as"`

	// SameAsFor is SameAs restricted to one backend ("go"/"kotlin"), for a
	// case whose two backends are permitted to differ so much (a
	// status_by_backend case, #64) that the relation SameAs asserts holds on
	// one backend and not the other: Go's malformed-path answer IS its own
	// unregistered-path 404, but Kotlin's connector-level 400 (or OPTIONS *'s
	// 200) is not that backend's own 404. Keyed the same as StatusByBackend.
	// A case may set SameAs, SameAsFor, both (for different backends), or
	// neither.
	SameAsFor map[string]*Request `yaml:"same_as_for"`

	// Cookies asserts every Set-Cookie on the answer, by cookie name. A nil map
	// asserts nothing; an empty one (`cookies: {}`) asserts that no cookie is
	// set at all, and any cookie not named is a failure.
	Cookies map[string]CookieExpect `yaml:"cookies"`

	// Rows are queries run against the backend's own database after the
	// request, each with the rows it must return. Persisted state is axis 2
	// of the milestone: a response can be right while the row behind it is
	// not.
	Rows []RowsExpect `yaml:"rows"`
}

// StatusFor is the status one backend's answer must have: StatusByBackend's
// entry when the case sets one, else the shared Status.
func (e Expect) StatusFor(backend string) int {
	if e.StatusByBackend != nil {
		return e.StatusByBackend[backend]
	}
	return e.Status
}

// SameAsRequest is the same_as request one backend's answer must equal, if
// any: SameAsFor's entry for backend when the case sets one, else the shared
// SameAs, else nil (a case need not use either).
func (e Expect) SameAsRequest(backend string) *Request {
	if r, ok := e.SameAsFor[backend]; ok {
		return r
	}
	return e.SameAs
}

// CookieExpect is one Set-Cookie, attribute by attribute. Every field but
// Domain is required, so a case cannot quietly leave an attribute unpinned.
type CookieExpect struct {
	// Value is compared exactly; ValuePattern is a regular expression the
	// whole value must match, for a minted token. Exactly one of the two.
	Value        *string `yaml:"value"`
	ValuePattern string  `yaml:"value_pattern"`

	Path *string `yaml:"path"`
	// Domain empty asserts the attribute is absent: the session cookie is
	// host-only (BOOTSTRAP.md §5).
	Domain   string `yaml:"domain"`
	MaxAge   *int   `yaml:"max_age"`
	HttpOnly *bool  `yaml:"http_only"`
	Secure   *bool  `yaml:"secure"`
	SameSite string `yaml:"same_site"`

	// Expires is a relation, because the value is an instant: `absent`,
	// `past` (at or before the response's Date), or `max-age` (Date plus
	// Max-Age, to within ExpiresTolerance).
	Expires string `yaml:"expires"`
}

const (
	ExpiresAbsent = "absent"
	ExpiresPast   = "past"
	ExpiresMaxAge = "max-age"
)

// RowsExpect is a query and the rows it must return, in order. Cast in SQL
// to text, integer or boolean: a column of any other type is refused, so
// that the comparison never depends on how a driver renders a timestamp.
type RowsExpect struct {
	SQL  string           `yaml:"sql"`
	Rows []map[string]any `yaml:"rows"`
}

const (
	ProfileDefault = "default"
	// ProfileLocalDisabled is AUTH_LOCAL_ENABLED=false with Google on, since a
	// backend refuses to boot with no provider at all.
	ProfileLocalDisabled = "local-disabled"
)

// Profiles is every configuration scripts/conformance.sh boots a pair for.
var Profiles = []string{ProfileDefault, ProfileLocalDisabled}

// ProfileOf is the profile a case runs against.
func (c *Case) ProfileOf() string {
	if c.Profile == "" {
		return ProfileDefault
	}
	return c.Profile
}

// Load reads every *.yaml in dir, sorted by filename then by case name, so a
// run reports in a stable order.
func Load(dir string) ([]Case, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no case files in %s", dir)
	}
	sort.Strings(paths)

	var all []Case
	seen := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p) //nolint:gosec // paths come from our own glob
		if err != nil {
			return nil, err
		}
		var cases []Case
		if err := yaml.Unmarshal(b, &cases); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for i := range cases {
			c := &cases[i]
			c.SourceFile = filepath.Base(p)
			if err := c.validate(); err != nil {
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			if prev, dup := seen[c.Name]; dup {
				return nil, fmt.Errorf("%s: duplicate case name %q, already in %s", p, c.Name, prev)
			}
			seen[c.Name] = c.SourceFile
		}
		all = append(all, cases...)
	}
	return all, nil
}

// validate rejects a case that would silently assert nothing. A case file is
// hand-written, and the failure it is most likely to have is an empty expect
// block that passes against any answer at all.
func (c *Case) validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("a case has no name")
	}
	if err := c.Request.validate(c.Name, "request"); err != nil {
		return err
	}
	switch {
	case c.Expect.Status != 0 && c.Expect.StatusByBackend != nil:
		return fmt.Errorf("%s: expect.status and expect.status_by_backend are mutually exclusive", c.Name)
	case c.Expect.Status == 0 && c.Expect.StatusByBackend == nil:
		return fmt.Errorf("%s: expect.status is required (or expect.status_by_backend, for a case whose backends differ by ruling)", c.Name)
	case c.Expect.StatusByBackend != nil:
		for k := range c.Expect.StatusByBackend {
			if k != "go" && k != "kotlin" {
				return fmt.Errorf("%s: expect.status_by_backend has an unknown backend %q, want go or kotlin", c.Name, k)
			}
		}
		for _, backend := range []string{"go", "kotlin"} {
			if c.Expect.StatusByBackend[backend] == 0 {
				return fmt.Errorf("%s: expect.status_by_backend is missing %q", c.Name, backend)
			}
		}
		if c.Expect.StatusByBackend["go"] == c.Expect.StatusByBackend["kotlin"] {
			return fmt.Errorf("%s: expect.status_by_backend has the same status for both backends (%d); "+
				"use expect.status instead", c.Name, c.Expect.StatusByBackend["go"])
		}
		if len(c.Permit) == 0 {
			return fmt.Errorf("%s: expect.status_by_backend needs a case-scoped permit entry with `status: true`", c.Name)
		}
	}
	for k := range c.Expect.SameAsFor {
		if k != "go" && k != "kotlin" {
			return fmt.Errorf("%s: expect.same_as_for has an unknown backend %q, want go or kotlin", c.Name, k)
		}
	}
	if c.Expect.BodyJSON != nil && c.Expect.BodyRaw != "" {
		return fmt.Errorf("%s: expect.body_json and expect.body_raw are mutually exclusive", c.Name)
	}
	if c.Expect.BodyEmpty && (c.Expect.BodyJSON != nil || c.Expect.BodyRaw != "") {
		return fmt.Errorf("%s: expect.body_empty cannot be combined with a body assertion", c.Name)
	}
	if !slices.Contains(Profiles, c.ProfileOf()) {
		return fmt.Errorf("%s: unknown profile %q, want one of %v", c.Name, c.Profile, Profiles)
	}
	if s := c.Expect.SameAs; s != nil {
		if err := s.validate(c.Name, "expect.same_as"); err != nil {
			return err
		}
		if reflect.DeepEqual(*s, c.Request) {
			return fmt.Errorf("%s: expect.same_as is identical to request; it would only ever compare an answer "+
				"to itself, which asserts nothing", c.Name)
		}
	}
	for backend, s := range c.Expect.SameAsFor {
		if err := s.validate(c.Name, "expect.same_as_for."+backend); err != nil {
			return err
		}
		if reflect.DeepEqual(*s, c.Request) {
			return fmt.Errorf("%s: expect.same_as_for.%s is identical to request; it would only ever compare an "+
				"answer to itself, which asserts nothing", c.Name, backend)
		}
	}
	for i, g := range c.Given {
		at := fmt.Sprintf("given[%d]", i)
		kinds := 0
		for _, set := range []bool{g.Request != nil, strings.TrimSpace(g.SQL) != "", g.Outage} {
			if set {
				kinds++
			}
		}
		if kinds > 1 {
			return fmt.Errorf("%s: %s is one of request, sql or outage, not several", c.Name, at)
		}
		switch {
		case g.Outage:
			if g.Status != 0 {
				return fmt.Errorf("%s: %s.status belongs to a request step", c.Name, at)
			}
			if i != len(c.Given)-1 {
				return fmt.Errorf("%s: %s: an outage must be the last given step, since nothing after it can reach the database", c.Name, at)
			}
		case g.Request != nil:
			if err := g.Request.validate(c.Name, at+".request"); err != nil {
				return err
			}
			if g.Status == 0 {
				return fmt.Errorf("%s: %s.status is required on a request step", c.Name, at)
			}
		case strings.TrimSpace(g.SQL) != "":
			if g.Status != 0 {
				return fmt.Errorf("%s: %s.status belongs to a request step", c.Name, at)
			}
		default:
			return fmt.Errorf("%s: %s has none of request, sql or outage", c.Name, at)
		}
	}
	for name, ck := range c.Expect.Cookies {
		if err := ck.validate(); err != nil {
			return fmt.Errorf("%s: expect.cookies.%s: %w", c.Name, name, err)
		}
	}
	for i, r := range c.Expect.Rows {
		if strings.TrimSpace(r.SQL) == "" {
			return fmt.Errorf("%s: expect.rows[%d].sql is required", c.Name, i)
		}
		if r.Rows == nil {
			return fmt.Errorf("%s: expect.rows[%d].rows is required; write `rows: []` to assert none", c.Name, i)
		}
	}
	return nil
}

func (r *Request) validate(caseName, at string) error {
	if r.Method == "" {
		return fmt.Errorf("%s: %s.method is required", caseName, at)
	}
	switch {
	case r.Path != "" && r.RawTarget != "":
		return fmt.Errorf("%s: %s.path and %s.raw_target are mutually exclusive", caseName, at, at)
	case strings.HasPrefix(r.RawTarget, "*") || strings.Contains(r.RawTarget, "://"):
		// The asterisk-form request-target (RFC 9110 §7.1: "*", or a ruling
		// #70 shape such as "*?x=1") and the absolute-form (a full URI, e.g.
		// "http://x*") are never nested under the API base, so the "/" prefix
		// rule does not apply to either.
	case r.RawTarget != "":
		if !strings.HasPrefix(r.RawTarget, "/") {
			return fmt.Errorf("%s: %s.raw_target must start with / (or be an asterisk-form or absolute-form "+
				"target) and exclude the /api base", caseName, at)
		}
	case !strings.HasPrefix(r.Path, "/"):
		return fmt.Errorf("%s: %s.path must start with / and exclude the /api base", caseName, at)
	}
	for name, hexValue := range r.HeadersHex {
		if strings.EqualFold(name, "Host") {
			return fmt.Errorf("%s: %s.headers_hex.%s: Host has no raw-bytes equivalent - do() sets req.Host from "+
				"%s.headers.Host, never a literal header - so put it there instead", caseName, at, name, at)
		}
		for other := range r.Headers {
			if strings.EqualFold(name, other) {
				return fmt.Errorf("%s: %s.headers_hex.%s also appears in %s.headers.%s (header names are "+
					"case-insensitive)", caseName, at, name, at, other)
			}
		}
		if _, err := hex.DecodeString(hexValue); err != nil {
			return fmt.Errorf("%s: %s.headers_hex.%s is not valid hex: %w", caseName, at, name, err)
		}
	}
	if r.Pad != nil {
		if r.Pad.With == "" || r.Pad.Count <= 0 {
			return fmt.Errorf("%s: %s.pad needs a non-empty `with` and a positive `count`", caseName, at)
		}
		if strings.Count(r.Body, padToken) != 1 {
			return fmt.Errorf("%s: %s.pad needs exactly one %s in the body", caseName, at, padToken)
		}
	}
	return nil
}

func (ck *CookieExpect) validate() error {
	switch {
	case ck.Value != nil && ck.ValuePattern != "":
		return fmt.Errorf("value and value_pattern are mutually exclusive")
	case ck.Value == nil && ck.ValuePattern == "":
		return fmt.Errorf("one of value or value_pattern is required")
	}
	if ck.ValuePattern != "" {
		if _, err := regexp.Compile(ck.ValuePattern); err != nil {
			return fmt.Errorf("value_pattern: %w", err)
		}
	}
	missing := []string{}
	if ck.Path == nil {
		missing = append(missing, "path")
	}
	if ck.MaxAge == nil {
		missing = append(missing, "max_age")
	}
	if ck.HttpOnly == nil {
		missing = append(missing, "http_only")
	}
	if ck.Secure == nil {
		missing = append(missing, "secure")
	}
	if ck.SameSite == "" {
		missing = append(missing, "same_site")
	}
	if len(missing) > 0 {
		return fmt.Errorf("every attribute is pinned, and these are missing: %s", strings.Join(missing, ", "))
	}
	if !slices.Contains([]string{ExpiresAbsent, ExpiresPast, ExpiresMaxAge}, ck.Expires) {
		return fmt.Errorf("expires must be %s, %s or %s, not %q", ExpiresAbsent, ExpiresPast, ExpiresMaxAge, ck.Expires)
	}
	return nil
}
