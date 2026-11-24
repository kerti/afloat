package pocket_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/pocket"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// A Household always keeps at least one active, live Pocket (#140 R1, PRD S10):
// a delete, or a deactivation, that would leave none is 409 LAST_POCKET. Real
// Postgres, no t.Parallel (ADR-0004).

// A Household whose only active Pocket is `last`, plus whatever the test adds.
func (f fixture) lastOnly(t *testing.T) (ctx context.Context, householdID pgtype.UUID, last uuid.UUID) {
	t.Helper()
	householdID, ctx = f.member(t, "Home", "a@example.com")
	last = newID(t)
	assertCreated(t, f.create(ctx, t, last, "Cash"))
	return ctx, householdID, last
}

func assertLastPocketOnDelete(t *testing.T, resp api.DeletePocketResponseObject) {
	t.Helper()
	if got := deleteStatus(resp); got != "409 LAST_POCKET" {
		t.Fatalf("DeletePocket = %s, want 409 LAST_POCKET", got)
	}
	if code, body := wireOfDelete(t, resp); code != 409 || strings.TrimSpace(body) != `{"code":"LAST_POCKET"}` {
		t.Errorf("wire = %d %s, want 409 {\"code\":\"LAST_POCKET\"} with no args", code, body)
	}
}

func assertLastPocketOnUpdate(t *testing.T, resp api.UpdatePocketResponseObject) {
	t.Helper()
	if got := updateStatus(resp); got != "409 LAST_POCKET" {
		t.Fatalf("UpdatePocket = %s, want 409 LAST_POCKET", got)
	}
	if code, body := wireOfUpdate(t, resp); code != 409 || body != `{"code":"LAST_POCKET"}` {
		t.Errorf("wire = %d %s, want 409 {\"code\":\"LAST_POCKET\"} with no args", code, body)
	}
}

func TestDeletingTheLastActivePocketIsLastPocketAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	ctx, _, last := f.lastOnly(t)
	before := f.row(t, last)

	assertLastPocketOnDelete(t, f.del(ctx, t, last))
	if after := f.row(t, last); after != before {
		t.Errorf("a refused delete changed the row:\n%s\n%s", before, after)
	}
}

func TestDeactivatingTheLastActivePocketIsLastPocketAndAppliesNothingOfTheBody(t *testing.T) {
	f := newFixture(t)
	ctx, _, last := f.lastOnly(t)
	before := f.row(t, last)

	assertLastPocketOnUpdate(t, f.update(ctx, t, last, nil, ptr(false)))
	// A rename sent beside the deactivation is refused with it: the whole PATCH.
	assertLastPocketOnUpdate(t, f.update(ctx, t, last, ptr("Renamed"), ptr(false)))
	if after := f.row(t, last); after != before {
		t.Errorf("a refused deactivation changed the row:\n%s\n%s", before, after)
	}
}

// What the invariant does not touch: renaming and reactivating, which never
// shrink the set.
func TestRenamingAndReactivatingTheLastActivePocketAreNotRefused(t *testing.T) {
	f := newFixture(t)
	ctx, _, last := f.lastOnly(t)

	assertUpdated(t, f.update(ctx, t, last, ptr("Wallet"), nil))
	assertUpdated(t, f.update(ctx, t, last, nil, ptr(true)))
	assertUpdated(t, f.update(ctx, t, last, nil, nil))
}

// "Others" are active AND live: an inactive or a deleted Pocket is no
// substitute for the last active one.
func TestOnlyAnActiveLivePocketCountsAsAnother(t *testing.T) {
	for _, tc := range []struct {
		name  string
		other func(t *testing.T, f fixture, ctx context.Context, id uuid.UUID)
	}{
		{"an inactive one", func(t *testing.T, f fixture, ctx context.Context, id uuid.UUID) {
			assertUpdated(t, f.update(ctx, t, id, nil, ptr(false)))
		}},
		{"a deleted one", func(t *testing.T, f fixture, ctx context.Context, id uuid.UUID) {
			assertDeleted(t, f.del(ctx, t, id))
		}},
		{"another Household's", nil},
	} {
		for _, act := range []string{"delete", "deactivate"} {
			t.Run(tc.name+" does not stand in for it, "+act, func(t *testing.T) {
				f := newFixture(t)
				ctx, _, last := f.lastOnly(t)
				switch {
				case tc.other != nil:
					other := newID(t)
					assertCreated(t, f.create(ctx, t, other, "Other"))
					tc.other(t, f, ctx, other)
				default:
					f.tdb.CreatePocket(t, f.tdb.CreateHousehold(t, "Elsewhere"), "Theirs")
				}

				if act == "delete" {
					assertLastPocketOnDelete(t, f.del(ctx, t, last))
				} else {
					assertLastPocketOnUpdate(t, f.update(ctx, t, last, nil, ptr(false)))
				}
			})
		}
	}
}

