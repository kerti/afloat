package category_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
)

// DeleteCategory and RestoreCategory against a real Postgres, no t.Parallel
// (ADR-0004). Delete is soft, idempotent and keeps the first deletion instant;
// it is refused (CATEGORY_IN_USE) while another Budget Period holds an
// allocation, and that refusal is race-free: see the race tests at the end.
// Restore is its Undo: within the Period it was deleted in it writes no
// allocation; after a rollover it carries the allocation forward.

func (f fixture) del(ctx context.Context, t *testing.T, id uuid.UUID) api.DeleteCategoryResponseObject {
	t.Helper()
	resp, err := f.handlers.DeleteCategory(ctx, api.DeleteCategoryRequestObject{Id: id})
	if err != nil {
		t.Fatalf("DeleteCategory returned an error: %v", err)
	}
	return resp
}

func (f fixture) restore(ctx context.Context, t *testing.T, id uuid.UUID) api.RestoreCategoryResponseObject {
	t.Helper()
	resp, err := f.handlers.RestoreCategory(ctx, api.RestoreCategoryRequestObject{Id: id})
	if err != nil {
		t.Fatalf("RestoreCategory returned an error: %v", err)
	}
	return resp
}

func deleteStatus(resp api.DeleteCategoryResponseObject) string {
	switch r := resp.(type) {
	case api.DeleteCategory204Response:
		return "204"
	case api.DeleteCategory400JSONResponse:
		return "400"
	case api.DeleteCategory401JSONResponse:
		return "401"
	case api.DeleteCategory404JSONResponse:
		return "404 " + string(r.Code)
	case api.DeleteCategory409JSONResponse:
		return "409 " + string(r.Code)
	case api.DeleteCategory500JSONResponse:
		return "500"
	}
	return fmt.Sprintf("%T", resp)
}

func restoreStatus(resp api.RestoreCategoryResponseObject) string {
	switch r := resp.(type) {
	case api.RestoreCategory200JSONResponse:
		return "200"
	case api.RestoreCategory400JSONResponse:
		return "400"
	case api.RestoreCategory401JSONResponse:
		return "401"
	case api.RestoreCategory404JSONResponse:
		return "404 " + string(r.Code)
	case api.RestoreCategory409JSONResponse:
		return "409 " + string(r.Code)
	case api.RestoreCategory500JSONResponse:
		return "500"
	}
	return fmt.Sprintf("%T", resp)
}

func assertDeleted(t *testing.T, resp api.DeleteCategoryResponseObject) {
	t.Helper()
	if got := deleteStatus(resp); got != "204" {
		t.Fatalf("DeleteCategory = %s, want 204", got)
	}
}

func assertRestored(t *testing.T, resp api.RestoreCategoryResponseObject) api.Category {
	t.Helper()
	got, is := resp.(api.RestoreCategory200JSONResponse)
	if !is {
		t.Fatalf("RestoreCategory = %s, want 200", restoreStatus(resp))
	}
	return api.Category(got)
}

func wireOfDelete(t *testing.T, resp api.DeleteCategoryResponseObject) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitDeleteCategoryResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func wireOfRestore(t *testing.T, resp api.RestoreCategoryResponseObject) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitRestoreCategoryResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func (f fixture) isDeleted(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	return f.scalar(t, `SELECT (deleted_at IS NOT NULL)::text FROM categories WHERE id = $1`, id) == "true"
}

func (f fixture) setBudget(ctx context.Context, t *testing.T, changes ...api.AllocationChange) api.Budget {
	t.Helper()
	body := api.UpdateBudgetJSONRequestBody{Allocations: &changes}
	resp, err := f.budget.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &body})
	if err != nil {
		t.Fatalf("UpdateBudget returned an error: %v", err)
	}
	got, is := resp.(api.UpdateBudget200JSONResponse)
	if !is {
		t.Fatalf("UpdateBudget = %#v, want 200", resp)
	}
	return api.Budget(got)
}

// ---- soft delete -----------------------------------------------------------

func TestDeleteIsSoftAndHidesTheCategoryFromListAndUpdateAndReleasesTheName(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	rent, food := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, nil))

	resp := f.del(ctx, t, rent)
	assertDeleted(t, resp)
	if code, body := wireOfDelete(t, resp); code != 204 || body != "" {
		t.Errorf("wire = %d %q, want 204 with no body", code, body)
	}

	if !f.isDeleted(t, rent) || f.count(t, "categories") != 2 {
		t.Error("the row must stay, with deleted_at set")
	}
	if got := f.list(ctx, t); len(got) != 1 || got[0].Id != food {
		t.Errorf("list = %+v, want only Food", got)
	}
	assertNotFound(t, f.update(ctx, t, rent, ptr("x"), nil))
	// Its name is free at once, in either bucket.
	assertCreated(t, f.create(ctx, t, newID(t), "rent", api.Daily, nil))
}

func TestDeleteOfAnInactiveCategoryWorksAndRestoreKeepsItInactive(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))
	assertUpdated(t, f.update(ctx, t, id, nil, ptr(false)))

	assertDeleted(t, f.del(ctx, t, id))
	got := assertRestored(t, f.restore(ctx, t, id))
	if got.Active || got.CategoryDisplayName != "Food" || got.Bucket != api.Daily || got.Id != id {
		t.Errorf("restore = %+v, want Food as it was, inactive", got)
	}
}

func TestDeletingADeletedCategoryIs204AndKeepsTheFirstInstant(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))

	assertDeleted(t, f.del(ctx, t, id))
	first := f.row(t, id)
	time.Sleep(5 * time.Millisecond)
	assertDeleted(t, f.del(ctx, t, id))
	if after := f.row(t, id); after != first {
		t.Errorf("a repeat delete changed the row:\n%s\n%s", first, after)
	}
}

// The first instant is the first delete's, even when the Category is restored
// and deleted again: the second delete is a new first.
func TestDeleteRestoreDeleteSetsANewInstant(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))
	assertDeleted(t, f.del(ctx, t, id))
	first := f.scalar(t, `SELECT deleted_at::text FROM categories WHERE id = $1`, id)
	assertRestored(t, f.restore(ctx, t, id))
	if f.isDeleted(t, id) {
		t.Fatal("still deleted after a restore")
	}
	time.Sleep(5 * time.Millisecond)
	assertDeleted(t, f.del(ctx, t, id))
	if second := f.scalar(t, `SELECT deleted_at::text FROM categories WHERE id = $1`, id); second == first {
		t.Error("the second delete kept the first one's instant")
	}
}

// ---- what a delete does to the figures --------------------------------------

