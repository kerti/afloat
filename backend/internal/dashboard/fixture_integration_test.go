package dashboard_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/dashboard"
	"github.com/kerti/afloat/backend/internal/trajectory"
)

// contract/testdata/dashboard.json, read row by row through the real
// statements: where Today and the Period land for an instant, what PeriodSpend
// returns and what the daily bucket folds into, the Daily Pool through the
// Period and the figures made of it, and what the wire carries at this stage of
// #155 (the breakdowns' rows in the fixture's terms: a key for an id). Every Household of the file is in the database for every
// row, so another Household's rows are present throughout (tenancy).
func TestTheFixtureRowsAreAnsweredAsPinned(t *testing.T) {
	f := newFixture(t)
	households := f.seed(t, loadFixture(t))

	for _, h := range households {
		for _, row := range h.spec.Reads {
			t.Run(h.spec.Name+"/"+row.Name, func(t *testing.T) {
				f.clock.set(t, row.Now)

				at, _, err := budget.LocateHousehold(h.ctx, f.tdb.Queries, f.clock.now, mustUser(t, h))
				if err != nil {
					t.Fatalf("LocateHousehold: %v", err)
				}
				want := row.Expected
				if at.Today.String() != want.Today {
					t.Errorf("Today = %s, want %s", at.Today, want.Today)
				}
				if at.Period.YearMonth != want.Period.YearMonth || at.Period.StartsOn.String() != want.Period.StartsOn || at.Period.EndsOn.String() != want.Period.EndsOn {
					t.Errorf("Period = %s %s..%s, want %s %s..%s", at.Period.YearMonth, at.Period.StartsOn, at.Period.EndsOn,
						want.Period.YearMonth, want.Period.StartsOn, want.Period.EndsOn)
				}

				spend, err := dashboard.LoadSpend(h.ctx, f.tdb.Queries, h.householdID, at)
				if err != nil {
					t.Fatalf("LoadSpend: %v", err)
				}
				gotByDay := map[string]string{}
				for day, amount := range spend.ByDay {
					gotByDay[day.String()] = amount.StringFixed(4)
				}
				if !maps.Equal(gotByDay, want.Spend.ByDay) {
					t.Errorf("daily spend by day = %v\nwant %v", gotByDay, want.Spend.ByDay)
				}
				if got := spend.Settled.StringFixed(4); got != want.Spend.Settled {
					t.Errorf("S_settled = %s, want %s", got, want.Spend.Settled)
				}
				if got := spend.Today.StringFixed(4); got != want.Spend.Today {
					t.Errorf("S_today = %s, want %s", got, want.Spend.Today)
				}
				if got := spend.Future.StringFixed(4); got != want.Spend.Future {
					t.Errorf("S_future = %s, want %s", got, want.Spend.Future)
				}

				// The Period is brought into being first, as a request does: a
				// Period nobody has read carries the previous one's Budget forward
				// when it is first read.
				at, household, err := budget.LocateHousehold(h.ctx, f.tdb.Queries, f.clock.now, mustUser(t, h))
				if err != nil {
					t.Fatalf("LocateHousehold: %v", err)
				}
				if err := budget.EnsurePeriod(h.ctx, f.tdb.Queries, f.tdb.Pool, h.householdID, at.Period); err != nil {
					t.Fatalf("EnsurePeriod: %v", err)
				}
				assembly, ok, err := dashboard.Assemble(h.ctx, f.tdb.Queries, h.householdID, at, trajectory.Mode(household.AllowanceMode))
				if err != nil {
					t.Fatalf("Assemble: %v", err)
				}
				if wantTrajectory := want.Wire["trajectory"] != nil; ok != wantTrajectory {
					t.Fatalf("the Household has a trajectory = %v, want %v", ok, wantTrajectory)
				}
				if ok {
					checkPool(t, assembly, want)
					checkPace(t, assembly, want)
					checkAgainstGetBudget(t, f, h, assembly)
				}

				// The wire, as it is at this stage.
				resp, err := f.handlers.GetDashboard(h.ctx, api.GetDashboardRequestObject{})
				if err != nil {
					t.Fatalf("GetDashboard: %v", err)
				}
				rec := httptest.NewRecorder()
				if err := resp.VisitGetDashboardResponse(rec); err != nil || rec.Code != 200 {
					t.Fatalf("GetDashboard = %d %v, want 200", rec.Code, err)
				}
				var gotWire map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &gotWire); err != nil {
					t.Fatalf("decode the answer: %v", err)
				}
				h.inFixtureTerms(gotWire)
				if !reflect.DeepEqual(gotWire, want.Wire) {
					gotJSON, _ := json.MarshalIndent(gotWire, "", " ")
					wantJSON, _ := json.MarshalIndent(want.Wire, "", " ")
					t.Errorf("answer = %s\nwant %s", gotJSON, wantJSON)
				}
			})
		}
	}
}