func TestTheSecondOfTwoActivePocketsIsTheLastOnceTheFirstIsGone(t *testing.T) {
	for _, first := range []string{"delete", "deactivate"} {
		for _, second := range []string{"delete", "deactivate"} {
			t.Run(first+" then "+second, func(t *testing.T) {
				f := newFixture(t)
				ctx, _, a := f.lastOnly(t)
				b := newID(t)
				assertCreated(t, f.create(ctx, t, b, "Wallet"))

				if first == "delete" {
					assertDeleted(t, f.del(ctx, t, a))
				} else {
					assertUpdated(t, f.update(ctx, t, a, nil, ptr(false)))
				}
				if second == "delete" {
					assertLastPocketOnDelete(t, f.del(ctx, t, b))
				} else {
					assertLastPocketOnUpdate(t, f.update(ctx, t, b, nil, ptr(false)))
				}
			})
		}
	}
}

// Deleting an inactive Pocket leaves the active set as it was, so it is never
// refused, whatever the Household has left.
func TestDeletingAnInactivePocketIsNeverRefused(t *testing.T) {
	f := newFixture(t)
	ctx, _, last := f.lastOnly(t)
	inactive := newID(t)
	assertCreated(t, f.create(ctx, t, inactive, "Dana"))
	assertUpdated(t, f.update(ctx, t, inactive, nil, ptr(false)))

	assertDeleted(t, f.del(ctx, t, inactive))
	if !f.isDeleted(t, inactive) || f.isDeleted(t, last) {
		t.Error("the inactive Pocket was not the one deleted")
	}

	// And an inactive Pocket "deactivated" again is a no-op, not a refusal.
	other := newID(t)
	assertCreated(t, f.create(ctx, t, other, "Gopay"))
	assertUpdated(t, f.update(ctx, t, other, nil, ptr(false)))
	assertUpdated(t, f.update(ctx, t, other, nil, ptr(false)))
}

// Even in a Household that holds no active Pocket at all (a database from before
// the rule), deleting an inactive one is not refused: the answer depends on the
// Pocket being removed, not on a count that includes it.
func TestDeletingAnInactivePocketInAHouseholdWithNoActiveOneIsStillNotRefused(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	id := f.tdb.CreatePocket(t, householdID, "Old")
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, id)

	assertDeleted(t, f.del(ctx, t, uuid.UUID(id.Bytes)))
}

// A repeat delete of a deleted Pocket is 204 and writes nothing, even when the
// Pocket was active and is the one the Household would have been left without:
// the guard does not apply to what is no longer there (CATEGORY_IN_USE's rule).
func TestARepeatDeleteIsNeverRefused(t *testing.T) {
	f := newFixture(t)
	ctx, _, a := f.lastOnly(t)
	b := newID(t)
	assertCreated(t, f.create(ctx, t, b, "Wallet"))
	assertDeleted(t, f.del(ctx, t, b)) // b is deleted while active; a is now the last
	first := f.row(t, b)

	assertDeleted(t, f.del(ctx, t, b))
	if after := f.row(t, b); after != first {
		t.Errorf("a repeat delete changed the row:\n%s\n%s", first, after)
	}
	assertLastPocketOnDelete(t, f.del(ctx, t, a))
}

// Restore and create add to the set, so they unblock a removal.
func TestRestoreAndCreateGiveTheHouseholdAnotherActivePocket(t *testing.T) {
	f := newFixture(t)
	ctx, _, a := f.lastOnly(t)
	b := newID(t)
	assertCreated(t, f.create(ctx, t, b, "Wallet"))
	assertDeleted(t, f.del(ctx, t, b))
	assertLastPocketOnDelete(t, f.del(ctx, t, a))

	assertRestored(t, f.restore(ctx, t, b))
	assertDeleted(t, f.del(ctx, t, a))

	c := newID(t)
	assertCreated(t, f.create(ctx, t, c, "Gopay"))
	assertDeleted(t, f.del(ctx, t, b))
	assertLastPocketOnUpdate(t, f.update(ctx, t, c, nil, ptr(false)))
}

