package budget_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
)

// What the Budget does with allocations (#130 C4, C5, C11): the figures they
// move, the carry-forward that copies them into a new Period, and the
// materialisation race with them in it. The Categories are made with SQL, so
// these tests do not depend on the category package.

// category inserts a live, active Category and returns its id.
func (f fixture) category(t *testing.T, householdID pgtype.UUID, name, bucket string) pgtype.UUID {
	t.Helper()
	return f.tdb.CreateCategory(t, householdID, name, bucket)
}

// setAllocation is a change made through the package's own writer, as the
// Category create makes one: the Period materialised, the allocation dated the
// clock's Today for the caller, in one transaction.
func (f fixture) setAllocation(ctx context.Context, t *testing.T, categoryID pgtype.UUID, amount string) error {
	t.Helper()
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		t.Fatal("no User in the context")
	}
	at, err := budget.Locate(ctx, f.tdb.Queries, f.clock.now, user)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.tdb.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	if err := budget.Materialise(ctx, q, user.HouseholdID, at.Period); err != nil {
		t.Fatal(err)
	}
	if err := budget.SetAllocation(ctx, q, user.HouseholdID, at, categoryID, decimal.RequireFromString(amount)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// allocate writes an allocation row straight into the log, with the Period's
// marker made if it is not there. amount "" is a NULL row ("none from that day").
func (f fixture) allocate(t *testing.T, householdID pgtype.UUID, yearMonth, effectiveOn string, categoryID pgtype.UUID, amount string) {
	t.Helper()
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, $2) ON CONFLICT DO NOTHING`, householdID, yearMonth)
	var v any
	if amount != "" {
		v = amount
	}
	f.exec(t, `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, $2, $3::date, $4, $5::numeric)`,
		householdID, yearMonth, effectiveOn, categoryID, v)
}

// allocationShape is the household's allocation log as "year_month effective_on
// name amount", NULLs as "none", ordered.
func (f fixture) allocationShape(t *testing.T, householdID pgtype.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(a.year_month || ' ' || a.effective_on || ' ' || c.category_display_name || ' ' || coalesce(a.allocation::text, 'none'),
		                           '; ' ORDER BY a.year_month, a.effective_on, c.category_display_name), '')
		 FROM allocations a JOIN categories c ON c.id = a.category_id AND c.household_id = a.household_id
		 WHERE a.household_id = $1`, householdID).Scan(&s); err != nil {
		t.Fatalf("read allocations: %v", err)
	}
	return s
}

