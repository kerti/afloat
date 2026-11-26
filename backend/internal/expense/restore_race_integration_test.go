package expense_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// The restore of an Expense is the third writer of the recipe in
// race_integration_test.go (#140 X11): it brings back a row that references a
// Pocket and a Category, so it takes both FOR SHARE, with the liveness predicate
// in the locking statement, before it writes.
//
// What is and is not provable, said plainly. The shipped guards (POCKET_IN_USE,
// CATEGORY_IN_USE) count Expenses live AND deleted (R11: the bin never empties,
// and there is "no live-only lock"), so a Pocket or Category an Expense in the
// bin references can never be deleted, and a restore can never meet one that is.
// Against the shipped guards the restore's locks therefore change what waits,
// never what is stored, and the first two tests prove exactly that: both orders,
// nothing wrong at the end, the guard answering as it does whichever came first.
//
// The locks are a belt for the day a guard counts live rows only. The last tests
// make that day today, in the test only: a delete written here as the live-only
// recipe (lock the row, count LIVE referencing Expenses in a later statement,
// write), raced against the real restore in both orders. With the restore's locks
// the data is right (one of them refuses); with them stripped it is wrong, a live
// Expense on a deleted Pocket or Category. Those two are the red proofs, kept.

// restoreGuard is one referenced row a restore locks.
type restoreGuard struct {
	name string
	// shipped is the guard the delete of this row carries (race_integration_test).
	shipped guard
	// table and column say where the live-only delete counts.
	table, column string
	// target is the row's id in the world.
	target func(w world) pgtype.UUID
	// refusal is the status the shipped delete answers while the Expense exists.
	refusal string
}

var restoreGuards = []restoreGuard{
	{name: "pocket", shipped: guards[0], table: "pockets", column: "pocket_id", target: func(w world) pgtype.UUID { return w.pocket }, refusal: "POCKET_IN_USE"},
	{name: "category", shipped: guards[1], table: "categories", column: "category_id", target: func(w world) pgtype.UUID { return w.category }, refusal: "CATEGORY_IN_USE"},
}

// binWorld is a world with one deleted Expense on its Pocket and Category.
type binWorld struct {
	world
	expense pgtype.UUID
}

func newBinWorld(t *testing.T, restoreWrap, removalWrap func(db.DBTX) db.DBTX) binWorld {
	t.Helper()
	w := newWorld(t, restoreWrap, removalWrap)
	id := w.tdb.CreateExpense(t, w.household, w.pocket, testutil.ExpenseOpts{Category: w.category, Deleted: true})
	return binWorld{world: w, expense: id}
}

func (w binWorld) restore(t *testing.T) string {
	t.Helper()
	resp, err := w.expenses.RestoreExpense(w.ctx, api.RestoreExpenseRequestObject{Id: w.expense.Bytes})
	if err != nil {
		t.Errorf("RestoreExpense returned an error: %v", err)
	}
	return status(resp)
}

// wrong says what is wrong with the data: a LIVE Expense on a deleted Pocket or
// Category. (A deleted one on a deleted Pocket is what the shipped guards
// forbid and the existing tests check.)
func (w binWorld) wrong(t *testing.T) string {
	t.Helper()
	if n := scalarInt(t, w.world, `SELECT count(*) FROM expenses e JOIN pockets p ON p.id = e.pocket_id WHERE e.deleted_at IS NULL AND p.deleted_at IS NOT NULL`); n > 0 {
		return fmt.Sprintf("%d live Expense(s) on a deleted Pocket", n)
	}
	if n := scalarInt(t, w.world, `SELECT count(*) FROM expenses e JOIN categories c ON c.id = e.category_id WHERE e.deleted_at IS NULL AND c.deleted_at IS NOT NULL`); n > 0 {
		return fmt.Sprintf("%d live Expense(s) on a deleted Category", n)
	}
	return ""
}

type restoreResult struct {
	restore, removal string
	followerBlocked  bool
	violation        string
}

// ---- against the shipped guards ----------------------------------------------------

