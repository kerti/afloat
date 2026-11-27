package dashboard_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
)

// D5 through the real statements and the handler, on the 14th of November 2026
// (the fixture's clock). contract/testdata/dashboard.json pins the points row by
// row; these are the properties the wire promises beside it.

func TestTheChartIsAbsentWhileThereIsNoMonthlyBudgetWhateverWasSpent(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	f.expense(t, hh, wallet, pgtype.UUID{}, "2026-11-05", "10000")

	got := f.get(ctx, t)

	if got.Chart != nil {
		t.Errorf("chart = %+v, want none: nothing to read the spending against", got.Chart)
	}
}

// A Household with a Monthly Budget and no Expense still has its chart: the
// target line, and an actual of zero up to Today.
func TestWithAMonthlyBudgetAndNothingSpentTheChartHasATargetAndAZeroActual(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	f.tdb.CreatePocket(t, hh, "Wallet")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")

	got := f.get(ctx, t)

	if got.Chart == nil || len(got.Chart.Points) != 30 {
		t.Fatalf("chart = %+v, want 30 points", got.Chart)
	}
	rows := pointRows(got.Chart.Points)
	if rows[0] != "2026-11-01 100000.0000 0.0000" || rows[13] != "2026-11-14 1400000.0000 0.0000*true" || rows[14] != "2026-11-15 1500000.0000 -" {
		t.Errorf("points 1, 14 and 15 = %q, %q, %q", rows[0], rows[13], rows[14])
	}
}

