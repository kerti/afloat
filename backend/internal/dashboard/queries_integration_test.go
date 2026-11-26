package dashboard_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/dashboard"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
	"github.com/kerti/afloat/backend/internal/testutil"
)

func pgd(t *testing.T, s string) pgtype.Date {
	t.Helper()
	d, err := period.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return pgtype.Date{Time: d.Time(), Valid: true}
}

func (f fixture) expense(t *testing.T, householdID, pocketID pgtype.UUID, category pgtype.UUID, occurredOn, amount string) {
	t.Helper()
	var c any
	if category.Valid {
		c = category
	}
	f.exec(t, `INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id, category_id)
		VALUES ($1, $2, $3::date, $4::numeric, 'IDR', $5, $6)`, testutil.NewUUID(t), householdID, occurredOn, amount, pocketID, c)
}

// PeriodSpend answers one row per Period Day, Category and Pocket, summed, with
// the names the breakdowns will need riding along, and nothing of another
// Household's. Proven red against the statement with its household_id
// predicate neutralised: it answered the other Household's 99999 beside this
// one's.
func TestPeriodSpendGroupsByDayCategoryAndPocketAndReadsOnlyItsOwnHousehold(t *testing.T) {
	f := newFixture(t)
	mine, _ := f.member(t, "Mine", "a@example.com")
	theirs, _ := f.member(t, "Theirs", "b@example.com")
	myWallet, myCard := f.tdb.CreatePocket(t, mine, "Wallet"), f.tdb.CreatePocket(t, mine, "Card")
	theirWallet := f.tdb.CreatePocket(t, theirs, "Wallet")
	food, rent := f.tdb.CreateCategory(t, mine, "Food", "daily"), f.tdb.CreateCategory(t, mine, "Rent", "commitment")
	theirFood := f.tdb.CreateCategory(t, theirs, "Food", "daily")
	none := pgtype.UUID{}

	f.expense(t, mine, myWallet, food, "2026-11-03", "1000")
	f.expense(t, mine, myWallet, food, "2026-11-03", "2500") // one row with the one before
	f.expense(t, mine, myWallet, food, "2026-11-03", "-500") // a refund sums as one
	f.expense(t, mine, myCard, food, "2026-11-03", "70")     // another Pocket, another row
	f.expense(t, mine, myWallet, none, "2026-11-03", "40")   // uncategorised
	f.expense(t, mine, myWallet, rent, "2026-11-04", "300")  // a Commitment's, still a row here
	f.expense(t, mine, myWallet, food, "2026-12-01", "9")    // outside the window
	f.expense(t, theirs, theirWallet, theirFood, "2026-11-03", "99999")
	f.expense(t, theirs, theirWallet, none, "2026-11-04", "99999")
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, myCard)

	rows, err := f.tdb.Queries.PeriodSpend(context.Background(), db.PeriodSpendParams{
		HouseholdID: mine, StartsOn: pgd(t, "2026-11-01"), EndsOn: pgd(t, "2026-11-30"),
	})
	if err != nil {
		t.Fatalf("PeriodSpend: %v", err)
	}

	type row struct {
		day, bucket, pocket, amount string
		category                    string
		pocketActive                bool
	}
	var got []row
	for _, r := range rows {
		name := "<none>"
		if r.CategoryDisplayName != nil {
			name = *r.CategoryDisplayName
		}
		got = append(got, row{period.DateOf(r.OccurredOn.Time).String(), r.Bucket, r.PocketDisplayName, r.Amount.StringFixed(4), name, r.PocketActive})
	}
	// The order is the statement's own (day, then Category with none first, then
	// Pocket id), so a test asserts the part it promises and sorts the rest.
	if len(got) != 4 || got[0].category != "<none>" || got[3].day != "2026-11-04" {
		t.Fatalf("rows = %+v: want four, the uncategorised row leading its day and the 4th last", got)
	}
	byAmount := map[string]row{}
	for _, r := range got {
		byAmount[r.amount] = r
	}
	want := map[string]row{
		"70.0000":   {"2026-11-03", "daily", "Card", "70.0000", "Food", false},
		"3000.0000": {"2026-11-03", "daily", "Wallet", "3000.0000", "Food", true},
		"40.0000":   {"2026-11-03", "daily", "Wallet", "40.0000", "<none>", true},
		"300.0000":  {"2026-11-04", "commitment", "Wallet", "300.0000", "Rent", true},
	}
	for amount, w := range want {
		if byAmount[amount] != w {
			t.Errorf("the row of %s = %+v, want %+v", amount, byAmount[amount], w)
		}
	}
}

