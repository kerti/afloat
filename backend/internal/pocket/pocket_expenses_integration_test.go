package pocket_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// What the Expenses table adds to Pockets (#140): the id is unique twice, the
// delete is refused while any Expense references the Pocket, and the list names
// the Pocket capture offers first. Real Postgres, no t.Parallel (ADR-0004).

// A Pocket's id is unique twice now, the primary key and (household_id, id) for
// the Expenses' foreign key, and ON CONFLICT (id) arbitrates only the first: a
// concurrent replay of one id can trip the second (BOOTSTRAP.md §4). It is read
// back like a name clash, and is never a 500.
func TestAnIDKeyClashIsReadBackBeforeItIsIDTaken(t *testing.T) {
	idClash := func(context.Context, db.CreatePocketParams) (db.Pocket, error) {
		return db.Pocket{}, &pgconn.PgError{Code: "23505", ConstraintName: "pockets_household_id_id_key"}
	}
	for _, tc := range []struct {
		name string
		get  func(context.Context, db.GetLivePocketParams) (db.Pocket, error)
		want string
	}{
		{"the id names a live Pocket: a replay", func(_ context.Context, p db.GetLivePocketParams) (db.Pocket, error) {
			return db.Pocket{ID: p.ID, PocketDisplayName: "OVO", Active: true}, nil
		}, "200"},
		{"no such Pocket: it was deleted meanwhile", func(context.Context, db.GetLivePocketParams) (db.Pocket, error) {
			return db.Pocket{}, pgx.ErrNoRows
		}, "409 ID_TAKEN"},
		{"the read-back fails", func(context.Context, db.GetLivePocketParams) (db.Pocket, error) {
			return db.Pocket{}, errOutage
		}, "500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, func(q db.Querier) db.Querier {
				return breakable{Querier: q, create: idClash, get: tc.get}
			})
			_, ctx := f.member(t, "Home", "a@example.com")
			if got := statusOf(f.create(ctx, t, newID(t), "OVO")); got != tc.want {
				t.Errorf("status = %s, want %s", got, tc.want)
			}
		})
	}
}

// ---- POCKET_IN_USE -------------------------------------------------------------

func assertInUse(t *testing.T, resp api.DeletePocketResponseObject) {
	t.Helper()
	if got := deleteStatus(resp); got != "409 POCKET_IN_USE" {
		t.Fatalf("DeletePocket = %s, want 409 POCKET_IN_USE", got)
	}
	if code, body := wireOfDelete(t, resp); code != 409 || strings.TrimSpace(body) != `{"code":"POCKET_IN_USE"}` {
		t.Errorf("wire = %d %s, want 409 {\"code\":\"POCKET_IN_USE\"} with no args", code, body)
	}
}

// A Household with two active Pockets, so that deleting one is never LAST_POCKET:
// returns the one under test and the one that is kept.
func (f fixture) twoPockets(t *testing.T) (ctx context.Context, householdID pgtype.UUID, target, keeper uuid.UUID) {
	t.Helper()
	householdID, ctx = f.member(t, "Home", "a@example.com")
	target, keeper = newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, target, "Cash"))
	assertCreated(t, f.create(ctx, t, keeper, "Wallet"))
	return ctx, householdID, target, keeper
}

func TestAPocketAnExpenseReferencesCannotBeDeletedAndNothingChanges(t *testing.T) {
	for name, deleted := range map[string]bool{"a live Expense": false, "an Expense in the Recycle Bin": true} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ctx, householdID, target, _ := f.twoPockets(t)
			f.tdb.CreateExpense(t, householdID, pgtype.UUID{Bytes: target, Valid: true}, testutil.ExpenseOpts{Deleted: deleted})
			before := f.row(t, target)

			assertInUse(t, f.del(ctx, t, target))
			// A repeat is still refused: nothing was written, so there is no
			// "already deleted" to be idempotent about.
			assertInUse(t, f.del(ctx, t, target))
			if after := f.row(t, target); after != before {
				t.Errorf("a refused delete changed the row:\n%s\n%s", before, after)
			}
		})
	}
}

// Deactivating is the answer POCKET_IN_USE offers, and it touches no Expense.
func TestDeactivatingAPocketAnExpenseReferencesIsNotRefusedAndTouchesNoExpense(t *testing.T) {
	f := newFixture(t)
	ctx, householdID, target, _ := f.twoPockets(t)
	f.tdb.CreateExpense(t, householdID, pgtype.UUID{Bytes: target, Valid: true}, testutil.ExpenseOpts{})
	var before string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT to_jsonb(e)::text FROM expenses e`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	assertInUse(t, f.del(ctx, t, target))
	assertUpdated(t, f.update(ctx, t, target, nil, ptr(false)))
	assertInUse(t, f.del(ctx, t, target)) // inactive is still referenced

	var after string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT to_jsonb(e)::text FROM expenses e`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("an Expense changed:\n%s\n%s", before, after)
	}
}

