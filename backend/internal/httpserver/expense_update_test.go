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
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/expense"
	"github.com/kerti/afloat/backend/internal/pocket"
	"github.com/kerti/afloat/backend/internal/system"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// PATCH /expenses/{id} through the whole stack (the session check, the binder,
// the spec validator, the handler) over a real database (#140 X9, PR 3b).

type expenseFixture struct {
	tdb         *testutil.TestDB
	srv         http.Handler
	householdID pgtype.UUID
	cookie      string
}

const (
	expenseV7  = "0192f0a0-0000-7000-8004-000000000001"
	expenseV7b = "0192f0a0-0000-7000-8004-000000000002"
	pocketOne  = "0192f0a0-0000-7000-8001-000000000001"
	pocketTwo  = "0192f0a0-0000-7000-8001-000000000002"
	categoryV7 = "0192f0a0-0000-7000-8002-000000000001"
)

func newExpenseFixture(t *testing.T) expenseFixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	householdID := tdb.CreateHousehold(t, "Home")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "A")
	const token = "a-session-token-for-the-expense-tests"
	if _, err := tdb.Queries.CreateSession(context.Background(), db.CreateSessionParams{
		ID:        auth.HashToken(token),
		UserID:    userID,
		ExpiresAt: pgtypeTimestamptz(time.Now().Add(24 * time.Hour)),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	srv := New(Deps{
		System: system.New(system.Deps{Querier: tdb.Queries, Version: "test", LocalEnabled: true}),
		Auth: auth.New(auth.Deps{
			Querier: tdb.Queries, Beginner: tdb.Pool,
			SessionTTL: 30 * 24 * time.Hour, SessionMaxLifetime: 90 * 24 * time.Hour, CookieSecure: true,
		}),
		Pocket:         pocket.New(pocket.Deps{Querier: tdb.Queries, Beginner: tdb.Pool}),
		Expense:        expense.New(expense.Deps{Querier: tdb.Queries, Beginner: tdb.Pool}),
		HandlerTimeout: testHandlerTimeout,
	})
	f := expenseFixture{tdb: tdb, srv: srv, householdID: householdID, cookie: auth.SessionCookieName + "=" + token}
	f.exec(t, `INSERT INTO pockets (id, household_id, pocket_display_name) VALUES ($1, $2, 'Cash'), ($3, $2, 'Wallet')`, pocketOne, householdID, pocketTwo)
	f.exec(t, `INSERT INTO categories (id, household_id, category_display_name, bucket) VALUES ($1, $2, 'Food', 'daily')`, categoryV7, householdID)
	return f
}

func (f expenseFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tdb.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
}

func (f expenseFixture) do(method, target, reqBody string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(reqBody))
	if reqBody != "" {
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

func (f expenseFixture) post(reqBody string) *httptest.ResponseRecorder {
	return f.do(http.MethodPost, "/api/expenses", reqBody)
}

func (f expenseFixture) patch(id, reqBody string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.do(http.MethodPatch, "/api/expenses/"+id, reqBody, mutate...)
}

func (f expenseFixture) rows(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(to_jsonb(e)::text, E'\n' ORDER BY id), '') FROM expenses e`).Scan(&s); err != nil {
		t.Fatalf("read expenses: %v", err)
	}
	return s
}

func expenseBody(id, amount string) string {
	return `{"id":"` + id + `","amount":"` + amount + `","pocket_id":"` + pocketOne + `","occurred_on":"2026-11-20"}`
}

// The retry that was edited in between: a replayed POST answers what is stored
// and applies nothing, and a PATCH of the same id puts the edit there. One
// Expense, at the edited amount (the client's reconcile after a lost 201).
func TestAReplayedCaptureThenAnEditReconcilesThroughTheRouter(t *testing.T) {
	f := newExpenseFixture(t)

	rec := f.post(expenseBody(expenseV7, "25000"))
	if rec.Code != http.StatusCreated || !strings.Contains(body(rec), `"amount":"25000.0000"`) {
		t.Fatalf("create = %d %s, want 201 with 25000.0000", rec.Code, body(rec))
	}
	rec = f.post(expenseBody(expenseV7, "52000"))
	if rec.Code != http.StatusOK || !strings.Contains(body(rec), `"amount":"25000.0000"`) {
		t.Fatalf("replay = %d %s, want 200 with the stored 25000.0000", rec.Code, body(rec))
	}
	rec = f.patch(expenseV7, `{"amount":"52000"}`)
	if rec.Code != http.StatusOK || !strings.Contains(body(rec), `"amount":"52000.0000"`) || !strings.Contains(body(rec), `"id":"`+expenseV7+`"`) {
		t.Fatalf("patch = %d %s, want 200 with 52000.0000", rec.Code, body(rec))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	rec = f.do(http.MethodGet, "/api/expenses", "")
	list := body(rec)
	if rec.Code != http.StatusOK || strings.Count(list, `"id":"`) != 1 || !strings.Contains(list, `"amount":"52000.0000"`) || strings.Contains(list, "25000") {
		t.Errorf("list = %d %s, want exactly one Expense at 52000.0000", rec.Code, list)
	}
}

