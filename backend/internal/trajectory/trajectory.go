// Package trajectory computes the daily-bucket spending trajectory: Today's
// Allowance, Left to Spend Today, Forward Daily Rate, Trajectory Variance,
// Buffer Days and State, plus the mid-period Budget-change target line. It is
// pure — no DB, no HTTP, no clock read — and takes the aggregates it needs as
// arguments (docs/CONTEXT.md §Calculation spec). Commitment exclusion, Period
// Day math and date comparisons are the caller's job, not this package's
// (BOOTSTRAP.md §11 step 8 part A).
//
// Every figure is computed in exact rational arithmetic (math/big.Rat) and
// rounded only once, on output, so a non-terminating Baseline Daily Budget
// (Daily Pool not evenly divisible by Period Days) never loses precision
// partway through a formula. The one named exception is Left to Spend Today,
// which subtracts today's spend from the already-floored Today's Allowance
// (ruling R1, contract/testdata/trajectory.json).
package trajectory

import (
	"fmt"
	"math/big"

	"github.com/shopspring/decimal"
)

// Mode selects how Today's Allowance is computed. Trajectory Variance,
// Buffer Days and State are computed against the target line in both modes
// (CONTEXT.md §Adaptive vs fixed) — the setting changes only A, which in fixed
// mode is the baseline in force.
type Mode string

const (
	ModeAdaptive Mode = "adaptive"
	ModeFixed    Mode = "fixed"
)

// State is one of the three trajectory readings a Household sees
// (CONTEXT.md §Trajectory).
type State string

const (
	StateAfloat        State = "afloat"
	StateDrifting      State = "drifting"
	StateTakingOnWater State = "taking_on_water"
)

// Parameters are the warm-up floor and State thresholds. They are fixture
// inputs, not constants (BOOTSTRAP.md §11 step 8 ruling R3; Q-05 answered, the
// warm-up stays 3: #155 R13) — a caller passes its own configured Parameters, or
// DefaultParameters.
type Parameters struct {
	// WarmUpDays: State is pinned to Afloat while DaysElapsed is below this.
	// Every other figure is still computed and reported normally during
	// warm-up; only the State reading is pinned.
	WarmUpDays int

	// DriftingBelow and TakingOnWaterBelow are dimensionless multipliers of
	// the baseline in force (B_now). State compares Trajectory Variance (V)
	// against threshold x B directly, with no division, so a non-terminating
	// B never enters the comparison as a rounded number (ruling R2):
	// V >= DriftingBelow*B is Afloat, V >= TakingOnWaterBelow*B is Drifting,
	// otherwise Taking on Water.
	DriftingBelow      decimal.Decimal
	TakingOnWaterBelow decimal.Decimal
}

// DefaultParameters mirrors contract/testdata/trajectory.json's top-level
// `parameters`. It is a convenience for callers with no configured override,
// never a domain constant a caller should assume — thresholds "live in one
// place and are expected to be tuned once real data exists" (CONTEXT.md
// §State thresholds).
func DefaultParameters() Parameters {
	return Parameters{
		WarmUpDays:         3,
		DriftingBelow:      decimal.NewFromInt(-1),
		TakingOnWaterBelow: decimal.NewFromInt(-3),
	}
}

// Inputs are the daily-bucket aggregates for one read (CONTEXT.md §Symbols:
// P, D, e, S_settled, S_today, S_future). Every money field is DECIMAL(20,4).
type Inputs struct {
	DailyPool    decimal.Decimal // P: the Daily Pool in force Today
	PeriodDays   int             // D: Total Period Days
	DaysElapsed  int             // e: settled days, strictly before Today
	SettledSpend decimal.Decimal // S_settled: occurred_on strictly before Today
	TodaySpend   decimal.Decimal // S_today: occurred_on = Today
	FutureSpend  decimal.Decimal // S_future: occurred_on after Today
	Mode         Mode
	Parameters   Parameters

	// TargetThroughElapsed is T(e), the exact cumulative target through the e
	// settled days, and BaselineInForce is B_now, the exact slope of the
	// target line on Today. Both come from a TargetLine (AtToday) and are
	// given together or not at all. Both nil means the Daily Pool never
	// changed in the Period, so the line is flat: B_now = max(P / D, 0) and
	// T(e) = B_now x e. Neither is ever rounded on the way in.
	TargetThroughElapsed *big.Rat
	BaselineInForce      *big.Rat
}

