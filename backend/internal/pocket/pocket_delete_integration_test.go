package pocket_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// DeletePocket and RestorePocket against a real Postgres, no t.Parallel
// (ADR-0004). Delete is soft, idempotent and keeps the first deletion instant;
// restore is its Undo; both are one statement tenant-scoped in SQL.

func (f fixture) del(ctx context.Context, t *testing.T, id uuid.UUID) api.DeletePocketResponseObject {
	t.Helper()
	resp, err := f.handlers.DeletePocket(ctx, api.DeletePocketRequestObject{Id: id})
	if err != nil {
		t.Fatalf("DeletePocket returned an error: %v", err)
	}
	return resp
}

func (f fixture) restore(ctx context.Context, t *testing.T, id uuid.UUID) api.RestorePocketResponseObject {
	t.Helper()
	resp, err := f.handlers.RestorePocket(ctx, api.RestorePocketRequestObject{Id: id})
	if err != nil {
		t.Fatalf("RestorePocket returned an error: %v", err)
	}
	return resp
}

func deleteStatus(resp api.DeletePocketResponseObject) string {
	switch r := resp.(type) {
	case api.DeletePocket204Response:
		return "204"
	case api.DeletePocket400JSONResponse:
		return "400"
	case api.DeletePocket401JSONResponse:
		return "401"
	case api.DeletePocket404JSONResponse:
		return "404 " + string(r.Code)
	case api.DeletePocket500JSONResponse:
		return "500"
	}
	return fmt.Sprintf("%T", resp)
}

func restoreStatus(resp api.RestorePocketResponseObject) string {
	switch r := resp.(type) {
	case api.RestorePocket200JSONResponse:
		return "200"
	case api.RestorePocket400JSONResponse:
		return "400"
	case api.RestorePocket401JSONResponse:
		return "401"
	case api.RestorePocket404JSONResponse:
		return "404 " + string(r.Code)
	case api.RestorePocket409JSONResponse:
		return "409 " + string(r.Code)
	case api.RestorePocket500JSONResponse:
		return "500"
	}
	return fmt.Sprintf("%T", resp)
}

func assertDeleted(t *testing.T, resp api.DeletePocketResponseObject) {
	t.Helper()
	if got := deleteStatus(resp); got != "204" {
		t.Fatalf("DeletePocket = %s, want 204", got)
	}
}

func assertRestored(t *testing.T, resp api.RestorePocketResponseObject) api.Pocket {
	t.Helper()
	got, is := resp.(api.RestorePocket200JSONResponse)
	if !is {
		t.Fatalf("RestorePocket = %s, want 200", restoreStatus(resp))
	}
	return api.Pocket(got)
}

func wireOfDelete(t *testing.T, resp api.DeletePocketResponseObject) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitDeletePocketResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	return rec.Code, rec.Body.String()
}

func wireOfRestore(t *testing.T, resp api.RestorePocketResponseObject) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitRestorePocketResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	return rec.Code, rec.Body.String()
}

// rowWithout is the row as JSON with the named columns removed, to compare the
// columns a restore must not touch.
func (f fixture) rowWithout(t *testing.T, id uuid.UUID, columns ...string) string {
	t.Helper()
	expr := "to_jsonb(p)"
	for _, c := range columns {
		expr += fmt.Sprintf(" - '%s'", c)
	}
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT (`+expr+`)::text FROM pockets p WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read pocket: %v", err)
	}
	return s
}

