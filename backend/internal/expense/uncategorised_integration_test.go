package expense_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
)

// GET /expenses?uncategorised=true and uncategorised_count (#140 X8, R8) against
// a real Postgres, at the handler. A value that is not a boolean is the binder's
// (`VALIDATION {uncategorised, invalid}`) and is proven through the router and the
// conformance suite.

// mixed seeds eleven live Expenses over four days, every other one with a
// Category, plus a deleted uncategorised one and another Household's. The
// uncategorised ones are the odd numbers; the order has times, so the filter
// has to keep the effective time and not only the date.
func (f fixture) mixed(t *testing.T) (ctx context.Context, household, pocket pgtype.UUID, uncategorised []uuid.UUID, all []uuid.UUID) {
	t.Helper()
	household, pocket, c := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	days := []string{"2026-11-24", "2026-11-24", "2026-11-24", "2026-11-23", "2026-11-23", "2026-11-23", "2026-11-22", "2026-11-22", "2026-11-22", "2026-11-20", "2026-11-20"}
	times := []string{"", "09:00", "01:30", "23:00", "", "04:00", "", "", "12:00", "", "07:00"}
	for i := 1; i <= 11; i++ {
		s := seed{
			id:           sid(i),
			occurredOn:   days[i-1],
			occurredTime: times[i-1],
			capturedAt:   fmt.Sprintf("2026-11-2%dT05:%02d:00Z", 1+i%3, i),
		}
		if i%2 == 0 {
			s.category = category
		}
		f.insert(t, household, pocket, s)
	}
	f.insert(t, household, pocket, seed{id: sid(90), occurredOn: "2026-11-24", capturedAt: "2026-11-24T05:59:00Z", deleted: true})
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	f.insert(t, other, f.tdb.CreatePocket(t, other, "Theirs"), seed{id: sid(91), occurredOn: "2026-11-24", capturedAt: "2026-11-24T05:59:00Z"})

	for _, e := range f.list(c, t, ptr(100), nil).Items {
		all = append(all, e.Id)
		if e.CategoryId == nil {
			uncategorised = append(uncategorised, e.Id)
		}
	}
	return c, household, pocket, uncategorised, all
}

func TestTheFilterIsTheSameListLessTheCategorisedRowsInTheSameOrder(t *testing.T) {
	f := newFixture(t)
	ctx, _, _, uncategorised, all := f.mixed(t)

	if len(all) != 11 || len(uncategorised) != 6 {
		t.Fatalf("seeded %d live, %d uncategorised; want 11 and 6", len(all), len(uncategorised))
	}
	// The deleted and the foreign uncategorised rows are in neither.
	for _, id := range all {
		if id == sid(90) || id == sid(91) {
			t.Fatalf("the unfiltered list holds %v", id)
		}
	}

	filtered := f.listFiltered(ctx, t, ptr(100), nil, ptr(true))
	if got := ids(filtered); !slices.Equal(got, uncategorised) {
		t.Errorf("filtered = %v\nwant the unfiltered list's uncategorised rows, in its order, %v", got, uncategorised)
	}
	for _, e := range filtered.Items {
		if e.CategoryId != nil {
			t.Errorf("%v carries a Category in the uncategorised list", e.Id)
		}
	}
}

// false is the same as absent: the whole list.
func TestFalseAndAbsentAreTheWholeList(t *testing.T) {
	f := newFixture(t)
	ctx, _, _, _, all := f.mixed(t)

	for name, p := range map[string]*bool{"absent": nil, "false": ptr(false)} {
		page := f.listFiltered(ctx, t, ptr(100), nil, p)
		if got := ids(page); !slices.Equal(got, all) {
			t.Errorf("%s: %v, want all %v", name, got, all)
		}
	}
}