// Pace is what reads the trajectory against the target line: it exists only
// while a baseline is in force. Present means a State can be said.
type Pace struct {
	BaselineDailyBudget decimal.Decimal // B_now, floored (R1)
	TrajectoryVariance  decimal.Decimal // V = T(e) - S_settled, half-up (R2)
	BufferDays          decimal.Decimal // V / B_now, half-up (R2)
	State               State
}

// Figures are every trajectory number for one read, at the storage scale
// (DECIMAL(20,4), BOOTSTRAP.md §4). ForwardDailyRate is nil exactly when
// r = D - e = 1 — the last day of a Period — per CONTEXT.md's "undefined on
// the last day" rule; a wire response renders that as "—".
//
// Pace is nil exactly when the household is fully committed: the baseline in
// force is zero, so there is no pace to read and no State, Baseline Daily
// Budget or Trajectory Variance to say (CONTEXT.md §Fully committed). That is
// the target line's situation and says nothing about money; the figures that
// are about money (AvailablePool, TodaysAllowance, LeftToSpendToday) are
// always answered, and a negative one is a Household whose Available Pool is
// used up.
type Figures struct {
	AvailablePool    decimal.Decimal  // P' = P - max(S_future, 0)
	TodaysAllowance  decimal.Decimal  // A, floored (R1)
	LeftToSpendToday decimal.Decimal  // floored A - S_today (R1)
	ForwardDailyRate *decimal.Decimal // floored (R1); nil when r = 1
	Pace             *Pace            // nil when fully committed
}

// Calculate implements CONTEXT.md §Calculation spec's core figures. It never
// refuses a Daily Pool at or below zero: that is a fully committed Household
// (Pace nil), a situation to answer, not an error.
func Calculate(in Inputs) (Figures, error) {
	if in.PeriodDays <= 0 {
		return Figures{}, fmt.Errorf("trajectory: period_days must be positive, got %d", in.PeriodDays)
	}
	r := in.PeriodDays - in.DaysElapsed
	if r <= 0 {
		return Figures{}, fmt.Errorf("trajectory: days_elapsed %d leaves no days remaining in a %d-day period", in.DaysElapsed, in.PeriodDays)
	}
	if in.Mode != ModeAdaptive && in.Mode != ModeFixed {
		return Figures{}, fmt.Errorf("trajectory: unknown mode %q", in.Mode)
	}
	if (in.TargetThroughElapsed == nil) != (in.BaselineInForce == nil) {
		return Figures{}, fmt.Errorf("trajectory: target_through_elapsed and baseline_in_force are given together or not at all")
	}

	p := in.DailyPool.Rat()
	e := big.NewRat(int64(in.DaysElapsed), 1)
	rRat := big.NewRat(int64(r), 1)
	sSettled := in.SettledSpend.Rat()
	sToday := in.TodaySpend.Rat()

	var bNow, target *big.Rat
	if in.BaselineInForce != nil {
		bNow, target = in.BaselineInForce, in.TargetThroughElapsed
		if bNow.Sign() < 0 || target.Sign() < 0 {
			return Figures{}, fmt.Errorf("trajectory: a target line never runs below zero (baseline %s, target %s)", bNow.RatString(), target.RatString())
		}
	} else {
		// A pool at or below zero is a flat target at zero, like every segment.
		bNow = clampAtZero(new(big.Rat).Quo(p, big.NewRat(int64(in.PeriodDays), 1)))
		target = new(big.Rat).Mul(bNow, e)
	}

	// P' = P - max(S_future, 0): a refund dated in the future never raises
	// Today's Allowance (#155 R3). Rounding-free: both are already at scale <= 4.
	futureSpend := decimal.Max(in.FutureSpend, decimal.Zero)
	pPrime := new(big.Rat).Sub(p, futureSpend.Rat())

	var a *big.Rat
	if in.Mode == ModeFixed {
		a = bNow // A = B_now [fixed mode]; zero when fully committed
	} else {
		a = new(big.Rat).Quo(new(big.Rat).Sub(pPrime, sSettled), rRat) // A = (P' - S_settled) / r [adaptive mode]
	}
	aFloored := floorAt4dp(a)

	var forwardRate *decimal.Decimal
	if r > 1 {
		rMinus1 := big.NewRat(int64(r-1), 1)
		fwdNumerator := new(big.Rat).Sub(new(big.Rat).Sub(pPrime, sSettled), sToday)
		fwd := floorAt4dp(new(big.Rat).Quo(fwdNumerator, rMinus1))
		forwardRate = &fwd
	}

	figures := Figures{
		AvailablePool:    in.DailyPool.Sub(futureSpend), // exact
		TodaysAllowance:  aFloored,
		LeftToSpendToday: aFloored.Sub(in.TodaySpend), // Left = floored A - S_today (R1); S_today is exact at 4dp
		ForwardDailyRate: forwardRate,
	}
	if bNow.Sign() == 0 {
		return figures, nil
	}

	v := new(big.Rat).Sub(target, sSettled) // V = T(e) - S_settled
	buffer := new(big.Rat).Quo(v, bNow)     // Buffer Days = V / B_now
	figures.Pace = &Pace{
		BaselineDailyBudget: floorAt4dp(bNow),
		TrajectoryVariance:  halfUpAt4dp(v),
		BufferDays:          halfUpAt4dp(buffer),
		State:               decideState(in.DaysElapsed, in.Parameters, v, bNow),
	}
	return figures, nil
}

