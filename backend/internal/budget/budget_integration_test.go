package budget_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// The tests here talk to a real Postgres and none is parallel (ADR-0004): they
// share one database that each test empties. The concurrent ones start their own
// goroutines, which is the point of them. Every one pins the clock, so the
// Period Day arithmetic is asserted on exact dates.

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t *testing.T, rfc3339 string) {
	t.Helper()
	at, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatalf("clock %q: %v", rfc3339, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = at
}

type fixture struct {
	tdb      *testutil.TestDB
	clock    *clock
	handlers *budget.Handlers
	begins   *countingBeginner
}

// countingBeginner counts the transactions a request opens: a request that
// finds its Period already there must open none.
type countingBeginner struct {
	budget.Beginner
	mu    sync.Mutex
	n     int
	reads []pgx.TxOptions
}

func (b *countingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	b.mu.Lock()
	b.n++
	b.mu.Unlock()
	return b.Beginner.Begin(ctx)
}

// BeginTx is the snapshot read of GET /budget: counted apart from a write's
// Begin, with the options it asked for.
func (b *countingBeginner) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	b.mu.Lock()
	b.reads = append(b.reads, opts)
	b.mu.Unlock()
	return b.Beginner.BeginTx(ctx, opts)
}

// count is the write transactions opened.
func (b *countingBeginner) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.n
}

// snapshots is the read transactions opened, with their options.
func (b *countingBeginner) snapshots() []pgx.TxOptions {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.reads)
}

// 10:00 on 14 Nov 2026 in Jakarta, the User's zone unless a test moves them.
const day14 = "2026-11-14T10:00:00+07:00"

func newFixture(t *testing.T) fixture {
	t.Helper()
	return newFixtureWith(t, nil, nil)
}

// newFixtureWith wraps the real Querier and Beginner, so a test can break one
// call.
func newFixtureWith(t *testing.T, wrapQ func(db.Querier) db.Querier, wrapTx func(budget.Beginner) budget.Beginner) fixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	c := &clock{}
	c.set(t, day14)
	var q db.Querier = tdb.Queries
	if wrapQ != nil {
		q = wrapQ(q)
	}
	counting := &countingBeginner{Beginner: tdb.Pool}
	var tx budget.Beginner = counting
	if wrapTx != nil {
		tx = wrapTx(counting)
	}
	return fixture{
		tdb:      tdb,
		clock:    c,
		handlers: budget.New(budget.Deps{Querier: q, Beginner: tx, Now: c.now}),
		begins:   counting,
	}
}

// member creates a Household with one User (Asia/Jakarta, the default) and
// returns the Household's id, the User's id and the context of a request that
// User's session produced.
func (f fixture) member(t *testing.T, name, email string) (pgtype.UUID, pgtype.UUID, context.Context) {
	t.Helper()
	householdID := f.tdb.CreateHousehold(t, name)
	userID := f.tdb.CreateUser(t, householdID, email, "A")
	return householdID, userID, f.ctxOf(t, userID)
}

func (f fixture) ctxOf(t *testing.T, userID pgtype.UUID) context.Context {
	t.Helper()
	user, err := f.tdb.Queries.GetUserByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	return auth.WithUser(context.Background(), user)
}

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tdb.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
}

func (f fixture) get(ctx context.Context, t *testing.T) api.Budget {
	t.Helper()
	resp, err := f.handlers.GetBudget(ctx, api.GetBudgetRequestObject{})
	if err != nil {
		t.Fatalf("GetBudget returned an error: %v", err)
	}
	got, is := resp.(api.GetBudget200JSONResponse)
	if !is {
		t.Fatalf("GetBudget = %#v, want 200", resp)
	}
	return api.Budget(got)
}

func (f fixture) patch(ctx context.Context, t *testing.T, monthly string) api.Budget {
	t.Helper()
	resp := f.patchResp(ctx, t, monthly)
	got, is := resp.(api.UpdateBudget200JSONResponse)
	if !is {
		t.Fatalf("UpdateBudget = %#v, want 200", resp)
	}
	return api.Budget(got)
}

// patchResp sends a body with monthly_budget set, or with it left out when
// monthly is empty.
func (f fixture) patchResp(ctx context.Context, t *testing.T, monthly string) api.UpdateBudgetResponseObject {
	t.Helper()
	body := api.UpdateBudgetJSONRequestBody{}
	if monthly != "" {
		body.MonthlyBudget = &monthly
	}
	resp, err := f.handlers.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &body})
	if err != nil {
		t.Fatalf("UpdateBudget returned an error: %v", err)
	}
	return resp
}

// versions is the household's Monthly Budget log, whole rows, ordered.
func (f fixture) versions(t *testing.T, householdID pgtype.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(to_jsonb(m)::text, E'\n' ORDER BY year_month, effective_on), '')
		 FROM monthly_budgets m WHERE household_id = $1`, householdID).Scan(&s); err != nil {
		t.Fatalf("read versions: %v", err)
	}
	return s
}

// shape is the log reduced to "year_month effective_on amount", for assertions
// that care about what, not about every column.
func (f fixture) shape(t *testing.T, householdID pgtype.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(year_month || ' ' || effective_on || ' ' || monthly_budget, '; ' ORDER BY year_month, effective_on), '')
		 FROM monthly_budgets WHERE household_id = $1`, householdID).Scan(&s); err != nil {
		t.Fatalf("read versions: %v", err)
	}
	return s
}