func (f fixture) allocationsRows(t *testing.T, householdID pgtype.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(to_jsonb(a)::text, E'\n' ORDER BY year_month, effective_on, category_id), '') FROM allocations a WHERE household_id = $1`, householdID).Scan(&s); err != nil {
		t.Fatalf("read allocations: %v", err)
	}
	return s
}

// listed is the answer's allocations as "category_id=amount" in wire order.
func listed(got api.Budget) string {
	parts := make([]string, 0, len(got.Allocations))
	for _, a := range got.Allocations {
		parts = append(parts, a.CategoryId.String()+"="+a.Allocation)
	}
	return strings.Join(parts, ",")
}

// ---- budget.json -----------------------------------------------------------

type budgetFixture struct {
	Rows []struct {
		Name          string  `json:"name"`
		MonthlyBudget *string `json:"monthly_budget"`
		Categories    []struct {
			Name       string  `json:"name"`
			Bucket     string  `json:"bucket"`
			Allocation *string `json:"allocation"`
			Active     bool    `json:"active"`
			Deleted    bool    `json:"deleted"`
		} `json:"categories"`
		Expected struct {
			CommitmentsBudget string  `json:"commitments_budget"`
			DailyPool         *string `json:"daily_pool"`
			Unallocated       *string `json:"unallocated"`
			Allocations       []struct {
				Category   string `json:"category"`
				Allocation string `json:"allocation"`
			} `json:"allocations"`
		} `json:"expected"`
	} `json:"rows"`
}

// The Budget's figures as pinned by contract/testdata/budget.json (BOOTSTRAP.md
// §6, #130 C11): each row builds a Household, its Categories and their
// allocations in the current Period, and what GET /budget answers is compared
// with the file. The suite keeps no copy of the numbers.
func TestTheBudgetFiguresAreThoseOfBudgetJSON(t *testing.T) {
	raw, err := os.ReadFile("../../../contract/testdata/budget.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx budgetFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(fx.Rows) == 0 {
		t.Fatal("fixture has no rows: this suite would check nothing")
	}

	for _, row := range fx.Rows {
		t.Run(row.Name, func(t *testing.T) {
			f := newFixture(t)
			householdID, _, ctx := f.member(t, "Home", "a@example.com")
			for _, c := range row.Categories {
				id := f.category(t, householdID, c.Name, c.Bucket)
				if !c.Active {
					f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, id)
				}
				if c.Allocation != nil {
					f.allocate(t, householdID, "2026-11", "2026-11-14", id, *c.Allocation)
				}
				if c.Deleted {
					f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, id)
				}
			}
			if row.MonthlyBudget != nil {
				f.patch(ctx, t, *row.MonthlyBudget)
			}
			got := f.get(ctx, t)

			if got.CommitmentsBudget != row.Expected.CommitmentsBudget {
				t.Errorf("commitments_budget = %s, want %s", got.CommitmentsBudget, row.Expected.CommitmentsBudget)
			}
			if deref(got.DailyPool) != orOmitted(row.Expected.DailyPool) {
				t.Errorf("daily_pool = %s, want %s", deref(got.DailyPool), orOmitted(row.Expected.DailyPool))
			}
			if deref(got.Unallocated) != orOmitted(row.Expected.Unallocated) {
				t.Errorf("unallocated = %s, want %s", deref(got.Unallocated), orOmitted(row.Expected.Unallocated))
			}
			var want []string
			for _, a := range row.Expected.Allocations {
				want = append(want, fmt.Sprintf("%s=%s", f.idOf(t, householdID, a.Category), a.Allocation))
			}
			if listed(got) != strings.Join(want, ",") {
				t.Errorf("allocations = %q, want %q", listed(got), strings.Join(want, ","))
			}
		})
	}
}

func orOmitted(s *string) string {
	if s == nil {
		return "<omitted>"
	}
	return *s
}

// idOf is the id of the Household's Category of that name, live or not.
func (f fixture) idOf(t *testing.T, householdID pgtype.UUID, name string) string {
	t.Helper()
	var id pgtype.UUID
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT id FROM categories WHERE household_id = $1 AND category_display_name = $2`, householdID, name).Scan(&id); err != nil {
		t.Fatalf("id of %s: %v", name, err)
	}
	return id.String()
}

// ---- the as-of rule for allocations ----------------------------------------

// #130 C4: allocations are read as of max(Today, F), F being the Period's first
// Monthly Budget, so a Commitment typed before the Monthly Budget is part of
// the same setup.
func TestAnAllocationTypedBeforeTheMonthlyBudgetIsPartOfTheSameSetup(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.allocate(t, householdID, "2026-11", "2026-11-10", rent, "3000000")

	f.clock.set(t, "2026-11-10T10:00:00+07:00")
	before := f.get(ctx, t)
	if listed(before) != rent.String()+"=3000000.0000" || before.CommitmentsBudget != "3000000.0000" {
		t.Errorf("before a Monthly Budget: allocations %q, commitments %s; want the allocation listed", listed(before), before.CommitmentsBudget)
	}
	if before.MonthlyBudget != nil || before.DailyPool != nil || before.Unallocated != nil {
		t.Errorf("before a Monthly Budget the pool figures must be omitted: %+v", before)
	}

	f.clock.set(t, "2026-11-14T10:00:00+07:00")
	after := f.patch(ctx, t, "9000000")
	if deref(after.DailyPool) != "6000000.0000" || after.CommitmentsBudget != "3000000.0000" {
		t.Errorf("pool %s commitments %s, want 6000000.0000 and 3000000.0000", deref(after.DailyPool), after.CommitmentsBudget)
	}
}