func (f fixture) isDeleted(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var deleted bool
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT deleted_at IS NOT NULL FROM pockets WHERE id = $1`, id).Scan(&deleted); err != nil {
		t.Fatalf("read pocket: %v", err)
	}
	return deleted
}

// ---- delete ----------------------------------------------------------------

// The row survives with deleted_at set; the Pocket is gone from the list and
// from PATCH, its name is free at once, and its id is never reused.
func TestDeleteIsSoftAndHidesThePocketFromListAndUpdate(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id, keep := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	assertCreated(t, f.create(ctx, t, keep, "Gopay"))
	keptBefore := f.row(t, keep)

	assertDeleted(t, f.del(ctx, t, id))

	if n := f.count(t); n != 2 {
		t.Errorf("pockets = %d, want 2: a delete must not remove the row", n)
	}
	if !f.isDeleted(t, id) {
		t.Error("deleted_at is not set")
	}
	if got := f.list(ctx, t); len(got) != 1 || got[0].Id != keep {
		t.Errorf("list = %+v, want only the Pocket that was not deleted", got)
	}
	assertNotFound(t, f.update(ctx, t, id, ptr("Renamed"), nil))
	assertNotFound(t, f.update(ctx, t, id, nil, nil))
	if after := f.row(t, keep); after != keptBefore {
		t.Errorf("another Pocket changed:\n%s\n%s", keptBefore, after)
	}

	t.Run("the name is free for a new Pocket", func(t *testing.T) {
		fresh := newID(t)
		if got := assertCreated(t, f.create(ctx, t, fresh, "ovo")); got.PocketDisplayName != "ovo" {
			t.Errorf("created = %+v", got)
		}
	})
	t.Run("the id is never reused", func(t *testing.T) {
		assertConflict(t, f.create(ctx, t, id, "Another"), api.IDTAKEN)
	})
}

func TestDeleteOfAnInactivePocketWorks(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	assertUpdated(t, f.update(ctx, t, id, nil, ptr(false)))

	assertDeleted(t, f.del(ctx, t, id))
	if !f.isDeleted(t, id) {
		t.Error("an inactive Pocket was not deleted")
	}
}

// Idempotent, and the FIRST deleted_at survives. The repeat changes no column
// at all, updated_at included. Proven red against `deleted_at = now()`.
func TestDeletingADeletedPocketIs204AndKeepsTheFirstInstant(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))

	assertDeleted(t, f.del(ctx, t, id))
	first := f.row(t, id)

	for range 3 {
		assertDeleted(t, f.del(ctx, t, id))
	}
	if after := f.row(t, id); after != first {
		t.Errorf("a repeat changed the row (the first deleted_at must be kept):\n%s\n%s", first, after)
	}

	// Against a deleted_at the clock cannot reproduce: it is the stored value
	// that is kept, not one that happens to be recent.
	f.exec(t, `UPDATE pockets SET deleted_at = '2026-01-01T00:00:00Z' WHERE id = $1`, id)
	assertDeleted(t, f.del(ctx, t, id))
	var kept bool
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT deleted_at = '2026-01-01T00:00:00Z' FROM pockets WHERE id = $1`, id).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Error("a repeat delete overwrote deleted_at")
	}
}

func TestConcurrentDeletesOfOnePocketAreAll204(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	for round := range 8 {
		id := newID(t)
		assertCreated(t, f.create(ctx, t, id, fmt.Sprintf("OVO %d", round)))
		for i, r := range race(4, func(int) string { return deleteStatus(f.del(ctx, t, id)) }) {
			if r != "204" {
				t.Fatalf("round %d, caller %d: %s, want 204", round, i, r)
			}
		}
		if !f.isDeleted(t, id) {
			t.Fatalf("round %d: not deleted", round)
		}
	}
}

// ---- restore ---------------------------------------------------------------

