package trajectory

import (
	"fmt"
	"math/big"

	"github.com/shopspring/decimal"
)

// PoolChange is a Daily Pool taking effect on a Period Day (CONTEXT.md
// §Mid-period Budget changes). EffectiveDay is 1-indexed: the pool applies
// from that day of the Period onward. Any change of the Daily Pool is one,
// whether it came from the Monthly Budget or from a Commitment's allocation.
type PoolChange struct {
	EffectiveDay int
	DailyPool    decimal.Decimal
}

// segment is one straight stretch of the target line. before is T(start-1),
// the cumulative target through the day before the segment begins; raw is the
// slope that would end the line exactly on the segment's pool, and slope is
// raw clamped at zero, so the line never falls.
type segment struct {
	start  int
	before *big.Rat
	raw    *big.Rat
	slope  *big.Rat
}

// TargetLine is the cumulative target line of a Period whose Daily Pool may
// have changed on the way (CONTEXT.md §Mid-period Budget changes). It is built
// once from the pool the Period began with and the changes since, so repeated
// reads don't redo the exact-rational setup. Every value is an exact rational
// until a caller asks for a rounded one: T(j-1) at each kink is never rounded,
// so no change accumulates error from the one before it.
//
// The line kinks continuously: a segment starting on day j has the slope that
// would carry it from T(j-1) to its pool on the last day, and the slope of
// every segment, the first included, is clamped at zero. A pool at or below
// zero is a flat target at zero from day 1; a cut to at or below what the
// line had already reached holds it flat at T(j-1); and a later raise starts
// from that flat value, not from where an unclamped line would have been.
type TargetLine struct {
	periodDays int
	segments   []segment
}

// NewTargetLine builds the line for a Period of periodDays days that began with
// basePool and then took changes, in effective-day order. A change effective on
// day 1 replaces the base (no kink). Two changes on one day are one: the later
// wins. A change to the pool already in force merges away. A Period that never
// changed passes none.
func NewTargetLine(periodDays int, basePool decimal.Decimal, changes []PoolChange) (TargetLine, error) {
	if periodDays <= 0 {
		return TargetLine{}, fmt.Errorf("trajectory: period_days must be positive, got %d", periodDays)
	}
	last := 1
	for _, c := range changes {
		if c.EffectiveDay < 1 || c.EffectiveDay > periodDays {
			return TargetLine{}, fmt.Errorf("trajectory: effective_day %d out of range for a %d-day period", c.EffectiveDay, periodDays)
		}
		if c.EffectiveDay < last {
			return TargetLine{}, fmt.Errorf("trajectory: changes out of order: day %d after day %d", c.EffectiveDay, last)
		}
		last = c.EffectiveDay
	}

	merged := make([]PoolChange, 0, len(changes))
	for _, c := range changes {
		if n := len(merged); n > 0 && merged[n-1].EffectiveDay == c.EffectiveDay {
			merged[n-1].DailyPool = c.DailyPool
			before := basePool
			if n > 1 {
				before = merged[n-2].DailyPool
			}
			if c.DailyPool.Equal(before) {
				merged = merged[:n-1]
			}
			continue
		}
		before := basePool
		if n := len(merged); n > 0 {
			before = merged[n-1].DailyPool
		}
		if !c.DailyPool.Equal(before) {
			merged = append(merged, c)
		}
	}

	line := TargetLine{periodDays: periodDays}
	first := new(big.Rat).Quo(basePool.Rat(), big.NewRat(int64(periodDays), 1))
	line.segments = append(line.segments, segment{start: 1, before: new(big.Rat), raw: first, slope: clampAtZero(first)})
	for _, c := range merged {
		before := line.ThroughDay(c.EffectiveDay - 1)
		remaining := big.NewRat(int64(periodDays-(c.EffectiveDay-1)), 1) // positive: EffectiveDay <= periodDays
		raw := new(big.Rat).Quo(new(big.Rat).Sub(c.DailyPool.Rat(), before), remaining)
		line.segments = append(line.segments, segment{start: c.EffectiveDay, before: before, raw: raw, slope: clampAtZero(raw)})
	}
	return line, nil
}

func clampAtZero(r *big.Rat) *big.Rat {
	if r.Sign() < 0 {
		return new(big.Rat)
	}
	return new(big.Rat).Set(r)
}

// segmentAt is the segment in force on day: the last one that has started.
func (t TargetLine) segmentAt(day int) segment {
	for i := len(t.segments) - 1; i > 0; i-- {
		if t.segments[i].start <= day {
			return t.segments[i]
		}
	}
	return t.segments[0]
}

// ThroughDay is T(day), the exact cumulative target through the given day
// (0 to D); T(0) is zero.
func (t TargetLine) ThroughDay(day int) *big.Rat {
	if day <= 0 {
		return new(big.Rat)
	}
	s := t.segmentAt(day)
	elapsed := big.NewRat(int64(day-(s.start-1)), 1)
	return new(big.Rat).Add(s.before, new(big.Rat).Mul(s.slope, elapsed))
}

// BaselineInForce is the exact slope of the line on the given day (1 to D):
// the Baseline Daily Budget a Household reads against that day, clamped at
// zero, so it is never negative. Zero means the line is flat there.
func (t TargetLine) BaselineInForce(day int) *big.Rat {
	return new(big.Rat).Set(t.segmentAt(day).slope)
}

// AtToday returns the two numbers Calculate takes from a line: T(e), the
// cumulative target through the e settled days, and B_now, the slope in force
// on Today (day e+1).
func (t TargetLine) AtToday(daysElapsed int) (target, baseline *big.Rat) {
	return t.ThroughDay(daysElapsed), t.BaselineInForce(daysElapsed + 1)
}

// At returns the cumulative target through the given day, half-up at 4dp.
func (t TargetLine) At(day int) decimal.Decimal {
	return halfUpAt4dp(t.ThroughDay(day))
}

// BaselineAt is the slope in force on the given day, floored at 4dp like every
// allowance figure (ruling R1).
func (t TargetLine) BaselineAt(day int) decimal.Decimal {
	return floorAt4dp(t.BaselineInForce(day))
}

// BaselineAfter is the last segment's slope before the clamp, floored at 4dp:
// the B_new of CONTEXT.md's formula, which is zero or negative exactly when
// the last change cut the Budget to at or below what the line had already
// targeted. That is the fact the clamp responds to, not an error. The line
// itself never has a negative slope; BaselineAt is what is in force.
func (t TargetLine) BaselineAfter() decimal.Decimal {
	return floorAt4dp(t.segments[len(t.segments)-1].raw)
}