// A delete drops the Category out of every figure of the current Period at once,
// and a restore brings them back exactly; the allocation rows are never touched
// (#130 C8).
func TestDeleteDropsTheCategoryOutOfTheFiguresAndRestoreBringsThemBackExactly(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	rent, food := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, ptr("2000000.5")))
	if _, err := f.budget.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{MonthlyBudget: ptr("9000000")}}); err != nil {
		t.Fatal(err)
	}
	before := f.getBudget(ctx, t)
	if before.CommitmentsBudget != "3000000.0000" || *before.DailyPool != "6000000.0000" || *before.Unallocated != "3999999.5000" {
		t.Fatalf("setup: %+v", before)
	}
	rows := f.allocationRows(t)

	assertDeleted(t, f.del(ctx, t, rent))
	got := f.getBudget(ctx, t)
	if got.CommitmentsBudget != "0.0000" || *got.DailyPool != "9000000.0000" || *got.Unallocated != "6999999.5000" || len(got.Allocations) != 1 {
		t.Errorf("after the delete: commitments %s pool %s unallocated %s allocations %d",
			got.CommitmentsBudget, *got.DailyPool, *got.Unallocated, len(got.Allocations))
	}
	if f.allocationRows(t) != rows {
		t.Error("a delete changed an allocation row")
	}

	assertDeleted(t, f.del(ctx, t, food))
	got = f.getBudget(ctx, t)
	if *got.Unallocated != "9000000.0000" || len(got.Allocations) != 0 {
		t.Errorf("after both: unallocated %s allocations %d", *got.Unallocated, len(got.Allocations))
	}

	assertRestored(t, f.restore(ctx, t, rent))
	assertRestored(t, f.restore(ctx, t, food))
	if after := f.getBudget(ctx, t); !reflect.DeepEqual(after, before) {
		t.Errorf("after the restore:\n%+v\nwant\n%+v", after, before)
	}
	if f.allocationRows(t) != rows {
		t.Error("a restore changed an allocation row")
	}
}

// A deleted Category is not carried into the next Period (PR 2's copy joins live
// Categories).
func TestADeletedCategoryIsNotCarriedForward(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-11-05T10:00:00+07:00")
	rent, food := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, ptr("1000")))
	assertDeleted(t, f.del(ctx, t, rent))

	f.setClock(t, "2026-12-02T10:00:00+07:00")
	got := f.getBudget(ctx, t)
	if len(got.Allocations) != 1 || got.Allocations[0].CategoryId != food {
		t.Errorf("December allocations = %+v, want only Food", got.Allocations)
	}
	if v := f.allocations(t); v != "2026-11 2026-11-05 Food 1000.0000; 2026-11 2026-11-05 Rent 3000000.0000; 2026-12 2026-12-01 Food 1000.0000" {
		t.Errorf("allocations = %q: Rent must not be copied into December", v)
	}
}

// ---- the in-use guard --------------------------------------------------------

// An earlier Period holds an amount: refused, with nothing changed, and the
// answer is one code with no args. Whether the current Period has been read yet
// does not matter.
func TestDeleteRefusedWhileAnEarlierPeriodHoldsAnAllocation(t *testing.T) {
	for _, materialiseNovember := range []bool{false, true} {
		t.Run(fmt.Sprintf("november read first: %v", materialiseNovember), func(t *testing.T) {
			f := newFixture(t)
			_, ctx := f.member(t, "Home", "a@example.com")
			f.setClock(t, "2026-10-10T10:00:00+07:00")
			rent := newID(t)
			assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))

			f.setClock(t, day14)
			if materialiseNovember {
				f.getBudget(ctx, t)
			}
			rowBefore, allocBefore, markersBefore := f.row(t, rent), f.allocationRows(t), f.markers(t)

			resp := f.del(ctx, t, rent)
			if got := deleteStatus(resp); got != "409 CATEGORY_IN_USE" {
				t.Fatalf("DeleteCategory = %s, want 409 CATEGORY_IN_USE", got)
			}
			if code, body := wireOfDelete(t, resp); code != 409 || body != `{"code":"CATEGORY_IN_USE"}` {
				t.Errorf("wire = %d %s, want 409 {\"code\":\"CATEGORY_IN_USE\"}", code, body)
			}
			if f.row(t, rent) != rowBefore || f.allocationRows(t) != allocBefore || f.markers(t) != markersBefore {
				t.Error("a refused delete changed something")
			}
			// What it offers instead.
			assertUpdated(t, f.update(ctx, t, rent, nil, ptr(false)))
		})
	}
}

// Only the current Period's own allocation never blocks: a Category made today
// and deleted today is a mistake that can be undone.
func TestTheCurrentPeriodsOwnAllocationNeverBlocksADelete(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	// Several versions in the Period, all the current one's.
	f.setBudget(ctx, t, api.AllocationChange{CategoryId: rent, Allocation: ptr("3100000")})
	f.setClock(t, "2026-11-20T10:00:00+07:00")
	f.setBudget(ctx, t, api.AllocationChange{CategoryId: rent, Allocation: ptr("3200000")})

	assertDeleted(t, f.del(ctx, t, rent))
}

// A later Period counts as another: after a period_start_day change, or a
// clock that moved back, "other than the current one" is not "earlier".
func TestAnAllocationInALaterPeriodAlsoBlocks(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-12-03T10:00:00+07:00")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))

	f.setClock(t, day14)
	if got := deleteStatus(f.del(ctx, t, rent)); got != "409 CATEGORY_IN_USE" {
		t.Errorf("DeleteCategory = %s, want 409 CATEGORY_IN_USE", got)
	}
}

// The test is the amount, not the row: an earlier Period that holds only "none"
// for the Category does not block (a Daily Category may have none), while one
// that held an amount and later cleared it still does (it is part of that
// Period's record).
func TestOnlyANonNullAmountInAnotherPeriodBlocks(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-10-10T10:00:00+07:00")
	onlyNone, clearedLater := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, onlyNone, "OnlyNone", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, clearedLater, "ClearedLater", api.Daily, ptr("500")))
	f.setBudget(ctx, t, api.AllocationChange{CategoryId: onlyNone, Allocation: nil})
	f.setClock(t, "2026-10-12T10:00:00+07:00")
	f.setBudget(ctx, t, api.AllocationChange{CategoryId: clearedLater, Allocation: nil})
	if got := f.scalar(t, `SELECT count(*)::text FROM allocations WHERE category_id = $1 AND allocation IS NULL`, onlyNone); got != "1" {
		t.Fatalf("setup: the None row is missing (%s)", got)
	}

	f.setClock(t, day14)
	if got := deleteStatus(f.del(ctx, t, onlyNone)); got != "204" {
		t.Errorf("a Category with only a none row in October: %s, want 204", got)
	}
	if got := deleteStatus(f.del(ctx, t, clearedLater)); got != "409 CATEGORY_IN_USE" {
		t.Errorf("a Category that held an amount in October and cleared it: %s, want 409", got)
	}
}

// Deleting what is already deleted is the idempotent 204 and is never refused,
// even after the Period moved on and the guard would now say no: a repeat is not
// a new act.
func TestARepeatDeleteIsNeverRefused(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertDeleted(t, f.del(ctx, t, rent))
	first := f.row(t, rent)

	f.setClock(t, "2026-12-05T10:00:00+07:00")
	assertDeleted(t, f.del(ctx, t, rent))
	if f.row(t, rent) != first {
		t.Error("the repeat changed the row")
	}
}