// Restore brings the Pocket back with every other column as it was, and an
// inactive Pocket comes back inactive.
func TestRestoreBringsBackTheSamePocketAndKeepsActive(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	active, inactive := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, active, "OVO"))
	assertCreated(t, f.create(ctx, t, inactive, "Dana"))
	assertUpdated(t, f.update(ctx, t, inactive, nil, ptr(false)))
	beforeActive := f.rowWithout(t, active, "deleted_at", "updated_at")
	beforeInactive := f.rowWithout(t, inactive, "deleted_at", "updated_at")

	assertDeleted(t, f.del(ctx, t, active))
	assertDeleted(t, f.del(ctx, t, inactive))
	if got := f.list(ctx, t); len(got) != 0 {
		t.Fatalf("list = %+v, want empty", got)
	}

	if got := assertRestored(t, f.restore(ctx, t, active)); got != (api.Pocket{Id: active, PocketDisplayName: "OVO", Active: true}) {
		t.Errorf("restored = %+v", got)
	}
	if got := assertRestored(t, f.restore(ctx, t, inactive)); got != (api.Pocket{Id: inactive, PocketDisplayName: "Dana", Active: false}) {
		t.Errorf("restored inactive = %+v, want it back still inactive", got)
	}
	if f.isDeleted(t, active) || f.isDeleted(t, inactive) {
		t.Error("deleted_at is still set")
	}
	if got := f.rowWithout(t, active, "deleted_at", "updated_at"); got != beforeActive {
		t.Errorf("restore changed a column other than deleted_at and updated_at:\n%s\n%s", beforeActive, got)
	}
	if got := f.rowWithout(t, inactive, "deleted_at", "updated_at"); got != beforeInactive {
		t.Errorf("restore changed a column other than deleted_at and updated_at:\n%s\n%s", beforeInactive, got)
	}
	// Back in the list, in its original creation-order place, and editable.
	if got := f.list(ctx, t); len(got) != 2 || got[0].Id != active || got[1].Id != inactive {
		t.Errorf("list = %+v, want both, in creation order", got)
	}
	assertUpdated(t, f.update(ctx, t, active, ptr("Cash"), nil))
}

// Restoring a Pocket that is not deleted is 200 and changes nothing, updated_at
// included.
func TestRestoringALivePocketIs200AndChangesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	assertUpdated(t, f.update(ctx, t, id, nil, ptr(false)))
	before := f.row(t, id)

	got := assertRestored(t, f.restore(ctx, t, id))
	if got != (api.Pocket{Id: id, PocketDisplayName: "OVO", Active: false}) {
		t.Errorf("restore of a live Pocket = %+v", got)
	}
	if after := f.row(t, id); after != before {
		t.Errorf("restore of a live Pocket changed the row:\n%s\n%s", before, after)
	}

	// And a second restore after a real one is as idempotent.
	assertDeleted(t, f.del(ctx, t, id))
	assertRestored(t, f.restore(ctx, t, id))
	restored := f.row(t, id)
	assertRestored(t, f.restore(ctx, t, id))
	if after := f.row(t, id); after != restored {
		t.Errorf("a second restore changed the row:\n%s\n%s", restored, after)
	}
}

// ---- the name index --------------------------------------------------------

// A restore whose name is held is 409 NAME_TAKEN and the Pocket stays deleted,
// byte for byte. Held means held by a Pocket that is not deleted, active or
// inactive, in any case; another Household's name and a deleted Pocket's do not
// hold it.
func TestRestoreIntoAHeldNameIsNameTakenAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, foreign := f.member(t, "Other", "b@example.com")

	for _, tc := range []struct {
		name      string
		held      string // the deleted Pocket's name
		holder    string // what the live Pocket is called
		holderOff bool
	}{
		{"a live Pocket with the same name", "Cash", "Cash", false},
		{"a live Pocket, in another case", "Wallet", "wALLET", false},
		{"an inactive Pocket still holds its name", "Debit", "Debit", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gone := newID(t)
			assertCreated(t, f.create(ctx, t, gone, tc.held))
			assertDeleted(t, f.del(ctx, t, gone))
			holder := newID(t)
			assertCreated(t, f.create(ctx, t, holder, tc.holder))
			if tc.holderOff {
				assertUpdated(t, f.update(ctx, t, holder, nil, ptr(false)))
			}
			before, holderBefore := f.row(t, gone), f.row(t, holder)

			resp := f.restore(ctx, t, gone)
			got, is := resp.(api.RestorePocket409JSONResponse)
			if !is || got.Code != api.NAMETAKEN || got.Args != nil {
				t.Fatalf("restore = %s, want 409 NAME_TAKEN with no args", restoreStatus(resp))
			}
			if !f.isDeleted(t, gone) {
				t.Error("the Pocket was restored despite the 409")
			}
			if f.row(t, gone) != before || f.row(t, holder) != holderBefore {
				t.Error("a refused restore changed a row")
			}

			// Deleting the holder frees the name: the restore now works.
			assertDeleted(t, f.del(ctx, t, holder))
			assertRestored(t, f.restore(ctx, t, gone))
		})
	}

	t.Run("another Household's Pocket does not hold the name", func(t *testing.T) {
		gone := newID(t)
		assertCreated(t, f.create(ctx, t, gone, "Shared Name"))
		assertDeleted(t, f.del(ctx, t, gone))
		assertCreated(t, f.create(foreign, t, newID(t), "Shared Name"))
		assertRestored(t, f.restore(ctx, t, gone))
	})
	t.Run("another deleted Pocket of the same name does not hold it", func(t *testing.T) {
		first, second := newID(t), newID(t)
		assertCreated(t, f.create(ctx, t, first, "Twice"))
		assertDeleted(t, f.del(ctx, t, first))
		assertCreated(t, f.create(ctx, t, second, "Twice"))
		assertDeleted(t, f.del(ctx, t, second))
		assertRestored(t, f.restore(ctx, t, first))
		if got := restoreStatus(f.restore(ctx, t, second)); got != "409 NAME_TAKEN" {
			t.Errorf("the second restore = %s, want 409 NAME_TAKEN (the first now holds it)", got)
		}
	})
}

