package trajectory_test

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/trajectory"
)

// The trajectory calculation as pinned by contract/testdata/trajectory.json
// (BOOTSTRAP.md §11 step 8, issue #73). The suite keeps no copy of the
// numbers: every expected value below is decoded from the fixture.
type fixture struct {
	Parameters struct {
		WarmUpDays         int    `json:"warm_up_days"`
		DriftingBelow      string `json:"drifting_below"`
		TakingOnWaterBelow string `json:"taking_on_water_below"`
	} `json:"parameters"`
	Figures           []figureRow         `json:"figures"`
	TargetLine        []targetLine        `json:"target_line"`
	TargetLineChanges []targetLineChanges `json:"target_line_changes"`
}

type changeRow struct {
	EffectiveDay int    `json:"effective_day"`
	DailyPool    string `json:"daily_pool"`
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

		// A row whose Daily Pool changed in the Period: the pool the Period
		// began with and the changes since. `daily_pool` is then the pool in
		// force Today, which is what the dashboard passes as P.
		BaseDailyPool string      `json:"base_daily_pool"`
		Changes       []changeRow `json:"changes"`

		// Per-row overrides of the fixture-wide parameters. Pointers so a row
		// that doesn't mention a field falls back to the fixture default
		// rather than to Go's zero value.
		WarmUpDays         *int    `json:"warm_up_days"`
		DriftingBelow      *string `json:"drifting_below"`
		TakingOnWaterBelow *string `json:"taking_on_water_below"`
	} `json:"inputs"`
	Expected struct {
		// FullyCommitted: the baseline in force is zero, so there is no
		// State, baseline or variance; those keys are then absent.
		FullyCommitted      bool    `json:"fully_committed"`
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

type targetLineChanges struct {
	Name   string `json:"name"`
	Inputs struct {
		PeriodDays    int         `json:"period_days"`
		BaseDailyPool string      `json:"base_daily_pool"`
		Changes       []changeRow `json:"changes"`
	} `json:"inputs"`
	Expected struct {
		Points []struct {
			Day      int    `json:"day"`
			Target   string `json:"target"`
			Baseline string `json:"baseline"`
		} `json:"points"`
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
	if len(fx.TargetLineChanges) == 0 {
		t.Fatal("fixture `target_line_changes` is empty: a fixture that asserts nothing looks like coverage")
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

func poolChanges(t *testing.T, rows []changeRow) []trajectory.PoolChange {
	t.Helper()
	changes := make([]trajectory.PoolChange, 0, len(rows))
	for _, c := range rows {
		changes = append(changes, trajectory.PoolChange{EffectiveDay: c.EffectiveDay, DailyPool: mustDecimal(t, c.DailyPool)})
	}
	return changes
}

// lineFor builds a figures row's target line from its base pool and changes.
// A row with no changes has no line: Calculate reads a flat one itself.
func lineFor(t *testing.T, row figureRow) (trajectory.TargetLine, bool) {
	t.Helper()
	if len(row.Inputs.Changes) == 0 {
		return trajectory.TargetLine{}, false
	}
	if row.Inputs.BaseDailyPool == "" {
		t.Fatal("a row with `changes` needs its `base_daily_pool`")
	}
	line, err := trajectory.NewTargetLine(row.Inputs.PeriodDays, mustDecimal(t, row.Inputs.BaseDailyPool), poolChanges(t, row.Inputs.Changes))
	if err != nil {
		t.Fatalf("NewTargetLine: %v", err)
	}
	return line, true
}

// inputsFor builds Calculate's inputs the way the dashboard will: the Daily
// Pool in force Today as P, and T(e) and B_now from the line when there is one.
func inputsFor(t *testing.T, fx fixture, row figureRow) trajectory.Inputs {
	t.Helper()
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
	if line, ok := lineFor(t, row); ok {
		in.TargetThroughElapsed, in.BaselineInForce = line.AtToday(row.Inputs.DaysElapsed)
	}
	return in
}

// poolInForceToday is the pool a row's own changes put in force Today (day
// e+1): the last change that has started, else the base.
func poolInForceToday(t *testing.T, row figureRow) decimal.Decimal {
	t.Helper()
	pool := mustDecimal(t, row.Inputs.BaseDailyPool)
	for _, c := range row.Inputs.Changes {
		if c.EffectiveDay <= row.Inputs.DaysElapsed+1 {
			pool = mustDecimal(t, c.DailyPool)
		}
	}
	return pool
}

// TestFiguresMatchTheSharedFixture drives every `figures` row in
// contract/testdata/trajectory.json through Calculate and checks every
// output field, at the exact 4dp string the fixture holds — StringFixed(4)
// mirrors what the wire actually carries (BOOTSTRAP.md §4). A row with
// `changes` goes through a TargetLine first, as the dashboard will.
func TestFiguresMatchTheSharedFixture(t *testing.T) {
	fx := loadFixture(t)

	for _, row := range fx.Figures {
		t.Run(row.Name, func(t *testing.T) {
			if len(row.Inputs.Changes) > 0 {
				if want := poolInForceToday(t, row); !want.Equal(mustDecimal(t, row.Inputs.DailyPool)) {
					t.Fatalf("fixture row is inconsistent: daily_pool %s, but its changes put %s in force Today", row.Inputs.DailyPool, want)
				}
			}

			got, err := trajectory.Calculate(inputsFor(t, fx, row))
			if err != nil {
				t.Fatalf("Calculate: %v", err)
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

			if row.Expected.FullyCommitted {
				if got.Pace != nil {
					t.Errorf("pace = %+v, want none: fully committed has no State, baseline or variance", *got.Pace)
				}
				if row.Expected.State != "" || row.Expected.BaselineDailyBudget != "" || row.Expected.TrajectoryVariance != "" || row.Expected.BufferDays != "" {
					t.Error("a fully_committed row must not carry baseline, variance, buffer or state")
				}
				return
			}
			if got.Pace == nil {
				t.Fatal("pace = none, want one (the baseline in force is not zero)")
			}
			if row.Expected.State == "" || row.Expected.BaselineDailyBudget == "" {
				t.Fatal("a row with a pace must carry baseline_daily_budget and state: a fixture that asserts nothing looks like coverage")
			}
			if s := got.Pace.BaselineDailyBudget.StringFixed(4); s != row.Expected.BaselineDailyBudget {
				t.Errorf("baseline_daily_budget = %s, want %s", s, row.Expected.BaselineDailyBudget)
			}
			if s := got.Pace.TrajectoryVariance.StringFixed(4); s != row.Expected.TrajectoryVariance {
				t.Errorf("trajectory_variance = %s, want %s", s, row.Expected.TrajectoryVariance)
			}
			if s := got.Pace.BufferDays.StringFixed(4); s != row.Expected.BufferDays {
				t.Errorf("buffer_days = %s, want %s", s, row.Expected.BufferDays)
			}
			if string(got.Pace.State) != row.Expected.State {
				t.Errorf("state = %s, want %s", got.Pace.State, row.Expected.State)
			}
		})
	}
}

// TestTrajectoryVarianceIsRTimesTheGapBetweenAllowanceAndBaseline is the
// cross-check from #155 (R2): with nothing future-dated, every change in
// force and the last segment unclamped, the target line ends on P, so
// T(e) = P - r x B_now and V = r x (A_adaptive - B_now). It is computed here
// in exact rationals from the inputs alone, never from Calculate's own A, so a
// kink that drifted from the line's endpoint fails it.
func TestTrajectoryVarianceIsRTimesTheGapBetweenAllowanceAndBaseline(t *testing.T) {
	fx := loadFixture(t)

	checked, kinked := 0, 0
	for _, row := range fx.Figures {
		if row.Expected.FullyCommitted || mustDecimal(t, row.Inputs.FutureSpend).Sign() != 0 {
			continue
		}
		in := inputsFor(t, fx, row)
		if line, hasLine := lineFor(t, row); hasLine {
			inForce := true
			for _, c := range row.Inputs.Changes {
				inForce = inForce && c.EffectiveDay <= in.DaysElapsed+1
			}
			if !inForce || line.BaselineAfter().Sign() < 0 {
				continue
			}
			kinked++
		}

		t.Run(row.Name, func(t *testing.T) {
			got, err := trajectory.Calculate(in)
			if err != nil {
				t.Fatalf("Calculate: %v", err)
			}
			bNow := in.BaselineInForce
			if bNow == nil {
				bNow = new(big.Rat).Quo(in.DailyPool.Rat(), big.NewRat(int64(in.PeriodDays), 1))
			}
			r := big.NewRat(int64(in.PeriodDays-in.DaysElapsed), 1)
			adaptive := new(big.Rat).Quo(new(big.Rat).Sub(in.DailyPool.Rat(), in.SettledSpend.Rat()), r)
			want := new(big.Rat).Mul(r, new(big.Rat).Sub(adaptive, bNow))

			// Calculate rounds V half-up at 4dp, so it lies within half a unit
			// of the last place of the exact value.
			diff := new(big.Rat).Sub(got.Pace.TrajectoryVariance.Rat(), want)
			if diff.Abs(diff).Cmp(big.NewRat(1, 20000)) > 0 {
				t.Errorf("V = %s, want r x (A - B_now) = %s", got.Pace.TrajectoryVariance.StringFixed(4), want.FloatString(8))
			}
		})
		checked++
	}
	if checked < 10 {
		t.Fatalf("only %d rows qualified for the cross-check", checked)
	}
	if kinked < 15 {
		t.Fatalf("only %d rows with a target line qualified for the cross-check: the fixture has lost its kinked rows", kinked)
	}
}

// TestTargetLineMatchesTheSharedFixture drives every `target_line` row
// through the list shape (the pool before, then one change), covering
// CONTEXT.md's continuity example and its clamp rule. Those rows predate the
// list and are unchanged: they prove the list agrees with the single change.
func TestTargetLineMatchesTheSharedFixture(t *testing.T) {
	fx := loadFixture(t)

	for _, row := range fx.TargetLine {
		t.Run(row.Name, func(t *testing.T) {
			if len(row.Expected.Targets) == 0 {
				t.Fatal("target_line row has no `expected.targets`: a fixture that asserts nothing looks like coverage")
			}

			line, err := trajectory.NewTargetLine(row.Inputs.PeriodDays, mustDecimal(t, row.Inputs.DailyPoolBefore), []trajectory.PoolChange{
				{EffectiveDay: row.Inputs.EffectiveDay, DailyPool: mustDecimal(t, row.Inputs.DailyPoolAfter)},
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

// TestTargetLineChangesMatchTheSharedFixture drives the list shape natively:
// several changes, a negative base, the clamp followed by a raise, same-day and
// equal-neighbour merges. `baseline` is the slope in force that day, clamped.
func TestTargetLineChangesMatchTheSharedFixture(t *testing.T) {
	fx := loadFixture(t)

	for _, row := range fx.TargetLineChanges {
		t.Run(row.Name, func(t *testing.T) {
			if len(row.Expected.Points) == 0 {
				t.Fatal("target_line_changes row has no `expected.points`: a fixture that asserts nothing looks like coverage")
			}
			line, err := trajectory.NewTargetLine(row.Inputs.PeriodDays, mustDecimal(t, row.Inputs.BaseDailyPool), poolChanges(t, row.Inputs.Changes))
			if err != nil {
				t.Fatalf("NewTargetLine: %v", err)
			}
			for _, p := range row.Expected.Points {
				if s := line.At(p.Day).StringFixed(4); s != p.Target {
					t.Errorf("At(%d) = %s, want %s", p.Day, s, p.Target)
				}
				if s := line.BaselineAt(p.Day).StringFixed(4); s != p.Baseline {
					t.Errorf("BaselineAt(%d) = %s, want %s", p.Day, s, p.Baseline)
				}
			}
		})
	}
}

// TestEveryFixtureLineNeverFalls holds the clamp over every line the fixture
// builds, at every day, not only the days a row asserts: a target that dips,
// or a slope below zero, is the bug the clamp exists to prevent.
func TestEveryFixtureLineNeverFalls(t *testing.T) {
	fx := loadFixture(t)

	check := func(t *testing.T, line trajectory.TargetLine, days int) {
		t.Helper()
		prev := line.ThroughDay(0)
		if prev.Sign() != 0 {
			t.Errorf("T(0) = %s, want 0", prev.RatString())
		}
		for d := 1; d <= days; d++ {
			cur := line.ThroughDay(d)
			if cur.Cmp(prev) < 0 {
				t.Errorf("T(%d) = %s fell below T(%d) = %s", d, cur.RatString(), d-1, prev.RatString())
			}
			if line.BaselineInForce(d).Sign() < 0 {
				t.Errorf("slope on day %d is negative", d)
			}
			prev = cur
		}
	}

	for _, row := range fx.TargetLineChanges {
		t.Run(row.Name, func(t *testing.T) {
			line, err := trajectory.NewTargetLine(row.Inputs.PeriodDays, mustDecimal(t, row.Inputs.BaseDailyPool), poolChanges(t, row.Inputs.Changes))
			if err != nil {
				t.Fatalf("NewTargetLine: %v", err)
			}
			check(t, line, row.Inputs.PeriodDays)
		})
	}
	for _, row := range fx.Figures {
		if line, ok := lineFor(t, row); ok {
			t.Run(row.Name, func(t *testing.T) { check(t, line, row.Inputs.PeriodDays) })
		}
	}
}

// TestDefaultParametersMatchTheFixture pins the defaults a caller gets with no
// configuration to the fixture's `parameters` (#155 R1). Every other test
// overwrites Parameters from the fixture, so without this a default could
// drift from it unseen.
func TestDefaultParametersMatchTheFixture(t *testing.T) {
	fx := loadFixture(t)
	got := trajectory.DefaultParameters()

	if got.WarmUpDays != fx.Parameters.WarmUpDays {
		t.Errorf("WarmUpDays = %d, fixture warm_up_days = %d", got.WarmUpDays, fx.Parameters.WarmUpDays)
	}
	if want := mustDecimal(t, fx.Parameters.DriftingBelow); !got.DriftingBelow.Equal(want) {
		t.Errorf("DriftingBelow = %s, fixture drifting_below = %s", got.DriftingBelow, want)
	}
	if want := mustDecimal(t, fx.Parameters.TakingOnWaterBelow); !got.TakingOnWaterBelow.Equal(want) {
		t.Errorf("TakingOnWaterBelow = %s, fixture taking_on_water_below = %s", got.TakingOnWaterBelow, want)
	}
}
