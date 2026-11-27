package dashboard_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/dashboard"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// ---- first read ------------------------------------------------------------

// A Household that has never read anything has no Period yet: the first read of
// the dashboard brings it into being, as GET /budget's does (PRD S13), and
// answers the currency. Nothing else is written.
func TestAHouseholdsFirstReadMaterialisesItsPeriodAndAnswersItsCurrency(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")

	got := f.get(ctx, t)

	if got.Currency != "IDR" {
		t.Errorf("currency = %q, want IDR", got.Currency)
	}
	if got := f.scan(t, `SELECT string_agg(year_month, ',') FROM budgets WHERE household_id = $1`, householdID); got != "2026-11" {
		t.Errorf("budgets = %q, want exactly the 2026-11 marker", got)
	}
	if got := f.scan(t, `SELECT count(*)::text FROM monthly_budgets WHERE household_id = $1`, householdID); got != "0" {
		t.Errorf("monthly_budgets rows = %s, want none: the first Period ever copies nothing", got)
	}
}

// The Budget a Household carried forward is the Budget the dashboard's first
// read of the new Period carries: it is the same sequence GET /budget runs
// (budget.EnsurePeriod), so a Household is never told by Home that it has none
// when the plan screen would show it.
func TestTheFirstReadOfANewPeriodCarriesTheBudgetForwardAsGetBudgetDoes(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	rent := f.tdb.CreateCategory(t, householdID, "Rent", "commitment")
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-10')`, householdID)
	f.exec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-10', '2026-10-10', 6000000)`, householdID)
	f.exec(t, `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, '2026-10', '2026-10-10', $2, 2000000)`, householdID, rent)

	f.get(ctx, t)

	afterDashboard := f.periodRows(t, householdID)
	if got := f.scan(t, `SELECT monthly_budget::text || ' ' || effective_on::text FROM monthly_budgets WHERE household_id = $1 AND year_month = '2026-11'`, householdID); got != "6000000.0000 2026-11-01" {
		t.Errorf("the copied Monthly Budget = %q, want 6000000.0000 dated the Period's first day", got)
	}
	if got := f.scan(t, `SELECT allocation::text || ' ' || effective_on::text FROM allocations WHERE household_id = $1 AND year_month = '2026-11'`, householdID); got != "2000000.0000 2026-11-01" {
		t.Errorf("the copied allocation = %q, want 2000000.0000 dated the Period's first day", got)
	}

	// The same state reached by GET /budget instead: the rows are identical,
	// ids and timestamps aside (the copied rows carry the same values either way).
	budgetHandlers := budget.New(budget.Deps{Querier: f.tdb.Queries, Beginner: f.tdb.Pool, Now: f.clock.now})
	f.exec(t, `DELETE FROM allocations WHERE household_id = $1 AND year_month = '2026-11'`, householdID)
	f.exec(t, `DELETE FROM monthly_budgets WHERE household_id = $1 AND year_month = '2026-11'`, householdID)
	f.exec(t, `DELETE FROM budgets WHERE household_id = $1 AND year_month = '2026-11'`, householdID)
	if _, err := budgetHandlers.GetBudget(ctx, api.GetBudgetRequestObject{}); err != nil {
		t.Fatalf("GetBudget: %v", err)
	}
	if afterBudget := f.periodRows(t, householdID); stripStamps(afterBudget) != stripStamps(afterDashboard) {
		t.Errorf("GET /budget left\n%s\nthe dashboard left\n%s", afterBudget, afterDashboard)
	}
}

// stripStamps drops the timestamps of a periodRows dump, which differ between
// two runs of the same write.
func stripStamps(s string) string {
	var out []string
	for _, part := range strings.Split(s, `"`) {
		if strings.Contains(part, "T") && strings.Contains(part, ":") && strings.Contains(part, "-") && len(part) > 20 {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, `"`)
}

// A request that finds its Period there writes nothing, so it opens no write
// transaction; its one transaction is the snapshot it reads from, which is
// REPEATABLE READ and READ ONLY.
func TestARequestThatFindsThePeriodThereOpensNoWriteTransactionAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	f.get(ctx, t)
	before := f.periodRows(t, householdID)
	writes, snapshots := f.begins.writes(), len(f.begins.snapshots())

	f.get(ctx, t)
	f.get(ctx, t)

	if n := f.begins.writes(); n != writes {
		t.Errorf("write transactions opened by two reads of an existing Period = %d, want 0", n-writes)
	}
	got := f.begins.snapshots()[snapshots:]
	want := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if len(got) != 2 || got[0] != want || got[1] != want {
		t.Errorf("snapshots opened by two reads = %+v, want two %+v", got, want)
	}
	if after := f.periodRows(t, householdID); after != before {
		t.Errorf("rows changed by a read:\n%s\n%s", before, after)
	}
}

