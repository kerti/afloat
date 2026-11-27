package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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
// answer plus the `trajectory`. Every money is four decimals.
const dashboardAnswerPR3 = `{"currency":"IDR","trajectory":{"baseline_daily_budget":"100000.0000","days_remaining":17,"left_to_spend_today":"176470.5882","remaining_daily_pool":"3000000.0000","todays_allowance":"176470.5882"}}`

// The same Household's answer at PR 6: PR 3's plus the `state` and the
// `trajectory_variance` (13 settled days of a 100.000 target, none spent, so
// 1.300.000 under it). Buffer Days and every day count but days_remaining are
// still not on it.
const dashboardAnswerPR6 = `{"currency":"IDR","trajectory":{"baseline_daily_budget":"100000.0000","days_remaining":17,"left_to_spend_today":"176470.5882","remaining_daily_pool":"3000000.0000","state":"afloat","todays_allowance":"176470.5882","trajectory_variance":"1300000.0000"}}`

// The answer at PR 8: PR 6's plus `chart`, one point for each of the Period's 30
// days. A target of 100.000 a day, read at the end of the day, so the first
// point is 100.000 and not zero; an actual of zero (nothing is spent) through
// Today, the 14th, which alone is in progress; no actual after it.
const dashboardAnswerPR8Chart = `{"points":[{"actual":"0.0000","date":"2026-11-01","target":"100000.0000"},{"actual":"0.0000","date":"2026-11-02","target":"200000.0000"},{"actual":"0.0000","date":"2026-11-03","target":"300000.0000"},{"actual":"0.0000","date":"2026-11-04","target":"400000.0000"},{"actual":"0.0000","date":"2026-11-05","target":"500000.0000"},{"actual":"0.0000","date":"2026-11-06","target":"600000.0000"},{"actual":"0.0000","date":"2026-11-07","target":"700000.0000"},{"actual":"0.0000","date":"2026-11-08","target":"800000.0000"},{"actual":"0.0000","date":"2026-11-09","target":"900000.0000"},{"actual":"0.0000","date":"2026-11-10","target":"1000000.0000"},{"actual":"0.0000","date":"2026-11-11","target":"1100000.0000"},{"actual":"0.0000","date":"2026-11-12","target":"1200000.0000"},{"actual":"0.0000","date":"2026-11-13","target":"1300000.0000"},{"actual":"0.0000","date":"2026-11-14","in_progress":true,"target":"1400000.0000"},{"date":"2026-11-15","target":"1500000.0000"},{"date":"2026-11-16","target":"1600000.0000"},{"date":"2026-11-17","target":"1700000.0000"},{"date":"2026-11-18","target":"1800000.0000"},{"date":"2026-11-19","target":"1900000.0000"},{"date":"2026-11-20","target":"2000000.0000"},{"date":"2026-11-21","target":"2100000.0000"},{"date":"2026-11-22","target":"2200000.0000"},{"date":"2026-11-23","target":"2300000.0000"},{"date":"2026-11-24","target":"2400000.0000"},{"date":"2026-11-25","target":"2500000.0000"},{"date":"2026-11-26","target":"2600000.0000"},{"date":"2026-11-27","target":"2700000.0000"},{"date":"2026-11-28","target":"2800000.0000"},{"date":"2026-11-29","target":"2900000.0000"},{"date":"2026-11-30","target":"3000000.0000"}]}`

