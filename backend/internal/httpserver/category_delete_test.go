package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DELETE /categories/{id} and POST /categories/{id}/restore through the whole
// stack: the cross-site guard, the session check, the binder, the spec
// validator and the handler, over a real database and a pinned clock (#130,
// PR 4). The handlers' own behaviour is tested in the category package; this is
// what only the router decides.

const unknownCategory = "0192f0a0-0000-7000-8002-0000000000ff"

func (f budgetFixture) delCategory(id string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.doAt(http.MethodDelete, "/api/categories/"+id, "", mutate...)
}

func (f budgetFixture) restoreCategory(id string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.doAt(http.MethodPost, "/api/categories/"+id+"/restore", "", mutate...)
}

func (f budgetFixture) categoryDeleted(t *testing.T, id string) (set bool) {
	t.Helper()
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT deleted_at IS NOT NULL FROM categories WHERE id = $1`, id).Scan(&set); err != nil {
		t.Fatalf("read category: %v", err)
	}
	return set
}

func TestDeleteAndRestoreACategoryThroughTheRouter(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	if rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catV7b+`","category_display_name":"Food","bucket":"daily"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	if rec := f.patchCategory(catV7, `{"active":false}`); rec.Code != http.StatusOK {
		t.Fatalf("deactivate = %d", rec.Code)
	}
	if rec := f.do(http.MethodPatch, `{"monthly_budget":"9000000"}`); rec.Code != http.StatusOK {
		t.Fatalf("budget = %d", rec.Code)
	}

	rec := f.delCategory(catV7)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("DELETE = %d %q, want 204 with no body", rec.Code, rec.Body.String())
	}
	if !f.categoryDeleted(t, catV7) {
		t.Error("the row has no deleted_at")
	}
	var n int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM categories`).Scan(&n); err != nil || n != 2 {
		t.Errorf("categories = %d (%v), want 2: a delete removes no row", n, err)
	}
	wantList := `{"items":[{"active":true,"bucket":"daily","category_display_name":"Food","id":"` + catV7b + `"}]}`
	if got := body(f.doAt(http.MethodGet, "/api/categories", "")); got != wantList {
		t.Errorf("list = %s, want %s", got, wantList)
	}
	if rec := f.patchCategory(catV7, `{"active":true}`); rec.Code != http.StatusNotFound || body(rec) != `{"code":"NOT_FOUND"}` {
		t.Errorf("PATCH on a deleted Category = %d %s, want 404 NOT_FOUND", rec.Code, body(rec))
	}
	// The Commitment is out of every figure of the Period, in the same answer.
	gone := `{"allocations":[],"commitments_budget":"0.0000","daily_pool":"9000000.0000","monthly_budget":"9000000.0000",` +
		`"period":{"ends_on":"2026-11-30","starts_on":"2026-11-01","year_month":"2026-11"},"unallocated":"9000000.0000"}`
	if got := body(f.do(http.MethodGet, "")); got != gone {
		t.Errorf("budget after the delete = %s, want %s", got, gone)
	}

	// Idempotent, and the repeat leaves the row exactly as the first left it.
	first := f.categoryRow(t, catV7)
	if rec := f.delCategory(catV7); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("a repeat DELETE = %d %q, want 204", rec.Code, rec.Body.String())
	}
	if after := f.categoryRow(t, catV7); after != first {
		t.Errorf("a repeat DELETE changed the row:\n%s\n%s", first, after)
	}

	// Restore: the Category as it was, still inactive; idempotent; and the
	// figures come back exactly.
	want := `{"active":false,"bucket":"commitment","category_display_name":"Rent","id":"` + catV7 + `"}`
	for _, label := range []string{"restore", "a repeat restore"} {
		rec := f.restoreCategory(catV7)
		if rec.Code != http.StatusOK || body(rec) != want {
			t.Errorf("%s = %d %s, want 200 %s", label, rec.Code, body(rec), want)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s Content-Type = %q", label, ct)
		}
	}
	back := `{"allocations":[{"allocation":"3000000.0000","category_id":"` + catV7 + `"}],"commitments_budget":"3000000.0000","daily_pool":"6000000.0000","monthly_budget":"9000000.0000",` +
		`"period":{"ends_on":"2026-11-30","starts_on":"2026-11-01","year_month":"2026-11"},"unallocated":"6000000.0000"}`
	if got := body(f.do(http.MethodGet, "")); got != back {
		t.Errorf("budget after the restore = %s, want %s", got, back)
	}
}

// The name was released by the delete; a restore into a name that was taken
// since, in either bucket, is 409 NAME_TAKEN, and the Category stays deleted.
func TestRestoreACategoryIntoATakenNameIs409ThroughTheRouter(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	if rec := f.delCategory(catV7); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rec.Code)
	}
	if rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catV7b+`","category_display_name":"RENT","bucket":"daily"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	before := f.categoryRow(t, catV7)

	rec := f.restoreCategory(catV7)
	if rec.Code != http.StatusConflict || body(rec) != `{"code":"NAME_TAKEN"}` {
		t.Errorf("restore = %d %s, want 409 NAME_TAKEN", rec.Code, body(rec))
	}
	if after := f.categoryRow(t, catV7); after != before {
		t.Errorf("a refused restore changed the row:\n%s\n%s", before, after)
	}
}