// ---- restore ----------------------------------------------------------------

func TestRestoringALiveCategoryIs200AndChangesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))
	before := f.row(t, id)

	got := assertRestored(t, f.restore(ctx, t, id))
	if got.Id != id || got.CategoryDisplayName != "Food" || !got.Active {
		t.Errorf("restore = %+v", got)
	}
	if f.row(t, id) != before {
		t.Error("restoring a live Category changed it")
	}
	code, body := wireOfRestore(t, f.restore(ctx, t, id))
	if code != 200 || body != `{"active":true,"bucket":"daily","category_display_name":"Food","id":"`+id.String()+`"}` {
		t.Errorf("wire = %d %s", code, body)
	}
}

// A restore into a name that another live Category holds, in either bucket and
// in any case, is 409 NAME_TAKEN with the Category still deleted; the unique
// index decides it, and it stays restorable once the name is free.
func TestRestoreIntoAHeldNameIsNameTakenAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	gone, other := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, gone, "Rent", api.Commitment, ptr("3000000")))
	assertDeleted(t, f.del(ctx, t, gone))
	assertCreated(t, f.create(ctx, t, other, "RENT", api.Daily, nil))
	before := f.row(t, gone)

	resp := f.restore(ctx, t, gone)
	if got := restoreStatus(resp); got != "409 NAME_TAKEN" {
		t.Fatalf("restore = %s, want 409 NAME_TAKEN", got)
	}
	if code, body := wireOfRestore(t, resp); code != 409 || body != `{"code":"NAME_TAKEN"}` {
		t.Errorf("wire = %d %s", code, body)
	}
	if f.row(t, gone) != before || !f.isDeleted(t, gone) {
		t.Error("a refused restore changed the row")
	}

	assertUpdated(t, f.update(ctx, t, other, ptr("Housing"), nil))
	assertRestored(t, f.restore(ctx, t, gone))
}

// The maintainer's ruling on a restore: Undo is "as if it was never deleted". A
// Commitment deleted in November and restored after December has been read was
// skipped by December's copy, so the restore gives it what the copy would have:
// its latest November amount, dated December's first day. R10 holds, a
// Commitment never has none.
func TestARestoreAfterTheNextPeriodWasReadCarriesTheAllocationForward(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	rent, food := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, ptr("1000")))
	f.setClock(t, "2026-11-20T10:00:00+07:00")
	f.setBudget(ctx, t, api.AllocationChange{CategoryId: rent, Allocation: ptr("3100000")})
	if _, err := f.budget.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{MonthlyBudget: ptr("9000000")}}); err != nil {
		t.Fatal(err)
	}
	assertDeleted(t, f.del(ctx, t, rent))

	f.setClock(t, "2026-12-02T10:00:00+07:00")
	f.getBudget(ctx, t)
	if v := f.allocations(t); strings.Contains(v, "2026-12 2026-12-01 Rent") {
		t.Fatalf("setup: December carried the deleted Rent: %q", v)
	}

	got := assertRestored(t, f.restore(ctx, t, rent))
	if got.Bucket != api.Commitment || !got.Active || got.CategoryDisplayName != "Rent" {
		t.Errorf("restore = %+v", got)
	}
	want := "2026-11 2026-11-14 Food 1000.0000; 2026-11 2026-11-14 Rent 3000000.0000; 2026-11 2026-11-20 Rent 3100000.0000; " +
		"2026-12 2026-12-01 Food 1000.0000; 2026-12 2026-12-01 Rent 3100000.0000"
	if v := f.allocations(t); v != want {
		t.Errorf("allocations = %q, want %q: the latest November state, dated December's first day", v, want)
	}
	b := f.getBudget(ctx, t)
	if len(b.Allocations) != 2 || b.CommitmentsBudget != "3100000.0000" || *b.DailyPool != "5900000.0000" {
		t.Errorf("December: allocations %+v commitments %s pool %s", b.Allocations, b.CommitmentsBudget, *b.DailyPool)
	}
	// The same state a Category that was never deleted would have: nothing more
	// happens on a repeat, on a later first read, or on a later restore.
	rows := f.allocationRows(t)
	assertRestored(t, f.restore(ctx, t, rent))
	if f.allocationRows(t) != rows {
		t.Error("a repeat restore wrote an allocation")
	}
	// A Category an earlier Period held an amount for can be deactivated, never deleted.
	if got := deleteStatus(f.del(ctx, t, rent)); got != "409 CATEGORY_IN_USE" {
		t.Errorf("a second delete = %s, want 409 CATEGORY_IN_USE (November holds an amount)", got)
	}
}

// The Period has not been read at all: the restore brings it into being as a first
// read does, in the same transaction, and the Category is carried exactly once.
func TestARestoreInAPeriodNobodyHasReadMaterialisesItAndCarriesOnce(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	rent, food := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, ptr("1000")))
	assertDeleted(t, f.del(ctx, t, rent))

	f.setClock(t, "2026-12-02T10:00:00+07:00")
	if strings.Contains(f.markers(t), "2026-12") {
		t.Fatal("setup: December exists")
	}
	assertRestored(t, f.restore(ctx, t, rent))

	if got := f.markers(t); got != "2026-11,2026-12" {
		t.Errorf("budgets = %q, want December brought into being", got)
	}
	want := "2026-11 2026-11-14 Food 1000.0000; 2026-11 2026-11-14 Rent 3000000.0000; 2026-12 2026-12-01 Food 1000.0000; 2026-12 2026-12-01 Rent 3000000.0000"
	if v := f.allocations(t); v != want {
		t.Errorf("allocations = %q, want %q", v, want)
	}
	// The ordinary first read after it finds a Period that exists and writes nothing.
	rows := f.allocationRows(t)
	f.getBudget(ctx, t)
	if f.allocationRows(t) != rows {
		t.Error("a later read wrote an allocation")
	}
}

// The Period that the restore lands in was materialised from a Period that never
// held the Category (it was deleted there): the source is the nearest earlier
// Period that held an allocation for THIS Category, not for anyone.
func TestARestoreTwoPeriodsLaterTakesTheNearestPeriodThatHeldItsAllocation(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-10-10T10:00:00+07:00")
	rent, food := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, ptr("1000")))
	assertDeleted(t, f.del(ctx, t, rent))
	f.setClock(t, "2026-11-10T10:00:00+07:00")
	f.getBudget(ctx, t) // November: Food only
	f.setClock(t, "2026-12-10T10:00:00+07:00")
	assertRestored(t, f.restore(ctx, t, rent))

	if v := f.allocations(t); !strings.HasSuffix(v, "2026-12 2026-12-01 Food 1000.0000; 2026-12 2026-12-01 Rent 3000000.0000") {
		t.Errorf("allocations = %q: December must carry Rent from October, as if it had never been deleted", v)
	}
}