// A Commitment changed after the first day moves the Daily Pool from that day:
// the version in force is the latest one on or before Today.
func TestAnAllocationChangeOnALaterDayIsReadAsOfTodayAndNotBefore(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.patch(ctx, t, "9000000") // 14 Nov
	f.allocate(t, householdID, "2026-11", "2026-11-14", rent, "3000000")
	f.allocate(t, householdID, "2026-11", "2026-11-20", rent, "4000000")

	for _, tc := range []struct{ now, pool string }{
		{"2026-11-14T10:00:00+07:00", "6000000.0000"},
		{"2026-11-19T10:00:00+07:00", "6000000.0000"},
		{"2026-11-20T03:59:59+07:00", "6000000.0000"}, // still the 19th
		{"2026-11-20T04:00:00+07:00", "5000000.0000"},
		{"2026-11-28T10:00:00+07:00", "5000000.0000"},
	} {
		f.clock.set(t, tc.now)
		if got := f.get(ctx, t); deref(got.DailyPool) != tc.pool {
			t.Errorf("at %s the Daily Pool = %s, want %s", tc.now, deref(got.DailyPool), tc.pool)
		}
	}
}

// ---- SetAllocation ---------------------------------------------------------

func TestASecondAllocationTheSameDayOverwritesTheFirstAndANextDayAddsAVersion(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")

	if err := f.setAllocation(ctx, t, rent, "3000000"); err != nil {
		t.Fatal(err)
	}
	if err := f.setAllocation(ctx, t, rent, "3100000"); err != nil {
		t.Fatal(err)
	}
	if v := f.allocationShape(t, householdID); v != "2026-11 2026-11-14 Rent 3100000.0000" {
		t.Errorf("allocations = %q: the same day overwrites", v)
	}
	f.clock.set(t, "2026-11-15T10:00:00+07:00")
	if err := f.setAllocation(ctx, t, rent, "3200000"); err != nil {
		t.Fatal(err)
	}
	if v := f.allocationShape(t, householdID); v != "2026-11 2026-11-14 Rent 3100000.0000; 2026-11 2026-11-15 Rent 3200000.0000" {
		t.Errorf("allocations = %q: a later day adds a version", v)
	}
}

// Unknown, another Household's and deleted are one answer, and nothing is
// written: the statement that checks the Category is the one that writes.
func TestAnAllocationForACategoryThatIsNotTheCallersLiveOneIsRefusedAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	mine, _, myCtx := f.member(t, "Home", "a@example.com")
	theirs, _, _ := f.member(t, "Other", "b@example.com")
	foreign := f.category(t, theirs, "Theirs", "commitment")
	deleted := f.category(t, mine, "Gone", "commitment")
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, deleted)

	for name, id := range map[string]pgtype.UUID{
		"another Household's": foreign,
		"a deleted one":       deleted,
		"an unknown one":      {Bytes: [16]byte{1}, Valid: true},
	} {
		if err := f.setAllocation(myCtx, t, id, "1"); !errors.Is(err, budget.ErrCategoryNotLive) {
			t.Errorf("%s: err = %v, want ErrCategoryNotLive", name, err)
		}
	}
	if n := f.countAllocations(t); n != 0 {
		t.Errorf("allocations = %d, want none written", n)
	}
}