func (f fixture) markers(t *testing.T, householdID pgtype.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(to_jsonb(b)::text, E'\n' ORDER BY year_month), '') FROM budgets b WHERE household_id = $1`, householdID).Scan(&s); err != nil {
		t.Fatalf("read budgets: %v", err)
	}
	return s
}

func (f fixture) markerLabels(t *testing.T, householdID pgtype.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(year_month, ','  ORDER BY year_month), '') FROM budgets WHERE household_id = $1`, householdID).Scan(&s); err != nil {
		t.Fatalf("read budgets: %v", err)
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return "<omitted>"
	}
	return *s
}

func assertBudget(t *testing.T, got api.Budget, yearMonth, startsOn, endsOn, monthly, pool, unallocated string) {
	t.Helper()
	if got.Period.YearMonth != yearMonth || got.Period.StartsOn.Format("2006-01-02") != startsOn || got.Period.EndsOn.Format("2006-01-02") != endsOn {
		t.Errorf("period = %s %s..%s, want %s %s..%s", got.Period.YearMonth,
			got.Period.StartsOn.Format("2006-01-02"), got.Period.EndsOn.Format("2006-01-02"), yearMonth, startsOn, endsOn)
	}
	want := func(s string) string {
		if s == "" {
			return "<omitted>"
		}
		return s
	}
	if deref(got.MonthlyBudget) != want(monthly) || deref(got.DailyPool) != want(pool) || deref(got.Unallocated) != want(unallocated) {
		t.Errorf("monthly %s, pool %s, unallocated %s; want %s, %s, %s",
			deref(got.MonthlyBudget), deref(got.DailyPool), deref(got.Unallocated), want(monthly), want(pool), want(unallocated))
	}
	if got.CommitmentsBudget != "0.0000" {
		t.Errorf("commitments_budget = %s, want 0.0000 (no Category exists yet)", got.CommitmentsBudget)
	}
	if got.Allocations == nil || len(got.Allocations) != 0 {
		t.Errorf("allocations = %#v, want an empty, non-nil list", got.Allocations)
	}
}

func TestAHouseholdsFirstReadMaterialisesItsPeriodAndHasNoMonthlyBudget(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")

	got := f.get(ctx, t)
	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "", "", "")
	if labels := f.markerLabels(t, householdID); labels != "2026-11" {
		t.Errorf("budgets = %q, want exactly the 2026-11 marker", labels)
	}
	if v := f.shape(t, householdID); v != "" {
		t.Errorf("monthly_budgets = %q, want none: the first Period ever copies nothing", v)
	}
}

// A request that finds its Period there writes nothing, so it opens no write
// transaction; its one transaction is the snapshot it reads from, which is
// REPEATABLE READ and READ ONLY (see TestAConcurrentEditCannotTearAGetBudget).
func TestARequestThatFindsThePeriodThereOpensNoWriteTransactionAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.get(ctx, t)
	beforeMarkers := f.markers(t, householdID)
	opened := f.begins.count()
	snapshots := len(f.begins.snapshots())

	f.get(ctx, t)
	f.get(ctx, t)

	if n := f.begins.count(); n != opened {
		t.Errorf("write transactions opened by two reads of an existing Period = %d, want 0", n-opened)
	}
	got := f.begins.snapshots()[snapshots:]
	want := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if len(got) != 2 || got[0] != want || got[1] != want {
		t.Errorf("snapshots opened by two reads = %+v, want two %+v", got, want)
	}
	if after := f.markers(t, householdID); after != beforeMarkers {
		t.Errorf("marker changed by a read:\n%s\n%s", beforeMarkers, after)
	}
}

func TestTheFirstMonthlyBudgetOnDay14IsDatedDay14AndReadAsInForceFromDay1(t *testing.T) {
	f := newFixture(t)
	householdID, _, jakarta := f.member(t, "Home", "a@example.com")
	// A second member whose zone is a day behind at this instant: their Today
	// is 13 Nov, before the version's date.
	losAngeles := f.userInZone(t, householdID, "b@example.com", "America/Los_Angeles")

	got := f.patch(jakarta, t, "9000000")
	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "9000000.0000", "9000000.0000", "9000000.0000")
	if v := f.shape(t, householdID); v != "2026-11 2026-11-14 9000000.0000" {
		t.Errorf("monthly_budgets = %q, want one version dated 2026-11-14", v)
	}

	// Their Today (13 Nov) is before 14 Nov, the first version: it reads as of
	// that one, not as no Budget.
	got = f.get(losAngeles, t)
	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "9000000.0000", "9000000.0000", "9000000.0000")
}

func (f fixture) userInZone(t *testing.T, householdID pgtype.UUID, email, zone string) context.Context {
	t.Helper()
	userID := f.tdb.CreateUser(t, householdID, email, "B")
	f.exec(t, `UPDATE users SET time_zone = $1 WHERE id = $2`, zone, userID)
	return f.ctxOf(t, userID)
}

func TestASecondChangeTheSameDayOverwritesTheFirst(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")

	f.patch(ctx, t, "9000000")
	f.clock.set(t, "2026-11-14T20:00:00+07:00")
	got := f.patch(ctx, t, "9500000")

	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "9500000.0000", "9500000.0000", "9500000.0000")
	if v := f.shape(t, householdID); v != "2026-11 2026-11-14 9500000.0000" {
		t.Errorf("monthly_budgets = %q, want the one version, overwritten", v)
	}
}