// A restore that holds its locks is waited for by the delete of the Pocket or
// Category it references, and the delete then answers *_IN_USE: the Expense it
// brought back (and the one that was in the bin all along) count.
func TestARestoreThatGetsThereFirstIsSeenByTheGuardedDelete(t *testing.T) {
	for _, rg := range restoreGuards {
		t.Run(rg.name, func(t *testing.T) {
			held := newGate()
			w := newBinWorld(t, createWrap(held, "RestoreExpense", nil), identity)

			restored := make(chan string, 1)
			go func() { restored <- w.restore(t) }()
			held.waitReached(t)

			removed := make(chan string, 1)
			go func() { removed <- rg.shipped.remove(t, w.world) }()
			removal, finished := within(removed, blockedFor)
			if finished {
				t.Errorf("the delete did not wait for the restore that held the %s", rg.name)
			}

			held.let()
			restore, _ := within(restored, 10*time.Second)
			if !finished {
				removal, _ = within(removed, 10*time.Second)
			}
			if restore != "200" || removal != rg.refusal {
				t.Errorf("restore %s, delete %s; want 200 and %s", restore, removal, rg.refusal)
			}
			if v := w.wrong(t); v != "" {
				t.Error(v)
			}
		})
	}
}

// A guarded delete that holds its lock is waited for by the restore, which then
// restores all the same: the delete was refused, so the row it was about is still
// there, and the restore finds it live.
func TestAGuardedDeleteThatGetsThereFirstIsSeenByTheRestore(t *testing.T) {
	for _, rg := range restoreGuards {
		t.Run(rg.name, func(t *testing.T) {
			held := newGate()
			w := newBinWorld(t, identity, removalWrap(held, rg.shipped.heldAfter, rg.shipped.removalClause, nil))

			removed := make(chan string, 1)
			go func() { removed <- rg.shipped.remove(t, w.world) }()
			held.waitReached(t)

			restored := make(chan string, 1)
			go func() { restored <- w.restore(t) }()
			restore, finished := within(restored, blockedFor)
			if finished {
				t.Errorf("the restore did not wait for the delete that held the %s", rg.name)
			}

			held.let()
			removal, _ := within(removed, 10*time.Second)
			if !finished {
				restore, _ = within(restored, 10*time.Second)
			}
			if restore != "200" || removal != rg.refusal {
				t.Errorf("restore %s, delete %s; want 200 and %s", restore, removal, rg.refusal)
			}
			if v := w.wrong(t); v != "" {
				t.Error(v)
			}
		})
	}
}

// ---- the belt: against a delete that counts live Expenses only ---------------------