// Two restores into one name: one 200 and one 409, never a 500, and exactly one
// row ends up live. The partial unique index decides it, so a lookup-then-update
// would let both win; proven red against that (see the PR).
func TestConcurrentRestoresIntoOneNameAnswerOne200AndOne409(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")

	for round := range 12 {
		name := fmt.Sprintf("Contested %d", round)
		ids := []uuid.UUID{newID(t), newID(t), newID(t)}
		for _, id := range ids {
			// Each in turn: the name is free once the earlier one is deleted.
			assertCreated(t, f.create(ctx, t, id, name))
			assertDeleted(t, f.del(ctx, t, id))
		}
		results := race(len(ids), func(i int) string { return restoreStatus(f.restore(ctx, t, ids[i])) })
		counts := map[string]int{}
		for _, r := range results {
			counts[r]++
		}
		if counts["200"] != 1 || counts["409 NAME_TAKEN"] != len(ids)-1 {
			t.Fatalf("round %d: statuses = %v, want one 200 and %d 409 NAME_TAKEN", round, counts, len(ids)-1)
		}
		var live int
		if err := f.tdb.Pool.QueryRow(context.Background(),
			`SELECT count(*) FROM pockets WHERE lower(pocket_display_name) = lower($1) AND deleted_at IS NULL`, name).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if live != 1 {
			t.Fatalf("round %d: %d live Pockets hold the name, want 1", round, live)
		}
	}
}

// Restores of one id, concurrently, are all 200: it is the same Pocket, and it
// never raises the name index against itself.
func TestConcurrentRestoresOfOnePocketAreAll200(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	for round := range 8 {
		id := newID(t)
		assertCreated(t, f.create(ctx, t, id, fmt.Sprintf("OVO %d", round)))
		assertDeleted(t, f.del(ctx, t, id))
		for i, r := range race(4, func(int) string { return restoreStatus(f.restore(ctx, t, id)) }) {
			if r != "200" {
				t.Fatalf("round %d, caller %d: %s, want 200", round, i, r)
			}
		}
	}
}

// A delete and a restore of one Pocket raced end in one of the two states, with
// no error and no half-state.
func TestConcurrentDeleteAndRestoreOfOnePocketNeverFail(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	for round := range 8 {
		id := newID(t)
		assertCreated(t, f.create(ctx, t, id, fmt.Sprintf("OVO %d", round)))
		assertDeleted(t, f.del(ctx, t, id))
		results := race(2, func(i int) string {
			if i == 0 {
				return deleteStatus(f.del(ctx, t, id))
			}
			return restoreStatus(f.restore(ctx, t, id))
		})
		if results[0] != "204" || results[1] != "200" {
			t.Fatalf("round %d: %v, want 204 and 200", round, results)
		}
	}
}

// ---- tenancy and NOT_FOUND -------------------------------------------------

