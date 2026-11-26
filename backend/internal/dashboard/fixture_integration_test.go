package dashboard_test

import (
	"encoding/json"
	"maps"
	"net/http/httptest"
	"testing"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/dashboard"
)

// contract/testdata/dashboard.json, read row by row through the real
// statements: where Today and the Period land for an instant, what PeriodSpend
// returns and what the daily bucket folds into, and what the wire carries at
// this stage of #155. Every Household of the file is in the database for every
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

				// The wire, as it is at this stage: the currency and nothing else.
				resp, err := f.handlers.GetDashboard(h.ctx, api.GetDashboardRequestObject{})
				if err != nil {
					t.Fatalf("GetDashboard: %v", err)
				}
				rec := httptest.NewRecorder()
				if err := resp.VisitGetDashboardResponse(rec); err != nil || rec.Code != 200 {
					t.Fatalf("GetDashboard = %d %v, want 200", rec.Code, err)
				}
				var wire map[string]json.RawMessage
				if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
					t.Fatalf("decode the answer: %v", err)
				}
				wantWire, _ := json.Marshal(map[string]string{"currency": want.Wire.Currency})
				gotWire, _ := json.Marshal(wire)
				if string(gotWire) != string(wantWire) {
					t.Errorf("answer = %s, want %s", gotWire, wantWire)
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
