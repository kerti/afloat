package trajectory

import (
	"fmt"
	"math/big"

	"github.com/shopspring/decimal"
)

// TargetLineInputs describes a mid-period Budget change (CONTEXT.md §Mid-
// period Budget changes). EffectiveDay is 1-indexed: the new Daily Pool
// takes effect from that day of the Period onward. A Budget that never
// changed mid-Period never constructs a TargetLine; the caller's ordinary
// Baseline Daily Budget line covers that case.
type TargetLineInputs struct {
	PeriodDays      int             // D
	DailyPoolBefore decimal.Decimal // the old Daily Pool
	DailyPoolAfter  decimal.Decimal // the new Daily Pool, effective at EffectiveDay
	EffectiveDay    int             // j, 1-indexed
}

// TargetLine is the cumulative target line for a mid-period Budget change,
// computed once so repeated calls to At don't redo the exact-rational setup.
// The line kinks continuously at EffectiveDay — B_new is derived so the line
// still terminates exactly at DailyPoolAfter on the last day — and never dips
// below T(j-1): when DailyPoolAfter <= T(j-1) the household cut its Budget to
// at or below what it already targeted, B_new comes out zero or negative, and
// the line clamps flat at T(j-1) for the rest of the Period instead of
// falling (CONTEXT.md's clamp rule; ruling R4 puts this arithmetic in scope,
// not the Q-06 presentation question).
type TargetLine struct {
	periodDays    int
	effectiveDay  int
	bOld          *big.Rat // Baseline Daily Budget before the change
	tjm1          *big.Rat // T(j-1): the cumulative target through day j-1
	bNew          *big.Rat // the raw (possibly zero or negative) new baseline
	baselineAfter decimal.Decimal
}

// NewTargetLine computes T(j-1) and B_new once for repeated At calls.
func NewTargetLine(in TargetLineInputs) (TargetLine, error) {
	if in.PeriodDays <= 0 {
		return TargetLine{}, fmt.Errorf("trajectory: period_days must be positive, got %d", in.PeriodDays)
	}
	if in.EffectiveDay < 1 || in.EffectiveDay > in.PeriodDays {
		return TargetLine{}, fmt.Errorf("trajectory: effective_day %d out of range for a %d-day period", in.EffectiveDay, in.PeriodDays)
	}

	d := big.NewRat(int64(in.PeriodDays), 1)
	j := big.NewRat(int64(in.EffectiveDay), 1)
	jMinus1 := new(big.Rat).Sub(j, big.NewRat(1, 1))

	bOld := new(big.Rat).Quo(in.DailyPoolBefore.Rat(), d)
	tjm1 := new(big.Rat).Mul(bOld, jMinus1) // T(j-1) = B_old x (j-1)

	remainingDays := new(big.Rat).Sub(d, jMinus1) // D - (j-1); positive since EffectiveDay <= PeriodDays
	bNew := new(big.Rat).Quo(new(big.Rat).Sub(in.DailyPoolAfter.Rat(), tjm1), remainingDays)

	return TargetLine{
		periodDays:    in.PeriodDays,
		effectiveDay:  in.EffectiveDay,
		bOld:          bOld,
		tjm1:          tjm1,
		bNew:          bNew,
		baselineAfter: floorAt4dp(bNew),
	}, nil
}

// BaselineAfter is B_new, floored at 4dp. It may be zero or negative — that
// is the fact the clamp in At responds to, not an error.
func (t TargetLine) BaselineAfter() decimal.Decimal {
	return t.baselineAfter
}

// At returns the cumulative target through the given day (1-indexed),
// half-up at 4dp. A day before EffectiveDay uses the old baseline; a day at
// or after it uses the new one, clamped so the line never falls below
// T(j-1).
func (t TargetLine) At(day int) decimal.Decimal {
	dayRat := big.NewRat(int64(day), 1)
	jMinus1 := new(big.Rat).Sub(big.NewRat(int64(t.effectiveDay), 1), big.NewRat(1, 1))

	if dayRat.Cmp(jMinus1) <= 0 {
		return halfUpAt4dp(new(big.Rat).Mul(t.bOld, dayRat))
	}

	daysSinceChange := new(big.Rat).Sub(dayRat, jMinus1)
	candidate := new(big.Rat).Add(t.tjm1, new(big.Rat).Mul(t.bNew, daysSinceChange))
	if candidate.Cmp(t.tjm1) < 0 {
		return halfUpAt4dp(t.tjm1) // clamp: flat at T(j-1)
	}
	return halfUpAt4dp(candidate)
}