// A Category an earlier Period held an amount for is refused, with the code and
// no args, and nothing changes. The earlier Period is made by the database
// directly: the router has one Today.
func TestDeleteACategoryHeldInAnotherPeriodIs409ThroughTheRouter(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	for _, q := range []string{
		`INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-10')`,
		`INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation)
		 VALUES ($1, '2026-10', '2026-10-01', $2, 2900000)`,
	} {
		args := []any{f.householdID}
		if strings.Contains(q, "$2") {
			args = append(args, catV7)
		}
		if _, err := f.tdb.Pool.Exec(context.Background(), q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	before := f.categoryRow(t, catV7)
	allocationsBefore := f.allocationRows(t)

	rec := f.delCategory(catV7)
	if rec.Code != http.StatusConflict || body(rec) != `{"code":"CATEGORY_IN_USE"}` {
		t.Errorf("DELETE = %d %s, want 409 CATEGORY_IN_USE", rec.Code, body(rec))
	}
	if after := f.categoryRow(t, catV7); after != before {
		t.Errorf("a refused delete changed the row:\n%s\n%s", before, after)
	}
	if got := f.allocationRows(t); got != allocationsBefore {
		t.Errorf("a refused delete changed an allocation:\n%s\n%s", allocationsBefore, got)
	}
	// Deactivating, which it offers instead, works.
	if rec := f.patchCategory(catV7, `{"active":false}`); rec.Code != http.StatusOK {
		t.Errorf("deactivate = %d, want 200", rec.Code)
	}
}

func (f budgetFixture) allocationRows(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(to_jsonb(a)::text, ';' ORDER BY a.year_month, a.effective_on), '') FROM allocations a`).Scan(&s); err != nil {
		t.Fatalf("read allocations: %v", err)
	}
	return s
}

// Unknown and foreign ids are one answer, for both operations, byte for byte
// (and the same bytes PATCH gives); another Household's rows do not change.
func TestCategoryDeleteAndRestoreNotFoundIsOneAnswer(t *testing.T) {
	f := newBudgetFixture(t)
	other := f.tdb.CreateHousehold(t, "Other")
	theirsID := f.tdb.CreateCategory(t, other, "Theirs", "daily")
	var theirs string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT id::text FROM categories WHERE id = $1`, theirsID).Scan(&theirs); err != nil {
		t.Fatalf("read: %v", err)
	}
	before := f.categoryRow(t, theirs)
	f.createRent(t)

	for name, call := range map[string]func(string) *httptest.ResponseRecorder{
		"DELETE":  func(id string) *httptest.ResponseRecorder { return f.delCategory(id) },
		"restore": func(id string) *httptest.ResponseRecorder { return f.restoreCategory(id) },
	} {
		t.Run(name, func(t *testing.T) {
			ref := call(unknownCategory)
			if ref.Code != http.StatusNotFound || body(ref) != `{"code":"NOT_FOUND"}` {
				t.Fatalf("unknown id = %d %s, want 404 NOT_FOUND", ref.Code, body(ref))
			}
			for _, id := range []string{theirs, strings.ToUpper(theirs), "urn:uuid:" + theirs} {
				rec := call(id)
				if rec.Code != ref.Code || rec.Body.String() != ref.Body.String() || rec.Header().Get("Content-Type") != ref.Header().Get("Content-Type") {
					t.Errorf("%s = %d %s, want exactly the unknown id's %d %s", id, rec.Code, body(rec), ref.Code, body(ref))
				}
			}
		})
	}
	if after := f.categoryRow(t, theirs); after != before {
		t.Errorf("the other Household's row changed:\n%s\n%s", before, after)
	}
	patch := f.patchCategory(unknownCategory, `{}`)
	if rec := f.delCategory(unknownCategory); rec.Body.String() != patch.Body.String() {
		t.Errorf("DELETE 404 = %q, PATCH 404 = %q", rec.Body.String(), patch.Body.String())
	}
}

