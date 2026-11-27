package dashboard_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// D7 through the real statements, on the 14th of November 2026 (the fixture's
// clock). The rows are written as the lists answer them.

func (f fixture) monthlyBudget(t *testing.T, householdID pgtype.UUID, effectiveOn, amount string) {
	t.Helper()
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11') ON CONFLICT DO NOTHING`, householdID)
	f.exec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-11', $2::date, $3::numeric)`, householdID, effectiveOn, amount)
}

func (f fixture) allocate(t *testing.T, householdID, category pgtype.UUID, effectiveOn, amount string) {
	t.Helper()
	f.exec(t, `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, '2026-11', $2::date, $3, $4::numeric)`,
		householdID, effectiveOn, category, amount)
}

// categoryAs and pocketAs insert a row under an id the test chose, so a tie is
// broken by an id whose order is not the order of creation.
func (f fixture) categoryAs(t *testing.T, householdID pgtype.UUID, id, name, bucket string) pgtype.UUID {
	t.Helper()
	f.exec(t, `INSERT INTO categories (id, household_id, category_display_name, bucket) VALUES ($1::uuid, $2, $3, $4)`, id, householdID, name, bucket)
	return mustUUID(t, id)
}

func (f fixture) pocketAs(t *testing.T, householdID pgtype.UUID, id, name string) pgtype.UUID {
	t.Helper()
	f.exec(t, `INSERT INTO pockets (id, household_id, pocket_display_name) VALUES ($1::uuid, $2, $3)`, id, householdID, name)
	return mustUUID(t, id)
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatal(err)
	}
	return u
}

// rows is a list as a test reads it: "Name spent[/allocation][!]" per row, in
// the answer's order, "!" marking an inactive row. The uncategorised row is
// "(none)".
func categoryRows(d api.Dashboard) string {
	if d.Categories == nil {
		return "<absent>"
	}
	var out []string
	for _, c := range *d.Categories {
		name, flag := "(none)", ""
		if c.CategoryDisplayName != nil {
			name = *c.CategoryDisplayName
			if !*c.Active {
				flag = "!"
			}
			if c.CategoryId == nil {
				name += "?noid"
			}
		} else if c.CategoryId != nil || c.Active != nil {
			name += "?partial"
		}
		row := name + " " + c.Spent
		if c.Allocation != nil {
			row += "/" + *c.Allocation
		}
		out = append(out, row+flag)
	}
	return strings.Join(out, " | ")
}

func pocketRows(d api.Dashboard) string {
	if d.Pockets == nil {
		return "<absent>"
	}
	var out []string
	for _, p := range *d.Pockets {
		flag := ""
		if !p.Active {
			flag = "!"
		}
		out = append(out, p.PocketDisplayName+" "+p.Spent+flag)
	}
	return strings.Join(out, " | ")
}

func TestTheBreakdownsAreAbsentWhileThereIsNoMonthlyBudgetWhateverWasSpent(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	f.expense(t, hh, wallet, pgtype.UUID{}, "2026-11-05", "10000")

	got := f.get(ctx, t)

	if got.Trajectory != nil || got.Categories != nil || got.Pockets != nil {
		t.Errorf("answer = %+v, want the currency alone: no Monthly Budget, nothing to read the spending against", got)
	}
}

func TestWithAMonthlyBudgetAndNothingSpentNorAllocatedNeitherListIsOnTheWire(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	f.tdb.CreatePocket(t, hh, "Wallet")
	f.tdb.CreateCategory(t, hh, "Food", "daily") // a Category with neither spend nor allocation has no row
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")

	got := f.get(ctx, t)

	if got.Trajectory == nil {
		t.Fatal("no trajectory, want one: the Household has a Monthly Budget")
	}
	if got.Categories != nil || got.Pockets != nil {
		t.Errorf("categories %s, pockets %s: omitted when they have no row, never an empty list", categoryRows(got), pocketRows(got))
	}
}

