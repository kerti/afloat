// Package conformance drives both Afloat backends over HTTP and asserts that
// they answer identically.
//
// It is a test-only package: the runner lives in conformance_test.go. The types
// here describe the case-file format, which is the vocabulary the whole harness
// is written in.
package conformance

import (
	"fmt"
	"os"
	"path/filepath"
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

	Request Request `yaml:"request"`
	Expect  Expect  `yaml:"expect"`

	// SourceFile is set by Load, for error messages. Not part of the format.
	SourceFile string `yaml:"-"`
}

type Request struct {
	Method string `yaml:"method"`
	// Path is relative to each backend's base URL, so it excludes /api.
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
	// Body is sent verbatim. A string rather than a map, because some cases
	// exist precisely to send bytes that are not valid JSON.
	Body string `yaml:"body"`
}

// Expect is the asserted answer. Anything NOT named here is still compared
// between the two backends - that is failure mode 2, the one that catches the
// divergence nobody thought to assert.
type Expect struct {
	Status int `yaml:"status"`

	// Headers are compared exactly, by value, case-insensitively on the name.
	Headers map[string]string `yaml:"headers"`

	// BodyJSON is compared as parsed JSON, so key order and whitespace do not
	// matter. Use BodyRaw when the bytes themselves are the assertion.
	BodyJSON any    `yaml:"body_json"`
	BodyRaw  string `yaml:"body_raw"`

	// BodyEmpty asserts a zero-length body, which is distinct from "no
	// assertion" and is what every 204 in this contract requires.
	BodyEmpty bool `yaml:"body_empty"`
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
	if c.Request.Method == "" {
		return fmt.Errorf("%s: request.method is required", c.Name)
	}
	if !strings.HasPrefix(c.Request.Path, "/") {
		return fmt.Errorf("%s: request.path must start with / and exclude the /api base", c.Name)
	}
	if c.Expect.Status == 0 {
		return fmt.Errorf("%s: expect.status is required", c.Name)
	}
	if c.Expect.BodyJSON != nil && c.Expect.BodyRaw != "" {
		return fmt.Errorf("%s: expect.body_json and expect.body_raw are mutually exclusive", c.Name)
	}
	if c.Expect.BodyEmpty && (c.Expect.BodyJSON != nil || c.Expect.BodyRaw != "") {
		return fmt.Errorf("%s: expect.body_empty cannot be combined with a body assertion", c.Name)
	}
	return nil
}
