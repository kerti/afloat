package conformance

import (
	"fmt"
	"os"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// PermittedDifference is something the two backends are allowed to differ on.
//
// Three kinds. A global entry names one `header` and applies to every case. A
// case-scoped entry has a `name`, lists `headers` and/or `body`, and applies
// only to the cases that cite it under `permit:` - for a difference that is
// legitimate on one answer and would be a bug on any other. A column entry
// names one `table.column` the persisted-state comparison skips, on every
// case.
//
// Milestone "Base parity" axis 1 is "identical observable responses ... plus a
// documented and tested list of permitted differences". This file is that list,
// in the form the runner can read. README.md carries the prose; this carries
// the enforcement, so the two cannot drift into disagreeing.
//
// Every entry must say WHY. An entry that encodes a decision must also cite the
// issue that made it - an exemption with no decision behind it is a divergence
// someone hid rather than one anyone ruled on.
type PermittedDifference struct {
	// Header is matched case-insensitively against the response header name.
	// Set on a global entry only.
	Header string `yaml:"header"`

	// Name is what a case cites under `permit:`. Set on a case-scoped entry
	// only, together with Headers and/or Body.
	Name    string   `yaml:"name"`
	Headers []string `yaml:"headers"`
	// Body lets the body bytes differ. A case citing it must still say
	// something about the body, which is what CheckPermits enforces.
	Body bool `yaml:"body"`

	// Column is "table.column", left out of the persisted-state comparison.
	// For a value one backend cannot reproduce from the other's, such as a
	// hash of a random token; every other column is compared.
	Column string `yaml:"column"`

	// Why is required prose. Protocol facts (Date) need only this.
	Why string `yaml:"why"`

	// Issue is the number that decided it, for anything that is a ruling
	// rather than a property of HTTP itself.
	Issue string `yaml:"issue"`

	// Provisional marks an entry that exists only because its decision has not
	// landed yet. It must carry an Issue, and it is what keeps the harness
	// usable before Wave 1 is ruled on without pretending the difference is
	// settled.
	Provisional bool `yaml:"provisional"`
}

// PermittedSet answers "may these two differ here?".
type PermittedSet struct {
	byHeader map[string]PermittedDifference
	byName   map[string]PermittedDifference
	byColumn map[string]PermittedDifference
}

func LoadPermitted(path string) (*PermittedSet, error) {
	b, err := os.ReadFile(path) //nolint:gosec // fixed path from the caller
	if err != nil {
		return nil, err
	}
	var entries []PermittedDifference
	if err := yaml.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	set := &PermittedSet{
		byHeader: make(map[string]PermittedDifference, len(entries)),
		byName:   map[string]PermittedDifference{},
		byColumn: map[string]PermittedDifference{},
	}
	for _, e := range entries {
		if err := e.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if e.Name != "" {
			if _, dup := set.byName[e.Name]; dup {
				return nil, fmt.Errorf("%s: duplicate entry named %q", path, e.Name)
			}
			set.byName[e.Name] = e
			continue
		}
		if e.Column != "" {
			if _, dup := set.byColumn[e.Column]; dup {
				return nil, fmt.Errorf("%s: duplicate entry for column %q", path, e.Column)
			}
			set.byColumn[e.Column] = e
			continue
		}
		key := strings.ToLower(e.Header)
		if _, dup := set.byHeader[key]; dup {
			return nil, fmt.Errorf("%s: duplicate entry for %q", path, e.Header)
		}
		set.byHeader[key] = e
	}
	return set, nil
}

func (e PermittedDifference) validate() error {
	hasHeader := strings.TrimSpace(e.Header) != ""
	hasName := strings.TrimSpace(e.Name) != ""
	hasColumn := strings.TrimSpace(e.Column) != ""
	label := e.Header + e.Name + e.Column
	kinds := 0
	for _, has := range []bool{hasHeader, hasName, hasColumn} {
		if has {
			kinds++
		}
	}
	switch {
	case kinds > 1:
		return fmt.Errorf("%q: an entry is one of `header`, `name` or `column`, not several", label)
	case kinds == 0:
		return fmt.Errorf("an entry has none of `header`, `name` or `column`")
	}
	if hasColumn {
		if t, c, ok := strings.Cut(e.Column, "."); !ok || t == "" || c == "" || strings.Contains(c, ".") {
			return fmt.Errorf("%q: a column entry is written table.column", label)
		}
	}
	if strings.TrimSpace(e.Why) == "" {
		return fmt.Errorf("%q has no `why`", label)
	}
	if e.Provisional && strings.TrimSpace(e.Issue) == "" {
		return fmt.Errorf("%q is provisional but cites no issue", label)
	}
	if (hasHeader || hasColumn) && (len(e.Headers) > 0 || e.Body) {
		return fmt.Errorf("%q: `headers` and `body` belong to a case-scoped entry, which needs a `name`", label)
	}
	if hasName {
		// Scoping a difference to named cases is always a ruling about those
		// answers, never a property of HTTP.
		if strings.TrimSpace(e.Issue) == "" {
			return fmt.Errorf("%q is case-scoped but cites no issue", label)
		}
		if len(e.Headers) == 0 && !e.Body {
			return fmt.Errorf("%q permits nothing: give it `headers`, `body`, or both", label)
		}
	}
	return nil
}

// Allows reports whether a header may differ on every case.
func (p *PermittedSet) Allows(header string) bool {
	_, ok := p.byHeader[strings.ToLower(header)]
	return ok
}

// MasksColumn reports whether the persisted-state comparison skips a column.
func (p *PermittedSet) MasksColumn(table, column string) bool {
	_, ok := p.byColumn[table+"."+column]
	return ok
}

// ForCase is what may differ on one case: the global headers plus whatever
// the case-scoped entries it cites add.
func (p *PermittedSet) ForCase(c Case) (allowsHeader func(string) bool, allowsBody bool) {
	extra := map[string]bool{}
	for _, n := range c.Permit {
		e := p.byName[n]
		for _, h := range e.Headers {
			extra[strings.ToLower(h)] = true
		}
		allowsBody = allowsBody || e.Body
	}
	return func(h string) bool { return p.Allows(h) || extra[strings.ToLower(h)] }, allowsBody
}

// CheckPermits cross-checks the cases against the list. A case may cite only
// an entry that exists; a case permitted a body difference must pin the body
// some other way, or the parity check has gone blind on it; and a case-scoped
// entry no case cites is an exemption nothing uses.
func CheckPermits(cases []Case, p *PermittedSet) error {
	cited := map[string]bool{}
	for _, c := range cases {
		for _, n := range c.Permit {
			e, ok := p.byName[n]
			if !ok {
				return fmt.Errorf("%s: %q permits %q, which permitted-differences.yaml does not define",
					c.SourceFile, c.Name, n)
			}
			cited[n] = true
			if e.Body && c.Expect.SameAs == nil && c.Expect.BodyJSON == nil && c.Expect.BodyRaw == "" && !c.Expect.BodyEmpty {
				return fmt.Errorf("%s: %q permits a body difference via %q but asserts nothing about the body; "+
					"add expect.same_as or a body assertion", c.SourceFile, c.Name, n)
			}
		}
	}
	for n := range p.byName {
		if !cited[n] {
			return fmt.Errorf("case-scoped permitted difference %q is cited by no case", n)
		}
	}
	return nil
}

// Provisional lists the entries still waiting on a decision, so a run can say
// how much of its own allowlist is temporary rather than letting it calcify.
func (p *PermittedSet) Provisional() []PermittedDifference {
	var out []PermittedDifference
	for _, m := range []map[string]PermittedDifference{p.byHeader, p.byName, p.byColumn} {
		for _, e := range m {
			if e.Provisional {
				out = append(out, e)
			}
		}
	}
	return out
}