// NOT_FOUND is decided first. An id that is unknown or another Household's is
// the one 404 whether or not the caller has a last Pocket; a deleted id is 404
// to PATCH and the idempotent 204 to a delete. LAST_POCKET is only ever said of
// the caller's own live, active Pocket: it confirms nothing about anyone else's
// id, and it is not an oracle for "this id is mine".
func TestNotFoundIsDecidedBeforeLastPocket(t *testing.T) {
	f := newFixture(t)
	ctx, _, _ := f.lastOnly(t)
	gone := newID(t)
	assertCreated(t, f.create(ctx, t, gone, "Gone"))
	assertDeleted(t, f.del(ctx, t, gone))
	_, theirsCtx := f.member(t, "Elsewhere", "b@example.com")
	theirs := newID(t)
	assertCreated(t, f.create(theirsCtx, t, theirs, "Their only one"))
	before := f.row(t, theirs)

	for kind, id := range map[string]uuid.UUID{"unknown": newID(t), "another Household's last": theirs} {
		if got := deleteStatus(f.del(ctx, t, id)); got != "404 NOT_FOUND" {
			t.Errorf("DELETE of %s = %s, want 404 NOT_FOUND", kind, got)
		}
		if got := updateStatus(f.update(ctx, t, id, nil, ptr(false))); got != "404 NOT_FOUND" {
			t.Errorf("PATCH of %s = %s, want 404 NOT_FOUND", kind, got)
		}
	}
	if got := updateStatus(f.update(ctx, t, gone, nil, ptr(false))); got != "404 NOT_FOUND" {
		t.Errorf("PATCH active:false of a deleted Pocket = %s, want 404 NOT_FOUND", got)
	}
	if f.row(t, theirs) != before {
		t.Error("a request of Alice's changed the other Household's row")
	}
}

// Validation comes before the lookup and the guard: a bad name with a
// deactivation is a 400, as it is for a Pocket that is not the last.
func TestValidationComesBeforeTheGuard(t *testing.T) {
	f := newFixture(t)
	ctx, _, last := f.lastOnly(t)

	resp := f.update(ctx, t, last, ptr("   "), ptr(false))
	if got := updateStatus(resp); got != "400" {
		t.Errorf("PATCH = %s, want 400 VALIDATION before LAST_POCKET", got)
	}
}

// A name clash inside the guarded transaction is still NAME_TAKEN, with the
// Pocket still active: the transaction is not committed.
func TestARenameIntoAHeldNameBesideADeactivationIsNameTakenAndNothingApplies(t *testing.T) {
	f := newFixture(t)
	ctx, _, a := f.lastOnly(t)
	assertCreated(t, f.create(ctx, t, newID(t), "Wallet"))
	before := f.row(t, a)

	if got := updateStatus(f.update(ctx, t, a, ptr("wallet"), ptr(false))); got != "409 NAME_TAKEN" {
		t.Errorf("PATCH = %s, want 409 NAME_TAKEN", got)
	}
	if after := f.row(t, a); after != before {
		t.Errorf("a refused PATCH changed the row:\n%s\n%s", before, after)
	}
}

// ---- the race ----------------------------------------------------------------
//
// Two removals of a Household's last two active Pockets, run together, must
// leave one active (#140 R1; BOOTSTRAP.md §4's recipe: lock the Household row,
// count in a later statement, write). Each handler's transaction is held, by a
// Beginner wrapper, right after its count and until the other has counted too
// (or 500ms pass), which is the interleaving the lock exists to prevent: both
// count the other still active. With the lock the second removal cannot reach
// its count until the first has committed, so the wait times out and the count
// that follows sees the truth.

// meet makes n callers wait for each other, up to a deadline.
type meet struct {
	mu      sync.Mutex
	arrived int
	n       int
	all     chan struct{}
}

func newMeet(n int) *meet { return &meet{n: n, all: make(chan struct{})} }

func (m *meet) arrive() {
	m.mu.Lock()
	m.arrived++
	if m.arrived == m.n {
		close(m.all)
	}
	m.mu.Unlock()
	select {
	case <-m.all:
	case <-time.After(500 * time.Millisecond):
	}
}