// Home is the first read of every Period, so the first read after a boundary
// is a dashboard's, and many of them may arrive together (two members, two
// tabs), alongside a Budget change on the same new Period (both call
// budget.EnsurePeriod, as #130's recipe mixes them). One Period, one copy, one
// answer, whoever wins. Repeated over twelve
// Periods because the window a faulty materialiser leaves open is narrow, and
// checked for a marker without its copy the moment each answer comes back.
func TestConcurrentFirstReadsOfOnePeriodLeaveOnePeriodAndNeverAMarkerWithoutItsCopy(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-01')`, householdID)
	f.exec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-01', '2026-01-10', 6000000)`, householdID)
	budgets := budget.New(budget.Deps{Querier: f.tdb.Queries, Beginner: f.tdb.Pool, Now: f.clock.now})

	for month := 2; month <= 12; month++ {
		f.clock.set(t, fmt.Sprintf("2026-%02d-10T10:00:00+07:00", month))
		label := fmt.Sprintf("2026-%02d", month)
		wantRemaining := time.Date(2026, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day() - 9 // the 10th: nine days elapsed
		var wg sync.WaitGroup
		var mu sync.Mutex
		var problems []string
		fail := func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			problems = append(problems, fmt.Sprintf(format, args...))
		}
		start := make(chan struct{})
		for i := range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if i%3 == 0 {
					resp, err := budgets.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{}})
					if _, ok := resp.(api.UpdateBudget200JSONResponse); err != nil || !ok {
						fail("patch: %#v %v", resp, err)
					}
					return
				}
				resp, err := f.handlers.GetDashboard(ctx, api.GetDashboardRequestObject{})
				rec := httptest.NewRecorder()
				if err != nil {
					fail("get: %v", err)
					return
				}
				// The Budget carried forward is read: 6.000.000 on day 10, so the
				// pool of the Period is whole and nothing is spent.
				var got api.Dashboard
				if err := resp.VisitGetDashboardResponse(rec); err != nil || rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil ||
					got.Currency != "IDR" || got.Trajectory == nil || got.Trajectory.RemainingDailyPool != "6000000.0000" || got.Trajectory.DaysRemaining != wantRemaining {
					fail("get: %d %q %v, want 6000000.0000 left over %d days", rec.Code, rec.Body.String(), err, wantRemaining)
					return
				}
				// The answer is back, so the Period is committed whole: a marker
				// without its copy is a Period that looks materialised and is
				// empty.
				var markers, copies int
				if err := f.tdb.Pool.QueryRow(context.Background(),
					`SELECT (SELECT count(*) FROM budgets WHERE household_id = $1 AND year_month = $2),
					        (SELECT count(*) FROM monthly_budgets WHERE household_id = $1 AND year_month = $2)`,
					householdID, label).Scan(&markers, &copies); err != nil {
					fail("count: %v", err)
					return
				}
				if markers != 1 || copies != 1 {
					fail("after an answer: %d markers and %d copies of %s, want 1 and 1", markers, copies, label)
				}
			}()
		}
		close(start)
		wg.Wait()
		if len(problems) > 0 {
			t.Fatalf("month %d: %v", month, problems)
		}
	}
	if got := f.scan(t, `SELECT string_agg(year_month, ',' ORDER BY year_month) FROM budgets WHERE household_id = $1`, householdID); got !=
		"2026-01,2026-02,2026-03,2026-04,2026-05,2026-06,2026-07,2026-08,2026-09,2026-10,2026-11,2026-12" {
		t.Errorf("budgets = %q, want one marker per Period", got)
	}
	if got := f.scan(t, `SELECT count(*)::text FROM monthly_budgets WHERE household_id = $1`, householdID); got != "12" {
		t.Errorf("monthly_budgets rows = %s, want 12: one per Period", got)
	}
}

// Two Households' first reads do not touch each other's Period: the marker is
// each one's own.
func TestADifferentHouseholdsFirstReadDoesNotMaterialiseThisOnesPeriod(t *testing.T) {
	f := newFixture(t)
	mine, myCtx := f.member(t, "Mine", "a@example.com")
	theirs, _ := f.member(t, "Theirs", "b@example.com")

	f.get(myCtx, t)

	if rows := f.periodRows(t, theirs); rows != "||" {
		t.Errorf("the other Household's rows = %q, want none: a read writes only its own Household's Period", rows)
	}
	if rows := f.periodRows(t, mine); rows == "||" {
		t.Error("this Household's Period was not materialised")
	}
}

