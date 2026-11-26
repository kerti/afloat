package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/dashboard"
	"github.com/kerti/afloat/backend/internal/db"
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
