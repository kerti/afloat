package expense_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// DELETE /expenses/{id} and POST /expenses/{id}/restore (#140 X10, R11) against
// a real Postgres, at the handler. The shape of the id is the binder's and is
// proven through the router (internal/httpserver) and the conformance suite.

func (f fixture) del(ctx context.Context, t *testing.T, id pgtype.UUID) api.DeleteExpenseResponseObject {
	t.Helper()
	resp, err := f.handlers.DeleteExpense(ctx, api.DeleteExpenseRequestObject{Id: id.Bytes})
	if err != nil {
		t.Fatalf("DeleteExpense returned an error: %v", err)
	}
	return resp
}

func (f fixture) restore(ctx context.Context, t *testing.T, id pgtype.UUID) api.RestoreExpenseResponseObject {
	t.Helper()
	resp, err := f.handlers.RestoreExpense(ctx, api.RestoreExpenseRequestObject{Id: id.Bytes})
	if err != nil {
		t.Fatalf("RestoreExpense returned an error: %v", err)
	}
	return resp
}

// row is one Expense's every column as JSON, deleted or not.
func (f fixture) row(t *testing.T, id pgtype.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(to_jsonb(e)::text, '') FROM expenses e WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read expense: %v", err)
	}
	return s
}

func (f fixture) deletedAt(t *testing.T, id pgtype.UUID) pgtype.Timestamptz {
	t.Helper()
	var at pgtype.Timestamptz
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT deleted_at FROM expenses WHERE id = $1`, id).Scan(&at); err != nil {
		t.Fatalf("read deleted_at: %v", err)
	}
	return at
}

// bin is the Recycle Bin as the handler answers it: ?deleted=true.
func (f fixture) bin(ctx context.Context, t *testing.T, limit *int, after *string) api.ExpenseList {
	t.Helper()
	resp := f.binResp(ctx, t, api.ListExpensesParams{Limit: limit, After: after, Deleted: ptr(true)})
	got, ok := resp.(api.ListExpenses200JSONResponse)
	if !ok {
		t.Fatalf("ListExpenses?deleted=true = %s, want 200", status(resp))
	}
	return api.ExpenseList(got)
}

func (f fixture) binResp(ctx context.Context, t *testing.T, p api.ListExpensesParams) api.ListExpensesResponseObject {
	t.Helper()
	resp, err := f.handlers.ListExpenses(ctx, api.ListExpensesRequestObject{Params: p})
	if err != nil {
		t.Fatalf("ListExpenses returned an error: %v", err)
	}
	return resp
}

// ---- delete -----------------------------------------------------------------------

// A delete is soft: 204, the row stays with deleted_at set, and nothing else
// about it changes. It is gone from the live list, from GET and from PATCH, and
// it is in the bin.
func TestDeleteIsSoftAndTheRowLeavesEveryLiveRead(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	id, created := f.stored(ctx, t, pocket, category)
	before := f.row(t, id)

	assertStatus(t, f.del(ctx, t, id), "204")

	if n := f.count(t); n != 1 {
		t.Fatalf("%d rows, want the one that was deleted to stay", n)
	}
	at := f.deletedAt(t, id)
	if !at.Valid {
		t.Fatal("deleted_at is not set")
	}
	// Every column but deleted_at and updated_at is as it was.
	var same bool
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT (to_jsonb(e) - 'deleted_at' - 'updated_at') = ($2::jsonb - 'deleted_at' - 'updated_at') FROM expenses e WHERE id = $1`,
		id, before).Scan(&same); err != nil || !same {
		t.Errorf("a column other than deleted_at and updated_at changed (err %v)", err)
	}
	if page := f.list(ctx, t, nil, nil); len(page.Items) != 0 || uncount(page) != 0 {
		t.Errorf("the live list holds %d row(s), count %d; want none", len(page.Items), uncount(page))
	}
	assertStatus(t, f.get(ctx, t, id), "NOT_FOUND")
	resp, err := f.handlers.UpdateExpense(ctx, api.UpdateExpenseRequestObject{Id: id.Bytes, Body: &api.UpdateExpenseRequest{}})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, resp, "NOT_FOUND")

	bin := f.bin(ctx, t, nil, nil)
	if len(bin.Items) != 1 || bin.Items[0].Id != created.Id {
		t.Fatalf("the bin holds %v, want the deleted Expense", ids(bin))
	}
}

