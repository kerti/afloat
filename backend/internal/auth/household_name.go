package auth

import (
	"strings"
	"unicode/utf8"
)

// maxHouseholdDisplayNameCodePoints caps the Household's name, in code points
// like every other length in this codebase (BOOTSTRAP.md §5.1). Counted on the
// trimmed name: the contract's own maxLength is an outer bound that counts the
// padding.
const maxHouseholdDisplayNameCodePoints = 60

// CheckHouseholdDisplayName is the one rule for a Household's name, called by
// founding (CommitOnboardingChoice) and by editing (PATCH /household), so
// neither can store a name the other would refuse (BOOTSTRAP.md §5.1.1). It
// returns the name as it is to be stored, trimmed, and the VALIDATION rule it
// breaks, or "" when it is fine. It is CheckBoundedName at the Household's cap.
func CheckHouseholdDisplayName(raw string) (name, rule string) {
	return CheckBoundedName(raw, maxHouseholdDisplayNameCodePoints)
}

// CheckBoundedName is the rule every bounded display name follows (a
// Household's, a Pocket's: BOOTSTRAP.md §4's Names bullet), so no two entities
// can drift into disagreeing about what a name is. Only the cap differs, and
// the caller supplies it. It returns the name as it is to be stored, trimmed,
// and the VALIDATION rule it breaks, or "" when it is fine.
//
// The rules, in order: nothing left after the trim is `required`; more than
// maxCodePoints code points is `max`; a U+0000, which Postgres cannot store, is
// `pattern`; and a name of only whitespace and format characters is `required`,
// blank to the eye though not to TrimSpace. A format character inside a visible
// name is kept, bidi controls included.
func CheckBoundedName(raw string, maxCodePoints int) (name, rule string) {
	name = strings.TrimSpace(raw)
	switch {
	case name == "":
		return name, "required"
	case utf8.RuneCountInString(name) > maxCodePoints:
		return name, "max"
	case !storable(name):
		return name, "pattern"
	case invisible(name):
		return name, "required"
	}
	return name, ""
}