// The statements are found by the name comment sqlc leaves at the head of each
// one, so a reflow of the SQL under it does not silently stop a marker matching.
const (
	lockHouseholdQuery = "-- name: LockHousehold"
	lockPocketQuery    = "-- name: LockPocket"
	countsOtherPockets = "-- name: OtherActivePocketExists"
	softDeleteQuery    = "-- name: SoftDeletePocket"
	updatePocketQuery  = "-- name: UpdatePocket"
	// What the lock clause is; the red proof strips it.
	householdLockClause = "FOR NO KEY UPDATE"
)

// matchesAny reports whether sql carries any of on's "|"-separated markers.
func matchesAny(sql, on string) bool {
	for _, m := range strings.Split(on, "|") {
		if strings.Contains(sql, m) {
			return true
		}
	}
	return false
}

// racingTx pauses each handler's transaction after its count of the other
// active Pockets. With dropLock it also removes the lock clause from the
// Household lock, which is the "no lock" variant the red proof runs.
type racingTx struct {
	pgx.Tx
	m        *meet
	dropLock bool
}

type pausingRow struct {
	pgx.Row
	after func()
}

func (r pausingRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	r.after()
	return err
}

func (tx racingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.dropLock && strings.Contains(sql, lockHouseholdQuery) {
		if !strings.Contains(sql, householdLockClause) {
			panic("the Household lock no longer carries " + householdLockClause + ": the red proof's marker is stale")
		}
		sql = strings.Replace(sql, householdLockClause, "", 1)
	}
	row := tx.Tx.QueryRow(ctx, sql, args...)
	if strings.Contains(sql, countsOtherPockets) {
		return pausingRow{Row: row, after: tx.m.arrive}
	}
	return row
}

type racingBeginner struct {
	auth.Beginner
	m        *meet
	dropLock bool
}

func (b racingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return racingTx{Tx: tx, m: b.m, dropLock: b.dropLock}, nil
}

func newFixtureTx(t *testing.T, wrap func(auth.Beginner) auth.Beginner) fixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	return fixture{tdb: tdb, handlers: pocket.New(pocket.Deps{Querier: tdb.Queries, Beginner: wrap(tdb.Pool)})}
}

type removal struct {
	name string
	run  func(t *testing.T, f fixture, ctx context.Context, id uuid.UUID) string
}

var removals = []removal{
	{"delete", func(t *testing.T, f fixture, ctx context.Context, id uuid.UUID) string {
		return deleteStatus(f.del(ctx, t, id))
	}},
	{"deactivate", func(t *testing.T, f fixture, ctx context.Context, id uuid.UUID) string {
		return updateStatus(f.update(ctx, t, id, nil, ptr(false)))
	}},
}

func (f fixture) activeLive(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pockets WHERE active AND deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f fixture) raceTheLastTwo(t *testing.T, first, second removal) (statuses []string, activeLeft int) {
	t.Helper()
	ctx, _, a := f.lastOnly(t)
	b := newID(t)
	assertCreated(t, f.create(ctx, t, b, "Wallet"))
	ids := []uuid.UUID{a, b}
	ops := []removal{first, second}
	statuses = race(2, func(i int) string { return ops[i].run(t, f, ctx, ids[i]) })
	return statuses, f.activeLive(t)
}

func TestTwoRemovalsOfTheLastTwoActivePocketsCannotBothSucceed(t *testing.T) {
	for _, first := range removals {
		for _, second := range removals {
			t.Run(first.name+" and "+second.name, func(t *testing.T) {
				for round := range 3 {
					m := newMeet(2)
					f := newFixtureTx(t, func(b auth.Beginner) auth.Beginner { return racingBeginner{Beginner: b, m: m} })
					statuses, left := f.raceTheLastTwo(t, first, second)

					if left != 1 {
						t.Fatalf("round %d: %v leave %d active Pockets, want exactly 1", round, statuses, left)
					}
					won, refused := 0, 0
					for _, s := range statuses {
						switch s {
						case "204", "200":
							won++
						case "409 LAST_POCKET":
							refused++
						default:
							t.Errorf("round %d: answered %s, want a success or 409 LAST_POCKET", round, s)
						}
					}
					if won != 1 || refused != 1 {
						t.Errorf("round %d: %v, want one success and one 409 LAST_POCKET", round, statuses)
					}
				}
			})
		}
	}
}