// Deleting a deleted Expense is 204 again and changes no column at all: the
// first deletion instant is kept, and updated_at does not move.
func TestARepeatDeleteIs204AndKeepsTheFirstInstant(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})

	assertStatus(t, f.del(ctx, t, id), "204")
	first := f.row(t, id)
	// A repeat a moment later: the statement's now() would differ from the first
	// if it were written.
	f.exec(t, `SELECT pg_sleep(0.05)`)
	assertStatus(t, f.del(ctx, t, id), "204")
	assertStatus(t, f.del(ctx, t, id), "204")

	if got := f.row(t, id); got != first {
		t.Errorf("a repeat delete changed the row:\n first %s\n after %s", first, got)
	}
}

// Delete moves updated_at, and only the delete that did the work.
func TestDeleteMovesUpdatedAtOnce(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})
	f.exec(t, `UPDATE expenses SET updated_at = '2026-01-01T00:00:00Z'`)

	assertStatus(t, f.del(ctx, t, id), "204")
	var moved bool
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT updated_at > '2026-01-01T00:00:00Z' AND updated_at = deleted_at FROM expenses`).Scan(&moved); err != nil || !moved {
		t.Errorf("updated_at after the delete: moved to deleted_at = %v (err %v)", moved, err)
	}
}

// A refund is deleted as any Expense is and keeps its sign in the bin.
func TestARefundIsDeletedAndKeepsItsSignInTheBin(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	req := body(pocket)
	req.Amount = "-450000"
	assertCreated(t, f.create(ctx, t, req))
	assertStatus(t, f.del(ctx, t, pgtype.UUID{Bytes: req.Id, Valid: true}), "204")

	bin := f.bin(ctx, t, nil, nil)
	if len(bin.Items) != 1 || bin.Items[0].Amount != "-450000.0000" {
		t.Errorf("the bin holds %s, want the refund at four decimals", wire(t, bin))
	}
}

// An unknown id and another Household's are the one 404, byte for byte, and the
// other Household's Expense, live or deleted, is untouched.
func TestDeleteOfAnUnknownOrForeignIdIsTheOne404(t *testing.T) {
	f := newFixture(t)
	_, _, ctx := f.member(t, "Home", "a@example.com")
	theirHousehold, theirPocket, _ := f.member(t, "Other", "b@example.com")
	live, _ := f.stored(f.userIn(t, theirHousehold, "c@example.com"), t, theirPocket, pgtype.UUID{})
	gone := f.tdb.CreateExpense(t, theirHousehold, theirPocket, testutil.ExpenseOpts{Deleted: true})
	before := f.rows(t)

	unknown := wire(t, f.del(ctx, t, testutil.NewUUID(t)))
	for name, id := range map[string]pgtype.UUID{"live": live, "deleted": gone} {
		resp := f.del(ctx, t, id)
		assertStatus(t, resp, "NOT_FOUND")
		if got := wire(t, resp); got != unknown {
			t.Errorf("another Household's %s id answered %s, an unknown one %s", name, got, unknown)
		}
	}
	if after := f.rows(t); after != before {
		t.Errorf("a refused delete changed a row:\n%s\n%s", before, after)
	}
}

// ---- restore ----------------------------------------------------------------------

// The Undo: restore brings back the Expense with every property as it was, to
// the place in the list it had, and empties the bin of it.
func TestRestoreIsTheUndoOfDelete(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	id, created := f.stored(ctx, t, pocket, category)
	assertCreated(t, f.create(ctx, t, body(pocket)))
	wantOrder := ids(f.list(ctx, t, nil, nil))

	assertStatus(t, f.del(ctx, t, id), "204")
	resp := f.restore(ctx, t, id)
	assertStatus(t, resp, "200")
	got := api.Expense(resp.(api.RestoreExpense200JSONResponse))
	if wire(t, got) != wire(t, created) {
		t.Errorf("restore answered %s, want the Expense as it was %s", wire(t, got), wire(t, created))
	}
	if strings.Contains(wire(t, got), "deleted_at") {
		t.Errorf("a live Expense carries deleted_at: %s", wire(t, got))
	}
	if f.deletedAt(t, id).Valid {
		t.Error("deleted_at is still set")
	}
	if order := ids(f.list(ctx, t, nil, nil)); len(order) != len(wantOrder) || order[0] != wantOrder[0] || order[1] != wantOrder[1] {
		t.Errorf("the restored Expense is at %v, want the order it had, %v", order, wantOrder)
	}
	if bin := f.bin(ctx, t, nil, nil); len(bin.Items) != 0 {
		t.Errorf("the bin still holds %v", ids(bin))
	}
	assertStatus(t, f.get(ctx, t, id), "200")
}

// Restoring one that is not deleted is 200 as it stands, and writes nothing.
func TestRestoreOfALiveExpenseIs200AndChangesNothing(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, created := f.stored(ctx, t, pocket, pgtype.UUID{})
	before := f.row(t, id)

	for range 2 {
		resp := f.restore(ctx, t, id)
		assertStatus(t, resp, "200")
		if got := wire(t, resp); got != wire(t, created) {
			t.Errorf("restore of a live Expense answered %s, want %s", got, wire(t, created))
		}
	}
	if got := f.row(t, id); got != before {
		t.Errorf("restoring a live Expense changed the row:\n%s\n%s", before, got)
	}
}

// Restoring twice is 200 twice, and the second changes no column (updated_at
// moves only for the request that restored).
func TestRestoringTwiceIs200TwiceAndOnlyTheFirstWrites(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})
	assertStatus(t, f.del(ctx, t, id), "204")

	assertStatus(t, f.restore(ctx, t, id), "200")
	first := f.row(t, id)
	f.exec(t, `SELECT pg_sleep(0.05)`)
	assertStatus(t, f.restore(ctx, t, id), "200")
	if got := f.row(t, id); got != first {
		t.Errorf("the second restore changed the row:\n%s\n%s", first, got)
	}
}

// A restored Expense can be edited and deleted again, and the second delete
// takes a fresh instant: it is a new deletion.
func TestARestoredExpenseIsAnOrdinaryOneAgain(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})
	assertStatus(t, f.del(ctx, t, id), "204")
	firstAt := f.deletedAt(t, id)
	assertStatus(t, f.restore(ctx, t, id), "200")

	resp, err := f.handlers.UpdateExpense(ctx, api.UpdateExpenseRequestObject{Id: id.Bytes, Body: &api.UpdateExpenseRequest{Description: valueOf("Dinner")}})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, resp, "200")

	assertStatus(t, f.del(ctx, t, id), "204")
	if second := f.deletedAt(t, id); !second.Time.After(firstAt.Time) {
		t.Errorf("a delete after a restore kept the old instant %v, want a later one than %v", second.Time, firstAt.Time)
	}
}

// The R11 ruling, "no new restore refusal": an Expense whose Pocket or Category
// was deactivated while it sat in the bin is restored, still labelled by it.
func TestRestoreIntoADeactivatedPocketAndCategoryIsStill200(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	id, created := f.stored(ctx, t, pocket, category)
	assertStatus(t, f.del(ctx, t, id), "204")
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, pocket)
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, category)

	resp := f.restore(ctx, t, id)
	assertStatus(t, resp, "200")
	got := api.Expense(resp.(api.RestoreExpense200JSONResponse))
	if got.PocketId != created.PocketId || got.CategoryId == nil || *got.CategoryId != *created.CategoryId {
		t.Errorf("the restored Expense is on %v / %v, want %v / %v", got.PocketId, got.CategoryId, created.PocketId, created.CategoryId)
	}
}

// A restore finding a deleted Pocket or Category under the Expense (which
// POCKET_IN_USE and CATEGORY_IN_USE make impossible, deleted Expenses counting)
// refuses to resurrect the reference: 500, nothing written, the Expense still in
// the bin. The state is made here by soft-deleting the row with SQL, which is
// the guard no longer being there.
func TestRestoreNeverBringsBackAReferenceToADeletedRow(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"pocket", `UPDATE pockets SET deleted_at = now() WHERE id = $1`},
		{"category", `UPDATE categories SET deleted_at = now() WHERE id = $1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			household, pocket, ctx := f.member(t, "Home", "a@example.com")
			category := f.tdb.CreateCategory(t, household, "Food", "daily")
			id, _ := f.stored(ctx, t, pocket, category)
			assertStatus(t, f.del(ctx, t, id), "204")
			target := pocket
			if tc.name == "category" {
				target = category
			}
			f.exec(t, tc.sql, target)
			before := f.row(t, id)

			assertStatus(t, f.restore(ctx, t, id), "500")
			if got := f.row(t, id); got != before {
				t.Errorf("a refused restore changed the row:\n%s\n%s", before, got)
			}
			if bin := f.bin(ctx, t, nil, nil); len(bin.Items) != 1 {
				t.Errorf("the bin holds %d, want the Expense to stay in it", len(bin.Items))
			}
		})
	}
}