// An id that is unknown or another Household's answers one NOT_FOUND, the same
// bytes, for both operations, and writes nothing: the other Household's rows,
// deleted ones included, are byte-identical afterwards. Proven red against a
// statement whose WHERE lost household_id (see the PR).
func TestUnknownAndForeignIdsAreOneNotFoundForDeleteAndRestore(t *testing.T) {
	f := newFixture(t)
	_, alice := f.member(t, "Alice", "a@example.com")
	_, bob := f.member(t, "Bob", "b@example.com")
	live, inactive, gone := newID(t), newID(t), newID(t)
	assertCreated(t, f.create(bob, t, live, "OVO"))
	assertCreated(t, f.create(bob, t, inactive, "Dana"))
	assertCreated(t, f.create(bob, t, gone, "Gone"))
	assertUpdated(t, f.update(bob, t, inactive, nil, ptr(false)))
	assertDeleted(t, f.del(bob, t, gone))
	snapshot := func() []string { return []string{f.row(t, live), f.row(t, inactive), f.row(t, gone)} }
	before := snapshot()

	ids := map[string]uuid.UUID{
		"unknown":            newID(t),
		"another's live":     live,
		"another's inactive": inactive,
		"another's deleted":  gone,
	}

	t.Run("delete", func(t *testing.T) {
		var wantStatus int
		var wantBody string
		for kind, id := range ids {
			resp := f.del(alice, t, id)
			if got := deleteStatus(resp); got != "404 NOT_FOUND" {
				t.Fatalf("%s = %s, want 404 NOT_FOUND", kind, got)
			}
			status, body := wireOfDelete(t, resp)
			if wantBody == "" {
				wantStatus, wantBody = status, body
			}
			if status != wantStatus || body != wantBody {
				t.Errorf("%s answers %d %s, others %d %s", kind, status, body, wantStatus, wantBody)
			}
		}
	})
	t.Run("restore", func(t *testing.T) {
		var wantStatus int
		var wantBody string
		for kind, id := range ids {
			resp := f.restore(alice, t, id)
			if got := restoreStatus(resp); got != "404 NOT_FOUND" {
				t.Fatalf("%s = %s, want 404 NOT_FOUND", kind, got)
			}
			status, body := wireOfRestore(t, resp)
			if wantBody == "" {
				wantStatus, wantBody = status, body
			}
			if status != wantStatus || body != wantBody {
				t.Errorf("%s answers %d %s, others %d %s", kind, status, body, wantStatus, wantBody)
			}
		}
	})
	t.Run("delete and restore answer the same 404 as PATCH does", func(t *testing.T) {
		pStatus, pBody := wireOfUpdate(t, f.update(alice, t, live, nil, nil))
		dStatus, dBody := wireOfDelete(t, f.del(alice, t, live))
		rStatus, rBody := wireOfRestore(t, f.restore(alice, t, live))
		if dStatus != pStatus || rStatus != pStatus || strings.TrimSpace(dBody) != strings.TrimSpace(pBody) || strings.TrimSpace(rBody) != strings.TrimSpace(pBody) {
			t.Errorf("PATCH %d %s, DELETE %d %s, restore %d %s: one 404 expected", pStatus, pBody, dStatus, dBody, rStatus, rBody)
		}
	})

	after := snapshot()
	for i := range before {
		if after[i] != before[i] {
			t.Errorf("row %d of the other Household changed:\n%s\n%s", i, before[i], after[i])
		}
	}
	if got := f.list(bob, t); len(got) != 2 {
		t.Errorf("Bob's list = %+v, want his two live Pockets", got)
	}
}

// Alice's own operations never reach Bob's name index either: restoring
// Alice's deleted "OVO" is not a clash with Bob's live "OVO".
func TestDeleteAndRestoreAreScopedToTheCallersHousehold(t *testing.T) {
	f := newFixture(t)
	_, alice := f.member(t, "Alice", "a@example.com")
	_, bob := f.member(t, "Bob", "b@example.com")
	mine, theirs := newID(t), newID(t)
	assertCreated(t, f.create(alice, t, mine, "OVO"))
	assertCreated(t, f.create(bob, t, theirs, "OVO"))

	assertDeleted(t, f.del(alice, t, mine))
	assertRestored(t, f.restore(alice, t, mine))
	if f.isDeleted(t, theirs) {
		t.Error("Alice's delete reached Bob's Pocket")
	}
}