func TestOnceNoExpenseReferencesThePocketItCanBeDeleted(t *testing.T) {
	f := newFixture(t)
	ctx, householdID, target, keeper := f.twoPockets(t)
	expense := f.tdb.CreateExpense(t, householdID, pgtype.UUID{Bytes: target, Valid: true}, testutil.ExpenseOpts{})
	assertInUse(t, f.del(ctx, t, target))

	// Re-pointed (the edit that moves an Expense arrives with its slice).
	f.exec(t, `UPDATE expenses SET pocket_id = $1 WHERE id = $2`, keeper, expense)
	assertDeleted(t, f.del(ctx, t, target))
	// And now the one it was moved to is the one in use (with a third active
	// Pocket, so that it is not the last).
	assertCreated(t, f.create(ctx, t, newID(t), "Gopay"))
	assertInUse(t, f.del(ctx, t, keeper))
}

// Another Household's Expense, on its own Pocket, is no reason to refuse.
func TestAnotherHouseholdsExpensesAreNoReasonToRefuse(t *testing.T) {
	f := newFixture(t)
	ctx, _, target, _ := f.twoPockets(t)
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirs := f.tdb.CreatePocket(t, other, "Theirs")
	f.tdb.CreateExpense(t, other, theirs, testutil.ExpenseOpts{})

	assertDeleted(t, f.del(ctx, t, target))
}

// The order of the refusals: NOT_FOUND, then the idempotent 204 of a Pocket that
// is already deleted, then LAST_POCKET, then POCKET_IN_USE. A last active Pocket
// that is also in use is LAST_POCKET, since the Deactivate that POCKET_IN_USE
// offers would be refused for the same reason.
func TestTheOrderOfTheRefusalsIsNotFoundThenLastPocketThenInUse(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	only := newID(t)
	assertCreated(t, f.create(ctx, t, only, "Cash"))
	f.tdb.CreateExpense(t, householdID, pgtype.UUID{Bytes: only, Valid: true}, testutil.ExpenseOpts{})

	if got := deleteStatus(f.del(ctx, t, newID(t))); got != "404 NOT_FOUND" {
		t.Errorf("an unknown id = %s, want 404 NOT_FOUND", got)
	}
	assertLastPocketOnDelete(t, f.del(ctx, t, only))

	// Once another active Pocket exists, the same delete is IN_USE.
	assertCreated(t, f.create(ctx, t, newID(t), "Wallet"))
	assertInUse(t, f.del(ctx, t, only))

	// An inactive Pocket skips the LAST_POCKET count, and is in use all the same.
	assertUpdated(t, f.update(ctx, t, only, nil, ptr(false)))
	assertInUse(t, f.del(ctx, t, only))
}

// Another Household's Pocket is NOT_FOUND whatever it holds: the refusal is never
// said about a Pocket that is not the caller's.
func TestAForeignPocketWithExpensesIsNotFoundNotInUse(t *testing.T) {
	f := newFixture(t)
	ctx, _, _, _ := f.twoPockets(t)
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirs := f.tdb.CreatePocket(t, other, "Theirs")
	f.tdb.CreateExpense(t, other, theirs, testutil.ExpenseOpts{})

	if got := deleteStatus(f.del(ctx, t, uuid.UUID(theirs.Bytes))); got != "404 NOT_FOUND" {
		t.Errorf("a foreign Pocket in use = %s, want 404 NOT_FOUND", got)
	}
}

// ---- default_pocket_id -----------------------------------------------------------

func (f fixture) defaultPocket(ctx context.Context, t *testing.T) *uuid.UUID {
	t.Helper()
	resp, err := f.handlers.ListPockets(ctx, api.ListPocketsRequestObject{})
	if err != nil {
		t.Fatalf("ListPockets: %v", err)
	}
	got, ok := resp.(api.ListPockets200JSONResponse)
	if !ok {
		t.Fatalf("ListPockets = %#v, want 200", resp)
	}
	return got.DefaultPocketId
}

// expenseAt writes an Expense with a chosen capture instant.
func (f fixture) expenseAt(t *testing.T, householdID, pocketID pgtype.UUID, capturedAt string, deleted bool) {
	t.Helper()
	f.exec(t, `INSERT INTO expenses (id, household_id, occurred_on, captured_at, amount, currency, pocket_id, deleted_at)
		VALUES ($1, $2, '2026-11-23', $3::timestamptz, 1, 'IDR', $4, CASE WHEN $5::boolean THEN now() END)`,
		testutil.NewUUID(t), householdID, capturedAt, pocketID, deleted)
}