// Unknown and another Household's ids are the one 404, byte for byte, whether
// the foreign Expense is live or deleted, and nothing of theirs changes.
func TestRestoreOfAnUnknownOrForeignIdIsTheOne404(t *testing.T) {
	f := newFixture(t)
	_, _, ctx := f.member(t, "Home", "a@example.com")
	theirHousehold, theirPocket, _ := f.member(t, "Other", "b@example.com")
	live, _ := f.stored(f.userIn(t, theirHousehold, "c@example.com"), t, theirPocket, pgtype.UUID{})
	gone := f.tdb.CreateExpense(t, theirHousehold, theirPocket, testutil.ExpenseOpts{Deleted: true})
	before := f.rows(t)

	unknown := wire(t, f.restore(ctx, t, testutil.NewUUID(t)))
	for name, id := range map[string]pgtype.UUID{"live": live, "deleted": gone} {
		resp := f.restore(ctx, t, id)
		assertStatus(t, resp, "NOT_FOUND")
		if got := wire(t, resp); got != unknown {
			t.Errorf("another Household's %s id answered %s, an unknown one %s", name, got, unknown)
		}
	}
	if after := f.rows(t); after != before {
		t.Errorf("a refused restore changed a row:\n%s\n%s", before, after)
	}
}

// No User in the context is 401, never a statement with a zero household_id.
func TestDeleteAndRestoreWithNoSessionAre401(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})
	before := f.rows(t)

	assertStatus(t, f.del(context.Background(), t, id), "401")
	assertStatus(t, f.restore(context.Background(), t, id), "401")
	if after := f.rows(t); after != before {
		t.Error("an unauthenticated request changed a row")
	}
}

