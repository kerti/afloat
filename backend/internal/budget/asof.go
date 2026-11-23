package budget

import (
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/period"
)

// Version is one entry of a Period's Monthly Budget log: the amount from the
// day it took effect (CONTEXT.md §Mid-period Budget changes).
type Version struct {
	EffectiveOn   period.Date
	MonthlyBudget decimal.Decimal
}

// MonthlyBudgetAsOf is the Monthly Budget in force on a Period Day: the version
// with the greatest effective_on on or before it, with one clamp. A day before
// the Period's first version is read as of that first version, so the Monthly
// Budget a Household sets on day 14 is its Budget for the whole Period, days 1
// to 13 included, and the Expenses captured earlier count against it.
// Before any version exists no Budget is in force.
//
// This is the one place the as-of rule is written. The trajectory slice reads
// the same log and must call this rather than restate it. versions may be in
// any order.
func MonthlyBudgetAsOf(versions []Version, day period.Date) (decimal.Decimal, bool) {
	if len(versions) == 0 {
		return decimal.Zero, false
	}
	day = InForceDay(versions, day)
	var best *Version
	for i := range versions {
		v := &versions[i]
		if day.Before(v.EffectiveOn) {
			continue
		}
		if best == nil || best.EffectiveOn.Before(v.EffectiveOn) {
			best = v
		}
	}
	// day >= the first version's, so that version qualifies and best is set.
	return best.MonthlyBudget, true
}

// InForceDay is the day a Period Day is read as of: the day itself, except that
// one before the Period's first Monthly Budget is read as of that first one.
// Allocations are read as of the same day (#130 C4), so a Commitment typed
// before the Monthly Budget is part of the same setup. Before any Monthly
// Budget exists the day is its own.
func InForceDay(versions []Version, day period.Date) period.Date {
	if len(versions) == 0 {
		return day
	}
	first := versions[0].EffectiveOn
	for _, v := range versions[1:] {
		if v.EffectiveOn.Before(first) {
			first = v.EffectiveOn
		}
	}
	if day.Before(first) {
		return first
	}
	return day
}