// The count is the Household's live uncategorised rows: not the filter's, not the
// page's, not the deleted or another Household's.
func TestTheCountIsIndependentOfTheFilterAndThePage(t *testing.T) {
	f := newFixture(t)
	ctx, _, _, _, _ := f.mixed(t)

	for name, tc := range map[string]struct {
		limit  int
		filter *bool
	}{
		"whole list, whole page":     {100, nil},
		"whole list, one row":        {1, nil},
		"filtered, whole page":       {100, ptr(true)},
		"filtered, one row":          {1, ptr(true)},
		"filter false, a short page": {3, ptr(false)},
	} {
		page := f.listFiltered(ctx, t, &tc.limit, nil, tc.filter)
		if uncount(page) != 6 {
			t.Errorf("%s: uncategorised_count = %d, want 6", name, uncount(page))
		}
	}

	// Later pages of a walk carry it too, and a page past the filter's last row.
	first := f.listFiltered(ctx, t, ptr(4), nil, ptr(true))
	second := f.listFiltered(ctx, t, ptr(4), first.Next, ptr(true))
	if uncount(first) != 6 || uncount(second) != 6 {
		t.Errorf("counts across pages = %d, %d; want 6, 6", uncount(first), uncount(second))
	}
}

func TestACountOfZeroIsOnTheWire(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	f.insert(t, household, pocket, seed{id: sid(1), occurredOn: "2026-11-23", capturedAt: "2026-11-23T05:00:00Z", category: category})

	for _, p := range []*bool{nil, ptr(true)} {
		page := f.listFiltered(ctx, t, nil, nil, p)
		if !strings.Contains(wire(t, page), `"uncategorised_count":0`) {
			t.Errorf("filter %v: %s, want a zero count on the wire", p, wire(t, page))
		}
	}
	if got := ids(f.listFiltered(ctx, t, nil, nil, ptr(true))); len(got) != 0 {
		t.Errorf("filtered list of a Household with no uncategorised row = %v", got)
	}
}

// Keyset paging inside the filtered list: every page boundary, at every size,
// reads the same rows in the same order as the unpaged filter, with no repeat and
// no skip.
func TestPagingThroughTheFilteredListIsExact(t *testing.T) {
	f := newFixture(t)
	ctx, _, _, uncategorised, _ := f.mixed(t)

	for size := 1; size <= 7; size++ {
		var got []uuid.UUID
		var after *string
		pages := 0
		for {
			pages++
			page := f.listFiltered(ctx, t, &size, after, ptr(true))
			got = append(got, ids(page)...)
			if page.Next == nil {
				break
			}
			if pages > 20 {
				t.Fatal("paging does not end")
			}
			after = page.Next
		}
		if !slices.Equal(got, uncategorised) {
			t.Errorf("size %d: %v, want %v", size, got, uncategorised)
		}
		if want := (len(uncategorised) + size - 1) / size; pages != want {
			t.Errorf("size %d: %d pages, want %d", size, pages, want)
		}
	}
}

// A cursor names a position in one order that both lists share, so one issued for
// either list resumes in the other: nothing is repeated, nothing skipped.
func TestACursorOfEitherListResumesInTheOther(t *testing.T) {
	f := newFixture(t)
	ctx, _, _, uncategorised, all := f.mixed(t)

	whole := f.listFiltered(ctx, t, ptr(5), nil, nil)
	cont := f.listFiltered(ctx, t, ptr(100), whole.Next, ptr(true))
	last := ids(whole)[4]
	var want []uuid.UUID
	for _, id := range uncategorised {
		if slices.Index(all, id) > slices.Index(all, last) {
			want = append(want, id)
		}
	}
	if got := ids(cont); !slices.Equal(got, want) {
		t.Errorf("a whole-list cursor in the filtered list read %v, want %v", got, want)
	}

	filtered := f.listFiltered(ctx, t, ptr(2), nil, ptr(true))
	cont = f.listFiltered(ctx, t, ptr(100), filtered.Next, nil)
	last = ids(filtered)[1]
	want = all[slices.Index(all, last)+1:]
	if got := ids(cont); !slices.Equal(got, want) {
		t.Errorf("a filtered cursor in the whole list read %v, want %v", got, want)
	}
}