func (f fixture) countAllocations(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM allocations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ---- carry-forward ---------------------------------------------------------

func TestANewPeriodCopiesTheLatestAllocationStateDatedItsFirstDay(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	food := f.category(t, householdID, "Food", "daily")
	f.clock.set(t, "2026-11-01T10:00:00+07:00")
	f.patch(ctx, t, "9000000")
	f.allocate(t, householdID, "2026-11", "2026-11-01", rent, "3000000")
	f.allocate(t, householdID, "2026-11", "2026-11-12", rent, "3200000")
	f.allocate(t, householdID, "2026-11", "2026-11-05", food, "1000000")
	novBefore := f.allocationShape(t, householdID)

	f.clock.set(t, "2026-12-05T10:00:00+07:00")
	got := f.get(ctx, t)

	if listed(got) != rent.String()+"=3200000.0000,"+food.String()+"=1000000.0000" {
		t.Errorf("allocations = %q, want the latest state of each", listed(got))
	}
	if deref(got.DailyPool) != "5800000.0000" || deref(got.Unallocated) != "4800000.0000" {
		t.Errorf("pool %s unallocated %s, want 5800000.0000 and 4800000.0000", deref(got.DailyPool), deref(got.Unallocated))
	}
	if v := f.allocationShape(t, householdID); v != novBefore+"; 2026-12 2026-12-01 Food 1000000.0000; 2026-12 2026-12-01 Rent 3200000.0000" {
		t.Errorf("allocations = %q: want exactly the latest state copied, dated 2026-12-01, November untouched", v)
	}

	// A change in the new Period does not reach back, and one on the Period's
	// first day corrects the copy rather than adding a version.
	f.clock.set(t, "2026-12-01T10:00:00+07:00")
	if err := f.setAllocation(ctx, t, rent, "3300000"); err != nil {
		t.Fatal(err)
	}
	if v := f.allocationShape(t, householdID); v != novBefore+"; 2026-12 2026-12-01 Food 1000000.0000; 2026-12 2026-12-01 Rent 3300000.0000" {
		t.Errorf("allocations = %q", v)
	}
}

func TestAPeriodNobodyOpenedIsSkippedAndTheNextCopiesFromTheLastThatExists(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.patch(ctx, t, "9000000")
	f.allocate(t, householdID, "2026-11", "2026-11-14", rent, "3000000")

	f.clock.set(t, "2027-02-10T10:00:00+07:00")
	got := f.get(ctx, t)

	if listed(got) != rent.String()+"=3000000.0000" {
		t.Errorf("allocations = %q", listed(got))
	}
	if labels := f.markerLabels(t, householdID); labels != "2026-11,2027-02" {
		t.Errorf("budgets = %q, want only the two Periods that were opened", labels)
	}
	if v := f.allocationShape(t, householdID); v != "2026-11 2026-11-14 Rent 3000000.0000; 2027-02 2027-02-01 Rent 3000000.0000" {
		t.Errorf("allocations = %q", v)
	}
}

// The source is one Period for the Monthly Budget and the allocations alike, and
// a Period that holds only allocations is a source.
func TestAPeriodHoldingOnlyAllocationsIsASourceAndAMarkerOnlyOneIsNot(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.allocate(t, householdID, "2026-09", "2026-09-10", rent, "2000000")
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-10')`, householdID) // marker only

	got := f.get(ctx, t) // 14 Nov: 2026-10 is no source, 2026-09 is

	if listed(got) != rent.String()+"=2000000.0000" || got.CommitmentsBudget != "2000000.0000" {
		t.Errorf("allocations %q commitments %s, want September's carried", listed(got), got.CommitmentsBudget)
	}
	if got.MonthlyBudget != nil {
		t.Errorf("monthly_budget = %s, want none: the source held none", *got.MonthlyBudget)
	}
	if v := f.shape(t, householdID); v != "" {
		t.Errorf("monthly_budgets = %q, want none", v)
	}
}

// What is carried: every allocation whose latest row is non-null and whose
// Category is not deleted, inactive ones included.
func TestOnlyNonNullAllocationsOfCategoriesThatAreNotDeletedAreCarried(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	live := f.category(t, householdID, "Live", "commitment")
	inactive := f.category(t, householdID, "Inactive", "commitment")
	deleted := f.category(t, householdID, "Deleted", "daily")
	cleared := f.category(t, householdID, "Cleared", "daily")
	zero := f.category(t, householdID, "Zero", "commitment")
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, inactive)
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, deleted)
	f.clock.set(t, "2026-11-01T10:00:00+07:00")
	f.patch(ctx, t, "9000000")
	for _, c := range []pgtype.UUID{live, inactive, deleted, cleared} {
		f.allocate(t, householdID, "2026-11", "2026-11-01", c, "1000")
	}
	f.allocate(t, householdID, "2026-11", "2026-11-09", cleared, "") // none from the 9th
	f.allocate(t, householdID, "2026-11", "2026-11-01", zero, "0")

	f.clock.set(t, "2026-12-02T10:00:00+07:00")
	got := f.get(ctx, t)

	if v := f.allocationShape(t, householdID); !strings.HasSuffix(v, "2026-12 2026-12-01 Inactive 1000.0000; 2026-12 2026-12-01 Live 1000.0000; 2026-12 2026-12-01 Zero 0.0000") {
		t.Errorf("allocations = %q: want Inactive, Live and Zero carried (a zero is an allocation), not Deleted or Cleared", v)
	}
	if listed(got) != live.String()+"=1000.0000,"+inactive.String()+"=1000.0000,"+zero.String()+"=0.0000" {
		t.Errorf("allocations = %q", listed(got))
	}
}

