package trajectory_test

import (
	"math/big"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/trajectory"
)

// These guard the invariants Calculate and NewTargetLine must refuse rather
// than silently misbehave on — none of them are fixture rows, since a
// fixture row describes a valid read, not a malformed one.

func TestCalculateRejectsNonPositivePeriodDays(t *testing.T) {
	_, err := trajectory.Calculate(trajectory.Inputs{
		DailyPool:  decimal.NewFromInt(100),
		PeriodDays: 0,
		Mode:       trajectory.ModeAdaptive,
		Parameters: trajectory.DefaultParameters(),
	})
	if err == nil {
		t.Fatal("want an error for period_days = 0")
	}
}

func TestCalculateRejectsDaysElapsedAtOrPastThePeriod(t *testing.T) {
	// e = D leaves r = 0: there is no Today left in the Period.
	_, err := trajectory.Calculate(trajectory.Inputs{
		DailyPool:   decimal.NewFromInt(100),
		PeriodDays:  10,
		DaysElapsed: 10,
		Mode:        trajectory.ModeAdaptive,
		Parameters:  trajectory.DefaultParameters(),
	})
	if err == nil {
		t.Fatal("want an error when days_elapsed leaves no days remaining")
	}
}

func TestCalculateRejectsAnUnknownMode(t *testing.T) {
	_, err := trajectory.Calculate(trajectory.Inputs{
		DailyPool:  decimal.NewFromInt(100),
		PeriodDays: 10,
		Mode:       trajectory.Mode("adaptive-ish"),
		Parameters: trajectory.DefaultParameters(),
	})
	if err == nil {
		t.Fatal("want an error for an unrecognised mode")
	}
}