// liveOnlyDelete is the guard R11 rules out, written the way the recipe writes a
// guard: lock the row, count the LIVE Expenses that reference it in a separate
// later statement, write. held, when it is not nil, is paused after the count
// with the lock held.
func liveOnlyDelete(t *testing.T, w binWorld, rg restoreGuard, held *gate) string {
	t.Helper()
	ctx := context.Background()
	tx, err := w.tdb.Pool.Begin(ctx)
	if err != nil {
		t.Errorf("begin: %v", err)
		return "begin failed"
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id := rg.target(w.world)
	if _, err := tx.Exec(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE id = $1 FOR NO KEY UPDATE`, rg.table), id); err != nil {
		t.Errorf("lock: %v", err)
		return "lock failed"
	}
	var live int
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM expenses WHERE %s = $1 AND deleted_at IS NULL`, rg.column), id).Scan(&live); err != nil {
		t.Errorf("count: %v", err)
		return "count failed"
	}
	if live > 0 {
		return "IN_USE"
	}
	if held != nil {
		held.pause()
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET deleted_at = now() WHERE id = $1`, rg.table), id); err != nil {
		t.Errorf("write: %v", err)
		return "write failed"
	}
	if err := tx.Commit(ctx); err != nil {
		t.Errorf("commit: %v", err)
		return "commit failed"
	}
	return "204"
}

// holdRestore holds the restore with its locks held, starts the live-only delete,
// then lets the restore finish.
func (rg restoreGuard) holdRestore(t *testing.T, noLocks []string) (binWorld, restoreResult) {
	t.Helper()
	held := newGate()
	w := newBinWorld(t, createWrap(held, "RestoreExpense", noLocks), identity)
	restored := make(chan string, 1)
	go func() { restored <- w.restore(t) }()
	held.waitReached(t)

	removed := make(chan string, 1)
	go func() { removed <- liveOnlyDelete(t, w, rg, nil) }()
	removal, finished := within(removed, blockedFor)

	held.let()
	restore, _ := within(restored, 10*time.Second)
	if !finished {
		removal, _ = within(removed, 10*time.Second)
	}
	return w, restoreResult{restore: restore, removal: removal, followerBlocked: !finished, violation: w.wrong(t)}
}

// holdDelete holds the live-only delete after its count with its lock held,
// starts the restore, then lets the delete finish.
func (rg restoreGuard) holdDelete(t *testing.T, noLocks []string) (binWorld, restoreResult) {
	t.Helper()
	held := newGate()
	w := newBinWorld(t, createWrap(nil, "", noLocks), identity)
	removed := make(chan string, 1)
	go func() { removed <- liveOnlyDelete(t, w, rg, held) }()
	held.waitReached(t)

	restored := make(chan string, 1)
	go func() { restored <- w.restore(t) }()
	restore, finished := within(restored, blockedFor)

	held.let()
	removal, _ := within(removed, 10*time.Second)
	if !finished {
		restore, _ = within(restored, 10*time.Second)
	}
	return w, restoreResult{restore: restore, removal: removal, followerBlocked: !finished, violation: w.wrong(t)}
}

var restoreLocks = []string{lockPocketShared, lockCategoryShared}

// Restore first: the delete waits, then counts the Expense the restore brought
// back and refuses. Nothing is wrong.
func TestTheRestoresLocksMakeALiveOnlyDeleteWaitAndSeeTheRestoredExpense(t *testing.T) {
	for _, rg := range restoreGuards {
		t.Run(rg.name, func(t *testing.T) {
			w, got := rg.holdRestore(t, nil)
			if !got.followerBlocked {
				t.Error("the delete did not wait for the restore that held the lock")
			}
			if got.restore != "200" || got.removal != "IN_USE" || got.violation != "" {
				t.Errorf("restore %s, delete %s, wrong %q; want 200, IN_USE, nothing", got.restore, got.removal, got.violation)
			}
			if n := scalarInt(t, w.world, `SELECT count(*) FROM expenses WHERE deleted_at IS NULL`); n != 1 {
				t.Errorf("%d live Expenses, want the restored one", n)
			}
		})
	}
}

// Delete first: the restore waits, then finds the row deleted and refuses
// (500, nothing written), so no live Expense stands on a deleted row.
func TestTheRestoresLocksMakeItWaitForALiveOnlyDeleteAndRefuseTheDeletedRow(t *testing.T) {
	for _, rg := range restoreGuards {
		t.Run(rg.name, func(t *testing.T) {
			w, got := rg.holdDelete(t, nil)
			if !got.followerBlocked {
				t.Error("the restore did not wait for the delete that held the lock")
			}
			if got.restore != "500" || got.removal != "204" || got.violation != "" {
				t.Errorf("restore %s, delete %s, wrong %q; want 500, 204, nothing", got.restore, got.removal, got.violation)
			}
			if n := scalarInt(t, w.world, `SELECT count(*) FROM expenses WHERE deleted_at IS NOT NULL`); n != 1 {
				t.Errorf("%d deleted Expenses, want the one that stayed in the bin", n)
			}
		})
	}
}

// The red proofs, kept: the same two interleavings with the restore's FOR SHARE
// taken off leave a live Expense on a deleted Pocket or Category. If one ever
// stops failing, the lock is no longer what keeps a restore from resurrecting a
// reference, and the restore needs a rethink, not a quiet deletion.
func TestWithoutItsSharedLocksARestoreResurrectsAReferenceToADeletedRow(t *testing.T) {
	for _, rg := range restoreGuards {
		t.Run(rg.name+" restore first", func(t *testing.T) {
			_, got := rg.holdRestore(t, restoreLocks)
			if got.followerBlocked {
				t.Error("the delete waited although the restore held no lock")
			}
			if got.violation == "" {
				t.Errorf("restore %s, delete %s: nothing is wrong, so the restore's locks are not what the belt relies on", got.restore, got.removal)
			}
		})
		t.Run(rg.name+" delete first", func(t *testing.T) {
			_, got := rg.holdDelete(t, restoreLocks)
			if got.followerBlocked {
				t.Error("the restore waited although it took no lock")
			}
			if got.violation == "" {
				t.Errorf("restore %s, delete %s: nothing is wrong, so the restore's locks are not what the belt relies on", got.restore, got.removal)
			}
		})
	}
}