// #130 C3: a period_start_day change re-labels Today. A label with no Budget
// copies from the nearest earlier Period, else the nearest later one, and the
// rows it copies from stay as they were.
func TestARelabelledPeriodCopiesAllocationsFromTheNearestEarlierElseTheLaterPeriod(t *testing.T) {
	t.Run("the nearest earlier one", func(t *testing.T) {
		f := newFixture(t)
		householdID, _, ctx := f.member(t, "Home", "a@example.com")
		rent := f.category(t, householdID, "Rent", "commitment")
		f.allocate(t, householdID, "2026-09", "2026-09-01", rent, "1000000")
		f.allocate(t, householdID, "2026-09", "2026-09-20", rent, "1100000")
		f.allocate(t, householdID, "2026-12", "2026-12-01", rent, "9000000")

		got := f.get(ctx, t) // 14 Nov: 2026-11 has no row; 2026-09 is the nearest earlier

		if listed(got) != rent.String()+"=1100000.0000" {
			t.Errorf("allocations = %q, want September's latest", listed(got))
		}
	})
	t.Run("else the nearest later one", func(t *testing.T) {
		f := newFixture(t)
		householdID, _, ctx := f.member(t, "Home", "a@example.com")
		rent := f.category(t, householdID, "Rent", "commitment")
		f.clock.set(t, "2026-11-10T10:00:00+07:00")
		f.allocate(t, householdID, "2026-11", "2026-11-10", rent, "3000000")
		f.exec(t, `UPDATE households SET period_start_day = 25 WHERE id = $1`, householdID)

		got := f.get(ctx, t) // 10 Nov is now in 2026-10, which opened on 25 Oct; nothing earlier holds anything

		if got.Period.YearMonth != "2026-10" || listed(got) != rent.String()+"=3000000.0000" {
			t.Errorf("period %s allocations %q, want 2026-10 carrying November's", got.Period.YearMonth, listed(got))
		}
		if v := f.allocationShape(t, householdID); v != "2026-10 2026-10-25 Rent 3000000.0000; 2026-11 2026-11-10 Rent 3000000.0000" {
			t.Errorf("allocations = %q: the copy is dated the re-cut Period's first day and November is untouched", v)
		}
	})
}

// Deleting a Category drops it out of the figures at once, whatever Period it
// was allocated in, because reads join live Categories.
func TestADeletedCategoryLeavesTheFiguresAndRestoringItBringsTheAllocationBack(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.patch(ctx, t, "9000000")
	f.allocate(t, householdID, "2026-11", "2026-11-14", rent, "3000000")
	before := f.allocationsRows(t, householdID)

	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, rent)
	got := f.get(ctx, t)
	if len(got.Allocations) != 0 || got.CommitmentsBudget != "0.0000" || deref(got.DailyPool) != "9000000.0000" {
		t.Errorf("deleted: allocations %q commitments %s pool %s, want it gone from every figure", listed(got), got.CommitmentsBudget, deref(got.DailyPool))
	}
	f.exec(t, `UPDATE categories SET deleted_at = NULL WHERE id = $1`, rent)
	got = f.get(ctx, t)
	if deref(got.DailyPool) != "6000000.0000" || f.allocationsRows(t, householdID) != before {
		t.Errorf("restored: pool %s, rows changed %v, want exactly as before", deref(got.DailyPool), f.allocationsRows(t, householdID) != before)
	}
}

// ---- tenancy ---------------------------------------------------------------

