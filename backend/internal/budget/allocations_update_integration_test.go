package budget_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// PATCH /budget's `allocations` (#130 R1, R2, R10): what a Category is planned
// at in the current Period, set to an amount, to zero, or (Daily only) to none.
// The Categories are made with SQL, so these tests do not depend on the
// category package.

// alloc is one item of a request: amount "" is the JSON `null` (none).
func alloc(categoryID pgtype.UUID, amount string) api.AllocationChange {
	c := api.AllocationChange{CategoryId: categoryID.Bytes}
	c.Allocation = nullable.NewNullNullable[string]()
	if amount != "" {
		c.Allocation = nullable.NewNullableWithValue(amount)
	}
	return c
}

// patchAllocs sends a body with the given items and, when monthly is not empty,
// a Monthly Budget.
func (f fixture) patchAllocs(ctx context.Context, t *testing.T, monthly string, items ...api.AllocationChange) api.UpdateBudgetResponseObject {
	t.Helper()
	body := api.UpdateBudgetJSONRequestBody{Allocations: &items}
	if monthly != "" {
		body.MonthlyBudget = &monthly
	}
	resp, err := f.handlers.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &body})
	if err != nil {
		t.Fatalf("UpdateBudget returned an error: %v", err)
	}
	return resp
}

func (f fixture) mustPatchAllocs(ctx context.Context, t *testing.T, monthly string, items ...api.AllocationChange) api.Budget {
	t.Helper()
	got, is := f.patchAllocs(ctx, t, monthly, items...).(api.UpdateBudget200JSONResponse)
	if !is {
		t.Fatalf("UpdateBudget did not answer 200")
	}
	return api.Budget(got)
}

// refusal is the 400 VALIDATION an UpdateBudget answered, "field rule".
func refusal(t *testing.T, resp api.UpdateBudgetResponseObject) string {
	t.Helper()
	got, is := resp.(api.UpdateBudget400JSONResponse)
	if !is {
		t.Fatalf("UpdateBudget = %#v, want 400", resp)
	}
	if got.Code != api.VALIDATION || got.Args == nil {
		t.Fatalf("400 = %s %v, want VALIDATION with args", got.Code, got.Args)
	}
	return fmt.Sprintf("%v %v", (*got.Args)["field"], (*got.Args)["rule"])
}