// Deleted and restored within one Period: it already holds the Category's
// allocation, whatever day the version is dated, and nothing is written.
func TestARestoreWithinThePeriodItWasDeletedInWritesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-11-10T10:00:00+07:00")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	f.setClock(t, "2026-11-20T10:00:00+07:00")
	f.setBudget(ctx, t, api.AllocationChange{CategoryId: rent, Allocation: ptr("3200000")}) // a second version, dated the 20th
	assertDeleted(t, f.del(ctx, t, rent))
	rows := f.allocationRows(t)

	assertRestored(t, f.restore(ctx, t, rent))
	if f.allocationRows(t) != rows {
		t.Errorf("a restore within the Period wrote an allocation:\n%s\nwas\n%s", f.allocationRows(t), rows)
	}
}

// A Daily Category is carried only if its latest allocation was an amount: "none"
// is a value and stays none, and one that never had an allocation gets none.
func TestARestoredDailyCategoryIsCarriedOnlyIfItsLatestAllocationWasAnAmount(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-10-10T10:00:00+07:00")
	amount, cleared, never, anchor := newID(t), newID(t), newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, amount, "Amount", api.Daily, ptr("500")))
	assertCreated(t, f.create(ctx, t, cleared, "Cleared", api.Daily, ptr("700")))
	assertCreated(t, f.create(ctx, t, never, "Never", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, anchor, "Anchor", api.Daily, ptr("1")))
	f.setClock(t, "2026-10-12T10:00:00+07:00")
	f.setBudget(ctx, t, api.AllocationChange{CategoryId: cleared, Allocation: nil})
	for _, id := range []uuid.UUID{amount, cleared, never} {
		assertDeleted(t, f.del(ctx, t, id))
	}

	f.setClock(t, "2026-11-10T10:00:00+07:00")
	for _, id := range []uuid.UUID{amount, cleared, never} {
		assertRestored(t, f.restore(ctx, t, id))
	}
	want := "2026-11 2026-11-01 Amount 500.0000; 2026-11 2026-11-01 Anchor 1.0000"
	if v := f.allocations(t); !strings.HasSuffix(v, want) {
		t.Errorf("allocations = %q, want November to end %q: Cleared stays none and Never has none", v, want)
	}
	if got := f.scalar(t, `SELECT count(*)::text FROM allocations WHERE year_month = '2026-11'`); got != "2" {
		t.Errorf("November rows = %s, want 2", got)
	}
}

// Restoring a Category that is not deleted writes nothing, and brings no Period
// into being.
func TestRestoringALiveCategoryWritesNoAllocationAndMaterialisesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	f.setClock(t, "2026-12-02T10:00:00+07:00")
	rows, markers := f.allocationRows(t), f.markers(t)

	assertRestored(t, f.restore(ctx, t, rent))
	if f.allocationRows(t) != rows || f.markers(t) != markers {
		t.Error("restoring a live Category wrote something")
	}
}

// A restore into a held name is refused with nothing written at all: not the
// Category, not an allocation, not the Period.
func TestARefusedRestoreWritesNoAllocationAndMaterialisesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertDeleted(t, f.del(ctx, t, rent))
	assertCreated(t, f.create(ctx, t, newID(t), "rent", api.Daily, nil))
	f.setClock(t, "2026-12-02T10:00:00+07:00")
	rows, markers := f.allocationRows(t), f.markers(t)

	if got := restoreStatus(f.restore(ctx, t, rent)); got != "409 NAME_TAKEN" {
		t.Fatalf("restore = %s", got)
	}
	if f.allocationRows(t) != rows || f.markers(t) != markers || !f.isDeleted(t, rent) {
		t.Error("a refused restore wrote something")
	}
}

// A Commitment restored into a Period that no other Period held an allocation for
// is impossible (a Commitment is created with one), but a Category whose only
// allocation is in a LATER Period (a period_start_day change) is carried from it.
func TestARestoreCarriesFromALaterPeriodWhenNoEarlierOneHoldsAnAllocation(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-12-03T10:00:00+07:00")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertDeleted(t, f.del(ctx, t, rent))
	f.setClock(t, day14)
	assertRestored(t, f.restore(ctx, t, rent))
	if v := f.allocations(t); v != "2026-11 2026-11-01 Rent 3000000.0000; 2026-12 2026-12-03 Rent 3000000.0000" {
		t.Errorf("allocations = %q", v)
	}
}

// A restore and the Period's first read, raced: one allocation row for the
// Category, no 500, whichever commits first. (The copy's ON CONFLICT DO NOTHING and
// NOT EXISTS, and the restore's own, are what make this hold.)
func TestARestoreRacingThePeriodsFirstReadLeavesOneAllocationRowAndNoError(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	const rounds = 25
	for round := range rounds {
		rent := newID(t)
		f.setClock(t, "2026-11-10T10:00:00+07:00")
		assertCreated(t, f.create(ctx, t, rent, fmt.Sprintf("Rent %d", round), api.Commitment, ptr("3000000")))
		assertDeleted(t, f.del(ctx, t, rent))

		// A Period nobody has read: a different one every round, a month on from the last.
		next := time.Date(2026, 12, 10, 10, 0, 0, 0, time.FixedZone("", 7*3600)).AddDate(0, round, 0)
		f.setClock(t, next.Format(time.RFC3339))
		results := race(2, func(i int) string {
			if i == 0 {
				return restoreStatus(f.restore(ctx, t, rent))
			}
			resp, err := f.budget.GetBudget(ctx, api.GetBudgetRequestObject{})
			if err != nil {
				return "error"
			}
			if _, is := resp.(api.GetBudget200JSONResponse); !is {
				return "not 200"
			}
			return "200"
		})
		if counts := tally(results); counts["200"] != 2 {
			t.Fatalf("round %d: statuses = %v, want two 200s", round, results)
		}
		ym := next.Format("2006-01")
		if got := f.scalar(t, `SELECT count(*)::text FROM allocations WHERE category_id = $1 AND year_month = $2`, rent, ym); got != "1" {
			t.Fatalf("round %d: allocation rows for the restored Category in %s = %s, want exactly 1", round, ym, got)
		}
	}
}

// The copy meeting a row another transaction is inserting at that moment writes
// nothing and fails nothing: ON CONFLICT DO NOTHING on the natural key. The
// copy's NOT EXISTS cannot see an uncommitted row, so it is the conflict clause
// that decides; without it this is a 23505 and a 500 for a request that did
// nothing wrong.
func TestTheCopyMeetingAConcurrentInsertOfTheSameRowWritesNothingAndFailsNothing(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-10-10T10:00:00+07:00")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`, householdID)

	bg := context.Background()
	tx, err := f.tdb.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(bg) }()
	if _, err := tx.Exec(bg, `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, '2026-11', '2026-11-01', $2, 9)`, householdID, rent); err != nil {
		t.Fatal(err)
	}

	type result struct {
		n   int64
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := f.tdb.Queries.CopyAllocationsForward(bg, db.CopyAllocationsForwardParams{
			HouseholdID: householdID,
			YearMonth:   "2026-11",
			EffectiveOn: pgtype.Date{Time: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Valid: true},
			Source:      "2026-10",
		})
		done <- result{n, err}
	}()
	f.waitForALockWait(t)
	if err := tx.Commit(bg); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got.err != nil || got.n != 0 {
		t.Errorf("copy = %d rows, %v; want 0 rows and no error", got.n, got.err)
	}
	if got := f.scalar(t, `SELECT string_agg(allocation::text, ',') FROM allocations WHERE year_month = '2026-11'`); got != "9.0000" {
		t.Errorf("November = %s, want the other transaction's row only", got)
	}
}