// PRD S2: any member may delete and restore any of the Household's Pockets.
func TestTwoUsersInOneHouseholdDeleteAndRestoreTheSamePocket(t *testing.T) {
	f := newFixture(t)
	householdID, first := f.member(t, "Home", "a@example.com")
	second := f.userIn(t, householdID, "b@example.com")
	id := newID(t)
	assertCreated(t, f.create(first, t, id, "OVO"))

	assertDeleted(t, f.del(second, t, id))
	if got := f.list(first, t); len(got) != 0 {
		t.Errorf("first User sees %+v, want the second's delete", got)
	}
	assertRestored(t, f.restore(first, t, id))
	if got := f.list(second, t); len(got) != 1 {
		t.Errorf("second User sees %+v, want the first's restore", got)
	}
}

func TestDeleteAndRestoreWithoutAUserAre401AndWriteNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	before := f.row(t, id)

	if got := deleteStatus(f.del(context.Background(), t, id)); got != "401" {
		t.Errorf("DeletePocket = %s, want 401", got)
	}
	assertDeleted(t, f.del(ctx, t, id))
	deleted := f.row(t, id)
	if got := restoreStatus(f.restore(context.Background(), t, id)); got != "401" {
		t.Errorf("RestorePocket = %s, want 401", got)
	}
	if f.row(t, id) != deleted || before == deleted {
		t.Error("an unauthenticated call changed the row")
	}
}

// ---- failure ---------------------------------------------------------------

type deleteBreakable struct {
	db.Querier
	soft    func(context.Context, db.SoftDeletePocketParams) (int64, error)
	restore func(context.Context, db.RestorePocketParams) (db.Pocket, error)
}

func (b deleteBreakable) SoftDeletePocket(ctx context.Context, p db.SoftDeletePocketParams) (int64, error) {
	return b.soft(ctx, p)
}

func (b deleteBreakable) RestorePocket(ctx context.Context, p db.RestorePocketParams) (db.Pocket, error) {
	return b.restore(ctx, p)
}

func TestADeleteErrorIsA500AndNoRowsAffectedIsA404(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int64
		err  error
		want string
	}{
		{"an outage", 0, errOutage, "500"},
		{"an error beside a row count", 1, errOutage, "500"},
		{"no row", 0, nil, "404 NOT_FOUND"},
		{"one row", 1, nil, "204"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, func(q db.Querier) db.Querier {
				return deleteBreakable{Querier: q, soft: func(context.Context, db.SoftDeletePocketParams) (int64, error) { return tc.n, tc.err }}
			})
			_, ctx := f.member(t, "Home", "a@example.com")
			if got := deleteStatus(f.del(ctx, t, newID(t))); got != tc.want {
				t.Errorf("DeletePocket = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestARestoreErrorThatIsNotNoRowsOrTheNameIndexIsA500(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"an outage", errOutage, "500"},
		{"a 23505 on another index", &pgconn.PgError{Code: "23505", ConstraintName: "something_else_key"}, "500"},
		{"a 23505 on the primary key", &pgconn.PgError{Code: "23505", ConstraintName: "pockets_pkey"}, "500"},
		{"a 23505 on the name index", &pgconn.PgError{Code: "23505", ConstraintName: "pockets_household_name_idx"}, "409 NAME_TAKEN"},
		{"ErrNoRows", pgx.ErrNoRows, "404 NOT_FOUND"},
		{"ErrNoRows wrapped", fmt.Errorf("scan: %w", pgx.ErrNoRows), "404 NOT_FOUND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, func(q db.Querier) db.Querier {
				return deleteBreakable{Querier: q, restore: func(context.Context, db.RestorePocketParams) (db.Pocket, error) {
					return db.Pocket{}, tc.err
				}}
			})
			_, ctx := f.member(t, "Home", "a@example.com")
			if got := restoreStatus(f.restore(ctx, t, newID(t))); got != tc.want {
				t.Errorf("RestorePocket = %s, want %s", got, tc.want)
			}
		})
	}
}

