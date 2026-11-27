package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/dashboard"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/household"
	"github.com/kerti/afloat/backend/internal/system"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// dashboardFixture is the whole stack over a real database, with one signed-in
// User and a pinned clock (10:00 on 14 Nov 2026 in Jakarta), so GET /dashboard
// is exercised through the session check, the spec validator and the generated
// binding rather than as a bare handler.
type dashboardFixture struct {
	tdb         *testutil.TestDB
	srv         http.Handler
	householdID pgtype.UUID
	cookie      string
}

func newDashboardFixture(t *testing.T) dashboardFixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	householdID := tdb.CreateHousehold(t, "Home")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "A")

	const token = "a-session-token-for-the-dashboard-tests"
	if _, err := tdb.Queries.CreateSession(context.Background(), db.CreateSessionParams{
		ID:        auth.HashToken(token),
		UserID:    userID,
		ExpiresAt: pgtypeTimestamptz(time.Now().Add(24 * time.Hour)),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	now, err := time.Parse(time.RFC3339, "2026-11-14T10:00:00+07:00")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Deps{
		System: system.New(system.Deps{Querier: tdb.Queries, Version: "test", LocalEnabled: true}),
		Auth: auth.New(auth.Deps{
			Querier:            tdb.Queries,
			Beginner:           tdb.Pool,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
		Household:      household.New(household.Deps{Querier: tdb.Queries, Beginner: tdb.Pool}),
		Dashboard:      dashboard.New(dashboard.Deps{Querier: tdb.Queries, Beginner: tdb.Pool, Now: func() time.Time { return now }}),
		HandlerTimeout: testHandlerTimeout,
	})
	return dashboardFixture{tdb: tdb, srv: srv, householdID: householdID, cookie: auth.SessionCookieName + "=" + token}
}

func (f dashboardFixture) do(method string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/dashboard", strings.NewReader(""))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Cookie", f.cookie)
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

// markers is the Household's Budget Period markers, whole rows.
func (f dashboardFixture) markers(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(year_month, ',' ORDER BY year_month), '') FROM budgets WHERE household_id = $1`, f.householdID).Scan(&s); err != nil {
		t.Fatalf("read budgets: %v", err)
	}
	return s
}

// The answer at the first PR of #155: the currency and nothing else. Each later
// PR's answer must be a strict superset of the one before it (#155 R5: no PR
// removes what an earlier one added), so this golden is only ever extended by
// properties, never edited into another shape.
const dashboardAnswerPR1 = `{"currency":"IDR"}`

// The answer at PR 3 of #155 for a Household with a Monthly Budget of 3.000.000
// from the Period's first day and no spending, read on 14 Nov 2026: PR 1's
// answer plus the `trajectory`. Every money is four decimals and nothing the
// merged PRs do not render (State, Trajectory Variance, Buffer Days, a day
// count other than days_remaining) is on it.
const dashboardAnswerPR3 = `{"currency":"IDR","trajectory":{"baseline_daily_budget":"100000.0000","days_remaining":17,"left_to_spend_today":"176470.5882","remaining_daily_pool":"3000000.0000","todays_allowance":"176470.5882"}}`

func TestGetDashboardThroughTheRouterAnswersTheCurrencyAndNothingElse(t *testing.T) {
	f := newDashboardFixture(t)

	rec := f.do(http.MethodGet)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != dashboardAnswerPR1 {
		t.Errorf("body = %s, want %s", got, dashboardAnswerPR1)
	}
}

// HEAD of a Household's first read of a Period materialises it, as GET does:
// RFC 9110 §9.1 has HEAD answer what GET would, and a GET that writes once per
// Period is a HEAD that does. The status and headers are GET's; the body is
// empty.
func TestAFirstHeadMaterialisesThePeriodLikeAFirstGet(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			f := newDashboardFixture(t)
			if got := f.markers(t); got != "" {
				t.Fatalf("budgets = %q before any read, want none", got)
			}

			rec := f.do(method)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			if got := f.markers(t); got != "2026-11" {
				t.Errorf("budgets = %q after the first %s, want the 2026-11 marker with no other write", got, method)
			}
			if method == http.MethodHead && rec.Body.Len() != 0 {
				t.Errorf("HEAD body = %q, want empty", rec.Body.String())
			}
			if method == http.MethodHead && rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("HEAD Content-Type = %q, want GET's", rec.Header().Get("Content-Type"))
			}
		})
	}
}

// GET and HEAD arriving together on a Period nobody has read leave one Period
// and answer alike.
func TestConcurrentFirstGetsAndHeadsThroughTheRouterLeaveOnePeriod(t *testing.T) {
	f := newDashboardFixture(t)
	const n = 24
	codes := make([]int, n)
	bodies := make([]string, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			method := http.MethodGet
			if i%2 == 1 {
				method = http.MethodHead
			}
			rec := f.do(method)
			codes[i], bodies[i] = rec.Code, strings.TrimSpace(rec.Body.String())
		}()
	}
	close(start)
	wg.Wait()

	for i := range n {
		want := dashboardAnswerPR1
		if i%2 == 1 {
			want = ""
		}
		if codes[i] != http.StatusOK || bodies[i] != want {
			t.Fatalf("request %d answered %d %q, want 200 %q", i, codes[i], bodies[i], want)
		}
	}
	if got := f.markers(t); got != "2026-11" {
		t.Errorf("budgets = %q, want one marker", got)
	}
}

// Without a session the answer is 401 and nothing is written, for both methods.
func TestTheDashboardWithoutASessionIs401AndWritesNothing(t *testing.T) {
	f := newDashboardFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := f.do(method, func(r *http.Request) { r.Header.Del("Cookie") })
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a cookie = %d, want 401", method, rec.Code)
		}
		if method == http.MethodGet && !strings.Contains(rec.Body.String(), `"UNAUTHORIZED"`) {
			t.Errorf("body = %s, want the UNAUTHORIZED envelope", rec.Body.String())
		}
	}
	if got := f.markers(t); got != "" {
		t.Errorf("budgets = %q, want none: an unauthenticated read writes nothing", got)
	}
}

// Only the reads are in the contract: nothing writes a dashboard.
func TestTheDashboardAnswersOnlyReads(t *testing.T) {
	f := newDashboardFixture(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/dashboard", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Cookie", f.cookie)
		rec := httptest.NewRecorder()
		f.srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/dashboard = %d, want 405", method, rec.Code)
		}
	}
	if got := f.markers(t); got != "" {
		t.Errorf("budgets = %q, want none: a refused method writes nothing", got)
	}
}

// Each PR's answer is a strict superset of the one before it (#155 R5: a
// property joins the wire in the PR that renders it, and none is removed). PR 1's
// golden is the answer of a Household with no Monthly Budget, which stays
// currency alone; once it has one, the same answer gains `trajectory` and loses
// nothing, and money is a four-decimals string, never a number or an exponent.
func TestTheAnswerWithAMonthlyBudgetIsPR1sAnswerPlusTheTrajectory(t *testing.T) {
	f := newDashboardFixture(t)
	f.mustExec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`)
	f.mustExec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-11', '2026-11-01', 3000000)`)

	rec := f.do(http.MethodGet)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	got := strings.TrimSpace(rec.Body.String())
	if got != dashboardAnswerPR3 {
		t.Errorf("body = %s\nwant %s", got, dashboardAnswerPR3)
	}

	var older, newer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(dashboardAnswerPR1), &older); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got), &newer); err != nil {
		t.Fatal(err)
	}
	for key, value := range older {
		if string(newer[key]) != string(value) {
			t.Errorf("PR 1's %q = %s, PR 3's = %s: an earlier answer's property was changed or removed", key, value, newer[key])
		}
	}
	var trajectory map[string]any
	if err := json.Unmarshal(newer["trajectory"], &trajectory); err != nil {
		t.Fatal(err)
	}
	money := regexp.MustCompile(`^-?[0-9]{1,16}\.[0-9]{4}$`)
	for key, value := range trajectory {
		switch key {
		case "days_remaining":
			if _, ok := value.(float64); !ok {
				t.Errorf("days_remaining = %#v, want an integer", value)
			}
		case "left_to_spend_today", "todays_allowance", "remaining_daily_pool", "baseline_daily_budget":
			if s, ok := value.(string); !ok || !money.MatchString(s) {
				t.Errorf("%s = %#v, want a money string with four decimals", key, value)
			}
		default:
			t.Errorf("trajectory carries %q: not rendered by any merged PR of #155, so not on the wire", key)
		}
		if value == nil {
			t.Errorf("%s is null: an absent property is absent", key)
		}
	}
}