// A Category that already has ANY row in the Period is not carried again, whatever
// day the row is dated: the copy writes nothing next to a version a writer dated
// Today.
func TestTheCopyWritesNothingForACategoryThatAlreadyHasARowInThePeriod(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-10-10T10:00:00+07:00")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`, householdID)
	f.exec(t, `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, '2026-11', '2026-11-20', $2, 9)`, householdID, rent)

	n, err := f.tdb.Queries.CopyAllocationsForward(context.Background(), db.CopyAllocationsForwardParams{
		HouseholdID: householdID,
		YearMonth:   "2026-11",
		EffectiveOn: pgtype.Date{Time: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		Source:      "2026-10",
	})
	if err != nil || n != 0 {
		t.Errorf("copy = %d rows, %v; want 0 rows and no error", n, err)
	}
	if got := f.count(t, "allocations"); got != 2 {
		t.Errorf("allocations = %d, want the October row and the November one", got)
	}
}

// ---- tenancy, auth ----------------------------------------------------------

func TestUnknownAndForeignIdsAreOneNotFoundForDeleteAndRestore(t *testing.T) {
	f := newFixture(t)
	_, mine := f.member(t, "Mine", "a@example.com")
	_, theirs := f.member(t, "Theirs", "b@example.com")
	theirRent, theirFood := newID(t), newID(t)
	assertCreated(t, f.create(theirs, t, theirRent, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(theirs, t, theirFood, "Food", api.Daily, ptr("1000")))
	assertDeleted(t, f.del(theirs, t, theirFood))
	before := f.snapshotOf(t, "Theirs")

	unknown := newID(t)
	dCode, dBody := wireOfDelete(t, f.del(mine, t, unknown))
	rCode, rBody := wireOfRestore(t, f.restore(mine, t, unknown))
	if dCode != 404 || dBody != `{"code":"NOT_FOUND"}` || rCode != 404 || rBody != `{"code":"NOT_FOUND"}` {
		t.Fatalf("unknown id: delete %d %s, restore %d %s", dCode, dBody, rCode, rBody)
	}
	for _, id := range []uuid.UUID{theirRent, theirFood} {
		if c, b := wireOfDelete(t, f.del(mine, t, id)); c != dCode || b != dBody {
			t.Errorf("DELETE of a foreign %v = %d %s, want the unknown id's", id, c, b)
		}
		if c, b := wireOfRestore(t, f.restore(mine, t, id)); c != rCode || b != rBody {
			t.Errorf("restore of a foreign %v = %d %s, want the unknown id's", id, c, b)
		}
	}
	if after := f.snapshotOf(t, "Theirs"); after != before {
		t.Errorf("the other Household's rows changed:\n%s\n%s", before, after)
	}
}

// The Household is the session's, in every statement of the delete's
// transaction: the lock, the guard (the allocations it reads are the
// Household's), and the soft delete, each carrying the path's id.
func TestTheDeleteStatementsCarryTheSessionsHouseholdAndThePathsId(t *testing.T) {
	var seen []recorded
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { return recordingBeginner{Beginner: b, seen: &seen} })
	householdID, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))
	seen = nil

	assertDeleted(t, f.del(ctx, t, id))

	want := map[string]bool{"FOR UPDATE": false, "year_month <>": false, "SET\n    deleted_at": false}
	for _, r := range seen {
		for marker := range want {
			if !strings.Contains(r.sql, marker) {
				continue
			}
			want[marker] = true
			var hasHousehold, hasID bool
			for _, a := range r.args {
				if a == householdID {
					hasHousehold = true
				}
				if p, ok := a.(pgtype.UUID); ok && p.Bytes == id {
					hasID = true
				}
			}
			if !hasHousehold || !hasID {
				t.Errorf("the statement with %q carried household=%v id=%v: %v", marker, hasHousehold, hasID, r.args)
			}
		}
	}
	for marker, ok := range want {
		if !ok {
			t.Errorf("no statement with %q ran", marker)
		}
	}
}

func TestDeleteAndRestoreWithoutAUserAre401AndWriteNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))
	before := f.row(t, id)

	if got := deleteStatus(f.del(context.Background(), t, id)); got != "401" {
		t.Errorf("DeleteCategory = %s, want 401", got)
	}
	assertDeleted(t, f.del(ctx, t, id))
	deleted := f.row(t, id)
	if got := restoreStatus(f.restore(context.Background(), t, id)); got != "401" {
		t.Errorf("RestoreCategory = %s, want 401", got)
	}
	if f.row(t, id) != deleted || before == deleted {
		t.Error("an unauthenticated call changed the row")
	}
	if code, body := wireOfDelete(t, f.del(context.Background(), t, id)); code != 401 || body != `{"code":"UNAUTHORIZED"}` {
		t.Errorf("wire = %d %s", code, body)
	}
}

// ---- failure ----------------------------------------------------------------

type recorded struct {
	sql  string
	args []any
}

type recordingTx struct {
	pgx.Tx
	seen *[]recorded
}

func (tx recordingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	*tx.seen = append(*tx.seen, recorded{sql, args})
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx recordingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	*tx.seen = append(*tx.seen, recorded{sql, args})
	return tx.Tx.Exec(ctx, sql, args...)
}

func (tx recordingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	*tx.seen = append(*tx.seen, recorded{sql, args})
	return tx.Tx.Query(ctx, sql, args...)
}

type recordingBeginner struct {
	budget.Beginner
	seen *[]recorded
}

func (b recordingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return recordingTx{Tx: tx, seen: b.seen}, nil
}

// zeroRowsTx answers that the soft delete touched no row.
type zeroRowsTx struct{ pgx.Tx }