// ---- the snapshot ----------------------------------------------------------

// hookBeginner runs fn once, just before the snapshot's before-th statement
// (the first when before is 0): after the Period was ensured and the Position
// worked out, and between two statements of the answer when before is later.
// Until fn is set it does nothing.
type hookBeginner struct {
	budget.Beginner
	mu     sync.Mutex
	fn     func()
	before int
	done   bool
}

func (b *hookBeginner) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &hookTx{Tx: tx, owner: b}, nil
}

type hookTx struct {
	pgx.Tx
	owner *hookBeginner
	seen  int
}

func (tx *hookTx) fire() {
	tx.seen++
	tx.owner.mu.Lock()
	run := tx.owner.fn != nil && !tx.owner.done && tx.seen >= max(tx.owner.before, 1)
	if run {
		tx.owner.done = true
	}
	fn := tx.owner.fn
	tx.owner.mu.Unlock()
	if run {
		fn()
	}
}

func (tx *hookTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.fire()
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx *hookTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.fire()
	return tx.Tx.Query(ctx, sql, args...)
}

// What the answer says about the Household is read in the snapshot, which is
// opened after the Period is ensured, and not carried from the read that
// worked out Today before it. A Household whose currency changes between the
// two (permitted while it has no Expense) is answered in the currency it has
// when the snapshot is taken, so the figures and the currency stating them are
// read together.
func TestTheCurrencyIsReadInTheSnapshotNotBeforeThePeriodWasEnsured(t *testing.T) {
	hook := &hookBeginner{}
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { hook.Beginner = b; return hook })
	householdID, ctx := f.member(t, "Home", "a@example.com")
	hook.fn = func() {
		f.exec(t, `UPDATE households SET reporting_currency = 'USD' WHERE id = $1`, householdID)
	}

	got := f.get(ctx, t)

	if got.Currency != "USD" {
		t.Errorf("currency = %q: the answer was stated in the currency read before the snapshot, want USD, the one in force when it was read", got.Currency)
	}
	if !hook.done {
		t.Error("the hook never ran: the read did not go through the snapshot")
	}
}

// answerOf is the dashboard's whole answer as bytes: what two reads must
// agree on to be one answer.
func (f fixture) answerOf(ctx context.Context, t *testing.T) string {
	t.Helper()
	resp, err := f.handlers.GetDashboard(ctx, api.GetDashboardRequestObject{})
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitGetDashboardResponse(rec); err != nil || rec.Code != 200 {
		t.Fatalf("GetDashboard = %d %q %v, want 200", rec.Code, rec.Body.String(), err)
	}
	return rec.Body.String()
}

// seedTearHousehold is a Household whose answer a single write can change in
// two of the statements that make it: a Monthly Budget of 3.000.000 and one
// Expense of 100.000, read on the 14th.
func (f fixture) seedTearHousehold(t *testing.T) (householdID, pocket pgtype.UUID, ctx context.Context) {
	t.Helper()
	householdID, ctx = f.member(t, "Home", "a@example.com")
	pocket = f.tdb.CreatePocket(t, householdID, "Wallet")
	f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`, householdID)
	f.exec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-11', '2026-11-01', 3000000)`, householdID)
	f.expense(t, householdID, pocket, pgtype.UUID{}, "2026-11-05", "100000")
	return householdID, pocket, ctx
}