// Everything D7 counts and leaves out, in one Household, with another Household
// beside it that has the same names and bigger numbers.
func TestTheBreakdownsAddUpWhatCountsAndLeaveEverythingElseOut(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Mine", "a@example.com")
	other, _ := f.member(t, "Theirs", "b@example.com")

	wallet, card := f.tdb.CreatePocket(t, hh, "Wallet"), f.tdb.CreatePocket(t, hh, "Card")
	oldCard := f.tdb.CreatePocket(t, hh, "Old card")
	f.tdb.CreatePocket(t, hh, "Unused") // no spend: no row
	food := f.tdb.CreateCategory(t, hh, "Food", "daily")
	snacks := f.tdb.CreateCategory(t, hh, "Snacks", "daily")
	gym := f.tdb.CreateCategory(t, hh, "Gym", "daily")
	rent := f.tdb.CreateCategory(t, hh, "Rent", "commitment")
	gone := f.tdb.CreateCategory(t, hh, "Gone", "daily")
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, snacks)
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, oldCard)

	f.monthlyBudget(t, hh, "2026-11-01", "6000000")
	f.allocate(t, hh, food, "2026-11-01", "1000000")
	f.allocate(t, hh, food, "2026-11-10", "1500000") // a mid-Period change: this is in force Today
	f.allocate(t, hh, food, "2026-11-20", "9000000") // after Today: not in force
	f.allocate(t, hh, gym, "2026-11-12", "200000")   // an allocation and no spending
	f.allocate(t, hh, rent, "2026-11-01", "2000000") // a Commitment is never in categories
	f.allocate(t, hh, gone, "2026-11-01", "777000")
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, gone) // deleted: its allocation is in no row

	f.expense(t, hh, wallet, food, "2026-11-13", "30000")
	f.expense(t, hh, wallet, food, "2026-11-14", "15000")  // Today counts
	f.expense(t, hh, card, food, "2026-11-06", "-5000")    // a refund subtracts
	f.expense(t, hh, wallet, food, "2026-11-15", "50000")  // future-dated: in neither list
	f.expense(t, hh, wallet, food, "2026-11-20", "-80000") // a future refund lowers nothing
	f.expense(t, hh, wallet, food, "2026-10-31", "7000")   // before the Period
	f.expense(t, hh, wallet, food, "2026-12-01", "7000")   // after it
	f.expense(t, hh, oldCard, snacks, "2026-11-09", "4000")
	f.expense(t, hh, wallet, pgtype.UUID{}, "2026-11-05", "20000") // uncategorised
	f.expense(t, hh, card, pgtype.UUID{}, "2026-11-05", "5000")
	f.expense(t, hh, card, rent, "2026-11-07", "3000000")                                   // a Commitment: a Pocket's spend, no Category's here
	f.tdb.CreateExpense(t, hh, wallet, testutil.ExpenseOpts{Category: food, Deleted: true}) // 25000 on 2026-11-23: future today, and deleted

	// Another Household with the same names, an allocation and bigger numbers.
	oWallet := f.tdb.CreatePocket(t, other, "Wallet")
	oFood := f.tdb.CreateCategory(t, other, "Food", "daily")
	f.monthlyBudget(t, other, "2026-11-01", "60000000")
	f.allocate(t, other, oFood, "2026-11-01", "99999999")
	f.expense(t, other, oWallet, oFood, "2026-11-13", "99999")
	f.expense(t, other, oWallet, pgtype.UUID{}, "2026-11-13", "99999")

	got := f.get(ctx, t)

	// Food: 30000 + 15000 - 5000 = 40000, of 1.500.000 (the change of the 10th, not the one of the 20th).
	// Uncategorised: 25000. Snacks: 4000, inactive. Gym: nothing spent, 200.000 allocated.
	wantCategories := "Food 40000.0000/1500000.0000 | (none) 25000.0000 | Snacks 4000.0000! | Gym 0.0000/200000.0000"
	if g := categoryRows(got); g != wantCategories {
		t.Errorf("categories = %s\nwant       %s", g, wantCategories)
	}
	// Card: -5000 + 5000 + 3000000. Wallet: 30000 + 15000 + 20000 (the 'deleted' Expense is none of it).
	// Old card: 4000, inactive. A Pocket with no spend has no row.
	wantPockets := "Card 3000000.0000 | Wallet 65000.0000 | Old card 4000.0000!"
	if g := pocketRows(got); g != wantPockets {
		t.Errorf("pockets = %s\nwant    %s", g, wantPockets)
	}
	// The two lists are the same Expenses, read once: every counted Expense is
	// in one Pocket, and in one Category row unless it is a Commitment's (Rent).
	var categories, pockets decimal.Decimal
	for _, c := range *got.Categories {
		categories = categories.Add(decimal.RequireFromString(c.Spent))
	}
	for _, p := range *got.Pockets {
		pockets = pockets.Add(decimal.RequireFromString(p.Spent))
	}
	if commitment := decimal.RequireFromString("3000000"); !categories.Add(commitment).Equal(pockets) {
		t.Errorf("the lists do not reconcile: categories %s + Commitments %s != pockets %s", categories, commitment, pockets)
	}
}