// 03:59 and 04:00 are the same calendar day, and different Period Days.
func TestTheSamePeriodDayIsTheDayThatEndsAt0400NotAtMidnight(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")

	f.clock.set(t, "2026-11-14T10:00:00+07:00")
	f.patch(ctx, t, "1000000")
	// 02:00 on the 15th, Jakarta: still the 14th's Period Day.
	f.clock.set(t, "2026-11-15T02:00:00+07:00")
	f.patch(ctx, t, "2000000")
	if v := f.shape(t, householdID); v != "2026-11 2026-11-14 2000000.0000" {
		t.Errorf("after a change at 02:00 the next calendar day = %q, want it to overwrite the 14th", v)
	}
	// 04:00 sharp is the 15th.
	f.clock.set(t, "2026-11-15T04:00:00+07:00")
	f.patch(ctx, t, "3000000")
	if v := f.shape(t, householdID); v != "2026-11 2026-11-14 2000000.0000; 2026-11 2026-11-15 3000000.0000" {
		t.Errorf("after a change at 04:00 = %q, want a new version dated the 15th", v)
	}
}

func TestAChangeOnALaterDayIsANewVersionAndTheEarlierDayIsNotRedrawn(t *testing.T) {
	f := newFixture(t)
	householdID, _, jakarta := f.member(t, "Home", "a@example.com")
	losAngeles := f.userInZone(t, householdID, "b@example.com", "America/Los_Angeles")

	f.clock.set(t, "2026-11-01T10:00:00+07:00")
	f.patch(jakarta, t, "6000000")
	first := f.versions(t, householdID)

	f.clock.set(t, "2026-11-11T10:00:00+07:00")
	got := f.patch(jakarta, t, "7500000")
	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "7500000.0000", "7500000.0000", "7500000.0000")

	rows := f.versions(t, householdID)
	if !strings.HasPrefix(rows, first+"\n") {
		t.Errorf("the first version was rewritten:\nwas  %s\nnow  %s", first, rows)
	}
	if v := f.shape(t, householdID); v != "2026-11 2026-11-01 6000000.0000; 2026-11 2026-11-11 7500000.0000" {
		t.Errorf("monthly_budgets = %q", v)
	}

	// 10:00 on the 11th in Jakarta is 19:00 on the 10th in Los Angeles: their
	// Today is the 10th, the day before the change, and the earlier version is
	// the one in force for them.
	got = f.get(losAngeles, t)
	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "6000000.0000", "6000000.0000", "6000000.0000")
}

func TestAnEmptyPatchMaterialisesAndChangesNothingElse(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")

	got := f.patch(ctx, t, "")
	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "", "", "")
	if labels := f.markerLabels(t, householdID); labels != "2026-11" {
		t.Errorf("budgets = %q, want the marker: an empty PATCH still materialises", labels)
	}
	if v := f.shape(t, householdID); v != "" {
		t.Errorf("monthly_budgets = %q, want none", v)
	}

	f.patch(ctx, t, "5000000")
	before := f.versions(t, householdID)
	got = f.patch(ctx, t, "")
	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "5000000.0000", "5000000.0000", "5000000.0000")
	if after := f.versions(t, householdID); after != before {
		t.Errorf("an empty PATCH changed the log:\n%s\n%s", before, after)
	}
}

// Four decimals on the way out, whatever scale came in; zero is an amount.
func TestEveryAnswerIsAtFourDecimals(t *testing.T) {
	for in, want := range map[string]string{
		"9000000":               "9000000.0000",
		"1.5":                   "1.5000",
		"0.0001":                "0.0001",
		"0":                     "0.0000",
		"0.0":                   "0.0000",
		"00012":                 "12.0000",
		"100.1234":              "100.1234",
		"9999999999999999.9999": "9999999999999999.9999",
		"1000000000000000":      "1000000000000000.0000",
		"12345678901234.5":      "12345678901234.5000",
		"7.10":                  "7.1000",
		"250000000.00":          "250000000.0000",
		"1":                     "1.0000",
		"0.5":                   "0.5000",
		"9007199254740993":      "9007199254740993.0000", // not exact as a float64
		"0.1000":                "0.1000",
		"99999999999999.9999":   "99999999999999.9999",
		"3.1415":                "3.1415",
		"1234567890123456.7890": "1234567890123456.7890",
	} {
		t.Run(in, func(t *testing.T) {
			f := newFixture(t)
			householdID, _, ctx := f.member(t, "Home", "a@example.com")
			got := f.patch(ctx, t, in)
			assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", want, want, want)
			if v := f.shape(t, householdID); v != "2026-11 2026-11-14 "+want {
				t.Errorf("stored %q, want %s", v, want)
			}
			// And the read says the same.
			got = f.get(ctx, t)
			assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", want, want, want)
		})
	}
}

func TestANewPeriodCarriesTheLatestMonthlyBudgetForwardDatedItsFirstDay(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.clock.set(t, "2026-11-01T10:00:00+07:00")
	f.patch(ctx, t, "6000000")
	f.clock.set(t, "2026-11-11T10:00:00+07:00")
	f.patch(ctx, t, "7500000")
	novBefore := f.versions(t, householdID)

	f.clock.set(t, "2026-12-05T10:00:00+07:00")
	got := f.get(ctx, t)

	assertBudget(t, got, "2026-12", "2026-12-01", "2026-12-31", "7500000.0000", "7500000.0000", "7500000.0000")
	if v := f.shape(t, householdID); v != "2026-11 2026-11-01 6000000.0000; 2026-11 2026-11-11 7500000.0000; 2026-12 2026-12-01 7500000.0000" {
		t.Errorf("monthly_budgets = %q: want the latest version copied, dated 2026-12-01", v)
	}
	if !strings.HasPrefix(f.versions(t, householdID), novBefore+"\n") {
		t.Error("the carry-forward rewrote November")
	}

	// An edit in the new Period does not reach back.
	f.patch(ctx, t, "8000000")
	if v := f.shape(t, householdID); v != "2026-11 2026-11-01 6000000.0000; 2026-11 2026-11-11 7500000.0000; 2026-12 2026-12-01 7500000.0000; 2026-12 2026-12-05 8000000.0000" {
		t.Errorf("monthly_budgets = %q", v)
	}
}