// A write that commits between the answer's statements is in the answer whole
// or not at all. The write is one transaction that moves the Monthly Budget
// (read by the answer's second statement) and adds an Expense (read by its
// last), so a read that sees the first as it was and the second as it became
// is a Budget that never existed with spending it never had. The snapshot is
// taken at the first statement, so the answer is the state before the write.
// Proven red with the snapshot at READ COMMITTED: the answer was 6 days of a
// Budget and an Expense no stored state ever held together.
func TestAWriteCommittedBetweenTheStatementsIsInTheAnswerWholeOrNotAtAll(t *testing.T) {
	// The write lands before the third statement (Assemble's first
	// AllocationsAsOf): after the versions are read, before the spend. Reorder
	// Assemble's statements and this index must move with them.
	hook := &hookBeginner{before: 3}
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { hook.Beginner = b; return hook })
	householdID, pocket, ctx := f.seedTearHousehold(t)

	before := f.answerOf(ctx, t)

	hook.fn = func() {
		tx, err := f.tdb.Pool.Begin(context.Background())
		if err != nil {
			t.Errorf("begin: %v", err)
			return
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		for _, sql := range []string{
			`INSERT INTO monthly_budgets VALUES ($1, '2026-11', '2026-11-14', 6000000)`,
			`INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id) VALUES (gen_random_uuid(), $1, '2026-11-10', 500000, 'IDR', $2)`,
		} {
			args := []any{householdID}
			if strings.Contains(sql, "$2") {
				args = append(args, pocket)
			}
			if _, err := tx.Exec(context.Background(), sql, args...); err != nil {
				t.Errorf("%s: %v", sql, err)
				return
			}
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Errorf("commit: %v", err)
		}
	}
	during := f.answerOf(ctx, t)
	if !hook.done {
		t.Fatal("the write never ran between the statements")
	}
	after := f.answerOf(ctx, t)

	if after == before {
		t.Fatalf("the write changed nothing a read can see (%s): this test would prove nothing", after)
	}
	if during != before && during != after {
		t.Errorf("the answer is neither state of the Household:\n before %s\n during %s\n after  %s", before, during, after)
	}
	if during != before {
		t.Errorf("the answer saw the write the snapshot began before:\n before %s\n during %s", before, during)
	}
}

// A setting that moves the window (period_start_day) edited after the Position
// was worked out is not in the answer: every figure is keyed by the one Period
// the request located, so the answer is the one a request a moment earlier
// would have given, never the old Period's Budget over the new window's
// Expenses. Checked with the edit before the snapshot's first statement and
// between two of its statements.
func TestAPeriodStartDayEditedAfterThePositionWasWorkedOutDoesNotTearTheAnswer(t *testing.T) {
	for _, before := range []int{1, 3} {
		t.Run(fmt.Sprintf("before statement %d", before), func(t *testing.T) {
			hook := &hookBeginner{before: before}
			f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { hook.Beginner = b; return hook })
			householdID, pocket, ctx := f.seedTearHousehold(t)
			// Spend that the new window (20 Oct to 19 Nov) would count and the old
			// (1 to 30 Nov) does not: 28 Oct.
			f.expense(t, householdID, pocket, pgtype.UUID{}, "2026-10-28", "250000")

			beforeEdit := f.answerOf(ctx, t)

			hook.fn = func() {
				f.exec(t, `UPDATE households SET period_start_day = 20 WHERE id = $1`, householdID)
			}
			during := f.answerOf(ctx, t)
			if !hook.done {
				t.Fatal("the edit never ran after the Position was worked out")
			}
			afterEdit := f.answerOf(ctx, t)

			if afterEdit == beforeEdit {
				t.Fatalf("re-cutting the window changed nothing a read can see (%s): this test would prove nothing", afterEdit)
			}
			if during != beforeEdit {
				t.Errorf("the answer is not the one for the Period the request located:\n before the edit %s\n during         %s\n after the edit  %s", beforeEdit, during, afterEdit)
			}
		})
	}
}

// ---- failures --------------------------------------------------------------

var errBoom = errors.New("boom")

func capturedLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

type failingQuerier struct {
	db.Querier
	failHousehold bool
	failExists    bool
}

func (q failingQuerier) GetHouseholdByID(ctx context.Context, id pgtype.UUID) (db.Household, error) {
	if q.failHousehold {
		return db.Household{}, errBoom
	}
	return q.Querier.GetHouseholdByID(ctx, id)
}

func (q failingQuerier) BudgetExists(ctx context.Context, arg db.BudgetExistsParams) (bool, error) {
	if q.failExists {
		return false, errBoom
	}
	return q.Querier.BudgetExists(ctx, arg)
}

type failingBeginner struct {
	budget.Beginner
	failBegin   bool // the write transaction that materialises
	failBeginTx bool // the snapshot
}

func (b failingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	if b.failBegin {
		return nil, errBoom
	}
	return b.Beginner.Begin(ctx)
}

func (b failingBeginner) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if b.failBeginTx {
		return nil, errBoom
	}
	return b.Beginner.BeginTx(ctx, opts)
}

