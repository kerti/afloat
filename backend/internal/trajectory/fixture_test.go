package trajectory_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/trajectory"
)

// The trajectory calculation both backends must implement alike
// (contract/testdata/trajectory.json, BOOTSTRAP.md §11 step 8, issue #73).
// Kotlin's equivalent spec reads the same file. Neither suite keeps a copy of
// the numbers: every expected value below is decoded from the fixture.
type fixture struct {
	Parameters struct {
		WarmUpDays         int    `json:"warm_up_days"`
		DriftingBelow      string `json:"drifting_below"`
		TakingOnWaterBelow string `json:"taking_on_water_below"`
	} `json:"parameters"`
	Figures    []figureRow  `json:"figures"`
	TargetLine []targetLine `json:"target_line"`
}

type figureRow struct {
	Name   string `json:"name"`
	Inputs struct {
		DailyPool    string `json:"daily_pool"`
		PeriodDays   int    `json:"period_days"`
		DaysElapsed  int    `json:"days_elapsed"`
		SettledSpend string `json:"settled_spend"`
		TodaySpend   string `json:"today_spend"`
		FutureSpend  string `json:"future_spend"`
		Mode         string `json:"mode"`

		// Per-row overrides of the fixture-wide parameters. Pointers so a row
		// that doesn't mention a field falls back to the fixture default
		// rather than to Go's zero value.
		WarmUpDays         *int    `json:"warm_up_days"`
		DriftingBelow      *string `json:"drifting_below"`
		TakingOnWaterBelow *string `json:"taking_on_water_below"`
	} `json:"inputs"`
	Expected struct {
		BaselineDailyBudget string  `json:"baseline_daily_budget"`
		AvailablePool       string  `json:"available_pool"`
		TodaysAllowance     string  `json:"todays_allowance"`
		LeftToSpendToday    string  `json:"left_to_spend_today"`
		ForwardDailyRate    *string `json:"forward_daily_rate"`
		TrajectoryVariance  string  `json:"trajectory_variance"`
		BufferDays          string  `json:"buffer_days"`
		State               string  `json:"state"`
	} `json:"expected"`
}

type targetLine struct {
	Name   string `json:"name"`
	Inputs struct {
		PeriodDays      int    `json:"period_days"`
		DailyPoolBefore string `json:"daily_pool_before"`
		DailyPoolAfter  string `json:"daily_pool_after"`
		EffectiveDay    int    `json:"effective_day"`
	} `json:"inputs"`
	Expected struct {
		Targets []struct {
			Day    int    `json:"day"`
			Target string `json:"target"`
		} `json:"targets"`
		BaselineAfter string `json:"baseline_after"`
	} `json:"expected"`
}

// loadFixture fails the test outright on anything that would let a fixture
// section pass silently empty or unread — a fixture that asserts nothing
// looks like coverage.
func loadFixture(t *testing.T) fixture {
	t.Helper()
	raw, err := os.ReadFile("../../../contract/testdata/trajectory.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(fx.Figures) == 0 {
		t.Fatal("fixture `figures` is empty: a fixture that asserts nothing looks like coverage")
	}
	if len(fx.TargetLine) == 0 {
		t.Fatal("fixture `target_line` is empty: a fixture that asserts nothing looks like coverage")
	}
	if fx.Parameters.WarmUpDays == 0 {
		t.Fatal("fixture `parameters.warm_up_days` is missing or zero")
	}
	return fx
}

func mustDecimal(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("parse decimal %q: %v", s, err)
	}
	return d
}

func parametersFor(t *testing.T, fx fixture, row figureRow) trajectory.Parameters {
	t.Helper()
	params := trajectory.DefaultParameters()
	params.WarmUpDays = fx.Parameters.WarmUpDays
	params.DriftingBelow = mustDecimal(t, fx.Parameters.DriftingBelow)
	params.TakingOnWaterBelow = mustDecimal(t, fx.Parameters.TakingOnWaterBelow)

	if row.Inputs.WarmUpDays != nil {
		params.WarmUpDays = *row.Inputs.WarmUpDays
	}
	if row.Inputs.DriftingBelow != nil {
		params.DriftingBelow = mustDecimal(t, *row.Inputs.DriftingBelow)
	}
	if row.Inputs.TakingOnWaterBelow != nil {
		params.TakingOnWaterBelow = mustDecimal(t, *row.Inputs.TakingOnWaterBelow)
	}
	return params
}