// The red proof, kept: the same race with the Household lock taken off (the
// statement is run without its lock clause, so nothing orders the two) leaves the
// Household with no active Pocket. If this ever stops failing the guard, the
// lock is no longer what holds the invariant and the handler needs a rethink,
// not a quiet deletion.
func TestWithoutTheHouseholdLockTwoRemovalsLeaveNoActivePocket(t *testing.T) {
	for _, first := range removals {
		for _, second := range removals {
			t.Run(first.name+" and "+second.name, func(t *testing.T) {
				m := newMeet(2)
				f := newFixtureTx(t, func(b auth.Beginner) auth.Beginner { return racingBeginner{Beginner: b, m: m, dropLock: true} })
				statuses, left := f.raceTheLastTwo(t, first, second)

				if left != 0 {
					t.Errorf("%v leave %d active Pockets: without the lock both removals were expected to pass their count and leave none", statuses, left)
				}
			})
		}
	}
}

// ---- the lock's mode ---------------------------------------------------------
//
// The removals' locks are FOR NO KEY UPDATE, not FOR UPDATE, because the FK
// check of any insert takes FOR KEY SHARE on its parent row, which FOR UPDATE
// blocks. A writer that holds FOR SHARE on a Pocket (an Expense create, the next
// slice) and then inserts would wait at its FK check behind a removal that holds
// the Household and waits on that Pocket: a 40P01 deadlock, answered 500.

// holdingTheHouseholdLock opens a transaction that has taken the removals' real
// Household lock, and releases it with the test.
func (f fixture) holdingTheHouseholdLock(t *testing.T, householdID pgtype.UUID) pgx.Tx {
	t.Helper()
	tx, err := f.tdb.Pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := f.tdb.Queries.WithTx(tx).LockHousehold(context.Background(), householdID); err != nil {
		t.Fatalf("LockHousehold: %v", err)
	}
	return tx
}

func TestAnInsertCarryingAForeignKeyToTheHouseholdIsNotBlockedByARemovalsLock(t *testing.T) {
	f := newFixture(t)
	ctx, householdID, _ := f.lastOnly(t)
	f.holdingTheHouseholdLock(t, householdID)

	// A Pocket create inserts a row whose household_id references the locked
	// row. It must finish while the lock is held, not wait for it.
	done := make(chan api.CreatePocketResponseObject, 1)
	go func() {
		resp, err := f.handlers.CreatePocket(ctx, api.CreatePocketRequestObject{
			Body: &api.CreatePocketRequest{Id: newID(t), PocketDisplayName: "Wallet"},
		})
		if err != nil {
			t.Errorf("CreatePocket returned an error: %v", err)
		}
		done <- resp
	}()
	select {
	case resp := <-done:
		assertCreated(t, resp)
	case <-time.After(3 * time.Second):
		t.Fatal("a Pocket create is blocked behind a removal's Household lock: the lock takes more than FOR NO KEY UPDATE")
	}
}

// The control for the test above: a FOR UPDATE on the same row does block the
// same insert, so a pass there is the mode and not an insert that never needed
// the row.
func TestControlAForUpdateLockOnTheHouseholdBlocksTheSameInsert(t *testing.T) {
	f := newFixture(t)
	_, householdID, _ := f.lastOnly(t)
	tx, err := f.tdb.Pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(context.Background(), `SELECT id FROM households WHERE id = $1 FOR UPDATE`, householdID); err != nil {
		t.Fatal(err)
	}

	blocked, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err = f.tdb.Pool.Exec(blocked,
		`INSERT INTO pockets (id, household_id, pocket_display_name) VALUES ($1, $2, 'Wallet')`, newID(t), householdID)
	if err == nil {
		t.Fatal("the insert finished under FOR UPDATE: the control does not discriminate")
	}
}