func TestTheDefaultPocketIsTheLastUsedByCaptureInstantAndOmittedWhenThereIsNone(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	a, b := f.tdb.CreatePocket(t, householdID, "Cash"), f.tdb.CreatePocket(t, householdID, "Wallet")

	if got := f.defaultPocket(ctx, t); got != nil {
		t.Fatalf("default with no Expense = %v, want none", got)
	}
	// Omitted on the wire, never null.
	resp, _ := f.handlers.ListPockets(ctx, api.ListPocketsRequestObject{})
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "default_pocket_id") {
		t.Errorf("wire = %s, want no default_pocket_id", body)
	}

	f.expenseAt(t, householdID, a, "2026-11-20T10:00:00Z", false)
	f.expenseAt(t, householdID, b, "2026-11-21T10:00:00Z", false)
	if got := f.defaultPocket(ctx, t); got == nil || *got != uuid.UUID(b.Bytes) {
		t.Errorf("default = %v, want the later-captured Pocket %v", got, b)
	}
	// By captured_at, not by occurred_on: this one is dated long ago and captured
	// last.
	f.exec(t, `INSERT INTO expenses (id, household_id, occurred_on, captured_at, amount, currency, pocket_id)
		VALUES ($1, $2, '2020-01-01', '2026-11-22T10:00:00Z', 1, 'IDR', $3)`, testutil.NewUUID(t), householdID, a)
	if got := f.defaultPocket(ctx, t); got == nil || *got != uuid.UUID(a.Bytes) {
		t.Errorf("default = %v, want %v (captured last)", got, a)
	}
}

// "Most recent live Expense whose Pocket is active and live": a deleted Expense
// and an inactive or deleted Pocket do not stand in, and the next most recent
// one that qualifies does.
func TestTheDefaultPocketSkipsDeletedExpensesAndInactiveOrDeletedPockets(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	older, newer := f.tdb.CreatePocket(t, householdID, "Cash"), f.tdb.CreatePocket(t, householdID, "Wallet")
	f.expenseAt(t, householdID, older, "2026-11-20T10:00:00Z", false)
	f.expenseAt(t, householdID, newer, "2026-11-21T10:00:00Z", false)

	f.exec(t, `UPDATE expenses SET deleted_at = now() WHERE pocket_id = $1`, newer)
	if got := f.defaultPocket(ctx, t); got == nil || *got != uuid.UUID(older.Bytes) {
		t.Errorf("a deleted Expense stood in: %v, want %v", got, older)
	}
	f.exec(t, `UPDATE expenses SET deleted_at = NULL WHERE pocket_id = $1`, newer)

	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, newer)
	if got := f.defaultPocket(ctx, t); got == nil || *got != uuid.UUID(older.Bytes) {
		t.Errorf("an inactive Pocket stood in: %v, want %v", got, older)
	}
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, older)
	if got := f.defaultPocket(ctx, t); got != nil {
		t.Errorf("every Pocket inactive, default = %v, want none", got)
	}
}

func TestTheDefaultPocketIsTheHouseholdsAndNobodyElses(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	f.tdb.CreatePocket(t, householdID, "Cash")
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirs := f.tdb.CreatePocket(t, other, "Theirs")
	f.expenseAt(t, other, theirs, "2026-11-22T10:00:00Z", false)

	if got := f.defaultPocket(ctx, t); got != nil {
		t.Errorf("another Household's Expense made %v the default", got)
	}
	// A second User of the same Household sees the same default.
	mine := f.tdb.CreatePocket(t, householdID, "Wallet")
	f.expenseAt(t, householdID, mine, "2026-11-21T10:00:00Z", false)
	second := f.userIn(t, householdID, "b@example.com")
	if got := f.defaultPocket(second, t); got == nil || *got != uuid.UUID(mine.Bytes) {
		t.Errorf("a second member's default = %v, want %v", got, mine)
	}
}

func TestAFailureToFindTheDefaultIsA500(t *testing.T) {
	f := newFixtureWith(t, func(q db.Querier) db.Querier { return lastUsedBroken{Querier: q} })
	_, ctx := f.member(t, "Home", "a@example.com")
	resp, err := f.handlers.ListPockets(ctx, api.ListPocketsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resp.(api.ListPockets500JSONResponse); !ok {
		t.Errorf("ListPockets = %#v, want 500", resp)
	}
}

type lastUsedBroken struct{ db.Querier }

func (lastUsedBroken) LastUsedPocket(context.Context, pgtype.UUID) (pgtype.UUID, error) {
	return pgtype.UUID{}, errOutage
}