// everything is every Budget row of every Household, whole, so "nothing was
// written" is a byte comparison.
func (f fixture) everything(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT (SELECT coalesce(string_agg(to_jsonb(b)::text, ';' ORDER BY household_id, year_month), '') FROM budgets b) || '|' ||
		        (SELECT coalesce(string_agg(to_jsonb(m)::text, ';' ORDER BY household_id, year_month, effective_on), '') FROM monthly_budgets m) || '|' ||
		        (SELECT coalesce(string_agg(to_jsonb(a)::text, ';' ORDER BY household_id, year_month, effective_on, category_id), '') FROM allocations a)`).Scan(&s); err != nil {
		t.Fatalf("read everything: %v", err)
	}
	return s
}

func TestAnAllocationIsSetInTheCurrentPeriodDatedTodayAndMovesTheFigures(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	food := f.category(t, householdID, "Food", "daily")

	got := f.mustPatchAllocs(ctx, t, "9000000", alloc(rent, "3000000"), alloc(food, "2000000.5"))

	if listed(got) != rent.String()+"=3000000.0000,"+food.String()+"=2000000.5000" {
		t.Errorf("allocations = %q", listed(got))
	}
	// A Commitment moves the Daily Pool; a Daily Category only Unallocated.
	if got.CommitmentsBudget != "3000000.0000" || deref(got.DailyPool) != "6000000.0000" || deref(got.Unallocated) != "3999999.5000" {
		t.Errorf("commitments %s, pool %s, unallocated %s", got.CommitmentsBudget, deref(got.DailyPool), deref(got.Unallocated))
	}
	if v := f.allocationShape(t, householdID); v != "2026-11 2026-11-14 Food 2000000.5000; 2026-11 2026-11-14 Rent 3000000.0000" {
		t.Errorf("allocations rows = %q, want two versions dated Today in the current Period", v)
	}
	if again := f.get(ctx, t); listed(again) != listed(got) {
		t.Errorf("a read after = %q, want what the write answered, %q", listed(again), listed(got))
	}
}

// R2: past Periods keep what they held, and a Period that is not yet current is
// not written ahead of time.
func TestAPatchNeverTouchesAnotherPeriod(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.clock.set(t, "2026-10-10T10:00:00+07:00")
	f.patch(ctx, t, "9000000")
	f.allocate(t, householdID, "2026-10", "2026-10-10", rent, "3000000")
	f.clock.set(t, "2026-11-14T10:00:00+07:00")
	octoberBefore := f.allocationsRows(t, householdID)

	got := f.mustPatchAllocs(ctx, t, "", alloc(rent, "3500000"))

	if listed(got) != rent.String()+"=3500000.0000" {
		t.Errorf("November = %q", listed(got))
	}
	if v := f.allocationShape(t, householdID); v != "2026-10 2026-10-10 Rent 3000000.0000; 2026-11 2026-11-01 Rent 3000000.0000; 2026-11 2026-11-14 Rent 3500000.0000" {
		t.Errorf("allocations = %q: October untouched, November's carried copy kept and the change a version dated Today", v)
	}
	if !strings.HasPrefix(f.allocationsRows(t, householdID), octoberBefore) {
		t.Errorf("October's row changed:\n%s\n%s", octoberBefore, f.allocationsRows(t, householdID))
	}
	if labels := f.markerLabels(t, householdID); labels != "2026-10,2026-11" {
		t.Errorf("budgets = %q, want no Period but the two", labels)
	}
}

func TestZeroIsAnAllocationAndNullClearsADailyCategoryOnly(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	food := f.category(t, householdID, "Food", "daily")
	f.mustPatchAllocs(ctx, t, "9000000", alloc(rent, "3000000"), alloc(food, "2000000"))

	// "0" is an allocation: a retired Commitment is freed this way, and stays on
	// the list at 0.0000.
	got := f.mustPatchAllocs(ctx, t, "", alloc(rent, "0"))
	if listed(got) != rent.String()+"=0.0000,"+food.String()+"=2000000.0000" || got.CommitmentsBudget != "0.0000" || deref(got.DailyPool) != "9000000.0000" {
		t.Errorf("after zeroing: %q commitments %s pool %s", listed(got), got.CommitmentsBudget, deref(got.DailyPool))
	}

	// null for a Daily Category is none: absent from the list, not zero.
	got = f.mustPatchAllocs(ctx, t, "", alloc(food, ""))
	if listed(got) != rent.String()+"=0.0000" || deref(got.Unallocated) != "9000000.0000" {
		t.Errorf("after clearing Daily: %q unallocated %s", listed(got), deref(got.Unallocated))
	}
	// A NULL version, the same Period Day: the row that said 2000000 now says none.
	if v := f.allocationShape(t, householdID); v != "2026-11 2026-11-14 Food none; 2026-11 2026-11-14 Rent 0.0000" {
		t.Errorf("allocations = %q", v)
	}

	// And it stays none in the Period that follows: a cleared Category carries
	// nothing, a zeroed one carries its zero.
	f.clock.set(t, "2026-12-03T10:00:00+07:00")
	got = f.get(ctx, t)
	if listed(got) != rent.String()+"=0.0000" {
		t.Errorf("December = %q, want the zero carried and the cleared one not", listed(got))
	}
}

// R10: a Commitment's allocation is never null, and the refusal writes nothing,
// not even what the same request carried that was fine.
func TestNullForACommitmentIsRefusedAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	food := f.category(t, householdID, "Food", "daily")
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, rent)
	before := f.everything(t)

	resp := f.patchAllocs(ctx, t, "9000000", alloc(food, "1000000"), alloc(rent, ""))
	if got := refusal(t, resp); got != "allocation required" {
		t.Errorf("refusal = %q, want allocation required (an inactive Commitment is still a Commitment)", got)
	}
	if after := f.everything(t); after != before {
		t.Errorf("a refused request wrote:\n%s\n%s", before, after)
	}
}

func TestACategoryListedTwiceIsRefusedAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	food := f.category(t, householdID, "Food", "daily")
	before := f.everything(t)

	resp := f.patchAllocs(ctx, t, "9000000", alloc(food, "1"), alloc(food, "2"))
	if got := refusal(t, resp); got != "category_id unique" {
		t.Errorf("refusal = %q, want category_id unique", got)
	}
	// Whatever the repeats say, including the same amount twice.
	resp = f.patchAllocs(ctx, t, "", alloc(food, "1"), alloc(food, "1"))
	if got := refusal(t, resp); got != "category_id unique" {
		t.Errorf("refusal = %q, want category_id unique", got)
	}
	if after := f.everything(t); after != before {
		t.Errorf("a refused request wrote:\n%s\n%s", before, after)
	}
}

// Unknown, another Household's and deleted are one answer, the same bytes, so a
// foreign id is never confirmed (BOOTSTRAP §4, tenancy on a path id; here the id
// is in the body).
func TestAnUnknownForeignOrDeletedCategoryIsOneAnswerAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	mine, _, ctx := f.member(t, "Home", "a@example.com")
	theirs, _, _ := f.member(t, "Other", "b@example.com")
	food := f.category(t, mine, "Food", "daily")
	gone := f.category(t, mine, "Gone", "daily")
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, gone)
	foreign := f.category(t, theirs, "Foreign", "daily")
	foreignRent := f.category(t, theirs, "Their rent", "commitment")
	unknown := testutil.NewUUID(t)
	before := f.everything(t)

	for name, id := range map[string]pgtype.UUID{"unknown": unknown, "deleted": gone, "foreign": foreign, "foreign Commitment": foreignRent} {
		// An amount, and a null (which for a foreign Commitment must not turn
		// into the Commitment rule: nothing may tell the foreign bucket).
		for _, amount := range []string{"5", ""} {
			resp := f.patchAllocs(ctx, t, "9000000", alloc(food, "1"), alloc(id, amount))
			if got := refusal(t, resp); got != "category_id invalid" {
				t.Errorf("%s (amount %q): refusal = %q, want category_id invalid", name, amount, got)
			}
		}
	}
	if after := f.everything(t); after != before {
		t.Errorf("a refused request wrote, or touched the other Household:\n%s\n%s", before, after)
	}
}

// The order the handler's own rules are applied in is deterministic: a repeat
// first, then the items in the order sent.
func TestTheHandlersRulesAreAppliedInAFixedOrder(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	food := f.category(t, householdID, "Food", "daily")
	unknown := testutil.NewUUID(t)

	for _, tc := range []struct {
		name  string
		items []api.AllocationChange
		want  string
	}{
		{"a repeat outranks everything", []api.AllocationChange{alloc(unknown, "1"), alloc(rent, ""), alloc(food, "1"), alloc(food, "2")}, "category_id unique"},
		{"null for a Commitment, then an unknown id", []api.AllocationChange{alloc(rent, ""), alloc(unknown, "1")}, "allocation required"},
		{"an unknown id, then null for a Commitment", []api.AllocationChange{alloc(unknown, "1"), alloc(rent, "")}, "category_id invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := refusal(t, f.patchAllocs(ctx, t, "", tc.items...)); got != tc.want {
				t.Errorf("refusal = %q, want %q", got, tc.want)
			}
		})
	}
}

// A Category that was live when the request was checked and is deleted before
// it is written: the same answer as one that was never live, and nothing of the
// request survives, the marker of a fresh Period included.
func TestACategoryDeletedBetweenTheCheckAndTheWriteRollsTheWholeRequestBack(t *testing.T) {
	var f fixture
	var doomed pgtype.UUID
	f = newFixtureWith(t, func(q db.Querier) db.Querier {
		return deletingQuerier{Querier: q, after: func() {
			f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, doomed)
		}}
	}, nil)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	first := f.category(t, householdID, "First", "daily")
	doomed = f.category(t, householdID, "Second", "daily")
	before := f.everything(t)

	resp := f.patchAllocs(ctx, t, "9000000", alloc(first, "1"), alloc(doomed, "2"))

	if got := refusal(t, resp); got != "category_id invalid" {
		t.Errorf("refusal = %q, want category_id invalid", got)
	}
	if after := f.everything(t); after != before {
		t.Errorf("the request left something behind:\n%s\n%s", before, after)
	}
}

// deletingQuerier runs a hook right after the Categories were listed: the
// moment between the handler's check and its writes.
type deletingQuerier struct {
	db.Querier
	after func()
}

func (q deletingQuerier) ListCategories(ctx context.Context, householdID pgtype.UUID) ([]db.Category, error) {
	rows, err := q.Querier.ListCategories(ctx, householdID)
	if err == nil {
		q.after()
	}
	return rows, err
}

func TestAnEmptyAllocationListIsANoOpThatStillAnswersTheBudget(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	want := f.mustPatchAllocs(ctx, t, "9000000", alloc(rent, "3000000"))
	before := f.everything(t)

	got := f.mustPatchAllocs(ctx, t, "")
	if listed(got) != listed(want) || got.CommitmentsBudget != want.CommitmentsBudget {
		t.Errorf("empty list answered %q, want %q", listed(got), listed(want))
	}
	if after := f.everything(t); after != before {
		t.Errorf("an empty list wrote:\n%s\n%s", before, after)
	}
}

// Merge: what a request leaves out is untouched, in both directions.
func TestAMonthlyBudgetOnlyAndAnAllocationOnlyPatchEachLeaveTheOtherAlone(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	food := f.category(t, householdID, "Food", "daily")
	f.mustPatchAllocs(ctx, t, "9000000", alloc(rent, "3000000"), alloc(food, "2000000"))

	got := f.patch(ctx, t, "10000000")
	if listed(got) != rent.String()+"=3000000.0000,"+food.String()+"=2000000.0000" || deref(got.MonthlyBudget) != "10000000.0000" {
		t.Errorf("a Monthly Budget only: %q monthly %s", listed(got), deref(got.MonthlyBudget))
	}
	got = f.mustPatchAllocs(ctx, t, "", alloc(food, "1"))
	if listed(got) != rent.String()+"=3000000.0000,"+food.String()+"=1.0000" || deref(got.MonthlyBudget) != "10000000.0000" {
		t.Errorf("one allocation only: %q monthly %s", listed(got), deref(got.MonthlyBudget))
	}
}

// C4: the same Period Day overwrites, a later one adds a version and kinks.
func TestAnAllocationChangedTheSameDayOverwritesAndOnALaterDayIsANewVersion(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.mustPatchAllocs(ctx, t, "9000000", alloc(rent, "3000000"))

	f.clock.set(t, "2026-11-14T22:00:00+07:00")
	f.mustPatchAllocs(ctx, t, "", alloc(rent, "3100000"))
	if v := f.allocationShape(t, householdID); v != "2026-11 2026-11-14 Rent 3100000.0000" {
		t.Errorf("same day: %q, want the one version overwritten", v)
	}

	f.clock.set(t, "2026-11-15T10:00:00+07:00")
	got := f.mustPatchAllocs(ctx, t, "", alloc(rent, "3200000"))
	if v := f.allocationShape(t, householdID); v != "2026-11 2026-11-14 Rent 3100000.0000; 2026-11 2026-11-15 Rent 3200000.0000" {
		t.Errorf("next day: %q, want a second version", v)
	}
	if got.CommitmentsBudget != "3200000.0000" {
		t.Errorf("commitments = %s, want the newer version in force", got.CommitmentsBudget)
	}
	// The day before is read as it was, which is what the trajectory slice will see.
	f.clock.set(t, "2026-11-14T23:00:00+07:00")
	if got := f.get(ctx, t); got.CommitmentsBudget != "3100000.0000" {
		t.Errorf("on the 14th: commitments = %s, want 3100000.0000", got.CommitmentsBudget)
	}
}

// An inactive Category keeps its allocation and can still be changed (deactivating
// moves no figure, so freeing a retired Commitment is a PATCH to "0").
func TestAnInactiveCategoryKeepsItsAllocationAndCanBeFreed(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.mustPatchAllocs(ctx, t, "9000000", alloc(rent, "3000000"))
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, rent)

	if got := f.get(ctx, t); got.CommitmentsBudget != "3000000.0000" {
		t.Errorf("deactivated: commitments = %s, want the allocation kept", got.CommitmentsBudget)
	}
	got := f.mustPatchAllocs(ctx, t, "", alloc(rent, "0"))
	if got.CommitmentsBudget != "0.0000" || deref(got.DailyPool) != "9000000.0000" {
		t.Errorf("freed: commitments %s pool %s", got.CommitmentsBudget, deref(got.DailyPool))
	}
}

// Tenancy, in both directions: one Household's request changes none of the
// other's rows, and cannot name one of the other's Categories.
func TestAllocationWritesNeverCrossHouseholds(t *testing.T) {
	f := newFixture(t)
	mine, _, myCtx := f.member(t, "Home", "a@example.com")
	theirs, _, theirCtx := f.member(t, "Other", "b@example.com")
	myFood := f.category(t, mine, "Food", "daily")
	theirFood := f.category(t, theirs, "Food", "daily")
	f.mustPatchAllocs(theirCtx, t, "20000000", alloc(theirFood, "7"))
	theirBefore := f.allocationsRows(t, theirs) + f.versions(t, theirs) + f.markers(t, theirs)

	f.mustPatchAllocs(myCtx, t, "9000000", alloc(myFood, "1"))

	if now := f.allocationsRows(t, theirs) + f.versions(t, theirs) + f.markers(t, theirs); now != theirBefore {
		t.Errorf("the other Household's rows changed:\n%s\n%s", theirBefore, now)
	}
	if got := refusal(t, f.patchAllocs(myCtx, t, "", alloc(theirFood, "999"))); got != "category_id invalid" {
		t.Errorf("naming the other Household's Category: %q", got)
	}
	if now := f.allocationsRows(t, theirs) + f.versions(t, theirs) + f.markers(t, theirs); now != theirBefore {
		t.Errorf("the other Household's rows changed after a refused request:\n%s\n%s", theirBefore, now)
	}
	if got := f.get(theirCtx, t); listed(got) != theirFood.String()+"=7.0000" || deref(got.MonthlyBudget) != "20000000.0000" {
		t.Errorf("their Budget = %q monthly %s", listed(got), deref(got.MonthlyBudget))
	}
}

// Two writers of different fields both land (BOOTSTRAP §4): each Category's
// allocation and the Monthly Budget are separate upserts on separate keys, never
// a read-modify-write of a snapshot.
func TestConcurrentPatchesOfDifferentFieldsAllLand(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.get(ctx, t)
	const n = 8
	ids := make([]pgtype.UUID, n)
	for i := range ids {
		ids[i] = f.category(t, householdID, fmt.Sprintf("C%d", i), "daily")
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan string, n+1)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp, err := f.handlers.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{
				Allocations: &[]api.AllocationChange{alloc(ids[i], fmt.Sprint(i+1))},
			}})
			if _, ok := resp.(api.UpdateBudget200JSONResponse); err != nil || !ok {
				errs <- fmt.Sprintf("allocation %d: %#v %v", i, resp, err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		resp, err := f.handlers.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{MonthlyBudget: ptrTo("9000000")}})
		if _, ok := resp.(api.UpdateBudget200JSONResponse); err != nil || !ok {
			errs <- fmt.Sprintf("monthly budget: %#v %v", resp, err)
		}
	}()
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}

	got := f.get(ctx, t)
	if len(got.Allocations) != n || deref(got.MonthlyBudget) != "9000000.0000" {
		t.Fatalf("allocations %d (want %d), monthly %s: a concurrent write was lost", len(got.Allocations), n, deref(got.MonthlyBudget))
	}
	// 1+2+...+8 = 36, all Daily.
	if deref(got.Unallocated) != "8999964.0000" {
		t.Errorf("unallocated = %s, want 8999964.0000", deref(got.Unallocated))
	}
}

func ptrTo[T any](v T) *T { return &v }

// barrierBeginner makes the first allocation upsert of every transaction wait
// until `parties` transactions have done theirs (or the timeout, which keeps a
// waiting test from hanging when the requests are ordered and the second one is
// blocked behind the first). It turns "two requests touch the same two
// Categories in opposite orders" from a race into a certainty.
type barrierBeginner struct {
	budget.Beginner
	b *barrier
}

type barrier struct {
	mu      sync.Mutex
	parties int
	arrived int
	release chan struct{}
	timeout time.Duration
}

func newBarrier(parties int, timeout time.Duration) *barrier {
	return &barrier{parties: parties, release: make(chan struct{}), timeout: timeout}
}

func (b *barrier) wait() {
	b.mu.Lock()
	b.arrived++
	if b.arrived == b.parties {
		close(b.release)
	}
	b.mu.Unlock()
	select {
	case <-b.release:
	case <-time.After(b.timeout):
	}
}

func (bb barrierBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := bb.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &barrierTx{Tx: tx, b: bb.b}, nil
}

type barrierTx struct {
	pgx.Tx
	b    *barrier
	done bool
}

func (tx *barrierTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	if err == nil && !tx.done && strings.Contains(sql, "INSERT INTO allocations") && strings.Contains(sql, "DO UPDATE SET allocation") {
		tx.done = true
		tx.b.wait()
	}
	return tag, err
}

// Two requests that change the same two Categories, listed in opposite orders,
// must not deadlock: each upsert holds its row until the transaction ends, so
// written in the order sent, each request would hold the one the other wants
// next. The transaction writes in Category-id order whatever the order sent.
// Proven red against a loop that skips the sort: one of the two is aborted by
// Postgres's deadlock detector and answers 500.
func TestTwoRequestsListingTheSameCategoriesInOppositeOrdersDoNotDeadlock(t *testing.T) {
	bar := newBarrier(2, 1500*time.Millisecond)
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { return barrierBeginner{Beginner: b, b: bar} })
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	x := f.category(t, householdID, "X", "daily")
	y := f.category(t, householdID, "Y", "daily")
	f.get(ctx, t)

	var wg sync.WaitGroup
	results := make([]api.UpdateBudgetResponseObject, 2)
	orders := [][]api.AllocationChange{
		{alloc(x, "1"), alloc(y, "2")},
		{alloc(y, "3"), alloc(x, "4")},
	}
	for i := range orders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items := orders[i]
			results[i], _ = f.handlers.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{Allocations: &items}})
		}()
	}
	wg.Wait()

	for i, r := range results {
		if _, ok := r.(api.UpdateBudget200JSONResponse); !ok {
			t.Errorf("request %d = %#v, want 200: a deadlock aborted it", i, r)
		}
	}
}

// A write that fails part-way rolls everything back, and the log carries no
// amount and no Category. (A failure of the read that answers, after the commit,
// is the next test's.)
func TestAFailedAllocationWriteChangesNothingAndLeaksNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		brk  breakingBeginner
	}{
		{"the allocation write", breakingBeginner{failOn: "DO UPDATE SET allocation"}},
		{"commit", breakingBeginner{failCommit: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brk := tc.brk
			f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { brk.Beginner = b; return brk })
			householdID, _, ctx := f.member(t, "Home", "a@example.com")
			food := f.category(t, householdID, "Groceries", "daily")
			logs := capturedLog(t)
			before := f.everything(t)

			resp := f.patchAllocs(ctx, t, "9000000", alloc(food, "123456"))

			if got, ok := resp.(api.UpdateBudget500JSONResponse); !ok || got.Code != api.INTERNAL {
				t.Errorf("UpdateBudget = %#v, want 500 INTERNAL", resp)
			}
			if after := f.everything(t); after != before {
				t.Errorf("a failed request left something behind:\n%s\n%s", before, after)
			}
			for _, secret := range []string{"123456", "9000000", "Groceries", "Home", "a@example.com"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("the log leaks %q:\n%s", secret, logs.String())
				}
			}
		})
	}
}

// A failure of the checking read (before anything is written) is a 500 too.
func TestAFailureToListTheCategoriesIs500(t *testing.T) {
	f := newFixtureWith(t, func(q db.Querier) db.Querier { return failingListQuerier{q} }, nil)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	food := f.category(t, householdID, "Food", "daily")
	capturedLog(t)

	if got, ok := f.patchAllocs(ctx, t, "", alloc(food, "1")).(api.UpdateBudget500JSONResponse); !ok || got.Code != api.INTERNAL {
		t.Errorf("UpdateBudget = %#v, want 500 INTERNAL", got)
	}
}

type failingListQuerier struct{ db.Querier }

func (failingListQuerier) ListCategories(context.Context, pgtype.UUID) ([]db.Category, error) {
	return nil, errBoom
}

// ---- GET /budget reads one snapshot ----------------------------------------

// tearHook runs fn once, just before the allocations are read: the moment
// between GET /budget's two statements. Until fn is set it does nothing, so a
// test can arrange its state first.
type tearHook struct {
	once sync.Once
	fn   func()
}

func (h *tearHook) fire() {
	if h.fn != nil {
		h.once.Do(h.fn)
	}
}

// tearingQuerier is the same moment for a read that goes through the Querier.
type tearingQuerier struct {
	db.Querier
	hook *tearHook
}

func (q tearingQuerier) AllocationsAsOf(ctx context.Context, arg db.AllocationsAsOfParams) ([]db.AllocationsAsOfRow, error) {
	q.hook.fire()
	return q.Querier.AllocationsAsOf(ctx, arg)
}

type tearingBeginner struct {
	budget.Beginner
	hook *tearHook
}

func (b tearingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return tearingTx{Tx: tx, hook: b.hook}, nil
}

func (b tearingBeginner) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return tearingTx{Tx: tx, hook: b.hook}, nil
}

type tearingTx struct {
	pgx.Tx
	hook *tearHook
}

func (tx tearingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "c.bucket, a.allocation") {
		tx.hook.fire()
	}
	return tx.Tx.Query(ctx, sql, args...)
}

// A GET /budget is built from two statements, and an edit that commits between
// them must not be half in the answer: the old Monthly Budget beside the new
// Commitment is a Budget that never existed. The edit here changes both in one
// transaction, between the statements; the answer must be wholly before or
// wholly after it. Proven red against a read that is not one snapshot (READ
// COMMITTED, the transaction's default): it answered a Monthly Budget of
// 9000000 with a Commitments Budget of 4000000.
func TestAConcurrentEditCannotTearAGetBudget(t *testing.T) {
	hook := &tearHook{}
	f := newFixtureWith(t,
		func(q db.Querier) db.Querier { return tearingQuerier{Querier: q, hook: hook} },
		func(b budget.Beginner) budget.Beginner { return tearingBeginner{Beginner: b, hook: hook} })
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.mustPatchAllocs(ctx, t, "9000000", alloc(rent, "3000000"))

	// The editor is a separate handler on the plain pool, so its own writes are
	// not caught by the hook.
	editor := budget.New(budget.Deps{Querier: f.tdb.Queries, Beginner: f.tdb.Pool, Now: f.clock.now})
	hook.fn = func() {
		monthly := "12000000"
		resp, err := editor.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{
			MonthlyBudget: &monthly,
			Allocations:   &[]api.AllocationChange{alloc(rent, "4000000")},
		}})
		if _, ok := resp.(api.UpdateBudget200JSONResponse); err != nil || !ok {
			t.Errorf("the concurrent edit = %#v %v", resp, err)
		}
	}

	got := f.get(ctx, t)

	before := [3]string{"9000000.0000", "3000000.0000", "6000000.0000"}
	after := [3]string{"12000000.0000", "4000000.0000", "8000000.0000"}
	have := [3]string{deref(got.MonthlyBudget), got.CommitmentsBudget, deref(got.DailyPool)}
	if have != before && have != after {
		t.Errorf("torn: monthly %s, commitments %s, pool %s; want %v (before the edit) or %v (after)", have[0], have[1], have[2], before, after)
	}
	// The edit did land, so the next read is after it.
	if next := f.get(ctx, t); next.CommitmentsBudget != "4000000.0000" || deref(next.MonthlyBudget) != "12000000.0000" {
		t.Errorf("the next read: monthly %s commitments %s", deref(next.MonthlyBudget), next.CommitmentsBudget)
	}
}

// The answer is read after the write commits (the maintainer's ruling): a read
// that fails is a 500, the change stands, and repeating the request is safe.
func TestAFailedAnsweringReadIs500ButTheChangeStandsAndARepeatIsSafe(t *testing.T) {
	for _, failOn := range []string{"ORDER BY effective_on", "c.bucket, a.allocation"} {
		t.Run(failOn, func(t *testing.T) {
			brk := breakingBeginner{failOn: failOn}
			f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { brk.Beginner = b; return brk })
			householdID, _, ctx := f.member(t, "Home", "a@example.com")
			food := f.category(t, householdID, "Groceries", "daily")
			logs := capturedLog(t)

			resp := f.patchAllocs(ctx, t, "9000000", alloc(food, "123456"))

			if got, ok := resp.(api.UpdateBudget500JSONResponse); !ok || got.Code != api.INTERNAL {
				t.Fatalf("UpdateBudget = %#v, want 500 INTERNAL", resp)
			}
			if v := f.shape(t, householdID); v != "2026-11 2026-11-14 9000000.0000" {
				t.Errorf("monthly_budgets = %q, want the change saved", v)
			}
			if v := f.allocationShape(t, householdID); v != "2026-11 2026-11-14 Groceries 123456.0000" {
				t.Errorf("allocations = %q, want the change saved", v)
			}
			for _, secret := range []string{"123456", "9000000", "Groceries", "Home", "a@example.com"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("the log leaks %q:\n%s", secret, logs.String())
				}
			}

			// Repeating it, once the read works, answers the same Budget and
			// leaves the rows as they were.
			rows := f.allocationShape(t, householdID) + "|" + f.shape(t, householdID)
			healthy := budget.New(budget.Deps{Querier: f.tdb.Queries, Beginner: f.tdb.Pool, Now: f.clock.now})
			resp2, err := healthy.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{
				MonthlyBudget: ptrTo("9000000"),
				Allocations:   &[]api.AllocationChange{alloc(food, "123456")},
			}})
			got200, is := resp2.(api.UpdateBudget200JSONResponse)
			if err != nil || !is {
				t.Fatalf("the repeat = %#v %v, want 200", resp2, err)
			}
			got := api.Budget(got200)
			if deref(got.MonthlyBudget) != "9000000.0000" || listed(got) != food.String()+"=123456.0000" {
				t.Errorf("the repeat answered %q monthly %s", listed(got), deref(got.MonthlyBudget))
			}
			if now := f.allocationShape(t, householdID) + "|" + f.shape(t, householdID); now != rows {
				t.Errorf("a repeat changed the rows:\n%s\n%s", rows, now)
			}
		})
	}
}

// The answer to a PATCH can no more be torn than a GET's: an edit that commits
// between the answer's two statements (here, one that changes the Monthly
// Budget and an allocation together) is in the answer whole or not at all.
// Proven red against an answer read inside the writing transaction at READ
// COMMITTED, where it came back with the Monthly Budget of before the edit
// beside the Commitment of after it.
func TestAConcurrentEditCannotTearAPatchBudgetAnswer(t *testing.T) {
	hook := &tearHook{}
	f := newFixtureWith(t,
		func(q db.Querier) db.Querier { return tearingQuerier{Querier: q, hook: hook} },
		func(b budget.Beginner) budget.Beginner { return tearingBeginner{Beginner: b, hook: hook} })
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.mustPatchAllocs(ctx, t, "9000000", alloc(rent, "3000000"))

	editor := budget.New(budget.Deps{Querier: f.tdb.Queries, Beginner: f.tdb.Pool, Now: f.clock.now})
	hook.fn = func() {
		monthly := "12000000"
		resp, err := editor.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{
			MonthlyBudget: &monthly,
			Allocations:   &[]api.AllocationChange{alloc(rent, "4000000")},
		}})
		if _, ok := resp.(api.UpdateBudget200JSONResponse); err != nil || !ok {
			t.Errorf("the concurrent edit = %#v %v", resp, err)
		}
	}

	// The request under test changes something else, so its own write does not
	// decide the values the edit changes.
	food := f.category(t, householdID, "Food", "daily")
	got := f.mustPatchAllocs(ctx, t, "", alloc(food, "1"))

	before := [3]string{"9000000.0000", "3000000.0000", "6000000.0000"}
	after := [3]string{"12000000.0000", "4000000.0000", "8000000.0000"}
	have := [3]string{deref(got.MonthlyBudget), got.CommitmentsBudget, deref(got.DailyPool)}
	if have != before && have != after {
		t.Errorf("torn: monthly %s, commitments %s, pool %s; want %v (before the edit) or %v (after)", have[0], have[1], have[2], before, after)
	}
}