// The deadlock itself, with the real queries: a writer holds FOR SHARE on a
// Pocket and inserts a row that references the Household, while a removal
// holds the Household and waits on that Pocket. Both must finish.
func TestAShareLockingWriterAndARemovalDoNotDeadlock(t *testing.T) {
	f := newFixture(t)
	_, householdID, p := f.lastOnly(t)
	pocketID := pgtype.UUID{Bytes: p, Valid: true}
	bg := context.Background()

	writer, err := f.tdb.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Rollback(bg) })
	if _, err := writer.Exec(bg, `SELECT 1 FROM pockets WHERE id = $1 FOR SHARE`, pocketID); err != nil {
		t.Fatal(err)
	}

	removal := make(chan error, 1)
	go func() {
		tx, err := f.tdb.Pool.Begin(bg)
		if err != nil {
			removal <- err
			return
		}
		defer func() { _ = tx.Rollback(bg) }()
		q := f.tdb.Queries.WithTx(tx)
		if _, err := q.LockHousehold(bg, householdID); err != nil {
			removal <- err
			return
		}
		// Waits here until the writer commits.
		_, err = q.LockPocket(bg, db.LockPocketParams{ID: pocketID, HouseholdID: householdID})
		removal <- err
	}()
	// Let the removal reach the Pocket lock and wait on the writer's FOR SHARE.
	time.Sleep(300 * time.Millisecond)

	if _, err := writer.Exec(bg,
		`INSERT INTO pockets (id, household_id, pocket_display_name) VALUES ($1, $2, 'Wallet')`, newID(t), householdID); err != nil {
		t.Fatalf("the writer's insert failed (a deadlock is 40P01): %v", err)
	}
	if err := writer.Commit(bg); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-removal:
		if err != nil {
			t.Errorf("the removal failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the removal never got the Pocket after the writer committed")
	}
}

// ---- the statements and their failures ---------------------------------------

type recordedStatement struct {
	sql  string
	args []any
}

type recordingTx struct {
	pgx.Tx
	seen *[]recordedStatement
}

func (tx recordingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	*tx.seen = append(*tx.seen, recordedStatement{sql, args})
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx recordingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	*tx.seen = append(*tx.seen, recordedStatement{sql, args})
	return tx.Tx.Exec(ctx, sql, args...)
}

type recordingBeginner struct {
	auth.Beginner
	seen *[]recordedStatement
}

func (b recordingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return recordingTx{Tx: tx, seen: b.seen}, nil
}

// The guarded transaction's statements, in order, each carrying the session's
// Household (and the path's id where it names a Pocket): the lock, the count
// as a statement of its own, then the write.
func TestTheGuardedTransactionRunsLockCountWriteInOrderScopedToTheHousehold(t *testing.T) {
	for _, tc := range []struct {
		name    string
		write   string
		run     func(t *testing.T, f fixture, ctx context.Context, id uuid.UUID)
		wantArg func(args []any) bool // the guarded write's own arguments
	}{
		{"delete", softDeleteQuery, func(t *testing.T, f fixture, ctx context.Context, id uuid.UUID) { assertDeleted(t, f.del(ctx, t, id)) }, nil},
		{"deactivate", updatePocketQuery, func(t *testing.T, f fixture, ctx context.Context, id uuid.UUID) {
			assertUpdated(t, f.update(ctx, t, id, ptr("  Renamed "), ptr(false)))
		}, func(args []any) bool {
			// UpdatePocketParams, in the order of the statement: the trimmed
			// name, active = false (a value, not "absent"), the id, the Household.
			if len(args) != 4 {
				return false
			}
			name, okName := args[0].(*string)
			active, okActive := args[1].(*bool)
			return okName && name != nil && *name == "Renamed" && okActive && active != nil && !*active
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markers := []string{lockHouseholdQuery, lockPocketQuery, countsOtherPockets, tc.write}
			var seen []recordedStatement
			f := newFixtureTx(t, func(b auth.Beginner) auth.Beginner { return recordingBeginner{Beginner: b, seen: &seen} })
			ctx, householdID, a := f.lastOnly(t)
			assertCreated(t, f.create(ctx, t, newID(t), "Wallet"))
			seen = nil

			tc.run(t, f, ctx, a)

			var order []int
			for _, r := range seen {
				for i, m := range markers {
					if !strings.Contains(r.sql, m) {
						continue
					}
					order = append(order, i)
					var hasHousehold, hasID bool
					for _, arg := range r.args {
						if arg == householdID {
							hasHousehold = true
						}
						if p, ok := arg.(pgtype.UUID); ok && p.Bytes == a {
							hasID = true
						}
					}
					if !hasHousehold {
						t.Errorf("the statement with %q did not carry the session's Household: %v", m, r.args)
					}
					if i >= 1 && !hasID {
						t.Errorf("the statement with %q did not carry the path's id: %v", m, r.args)
					}
				}
			}
			if tc.wantArg != nil {
				var write []any
				for _, r := range seen {
					if strings.Contains(r.sql, tc.write) {
						write = r.args
					}
				}
				if !tc.wantArg(write) {
					t.Errorf("the guarded write's arguments are %#v, want the trimmed name %q and active = false", write, "Renamed")
				}
			}
			if fmt.Sprint(order) != "[0 1 2 3]" {
				t.Errorf("statements ran in order %v, want the Household lock, the Pocket lock, the count, then the write ([0 1 2 3])", order)
			}
			for _, r := range seen[:min(2, len(seen))] {
				if !strings.Contains(r.sql, householdLockClause) {
					t.Errorf("the lock statement is %q, want %s", r.sql, householdLockClause)
				}
			}
		})
	}
}