func TestACarriedCopyDatedTheFirstDayIsOverwrittenByAChangeThatDay(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.clock.set(t, "2026-11-01T10:00:00+07:00")
	f.patch(ctx, t, "6000000")

	f.clock.set(t, "2026-12-01T10:00:00+07:00")
	f.get(ctx, t)
	f.patch(ctx, t, "6500000")

	if v := f.shape(t, householdID); v != "2026-11 2026-11-01 6000000.0000; 2026-12 2026-12-01 6500000.0000" {
		t.Errorf("monthly_budgets = %q: a change on the first day corrects the copy", v)
	}
}

func TestAPeriodNobodyOpenedIsNotMaterialisedAndTheNextOneCopiesFromTheLastThatExists(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.patch(ctx, t, "6000000")

	f.clock.set(t, "2027-02-10T10:00:00+07:00")
	got := f.get(ctx, t)

	assertBudget(t, got, "2027-02", "2027-02-01", "2027-02-28", "6000000.0000", "6000000.0000", "6000000.0000")
	if labels := f.markerLabels(t, householdID); labels != "2026-11,2027-02" {
		t.Errorf("budgets = %q, want only the two Periods that were opened", labels)
	}
	if v := f.shape(t, householdID); v != "2026-11 2026-11-14 6000000.0000; 2027-02 2027-02-01 6000000.0000" {
		t.Errorf("monthly_budgets = %q", v)
	}
}

// A Period that holds a marker and no Monthly Budget is not a source: the copy
// looks for one that holds a Monthly Budget.
func TestAMarkerOnlyPeriodIsNotASource(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.clock.set(t, "2026-10-10T10:00:00+07:00")
	f.patch(ctx, t, "4000000")
	// November: carried (4M), so it is a source too. Make a marker-only one by
	// inserting directly, as a Period whose copy found nothing would be.
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`, householdID)

	f.clock.set(t, "2026-12-10T10:00:00+07:00")
	got := f.get(ctx, t)

	assertBudget(t, got, "2026-12", "2026-12-01", "2026-12-31", "4000000.0000", "4000000.0000", "4000000.0000")
}

// #130 C3: Today's label can change with period_start_day; every stored row
// stays, and a label with no Budget materialises from the nearest earlier
// Period, else the nearest later one.
func TestChangingPeriodStartDayMovesTheLabelAndKeepsEveryRow(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.clock.set(t, "2026-11-10T10:00:00+07:00")
	f.patch(ctx, t, "6000000")
	novBefore := f.versions(t, householdID)

	f.exec(t, `UPDATE households SET period_start_day = 25 WHERE id = $1`, householdID)
	got := f.get(ctx, t)

	// 10 Nov is now in the Period that opened on 25 Oct. Nothing earlier holds
	// a Monthly Budget, so it copies from the nearest later one: November's.
	assertBudget(t, got, "2026-10", "2026-10-25", "2026-11-24", "6000000.0000", "6000000.0000", "6000000.0000")
	if labels := f.markerLabels(t, householdID); labels != "2026-10,2026-11" {
		t.Errorf("budgets = %q", labels)
	}
	if v := f.shape(t, householdID); v != "2026-10 2026-10-25 6000000.0000; 2026-11 2026-11-10 6000000.0000" {
		t.Errorf("monthly_budgets = %q: want the copy dated the re-cut Period's first day", v)
	}
	if !strings.HasSuffix(f.versions(t, householdID), novBefore) {
		t.Error("November's rows changed with period_start_day")
	}

	// A Period that already exists is never re-materialised, and keeps its
	// older values: an edit of the Period that is now current does not reach it.
	f.patch(ctx, t, "6600000")
	f.exec(t, `UPDATE households SET period_start_day = 1 WHERE id = $1`, householdID)
	got = f.get(ctx, t)
	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "6000000.0000", "6000000.0000", "6000000.0000")
}

func TestARelabelledPeriodPrefersTheNearestEarlierSourceOverALaterOne(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-09'), ($1, '2026-12')`, householdID)
	f.exec(t, `INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES
		($1, '2026-09', '2026-09-01', 1000000), ($1, '2026-09', '2026-09-20', 1100000), ($1, '2026-12', '2026-12-01', 9000000)`, householdID)

	got := f.get(ctx, t) // 14 Nov: 2026-11, which has no row; 2026-09 is the nearest earlier.

	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "1100000.0000", "1100000.0000", "1100000.0000")
}

func TestThePeriodChangesAt0400OnTheLastDayNotAtMidnight(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.clock.set(t, "2026-11-30T10:00:00+07:00")
	f.patch(ctx, t, "6000000")

	// 03:59:59 on 1 Dec, Jakarta, is still 30 Nov.
	f.clock.set(t, "2026-12-01T03:59:59+07:00")
	got := f.get(ctx, t)
	if got.Period.YearMonth != "2026-11" {
		t.Errorf("at 03:59:59 on 1 Dec the Period is %s, want 2026-11", got.Period.YearMonth)
	}
	if labels := f.markerLabels(t, householdID); labels != "2026-11" {
		t.Errorf("budgets = %q: nothing was materialised before the boundary", labels)
	}

	f.clock.set(t, "2026-12-01T04:00:00+07:00")
	got = f.get(ctx, t)
	if got.Period.YearMonth != "2026-12" {
		t.Errorf("at 04:00 on 1 Dec the Period is %s, want 2026-12", got.Period.YearMonth)
	}
}