// The setting reaches the figure: after PATCH /household sets `fixed`, the next
// read answers Today's Allowance as the Baseline Daily Budget (CONTEXT: a fixed
// allowance is the baseline in force), and `adaptive` restores the recalculated
// one. Nothing is cached between the two.
func TestPatchingTheAllowanceModeChangesTheNextDashboardRead(t *testing.T) {
	f := newDashboardFixture(t)
	f.mustExec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`)
	f.mustExec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-11', '2026-11-01', 3000000)`)

	trajectory := func() map[string]any {
		t.Helper()
		rec := f.do(http.MethodGet)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var got struct {
			Trajectory map[string]any `json:"trajectory"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.Trajectory
	}
	patch := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/api/household", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Cookie", f.cookie)
		rec := httptest.NewRecorder()
		f.srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH %s = %d, want 200 (body %s)", body, rec.Code, rec.Body.String())
		}
	}

	before := trajectory()
	if before["todays_allowance"] != "176470.5882" || before["baseline_daily_budget"] != "100000.0000" {
		t.Fatalf("adaptive trajectory = %v, want an allowance above the baseline", before)
	}

	patch(`{"allowance_mode":"fixed"}`)
	fixed := trajectory()
	if fixed["todays_allowance"] != fixed["baseline_daily_budget"] || fixed["todays_allowance"] != "100000.0000" {
		t.Errorf("fixed: todays_allowance = %v, baseline_daily_budget = %v, want both 100000.0000", fixed["todays_allowance"], fixed["baseline_daily_budget"])
	}

	patch(`{"allowance_mode":"adaptive"}`)
	if again := trajectory(); again["todays_allowance"] != "176470.5882" {
		t.Errorf("adaptive again: todays_allowance = %v, want 176470.5882", again["todays_allowance"])
	}
}

// A Household fully committed has a trajectory with no baseline: absent, never
// null, and nothing about a State.
func TestAFullyCommittedHouseholdsTrajectoryHasNoBaselineProperty(t *testing.T) {
	f := newDashboardFixture(t)
	var rent pgtype.UUID
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`INSERT INTO categories (id, household_id, category_display_name, bucket) VALUES (gen_random_uuid(), $1, 'Rent', 'commitment') RETURNING id`, f.householdID).Scan(&rent); err != nil {
		t.Fatal(err)
	}
	f.mustExec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`)
	f.mustExec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-11', '2026-11-01', 3000000)`)
	if _, err := f.tdb.Pool.Exec(context.Background(),
		`INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, '2026-11', '2026-11-01', $2, 3500000)`, f.householdID, rent); err != nil {
		t.Fatal(err)
	}

	rec := f.do(http.MethodGet)

	want := `{"currency":"IDR","trajectory":{"days_remaining":17,"left_to_spend_today":"-29411.7648","remaining_daily_pool":"-500000.0000","todays_allowance":"-29411.7648"}}`
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != want {
		t.Errorf("answer = %d %s\nwant %s", rec.Code, got, want)
	}
}

func (f dashboardFixture) mustExec(t *testing.T, sql string) {
	t.Helper()
	if _, err := f.tdb.Pool.Exec(context.Background(), sql, f.householdID); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}
