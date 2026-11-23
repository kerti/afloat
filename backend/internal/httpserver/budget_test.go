package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/category"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/system"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// budgetFixture is the whole stack over a real database, with one signed-in
// User and a pinned clock (10:00 on 14 Nov 2026 in Jakarta), so the Budget
// operations are exercised through the session check, the spec validator and
// the generated binding rather than as bare handlers.
type budgetFixture struct {
	tdb         *testutil.TestDB
	srv         http.Handler
	householdID pgtype.UUID
	cookie      string
}

func newBudgetFixture(t *testing.T) budgetFixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	householdID := tdb.CreateHousehold(t, "Home")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "A")

	const token = "a-session-token-for-the-budget-tests"
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
		Budget:         budget.New(budget.Deps{Querier: tdb.Queries, Beginner: tdb.Pool, Now: func() time.Time { return now }}),
		Category:       category.New(category.Deps{Querier: tdb.Queries, Beginner: tdb.Pool, Now: func() time.Time { return now }}),
		HandlerTimeout: testHandlerTimeout,
	})
	return budgetFixture{tdb: tdb, srv: srv, householdID: householdID, cookie: auth.SessionCookieName + "=" + token}
}

func (f budgetFixture) do(method, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.doAt(method, "/api/budget", body, mutate...)
}

