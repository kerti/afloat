package household_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// CURRENCY_LOCKED (#113 D2, #140 R12): once the Household holds any Expense, live
// or deleted, a reporting_currency that differs from the stored one is refused
// with nothing applied; the stored one is a no-op. The race the lock exists for
// is in internal/expense (race_integration_test.go), which holds an Expense
// create and a currency change against each other. Real Postgres, no t.Parallel
// (ADR-0004).

func (f fixture) withExpense(t *testing.T, householdID pgtype.UUID, deleted bool) {
	t.Helper()
	pocket := f.tdb.CreatePocket(t, householdID, "Cash")
	f.tdb.CreateExpense(t, householdID, pocket, testutil.ExpenseOpts{Deleted: deleted})
}

func assertLocked(t *testing.T, resp api.UpdateHouseholdResponseObject) {
	t.Helper()
	got, is := resp.(api.UpdateHousehold409JSONResponse)
	if !is || got.Code != api.CURRENCYLOCKED {
		t.Fatalf("response = %#v, want 409 CURRENCY_LOCKED", resp)
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitUpdateHouseholdResponse(rec); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 409 || strings.TrimSpace(rec.Body.String()) != `{"code":"CURRENCY_LOCKED"}` {
		t.Errorf("wire = %d %s, want 409 {\"code\":\"CURRENCY_LOCKED\"} with no args", rec.Code, rec.Body.String())
	}
}

func TestACurrencyChangeIsFreeUntilTheFirstExpense(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	if got := f.ok(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr("USD")}); got.ReportingCurrency != "USD" {
		t.Errorf("currency = %s, want USD", got.ReportingCurrency)
	}
	if got := f.ok(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr("EUR")}); got.ReportingCurrency != "EUR" {
		t.Errorf("currency = %s, want EUR", got.ReportingCurrency)
	}
}

func TestADifferentCurrencyIsRefusedOnceAnExpenseExistsAndNothingIsApplied(t *testing.T) {
	for name, deleted := range map[string]bool{"a live Expense": false, "an Expense in the Recycle Bin": true} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			id, ctx := f.member(t, "Home", "a@example.com")
			f.withExpense(t, id, deleted)
			before := f.row(t, id)

			assertLocked(t, f.patch(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr("USD")}))
			// The rest of the body is refused with it: the whole PATCH.
			assertLocked(t, f.patch(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr("USD"), HouseholdDisplayName: ptr("Renamed"), PeriodStartDay: ptr(5)}))
			if f.row(t, id) != before {
				t.Error("a refused PATCH changed the row")
			}
		})
	}
}

// Sending the stored currency is a no-op, not a refusal, and so is every PATCH
// that leaves the currency out.
func TestTheStoredCurrencyAndAbsentOnesAreNeverRefused(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")
	f.withExpense(t, id, false)

	got := f.ok(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr("IDR"), HouseholdDisplayName: ptr("Renamed")})
	if got.ReportingCurrency != "IDR" || got.HouseholdDisplayName != "Renamed" {
		t.Errorf("answered %+v", got)
	}
	f.ok(ctx, t, api.UpdateHouseholdRequest{PeriodStartDay: ptr(7)})
	f.ok(ctx, t, api.UpdateHouseholdRequest{})
}

// An Expense stays locked for ever: deleting every one does not release it (the
// Recycle Bin never empties).
func TestDeletingEveryExpenseDoesNotReleaseTheCurrency(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")
	f.withExpense(t, id, false)
	if _, err := f.tdb.Pool.Exec(context.Background(), `UPDATE expenses SET deleted_at = now()`); err != nil {
		t.Fatal(err)
	}
	assertLocked(t, f.patch(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr("USD")}))
}

// Another Household's Expenses are no reason to refuse.
func TestAnotherHouseholdsExpensesDoNotLockOurCurrency(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Ours", "ours@example.com")
	theirs, _ := f.member(t, "Theirs", "theirs@example.com")
	f.withExpense(t, theirs, false)

	f.ok(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr("USD")})
}

// Validation comes before the lock: a currency that is not one is a 400 whatever
// the Household holds.
func TestAMalformedCurrencyIsValidationBeforeTheGuard(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")
	f.withExpense(t, id, false)

	assertValidation(t, f.patch(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr("XXZ")}), "reporting_currency", "iso4217")
}

// A Household that is gone cannot be served coherently: the guarded transaction
// answers 500 and writes nothing, as the single statement does.
func TestAHouseholdThatIsGoneIsInternalInTheGuardedTransactionToo(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")
	f.exec(t, `UPDATE households SET deleted_at = now() WHERE id = $1`, id)
	before := f.row(t, id)

	if _, is := f.patch(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr("USD")}).(api.UpdateHousehold500JSONResponse); !is {
		t.Fatal("want 500 for a Household that is gone")
	}
	if f.row(t, id) != before {
		t.Error("a failed PATCH wrote")
	}
}

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tdb.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
}
