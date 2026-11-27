package dashboard_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/dashboard"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
	"github.com/kerti/afloat/backend/internal/trajectory"
)

// The chart's points on a line and a fold built by hand: no database, so every
// expected number is the one worked out on paper below it.

func lineOf(t *testing.T, days int, base string, changes ...trajectory.PoolChange) trajectory.TargetLine {
	t.Helper()
	line, err := trajectory.NewTargetLine(days, decimal.RequireFromString(base), changes)
	if err != nil {
		t.Fatalf("NewTargetLine: %v", err)
	}
	return line
}

func byDay(t *testing.T, pairs ...string) map[period.Date]decimal.Decimal {
	t.Helper()
	out := map[period.Date]decimal.Decimal{}
	for i := 0; i < len(pairs); i += 2 {
		d, err := period.ParseDate(pairs[i])
		if err != nil {
			t.Fatal(err)
		}
		out[d] = decimal.RequireFromString(pairs[i+1])
	}
	return out
}

// pointRows reads the points as a test writes them: "date target actual" per
// point, "-" for an absent actual and "*" on the one in progress.
func pointRows(points []api.ChartPoint) []string {
	var out []string
	for _, p := range points {
		actual, mark := "-", ""
		if p.Actual != nil {
			actual = *p.Actual
		}
		if p.InProgress != nil {
			mark = fmt.Sprintf("*%v", *p.InProgress)
		}
		out = append(out, fmt.Sprintf("%s %s %s%s", p.Date.Format("2006-01-02"), p.Target, actual, mark))
	}
	return out
}

func chartOf(t *testing.T, today string, line trajectory.TargetLine, spend map[period.Date]decimal.Decimal) []api.ChartPoint {
	t.Helper()
	at := positionOf(t, today)
	return dashboard.ChartPoints(line, at.Period, at.Today, spend)
}

func TestTheChartHasOnePointForEveryDayOfThePeriodInOrder(t *testing.T) {
	for _, today := range []string{"2026-11-01", "2026-11-15", "2026-11-30"} {
		points := chartOf(t, today, lineOf(t, 30, "3000000"), nil)
		if len(points) != 30 {
			t.Fatalf("Today %s: %d points, want the Period's 30 days", today, len(points))
		}
		if first, last := points[0].Date.Format("2006-01-02"), points[29].Date.Format("2006-01-02"); first != "2026-11-01" || last != "2026-11-30" {
			t.Errorf("Today %s: runs %s to %s, want the Period's first day to its last", today, first, last)
		}
		for i := 1; i < len(points); i++ {
			if !points[i].Date.After(points[i-1].Date.Time) {
				t.Fatalf("Today %s: point %d is not after point %d", today, i, i-1)
			}
		}
	}
}

// Day j's target is 100.000 x j, read at the end of the day, so the first point
// is one day's worth. The actual is what has been spent through the day, a
// refund subtracting, and Today's point is the one in progress; nothing after
// it has an actual, whatever is dated there.
func TestTheActualIsCumulativeThroughTodayAndAbsentAfterIt(t *testing.T) {
	spend := byDay(t,
		"2026-11-01", "50000",
		"2026-11-02", "-20000", // a refund subtracts
		"2026-11-04", "30000", // Today
		"2026-11-06", "99999", // future-dated: on no point before its date
		"2026-11-09", "-500000", // a future refund lowers nothing
	)
	got := pointRows(chartOf(t, "2026-11-04", lineOf(t, 30, "3000000"), spend))

	want := []string{
		"2026-11-01 100000.0000 50000.0000",
		"2026-11-02 200000.0000 30000.0000",
		"2026-11-03 300000.0000 30000.0000", // a day with nothing carries the total
		"2026-11-04 400000.0000 60000.0000*true",
		"2026-11-05 500000.0000 -",
		"2026-11-06 600000.0000 -",
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("point %d = %q, want %q", i+1, got[i], w)
		}
	}
	if last := got[29]; last != "2026-11-30 3000000.0000 -" {
		t.Errorf("last point = %q: the target reaches the Daily Pool on the Period's last day and the actual is absent", last)
	}
	var marked, withActual int
	for _, p := range chartOf(t, "2026-11-04", lineOf(t, 30, "3000000"), spend) {
		if p.InProgress != nil {
			marked++
		}
		if p.Actual != nil {
			withActual++
		}
	}
	if marked != 1 || withActual != 4 {
		t.Errorf("%d points in progress and %d with an actual, want 1 and 4 (days 1 to Today)", marked, withActual)
	}
}

// Day one and the last day: Today is the first or the last point.
func TestTodayOnTheFirstAndOnTheLastDayIsTheOnePointInProgress(t *testing.T) {
	first := chartOf(t, "2026-11-01", lineOf(t, 30, "3000000"), byDay(t, "2026-11-01", "7", "2026-11-02", "9"))
	if g := pointRows(first)[0]; g != "2026-11-01 100000.0000 7.0000*true" {
		t.Errorf("day one = %q", g)
	}
	if first[1].Actual != nil || first[1].InProgress != nil {
		t.Errorf("day two = %+v, want a target and nothing else: its Expense is dated in the future", first[1])
	}
	last := chartOf(t, "2026-11-30", lineOf(t, 30, "3000000"), byDay(t, "2026-11-01", "7", "2026-11-30", "9"))
	if g := pointRows(last)[29]; g != "2026-11-30 3000000.0000 16.0000*true" {
		t.Errorf("last day = %q", g)
	}
	for i, p := range last[:29] {
		if p.InProgress != nil {
			t.Errorf("point %d is in progress, only Today's is", i+1)
		}
	}
}