// The same instant is two Period Days for two Users in two zones, and a User
// who moves is in the Period their new zone says.
func TestTodayFollowsTheCallersOwnTimeZone(t *testing.T) {
	f := newFixture(t)
	householdID, userID, jakarta := f.member(t, "Home", "a@example.com")
	f.clock.set(t, "2026-11-30T21:30:00Z") // 04:30 on 1 Dec in Jakarta, 13:30 on 30 Nov in Los Angeles

	if got := f.get(jakarta, t); got.Period.YearMonth != "2026-12" {
		t.Errorf("Jakarta: Period = %s, want 2026-12", got.Period.YearMonth)
	}
	losAngeles := f.userInZone(t, householdID, "b@example.com", "America/Los_Angeles")
	if got := f.get(losAngeles, t); got.Period.YearMonth != "2026-11" {
		t.Errorf("Los Angeles: Period = %s, want 2026-11", got.Period.YearMonth)
	}

	f.exec(t, `UPDATE users SET time_zone = 'America/Los_Angeles' WHERE id = $1`, userID)
	if got := f.get(f.ctxOf(t, userID), t); got.Period.YearMonth != "2026-11" {
		t.Errorf("Jakarta User after moving to Los Angeles: Period = %s, want 2026-11", got.Period.YearMonth)
	}
}

func TestDayStartsAtMovesTheBoundary(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.exec(t, `UPDATE households SET day_starts_at = '00:00' WHERE id = $1`, householdID)

	f.clock.set(t, "2026-12-01T00:00:00+07:00")
	if got := f.get(ctx, t); got.Period.YearMonth != "2026-12" {
		t.Errorf("with day_starts_at 00:00, midnight on 1 Dec is Period %s, want 2026-12", got.Period.YearMonth)
	}
}

func TestEachHouseholdSeesAndChangesOnlyItsOwnBudget(t *testing.T) {
	f := newFixture(t)
	mine, _, myCtx := f.member(t, "Home", "a@example.com")
	theirs, _, theirCtx := f.member(t, "Other", "b@example.com")

	f.patch(theirCtx, t, "1234567")
	theirMarkers, theirVersions := f.markers(t, theirs), f.versions(t, theirs)

	if got := f.get(myCtx, t); got.MonthlyBudget != nil {
		t.Errorf("a Household with no Budget sees %s from another's", *got.MonthlyBudget)
	}
	// Another Household holding the same label is not this one's Period: the
	// first read still materialises it.
	if labels := f.markerLabels(t, mine); labels != "2026-11" {
		t.Errorf("budgets = %q, want this Household's own 2026-11 marker", labels)
	}
	got := f.patch(myCtx, t, "9000000")
	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "9000000.0000", "9000000.0000", "9000000.0000")
	f.patch(myCtx, t, "9100000")
	f.clock.set(t, "2026-12-03T10:00:00+07:00")
	f.get(myCtx, t) // carries forward: must read only its own Household's Periods
	f.patch(myCtx, t, "")

	if got := f.markers(t, theirs); got != theirMarkers {
		t.Errorf("the other Household's budgets changed:\n%s\n%s", theirMarkers, got)
	}
	if got := f.versions(t, theirs); got != theirVersions {
		t.Errorf("the other Household's Monthly Budget changed:\n%s\n%s", theirVersions, got)
	}
	// The copy into 2026-12 is the caller's own amount, not the other
	// Household's: the source is scoped by household_id.
	if v := f.shape(t, mine); v != "2026-11 2026-11-14 9100000.0000; 2026-12 2026-12-01 9100000.0000" {
		t.Errorf("monthly_budgets = %q", v)
	}
}

// The copy's source must be the caller's own Household's, even when another
// Household holds a nearer Period with a bigger number: a source chosen across
// Households would copy nothing here, or someone else's amount.
func TestTheCarryForwardNeverReadsAnotherHouseholdsPeriods(t *testing.T) {
	f := newFixture(t)
	_, _, myCtx := f.member(t, "Home", "a@example.com")
	_, _, theirCtx := f.member(t, "Other", "b@example.com")
	f.clock.set(t, "2026-09-10T10:00:00+07:00")
	f.patch(myCtx, t, "1000000")
	f.clock.set(t, "2026-10-10T10:00:00+07:00")
	f.patch(theirCtx, t, "99999999")

	f.clock.set(t, "2026-11-10T10:00:00+07:00")
	got := f.get(myCtx, t) // 2026-11 for me: my own September, not their nearer October

	assertBudget(t, got, "2026-11", "2026-11-01", "2026-11-30", "1000000.0000", "1000000.0000", "1000000.0000")
}

// The same for the fallback to a later Period, which only a period_start_day
// change makes reachable.
func TestTheLaterSourceIsAlsoScopedToTheCallersHousehold(t *testing.T) {
	f := newFixture(t)
	mine, _, myCtx := f.member(t, "Home", "a@example.com")
	theirs, _, _ := f.member(t, "Other", "b@example.com")
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-12'), ($2, '2026-11')`, mine, theirs)
	f.exec(t, `INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES
		($1, '2026-12', '2026-12-01', 2000000), ($2, '2026-11', '2026-11-01', 99999999)`, mine, theirs)

	f.clock.set(t, "2026-10-10T10:00:00+07:00")
	got := f.get(myCtx, t) // nothing earlier anywhere: my December, not their nearer November

	assertBudget(t, got, "2026-10", "2026-10-01", "2026-10-31", "2000000.0000", "2000000.0000", "2000000.0000")
}

