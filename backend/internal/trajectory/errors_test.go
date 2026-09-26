package trajectory_test

import (
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

func TestCalculateRejectsAZeroDailyPool(t *testing.T) {
	// B = 0/D = 0 makes Buffer Days (V / B) undefined.
	_, err := trajectory.Calculate(trajectory.Inputs{
		DailyPool:  decimal.Zero,
		PeriodDays: 10,
		Mode:       trajectory.ModeAdaptive,
		Parameters: trajectory.DefaultParameters(),
	})
	if err == nil {
		t.Fatal("want an error for a zero daily pool")
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

func TestNewTargetLineRejectsNonPositivePeriodDays(t *testing.T) {
	_, err := trajectory.NewTargetLine(trajectory.TargetLineInputs{
		PeriodDays:      0,
		DailyPoolBefore: decimal.NewFromInt(100),
		DailyPoolAfter:  decimal.NewFromInt(200),
		EffectiveDay:    1,
	})
	if err == nil {
		t.Fatal("want an error for period_days = 0")
	}
}

func TestNewTargetLineRejectsAnEffectiveDayOutOfRange(t *testing.T) {
	for _, day := range []int{0, -1, 31} {
		_, err := trajectory.NewTargetLine(trajectory.TargetLineInputs{
			PeriodDays:      30,
			DailyPoolBefore: decimal.NewFromInt(100),
			DailyPoolAfter:  decimal.NewFromInt(200),
			EffectiveDay:    day,
		})
		if err == nil {
			t.Fatalf("want an error for effective_day = %d in a 30-day period", day)
		}
	}
}
