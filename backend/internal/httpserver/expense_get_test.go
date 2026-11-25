package httpserver

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kerti/afloat/backend/internal/testutil"
)

// GET /expenses/{id} and GET /expenses?uncategorised= through the whole stack
// (the session check, the binder, the spec validator, the handler) over a real
// database (#140 X1, X8, PR 4).

func TestGettingAnExpenseThroughTheRouterAnswersTheStoredRow(t *testing.T) {
	f := newExpenseFixture(t)
	created := f.post(expenseBody(expenseV7, "-25000"))
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, body(created))
	}

	rec := f.do(http.MethodGet, "/api/expenses/"+expenseV7, "")
	if rec.Code != http.StatusOK || body(rec) != body(created) {
		t.Errorf("GET = %d %s, want 200 and the create's %s", rec.Code, body(rec), body(created))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestGettingAnExpenseThatIsNotTheCallersIsOne404ThroughTheRouter(t *testing.T) {
	f := newExpenseFixture(t)
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
	theirs := f.tdb.CreateExpense(t, other, theirPocket, testutil.ExpenseOpts{})
	if rec := f.post(expenseBody(expenseV7, "25000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	f.exec(t, `UPDATE expenses SET deleted_at = now() WHERE id = $1`, expenseV7)

	for name, id := range map[string]string{"deleted": expenseV7, "foreign": theirs.String(), "unknown": expenseV7b} {
		rec := f.do(http.MethodGet, "/api/expenses/"+id, "")
		if rec.Code != http.StatusNotFound || body(rec) != `{"code":"NOT_FOUND"}` {
			t.Errorf("%s -> %d %s, want 404 NOT_FOUND", name, rec.Code, body(rec))
		}
	}
}

// A value that is not a UUID is the binder's 400, as on every {id} route; with
// no session, the session answers first (path_param_session_test sweeps it).
func TestGettingAnExpenseByAnIdThatIsNotAUUIDIsInvalidThroughTheRouter(t *testing.T) {
	f := newExpenseFixture(t)
	rec := f.do(http.MethodGet, "/api/expenses/not-a-uuid", "")
	want := `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}`
	if rec.Code != http.StatusBadRequest || body(rec) != want {
		t.Errorf("GET /expenses/not-a-uuid -> %d %s, want 400 %s", rec.Code, body(rec), want)
	}

	rec = f.do(http.MethodGet, "/api/expenses/"+expenseV7, "", func(r *http.Request) { r.Header.Del("Cookie") })
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET without a session -> %d %s, want 401", rec.Code, body(rec))
	}
}

// ?uncategorised is a boolean: true filters, false and absent do not, and
// anything else is VALIDATION {uncategorised, invalid}, as limit's non-integers
// are.
func TestTheUncategorisedParameterThroughTheRouter(t *testing.T) {
	f := newExpenseFixture(t)
	if rec := f.post(expenseBody(expenseV7, "25000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	if rec := f.post(`{"id":"` + expenseV7b + `","amount":"1000","pocket_id":"` + pocketOne + `","occurred_on":"2026-11-19","category_id":"` + categoryV7 + `"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, body(rec))
	}

	for query, wantIDs := range map[string][]string{
		"":                            {expenseV7, expenseV7b},
		"?uncategorised=true":         {expenseV7},
		"?uncategorised=false":        {expenseV7, expenseV7b},
		"?uncategorised=true&limit=1": {expenseV7},
	} {
		rec := f.do(http.MethodGet, "/api/expenses"+query, "")
		got := body(rec)
		if rec.Code != http.StatusOK || strings.Count(got, `"id":"`) != len(wantIDs) || !strings.Contains(got, `"uncategorised_count":1`) {
			t.Errorf("%q -> %d %s, want %v and a count of 1", query, rec.Code, got, wantIDs)
		}
		for _, id := range wantIDs {
			if !strings.Contains(got, id) {
				t.Errorf("%q: %s lacks %s", query, got, id)
			}
		}
	}

	want := `{"code":"VALIDATION","args":{"field":"uncategorised","rule":"invalid"}}`
	for _, v := range []string{"abc", "", "yes", "2", "truee"} {
		rec := f.do(http.MethodGet, "/api/expenses?uncategorised="+v, "")
		if rec.Code != http.StatusBadRequest || body(rec) != want {
			t.Errorf("uncategorised=%q -> %d %s, want 400 %s", v, rec.Code, body(rec), want)
		}
	}
}
