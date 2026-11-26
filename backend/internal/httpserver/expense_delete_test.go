package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DELETE /expenses/{id}, POST /expenses/{id}/restore and GET /expenses?deleted=
// through the whole stack (the session check, the cross-site guard, the binder,
// the spec validator, the handler) over a real database (#140 X10, R11, PR 5).

func (f expenseFixture) del(id string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.do(http.MethodDelete, "/api/expenses/"+id, "", mutate...)
}

func TestDeleteAndRestoreThroughTheRouter(t *testing.T) {
	f := newExpenseFixture(t)
	created := f.post(expenseBody(expenseV7, "-25000"))
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, body(created))
	}

	rec := f.do(http.MethodDelete, "/api/expenses/"+expenseV7, "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("DELETE = %d %q, want 204 and no body", rec.Code, body(rec))
	}
	if rec := f.do(http.MethodGet, "/api/expenses/"+expenseV7, ""); rec.Code != http.StatusNotFound {
		t.Errorf("GET of a deleted Expense = %d, want 404", rec.Code)
	}
	if rec := f.patch(expenseV7, `{}`); rec.Code != http.StatusNotFound {
		t.Errorf("PATCH of a deleted Expense = %d, want 404", rec.Code)
	}
	if got := body(f.do(http.MethodGet, "/api/expenses", "")); got != `{"items":[],"uncategorised_count":0}` {
		t.Errorf("the live list = %s, want it empty", got)
	}

	bin := f.do(http.MethodGet, "/api/expenses?deleted=true", "")
	got := body(bin)
	if bin.Code != http.StatusOK || !strings.Contains(got, `"id":"`+expenseV7+`"`) || !strings.Contains(got, `"amount":"-25000.0000"`) ||
		!strings.Contains(got, `"deleted_at":"`) || strings.Contains(got, "uncategorised_count") {
		t.Errorf("the bin = %d %s, want the refund, with deleted_at and no uncategorised_count", bin.Code, got)
	}

	rec = f.do(http.MethodPost, "/api/expenses/"+expenseV7+"/restore", "")
	if rec.Code != http.StatusOK || body(rec) != body(created) {
		t.Errorf("restore = %d %s, want 200 and the Expense as created, %s", rec.Code, body(rec), body(created))
	}
	if got := body(f.do(http.MethodGet, "/api/expenses?deleted=true", "")); got != `{"items":[]}` {
		t.Errorf("the bin after the restore = %s, want it empty", got)
	}
	if rec := f.do(http.MethodGet, "/api/expenses/"+expenseV7, ""); rec.Code != http.StatusOK {
		t.Errorf("GET after the restore = %d, want 200", rec.Code)
	}
}