// Both are protected before anything else about the request is read.
func TestCategoryDeleteAndRestoreWithoutASessionAre401(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	before := f.categoryRow(t, catV7)
	noCookie := func(r *http.Request) { r.Header.Del("Cookie") }

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"DELETE":                    f.delCategory(catV7, noCookie),
		"DELETE, a bad id":          f.delCategory("not-a-uuid", noCookie),
		"DELETE, an unknown id":     f.delCategory(unknownCategory, noCookie),
		"restore":                   f.restoreCategory(catV7, noCookie),
		"restore, a bad id":         f.restoreCategory("not-a-uuid", noCookie),
		"restore, an unknown id":    f.restoreCategory(unknownCategory, noCookie),
		"restore, a malformed path": f.restoreCategory("%2541", noCookie),
		"restore, an empty id":      f.restoreCategory("", noCookie),
	} {
		if rec.Code != http.StatusUnauthorized || body(rec) != `{"code":"UNAUTHORIZED"}` {
			t.Errorf("%s = %d %s, want 401 UNAUTHORIZED", name, rec.Code, body(rec))
		}
	}
	if after := f.categoryRow(t, catV7); after != before {
		t.Errorf("an unauthenticated request changed the row:\n%s\n%s", before, after)
	}
}

// The cross-site guard covers every non-safe method and answers before the
// session, the binder and the handler, so a refused request deletes nothing and
// restores nothing, and an id that would not bind is refused first.
func TestCrossSiteCategoryDeleteAndRestoreAreBlockedAndChangeNothing(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	if rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catV7b+`","category_display_name":"Gone","bucket":"daily"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	if rec := f.delCategory(catV7b); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rec.Code)
	}
	liveBefore, goneBefore := f.categoryRow(t, catV7), f.categoryRow(t, catV7b)

	cross := func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }
	foreignOrigin := func(r *http.Request) {
		r.Header.Del("Sec-Fetch-Site")
		r.Host = "afloat.test"
		r.Header.Set("Origin", "http://evil.example")
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"DELETE, cross-site":                 f.delCategory(catV7, cross),
		"DELETE, a foreign Origin":           f.delCategory(catV7, foreignOrigin),
		"DELETE, cross-site, a bad id":       f.delCategory("not-a-uuid", cross),
		"DELETE, cross-site, no session":     f.delCategory(catV7, cross, func(r *http.Request) { r.Header.Del("Cookie") }),
		"DELETE, cross-site, an unknown id":  f.delCategory(unknownCategory, cross),
		"restore, cross-site":                f.restoreCategory(catV7b, cross),
		"restore, a foreign Origin":          f.restoreCategory(catV7b, foreignOrigin),
		"restore, cross-site, a bad id":      f.restoreCategory("not-a-uuid", cross),
		"restore, cross-site, no session":    f.restoreCategory(catV7b, cross, func(r *http.Request) { r.Header.Del("Cookie") }),
		"restore, cross-site, an unknown id": f.restoreCategory(unknownCategory, cross),
	} {
		if rec.Code != http.StatusForbidden || body(rec) != `{"code":"CROSS_SITE_REQUEST_BLOCKED"}` {
			t.Errorf("%s = %d %s, want 403 CROSS_SITE_REQUEST_BLOCKED", name, rec.Code, body(rec))
		}
	}
	if f.categoryRow(t, catV7) != liveBefore || f.categoryRow(t, catV7b) != goneBefore {
		t.Error("a blocked request changed a row")
	}
}

// #106 item 1 against DELETE, the mutating verb a mis-decoded id would hurt:
// every shape of a path parameter through the real router and the generated
// binder. Every refusal leaves the Category live; every accepted spelling names
// the same Category and deletes it.
func TestDeleteCategoryPathParameterShapes(t *testing.T) {
	const invalid = `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}`

	for _, tc := range []struct {
		name, target string
		method       string
		status       int
		body         string // empty: assert nothing about it
		deleted      bool
	}{
		{"%2541 binds %41", "/api/categories/%2541", "DELETE", 400, invalid, false},
		{"%25 binds %", "/api/categories/%25", "DELETE", 400, invalid, false},
		{"100%25 binds 100%", "/api/categories/100%25", "DELETE", 400, invalid, false},
		{"%2F inside the id", "/api/categories/" + catV7 + "%2F", "DELETE", 400, invalid, false},
		{"%2f lower case", "/api/categories/a%2fb", "DELETE", 400, invalid, false},
		{"a semicolon", "/api/categories/a;b", "DELETE", 400, invalid, false},
		{"%3B", "/api/categories/a%3Bb", "DELETE", 400, invalid, false},
		{"%2e%2e", "/api/categories/%2e%2e", "DELETE", 400, invalid, false},
		{"%5C", "/api/categories/a%5Cb", "DELETE", 400, invalid, false},
		{"%00", "/api/categories/a%00b", "DELETE", 400, invalid, false},
		{"not a uuid", "/api/categories/not-a-uuid", "DELETE", 400, invalid, false},
		{"one digit short", "/api/categories/" + catV7[:len(catV7)-1], "DELETE", 400, invalid, false},

		{"a trailing slash", "/api/categories/" + catV7 + "/", "DELETE", 404, "404 page not found", false},
		{"an empty parameter", "/api/categories/", "DELETE", 404, "404 page not found", false},

		{"HEAD", "/api/categories/" + catV7, "HEAD", 405, "", false},

		{"upper case", "/api/categories/" + strings.ToUpper(catV7), "DELETE", 204, "", true},
		{"percent-encoded first digit", "/api/categories/%30192f0a0-0000-7000-8002-000000000001", "DELETE", 204, "", true},
		{"urn:uuid: prefixed", "/api/categories/urn:uuid:" + catV7, "DELETE", 204, "", true},
		{"braced", "/api/categories/%7B" + catV7 + "%7D", "DELETE", 204, "", true},
		{"32 hex digits", "/api/categories/" + strings.ReplaceAll(catV7, "-", ""), "DELETE", 204, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBudgetFixture(t)
			f.createRent(t)
			before := f.categoryRow(t, catV7)

			rec := f.doAt(tc.method, tc.target, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, body(rec))
			}
			if tc.body != "" && body(rec) != tc.body {
				t.Errorf("body = %s, want %s", body(rec), tc.body)
			}
			if got := f.categoryDeleted(t, catV7); got != tc.deleted {
				t.Errorf("deleted = %v, want %v (the id was %sbound as the Category's)", got, tc.deleted, map[bool]string{true: "not ", false: ""}[tc.deleted])
			}
			if !tc.deleted {
				if after := f.categoryRow(t, catV7); after != before {
					t.Errorf("the row changed:\n%s\n%s", before, after)
				}
			}
		})
	}
}

// The restore route has the same binder: a malformed id is the same 400 (an
// empty one included), a spelling uuid.Parse accepts is the same Category, and a
// trailing slash matches no route.
func TestRestoreCategoryPathParameterShapes(t *testing.T) {
	const invalid = `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}`

	for _, tc := range []struct {
		name, target string
		status       int
		body         string
		restored     bool
	}{
		{"%2541 binds %41", "/api/categories/%2541/restore", 400, invalid, false},
		{"an encoded slash", "/api/categories/a%2fb/restore", 400, invalid, false},
		{"not a uuid", "/api/categories/not-a-uuid/restore", 400, invalid, false},
		{"a trailing slash", "/api/categories/" + catV7 + "/restore/", 404, "404 page not found", false},
		// As for a Pocket's restore (#121): {id} matches the empty segment, so the
		// binder refuses it like any other value that is not a uuid (BOOTSTRAP.md
		// §5.2).
		{"an empty parameter", "/api/categories//restore", 400, invalid, false},
		{"upper case", "/api/categories/" + strings.ToUpper(catV7) + "/restore", 200, "", true},
		{"urn:uuid: prefixed", "/api/categories/urn:uuid:" + catV7 + "/restore", 200, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBudgetFixture(t)
			f.createRent(t)
			if rec := f.delCategory(catV7); rec.Code != http.StatusNoContent {
				t.Fatalf("DELETE = %d", rec.Code)
			}
			before := f.categoryRow(t, catV7)

			rec := f.doAt(http.MethodPost, tc.target, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, body(rec))
			}
			if tc.body != "" && body(rec) != tc.body {
				t.Errorf("body = %s, want %s", body(rec), tc.body)
			}
			if got := !f.categoryDeleted(t, catV7); got != tc.restored {
				t.Errorf("restored = %v, want %v", got, tc.restored)
			}
			if !tc.restored {
				if after := f.categoryRow(t, catV7); after != before {
					t.Errorf("the row changed:\n%s\n%s", before, after)
				}
			}
		})
	}
}