func (tx zeroRowsTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "SET\n    deleted_at") {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

type zeroRowsBeginner struct{ budget.Beginner }

func (b zeroRowsBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return zeroRowsTx{tx}, nil
}

// A failure at any step of the delete's transaction is a 500, never a 404 or a
// refusal, changes nothing, and logs no name. The 404 is the lock finding no
// row, and only that.
func TestAFailureAnywhereInTheDeleteTransactionIsA500AndChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		brk  breakingBeginner
	}{
		{"begin", breakingBeginner{failBegin: true}},
		{"the lock", breakingBeginner{failOn: "FOR UPDATE"}},
		{"the Household lookup", breakingBeginner{failOn: "FROM households"}},
		{"the guard", breakingBeginner{failOn: "year_month <>"}},
		{"the delete", breakingBeginner{failOn: "SET\n    deleted_at"}},
		{"commit", breakingBeginner{failCommit: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brk := tc.brk
			f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { brk.Beginner = b; return brk })
			_, ctx := f.member(t, "Home", "a@example.com")
			id := newID(t)
			assertCreated(t, f.create(ctx, t, id, "Secret Food Name", api.Daily, nil))
			before := f.row(t, id)
			var buf bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })

			if got := deleteStatus(f.del(ctx, t, id)); got != "500" {
				t.Errorf("DeleteCategory = %s, want 500", got)
			}
			if f.row(t, id) != before {
				t.Error("a failed delete left the row changed")
			}
			if strings.Contains(buf.String(), "Secret Food Name") {
				t.Errorf("the log leaks the name:\n%s", buf.String())
			}
			if buf.Len() == 0 {
				t.Error("nothing was logged")
			}
		})
	}
}

func TestADeleteWhoseUpdateTouchesNoRowIsA500(t *testing.T) {
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { return zeroRowsBeginner{b} })
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))
	slog.SetDefault(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if got := deleteStatus(f.del(ctx, t, id)); got != "500" {
		t.Errorf("DeleteCategory = %s, want 500", got)
	}
}

func TestAnUnknownTimeZoneIsA500ForADeleteOfALiveCategoryOnly(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	live, gone := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, live, "Food", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, gone, "Gone", api.Daily, nil))
	assertDeleted(t, f.del(ctx, t, gone))
	f.exec(t, `UPDATE users SET time_zone = 'Mars/Olympus'`)
	ctx = f.refresh(ctx, t)
	slog.SetDefault(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	if got := deleteStatus(f.del(ctx, t, live)); got != "500" {
		t.Errorf("DeleteCategory = %s, want 500", got)
	}
	if f.isDeleted(t, live) {
		t.Error("the failed delete deleted")
	}
	// A repeat needs no Period, so it needs no zone.
	assertDeleted(t, f.del(ctx, t, gone))
}

// errTx fails the statement that contains `on`, with err.
type errTx struct {
	pgx.Tx
	on  string
	err error
}

type errRowWith struct{ err error }

func (r errRowWith) Scan(...any) error { return r.err }