// Two Households, one Period label: each sees and carries only its own
// allocations, and a read or a materialisation by one leaves every column of the
// other's rows byte-identical. Proven red against statements that lost their
// household_id filter (BOOTSTRAP §4, tenancy).
func TestTenancyAllocationsNeverCrossHouseholds(t *testing.T) {
	f := newFixture(t)
	mine, _, myCtx := f.member(t, "Home", "a@example.com")
	theirs, _, theirCtx := f.member(t, "Other", "b@example.com")
	myRent := f.category(t, mine, "Rent", "commitment")
	theirRent := f.category(t, theirs, "Rent", "commitment")
	theirFood := f.category(t, theirs, "Food", "daily")
	f.clock.set(t, "2026-10-10T10:00:00+07:00")
	f.patch(myCtx, t, "9000000")
	f.patch(theirCtx, t, "20000000")
	f.allocate(t, mine, "2026-10", "2026-10-10", myRent, "1000000")
	f.allocate(t, theirs, "2026-10", "2026-10-10", theirRent, "7000000")
	f.allocate(t, theirs, "2026-10", "2026-10-10", theirFood, "999")
	theirBefore := f.allocationsRows(t, theirs)

	// 1. Reading the Budget lists only one's own.
	if got := f.get(myCtx, t); listed(got) != myRent.String()+"=1000000.0000" || got.CommitmentsBudget != "1000000.0000" {
		t.Errorf("my Budget: allocations %q commitments %s", listed(got), got.CommitmentsBudget)
	}
	// 2. Materialising a new Period copies one's own and never the other's, even
	// though the other's are the nearer-by-id and bigger.
	f.clock.set(t, "2026-11-03T10:00:00+07:00")
	got := f.get(myCtx, t)
	if listed(got) != myRent.String()+"=1000000.0000" {
		t.Errorf("my November allocations = %q, want only my own carried", listed(got))
	}
	if v := f.allocationShape(t, mine); v != "2026-10 2026-10-10 Rent 1000000.0000; 2026-11 2026-11-01 Rent 1000000.0000" {
		t.Errorf("my allocations = %q", v)
	}
	if now := f.allocationsRows(t, theirs); now != theirBefore {
		t.Errorf("the other Household's allocations changed:\n%s\n%s", theirBefore, now)
	}
	// 3. Nothing of the other's ever lands on one's own Period, whichever order
	// they are first read in.
	if theirGot := f.get(theirCtx, t); listed(theirGot) != theirRent.String()+"=7000000.0000,"+theirFood.String()+"=999.0000" {
		t.Errorf("their November allocations = %q", listed(theirGot))
	}
	if v := f.allocationShape(t, mine); v != "2026-10 2026-10-10 Rent 1000000.0000; 2026-11 2026-11-01 Rent 1000000.0000" {
		t.Errorf("my allocations = %q after the other's first read", v)
	}
}

// The later-source fallback is scoped too.
func TestTheLaterAllocationSourceIsScopedToTheCallersHousehold(t *testing.T) {
	f := newFixture(t)
	mine, _, myCtx := f.member(t, "Home", "a@example.com")
	theirs, _, _ := f.member(t, "Other", "b@example.com")
	myRent := f.category(t, mine, "Rent", "commitment")
	theirRent := f.category(t, theirs, "Rent", "commitment")
	f.allocate(t, mine, "2026-12", "2026-12-01", myRent, "2000000")
	f.allocate(t, theirs, "2026-11", "2026-11-01", theirRent, "99999999")

	f.clock.set(t, "2026-10-10T10:00:00+07:00")
	got := f.get(myCtx, t) // nothing earlier anywhere: my December, not their nearer November

	if listed(got) != myRent.String()+"=2000000.0000" {
		t.Errorf("allocations = %q, want my own December's", listed(got))
	}
}

// ---- the race --------------------------------------------------------------