// decideState never divides: it compares V against threshold x B directly, on
// the exact (unrounded) V and B, per ruling R2. B is the baseline in force
// (B_now), so a household that cut its Budget is read against its new pace.
func decideState(daysElapsed int, params Parameters, v, b *big.Rat) State {
	if daysElapsed < params.WarmUpDays {
		return StateAfloat
	}
	driftingThreshold := new(big.Rat).Mul(params.DriftingBelow.Rat(), b)
	takingOnWaterThreshold := new(big.Rat).Mul(params.TakingOnWaterBelow.Rat(), b)
	switch {
	case v.Cmp(driftingThreshold) >= 0:
		return StateAfloat
	case v.Cmp(takingOnWaterThreshold) >= 0:
		return StateDrifting
	default:
		return StateTakingOnWater
	}
}

// scale4 is the money storage scale, DECIMAL(20,4) (BOOTSTRAP.md §4).
var scale4 = big.NewInt(10000)

// floorAt4dp rounds r toward negative infinity — not toward zero — to 4
// decimal places (ruling R1). It covers Today's Allowance, Left to Spend
// Today (via the already-floored A), Forward Daily Rate and the Baseline
// Daily Budget: Afloat never tells a Household it can spend more than it can.
func floorAt4dp(r *big.Rat) decimal.Decimal {
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(scale4))
	num := scaled.Num()   // signed
	den := scaled.Denom() // always positive for a big.Rat in lowest terms
	q, m := new(big.Int).QuoRem(num, den, new(big.Int))
	if m.Sign() != 0 && num.Sign() < 0 {
		q.Sub(q, big.NewInt(1))
	}
	return decimal.NewFromBigInt(q, -4)
}

// halfUpAt4dp rounds r to 4 decimal places, half away from zero (ruling R2),
// matching shopspring's own Round. It
// covers Trajectory Variance and Buffer Days.
func halfUpAt4dp(r *big.Rat) decimal.Decimal {
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(scale4))
	num := scaled.Num()
	den := scaled.Denom()
	q, m := new(big.Int).QuoRem(num, den, new(big.Int))
	twiceRemainder := new(big.Int).Mul(new(big.Int).Abs(m), big.NewInt(2))
	if twiceRemainder.Cmp(den) >= 0 {
		if num.Sign() >= 0 {
			q.Add(q, big.NewInt(1))
		} else {
			q.Sub(q, big.NewInt(1))
		}
	}
	return decimal.NewFromBigInt(q, -4)
}