// What counts and what does not, in one Household with another beside it that
// has the same days and bigger numbers: Today's Expense is on Today's point and
// moves the actual through the day, a future-dated Expense and a future-dated
// refund are on no point, a Commitment is on none, an Expense of another Period
// is on none, a deleted one is on none.
func TestTheChartCountsTheDailyBucketAndNothingElse(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Mine", "a@example.com")
	other, _ := f.member(t, "Theirs", "b@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	food := f.tdb.CreateCategory(t, hh, "Food", "daily")
	rent := f.tdb.CreateCategory(t, hh, "Rent", "commitment")
	f.monthlyBudget(t, hh, "2026-11-01", "6000000")
	f.allocate(t, hh, rent, "2026-11-01", "2000000") // a Daily Pool of 4.000.000, so 133.333,3333 a day

	f.expense(t, hh, wallet, food, "2026-11-13", "30000")
	f.expense(t, hh, wallet, pgtype.UUID{}, "2026-11-13", "12000") // uncategorised is Daily
	f.expense(t, hh, wallet, food, "2026-11-06", "-5000")          // a refund subtracts
	f.expense(t, hh, wallet, food, "2026-11-14", "15000")          // Today
	f.expense(t, hh, wallet, rent, "2026-11-07", "3000000")        // a Commitment: on no point
	f.expense(t, hh, wallet, food, "2026-11-15", "50000")          // future-dated: on no point before its date
	f.expense(t, hh, wallet, food, "2026-11-20", "-80000")         // a future refund lowers none
	f.expense(t, hh, wallet, food, "2026-10-31", "7000")           // before the Period
	f.expense(t, hh, wallet, food, "2026-12-01", "7000")           // after it
	f.exec(t, `INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id, category_id, deleted_at)
		VALUES (gen_random_uuid(), $1, '2026-11-08', 99000, 'IDR', $2, $3, now())`, hh, wallet, food) // deleted
	oWallet := f.tdb.CreatePocket(t, other, "Wallet")
	f.monthlyBudget(t, other, "2026-11-01", "60000000")
	f.expense(t, other, oWallet, pgtype.UUID{}, "2026-11-13", "99999")
	f.expense(t, other, oWallet, pgtype.UUID{}, "2026-11-14", "99999")

	got := f.get(ctx, t)
	rows := pointRows(got.Chart.Points)

	for day, want := range map[int]string{
		5:  "2026-11-05 666666.6667 0.0000",
		6:  "2026-11-06 800000.0000 -5000.0000",
		12: "2026-11-12 1600000.0000 -5000.0000",
		13: "2026-11-13 1733333.3333 37000.0000", // -5000 + 30000 + 12000
		14: "2026-11-14 1866666.6667 52000.0000*true",
		15: "2026-11-15 2000000.0000 -",
		20: "2026-11-20 2666666.6667 -",
		30: "2026-11-30 4000000.0000 -",
	} {
		if rows[day-1] != want {
			t.Errorf("point %d = %q, want %q", day, rows[day-1], want)
		}
	}
}

// The chart and the pace are read from one line and one fold, so they agree:
// the target on the day before Today, less what has settled, is the Trajectory
// Variance, to the digit, after the pool kinked twice (a raise, then a
// Commitment's allocation).
func TestTheChartAgreesWithTheTrajectoryVarianceAfterTheLineKinked(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	rent := f.tdb.CreateCategory(t, hh, "Rent", "commitment")
	f.monthlyBudget(t, hh, "2026-11-01", "6000000")
	f.monthlyBudget(t, hh, "2026-11-11", "7500000")
	f.allocate(t, hh, rent, "2026-11-01", "2000000")
	f.allocate(t, hh, rent, "2026-11-12", "2500000")
	f.expense(t, hh, wallet, pgtype.UUID{}, "2026-11-03", "123456.7891")
	f.expense(t, hh, wallet, pgtype.UUID{}, "2026-11-13", "42000")
	f.expense(t, hh, wallet, pgtype.UUID{}, "2026-11-14", "9000")

	got := f.get(ctx, t)

	if got.Trajectory == nil || got.Trajectory.TrajectoryVariance == nil {
		t.Fatalf("trajectory = %+v, want a variance", got.Trajectory)
	}
	var yesterday, today api.ChartPoint
	for _, p := range got.Chart.Points {
		switch p.Date.Format("2006-01-02") {
		case "2026-11-13":
			yesterday = p
		case "2026-11-14":
			today = p
		}
	}
	// The settled spend is the actual on yesterday's point; the target there is T(e).
	settled, target := mustDecimal(t, *yesterday.Actual), mustDecimal(t, yesterday.Target)
	if g := target.Sub(settled).StringFixed(4); g != *got.Trajectory.TrajectoryVariance {
		t.Errorf("T(e) - settled = %s, the answer's trajectory_variance = %s", g, *got.Trajectory.TrajectoryVariance)
	}
	if today.InProgress == nil || !*today.InProgress {
		t.Errorf("Today's point = %+v, want it in progress", today)
	}
	if g, want := mustDecimal(t, *today.Actual).Sub(settled).StringFixed(4), "9000.0000"; g != want {
		t.Errorf("Today's actual less what settled = %s, want %s (Today's spend so far)", g, want)
	}
}

// The wire carries money as strings and `in_progress` only where it is true:
// no JSON number, no `false`, no `null`.
func TestTheChartOnTheWireIsStringsAndOneTrue(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	f.tdb.CreatePocket(t, hh, "Wallet")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")

	raw, err := json.Marshal(f.get(ctx, t).Chart)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Points []map[string]any `json:"points"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	inProgress := 0
	for _, p := range decoded.Points {
		for _, k := range []string{"target", "actual"} {
			if v, ok := p[k]; ok {
				if s, isString := v.(string); !isString || !strings.Contains(s, ".") {
					t.Errorf("%s = %#v on %v, want a four-decimal string", k, v, p["date"])
				}
			}
		}
		if v, ok := p["in_progress"]; ok {
			inProgress++
			if v != true {
				t.Errorf("in_progress = %#v on %v, want true or absent", v, p["date"])
			}
		}
	}
	if inProgress != 1 {
		t.Errorf("%d points in progress, want exactly Today's", inProgress)
	}
}

func mustDecimal(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