// N concurrent first reads and changes of one Period, with allocations to copy:
// one marker, one copy of each allocation, no 500, every answer identical. The
// marker and its copy are one transaction, so no request sees the first without
// the second. Proven red against a SELECT-then-INSERT marker (#130 C5).
func TestAConcurrentFirstReadOfOnePeriodCopiesEachAllocationOnceAndEveryAnswerAgrees(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	food := f.category(t, householdID, "Food", "daily")
	f.clock.set(t, "2026-10-10T10:00:00+07:00")
	f.patch(ctx, t, "9000000")
	f.allocate(t, householdID, "2026-10", "2026-10-10", rent, "3000000")
	f.allocate(t, householdID, "2026-10", "2026-10-10", food, "1000000")
	f.clock.set(t, "2026-11-10T10:00:00+07:00")

	const n = 24
	answers := make([]string, n)
	statuses := make([]string, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := f.handlers.GetBudget(ctx, api.GetBudgetRequestObject{})
			if err != nil {
				statuses[i] = err.Error()
				return
			}
			ok, is := got.(api.GetBudget200JSONResponse)
			if !is {
				statuses[i] = fmt.Sprintf("%T", got)
				return
			}
			statuses[i] = "200"
			b, _ := json.Marshal(api.Budget(ok))
			answers[i] = string(b)
		}()
	}
	close(start)
	wg.Wait()

	for i := range n {
		if statuses[i] != "200" {
			t.Fatalf("request %d answered %s, want 200", i, statuses[i])
		}
		if answers[i] != answers[0] {
			t.Fatalf("answers differ:\n%s\n%s", answers[0], answers[i])
		}
	}
	if !strings.Contains(answers[0], `"commitments_budget":"3000000.0000"`) || !strings.Contains(answers[0], `"daily_pool":"6000000.0000"`) {
		t.Errorf("answer = %s, want the carried allocations in every answer", answers[0])
	}
	if labels := f.markerLabels(t, householdID); labels != "2026-10,2026-11" {
		t.Errorf("budgets = %q, want one marker per Period", labels)
	}
	if v := f.allocationShape(t, householdID); v != "2026-10 2026-10-10 Food 1000000.0000; 2026-10 2026-10-10 Rent 3000000.0000; 2026-11 2026-11-01 Food 1000000.0000; 2026-11 2026-11-01 Rent 3000000.0000" {
		t.Errorf("allocations = %q, want exactly one copy of each", v)
	}
}

// Repeated across Periods, because the window a faulty materialiser leaves open
// is narrow: a marker visible without its allocations shows as a Budget with no
// Commitments Budget.
func TestConcurrentFirstReadsAndChangesNeverShowAMarkerWithoutItsAllocations(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	rent := f.category(t, householdID, "Rent", "commitment")
	f.clock.set(t, "2026-01-10T10:00:00+07:00")
	f.patch(ctx, t, "6000000")
	f.allocate(t, householdID, "2026-01", "2026-01-10", rent, "2000000")

	for month := 2; month <= 12; month++ {
		f.clock.set(t, fmt.Sprintf("2026-%02d-10T10:00:00+07:00", month))
		var wg sync.WaitGroup
		var mu sync.Mutex
		var problems []string
		start := make(chan struct{})
		for i := range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				var got api.Budget
				if i%3 == 0 {
					resp, err := f.handlers.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{}})
					r, ok := resp.(api.UpdateBudget200JSONResponse)
					if err != nil || !ok {
						mu.Lock()
						problems = append(problems, fmt.Sprintf("patch: %#v %v", resp, err))
						mu.Unlock()
						return
					}
					got = api.Budget(r)
				} else {
					resp, err := f.handlers.GetBudget(ctx, api.GetBudgetRequestObject{})
					r, ok := resp.(api.GetBudget200JSONResponse)
					if err != nil || !ok {
						mu.Lock()
						problems = append(problems, fmt.Sprintf("get: %#v %v", resp, err))
						mu.Unlock()
						return
					}
					got = api.Budget(r)
				}
				if got.CommitmentsBudget != "2000000.0000" || deref(got.DailyPool) != "4000000.0000" {
					mu.Lock()
					problems = append(problems, "an answer without the carried allocation: commitments "+got.CommitmentsBudget)
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()
		if len(problems) > 0 {
			t.Fatalf("month %d: %v", month, problems)
		}
	}
	if n := f.countAllocations(t); n != 12 {
		t.Errorf("allocations rows = %d, want 12: one per Period", n)
	}
}