// An Expense edited to no Category joins the list and the count; one given a
// Category leaves both. The row the client was looking at moves with the edit.
func TestAnEditMovesAnExpenseInAndOutOfTheFilterAndTheCount(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	food := f.tdb.CreateCategory(t, household, "Food", "daily")
	id, _ := f.stored(ctx, t, pocket, food)

	page := f.listFiltered(ctx, t, nil, nil, ptr(true))
	if len(page.Items) != 0 || uncount(page) != 0 {
		t.Fatalf("a categorised Expense is in the filter: %s", wire(t, page))
	}

	assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{CategoryId: nullOf[uuid.UUID]()}))
	page = f.listFiltered(ctx, t, nil, nil, ptr(true))
	if got := ids(page); !slices.Equal(got, []uuid.UUID{id.Bytes}) || uncount(page) != 1 {
		t.Errorf("after category_id: null: %s, want the Expense and a count of 1", wire(t, page))
	}
	if whole := f.list(ctx, t, nil, nil); uncount(whole) != 1 {
		t.Errorf("the whole list says %d, want 1", uncount(whole))
	}

	assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{CategoryId: valueOf(uuid.UUID(food.Bytes))}))
	page = f.listFiltered(ctx, t, nil, nil, ptr(true))
	if len(page.Items) != 0 || uncount(page) != 0 {
		t.Errorf("after a Category was set: %s, want nothing and a count of 0", wire(t, page))
	}
	if whole := f.list(ctx, t, nil, nil); len(whole.Items) != 1 || uncount(whole) != 0 {
		t.Errorf("the whole list = %s, want the row and a count of 0", wire(t, whole))
	}
}

// A soft-deleted uncategorised Expense is not counted and not listed, and counts
// again when it is restored.
func TestADeletedExpenseIsNeitherListedNorCounted(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	f.insert(t, household, pocket, seed{id: sid(1), occurredOn: "2026-11-23", capturedAt: "2026-11-23T05:00:00Z"})
	f.insert(t, household, pocket, seed{id: sid(2), occurredOn: "2026-11-23", capturedAt: "2026-11-23T06:00:00Z"})

	assertStatus(t, f.del(ctx, t, pgtype.UUID{Bytes: sid(2), Valid: true}), "204")
	page := f.listFiltered(ctx, t, nil, nil, ptr(true))
	if got := ids(page); !slices.Equal(got, []uuid.UUID{sid(1)}) || uncount(page) != 1 {
		t.Errorf("with one deleted: %s", wire(t, page))
	}
	assertStatus(t, f.restore(ctx, t, pgtype.UUID{Bytes: sid(2), Valid: true}), "200")
	if page := f.listFiltered(ctx, t, nil, nil, ptr(true)); len(page.Items) != 2 || uncount(page) != 2 {
		t.Errorf("restored: %s", wire(t, page))
	}
}

// Tenancy: another Household's uncategorised rows are in neither the list nor the
// count, whichever Household reads.
func TestTheCountAndTheFilterStayInTheCallersHousehold(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
	theirs := f.userIn(t, other, "b@example.com")
	f.insert(t, household, pocket, seed{id: sid(1), occurredOn: "2026-11-23", capturedAt: "2026-11-23T05:00:00Z"})
	for i := 2; i <= 4; i++ {
		f.insert(t, other, theirPocket, seed{id: sid(i), occurredOn: "2026-11-23", capturedAt: "2026-11-23T05:00:00Z"})
	}

	mine := f.listFiltered(ctx, t, nil, nil, ptr(true))
	their := f.listFiltered(theirs, t, nil, nil, ptr(true))
	if got := ids(mine); !slices.Equal(got, []uuid.UUID{sid(1)}) || uncount(mine) != 1 {
		t.Errorf("mine = %s", wire(t, mine))
	}
	if len(their.Items) != 3 || uncount(their) != 3 {
		t.Errorf("theirs = %s", wire(t, their))
	}
}

// uncount is a live page's uncategorised_count, or -1 when the page omitted it,
// which a live read never does (it is omitted only from the Recycle Bin's).
func uncount(p api.ExpenseList) int {
	if p.UncategorisedCount == nil {
		return -1
	}
	return *p.UncategorisedCount
}