// What the wire says about each clearable property: absent, null and a value,
// and a cleared one is omitted, never null.
func TestPatchingAnExpenseThroughTheRouterMergesAndClears(t *testing.T) {
	f := newExpenseFixture(t)
	if rec := f.post(`{"id":"` + expenseV7 + `","amount":"25000","pocket_id":"` + pocketOne + `","occurred_on":"2026-11-20","occurred_time":"19:05","category_id":"` + categoryV7 + `","description":"Lunch"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, body(rec))
	}

	for _, tc := range []struct{ name, body, contains, absent string }{
		{"absent leaves them", `{"amount":"-5"}`, `"description":"Lunch"`, ""},
		{"a refund keeps its sign", `{"amount":"-25000"}`, `"amount":"-25000.0000"`, ""},
		{"null clears the Category", `{"category_id":null}`, `"occurred_time":"19:05"`, `"category_id"`},
		{"null clears the time", `{"occurred_time":null}`, `"description":"Lunch"`, `"occurred_time"`},
		{"null clears the text", `{"description":null}`, `"pocket_id":"` + pocketOne + `"`, `"description"`},
		{"a value sets one", `{"category_id":"` + categoryV7 + `","description":"  Dinner ","occurred_time":"07:30"}`, `"description":"Dinner"`, ""},
		{"a Pocket", `{"pocket_id":"` + pocketTwo + `"}`, `"pocket_id":"` + pocketTwo + `"`, ""},
		{"a date in the future", `{"occurred_on":"2999-12-31"}`, `"occurred_on":"2999-12-31"`, ""},
		{"{}", `{}`, `"occurred_on":"2999-12-31"`, ""},
	} {
		rec := f.patch(expenseV7, tc.body)
		got := body(rec)
		if rec.Code != http.StatusOK || !strings.Contains(got, tc.contains) || (tc.absent != "" && strings.Contains(got, tc.absent)) || strings.Contains(got, "null") {
			t.Errorf("%s: %s -> %d %s", tc.name, tc.body, rec.Code, got)
		}
	}
}

// The contract's own rules, before any lookup: a null on a property that cannot
// be cleared, an unknown property and a wrong type are INVALID_JSON_BODY.
func TestPatchingAnExpenseThroughTheRouterRefusesWhatTheContractRefuses(t *testing.T) {
	f := newExpenseFixture(t)
	if rec := f.post(expenseBody(expenseV7, "25000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	before := f.rows(t)

	for _, tc := range []struct{ name, body, want string }{
		{"null amount", `{"amount":null}`, `{"code":"INVALID_JSON_BODY"}`},
		{"null date", `{"occurred_on":null}`, `{"code":"INVALID_JSON_BODY"}`},
		{"null pocket", `{"pocket_id":null}`, `{"code":"INVALID_JSON_BODY"}`},
		{"null ownership", `{"ownership":null}`, `{"code":"INVALID_JSON_BODY"}`},
		{"null owner", `{"owner_user_id":null}`, `{"code":"INVALID_JSON_BODY"}`},
		{"a JSON number amount", `{"amount":5}`, `{"code":"INVALID_JSON_BODY"}`},
		{"currency", `{"currency":"USD"}`, `{"code":"INVALID_JSON_BODY"}`},
		{"captured_at", `{"captured_at":"2026-01-01T00:00:00Z"}`, `{"code":"INVALID_JSON_BODY"}`},
		{"id", `{"id":"` + expenseV7b + `"}`, `{"code":"INVALID_JSON_BODY"}`},
		{"household_id", `{"household_id":"` + expenseV7b + `"}`, `{"code":"INVALID_JSON_BODY"}`},
		{"deleted_at", `{"deleted_at":null}`, `{"code":"INVALID_JSON_BODY"}`},
		{"not an object", `[]`, `{"code":"INVALID_JSON_BODY"}`},
		{"an exponent amount", `{"amount":"2.5e4"}`, `{"code":"VALIDATION","args":{"field":"amount","rule":"pattern"}}`},
		{"five decimals", `{"amount":"1.00001"}`, `{"code":"VALIDATION","args":{"field":"amount","rule":"pattern"}}`},
		{"zero", `{"amount":"-0"}`, `{"args":{"field":"amount","rule":"nonzero"},"code":"VALIDATION"}`},
		{"a bad date", `{"occurred_on":"2026-02-30"}`, `{"args":{"field":"occurred_on","rule":"invalid"},"code":"VALIDATION"}`},
		{"a v4 pocket", `{"pocket_id":"6ba7b810-9dad-41d1-80b4-00c04fd430c8"}`, `{"code":"VALIDATION","args":{"field":"pocket_id","rule":"pattern"}}`},
		{"a blank text", `{"description":"   "}`, `{"args":{"field":"description","rule":"required"},"code":"VALIDATION"}`},
		{"a bad time", `{"occurred_time":"24:00"}`, `{"code":"VALIDATION","args":{"field":"occurred_time","rule":"pattern"}}`},
		{"joint with an owner", `{"ownership":"joint","owner_user_id":"` + expenseV7b + `"}`, `{"args":{"field":"owner_user_id","rule":"invalid"},"code":"VALIDATION"}`},
	} {
		rec := f.patch(expenseV7, tc.body)
		if rec.Code != http.StatusBadRequest || body(rec) != tc.want {
			t.Errorf("%s: %s -> %d %s, want 400 %s", tc.name, tc.body, rec.Code, body(rec), tc.want)
		}
	}
	if after := f.rows(t); after != before {
		t.Errorf("a refused patch changed the row:\n%s\n%s", before, after)
	}
}

// Unknown, another Household's and deleted: one 404, even for {}, and a bad body
// is the bad body's 400 whatever the id.
func TestPatchingAnExpenseThatIsNotTheCallersIsOne404ThroughTheRouter(t *testing.T) {
	f := newExpenseFixture(t)
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
	theirs := f.tdb.CreateExpense(t, other, theirPocket, testutil.ExpenseOpts{})
	if rec := f.post(expenseBody(expenseV7, "25000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	f.exec(t, `UPDATE expenses SET deleted_at = now() WHERE id = $1`, expenseV7)
	before := f.rows(t)

	for _, b := range []string{`{}`, `{"amount":"1"}`, `{"category_id":null}`, `{"pocket_id":"` + pocketTwo + `"}`} {
		for name, id := range map[string]string{"deleted": expenseV7, "foreign": theirs.String(), "unknown": expenseV7b} {
			rec := f.patch(id, b)
			if rec.Code != http.StatusNotFound || body(rec) != `{"code":"NOT_FOUND"}` {
				t.Errorf("%s with %s -> %d %s, want 404 NOT_FOUND", name, b, rec.Code, body(rec))
			}
		}
	}
	// Validation before lookup: the same bytes for the three.
	var answers []string
	for _, id := range []string{expenseV7, theirs.String(), expenseV7b} {
		rec := f.patch(id, `{"amount":"0"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("a bad body on %s -> %d, want 400", id, rec.Code)
		}
		answers = append(answers, body(rec))
	}
	if answers[0] != answers[1] || answers[0] != answers[2] {
		t.Errorf("the three differ: %v", answers)
	}
	// An id that is not a UUID is the binder's.
	if rec := f.patch("not-a-uuid", `{}`); rec.Code != http.StatusBadRequest || body(rec) != `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}` {
		t.Errorf("a malformed id -> %d %s", rec.Code, body(rec))
	}
	if after := f.rows(t); after != before {
		t.Errorf("a 404 changed a row:\n%s\n%s", before, after)
	}
}

func TestPatchingAnExpenseWithoutASessionIs401AndAcrossSitesIs403(t *testing.T) {
	f := newExpenseFixture(t)
	if rec := f.post(expenseBody(expenseV7, "25000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	before := f.rows(t)

	rec := f.patch(expenseV7, `{"amount":"1"}`, func(r *http.Request) { r.Header.Del("Cookie") })
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no cookie -> %d %s, want 401", rec.Code, body(rec))
	}
	rec = f.patch(expenseV7, `{"amount":"1"}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site -> %d %s, want 403", rec.Code, body(rec))
	}
	if after := f.rows(t); after != before {
		t.Errorf("a refused patch changed the row:\n%s\n%s", before, after)
	}
}