// The window is inclusive at both ends, and only live Expenses are read.
func TestPeriodSpendWindowIsInclusiveAndSkipsTheDeleted(t *testing.T) {
	f := newFixture(t)
	mine, _ := f.member(t, "Mine", "a@example.com")
	wallet := f.tdb.CreatePocket(t, mine, "Wallet")
	for _, d := range []string{"2026-10-31", "2026-11-01", "2026-11-30", "2026-12-01"} {
		f.expense(t, mine, wallet, pgtype.UUID{}, d, "1")
	}
	f.exec(t, `UPDATE expenses SET deleted_at = now() WHERE household_id = $1 AND occurred_on = '2026-11-30'`, mine)
	f.expense(t, mine, wallet, pgtype.UUID{}, "2026-11-30", "10")

	rows, err := f.tdb.Queries.PeriodSpend(context.Background(), db.PeriodSpendParams{
		HouseholdID: mine, StartsOn: pgd(t, "2026-11-01"), EndsOn: pgd(t, "2026-11-30"),
	})
	if err != nil {
		t.Fatalf("PeriodSpend: %v", err)
	}
	var days []string
	for _, r := range rows {
		days = append(days, period.DateOf(r.OccurredOn.Time).String()+"="+r.Amount.StringFixed(0))
	}
	if got := strings.Join(days, " "); got != "2026-11-01=1 2026-11-30=10" {
		t.Errorf("rows = %s, want the first and last days, live, and nothing outside", got)
	}
}