// Every fixture Household's rows are summed in its own figure and no other's:
// the sum of a Household's own Expenses in the Period is what the fold says,
// with the other's rows in the same table on the same days.
func TestAnotherHouseholdsExpensesAreInNoFigureOfThisOne(t *testing.T) {
	f := newFixture(t)
	households := f.seed(t, loadFixture(t))
	f.clock.set(t, "2026-11-26T10:00:00+07:00") // inside both Households' Periods

	for _, h := range households {
		at, _, err := budget.LocateHousehold(h.ctx, f.tdb.Queries, f.clock.now, mustUser(t, h))
		if err != nil {
			t.Fatalf("LocateHousehold: %v", err)
		}
		spend, err := dashboard.LoadSpend(h.ctx, f.tdb.Queries, h.householdID, at)
		if err != nil {
			t.Fatalf("LoadSpend: %v", err)
		}
		// Added up in SQL, in this Household alone, over the same window and
		// the same bucket.
		want := f.scan(t, `
			SELECT coalesce(sum(e.amount), 0)::numeric(20,4)::text
			FROM expenses e LEFT JOIN categories c ON c.id = e.category_id
			WHERE e.household_id = $1 AND e.deleted_at IS NULL
			  AND e.occurred_on BETWEEN $2::date AND $3::date
			  AND coalesce(c.bucket, 'daily') = 'daily'`,
			h.householdID, at.Period.StartsOn.String(), at.Period.EndsOn.String())
		got := spend.Settled.Add(spend.Today).Add(spend.Future).StringFixed(4)
		if got != want {
			t.Errorf("%s: the Period's daily spend = %s, want %s from its own Expenses alone", h.spec.Name, got, want)
		}
	}
}

func TestEveryHouseholdSeesItsOwnCurrencyAndNeverAnothers(t *testing.T) {
	f := newFixture(t)
	households := f.seed(t, loadFixture(t))
	f.clock.set(t, "2026-11-26T10:00:00+07:00")

	for _, h := range households {
		if got := f.get(h.ctx, t).Currency; got != h.spec.ReportingCurrency {
			t.Errorf("%s: currency = %s, want %s", h.spec.Name, got, h.spec.ReportingCurrency)
		}
	}
}

// checkPool pins the Daily Pool the target line is built from: the pool in force
// Today, the pool the Period's first day is read as of, and the days it really
// changed on.
func checkPool(t *testing.T, got dashboard.Assembly, want fixtureExpected) {
	t.Helper()
	if want.Pool == nil {
		t.Fatal("the row has a trajectory and no pool to pin")
	}
	if g := got.DailyPool.StringFixed(4); g != want.Pool.Today {
		t.Errorf("the Daily Pool today = %s, want %s", g, want.Pool.Today)
	}
	if g := got.BasePool.StringFixed(4); g != want.Pool.Base {
		t.Errorf("the base Daily Pool = %s, want %s", g, want.Pool.Base)
	}
	var gotChanges []string
	for _, c := range got.Changes {
		gotChanges = append(gotChanges, fmt.Sprintf("day %d: %s", c.EffectiveDay, c.DailyPool.StringFixed(4)))
	}
	var wantChanges []string
	for _, c := range want.Pool.Changes {
		wantChanges = append(wantChanges, fmt.Sprintf("day %d: %s", c.Day, c.Pool))
	}
	if !slices.Equal(gotChanges, wantChanges) {
		t.Errorf("the pool's changes = %v, want %v", gotChanges, wantChanges)
	}
}

// checkPace pins what the calculation reads against the line, Buffer Days
// included, which the wire never carries.
func checkPace(t *testing.T, got dashboard.Assembly, want fixtureExpected) {
	t.Helper()
	pace := got.Figures.Pace
	switch {
	case want.Pace == nil && pace != nil:
		t.Errorf("pace = %+v, want none: the Household is fully committed", *pace)
	case want.Pace != nil && pace == nil:
		t.Errorf("no pace, want %+v", *want.Pace)
	case want.Pace != nil:
		if string(pace.State) != want.Pace.State {
			t.Errorf("State = %s, want %s", pace.State, want.Pace.State)
		}
		if g := pace.TrajectoryVariance.StringFixed(4); g != want.Pace.TrajectoryVariance {
			t.Errorf("Trajectory Variance = %s, want %s", g, want.Pace.TrajectoryVariance)
		}
		if g := pace.BufferDays.StringFixed(4); g != want.Pace.BufferDays {
			t.Errorf("Buffer Days = %s, want %s", g, want.Pace.BufferDays)
		}
	}
}

// The dashboard's pool is GET /budget's daily_pool, asserted on one state: the
// plan screen and Home can never state two Daily Pools for one Household.
func checkAgainstGetBudget(t *testing.T, f fixture, h seeded, got dashboard.Assembly) {
	t.Helper()
	budgets := budget.New(budget.Deps{Querier: f.tdb.Queries, Beginner: f.tdb.Pool, Now: f.clock.now})
	resp, err := budgets.GetBudget(h.ctx, api.GetBudgetRequestObject{})
	if err != nil {
		t.Fatalf("GetBudget: %v", err)
	}
	b, ok := resp.(api.GetBudget200JSONResponse)
	if !ok || b.DailyPool == nil {
		t.Fatalf("GetBudget = %#v, want 200 with a daily_pool", resp)
	}
	if *b.DailyPool != got.DailyPool.StringFixed(4) {
		t.Errorf("GET /budget's daily_pool = %s, the dashboard's pool = %s", *b.DailyPool, got.DailyPool.StringFixed(4))
	}
}