// Equal spend is ordered by id, ascending, the same on every read and whatever
// order the rows were written or came back in; the uncategorised row, which has
// no id, follows the rows it ties with.
func TestEqualSpendIsOrderedByIdAscendingAndTheUncategorisedRowFollowsItsTies(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	// Created out of id order on purpose.
	p3 := f.pocketAs(t, hh, "01900000-0000-7000-8000-000000000003", "Third")
	p1 := f.pocketAs(t, hh, "01900000-0000-7000-8000-000000000001", "First")
	p2 := f.pocketAs(t, hh, "01900000-0000-7000-8000-000000000002", "Second")
	c3 := f.categoryAs(t, hh, "01900000-0000-7000-8000-0000000000c3", "C three", "daily")
	c1 := f.categoryAs(t, hh, "01900000-0000-7000-8000-0000000000c1", "C one", "daily")
	c2 := f.categoryAs(t, hh, "01900000-0000-7000-8000-0000000000c2", "C two", "daily")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")
	for _, p := range []pgtype.UUID{p3, p1, p2} {
		f.expense(t, hh, p, pgtype.UUID{}, "2026-11-05", "100")
	}
	for _, c := range []pgtype.UUID{c3, c1, c2} {
		f.expense(t, hh, p1, c, "2026-11-06", "100")
	}
	f.expense(t, hh, p1, pgtype.UUID{}, "2026-11-06", "50")

	for run := 0; run < 5; run++ {
		got := f.get(ctx, t)
		if g, want := pocketRows(got), "First 450.0000 | Second 100.0000 | Third 100.0000"; g != want {
			t.Fatalf("pockets = %s, want %s", g, want)
		}
		// The uncategorised 350 (three Pockets' 100 and the 50) leads; the three Categories tie at 100 and
		// follow by id, though they were written in another order.
		if g, want := categoryRows(got), "(none) 350.0000 | C one 100.0000 | C two 100.0000 | C three 100.0000"; g != want {
			t.Fatalf("categories = %s, want %s", g, want)
		}
	}
}

// The uncategorised row has no id to break a tie with, so it follows the rows
// it ties with.
func TestTheUncategorisedRowFollowsTheCategoriesItTiesWith(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	c1 := f.categoryAs(t, hh, "01900000-0000-7000-8000-0000000000c1", "C one", "daily")
	c2 := f.categoryAs(t, hh, "01900000-0000-7000-8000-0000000000c2", "C two", "daily")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")
	f.expense(t, hh, wallet, c2, "2026-11-06", "100")
	f.expense(t, hh, wallet, pgtype.UUID{}, "2026-11-06", "100")
	f.expense(t, hh, wallet, c1, "2026-11-06", "100")

	if g, want := categoryRows(f.get(ctx, t)), "C one 100.0000 | C two 100.0000 | (none) 100.0000"; g != want {
		t.Errorf("categories = %s, want %s", g, want)
	}
}

// Today decides what counts: the same rows, read a day earlier, leave the
// Expense of the 14th out of both lists, and the allocation that took effect on
// the 14th is not yet in force.
func TestTheBreakdownsFollowTodayAcrossTheFourAmBoundary(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	food := f.tdb.CreateCategory(t, hh, "Food", "daily")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")
	f.allocate(t, hh, food, "2026-11-01", "100000")
	f.allocate(t, hh, food, "2026-11-14", "200000")
	f.expense(t, hh, wallet, food, "2026-11-13", "10")
	f.expense(t, hh, wallet, food, "2026-11-14", "5")

	f.clock.set(t, "2026-11-14T04:00:00+07:00")
	if g, want := categoryRows(f.get(ctx, t)), "Food 15.0000/200000.0000"; g != want {
		t.Errorf("at 04:00 on the 14th: categories = %s, want %s", g, want)
	}
	f.clock.set(t, "2026-11-14T03:59:59+07:00")
	if g, want := categoryRows(f.get(ctx, t)), "Food 10.0000/100000.0000"; g != want {
		t.Errorf("a second earlier, still the 13th: categories = %s, want %s", g, want)
	}
}