func TestAConcurrentFirstReadOfOnePeriodMaterialisesOnceAndEveryAnswerAgrees(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.clock.set(t, "2026-10-10T10:00:00+07:00")
	f.patch(ctx, t, "6000000")
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
			resp, err := f.handlers.GetBudget(ctx, api.GetBudgetRequestObject{})
			if err != nil {
				statuses[i] = err.Error()
				return
			}
			rec := httptest.NewRecorder()
			if err := resp.VisitGetBudgetResponse(rec); err != nil {
				statuses[i] = err.Error()
				return
			}
			statuses[i] = fmt.Sprint(rec.Code)
			answers[i] = rec.Body.String()
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
	if !strings.Contains(answers[0], `"monthly_budget":"6000000.0000"`) {
		t.Errorf("answer = %s, want the carried Monthly Budget in every answer", answers[0])
	}
	if labels := f.markerLabels(t, householdID); labels != "2026-10,2026-11" {
		t.Errorf("budgets = %q, want one marker per Period", labels)
	}
	if v := f.shape(t, householdID); v != "2026-10 2026-10-10 6000000.0000; 2026-11 2026-11-01 6000000.0000" {
		t.Errorf("monthly_budgets = %q, want exactly one copy", v)
	}
}

// Repeated, because the window a faulty materialiser leaves open is narrow.
func TestConcurrentFirstReadsAndChangesNeverShowAMarkerWithoutItsCopy(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	f.clock.set(t, "2026-01-10T10:00:00+07:00")
	f.patch(ctx, t, "6000000")

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
				if got.MonthlyBudget == nil || *got.MonthlyBudget != "6000000.0000" {
					mu.Lock()
					problems = append(problems, "an answer without the carried Monthly Budget: "+deref(got.MonthlyBudget))
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
	if labels := f.markerLabels(t, householdID); labels != "2026-01,2026-02,2026-03,2026-04,2026-05,2026-06,2026-07,2026-08,2026-09,2026-10,2026-11,2026-12" {
		t.Errorf("budgets = %q", labels)
	}
	var copies int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM monthly_budgets WHERE household_id = $1`, householdID).Scan(&copies); err != nil {
		t.Fatal(err)
	}
	if copies != 12 {
		t.Errorf("monthly_budgets rows = %d, want 12: one per Period", copies)
	}
}

func TestConcurrentFirstChangesToOnePeriodAllLandAndTheLastWriterWins(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")

	const n = 16
	var wg sync.WaitGroup
	errs := make([]string, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			monthly := fmt.Sprint(1000000 + i)
			resp, err := f.handlers.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{MonthlyBudget: &monthly}})
			if _, ok := resp.(api.UpdateBudget200JSONResponse); err != nil || !ok {
				errs[i] = fmt.Sprintf("%#v %v", resp, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	for i, e := range errs {
		if e != "" {
			t.Fatalf("request %d: %s", i, e)
		}
	}
	if labels := f.markerLabels(t, householdID); labels != "2026-11" {
		t.Errorf("budgets = %q, want one marker", labels)
	}
	var rows int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM monthly_budgets WHERE household_id = $1`, householdID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("monthly_budgets rows = %d, want 1: the same Period Day overwrites", rows)
	}
}

func TestADifferentHouseholdsFirstReadsDoNotWaitOnEachOther(t *testing.T) {
	f := newFixture(t)
	_, _, a := f.member(t, "Home", "a@example.com")
	_, _, b := f.member(t, "Other", "b@example.com")
	var wg sync.WaitGroup
	for _, ctx := range []context.Context{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.get(ctx, t)
		}()
	}
	wg.Wait()
}

// A failure at any step changes nothing: the marker and its copy are one
// transaction.
type failingTx struct {
	pgx.Tx
	failOn     string // a substring of the statement to fail
	failCommit bool
}

var errBoom = errors.New("boom")