// ---- the Recycle Bin --------------------------------------------------------------

// insertDeleted writes a deleted Expense with the instant of its deletion chosen
// by the test, which is the key the bin is ordered by.
func (f fixture) insertDeleted(t *testing.T, household, pocket pgtype.UUID, n int, deletedAt string) pgtype.UUID {
	t.Helper()
	id := pgtype.UUID{Bytes: sid(n), Valid: true}
	f.exec(t, `INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id, deleted_at)
		VALUES ($1, $2, '2026-11-20', 25000, 'IDR', $3, $4::timestamptz)`, id, household, pocket, deletedAt)
	return id
}

// The bin lists deleted rows only, newest deletion first, and each carries its
// deleted_at; it omits uncategorised_count, which a live read never does.
func TestTheBinListsDeletedRowsNewestDeletionFirst(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	theirs, theirPocket, _ := f.member(t, "Other", "b@example.com")
	f.insert(t, household, pocket, seed{id: sid(9), occurredOn: "2026-11-25", capturedAt: "2026-11-25T01:00:00Z"})
	f.insertDeleted(t, household, pocket, 1, "2026-11-24T10:00:00Z")
	f.insertDeleted(t, household, pocket, 2, "2026-11-25T10:00:00Z")
	f.insertDeleted(t, household, pocket, 3, "2026-11-23T10:00:00Z")
	f.insertDeleted(t, theirs, theirPocket, 4, "2026-11-26T10:00:00Z")

	bin := f.bin(ctx, t, nil, nil)
	want := []string{sid(2).String(), sid(1).String(), sid(3).String()}
	var got []string
	for _, e := range bin.Items {
		got = append(got, e.Id.String())
		if e.DeletedAt == nil {
			t.Errorf("%s carries no deleted_at", e.Id)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the bin is %v, want %v", got, want)
	}
	if bin.UncategorisedCount != nil {
		t.Errorf("the bin carries uncategorised_count %d", *bin.UncategorisedCount)
	}
	if w := wire(t, bin); strings.Contains(w, "uncategorised_count") || strings.Contains(w, `"next"`) {
		t.Errorf("the bin's wire has a key it must omit: %s", w)
	}
	if !strings.Contains(wire(t, bin.Items[0]), `"deleted_at":"2026-11-25T10:00:00Z"`) {
		t.Errorf("deleted_at is not on the wire in UTC: %s", wire(t, bin.Items[0]))
	}

	live := f.list(ctx, t, nil, nil)
	if len(live.Items) != 1 || live.Items[0].DeletedAt != nil || strings.Contains(wire(t, live), "deleted_at") {
		t.Errorf("the live list shows a deleted row or a deleted_at: %s", wire(t, live))
	}
	if uncount(live) != 1 {
		t.Errorf("the live list's uncategorised_count = %d, want 1 (and present)", uncount(live))
	}
}

// An empty bin is {"items":[]}: never null, no count, no cursor.
func TestAnEmptyBinIsItemsAndNothingElse(t *testing.T) {
	f := newFixture(t)
	_, _, ctx := f.member(t, "Home", "a@example.com")
	if got := wire(t, f.bin(ctx, t, nil, nil)); got != `{"items":[]}` {
		t.Errorf("wire = %s, want {\"items\":[]}", got)
	}
}

// Keyset paging in the bin: pages of two over five rows, two of which share a
// deletion instant that falls on a page boundary (id DESC breaks it), repeat
// nothing and skip nothing, and the last page has no next.
func TestTheBinPagesWithoutRepeatingOrSkipping(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	f.insertDeleted(t, household, pocket, 1, "2026-11-20T10:00:00Z")
	f.insertDeleted(t, household, pocket, 2, "2026-11-22T10:00:00Z")
	f.insertDeleted(t, household, pocket, 3, "2026-11-22T10:00:00Z")
	f.insertDeleted(t, household, pocket, 4, "2026-11-22T10:00:00Z")
	f.insertDeleted(t, household, pocket, 5, "2026-11-23T10:00:00.123456Z")

	want := []string{sid(5).String(), sid(4).String(), sid(3).String(), sid(2).String(), sid(1).String()}
	for _, size := range []int{1, 2, 3, 5, 100} {
		var got []string
		var after *string
		pages := 0
		for {
			page := f.bin(ctx, t, &size, after)
			pages++
			for _, e := range page.Items {
				got = append(got, e.Id.String())
			}
			if page.Next == nil {
				break
			}
			if len(page.Items) != size {
				t.Fatalf("size %d: a page with a next holds %d rows", size, len(page.Items))
			}
			after = page.Next
			if pages > 10 {
				t.Fatal("the walk does not end")
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("size %d: walked %v, want %v", size, got, want)
		}
	}
}

// A page that holds every row has no next, and microseconds survive the cursor.
func TestTheBinCursorKeepsMicroseconds(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	f.insertDeleted(t, household, pocket, 1, "2026-11-23T10:00:00.000001Z")
	f.insertDeleted(t, household, pocket, 2, "2026-11-23T10:00:00.000002Z")
	one := 1
	first := f.bin(ctx, t, &one, nil)
	if len(first.Items) != 1 || first.Items[0].Id.String() != sid(2).String() || first.Next == nil {
		t.Fatalf("first page %s", wire(t, first))
	}
	second := f.bin(ctx, t, &one, first.Next)
	if len(second.Items) != 1 || second.Items[0].Id.String() != sid(1).String() || second.Next != nil {
		t.Errorf("second page %s, want the earlier microsecond and no next", wire(t, second))
	}
}

// The two lists' cursors are not interchangeable: each names a position in its
// own order.
func TestTheTwoListsCursorsAreNotInterchangeable(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	f.insert(t, household, pocket, seed{id: sid(7), occurredOn: "2026-11-21", capturedAt: "2026-11-21T01:00:00Z"})
	f.insert(t, household, pocket, seed{id: sid(8), occurredOn: "2026-11-22", capturedAt: "2026-11-22T01:00:00Z"})
	f.insertDeleted(t, household, pocket, 1, "2026-11-20T10:00:00Z")
	f.insertDeleted(t, household, pocket, 2, "2026-11-21T10:00:00Z")
	one := 1
	liveNext := f.list(ctx, t, &one, nil).Next
	binNext := f.bin(ctx, t, &one, nil).Next
	if liveNext == nil || binNext == nil {
		t.Fatal("a page of one over two rows has no cursor")
	}

	assertStatus(t, f.binResp(ctx, t, api.ListExpensesParams{After: liveNext, Deleted: ptr(true)}), "VALIDATION after invalid")
	assertStatus(t, f.binResp(ctx, t, api.ListExpensesParams{After: binNext}), "VALIDATION after invalid")
	assertStatus(t, f.binResp(ctx, t, api.ListExpensesParams{After: ptr("garbage"), Deleted: ptr(true)}), "VALIDATION after invalid")
	assertStatus(t, f.binResp(ctx, t, api.ListExpensesParams{After: ptr(""), Deleted: ptr(true)}), "VALIDATION after invalid")
	assertStatus(t, f.binResp(ctx, t, api.ListExpensesParams{After: binNext, Deleted: ptr(true)}), "200")
}

// The bin has no filter: ?uncategorised=true beside ?deleted=true is refused,
// never answered as an unfiltered bin. A bad cursor competes by (field, rule)
// and `after` is the least. The false spellings are the parameter absent.
func TestTheBinHasNoFilter(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	f.insertDeleted(t, household, pocket, 1, "2026-11-20T10:00:00Z")

	assertStatus(t, f.binResp(ctx, t, api.ListExpensesParams{Deleted: ptr(true), Uncategorised: ptr(true)}), "VALIDATION uncategorised invalid")
	assertStatus(t, f.binResp(ctx, t, api.ListExpensesParams{Deleted: ptr(true), Uncategorised: ptr(true), After: ptr("garbage")}), "VALIDATION after invalid")
	assertStatus(t, f.binResp(ctx, t, api.ListExpensesParams{Deleted: ptr(true), Uncategorised: ptr(false)}), "200")

	// deleted=false is the live list, uncategorised and all.
	resp := f.binResp(ctx, t, api.ListExpensesParams{Deleted: ptr(false), Uncategorised: ptr(true)})
	assertStatus(t, resp, "200")
	if page := api.ExpenseList(resp.(api.ListExpenses200JSONResponse)); len(page.Items) != 0 || uncount(page) != 0 {
		t.Errorf("deleted=false answered %s, want the (empty) live list", wire(t, page))
	}
}

// A restored row leaves the bin, and a row deleted again takes the top of it.
func TestARestoredRowLeavesTheBinAndADeletedOneTakesTheTop(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	a := f.insertDeleted(t, household, pocket, 1, "2026-11-20T10:00:00Z")
	b := f.insertDeleted(t, household, pocket, 2, "2026-11-21T10:00:00Z")

	assertStatus(t, f.restore(ctx, t, b), "200")
	if got := ids(f.bin(ctx, t, nil, nil)); len(got) != 1 || got[0] != a.Bytes {
		t.Errorf("after restoring b the bin is %v, want only a", got)
	}
	assertStatus(t, f.del(ctx, t, b), "204")
	if got := ids(f.bin(ctx, t, nil, nil)); len(got) != 2 || got[0] != b.Bytes {
		t.Errorf("after deleting b again the bin is %v, want b first", got)
	}
}

// With no User in the context, the bin is 401 too.
func TestTheBinWithNoSessionIs401(t *testing.T) {
	f := newFixture(t)
	assertStatus(t, f.binResp(context.Background(), t, api.ListExpensesParams{Deleted: ptr(true)}), "401")
}