func (tx errTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, tx.on) {
		return errRowWith{tx.err}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

type errBeginner struct {
	budget.Beginner
	on  string
	err error
}

func (b errBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return errTx{Tx: tx, on: b.on, err: b.err}, nil
}

func TestARestoreErrorThatIsNotNoRowsOrTheNameIndexIsA500(t *testing.T) {
	for _, tc := range []struct {
		name string
		on   string
		err  error
		want string
	}{
		{"an outage", "deleted_at = NULL", errOutage, "500"},
		{"a 23505 on another index", "deleted_at = NULL", &pgconn.PgError{Code: "23505", ConstraintName: "something_else_key"}, "500"},
		{"a 23505 on the primary key", "deleted_at = NULL", &pgconn.PgError{Code: "23505", ConstraintName: "categories_pkey"}, "500"},
		{"a 23505 on the name index", "deleted_at = NULL", &pgconn.PgError{Code: "23505", ConstraintName: "categories_household_name_idx"}, "409 NAME_TAKEN"},
		{"ErrNoRows from the update", "deleted_at = NULL", pgx.ErrNoRows, "404 NOT_FOUND"},
		{"ErrNoRows from the lock", "FOR UPDATE", pgx.ErrNoRows, "404 NOT_FOUND"},
		{"ErrNoRows from the lock, wrapped", "FOR UPDATE", fmt.Errorf("scan: %w", pgx.ErrNoRows), "404 NOT_FOUND"},
		{"an outage at the lock", "FOR UPDATE", errOutage, "500"},
		{"a 23505 on the name index at the lock", "FOR UPDATE", &pgconn.PgError{Code: "23505", ConstraintName: "categories_household_name_idx"}, "500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { return errBeginner{Beginner: b, on: tc.on, err: tc.err} })
			_, ctx := f.member(t, "Home", "a@example.com")
			slog.SetDefault(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
			// A Category to find, deleted, so every statement of the restore is reached.
			id := newID(t)
			f.exec(t, `INSERT INTO categories (id, household_id, category_display_name, bucket, deleted_at) VALUES ($1, (SELECT id FROM households LIMIT 1), 'Gone', 'daily', now())`, id)
			if got := restoreStatus(f.restore(ctx, t, id)); got != tc.want {
				t.Errorf("RestoreCategory = %s, want %s", got, tc.want)
			}
		})
	}
}

// A failure at any step of the restore's transaction is a 500 and leaves the
// Category deleted and nothing written: not the clear, not the Period, not the copy.
func TestAFailureAnywhereInTheRestoreTransactionIsA500AndLeavesNothingBehind(t *testing.T) {
	for _, tc := range []struct {
		name         string
		brk          breakingBeginner
		materialised bool
	}{
		{"begin", breakingBeginner{failBegin: true}, false},
		{"the Household lookup", breakingBeginner{failOn: "FROM households"}, false},
		{"the marker", breakingBeginner{failOn: "INSERT INTO budgets"}, false},
		{"the Period's source choice", breakingBeginner{failOn: "FROM budgets b"}, false},
		{"the Period's copy", breakingBeginner{failOn: "FROM allocations s"}, false},
		{"the Category's source choice", breakingBeginner{failOn: "x.category_id = $"}, true},
		{"the Category's copy", breakingBeginner{failOn: "FROM allocations s"}, true},
		{"commit", breakingBeginner{failCommit: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brk := tc.brk
			f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { brk.Beginner = b; return brk })
			householdID, ctx := f.member(t, "Home", "a@example.com")
			f.setClock(t, "2026-11-10T10:00:00+07:00")
			rent := newID(t)
			f.exec(t, `INSERT INTO categories (id, household_id, category_display_name, bucket, deleted_at) VALUES ($1, $2, 'Secret Rent Name', 'commitment', now())`, rent, householdID)
			f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-10')`, householdID)
			f.exec(t, `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, '2026-10', '2026-10-01', $2, 424242)`, householdID, rent)
			if tc.materialised {
				f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`, householdID)
			}
			rows, markers, row := f.allocationRows(t), f.markers(t), f.row(t, rent)
			var buf bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })

			if got := restoreStatus(f.restore(ctx, t, rent)); got != "500" {
				t.Errorf("RestoreCategory = %s, want 500", got)
			}
			if f.row(t, rent) != row || f.allocationRows(t) != rows || f.markers(t) != markers {
				t.Error("a failed restore left something behind")
			}
			for _, secret := range []string{"Secret Rent Name", "424242"} {
				if strings.Contains(buf.String(), secret) {
					t.Errorf("the log leaks %q", secret)
				}
			}
		})
	}
}

func TestAnUnknownTimeZoneIsA500ForARestoreOfADeletedCategoryOnly(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	gone, live := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, gone, "Gone", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, live, "Live", api.Daily, nil))
	assertDeleted(t, f.del(ctx, t, gone))
	f.exec(t, `UPDATE users SET time_zone = 'Mars/Olympus'`)
	ctx = f.refresh(ctx, t)
	slog.SetDefault(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	if got := restoreStatus(f.restore(ctx, t, gone)); got != "500" {
		t.Errorf("RestoreCategory = %s, want 500", got)
	}
	if !f.isDeleted(t, gone) {
		t.Error("the failed restore restored")
	}
	// A live Category needs no Period, so it needs no zone.
	assertRestored(t, f.restore(ctx, t, live))
}

// What reaches the restore's statements is the session's Household and the path's
// id, and nothing else.
func TestTheRestoreStatementsCarryTheSessionsHouseholdAndThePathsId(t *testing.T) {
	var seen []recorded
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { return recordingBeginner{Beginner: b, seen: &seen} })
	householdID, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	f.setClock(t, "2026-10-10T10:00:00+07:00")
	assertCreated(t, f.create(ctx, t, id, "Rent", api.Commitment, ptr("1")))
	assertDeleted(t, f.del(ctx, t, id))
	f.setClock(t, "2026-11-10T10:00:00+07:00")
	seen = nil

	assertRestored(t, f.restore(ctx, t, id))

	want := map[string]bool{"FOR UPDATE": false, "deleted_at = NULL": false, "INSERT INTO allocations": false}
	for _, r := range seen {
		for marker := range want {
			if !strings.Contains(r.sql, marker) {
				continue
			}
			want[marker] = true
			var hasHousehold, hasID bool
			for _, a := range r.args {
				if a == householdID {
					hasHousehold = true
				}
				if p, ok := a.(pgtype.UUID); ok && p.Bytes == id {
					hasID = true
				}
			}
			if !hasHousehold || (marker != "INSERT INTO allocations" && !hasID) {
				t.Errorf("the statement with %q carried household=%v id=%v: %v", marker, hasHousehold, hasID, r.args)
			}
		}
	}
	for marker, ok := range want {
		if !ok {
			t.Errorf("no statement with %q ran", marker)
		}
	}
}

// ---- concurrency -------------------------------------------------------------

func TestConcurrentDeletesOfOneCategoryAreAll204(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Rent", api.Commitment, ptr("1")))

	results := race(12, func(int) string { return deleteStatus(f.del(ctx, t, id)) })
	if counts := tally(results); counts["204"] != 12 {
		t.Errorf("statuses = %v, want 12 x 204", counts)
	}
	if !f.isDeleted(t, id) {
		t.Error("not deleted")
	}
}

func TestConcurrentDeleteAndRestoreOfOneCategoryNeverFail(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))

	for round := range 20 {
		results := race(2, func(i int) string {
			if i == 0 {
				return deleteStatus(f.del(ctx, t, id))
			}
			return restoreStatus(f.restore(ctx, t, id))
		})
		for _, r := range results {
			if r != "204" && r != "200" {
				t.Fatalf("round %d: statuses = %v, want 204 and 200 only", round, results)
			}
		}
	}
}

func TestConcurrentRestoresIntoOneNameAnswerOne200AndOne409(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	for round := range 10 {
		name := fmt.Sprintf("Contested %d", round)
		a, b := newID(t), newID(t)
		assertCreated(t, f.create(ctx, t, a, name, api.Daily, nil))
		assertDeleted(t, f.del(ctx, t, a))
		assertCreated(t, f.create(ctx, t, b, strings.ToUpper(name), api.Daily, nil))
		assertDeleted(t, f.del(ctx, t, b))

		results := race(2, func(i int) string {
			if i == 0 {
				return restoreStatus(f.restore(ctx, t, a))
			}
			return restoreStatus(f.restore(ctx, t, b))
		})
		if counts := tally(results); counts["200"] != 1 || counts["409 NAME_TAKEN"] != 1 {
			t.Fatalf("round %d: statuses = %v, want one 200 and one 409 NAME_TAKEN", round, results)
		}
	}
}

// ---- the in-use race ----------------------------------------------------------
//
// #130 C8, proven red first. The rule: a deleted Category never holds an
// allocation in a Period other than the one it was deleted in. What can break it
// is a writer that puts an allocation in ANOTHER Period while the delete runs:
// the first read of the next Period carrying the Category forward, or a write
// from a User whose Today is already in the next Period (two time zones, or
// the delete a minute before a boundary the reader has crossed). The deleter's
// check sees only what has committed, so the write has to be ordered against the
// delete by a lock, not by luck.
//
// Each scenario holds the writer's transaction open, starts the delete, waits
// until Postgres says the delete is blocked on a lock, and only then commits the
// writer. The same scenario is run against a single `UPDATE ... WHERE NOT
// EXISTS`, the obvious statement, to show that it does not hold: it is the
// reason the handler takes the lock first and checks in a later statement.

// waitForALockWait returns once some backend is waiting for a lock.
func (f fixture) waitForALockWait(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := f.tdb.Pool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).Scan(&n); err != nil {
			t.Fatalf("pg_stat_activity: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no backend ever waited for a lock: the delete did not block on the writer")
}

func decemberPosition(t *testing.T) budget.Position {
	t.Helper()
	today, err := period.ParseDate("2026-12-01")
	if err != nil {
		t.Fatal(err)
	}
	p, err := period.Of(today, 1)
	if err != nil {
		t.Fatal(err)
	}
	return budget.Position{Today: today, Period: p}
}

// A writer is something that puts an allocation for Rent in December inside a
// transaction it has not committed yet.
type writer struct {
	name  string
	write func(t *testing.T, f fixture, tx pgx.Tx, householdID, rent pgtype.UUID)
}

var writers = []writer{
	{
		// A first read of December by a User already in it: the copy takes
		// FOR SHARE on the Categories it carries.
		"the carry-forward of a first read",
		func(t *testing.T, _ fixture, tx pgx.Tx, householdID, _ pgtype.UUID) {
			if err := budget.Materialise(context.Background(), db.New(tx), householdID, decemberPosition(t).Period); err != nil {
				t.Fatalf("materialise: %v", err)
			}
		},
	},
	{
		// A PATCH /budget by a User already in December, on a Period that was
		// already materialised without the Category.
		"an allocation write",
		func(t *testing.T, f fixture, tx pgx.Tx, householdID, rent pgtype.UUID) {
			f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-12')`, householdID)
			if err := budget.SetAllocation(context.Background(), db.New(tx), householdID, decemberPosition(t), rent, decimal.RequireFromString("5")); err != nil {
				t.Fatalf("set allocation: %v", err)
			}
		},
	},
}

// naiveDelete is the obvious statement: the guard inside the UPDATE's WHERE.
const naiveDelete = `
	UPDATE categories SET deleted_at = coalesce(deleted_at, now()), updated_at = now()
	WHERE id = $1 AND household_id = $2
	  AND NOT EXISTS (SELECT 1 FROM allocations a
	                  WHERE a.household_id = $2 AND a.category_id = $1 AND a.year_month <> $3 AND a.allocation IS NOT NULL)`

type raceOutcome struct {
	status       string // the deleter's answer
	deleted      bool
	decemberRows int
}

func (o raceOutcome) broken() bool { return o.deleted && o.decemberRows > 0 }

// runInUseRace: November holds Rent's only allocation (so a delete in November
// is allowed), a December writer holds its allocation uncommitted, and the
// delete, issued by a User in November, blocks behind it.
func (f fixture) runInUseRace(t *testing.T, w writer, naive bool) raceOutcome {
	t.Helper()
	householdID, ctx := f.member(t, "Home", "a@example.com")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	rentID := pgtype.UUID{Bytes: rent, Valid: true}

	bg := context.Background()
	tx, err := f.tdb.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(bg) }()
	w.write(t, f, tx, householdID, rentID)

	done := make(chan string, 1)
	go func() {
		if naive {
			tag, err := f.tdb.Pool.Exec(bg, naiveDelete, rentID, householdID, "2026-11")
			switch {
			case err != nil:
				done <- "error " + err.Error()
			case tag.RowsAffected() == 1:
				done <- "204"
			default:
				done <- "409 CATEGORY_IN_USE"
			}
			return
		}
		resp, err := f.handlers.DeleteCategory(ctx, api.DeleteCategoryRequestObject{Id: rent})
		if err != nil {
			done <- "error " + err.Error()
			return
		}
		done <- deleteStatus(resp)
	}()

	f.waitForALockWait(t)
	if err := tx.Commit(bg); err != nil {
		t.Fatalf("commit the writer: %v", err)
	}
	status := <-done

	return raceOutcome{
		status:       status,
		deleted:      f.isDeleted(t, rent),
		decemberRows: f.count2(t, `SELECT count(*) FROM allocations WHERE category_id = $1 AND year_month = '2026-12'`, rent),
	}
}

func (f fixture) count2(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.tdb.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s -> %v", sql, err)
	}
	return n
}

// The delete waits for the writer, then sees its allocation and refuses; the
// Category is not deleted and December keeps its row.
func TestADeleteRacingAnAllocationWriteForAnotherPeriodIsRefused(t *testing.T) {
	for _, w := range writers {
		t.Run(w.name, func(t *testing.T) {
			f := newFixture(t)
			got := f.runInUseRace(t, w, false)
			if got.status != "409 CATEGORY_IN_USE" || got.deleted || got.decemberRows != 1 {
				t.Errorf("outcome = %+v, want 409 CATEGORY_IN_USE, not deleted, December's allocation kept", got)
			}
			if got.broken() {
				t.Error("a deleted Category holds an allocation in another Period")
			}
		})
	}
}

// The red proof: the same scenarios against `UPDATE ... WHERE NOT EXISTS` leave
// a deleted Category holding December's allocation. That the UPDATE waits for
// the writer's lock and then proceeds on a check made before it waited is the
// whole reason for the lock-then-check order; if this ever starts passing as
// "refused", Postgres changed and the handler's two steps are worth a rethink,
// not a quiet deletion.
func TestASingleUpdateWithTheGuardInItsWhereLosesTheRace(t *testing.T) {
	for _, w := range writers {
		t.Run(w.name, func(t *testing.T) {
			f := newFixture(t)
			got := f.runInUseRace(t, w, true)
			if !got.broken() {
				t.Errorf("outcome = %+v: the single statement was expected to delete a Category that December holds an allocation for", got)
			}
		})
	}
}

// The other order: the delete holds the row, and a writer that arrives while it
// does is refused by the same lock, so nothing is added to a deleted Category.
// The delete's two statements are held open by hand.
func TestAnAllocationWriteRacingADeleteIsRefusedOrNotCopied(t *testing.T) {
	t.Run("the carry-forward of a first read copies nothing", func(t *testing.T) {
		f := newFixture(t)
		householdID, ctx := f.member(t, "Home", "a@example.com")
		rent, food := newID(t), newID(t)
		assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
		assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, ptr("1000")))

		bg := context.Background()
		tx := f.holdDelete(t, householdID, rent)
		defer func() { _ = tx.Rollback(bg) }()
		done := make(chan error, 1)
		go func() {
			// A first read of December by a User already in it: its own transaction.
			tx, err := f.tdb.Pool.Begin(bg)
			if err != nil {
				done <- err
				return
			}
			defer func() { _ = tx.Rollback(bg) }()
			if err := budget.Materialise(bg, db.New(tx), householdID, decemberPosition(t).Period); err != nil {
				done <- err
				return
			}
			done <- tx.Commit(bg)
		}()
		f.waitForALockWait(t)
		if err := tx.Commit(bg); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatalf("materialise: %v", err)
		}
		if v := f.allocations(t); v != "2026-11 2026-11-14 Food 1000.0000; 2026-11 2026-11-14 Rent 3000000.0000; 2026-12 2026-12-01 Food 1000.0000" {
			t.Errorf("allocations = %q: December must carry Food and not the Category being deleted", v)
		}
	})

	t.Run("an allocation write is refused as not live", func(t *testing.T) {
		f := newFixture(t)
		householdID, ctx := f.member(t, "Home", "a@example.com")
		rent := newID(t)
		assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))

		bg := context.Background()
		tx := f.holdDelete(t, householdID, rent)
		defer func() { _ = tx.Rollback(bg) }()
		done := make(chan api.UpdateBudgetResponseObject, 1)
		go func() {
			resp, err := f.budget.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{
				Allocations: &[]api.AllocationChange{{CategoryId: rent, Allocation: ptr("1")}},
			}})
			if err != nil {
				t.Errorf("UpdateBudget: %v", err)
			}
			done <- resp
		}()
		f.waitForALockWait(t)
		if err := tx.Commit(bg); err != nil {
			t.Fatal(err)
		}
		resp := <-done
		bad, is := resp.(api.UpdateBudget400JSONResponse)
		if !is || bad.Code != api.VALIDATION || fmt.Sprint(*bad.Args) != "map[field:category_id rule:invalid]" {
			t.Errorf("UpdateBudget = %#v, want 400 VALIDATION {category_id, invalid}", resp)
		}
		if got := f.allocations(t); got != "2026-11 2026-11-14 Rent 3000000.0000" {
			t.Errorf("allocations = %q, want only the original", got)
		}
	})
}

// holdDelete runs the delete's own statements, as the handler does, in a
// transaction that stays open: the row is locked and soft-deleted, uncommitted.
func (f fixture) holdDelete(t *testing.T, householdID pgtype.UUID, id uuid.UUID) pgx.Tx {
	t.Helper()
	bg := context.Background()
	tx, err := f.tdb.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	q := db.New(tx)
	p := db.LockCategoryParams{ID: pgtype.UUID{Bytes: id, Valid: true}, HouseholdID: householdID}
	if _, err := q.LockCategory(bg, p); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := q.SoftDeleteCategory(bg, db.SoftDeleteCategoryParams{ID: p.ID, HouseholdID: householdID}); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	return tx
}