func (tx failingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.failOn != "" && strings.Contains(sql, tx.failOn) {
		return pgconn.CommandTag{}, errBoom
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func (tx failingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx.failOn != "" && strings.Contains(sql, tx.failOn) {
		return nil, errBoom
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.failOn != "" && strings.Contains(sql, tx.failOn) {
		return errRow{}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx failingTx) Commit(ctx context.Context) error {
	if tx.failCommit {
		_ = tx.Rollback(ctx)
		return errBoom
	}
	return tx.Tx.Commit(ctx)
}

type errRow struct{}

func (errRow) Scan(...any) error { return errBoom }

type breakingBeginner struct {
	budget.Beginner
	failOn     string
	failCommit bool
	failBegin  bool
}

func (b breakingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	if b.failBegin {
		return nil, errBoom
	}
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failingTx{Tx: tx, failOn: b.failOn, failCommit: b.failCommit}, nil
}

// BeginTx breaks the snapshot read of GET /budget the same way.
func (b breakingBeginner) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if b.failBegin {
		return nil, errBoom
	}
	tx, err := b.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return failingTx{Tx: tx, failOn: b.failOn, failCommit: b.failCommit}, nil
}

// capturedLog collects what the handlers log, so a test can show that a 500
// leaks no amount.
func capturedLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestAFailureWhileMaterialisingLeavesNoMarkerAndAnswers500(t *testing.T) {
	// What a failed GET leaves behind. A failure while the Period is being
	// materialised leaves nothing (the marker and its copy are one transaction);
	// the answer is read after that transaction has committed, so a failure of
	// the read leaves the Period whole: marker and copy, never one without the
	// other.
	const (
		nothing = "nothing"
		whole   = "whole"
	)
	// "whole" rows fail only the read of the answer, which both GET and PATCH
	// make after their writes have committed.
	for _, tc := range []struct {
		name string
		brk  breakingBeginner
		// whether a GET reaches the failing step too, or only a change does
		get bool
		// what a failed GET leaves
		left string
	}{
		{"begin", breakingBeginner{failBegin: true}, true, nothing},
		{"the copy", breakingBeginner{failOn: "FROM monthly_budgets m"}, true, nothing},
		{"the choice of source", breakingBeginner{failOn: "FROM budgets b"}, true, nothing},
		{"the allocation copy", breakingBeginner{failOn: "INSERT INTO allocations"}, true, nothing},
		{"the marker", breakingBeginner{failOn: "INSERT INTO budgets"}, true, nothing},
		{"commit", breakingBeginner{failCommit: true}, true, nothing},
		{"the version read", breakingBeginner{failOn: "ORDER BY effective_on"}, true, whole},
		{"the allocation read", breakingBeginner{failOn: "c.bucket, a.allocation"}, true, whole},
		{"the version", breakingBeginner{failOn: "DO UPDATE SET monthly_budget"}, false, nothing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brk := tc.brk
			f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { brk.Beginner = b; return brk })
			householdID, _, ctx := f.member(t, "Home", "a@example.com")
			// A source Period exists, so the copy has something to copy.
			f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-10')`, householdID)
			f.exec(t, `INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ($1, '2026-10', '2026-10-01', 123456)`, householdID)
			f.allocate(t, householdID, "2026-10", "2026-10-01", f.category(t, householdID, "Rent", "commitment"), "654321")
			logs := capturedLog(t)

			wantMarkers := "2026-10"
			wantMonthly := "2026-10 2026-10-01 123456.0000"
			wantAllocations := "2026-10 2026-10-01 Rent 654321.0000"
			if tc.get && tc.left == whole {
				wantMarkers = "2026-10,2026-11"
				wantMonthly += "; 2026-11 2026-11-01 123456.0000"
				wantAllocations += "; 2026-11 2026-11-01 Rent 654321.0000"
			}
			assertLeft := func(when string) {
				t.Helper()
				if labels := f.markerLabels(t, householdID); labels != wantMarkers {
					t.Errorf("budgets = %q %s, want %q", labels, when, wantMarkers)
				}
				if v := f.shape(t, householdID); v != wantMonthly {
					t.Errorf("monthly_budgets = %q %s, want %q", v, when, wantMonthly)
				}
				if v := f.allocationShape(t, householdID); v != wantAllocations {
					t.Errorf("allocations = %q %s, want %q", v, when, wantAllocations)
				}
			}

			if tc.get {
				resp, err := f.handlers.GetBudget(ctx, api.GetBudgetRequestObject{})
				if err != nil {
					t.Fatalf("GetBudget returned an error: %v", err)
				}
				if got, ok := resp.(api.GetBudget500JSONResponse); !ok || got.Code != api.INTERNAL {
					t.Errorf("GetBudget = %#v, want 500 INTERNAL", resp)
				}
				assertLeft("after a failed read")
			}

			// The same for a change. What it writes is one transaction and never
			// leaves a half; its answer is read after that commits, so a failure
			// of the read is a 500 with the change saved (the maintainer's
			// ruling), and a failure of the write is a 500 with nothing changed.
			uresp := f.patchResp(ctx, t, "9000000")
			if got, ok := uresp.(api.UpdateBudget500JSONResponse); !ok || got.Code != api.INTERNAL {
				t.Errorf("UpdateBudget = %#v, want 500 INTERNAL", uresp)
			}
			if tc.left == whole && tc.get {
				wantMonthly += "; 2026-11 2026-11-14 9000000.0000"
			}
			assertLeft("after a failed change")
			for _, secret := range []string{"123456", "654321", "9000000", "Home", "Rent", "a@example.com"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("the log leaks %q:\n%s", secret, logs.String())
				}
			}
		})
	}
}

// Reading an existing Period needs no write, so everything that can fail is the
// snapshot: the transaction, either of its two statements, or ending it.
func TestAFailureOfTheSnapshotReadIs500(t *testing.T) {
	for _, tc := range []struct {
		name string
		brk  breakingBeginner
	}{
		{"begin", breakingBeginner{failBegin: true}},
		{"the version read", breakingBeginner{failOn: "ORDER BY effective_on"}},
		{"the allocation read", breakingBeginner{failOn: "c.bucket, a.allocation"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brk := tc.brk
			f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { brk.Beginner = b; return brk })
			householdID, _, ctx := f.member(t, "Home", "a@example.com")
			f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`, householdID)
			logs := capturedLog(t)

			resp, err := f.handlers.GetBudget(ctx, api.GetBudgetRequestObject{})
			if err != nil {
				t.Fatalf("GetBudget error: %v", err)
			}
			if got, ok := resp.(api.GetBudget500JSONResponse); !ok || got.Code != api.INTERNAL {
				t.Errorf("GetBudget = %#v, want 500 INTERNAL", resp)
			}
			if strings.Contains(logs.String(), "Home") || strings.Contains(logs.String(), "a@example.com") {
				t.Errorf("the log leaks user content:\n%s", logs.String())
			}
		})
	}
}

// A failure of the version write rolls back the marker and the copy with it.
func TestAFailedChangeOnAFreshPeriodRollsBackItsMarker(t *testing.T) {
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner {
		return breakingBeginner{Beginner: b, failOn: "DO UPDATE SET monthly_budget"}
	})
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	capturedLog(t)

	resp := f.patchResp(ctx, t, "9000000")
	if got, ok := resp.(api.UpdateBudget500JSONResponse); !ok || got.Code != api.INTERNAL {
		t.Fatalf("UpdateBudget = %#v, want 500 INTERNAL", resp)
	}
	if labels := f.markerLabels(t, householdID); labels != "" {
		t.Errorf("budgets = %q: the marker committed without the change it was made for", labels)
	}
}

// A querier that fails: the database outage is a 500, never a 401 or a panic,
// and it logs no amount.
type brokenQuerier struct {
	db.Querier
	household, exists bool
}

func (q brokenQuerier) GetHouseholdByID(ctx context.Context, id pgtype.UUID) (db.Household, error) {
	if q.household {
		return db.Household{}, errBoom
	}
	return q.Querier.GetHouseholdByID(ctx, id)
}

func (q brokenQuerier) BudgetExists(ctx context.Context, arg db.BudgetExistsParams) (bool, error) {
	if q.exists {
		return false, errBoom
	}
	return q.Querier.BudgetExists(ctx, arg)
}

func TestADatabaseFailureIs500(t *testing.T) {
	for _, tc := range []struct {
		name  string
		brk   brokenQuerier
		patch bool // whether a change reaches the failing statement too
	}{
		{"the Household lookup", brokenQuerier{household: true}, true},
		{"the existence check", brokenQuerier{exists: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brk := tc.brk
			f := newFixtureWith(t, func(q db.Querier) db.Querier { brk.Querier = q; return brk }, nil)
			householdID, _, ctx := f.member(t, "Home", "a@example.com")
			// The Period exists already, so a GET writes nothing and the
			// failing statement is the only thing that can fail it.
			f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`, householdID)
			logs := capturedLog(t)

			resp, err := f.handlers.GetBudget(ctx, api.GetBudgetRequestObject{})
			if err != nil {
				t.Fatalf("GetBudget error: %v", err)
			}
			if got, ok := resp.(api.GetBudget500JSONResponse); !ok || got.Code != api.INTERNAL {
				t.Errorf("GetBudget = %#v, want 500 INTERNAL", resp)
			}
			if tc.patch {
				if _, ok := f.patchResp(ctx, t, "1").(api.UpdateBudget500JSONResponse); !ok {
					t.Error("UpdateBudget did not answer 500 INTERNAL")
				}
			}
			if strings.Contains(logs.String(), "Home") || strings.Contains(logs.String(), "a@example.com") {
				t.Errorf("the log leaks user content:\n%s", logs.String())
			}
		})
	}
}