func TestForwardDailyRateIsNilOnlyOnTheLastDay(t *testing.T) {
	// r = 2: a forward rate exists (there is a day after Today).
	got, err := trajectory.Calculate(trajectory.Inputs{
		DailyPool:   decimal.NewFromInt(300),
		PeriodDays:  10,
		DaysElapsed: 8,
		Mode:        trajectory.ModeAdaptive,
		Parameters:  trajectory.DefaultParameters(),
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if got.ForwardDailyRate == nil {
		t.Fatal("forward_daily_rate should not be nil when r > 1")
	}
}

func TestCalculateAnswersAWhenTheDailyPoolIsZeroAndSaysNoState(t *testing.T) {
	// P = 0 gives a zero baseline: fully committed. That is a situation to
	// answer (A and Left to Spend Today are still true), never an error, and
	// there is no State to say: Pace is nil, not a State that reads afloat.
	got, err := trajectory.Calculate(trajectory.Inputs{
		DailyPool:    decimal.Zero,
		PeriodDays:   10,
		DaysElapsed:  4,
		SettledSpend: decimal.NewFromInt(60),
		Mode:         trajectory.ModeAdaptive,
		Parameters:   trajectory.DefaultParameters(),
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if got.Pace != nil {
		t.Errorf("pace = %+v, want none for a zero daily pool", *got.Pace)
	}
	// (0 - 60) / 6 = -10: the money is gone and the allowance says so.
	if want := "-10.0000"; got.TodaysAllowance.StringFixed(4) != want {
		t.Errorf("todays_allowance = %s, want %s", got.TodaysAllowance.StringFixed(4), want)
	}
}

func TestCalculateAnswersANegativeDailyPoolWithNoStateAndAZeroFixedAllowance(t *testing.T) {
	// Commitments above the Monthly Budget: a negative pool is a flat target
	// at zero, so a fixed allowance is zero and not the raw negative P / D.
	got, err := trajectory.Calculate(trajectory.Inputs{
		DailyPool:  decimal.NewFromInt(-100),
		PeriodDays: 10,
		Mode:       trajectory.ModeFixed,
		Parameters: trajectory.DefaultParameters(),
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if got.Pace != nil {
		t.Errorf("pace = %+v, want none for a negative daily pool", *got.Pace)
	}
	if !got.TodaysAllowance.IsZero() {
		t.Errorf("todays_allowance = %s, want 0", got.TodaysAllowance)
	}
}

func TestCalculateRejectsHalfALineOrANegativeOne(t *testing.T) {
	base := trajectory.Inputs{
		DailyPool:  decimal.NewFromInt(100),
		PeriodDays: 10,
		Mode:       trajectory.ModeAdaptive,
		Parameters: trajectory.DefaultParameters(),
	}
	cases := map[string]func(*trajectory.Inputs){
		"target without baseline": func(in *trajectory.Inputs) { in.TargetThroughElapsed = big.NewRat(0, 1) },
		"baseline without target": func(in *trajectory.Inputs) { in.BaselineInForce = big.NewRat(10, 1) },
		"negative baseline": func(in *trajectory.Inputs) {
			in.TargetThroughElapsed, in.BaselineInForce = big.NewRat(0, 1), big.NewRat(-1, 1)
		},
		"negative target": func(in *trajectory.Inputs) {
			in.TargetThroughElapsed, in.BaselineInForce = big.NewRat(-1, 1), big.NewRat(1, 1)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := base
			mutate(&in)
			if _, err := trajectory.Calculate(in); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestCalculateIgnoresAFutureDatedRefundForThePool(t *testing.T) {
	// P' = P - max(S_future, 0): a refund dated in the future never raises
	// the Available Pool or Today's Allowance (#155 R3).
	got, err := trajectory.Calculate(trajectory.Inputs{
		DailyPool:   decimal.NewFromInt(300),
		PeriodDays:  10,
		DaysElapsed: 0,
		FutureSpend: decimal.NewFromInt(-50),
		Mode:        trajectory.ModeAdaptive,
		Parameters:  trajectory.DefaultParameters(),
	})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if got.AvailablePool.StringFixed(4) != "300.0000" || got.TodaysAllowance.StringFixed(4) != "30.0000" {
		t.Errorf("available_pool %s, todays_allowance %s; want 300.0000 and 30.0000", got.AvailablePool, got.TodaysAllowance)
	}
}

func TestNewTargetLineRejectsNonPositivePeriodDays(t *testing.T) {
	_, err := trajectory.NewTargetLine(0, decimal.NewFromInt(100), []trajectory.PoolChange{
		{EffectiveDay: 1, DailyPool: decimal.NewFromInt(200)},
	})
	if err == nil {
		t.Fatal("want an error for period_days = 0")
	}
}

func TestNewTargetLineRejectsAnEffectiveDayOutOfRange(t *testing.T) {
	for _, day := range []int{0, -1, 31} {
		_, err := trajectory.NewTargetLine(30, decimal.NewFromInt(100), []trajectory.PoolChange{
			{EffectiveDay: day, DailyPool: decimal.NewFromInt(200)},
		})
		if err == nil {
			t.Fatalf("want an error for effective_day = %d in a 30-day period", day)
		}
	}
}

func TestNewTargetLineRejectsChangesOutOfOrder(t *testing.T) {
	_, err := trajectory.NewTargetLine(30, decimal.NewFromInt(100), []trajectory.PoolChange{
		{EffectiveDay: 20, DailyPool: decimal.NewFromInt(200)},
		{EffectiveDay: 10, DailyPool: decimal.NewFromInt(300)},
	})
	if err == nil {
		t.Fatal("want an error for changes that run backwards")
	}
}

func TestATargetLineWithNoChangesIsTheFlatLine(t *testing.T) {
	line, err := trajectory.NewTargetLine(30, decimal.NewFromInt(6000000), nil)
	if err != nil {
		t.Fatalf("NewTargetLine: %v", err)
	}
	if got := line.At(15).StringFixed(4); got != "3000000.0000" {
		t.Errorf("At(15) = %s, want 3000000.0000", got)
	}
	target, baseline := line.AtToday(15)
	if target.Cmp(big.NewRat(3000000, 1)) != 0 || baseline.Cmp(big.NewRat(200000, 1)) != 0 {
		t.Errorf("AtToday(15) = %s, %s; want 3000000 and 200000", target.RatString(), baseline.RatString())
	}
}
