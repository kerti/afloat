package conformance

import (
	"fmt"
	"os"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// PermittedDifference is one response header the two backends are allowed to
// differ on, or to send unilaterally.
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
	Header string `yaml:"header"`

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

// PermittedSet answers "may these two differ here?" for a header name.
type PermittedSet struct {
	byHeader map[string]PermittedDifference
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
	set := &PermittedSet{byHeader: make(map[string]PermittedDifference, len(entries))}
	for _, e := range entries {
		if strings.TrimSpace(e.Header) == "" {
			return nil, fmt.Errorf("%s: an entry has no header", path)
		}
		if strings.TrimSpace(e.Why) == "" {
			return nil, fmt.Errorf("%s: %q has no `why`", path, e.Header)
		}
		if e.Provisional && strings.TrimSpace(e.Issue) == "" {
			return nil, fmt.Errorf("%s: %q is provisional but cites no issue", path, e.Header)
		}
		key := strings.ToLower(e.Header)
		if _, dup := set.byHeader[key]; dup {
			return nil, fmt.Errorf("%s: duplicate entry for %q", path, e.Header)
		}
		set.byHeader[key] = e
	}
	return set, nil
}

func (p *PermittedSet) Allows(header string) bool {
	_, ok := p.byHeader[strings.ToLower(header)]
	return ok
}

// Provisional lists the entries still waiting on a decision, so a run can say
// how much of its own allowlist is temporary rather than letting it calcify.
func (p *PermittedSet) Provisional() []PermittedDifference {
	var out []PermittedDifference
	for _, e := range p.byHeader {
		if e.Provisional {
			out = append(out, e)
		}
	}
	return out
}