// ListAllocationChangeDays is the Period's distinct version dates, oldest
// first, of this Household's Period alone.
func TestListAllocationChangeDaysIsDistinctOrderedAndScoped(t *testing.T) {
	f := newFixture(t)
	mine, _ := f.member(t, "Mine", "a@example.com")
	theirs, _ := f.member(t, "Theirs", "b@example.com")
	food, rent := f.tdb.CreateCategory(t, mine, "Food", "daily"), f.tdb.CreateCategory(t, mine, "Rent", "commitment")
	theirRent := f.tdb.CreateCategory(t, theirs, "Rent", "commitment")
	for _, h := range []pgtype.UUID{mine, theirs} {
		for _, ym := range []string{"2026-10", "2026-11"} {
			f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, $2)`, h, ym)
		}
	}
	ins := func(h pgtype.UUID, ym, day string, c pgtype.UUID) {
		f.exec(t, `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, $2, $3::date, $4, 1)`, h, ym, day, c)
	}
	ins(mine, "2026-11", "2026-11-12", rent)
	ins(mine, "2026-11", "2026-11-12", food) // the same day: one change day
	ins(mine, "2026-11", "2026-11-01", rent)
	ins(mine, "2026-11", "2026-11-20", food)
	ins(mine, "2026-10", "2026-10-05", rent) // another Period
	ins(theirs, "2026-11", "2026-11-07", theirRent)

	days, err := f.tdb.Queries.ListAllocationChangeDays(context.Background(), db.ListAllocationChangeDaysParams{HouseholdID: mine, YearMonth: "2026-11"})
	if err != nil {
		t.Fatalf("ListAllocationChangeDays: %v", err)
	}
	var got []string
	for _, d := range days {
		got = append(got, period.DateOf(d.Time).String())
	}
	if want := "2026-11-01 2026-11-12 2026-11-20"; strings.Join(got, " ") != want {
		t.Errorf("change days = %v, want %s", got, want)
	}
	none, err := f.tdb.Queries.ListAllocationChangeDays(context.Background(), db.ListAllocationChangeDaysParams{HouseholdID: mine, YearMonth: "2026-12"})
	if err != nil || len(none) != 0 {
		t.Errorf("an unmaterialised Period's change days = %v, %v, want none", none, err)
	}
}

// ---- the fold, without a database -----------------------------------------

func rowOf(t *testing.T, day, bucket, amount string) db.PeriodSpendRow {
	t.Helper()
	return db.PeriodSpendRow{OccurredOn: pgd(t, day), Bucket: bucket, Amount: decimal.RequireFromString(amount)}
}

func positionOf(t *testing.T, today string) budget.Position {
	t.Helper()
	d, err := period.ParseDate(today)
	if err != nil {
		t.Fatal(err)
	}
	p, err := period.Of(d, 1)
	if err != nil {
		t.Fatal(err)
	}
	return budget.Position{Today: d, Period: p}
}

func TestFoldSpendSplitsSettledTodayAndFutureAndLeavesCommitmentsOut(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	got, err := dashboard.FoldSpend([]db.PeriodSpendRow{
		rowOf(t, "2026-11-13", "daily", "100"),
		rowOf(t, "2026-11-13", "daily", "-30"),
		rowOf(t, "2026-11-14", "daily", "7"),
		rowOf(t, "2026-11-14", "commitment", "5000"),
		rowOf(t, "2026-11-30", "daily", "-2"),
	}, at)
	if err != nil {
		t.Fatalf("FoldSpend: %v", err)
	}
	if got.Settled.String() != "70" || got.Today.String() != "7" || got.Future.String() != "-2" {
		t.Errorf("settled %s, today %s, future %s; want 70, 7, -2", got.Settled, got.Today, got.Future)
	}
	if len(got.ByDay) != 3 || got.ByDay[period.Date{Year: 2026, Month: time.November, Day: 14}].String() != "7" {
		t.Errorf("by day = %v, want three days and Today's 7: the Commitment is in no daily figure", got.ByDay)
	}
}

func TestFoldSpendRefusesARowThatIsNotDailyOrCommitmentOrNotInThePeriod(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	if _, err := dashboard.FoldSpend([]db.PeriodSpendRow{rowOf(t, "2026-11-13", "other", "1")}, at); err == nil {
		t.Error("an unknown bucket was folded, want an error")
	}
	for _, day := range []string{"2026-10-31", "2026-12-01"} {
		if _, err := dashboard.FoldSpend([]db.PeriodSpendRow{rowOf(t, day, "daily", "1")}, at); err == nil {
			t.Errorf("a row dated %s was folded into the November Period, want an error", day)
		}
	}
	if got, err := dashboard.FoldSpend(nil, at); err != nil || len(got.ByDay) != 0 || !got.Settled.IsZero() {
		t.Errorf("FoldSpend(nil) = %+v, %v, want zero spend", got, err)
	}
}

// A failed statement is an error, not zero spend.
func TestLoadSpendReportsAFailedStatement(t *testing.T) {
	f := newFixtureWith(t, func(q db.Querier) db.Querier { return spendFailing{q} }, nil)
	mine, _ := f.member(t, "Mine", "a@example.com")
	if _, err := dashboard.LoadSpend(context.Background(), spendFailing{f.tdb.Queries}, mine, positionOf(t, "2026-11-14")); err == nil {
		t.Error("LoadSpend swallowed a failed PeriodSpend")
	}
}

type spendFailing struct{ db.Querier }

func (spendFailing) PeriodSpend(context.Context, db.PeriodSpendParams) ([]db.PeriodSpendRow, error) {
	return nil, errBoom
}
