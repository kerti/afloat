package dashboard_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/dashboard"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// The tests here talk to a real Postgres and none is parallel (ADR-0004): they
// share one database that each test empties. The concurrent ones start their own
// goroutines, which is the point of them. Every one pins the clock, so Today is
// asserted on exact dates.

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

// countingBeginner counts the transactions a request opens: a request that
// finds its Period already there must open no write transaction.
type countingBeginner struct {
	budget.Beginner
	mu    sync.Mutex
	begun int
	reads []pgx.TxOptions
}

func (b *countingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	b.mu.Lock()
	b.begun++
	b.mu.Unlock()
	return b.Beginner.Begin(ctx)
}

func (b *countingBeginner) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	b.mu.Lock()
	b.reads = append(b.reads, opts)
	b.mu.Unlock()
	return b.Beginner.BeginTx(ctx, opts)
}

func (b *countingBeginner) writes() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.begun
}

func (b *countingBeginner) snapshots() []pgx.TxOptions {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.reads)
}

// 10:00 on 14 Nov 2026 in Jakarta, the User's zone unless a test moves them.
const day14 = "2026-11-14T10:00:00+07:00"

type fixture struct {
	tdb      *testutil.TestDB
	clock    *clock
	handlers *dashboard.Handlers
	begins   *countingBeginner
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return newFixtureWith(t, nil, nil)
}

// newFixtureWith wraps the real Querier and Beginner, so a test can break or
// watch one call.
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
		handlers: dashboard.New(dashboard.Deps{Querier: q, Beginner: tx, Now: c.now}),
		begins:   counting,
	}
}