// The target is the line's own value, half-up at four decimals from the exact
// rational and never accumulated from rounded steps: 1.000.000 / 30 is
// 33333.3333... a day, so 66666.666... reads ...6667 and 100.000 is exact.
func TestTheTargetIsTheExactLineRoundedHalfUpAtFourDecimals(t *testing.T) {
	got := pointRows(chartOf(t, "2026-11-01", lineOf(t, 30, "1000000"), nil))
	for i, want := range map[int]string{0: "33333.3333", 1: "66666.6667", 2: "100000.0000", 3: "133333.3333", 29: "1000000.0000"} {
		if g := strings.Fields(got[i])[1]; g != want {
			t.Errorf("point %d target = %s, want %s", i+1, g, want)
		}
	}
}

// 3.000.000 a Period is 100.000 a day; cut on day 11 to 1.500.000, the line has
// reached 1.000.000 and runs (1.500.000 - 1.000.000) / 20 = 25.000 a day; cut
// on day 21 to 500.000 it has reached 1.250.000, is already past the pool, and
// stays flat there to the end: the slope never goes below zero.
func TestTheTargetKinksAtAChangeAndHoldsFlatWhenACutFallsBelowIt(t *testing.T) {
	line := lineOf(t, 30, "3000000",
		trajectory.PoolChange{EffectiveDay: 11, DailyPool: decimal.RequireFromString("1500000")},
		trajectory.PoolChange{EffectiveDay: 21, DailyPool: decimal.RequireFromString("500000")},
	)
	got := pointRows(chartOf(t, "2026-11-30", line, nil))
	targets := map[int]string{
		1: "100000.0000", 10: "1000000.0000", // the old slope, to the day before the change
		11: "1025000.0000", 20: "1250000.0000", // the new one
		21: "1250000.0000", 30: "1250000.0000", // flat: the cut is below what was reached
	}
	for day, want := range targets {
		if g := strings.Fields(got[day-1])[1]; g != want {
			t.Errorf("day %d target = %s, want %s", day, g, want)
		}
	}
	prev := decimal.Zero
	for _, p := range chartOf(t, "2026-11-30", line, nil) {
		cur := decimal.RequireFromString(p.Target)
		if cur.LessThan(prev) {
			t.Fatalf("the target falls from %s to %s on %s", prev, cur, p.Date.Format("2006-01-02"))
		}
		prev = cur
	}
}

// Fully committed: a Daily Pool at or below zero is a flat target at zero. The
// chart is still drawn, and the actual still counts.
func TestAFullyCommittedPeriodHasAFlatTargetAtZeroAndStillHasItsActual(t *testing.T) {
	got := pointRows(chartOf(t, "2026-11-03", lineOf(t, 30, "-1000000"), byDay(t, "2026-11-02", "40000")))
	for i, want := range []string{
		"2026-11-01 0.0000 0.0000",
		"2026-11-02 0.0000 40000.0000",
		"2026-11-03 0.0000 40000.0000*true",
		"2026-11-04 0.0000 -",
	} {
		if got[i] != want {
			t.Errorf("point %d = %q, want %q", i+1, got[i], want)
		}
	}
	if last := got[29]; last != "2026-11-30 0.0000 -" {
		t.Errorf("last point = %q, want the line flat at zero to the end", last)
	}
}

// A Commitment Category's Expense is in no daily figure, so it is on no point;
// an uncategorised one is Daily and is. The rows go through the fold the
// dashboard uses, which is what the chart's spend is.
func TestACommitmentsSpendIsOnNoPointAndAnUncategorisedOneIs(t *testing.T) {
	at := positionOf(t, "2026-11-10")
	food, rent, wallet := uid(1), uid(2), uid(10)
	spend, err := dashboard.FoldSpend([]db.PeriodSpendRow{
		spendRow(t, "2026-11-03", food, "Food", true, "daily", wallet, "Wallet", "1000"),
		spendRow(t, "2026-11-03", rent, "Rent", true, "commitment", wallet, "Wallet", "3000000"),
		spendRow(t, "2026-11-04", pgtype.UUID{}, "", true, "daily", wallet, "Wallet", "250"),
	}, at)
	if err != nil {
		t.Fatalf("FoldSpend: %v", err)
	}
	got := pointRows(dashboard.ChartPoints(lineOf(t, 30, "3000000"), at.Period, at.Today, spend.ByDay))
	if got[2] != "2026-11-03 300000.0000 1000.0000" || got[3] != "2026-11-04 400000.0000 1250.0000" {
		t.Errorf("points 3 and 4 = %q, %q: the rent is in neither and the uncategorised 250 is in the second", got[2], got[3])
	}
}

// A Period of another length is that many points: 31 and a leap February.
func TestThePointsFollowThePeriodsLength(t *testing.T) {
	for _, tc := range []struct {
		day  string
		want int
	}{{"2026-12-05", 31}, {"2028-02-10", 29}, {"2027-02-10", 28}} {
		at := positionOf(t, tc.day)
		points := dashboard.ChartPoints(lineOf(t, at.Period.Days(), "1000000"), at.Period, at.Today, nil)
		if len(points) != tc.want {
			t.Errorf("%s: %d points, want %d", tc.day, len(points), tc.want)
		}
	}
}