// TestFiguresMatchTheSharedFixture drives every `figures` row in
// contract/testdata/trajectory.json through Calculate and checks every
// output field, at the exact 4dp string the fixture holds — StringFixed(4)
// mirrors what the wire actually carries (BOOTSTRAP.md §4).
func TestFiguresMatchTheSharedFixture(t *testing.T) {
	fx := loadFixture(t)

	for _, row := range fx.Figures {
		t.Run(row.Name, func(t *testing.T) {
			in := trajectory.Inputs{
				DailyPool:    mustDecimal(t, row.Inputs.DailyPool),
				PeriodDays:   row.Inputs.PeriodDays,
				DaysElapsed:  row.Inputs.DaysElapsed,
				SettledSpend: mustDecimal(t, row.Inputs.SettledSpend),
				TodaySpend:   mustDecimal(t, row.Inputs.TodaySpend),
				FutureSpend:  mustDecimal(t, row.Inputs.FutureSpend),
				Mode:         trajectory.Mode(row.Inputs.Mode),
				Parameters:   parametersFor(t, fx, row),
			}

			got, err := trajectory.Calculate(in)
			if err != nil {
				t.Fatalf("Calculate: %v", err)
			}

			if s := got.BaselineDailyBudget.StringFixed(4); s != row.Expected.BaselineDailyBudget {
				t.Errorf("baseline_daily_budget = %s, want %s", s, row.Expected.BaselineDailyBudget)
			}
			if s := got.AvailablePool.StringFixed(4); s != row.Expected.AvailablePool {
				t.Errorf("available_pool = %s, want %s", s, row.Expected.AvailablePool)
			}
			if s := got.TodaysAllowance.StringFixed(4); s != row.Expected.TodaysAllowance {
				t.Errorf("todays_allowance = %s, want %s", s, row.Expected.TodaysAllowance)
			}
			if s := got.LeftToSpendToday.StringFixed(4); s != row.Expected.LeftToSpendToday {
				t.Errorf("left_to_spend_today = %s, want %s", s, row.Expected.LeftToSpendToday)
			}
			switch {
			case row.Expected.ForwardDailyRate == nil:
				if got.ForwardDailyRate != nil {
					t.Errorf("forward_daily_rate = %s, want null (r = 1)", got.ForwardDailyRate.StringFixed(4))
				}
			case got.ForwardDailyRate == nil:
				t.Errorf("forward_daily_rate = null, want %s", *row.Expected.ForwardDailyRate)
			default:
				if s := got.ForwardDailyRate.StringFixed(4); s != *row.Expected.ForwardDailyRate {
					t.Errorf("forward_daily_rate = %s, want %s", s, *row.Expected.ForwardDailyRate)
				}
			}
			if s := got.TrajectoryVariance.StringFixed(4); s != row.Expected.TrajectoryVariance {
				t.Errorf("trajectory_variance = %s, want %s", s, row.Expected.TrajectoryVariance)
			}
			if s := got.BufferDays.StringFixed(4); s != row.Expected.BufferDays {
				t.Errorf("buffer_days = %s, want %s", s, row.Expected.BufferDays)
			}
			if string(got.State) != row.Expected.State {
				t.Errorf("state = %s, want %s", got.State, row.Expected.State)
			}
		})
	}
}

// TestTargetLineMatchesTheSharedFixture drives every `target_line` row
// through NewTargetLine and At, covering CONTEXT.md's continuity example and
// its clamp rule.
func TestTargetLineMatchesTheSharedFixture(t *testing.T) {
	fx := loadFixture(t)

	for _, row := range fx.TargetLine {
		t.Run(row.Name, func(t *testing.T) {
			if len(row.Expected.Targets) == 0 {
				t.Fatal("target_line row has no `expected.targets`: a fixture that asserts nothing looks like coverage")
			}

			line, err := trajectory.NewTargetLine(trajectory.TargetLineInputs{
				PeriodDays:      row.Inputs.PeriodDays,
				DailyPoolBefore: mustDecimal(t, row.Inputs.DailyPoolBefore),
				DailyPoolAfter:  mustDecimal(t, row.Inputs.DailyPoolAfter),
				EffectiveDay:    row.Inputs.EffectiveDay,
			})
			if err != nil {
				t.Fatalf("NewTargetLine: %v", err)
			}

			if s := line.BaselineAfter().StringFixed(4); s != row.Expected.BaselineAfter {
				t.Errorf("baseline_after = %s, want %s", s, row.Expected.BaselineAfter)
			}
			for _, target := range row.Expected.Targets {
				if s := line.At(target.Day).StringFixed(4); s != target.Target {
					t.Errorf("At(%d) = %s, want %s", target.Day, s, target.Target)
				}
			}
		})
	}
}
