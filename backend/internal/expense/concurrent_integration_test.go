package expense_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
)

// N concurrent POSTs of one id: one 201, the rest 200, one row (BOOTSTRAP.md §4,
// idempotent create).
func TestConcurrentCreatesOfOneIDAreOne201AndTheRestReplays(t *testing.T) {
	for round := range 5 {
		f := newFixture(t)
		_, pocket, ctx := f.member(t, "Home", "a@example.com")
		req := body(pocket)

		statuses := race(8, func(int) string { return status(f.create(ctx, t, req)) })

		created, replayed := 0, 0
		for _, s := range statuses {
			switch s {
			case "201":
				created++
			case "200":
				replayed++
			default:
				t.Errorf("round %d: answered %s, want 201 or 200", round, s)
			}
		}
		if created != 1 || replayed != 7 {
			t.Errorf("round %d: %d created and %d replayed, want 1 and 7 (%v)", round, created, replayed, statuses)
		}
		if n := f.count(t); n != 1 {
			t.Errorf("round %d: %d rows, want 1", round, n)
		}
	}
}

// The red proof, kept: a SELECT-then-INSERT, with the callers held between the
// two as a double tap makes them, inserts the id twice or fails one of them. If
// this ever stops failing, the create no longer depends on the arbiter and the
// test above proves less than it did.
func TestASelectThenInsertCreateLosesTheRace(t *testing.T) {
	f := newFixture(t)
	household, pocket, _ := f.member(t, "Home", "a@example.com")
	id := uuid.Must(uuid.NewV7())

	var mu sync.Mutex
	arrived, all := 0, make(chan struct{})
	statuses := race(4, func(int) string {
		var exists bool
		if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM expenses WHERE id = $1)`, id).Scan(&exists); err != nil {
			return "read: " + err.Error()
		}
		mu.Lock()
		arrived++
		if arrived == 4 {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
		case <-time.After(2 * time.Second):
		}
		if exists {
			return "200"
		}
		_, err := f.tdb.Pool.Exec(context.Background(),
			`INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id) VALUES ($1, $2, '2026-11-25', 1, 'IDR', $3)`,
			id, household, pocket)
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return "500"
		}
		if err != nil {
			return err.Error()
		}
		return "201"
	})
	if !slices.Contains(statuses, "500") {
		t.Errorf("%v: no caller failed, so the SELECT-then-INSERT was expected to lose the race", statuses)
	}
}

// Expenses (recorded and re-pointed), Pocket deletions and deactivations, Category deletions and currency
// changes run together on one Household must never deadlock (40P01 is a 500) and
// never leave the data wrong. The lock order is Household, then Pocket, then
// Category, for every writer, in the modes BOOTSTRAP.md §4 names (a removal's
// FOR NO KEY UPDATE and an Expense's FOR SHARE).
func TestEveryWriterTogetherNeverDeadlocksAndNeverLeavesTheDataWrong(t *testing.T) {
	for round := range 5 {
		w := newWorld(t, identity, identity)
		extra := w.tdb.CreatePocket(t, w.household, "Wallet")

		ops := []func(i int) string{
			func(int) string {
				return w.create(t, api.CreateExpenseRequest{Id: uuid.Must(uuid.NewV7()), Amount: "1", PocketId: w.pocket.Bytes})
			},
			func(int) string {
				return w.create(t, api.CreateExpenseRequest{Id: uuid.Must(uuid.NewV7()), Amount: "1", PocketId: extra.Bytes})
			},
			func(int) string {
				c := uuid.UUID(w.category.Bytes)
				return w.create(t, api.CreateExpenseRequest{Id: uuid.Must(uuid.NewV7()), Amount: "1", PocketId: w.keeper.Bytes, CategoryId: &c})
			},
			// An edit is the create's twin: Expenses made on the keeper Pocket and
			// then re-pointed at the Pocket, the Category and the extra Pocket the
			// removals are after, and a Category cleared.
			func(int) string {
				id := uuid.Must(uuid.NewV7())
				w.create(t, api.CreateExpenseRequest{Id: id, Amount: "1", PocketId: w.keeper.Bytes})
				return w.edit(t, pgtype.UUID{Bytes: id, Valid: true}, api.UpdateExpenseRequest{PocketId: idPtr(w.pocket)})
			},
			func(int) string {
				id := uuid.Must(uuid.NewV7())
				w.create(t, api.CreateExpenseRequest{Id: id, Amount: "1", PocketId: w.keeper.Bytes})
				c := uuid.UUID(w.category.Bytes)
				return w.edit(t, pgtype.UUID{Bytes: id, Valid: true}, api.UpdateExpenseRequest{PocketId: idPtr(extra), CategoryId: valueOf(c)})
			},
			func(int) string {
				id := uuid.Must(uuid.NewV7())
				c := uuid.UUID(w.category.Bytes)
				w.create(t, api.CreateExpenseRequest{Id: id, Amount: "1", PocketId: w.keeper.Bytes, CategoryId: &c})
				return w.edit(t, pgtype.UUID{Bytes: id, Valid: true}, api.UpdateExpenseRequest{CategoryId: nullOf[uuid.UUID](), Amount: amount("2")})
			},
			func(int) string { // the last two active Pockets are w.pocket and w.keeper and extra: remove two of three
				resp, _ := w.pockets.DeletePocket(w.ctx, api.DeletePocketRequestObject{Id: w.pocket.Bytes})
				return removalStatus(resp)
			},
			func(int) string {
				resp, _ := w.pockets.UpdatePocket(w.ctx, api.UpdatePocketRequestObject{Id: extra.Bytes, Body: &api.UpdatePocketRequest{Active: ptr(false)}})
				if _, ok := resp.(api.UpdatePocket200JSONResponse); ok {
					return "200"
				}
				return "refused"
			},
			func(int) string {
				resp, _ := w.categories.DeleteCategory(w.ctx, api.DeleteCategoryRequestObject{Id: w.category.Bytes})
				return removalStatus(resp)
			},
			func(int) string {
				resp, _ := w.households.UpdateHousehold(w.ctx, api.UpdateHouseholdRequestObject{Body: &api.UpdateHouseholdRequest{ReportingCurrency: ptr("USD")}})
				return removalStatus(resp)
			},
		}
		n := 3 * len(ops)
		statuses := race(n, func(i int) string { return ops[i%len(ops)](i) })

		for i, s := range statuses {
			if s == "500" || s == "INTERNAL" || strings.HasPrefix(s, "unexpected") {
				t.Errorf("round %d: operation %d answered %s", round, i%len(ops), s)
			}
		}
		for name, check := range map[string]string{
			"an Expense on a deleted Pocket":   `SELECT count(*) FROM expenses e JOIN pockets p ON p.id = e.pocket_id WHERE p.deleted_at IS NOT NULL`,
			"an Expense in a deleted Category": `SELECT count(*) FROM expenses e JOIN categories c ON c.id = e.category_id WHERE c.deleted_at IS NOT NULL`,
			"an Expense in another currency":   `SELECT count(*) FROM expenses e JOIN households h ON h.id = e.household_id WHERE e.currency <> h.reporting_currency`,
			"no active Pocket":                 `SELECT CASE WHEN count(*) = 0 THEN 1 ELSE 0 END FROM pockets WHERE active AND deleted_at IS NULL`,
		} {
			if got := scalarInt(t, w, check); got != 0 {
				t.Errorf("round %d: %s (%d)", round, name, got)
			}
		}
	}
}