// What reaches either statement is the session's Household and the path's id,
// and nothing else.
func TestTheStatementsCarryTheSessionsHouseholdAndThePathsId(t *testing.T) {
	var gotDelete db.SoftDeletePocketParams
	var gotRestore db.RestorePocketParams
	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return deleteBreakable{
			Querier: q,
			soft: func(_ context.Context, p db.SoftDeletePocketParams) (int64, error) {
				gotDelete = p
				return 0, nil
			},
			restore: func(_ context.Context, p db.RestorePocketParams) (db.Pocket, error) {
				gotRestore = p
				return db.Pocket{}, pgx.ErrNoRows
			},
		}
	})
	householdID, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	f.del(ctx, t, id)
	f.restore(ctx, t, id)

	if gotDelete.HouseholdID != householdID || gotDelete.ID.Bytes != id {
		t.Errorf("delete params = %+v, want the caller's household and %v", gotDelete, id)
	}
	if gotRestore.HouseholdID != householdID || gotRestore.ID.Bytes != id {
		t.Errorf("restore params = %+v, want the caller's household and %v", gotRestore, id)
	}
}

// ---- more races ------------------------------------------------------------

// A restore and a create of one name, raced: the partial unique index lets
// exactly one in. The loser is 409 NAME_TAKEN, never a 500, and one live Pocket
// holds the name afterwards.
func TestConcurrentRestoreAndCreateOfOneNameAnswerOneWinnerAndNameTaken(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")

	for round := range 20 {
		name := fmt.Sprintf("Contested %d", round)
		gone, fresh := newID(t), newID(t)
		assertCreated(t, f.create(ctx, t, gone, name))
		assertDeleted(t, f.del(ctx, t, gone))

		results := race(2, func(i int) string {
			if i == 0 {
				return restoreStatus(f.restore(ctx, t, gone))
			}
			switch r := f.create(ctx, t, fresh, strings.ToUpper(name)).(type) {
			case api.CreatePocket201JSONResponse:
				return "201"
			case api.CreatePocket409JSONResponse:
				return "409 " + string(r.Code)
			}
			return "other"
		})
		restoreWon := results[0] == "200" && results[1] == "409 NAME_TAKEN"
		createWon := results[0] == "409 NAME_TAKEN" && results[1] == "201"
		if !restoreWon && !createWon {
			t.Fatalf("round %d: restore=%s create=%s, want one winner and one 409 NAME_TAKEN", round, results[0], results[1])
		}
		var live int
		if err := f.tdb.Pool.QueryRow(context.Background(),
			`SELECT count(*) FROM pockets WHERE lower(pocket_display_name) = lower($1) AND deleted_at IS NULL`, name).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if live != 1 {
			t.Fatalf("round %d: %d live Pockets hold the name, want 1", round, live)
		}
	}
}

// A delete and a PATCH of one Pocket, raced: the PATCH is applied before the
// delete (200) or finds it deleted (404), never a 500, and the row ends
// deleted whichever came first.
func TestConcurrentDeleteAndUpdateOfOnePocketEndDeleted(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")

	for round := range 20 {
		id := newID(t)
		assertCreated(t, f.create(ctx, t, id, fmt.Sprintf("OVO %d", round)))
		renamed := fmt.Sprintf("Renamed %d", round)
		results := race(2, func(i int) string {
			if i == 0 {
				return deleteStatus(f.del(ctx, t, id))
			}
			return updateStatus(f.update(ctx, t, id, &renamed, ptr(false)))
		})
		if results[0] != "204" || (results[1] != "200" && results[1] != "404 NOT_FOUND") {
			t.Fatalf("round %d: delete=%s update=%s, want 204 and 200 or 404 NOT_FOUND", round, results[0], results[1])
		}
		if !f.isDeleted(t, id) {
			t.Fatalf("round %d: the row is live after a delete and an update (update answered %s)", round, results[1])
		}
	}
}