// Every failure is 500 INTERNAL, logs the path and the error and never a name,
// and a failed materialising leaves no marker (the marker and its copy are one
// transaction).
func TestAFailureAtAnyStepIs500AndLeaksNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		wrapQ func(db.Querier) db.Querier
		wrapT func(budget.Beginner) budget.Beginner
	}{
		{"looking up the Household", func(q db.Querier) db.Querier { return failingQuerier{Querier: q, failHousehold: true} }, nil},
		{"looking up the Period", func(q db.Querier) db.Querier { return failingQuerier{Querier: q, failExists: true} }, nil},
		{"materialising the Period", nil, func(b budget.Beginner) budget.Beginner { return failingBeginner{Beginner: b, failBegin: true} }},
		{"opening the snapshot", nil, func(b budget.Beginner) budget.Beginner { return failingBeginner{Beginner: b, failBeginTx: true} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, tc.wrapQ, tc.wrapT)
			householdID, ctx := f.member(t, "Secret Household Name", "secret@example.com")
			logs := capturedLog(t)

			resp, err := f.handlers.GetDashboard(ctx, api.GetDashboardRequestObject{})

			if got, ok := resp.(api.GetDashboard500JSONResponse); err != nil || !ok || got.Code != api.INTERNAL {
				t.Fatalf("GetDashboard = %#v %v, want 500 INTERNAL", resp, err)
			}
			for _, secret := range []string{"Secret Household Name", "secret@example.com"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("the log leaks %q:\n%s", secret, logs.String())
				}
			}
			if tc.name == "materialising the Period" {
				if rows := f.periodRows(t, householdID); rows != "||" {
					t.Errorf("rows = %q after a failed materialising, want none", rows)
				}
			}
		})
	}
}

// A failure of the snapshot after the Period was committed is 500, the Period
// stands (it is what the next read would have made) and a repeat answers.
func TestARepeatAfterAFailedSnapshotAnswers(t *testing.T) {
	f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { return failingBeginner{Beginner: b, failBeginTx: true} })
	householdID, ctx := f.member(t, "Home", "a@example.com")
	capturedLog(t)
	if resp, _ := f.handlers.GetDashboard(ctx, api.GetDashboardRequestObject{}); resp == nil {
		t.Fatal("no response")
	}
	if rows := f.periodRows(t, householdID); rows == "||" {
		t.Error("the Period was not committed before the snapshot failed")
	}

	healthy := dashboard.New(dashboard.Deps{Querier: f.tdb.Queries, Beginner: f.tdb.Pool, Now: f.clock.now})
	resp, err := healthy.GetDashboard(ctx, api.GetDashboardRequestObject{})
	if got, ok := resp.(api.GetDashboard200JSONResponse); err != nil || !ok || got.Currency != "IDR" {
		t.Fatalf("the repeat = %#v %v, want 200 IDR", resp, err)
	}
}

func TestAnUnknownTimeZoneOnTheUserIs500NotAGuess(t *testing.T) {
	f := newFixture(t)
	householdID, _ := f.member(t, "Home", "a@example.com")
	f.exec(t, `UPDATE users SET time_zone = 'Mars/Olympus' WHERE household_id = $1`, householdID)
	ctx := f.ctxOf(t, userOf(t, f, householdID))
	capturedLog(t)

	resp, err := f.handlers.GetDashboard(ctx, api.GetDashboardRequestObject{})

	if got, ok := resp.(api.GetDashboard500JSONResponse); err != nil || !ok || got.Code != api.INTERNAL {
		t.Fatalf("GetDashboard = %#v %v, want 500 INTERNAL", resp, err)
	}
	if rows := f.periodRows(t, householdID); rows != "||" {
		t.Errorf("rows = %q, want none: no Period is made for a Today nobody could work out", rows)
	}
}

func userOf(t *testing.T, f fixture, householdID pgtype.UUID) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT id FROM users WHERE household_id = $1`, householdID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestWithoutAUserTheAnswerIs401(t *testing.T) {
	f := newFixture(t)
	resp, err := f.handlers.GetDashboard(context.Background(), api.GetDashboardRequestObject{})
	if got, ok := resp.(api.GetDashboard401JSONResponse); err != nil || !ok || got.Code != api.UNAUTHORIZED {
		t.Errorf("GetDashboard = %#v %v, want 401 UNAUTHORIZED", resp, err)
	}
}

func TestNewWithoutAClockUsesTheRealOne(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	householdID := tdb.CreateHousehold(t, "Home")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "A")
	user, err := tdb.Queries.GetUserByID(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	h := dashboard.New(dashboard.Deps{Querier: tdb.Queries, Beginner: tdb.Pool})

	resp, err := h.GetDashboard(auth.WithUser(context.Background(), user), api.GetDashboardRequestObject{})

	if got, ok := resp.(api.GetDashboard200JSONResponse); err != nil || !ok || got.Currency != "IDR" {
		t.Errorf("GetDashboard = %#v %v, want 200 IDR", resp, err)
	}
}