// member creates a Household with one User (Asia/Jakarta, the default) and
// returns the Household's id, the User's id and the context of a request that
// User's session produced.
func (f fixture) member(t *testing.T, name, email string) (pgtype.UUID, context.Context) {
	t.Helper()
	householdID := f.tdb.CreateHousehold(t, name)
	userID := f.tdb.CreateUser(t, householdID, email, "A")
	return householdID, f.ctxOf(t, userID)
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

func (f fixture) scan(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
	return s
}

func (f fixture) get(ctx context.Context, t *testing.T) api.Dashboard {
	t.Helper()
	resp, err := f.handlers.GetDashboard(ctx, api.GetDashboardRequestObject{})
	if err != nil {
		t.Fatalf("GetDashboard returned an error: %v", err)
	}
	got, ok := resp.(api.GetDashboard200JSONResponse)
	if !ok {
		t.Fatalf("GetDashboard = %#v, want 200", resp)
	}
	return api.Dashboard(got)
}

// periodRows is every row that bringing a Period into being writes, whole, so
// "nothing was written" is a byte comparison.
func (f fixture) periodRows(t *testing.T, householdID pgtype.UUID) string {
	t.Helper()
	return f.scan(t, `
		SELECT (SELECT coalesce(string_agg(to_jsonb(b)::text, ';' ORDER BY year_month), '') FROM budgets b WHERE household_id = $1) || '|' ||
		       (SELECT coalesce(string_agg(to_jsonb(m)::text, ';' ORDER BY year_month, effective_on), '') FROM monthly_budgets m WHERE household_id = $1) || '|' ||
		       (SELECT coalesce(string_agg(to_jsonb(a)::text, ';' ORDER BY year_month, effective_on, category_id), '') FROM allocations a WHERE household_id = $1)`,
		householdID)
}

// ---- dashboard.json --------------------------------------------------------

type fixtureFile struct {
	Households []fixtureHousehold `json:"households"`
}

type fixtureHousehold struct {
	Name              string `json:"name"`
	PeriodStartDay    int    `json:"period_start_day"`
	ReportingCurrency string `json:"reporting_currency"`
	TimeZone          string `json:"time_zone"`
	DayStartsAt       string `json:"day_starts_at"`
	AllowanceMode     string `json:"allowance_mode"`
	Categories        []struct {
		Key    string `json:"key"`
		Name   string `json:"name"`
		Bucket string `json:"bucket"`
		Active *bool  `json:"active"`
	} `json:"categories"`
	Pockets []struct {
		Key    string `json:"key"`
		Name   string `json:"name"`
		Active *bool  `json:"active"`
	} `json:"pockets"`
	Expenses []struct {
		Key        string `json:"key"`
		OccurredOn string `json:"occurred_on"`
		Amount     string `json:"amount"`
		Pocket     string `json:"pocket"`
		Category   string `json:"category"`
		Deleted    bool   `json:"deleted"`
		CapturedAt string `json:"captured_at"`
	} `json:"expenses"`
	MonthlyBudgets []struct {
		YearMonth     string `json:"year_month"`
		EffectiveOn   string `json:"effective_on"`
		MonthlyBudget string `json:"monthly_budget"`
	} `json:"monthly_budgets"`
	Allocations []struct {
		YearMonth   string `json:"year_month"`
		EffectiveOn string `json:"effective_on"`
		Category    string `json:"category"`
		Allocation  string `json:"allocation"`
	} `json:"allocations"`
	Reads []fixtureRead `json:"reads"`
}

type fixtureRead struct {
	Name     string          `json:"name"`
	Now      string          `json:"now"`
	Expected fixtureExpected `json:"expected"`
}

type fixtureExpected struct {
	Today  string `json:"today"`
	Period struct {
		YearMonth string `json:"year_month"`
		StartsOn  string `json:"starts_on"`
		EndsOn    string `json:"ends_on"`
	} `json:"period"`
	Spend struct {
		ByDay   map[string]string `json:"by_day"`
		Settled string            `json:"settled"`
		Today   string            `json:"today"`
		Future  string            `json:"future"`
	} `json:"spend"`
	// Pool is the Daily Pool the target line is built from: absent while there
	// is no Monthly Budget.
	Pool *struct {
		Today   string `json:"today"`
		Base    string `json:"base"`
		Changes []struct {
			Day  int    `json:"day"`
			Pool string `json:"pool"`
		} `json:"changes"`
	} `json:"pool"`
	// Pace is what the calculation reads against the line (State, Trajectory
	// Variance, Buffer Days): pinned here; State and Variance are on the wire from
	// PR 6 of #155 (Variance not on the Period's first day), Buffer Days never.
	// Absent exactly where the Household is fully committed.
	Pace *struct {
		State              string `json:"state"`
		TrajectoryVariance string `json:"trajectory_variance"`
		BufferDays         string `json:"buffer_days"`
	} `json:"pace"`
	// Wire is what GET /dashboard answers, as the JSON object it is.
	Wire map[string]any `json:"wire"`
}

func loadFixture(t *testing.T) fixtureFile {
	t.Helper()
	raw, err := os.ReadFile("../../../contract/testdata/dashboard.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f fixtureFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(f.Households) == 0 {
		t.Fatal("fixture has no Household: this suite would check nothing")
	}
	for _, h := range f.Households {
		if len(h.Reads) == 0 || len(h.Expenses) == 0 {
			t.Fatalf("fixture Household %q has no reads or no Expenses: this suite would check nothing", h.Name)
		}
	}
	return f
}

// seeded is one fixture Household as it stands in the database.
type seeded struct {
	spec        fixtureHousehold
	householdID pgtype.UUID
	ctx         context.Context
	// categoryKeys and pocketKeys name the ids the seeding minted, by their
	// canonical spelling, so an answer's rows can be read in the fixture's terms.
	categoryKeys, pocketKeys map[string]string
}

// seed writes every fixture Household into the database, side by side, and
// returns them in the file's order. Rows go in directly, as testutil's do, so a
// test of a read needs no handler to have something to read.
func (f fixture) seed(t *testing.T, file fixtureFile) []seeded {
	t.Helper()
	var out []seeded
	for i, h := range file.Households {
		householdID := testutil.NewUUID(t)
		mode := h.AllowanceMode
		if mode == "" {
			mode = "adaptive"
		}
		f.exec(t, `INSERT INTO households (id, household_display_name, reporting_currency, period_start_day, day_starts_at, allowance_mode)
			VALUES ($1, $2, $3, $4, $5::time, $6)`,
			householdID, h.Name, h.ReportingCurrency, h.PeriodStartDay, h.DayStartsAt, mode)
		userID := testutil.NewUUID(t)
		f.exec(t, `INSERT INTO users (id, household_id, email, user_display_name, time_zone) VALUES ($1, $2, $3, 'A', $4)`,
			userID, householdID, "user"+string(rune('a'+i))+"@example.com", h.TimeZone)

		pockets := map[string]pgtype.UUID{}
		pocketKeys, categoryKeys := map[string]string{}, map[string]string{}
		for _, p := range h.Pockets {
			id := testutil.NewUUID(t)
			pockets[p.Key] = id
			pocketKeys[uuidString(id)] = p.Key
			f.exec(t, `INSERT INTO pockets (id, household_id, pocket_display_name, active) VALUES ($1, $2, $3, $4)`,
				id, householdID, p.Name, p.Active == nil || *p.Active)
		}
		categories := map[string]pgtype.UUID{}
		for _, c := range h.Categories {
			id := testutil.NewUUID(t)
			categories[c.Key] = id
			categoryKeys[uuidString(id)] = c.Key
			f.exec(t, `INSERT INTO categories (id, household_id, category_display_name, bucket, active) VALUES ($1, $2, $3, $4, $5)`,
				id, householdID, c.Name, c.Bucket, c.Active == nil || *c.Active)
		}
		periods := map[string]bool{}
		mark := func(yearMonth string) {
			if !periods[yearMonth] {
				periods[yearMonth] = true
				f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, $2)`, householdID, yearMonth)
			}
		}
		for _, m := range h.MonthlyBudgets {
			mark(m.YearMonth)
			f.exec(t, `INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ($1, $2, $3::date, $4::numeric)`,
				householdID, m.YearMonth, m.EffectiveOn, m.MonthlyBudget)
		}
		for _, a := range h.Allocations {
			mark(a.YearMonth)
			f.exec(t, `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, $2, $3::date, $4, $5::numeric)`,
				householdID, a.YearMonth, a.EffectiveOn, categories[a.Category], a.Allocation)
		}
		for _, e := range h.Expenses {
			var category any
			if e.Category != "" {
				category = categories[e.Category]
			}
			var capturedAt any
			if e.CapturedAt != "" {
				capturedAt = e.CapturedAt
			}
			f.exec(t, `INSERT INTO expenses (id, household_id, occurred_on, captured_at, amount, currency, pocket_id, category_id, deleted_at)
				VALUES ($1, $2, $3::date, coalesce($4::timestamptz, now()), $5::numeric, $6, $7, $8, CASE WHEN $9::boolean THEN now() END)`,
				testutil.NewUUID(t), householdID, e.OccurredOn, capturedAt, e.Amount, h.ReportingCurrency, pockets[e.Pocket], category, e.Deleted)
		}
		out = append(out, seeded{spec: h, householdID: householdID, ctx: f.ctxOf(t, userID),
			categoryKeys: categoryKeys, pocketKeys: pocketKeys})
	}
	return out
}

// mustUser is the User behind a seeded Household's context.
func mustUser(t *testing.T, h seeded) db.User {
	t.Helper()
	user, ok := auth.UserFromContext(h.ctx)
	if !ok {
		t.Fatal("the seeded context has no User")
	}
	return user
}

func uuidString(id pgtype.UUID) string {
	return uuid.UUID(id.Bytes).String()
}

// inFixtureTerms rewrites an answer's `categories`, `pockets` and `commitments.items` the way the
// fixture writes them: the id of a row is the key of the Category or Pocket it
// was seeded as (`category`, `pocket`), since the ids are minted at seeding.
// An id the seeding did not mint is left as it is, and fails the comparison.
func (h seeded) inFixtureTerms(wire map[string]any) {
	rewrite := func(rows []any, idField, keyField string, keys map[string]string) {
		for _, r := range rows {
			row, _ := r.(map[string]any)
			if id, ok := row[idField].(string); ok {
				if key, known := keys[id]; known {
					delete(row, idField)
					row[keyField] = key
				}
			}
		}
	}
	categories, _ := wire["categories"].([]any)
	rewrite(categories, "category_id", "category", h.categoryKeys)
	pockets, _ := wire["pockets"].([]any)
	rewrite(pockets, "pocket_id", "pocket", h.pocketKeys)
	if commitments, ok := wire["commitments"].(map[string]any); ok {
		items, _ := commitments["items"].([]any)
		rewrite(items, "category_id", "category", h.categoryKeys)
	}
}