// ?deleted is a boolean, as ?uncategorised is; the bin has no filter.
func TestTheDeletedParameterThroughTheRouter(t *testing.T) {
	f := newExpenseFixture(t)
	if rec := f.post(expenseBody(expenseV7, "25000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	if rec := f.post(expenseBody(expenseV7b, "1000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	f.del(expenseV7b)

	for query, wantID := range map[string]string{
		"?deleted=false": expenseV7, "": expenseV7, "?deleted=true": expenseV7b, "?deleted=true&uncategorised=false": expenseV7b,
	} {
		rec := f.do(http.MethodGet, "/api/expenses"+query, "")
		got := body(rec)
		if rec.Code != http.StatusOK || strings.Count(got, `"id":"`) != 1 || !strings.Contains(got, wantID) {
			t.Errorf("%q -> %d %s, want only %s", query, rec.Code, got, wantID)
		}
	}

	for query, want := range map[string]string{
		"?deleted=abc":                       `{"code":"VALIDATION","args":{"field":"deleted","rule":"invalid"}}`,
		"?deleted=":                          `{"code":"VALIDATION","args":{"field":"deleted","rule":"invalid"}}`,
		"?deleted=yes":                       `{"code":"VALIDATION","args":{"field":"deleted","rule":"invalid"}}`,
		"?deleted=true&uncategorised=true":   `{"args":{"field":"uncategorised","rule":"invalid"},"code":"VALIDATION"}`,
		"?deleted=true&after=garbage":        `{"args":{"field":"after","rule":"invalid"},"code":"VALIDATION"}`,
		"?deleted=true&limit=0":              `{"code":"VALIDATION","args":{"field":"limit","rule":"min"}}`,
		"?deleted=true&limit=101":            `{"code":"VALIDATION","args":{"field":"limit","rule":"max"}}`,
		"?deleted=true&uncategorised=truthy": `{"code":"VALIDATION","args":{"field":"uncategorised","rule":"invalid"}}`,
	} {
		rec := f.do(http.MethodGet, "/api/expenses"+query, "")
		if rec.Code != http.StatusBadRequest || body(rec) != want {
			t.Errorf("%q -> %d %s, want 400 %s", query, rec.Code, body(rec), want)
		}
	}
}

// A repeat delete is 204; an unknown id is NOT_FOUND; a restore of a live
// Expense is 200. A value that is not a UUID is the binder's 400.
func TestDeleteAndRestoreAnswerTheContractsErrorsThroughTheRouter(t *testing.T) {
	f := newExpenseFixture(t)
	if rec := f.post(expenseBody(expenseV7, "25000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	for range 2 {
		if rec := f.do(http.MethodDelete, "/api/expenses/"+expenseV7, ""); rec.Code != http.StatusNoContent {
			t.Errorf("DELETE = %d, want 204 each time", rec.Code)
		}
	}
	for _, rec := range []*httptest.ResponseRecorder{
		f.del(expenseV7b),
		f.do(http.MethodPost, "/api/expenses/"+expenseV7b+"/restore", ""),
	} {
		if rec.Code != http.StatusNotFound || body(rec) != `{"code":"NOT_FOUND"}` {
			t.Errorf("an unknown id = %d %s, want 404 NOT_FOUND", rec.Code, body(rec))
		}
	}
	bad := `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}`
	for name, rec := range map[string]string{
		"delete":  body(f.do(http.MethodDelete, "/api/expenses/not-a-uuid", "")),
		"restore": body(f.do(http.MethodPost, "/api/expenses/not-a-uuid/restore", "")),
	} {
		if rec != bad {
			t.Errorf("%s of a bad id = %s, want %s", name, rec, bad)
		}
	}
}

// A cross-site DELETE or restore changes nothing (the guard of §5 covers every
// non-safe method).
func TestACrossSiteDeleteOrRestoreChangesNothing(t *testing.T) {
	f := newExpenseFixture(t)
	if rec := f.post(expenseBody(expenseV7, "25000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	cross := func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }

	before := f.rows(t)
	if rec := f.del(expenseV7, cross); rec.Code != http.StatusForbidden {
		t.Errorf("a cross-site DELETE = %d, want 403", rec.Code)
	}
	if after := f.rows(t); after != before {
		t.Errorf("a refused DELETE changed a row:\n%s\n%s", before, after)
	}

	f.del(expenseV7)
	before = f.rows(t)
	if rec := f.do(http.MethodPost, "/api/expenses/"+expenseV7+"/restore", "", cross); rec.Code != http.StatusForbidden {
		t.Errorf("a cross-site restore = %d, want 403", rec.Code)
	}
	if after := f.rows(t); after != before {
		t.Errorf("a refused restore changed a row:\n%s\n%s", before, after)
	}
}

// Every other verb on the new paths is a 405 that writes nothing.
func TestOtherVerbsOnTheRestorePathAreNotAllowed(t *testing.T) {
	f := newExpenseFixture(t)
	if rec := f.post(expenseBody(expenseV7, "25000")); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	f.del(expenseV7)
	before := f.rows(t)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if rec := f.do(method, "/api/expenses/"+expenseV7+"/restore", ""); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /expenses/{id}/restore = %d, want 405", method, rec.Code)
		}
	}
	if after := f.rows(t); after != before {
		t.Errorf("a 405 changed a row:\n%s\n%s", before, after)
	}
}