func (f budgetFixture) doAt(method, path, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Cookie", f.cookie)
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

// written is every Budget row, so "nothing was written" is a byte comparison.
func (f budgetFixture) written(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT (SELECT coalesce(string_agg(to_jsonb(b)::text, ';'), '') FROM budgets b) || '|' ||
		        (SELECT coalesce(string_agg(to_jsonb(m)::text, ';'), '') FROM monthly_budgets m)`).Scan(&s); err != nil {
		t.Fatalf("read budgets: %v", err)
	}
	return s
}

func TestGetBudgetThroughTheRouterOnAHouseholdWithNothing(t *testing.T) {
	f := newBudgetFixture(t)

	rec := f.do(http.MethodGet, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	want := `{"allocations":[],"commitments_budget":"0.0000","period":{"ends_on":"2026-11-30","starts_on":"2026-11-01","year_month":"2026-11"}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestPatchBudgetThroughTheRouterAnswersMoneyAsFourDecimalStrings(t *testing.T) {
	f := newBudgetFixture(t)

	rec := f.do(http.MethodPatch, `{"monthly_budget":"9000000"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	want := `{"allocations":[],"commitments_budget":"0.0000","daily_pool":"9000000.0000","monthly_budget":"9000000.0000",` +
		`"period":{"ends_on":"2026-11-30","starts_on":"2026-11-01","year_month":"2026-11"},"unallocated":"9000000.0000"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	// A GET after it says the same.
	if got := strings.TrimSpace(f.do(http.MethodGet, "").Body.String()); got != want {
		t.Errorf("GET after PATCH = %s, want %s", got, want)
	}
}

// The money spelling table (#130 C10). Every refusal answers 400 and writes
// nothing, not even the Period's marker: the contract's rule runs before the
// handler does.
func TestPatchBudgetRefusesEverySpellingTheContractDoesNotAllow(t *testing.T) {
	validation := `{"code":"VALIDATION","args":{"field":"monthly_budget","rule":"pattern"}}`
	invalid := `{"code":"INVALID_JSON_BODY"}`
	for _, tc := range []struct {
		name, body, want string
	}{
		{"exponent notation", `{"monthly_budget":"9e6"}`, validation},
		{"an upper-case exponent", `{"monthly_budget":"9E6"}`, validation},
		{"a negative", `{"monthly_budget":"-1"}`, validation},
		{"a negative zero", `{"monthly_budget":"-0"}`, validation},
		{"a plus sign", `{"monthly_budget":"+5"}`, validation},
		{"five decimals: refused, never rounded", `{"monthly_budget":"1.00001"}`, validation},
		{"five decimals that would round up", `{"monthly_budget":"0.99999"}`, validation},
		{"a leading space", `{"monthly_budget":" 5"}`, validation},
		{"a trailing space", `{"monthly_budget":"5 "}`, validation},
		{"a trailing newline", `{"monthly_budget":"5\n"}`, validation},
		{"a thousands separator", `{"monthly_budget":"1,000"}`, validation},
		{"a decimal comma", `{"monthly_budget":"1,5"}`, validation},
		{"an underscore", `{"monthly_budget":"1_000"}`, validation},
		{"17 integer digits", `{"monthly_budget":"10000000000000000"}`, validation},
		{"17 integer digits of zeros and a one", `{"monthly_budget":"00000000000000001"}`, validation},
		{"the empty string", `{"monthly_budget":""}`, validation},
		{"a bare point", `{"monthly_budget":"."}`, validation},
		{"a leading point", `{"monthly_budget":".5"}`, validation},
		{"a trailing point", `{"monthly_budget":"5."}`, validation},
		{"hexadecimal", `{"monthly_budget":"0x10"}`, validation},
		{"words", `{"monthly_budget":"nine million"}`, validation},
		{"NaN", `{"monthly_budget":"NaN"}`, validation},
		{"Infinity", `{"monthly_budget":"Infinity"}`, validation},
		{"Arabic-Indic digits", `{"monthly_budget":"١٢٣"}`, validation},
		{"full-width digits", `{"monthly_budget":"１２３"}`, validation},
		{"a currency symbol", `{"monthly_budget":"Rp5"}`, validation},
		{"a JSON number", `{"monthly_budget":9000000}`, invalid},
		{"a JSON number with a fraction", `{"monthly_budget":9000000.5}`, invalid},
		{"a JSON number in exponent notation", `{"monthly_budget":9e6}`, invalid},
		{"null: a Monthly Budget cannot be cleared", `{"monthly_budget":null}`, invalid},
		{"a boolean", `{"monthly_budget":true}`, invalid},
		{"an object", `{"monthly_budget":{"amount":"5"}}`, invalid},
		{"an array", `{"monthly_budget":["5"]}`, invalid},
		{"an unknown property", `{"monthly_budget":"5","currency":"IDR"}`, invalid},
		// BOOTSTRAP §5.2: a body that is not the declared shape is INVALID_JSON_BODY
		// "whatever else is wrong with it", so a bad amount does not outrank it.
		{"a bad amount and an unknown property", `{"monthly_budget":"-1","x":1}`, invalid},
		{"a property of the Budget the client may not set", `{"daily_pool":"5.0000"}`, invalid},
		{"a non-object root", `[]`, invalid},
		{"an empty body is not JSON", ``, invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBudgetFixture(t)
			before := f.written(t)

			rec := f.do(http.MethodPatch, tc.body, func(r *http.Request) {
				r.Header.Set("Content-Type", "application/json")
			})

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tc.want {
				t.Errorf("body = %s, want %s", got, tc.want)
			}
			if after := f.written(t); after != before {
				t.Errorf("a refused request wrote: %q -> %q", before, after)
			}
		})
	}
}

func TestPatchBudgetAcceptsEverySpellingTheContractAllows(t *testing.T) {
	for in, want := range map[string]string{
		"0":                     "0.0000",
		"0.0001":                "0.0001",
		"9000000":               "9000000.0000",
		"9000000.5":             "9000000.5000",
		"9000000.5000":          "9000000.5000",
		"0000000000000001":      "1.0000",
		"9999999999999999":      "9999999999999999.0000",
		"9999999999999999.9999": "9999999999999999.9999",
	} {
		t.Run(in, func(t *testing.T) {
			f := newBudgetFixture(t)
			rec := f.do(http.MethodPatch, `{"monthly_budget":"`+in+`"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"monthly_budget":"`+want+`"`) {
				t.Errorf("body = %s, want monthly_budget %s", rec.Body.String(), want)
			}
		})
	}
}

func TestPatchBudgetWithAnEmptyObjectIsANoOpThatAnswersTheBudget(t *testing.T) {
	f := newBudgetFixture(t)
	rec := f.do(http.MethodPatch, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got, want := strings.TrimSpace(rec.Body.String()), strings.TrimSpace(f.do(http.MethodGet, "").Body.String()); got != want {
		t.Errorf("PATCH {} = %s, GET = %s", got, want)
	}
}

// HEAD answers like GET (BOOTSTRAP.md §5.2), so it materialises too: the first
// read of a Period is the one that writes, whichever method makes it.
func TestHeadBudgetAnswersLikeGetAndMaterialises(t *testing.T) {
	f := newBudgetFixture(t)
	if before := f.written(t); before != "|" {
		t.Fatalf("a fresh database holds budgets: %q", before)
	}

	rec := f.do(http.MethodHead, "")

	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD = %d with %d body bytes, want 200 with none", rec.Code, rec.Body.Len())
	}
	if after := f.written(t); !strings.Contains(after, `"year_month": "2026-11"`) {
		t.Errorf("HEAD did not materialise the Period: %q", after)
	}
}

func TestBudgetNeedsASession(t *testing.T) {
	f := newBudgetFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodHead} {
		body := ""
		if method == http.MethodPatch {
			body = `{"monthly_budget":"1"}`
		}
		rec := f.do(method, body, func(r *http.Request) { r.Header.Del("Cookie") })
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session = %d, want 401", method, rec.Code)
		}
	}
	if got := f.written(t); got != "|" {
		t.Errorf("an unauthenticated request wrote: %q", got)
	}
}

// A refused body with no session is still 401: nothing is learned about the
// validator.
func TestBudgetAnUnauthenticatedBadBodyIs401NotValidation(t *testing.T) {
	f := newBudgetFixture(t)
	rec := f.do(http.MethodPatch, `{"monthly_budget":"-1"}`, func(r *http.Request) { r.Header.Del("Cookie") })
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestBudgetCrossSitePatchIsBlockedAndWritesNothing(t *testing.T) {
	f := newBudgetFixture(t)
	rec := f.do(http.MethodPatch, `{"monthly_budget":"1"}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "CROSS_SITE_REQUEST_BLOCKED") {
		t.Errorf("cross-site PATCH = %d %s, want 403 CROSS_SITE_REQUEST_BLOCKED", rec.Code, rec.Body.String())
	}
	if got := f.written(t); got != "|" {
		t.Errorf("a blocked request wrote: %q", got)
	}
}

// There is one Budget at /budget and nothing to create or remove there.
func TestBudgetServesOnlyGetHeadAndPatch(t *testing.T) {
	f := newBudgetFixture(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := f.do(method, `{"monthly_budget":"1"}`)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /budget = %d, want 405", method, rec.Code)
		}
	}
	if got := f.written(t); got != "|" {
		t.Errorf("a refused method wrote: %q", got)
	}
}
