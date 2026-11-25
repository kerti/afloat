package category_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// CATEGORY_IN_USE extended to Expenses (#130 C8, #140 R12): a Category that any
// Expense references, live or in the Recycle Bin, is not deleted, whatever the
// Budget holds. One code for both reasons. The race is in internal/expense
// (race_integration_test.go). Real Postgres, no t.Parallel (ADR-0004).

func (f fixture) inUse(t *testing.T, householdID pgtype.UUID, id uuid.UUID, deleted bool) uuid.UUID {
	t.Helper()
	pocket := f.tdb.CreatePocket(t, householdID, "Cash "+uuid.NewString()[:8])
	e := f.tdb.CreateExpense(t, householdID, pocket, testutil.ExpenseOpts{
		Category: pgtype.UUID{Bytes: id, Valid: true}, Deleted: deleted,
	})
	return uuid.UUID(e.Bytes)
}

func assertInUse(t *testing.T, resp api.DeleteCategoryResponseObject) {
	t.Helper()
	if got := deleteStatus(resp); got != "409 CATEGORY_IN_USE" {
		t.Fatalf("DeleteCategory = %s, want 409 CATEGORY_IN_USE", got)
	}
	if code, body := wireOfDelete(t, resp); code != 409 || strings.TrimSpace(body) != `{"code":"CATEGORY_IN_USE"}` {
		t.Errorf("wire = %d %s, want 409 {\"code\":\"CATEGORY_IN_USE\"} with no args", code, body)
	}
}

func TestACategoryAnExpenseReferencesCannotBeDeletedAndNothingChanges(t *testing.T) {
	for name, deleted := range map[string]bool{"a live Expense": false, "an Expense in the Recycle Bin": true} {
		for _, bucket := range []api.Bucket{api.Daily, api.Commitment} {
			t.Run(name+", "+string(bucket), func(t *testing.T) {
				f := newFixture(t)
				householdID, ctx := f.member(t, "Home", "a@example.com")
				food := newID(t)
				var allocation *string
				if bucket == api.Commitment {
					allocation = ptr("1000")
				}
				assertCreated(t, f.create(ctx, t, food, "Food", bucket, allocation))
				f.inUse(t, householdID, food, deleted)
				before := f.row(t, food)

				assertInUse(t, f.del(ctx, t, food))
				if f.row(t, food) != before {
					t.Error("a refused delete changed the row")
				}
				// What it offers instead, which touches no Expense.
				assertUpdated(t, f.update(ctx, t, food, nil, ptr(false)))
				assertInUse(t, f.del(ctx, t, food))
			})
		}
	}
}

func TestOnceNoExpenseReferencesTheCategoryItCanBeDeleted(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	food, rent := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Daily, nil))
	e := f.inUse(t, householdID, food, false)
	assertInUse(t, f.del(ctx, t, food))

	// Re-pointed, or made uncategorised (the edits that do so arrive with their
	// slice).
	f.exec(t, `UPDATE expenses SET category_id = $1 WHERE id = $2`, rent, e)
	assertDeleted(t, f.del(ctx, t, food))
	assertInUse(t, f.del(ctx, t, rent))
	f.exec(t, `UPDATE expenses SET category_id = NULL WHERE id = $1`, e)
	assertDeleted(t, f.del(ctx, t, rent))
}

// An uncategorised Expense references no Category, and another Household's
// Expense is no business of ours.
func TestUncategorisedAndForeignExpensesAreNoReasonToRefuse(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	food := newID(t)
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, nil))
	pocket := f.tdb.CreatePocket(t, householdID, "Cash")
	f.tdb.CreateExpense(t, householdID, pocket, testutil.ExpenseOpts{})
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
	f.tdb.CreateExpense(t, other, theirPocket, testutil.ExpenseOpts{})

	assertDeleted(t, f.del(ctx, t, food))
}

// The allocation reason is unchanged beside the new one: either alone refuses.
func TestTheAllocationReasonStillRefusesBesideTheExpenseOne(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-10-10T10:00:00+07:00")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	f.setClock(t, day14)

	assertInUse(t, f.del(ctx, t, rent))
}

// A repeat delete of a deleted Category is the idempotent 204 and the guard does
// not apply to what is no longer there.
func TestARepeatDeleteIsNotRefusedByAnExpenseThatAppearedAfterwards(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	food := newID(t)
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, nil))
	assertDeleted(t, f.del(ctx, t, food))
	// Not reachable through the API (a deleted Category is refused as a
	// reference), but written directly the guard still must not apply.
	f.inUse(t, householdID, food, false)

	assertDeleted(t, f.del(ctx, t, food))
}