// The answer at PR 7: PR 6's plus `categories` and `pockets`. A Daily Category's
// allocation leaves the Daily Pool alone, so the same Household with one
// allocated Category is PR 6's answer byte for byte plus the one list.
const dashboardAnswerPR7Categories = `[{"active":true,"allocation":"500000.0000","category_display_name":"Food","category_id":"%s","spent":"0.0000"}]`

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
// currency alone; once it has one, the same answer gains `trajectory`, and PR 6
// adds the State and the variance to it, losing nothing. Money is a
// four-decimals string, never a number or an exponent.
func TestTheAnswerWithAMonthlyBudgetIsEachEarlierAnswerPlusItsOwnProperties(t *testing.T) {
	f := newDashboardFixture(t)
	f.mustExec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`)
	f.mustExec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-11', '2026-11-01', 3000000)`)

	rec := f.do(http.MethodGet)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	got := strings.TrimSpace(rec.Body.String())
	// PR 6's answer, every property of it byte for byte, and PR 8's `chart`:
	// nothing else (no categories or pockets: nothing is spent or allocated).
	var wholePR6, newer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(dashboardAnswerPR6), &wholePR6); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got), &newer); err != nil {
		t.Fatal(err)
	}
	for key, value := range wholePR6 {
		if string(newer[key]) != string(value) {
			t.Errorf("PR 6's %q = %s, now %s: an earlier answer's property was changed or removed", key, value, newer[key])
		}
	}
	if string(newer["chart"]) != dashboardAnswerPR8Chart {
		t.Errorf("chart = %s\nwant %s", newer["chart"], dashboardAnswerPR8Chart)
	}
	if len(newer) != len(wholePR6)+1 {
		t.Errorf("the answer has %d properties, want PR 6's %d and `chart`", len(newer), len(wholePR6))
	}

	var older map[string]json.RawMessage
	if err := json.Unmarshal([]byte(dashboardAnswerPR1), &older); err != nil {
		t.Fatal(err)
	}
	for key, value := range older {
		if string(newer[key]) != string(value) {
			t.Errorf("PR 1's %q = %s, PR 6's = %s: an earlier answer's property was changed or removed", key, value, newer[key])
		}
	}
	// PR 3's trajectory is PR 6's with two properties more: every one of its
	// properties is there, byte for byte.
	var pr3, pr6 struct {
		Trajectory map[string]json.RawMessage `json:"trajectory"`
	}
	if err := json.Unmarshal([]byte(dashboardAnswerPR3), &pr3); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got), &pr6); err != nil {
		t.Fatal(err)
	}
	for key, value := range pr3.Trajectory {
		if string(pr6.Trajectory[key]) != string(value) {
			t.Errorf("PR 3's trajectory.%s = %s, PR 6's = %s: an earlier property was changed or removed", key, value, pr6.Trajectory[key])
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
		case "left_to_spend_today", "todays_allowance", "remaining_daily_pool", "baseline_daily_budget", "trajectory_variance":
			if s, ok := value.(string); !ok || !money.MatchString(s) {
				t.Errorf("%s = %#v, want a money string with four decimals", key, value)
			}
		case "state":
			if s, ok := value.(string); !ok || (s != "afloat" && s != "drifting" && s != "taking_on_water") {
				t.Errorf("state = %#v, want one of the three States", value)
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

// A Household fully committed has a trajectory with no baseline, no State and
// no variance: each absent, never null.
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

	// The chart is there, and is a flat target at zero: it is the trajectory
	// that says there is no pace, not the chart that goes missing.
	var got struct {
		Currency   string          `json:"currency"`
		Trajectory json.RawMessage `json:"trajectory"`
		Chart      struct {
			Points []struct {
				Target string `json:"target"`
			} `json:"points"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("answer = %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	const want = `{"days_remaining":17,"left_to_spend_today":"-29411.7648","remaining_daily_pool":"-500000.0000","todays_allowance":"-29411.7648"}`
	if got.Currency != "IDR" || string(got.Trajectory) != want {
		t.Errorf("answer = %s %s\nwant IDR %s", got.Currency, got.Trajectory, want)
	}
	if len(got.Chart.Points) != 30 {
		t.Fatalf("chart has %d points, want 30", len(got.Chart.Points))
	}
	for i, p := range got.Chart.Points {
		if p.Target != "0.0000" {
			t.Errorf("point %d target = %s, want the flat zero of a fully committed Period", i+1, p.Target)
		}
	}
}

func (f dashboardFixture) mustExec(t *testing.T, sql string) {
	t.Helper()
	if _, err := f.tdb.Pool.Exec(context.Background(), sql, f.householdID); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// PR 7 adds `categories` and `pockets` to PR 6's answer and changes nothing of it:
// the same Household with one Daily Category allocated (which moves no pool) is
// PR 6's answer, every property byte for byte, and the one list; no Pocket has
// spend, so `pockets` is omitted rather than empty.
func TestTheAnswerWithABreakdownIsPR6sAnswerPlusItsOwnProperties(t *testing.T) {
	f := newDashboardFixture(t)
	f.mustExec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`)
	f.mustExec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-11', '2026-11-01', 3000000)`)
	food := f.tdb.CreateCategory(t, f.householdID, "Food", "daily")
	if _, err := f.tdb.Pool.Exec(context.Background(),
		`INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, '2026-11', '2026-11-01', $2, 500000)`,
		f.householdID, food); err != nil {
		t.Fatal(err)
	}

	rec := f.do(http.MethodGet)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var older, newer map[string]json.RawMessage
	if err := json.Unmarshal([]byte(dashboardAnswerPR6), &older); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &newer); err != nil {
		t.Fatal(err)
	}
	for key, value := range older {
		if string(newer[key]) != string(value) {
			t.Errorf("PR 6's %q = %s, PR 7's = %s: an earlier property was changed or removed", key, value, newer[key])
		}
	}
	wantCategories := fmt.Sprintf(dashboardAnswerPR7Categories, uuidText(food))
	if got := string(newer["categories"]); got != wantCategories {
		t.Errorf("categories = %s\nwant %s", got, wantCategories)
	}
	if _, ok := newer["pockets"]; ok {
		t.Errorf("pockets = %s, want it omitted: no Pocket has spend", newer["pockets"])
	}
	if len(newer) != len(older)+2 {
		t.Errorf("the answer has %d properties, want PR 6's %d, `categories` and `chart`", len(newer), len(older))
	}
}

// The wire's spelling of both lists, through the router and the spec validator:
// money at four decimals, ids and names beside every figure, the uncategorised
// row with nothing but its spend, a refund negative, the order as the server
// declared it, and no `null` anywhere.
func TestBothBreakdownsAreSpelledAsTheContractSaysAndOrderedOnTheServer(t *testing.T) {
	f := newDashboardFixture(t)
	f.mustExec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-11')`)
	f.mustExec(t, `INSERT INTO monthly_budgets VALUES ($1, '2026-11', '2026-11-01', 3000000)`)
	wallet := f.tdb.CreatePocket(t, f.householdID, "Wallet")
	card := f.tdb.CreatePocket(t, f.householdID, "Card")
	food := f.tdb.CreateCategory(t, f.householdID, "Food", "daily")
	rent := f.tdb.CreateCategory(t, f.householdID, "Rent", "commitment")
	for _, e := range []struct {
		pocket, category pgtype.UUID
		on, amount       string
	}{
		{wallet, food, "2026-11-05", "12000"},
		{wallet, pgtype.UUID{}, "2026-11-06", "30000"},
		{card, food, "2026-11-07", "-2000"},
		{card, rent, "2026-11-08", "900000"},
		{wallet, food, "2026-11-20", "555"}, // after Today: in neither list
	} {
		var category any
		if e.category.Valid {
			category = e.category
		}
		if _, err := f.tdb.Pool.Exec(context.Background(),
			`INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id, category_id) VALUES ($1, $2, $3::date, $4::numeric, 'IDR', $5, $6)`,
			testutil.NewUUID(t), f.householdID, e.on, e.amount, e.pocket, category); err != nil {
			t.Fatal(err)
		}
	}

	rec := f.do(http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got struct {
		Categories []map[string]any `json:"categories"`
		Pockets    []map[string]any `json:"pockets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	wantCategories := []map[string]any{
		{"spent": "30000.0000"},
		{"category_id": uuidText(food), "category_display_name": "Food", "active": true, "spent": "10000.0000"},
	}
	wantPockets := []map[string]any{
		{"pocket_id": uuidText(card), "pocket_display_name": "Card", "active": true, "spent": "898000.0000"},
		{"pocket_id": uuidText(wallet), "pocket_display_name": "Wallet", "active": true, "spent": "42000.0000"},
	}
	if !reflect.DeepEqual(got.Categories, wantCategories) {
		t.Errorf("categories = %v\nwant %v", got.Categories, wantCategories)
	}
	if !reflect.DeepEqual(got.Pockets, wantPockets) {
		t.Errorf("pockets = %v\nwant %v", got.Pockets, wantPockets)
	}
	if strings.Contains(rec.Body.String(), "null") {
		t.Errorf("the answer carries a null: %s", rec.Body.String())
	}
}

func uuidText(id pgtype.UUID) string { return uuid.UUID(id.Bytes).String() }