func TestAnUnknownTimeZoneOnTheUserIs500NotAGuess(t *testing.T) {
	f := newFixture(t)
	_, userID, _ := f.member(t, "Home", "a@example.com")
	f.exec(t, `UPDATE users SET time_zone = 'Mars/Olympus_Mons' WHERE id = $1`, userID)
	capturedLog(t)

	resp, err := f.handlers.GetBudget(f.ctxOf(t, userID), api.GetBudgetRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := resp.(api.GetBudget500JSONResponse); !ok || got.Code != api.INTERNAL {
		t.Errorf("GetBudget = %#v, want 500 INTERNAL", resp)
	}
}

func TestWithoutAUserTheAnswerIs401(t *testing.T) {
	f := newFixture(t)
	if resp, _ := f.handlers.GetBudget(context.Background(), api.GetBudgetRequestObject{}); fmt.Sprintf("%T", resp) != "api.GetBudget401JSONResponse" {
		t.Errorf("GetBudget = %T, want 401", resp)
	}
	resp, _ := f.handlers.UpdateBudget(context.Background(), api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{}})
	if fmt.Sprintf("%T", resp) != "api.UpdateBudget401JSONResponse" {
		t.Errorf("UpdateBudget = %T, want 401", resp)
	}
}

func TestANilBodyIsInvalidJSONBody(t *testing.T) {
	f := newFixture(t)
	_, _, ctx := f.member(t, "Home", "a@example.com")
	resp, _ := f.handlers.UpdateBudget(ctx, api.UpdateBudgetRequestObject{})
	if got, ok := resp.(api.UpdateBudget400JSONResponse); !ok || got.Code != api.INVALIDJSONBODY {
		t.Errorf("UpdateBudget = %#v, want 400 INVALID_JSON_BODY", resp)
	}
}

func TestAnAmountThePatternShouldHaveRefusedIsValidationNotAWrite(t *testing.T) {
	f := newFixture(t)
	householdID, _, ctx := f.member(t, "Home", "a@example.com")
	bad := "not a number"
	resp, _ := f.handlers.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{MonthlyBudget: &bad}})
	got, ok := resp.(api.UpdateBudget400JSONResponse)
	if !ok || got.Code != api.VALIDATION || (*got.Args)["field"] != "monthly_budget" || (*got.Args)["rule"] != "pattern" {
		t.Errorf("UpdateBudget = %#v, want 400 VALIDATION monthly_budget pattern", resp)
	}
	if labels := f.markerLabels(t, householdID); labels != "" {
		t.Errorf("budgets = %q: a refused body writes nothing, not even the marker", labels)
	}
}

func TestNewWithoutAClockUsesTheRealOne(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	h := budget.New(budget.Deps{Querier: tdb.Queries, Beginner: tdb.Pool})
	householdID := tdb.CreateHousehold(t, "Home")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "A")
	user, err := tdb.Queries.GetUserByID(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := h.GetBudget(auth.WithUser(context.Background(), user), api.GetBudgetRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := resp.(api.GetBudget200JSONResponse)
	if !ok {
		t.Fatalf("GetBudget = %#v, want 200", resp)
	}
	// Within a day of the real now, whatever the zone: the label is one of the
	// months around it.
	now := time.Now().UTC()
	ok = false
	for _, d := range []time.Time{now.AddDate(0, 0, -2), now, now.AddDate(0, 0, 2)} {
		if got.Period.YearMonth == d.Format("2006-01") {
			ok = true
		}
	}
	if !ok {
		t.Errorf("Period = %s, want the month around %s", got.Period.YearMonth, now.Format("2006-01-02"))
	}
}