type failingTx struct {
	pgx.Tx
	on         string
	zeroRows   bool
	failCommit bool
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

func (tx failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.on != "" && matchesAny(sql, tx.on) {
		return errRow{errOutage}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx failingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.zeroRows && matchesAny(sql, softDeleteQuery+"|"+updatePocketQuery) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	if tx.on != "" && matchesAny(sql, tx.on) {
		return pgconn.CommandTag{}, errOutage
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func (tx failingTx) Commit(ctx context.Context) error {
	if tx.failCommit {
		_ = tx.Rollback(ctx)
		return errOutage
	}
	return tx.Tx.Commit(ctx)
}

type failingBeginner struct {
	auth.Beginner
	failBegin bool
	tx        failingTx
}

func (b failingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	if b.failBegin {
		return nil, errOutage
	}
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	t := b.tx
	t.Tx = tx
	return t, nil
}

// A failure at any step of the guarded transaction is a 500 that changes
// nothing and logs no name. The 404 is the Pocket lock finding no row, and only
// that; LAST_POCKET is the count saying so, and only that.
func TestAFailureAnywhereInTheGuardedTransactionIsA500AndChangesNothing(t *testing.T) {
	for _, step := range []struct {
		name string
		b    failingBeginner
	}{
		{"begin", failingBeginner{failBegin: true}},
		{"the Household lock", failingBeginner{tx: failingTx{on: lockHouseholdQuery}}},
		{"the Pocket lock", failingBeginner{tx: failingTx{on: lockPocketQuery}}},
		{"the count", failingBeginner{tx: failingTx{on: countsOtherPockets}}},
		{"the write", failingBeginner{tx: failingTx{on: softDeleteQuery + "|" + updatePocketQuery}}},
		{"commit", failingBeginner{tx: failingTx{failCommit: true}}},
	} {
		for _, op := range removals {
			t.Run(step.name+", "+op.name, func(t *testing.T) {
				b := step.b
				f := newFixtureTx(t, func(inner auth.Beginner) auth.Beginner { b.Beginner = inner; return b })
				ctx, _, a := f.lastOnly(t)
				assertCreated(t, f.create(ctx, t, newID(t), "Wallet"))
				before := f.row(t, a)
				var buf bytes.Buffer
				previous := slog.Default()
				slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
				t.Cleanup(func() { slog.SetDefault(previous) })

				if got := op.run(t, f, ctx, a); got != "500" {
					t.Errorf("%s = %s, want 500", op.name, got)
				}
				if f.row(t, a) != before {
					t.Error("a failed removal left the row changed")
				}
				if strings.Contains(buf.String(), "Cash") || strings.Contains(buf.String(), "Wallet") {
					t.Errorf("the log leaks a Pocket's name:\n%s", buf.String())
				}
				if buf.Len() == 0 {
					t.Error("nothing was logged")
				}
			})
		}
	}
}

// The write touching no row after its own lock is a failure, not a 404.
func TestAWriteThatTouchesNoRowAfterTheLockIsA500(t *testing.T) {
	f := newFixtureTx(t, func(inner auth.Beginner) auth.Beginner {
		return failingBeginner{Beginner: inner, tx: failingTx{zeroRows: true}}
	})
	ctx, _, a := f.lastOnly(t)
	assertCreated(t, f.create(ctx, t, newID(t), "Wallet"))
	slog.SetDefault(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	if got := deleteStatus(f.del(ctx, t, a)); got != "500" {
		t.Errorf("DeletePocket = %s, want 500", got)
	}
	if f.isDeleted(t, a) {
		t.Error("the Pocket was deleted")
	}
}