// One Household's allocation is never read as another's: were AllocationsAsOf
// to lose its household_id, the answer would name a Category that is not this
// Household's live one, which the fold refuses as a 500 rather than show. The
// Category list's own tenancy is its statement's tests': the fold only looks
// up ids AllocationsAsOf returned, so that list cannot surface another row.
func TestAnotherHouseholdsCategoriesAndAllocationsAreInNoRowOfThisOne(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Mine", "a@example.com")
	other, _ := f.member(t, "Theirs", "b@example.com")
	f.tdb.CreatePocket(t, hh, "Wallet")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")
	f.monthlyBudget(t, other, "2026-11-01", "3000000")
	theirs := f.tdb.CreateCategory(t, other, "Theirs only", "daily")
	f.allocate(t, other, theirs, "2026-11-01", "123456")

	got := f.get(ctx, t)

	if got.Categories != nil || got.Pockets != nil {
		t.Errorf("categories %s, pockets %s: another Household's Category and allocation leaked in", categoryRows(got), pocketRows(got))
	}
}

// ---- the snapshot ----------------------------------------------------------

// A Category renamed and deactivated after the answer's allocations were read
// and before its Category list is is in the answer as it was, whole: the list
// of names is read in the snapshot, not after it. Proven red with the snapshot
// at READ COMMITTED: the allocation-only row came back under the new name,
// active, beside an allocation read before the write.
func TestACategoryRenamedBetweenTheStatementsIsInTheBreakdownAsItWas(t *testing.T) {
	hook := &hookBeginner{beforeSQL: "name: ListCategories"}
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { hook.Beginner = b; return hook })
	hh, ctx := f.member(t, "Home", "a@example.com")
	f.tdb.CreatePocket(t, hh, "Wallet")
	gym := f.tdb.CreateCategory(t, hh, "Gym", "daily")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")
	f.allocate(t, hh, gym, "2026-11-01", "200000")

	before := categoryRows(f.get(ctx, t))
	if before != "Gym 0.0000/200000.0000" {
		t.Fatalf("categories = %s, want the allocation-only row under its name", before)
	}
	hook.fn = func() {
		f.exec(t, `UPDATE categories SET category_display_name = 'Fitness', active = false WHERE id = $1`, gym)
	}
	during := categoryRows(f.get(ctx, t))
	if !hook.done {
		t.Fatal("the write never ran before the Category list was read")
	}
	if during != before {
		t.Errorf("categories during the write = %s, want the answer as it stood before it: %s", during, before)
	}
	if after := categoryRows(f.get(ctx, t)); after != "Fitness 0.0000/200000.0000!" {
		t.Errorf("categories after = %s, want the rename seen on the next read (the test proves nothing otherwise)", after)
	}
}

// A write between the spend and everything before it lands in the lists whole
// or not at all, as the hero does: the Expense it adds is in neither list.
func TestAnExpenseCommittedBetweenTheStatementsIsInNeitherListOfTheAnswer(t *testing.T) {
	hook := &hookBeginner{beforeSQL: "name: PeriodSpend"}
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { hook.Beginner = b; return hook })
	hh, ctx := f.member(t, "Home", "a@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	food := f.tdb.CreateCategory(t, hh, "Food", "daily")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")
	f.expense(t, hh, wallet, food, "2026-11-05", "1000")

	before := f.answerOf(ctx, t)
	hook.fn = func() { f.expense(t, hh, wallet, food, "2026-11-06", "500") }
	during := f.answerOf(ctx, t)
	if !hook.done {
		t.Fatal("the write never ran before the spend was read")
	}
	if during != before {
		t.Errorf("the answer saw the Expense the snapshot began before:\n before %s\n during %s", before, during)
	}
	after := f.get(ctx, t)
	if g := fmt.Sprint(pocketRows(after), " ", categoryRows(after)); !strings.Contains(g, "1500.0000") {
		t.Errorf("after the write: %s, want 1500.0000 (the test proves nothing otherwise)", g)
	}
}
