package dashboard_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
)

// D6 through the real statements, on the 14th of November 2026 (the fixture's
// clock): "Name allocation/paid/still due" per row, "!" for an inactive one.

func commitmentRows(d api.Dashboard) string {
	if d.Commitments == nil {
		return "<absent>"
	}
	var out []string
	for _, c := range d.Commitments.Items {
		flag := ""
		if !c.Active {
			flag = "!"
		}
		out = append(out, c.CategoryDisplayName+" "+c.Allocation+"/"+c.Paid+"/"+c.StillDue+flag)
	}
	return strings.Join(out, " | ") + " ; " + d.Commitments.CommitmentsBudget + "/" + d.Commitments.Paid + "/" + d.Commitments.StillDue
}

func TestCommitmentsAreAbsentWhileThereIsNoMonthlyBudgetOrNoCommitment(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	rent := f.tdb.CreateCategory(t, hh, "Rent", "commitment")
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`, hh) // a Period with allocations and no Monthly Budget
	f.allocate(t, hh, rent, "2026-11-01", "2000000")
	f.expense(t, hh, wallet, rent, "2026-11-05", "2000000")
	if got := f.get(ctx, t); got.Commitments != nil {
		t.Errorf("commitments = %s, want absent: no Monthly Budget, nothing to read against", commitmentRows(got))
	}

	other, otherCtx := f.member(t, "Other", "b@example.com")
	f.monthlyBudget(t, other, "2026-11-01", "3000000")
	f.tdb.CreateCategory(t, other, "Food", "daily")
	got := f.get(otherCtx, t)
	if got.Trajectory == nil || got.Commitments != nil {
		t.Errorf("answer = %+v, want a trajectory and no commitments: the Household has no Commitment Category", got)
	}
}

// Everything D6 counts and leaves out, in one Household, with another beside it
// that has the same names and bigger numbers.
func TestCommitmentsAddUpWhatCountsAndLeaveEverythingElseOut(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Mine", "a@example.com")
	other, _ := f.member(t, "Theirs", "b@example.com")

	wallet, card := f.tdb.CreatePocket(t, hh, "Wallet"), f.tdb.CreatePocket(t, hh, "Card")
	rent := f.tdb.CreateCategory(t, hh, "Rent", "commitment")
	tax := f.tdb.CreateCategory(t, hh, "Tax", "commitment")
	school := f.tdb.CreateCategory(t, hh, "School", "commitment")
	gym := f.tdb.CreateCategory(t, hh, "Gym", "commitment")
	food := f.tdb.CreateCategory(t, hh, "Food", "daily")
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, gym)
	gone := f.tdb.CreateCategory(t, hh, "Gone", "commitment")
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, gone)

	f.monthlyBudget(t, hh, "2026-11-01", "9000000")
	f.allocate(t, hh, rent, "2026-11-01", "2000000")
	f.allocate(t, hh, rent, "2026-11-10", "2500000") // in force Today
	f.allocate(t, hh, rent, "2026-11-20", "9000000") // after Today: not in force
	f.allocate(t, hh, tax, "2026-11-01", "1000000")
	f.allocate(t, hh, school, "2026-11-01", "0") // an allocation of zero
	f.allocate(t, hh, gym, "2026-11-01", "300000")
	f.allocate(t, hh, food, "2026-11-01", "777000") // a Daily Category is in no row
	f.allocate(t, hh, gone, "2026-11-01", "555000") // deleted: no row, no part of the budget

	f.expense(t, hh, wallet, rent, "2026-11-07", "2000000")
	f.expense(t, hh, card, rent, "2026-11-14", "300000")   // Today counts
	f.expense(t, hh, wallet, rent, "2026-11-15", "400000") // future-dated: not paid until its day
	f.expense(t, hh, wallet, rent, "2026-11-25", "-90000") // a future refund lowers nothing
	f.expense(t, hh, wallet, rent, "2026-10-31", "111000") // before the Period
	f.expense(t, hh, wallet, rent, "2026-12-01", "111000") // after it
	f.expense(t, hh, wallet, tax, "2026-11-03", "1200000") // more than set aside
	f.expense(t, hh, wallet, school, "2026-11-04", "-50000")
	f.expense(t, hh, wallet, food, "2026-11-05", "999000")          // Daily: in no Commitment row
	f.expense(t, hh, wallet, pgtype.UUID{}, "2026-11-05", "888000") // uncategorised is Daily
	f.exec(t, `INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id, category_id, deleted_at)
		VALUES (gen_random_uuid(), $1, '2026-11-06', 55000, 'IDR', $2, $3, now())`, hh, wallet, rent) // deleted: not paid

	oWallet := f.tdb.CreatePocket(t, other, "Wallet")
	oRent := f.tdb.CreateCategory(t, other, "Rent", "commitment")
	f.monthlyBudget(t, other, "2026-11-01", "60000000")
	f.allocate(t, other, oRent, "2026-11-01", "99999999")
	f.expense(t, other, oWallet, oRent, "2026-11-13", "99999")

	got := f.get(ctx, t)

	// Rent: 2.500.000 set, 2.300.000 paid. Tax: 1.000.000 set, 1.200.000 paid, 200.000 more than set aside.
	// School: 0 set, -50.000 paid (a refund). Gym: 300.000 set, nothing paid, inactive.
	want := "Rent 2500000.0000/2300000.0000/200000.0000 | Tax 1000000.0000/1200000.0000/-200000.0000 | School 0.0000/-50000.0000/50000.0000 | Gym 300000.0000/0.0000/300000.0000!" +
		" ; 3800000.0000/3450000.0000/350000.0000"
	if g := commitmentRows(got); g != want {
		t.Errorf("commitments = %s\nwant        %s", g, want)
	}
}

// The panel and the Daily Pool read one set of allocations: the Commitments
// Budget is exactly what the pool was made of, and a Commitment Expense is in no
// daily figure whatever it is paid at.
func TestTheCommitmentsBudgetIsWhatTheDailyPoolWasMadeOf(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	rent := f.tdb.CreateCategory(t, hh, "Rent", "commitment")
	f.monthlyBudget(t, hh, "2026-11-01", "6000000")
	f.allocate(t, hh, rent, "2026-11-01", "2000000")
	f.allocate(t, hh, rent, "2026-11-12", "2500000")

	before := f.get(ctx, t)
	f.expense(t, hh, wallet, rent, "2026-11-13", "2500000")
	after := f.get(ctx, t)

	// Pool: 6.000.000 - 2.500.000 = 3.500.000, over 17 days with nothing settled.
	if after.Commitments == nil || after.Commitments.CommitmentsBudget != "2500000.0000" || after.Commitments.StillDue != "0.0000" {
		t.Fatalf("commitments = %s, want 2.500.000 set and nothing still due", commitmentRows(after))
	}
	b, _ := json.Marshal(before.Trajectory)
	a, _ := json.Marshal(after.Trajectory)
	if after.Trajectory.RemainingDailyPool != "3500000.0000" || string(a) != string(b) {
		t.Errorf("trajectory moved when a Commitment was paid: before %s after %s", b, a)
	}
}

func TestCommitmentsFollowTodayAcrossTheFourAmBoundary(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Home", "a@example.com")
	wallet := f.tdb.CreatePocket(t, hh, "Wallet")
	rent := f.tdb.CreateCategory(t, hh, "Rent", "commitment")
	f.monthlyBudget(t, hh, "2026-11-01", "6000000")
	f.allocate(t, hh, rent, "2026-11-01", "1000000")
	f.allocate(t, hh, rent, "2026-11-14", "2000000")
	f.expense(t, hh, wallet, rent, "2026-11-13", "10")
	f.expense(t, hh, wallet, rent, "2026-11-14", "5")

	f.clock.set(t, "2026-11-14T04:00:00+07:00")
	if g, want := commitmentRows(f.get(ctx, t)), "Rent 2000000.0000/15.0000/1999985.0000 ; 2000000.0000/15.0000/1999985.0000"; g != want {
		t.Errorf("at 04:00 on the 14th: %s, want %s", g, want)
	}
	f.clock.set(t, "2026-11-14T03:59:59+07:00")
	if g, want := commitmentRows(f.get(ctx, t)), "Rent 1000000.0000/10.0000/999990.0000 ; 1000000.0000/10.0000/999990.0000"; g != want {
		t.Errorf("a second earlier, still the 13th: %s, want %s", g, want)
	}
}

// One Household's Commitments are never read as another's: were the Category
// list or AllocationsAsOf to lose its household_id, the other Household's
// Commitment would be a row of this one.
func TestAnotherHouseholdsCommitmentsAreInNoRowOfThisOne(t *testing.T) {
	f := newFixture(t)
	hh, ctx := f.member(t, "Mine", "a@example.com")
	other, _ := f.member(t, "Theirs", "b@example.com")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")
	f.monthlyBudget(t, other, "2026-11-01", "3000000")
	theirs := f.tdb.CreateCategory(t, other, "Theirs only", "commitment")
	f.allocate(t, other, theirs, "2026-11-01", "123456")
	oWallet := f.tdb.CreatePocket(t, other, "Wallet")
	f.expense(t, other, oWallet, theirs, "2026-11-05", "123456")

	if got := f.get(ctx, t); got.Commitments != nil {
		t.Errorf("commitments = %s: another Household's Commitment leaked in", commitmentRows(got))
	}
}

// A Commitment Category's name and Inactive flag are read in the snapshot, with
// the allocations the figures were made of: a rename between the statements is
// in the answer as it was, whole, as the breakdowns' are.
func TestACommitmentRenamedBetweenTheStatementsIsInThePanelAsItWas(t *testing.T) {
	hook := &hookBeginner{beforeSQL: "name: ListCategories"}
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { hook.Beginner = b; return hook })
	hh, ctx := f.member(t, "Home", "a@example.com")
	f.tdb.CreatePocket(t, hh, "Wallet")
	rent := f.tdb.CreateCategory(t, hh, "Rent", "commitment")
	f.monthlyBudget(t, hh, "2026-11-01", "3000000")
	f.allocate(t, hh, rent, "2026-11-01", "1000000")

	before := commitmentRows(f.get(ctx, t))
	hook.fn = func() {
		f.exec(t, `UPDATE categories SET category_display_name = 'Housing', active = false WHERE id = $1`, rent)
	}
	during := commitmentRows(f.get(ctx, t))
	if !hook.done {
		t.Fatal("the write never ran before the Category list was read")
	}
	if during != before {
		t.Errorf("commitments during the write = %s, want the answer as it stood before it: %s", during, before)
	}
	if after := commitmentRows(f.get(ctx, t)); !strings.HasPrefix(after, "Housing ") || !strings.Contains(after, "!") {
		t.Errorf("commitments after = %s, want the rename seen on the next read (the test proves nothing otherwise)", after)
	}
}
