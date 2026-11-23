package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// PATCH /categories/{id} and the allocations of PATCH /budget through the whole
// stack: the session check, the binder, the spec validator and the handler,
// over a real database and a pinned clock. The handlers' own behaviour is
// tested in the category and budget packages; this is what only the router
// decides (#130, PR 3).

const (
	catV7    = catID1
	catV7b   = catID2
	rentBody = `{"id":"` + catID1 + `","category_display_name":"Rent","bucket":"commitment","allocation":"3000000"}`
)

func (f budgetFixture) patchCategory(id, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.doAt(http.MethodPatch, "/api/categories/"+id, body, mutate...)
}

func (f budgetFixture) createRent(t *testing.T) {
	t.Helper()
	if rec := f.doAt(http.MethodPost, "/api/categories", rentBody); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
}

func (f budgetFixture) categoryRow(t *testing.T, id string) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT to_jsonb(c)::text FROM categories c WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read category: %v", err)
	}
	return s
}

func TestUpdateCategoryRenamesAndTogglesActiveThroughTheRouter(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)

	rec := f.patchCategory(catV7, `{"category_display_name":"  Housing "}`)
	want := `{"active":true,"bucket":"commitment","category_display_name":"Housing","id":"` + catV7 + `"}`
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != want {
		t.Errorf("rename = %d %s, want 200 %s", rec.Code, got, want)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}

	rec = f.patchCategory(catV7, `{"active":false}`)
	want = `{"active":false,"bucket":"commitment","category_display_name":"Housing","id":"` + catV7 + `"}`
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != want {
		t.Errorf("deactivate = %d %s, want 200 %s", rec.Code, got, want)
	}

	// `{}` is a no-op that answers the Category as it stands.
	before := f.categoryRow(t, catV7)
	rec = f.patchCategory(catV7, `{}`)
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != want {
		t.Errorf("{} = %d %s, want 200 %s", rec.Code, got, want)
	}
	if f.categoryRow(t, catV7) == "" || before == "" {
		t.Error("row vanished")
	}

	// The allocation stayed with the Category through both.
	rec = f.do(http.MethodGet, "")
	if !strings.Contains(rec.Body.String(), `"commitments_budget":"3000000.0000"`) {
		t.Errorf("budget after a rename and a deactivation = %s, want the allocation kept", rec.Body.String())
	}
}

func TestUpdateCategoryRefusals(t *testing.T) {
	long256 := strings.Repeat("a", 256)
	for _, tc := range []struct {
		name, body string
		code       string
		args       map[string]any
	}{
		{"name over the outer bound", `{"category_display_name":"` + long256 + `"}`, "VALIDATION", map[string]any{"field": "category_display_name", "rule": "max"}},
		{"blank name", `{"category_display_name":"  "}`, "VALIDATION", map[string]any{"field": "category_display_name", "rule": "required"}},
		{"an empty name", `{"category_display_name":""}`, "VALIDATION", map[string]any{"field": "category_display_name", "rule": "required"}},
		{"61 code points", `{"category_display_name":"` + strings.Repeat("a", 61) + `"}`, "VALIDATION", map[string]any{"field": "category_display_name", "rule": "max"}},
		{"NUL in the name", `{"category_display_name":"Re\u0000nt"}`, "VALIDATION", map[string]any{"field": "category_display_name", "rule": "pattern"}},
		{"a bad name beside a deactivation", `{"category_display_name":" ","active":false}`, "VALIDATION", map[string]any{"field": "category_display_name", "rule": "required"}},

		// The bucket is fixed at creation: it is not a property of the request.
		{"bucket", `{"bucket":"daily"}`, "INVALID_JSON_BODY", nil},
		{"bucket beside a valid rename", `{"category_display_name":"Housing","bucket":"daily"}`, "INVALID_JSON_BODY", nil},
		{"the allocation belongs to the Budget", `{"allocation":"1"}`, "INVALID_JSON_BODY", nil},
		{"null name", `{"category_display_name":null}`, "INVALID_JSON_BODY", nil},
		{"null active", `{"active":null}`, "INVALID_JSON_BODY", nil},
		{"active as a string", `{"active":"false"}`, "INVALID_JSON_BODY", nil},
		{"active as a number", `{"active":0}`, "INVALID_JSON_BODY", nil},
		{"name as a number", `{"category_display_name":5}`, "INVALID_JSON_BODY", nil},
		{"id", `{"id":"` + catV7b + `"}`, "INVALID_JSON_BODY", nil},
		{"household_id", `{"household_id":"` + catV7b + `"}`, "INVALID_JSON_BODY", nil},
		{"deleted_at", `{"deleted_at":null}`, "INVALID_JSON_BODY", nil},
		{"the bare name", `{"name":"Rent"}`, "INVALID_JSON_BODY", nil},
		{"a bad name beside an unknown property", `{"category_display_name":" ","colour":"red"}`, "INVALID_JSON_BODY", nil},
		{"malformed JSON", `{"active":`, "INVALID_JSON_BODY", nil},
		{"not an object", `[]`, "INVALID_JSON_BODY", nil},
		{"JSON null", `null`, "INVALID_JSON_BODY", nil},
		{"no body", ``, "INVALID_JSON_BODY", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBudgetFixture(t)
			f.createRent(t)
			before := f.categoryRow(t, catV7)

			rec := f.patchCategory(catV7, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			var got struct {
				Code string         `json:"code"`
				Args map[string]any `json:"args"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body %s: %v", rec.Body.String(), err)
			}
			if got.Code != tc.code {
				t.Errorf("code = %s, want %s (body %s)", got.Code, tc.code, rec.Body.String())
			}
			if len(tc.args) > 0 && (got.Args["field"] != tc.args["field"] || got.Args["rule"] != tc.args["rule"]) {
				t.Errorf("args = %v, want %v", got.Args, tc.args)
			}
			if after := f.categoryRow(t, catV7); after != before {
				t.Errorf("a refused update changed the row:\n%s\n%s", before, after)
			}
		})
	}
}

func TestUpdateCategoryNameTakenAndNotFoundOnTheWire(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	if rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catV7b+`","category_display_name":"Food","bucket":"daily"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	other := f.tdb.CreateHousehold(t, "Other")
	theirs := f.tdb.CreateCategory(t, other, "Theirs", "daily")

	rec := f.patchCategory(catV7b, `{"category_display_name":"rent"}`)
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusConflict || got != `{"code":"NAME_TAKEN"}` {
		t.Errorf("rename into a held name = %d %s, want 409 NAME_TAKEN", rec.Code, got)
	}

	// Unknown and another Household's: the same bytes, `{}` included.
	var first string
	for _, id := range []string{"0192f0a0-0000-7000-8002-0000000000ff", theirs.String()} {
		for _, body := range []string{`{}`, `{"active":false}`, `{"category_display_name":"Mine"}`} {
			rec := f.patchCategory(id, body)
			got := strings.TrimSpace(rec.Body.String())
			if rec.Code != http.StatusNotFound || got != `{"code":"NOT_FOUND"}` {
				t.Errorf("PATCH %s %s = %d %s, want 404 NOT_FOUND", id, body, rec.Code, got)
			}
			if first == "" {
				first = got
			}
			if got != first {
				t.Errorf("answer differs: %s vs %s", got, first)
			}
		}
	}
	var name string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT category_display_name FROM categories WHERE id = $1`, theirs).Scan(&name); err != nil || name != "Theirs" {
		t.Errorf("the other Household's Category = %q %v", name, err)
	}
}

// BOOTSTRAP §4: the session first, then the id's shape, then the body.
func TestUpdateCategoryWithoutASessionIs401BeforeAnythingElse(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	before := f.categoryRow(t, catV7)
	noCookie := func(r *http.Request) { r.Header.Del("Cookie") }

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"a valid request":   f.patchCategory(catV7, `{"active":false}`, noCookie),
		"a bad body":        f.patchCategory(catV7, `{"active":`, noCookie),
		"a bad id":          f.patchCategory("not-a-uuid", `{"active":false}`, noCookie),
		"a bad id and body": f.patchCategory("not-a-uuid", `{"active":`, noCookie),
		"an unknown id":     f.patchCategory("0192f0a0-0000-7000-8002-0000000000ff", `{}`, noCookie),
	} {
		if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusUnauthorized || got != `{"code":"UNAUTHORIZED"}` {
			t.Errorf("%s = %d %s, want 401 UNAUTHORIZED", name, rec.Code, got)
		}
	}
	if after := f.categoryRow(t, catV7); after != before {
		t.Errorf("an unauthenticated update changed the row:\n%s\n%s", before, after)
	}
}

func TestUpdateCategoryCrossSiteIsBlockedAndChangesNothing(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	before := f.categoryRow(t, catV7)

	rec := f.patchCategory(catV7, `{"active":false}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "CROSS_SITE_REQUEST_BLOCKED") {
		t.Errorf("cross-site PATCH = %d %s, want 403 CROSS_SITE_REQUEST_BLOCKED", rec.Code, rec.Body.String())
	}
	if after := f.categoryRow(t, catV7); after != before {
		t.Errorf("a blocked request changed the row:\n%s\n%s", before, after)
	}
}

// The {id} path serves PATCH only for now (DELETE and restore are the next
// slice's). HEAD has no GET to answer it.
func TestCategoryPathServesOnlyPatch(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	before := f.categoryRow(t, catV7)
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := f.doAt(method, "/api/categories/"+catV7, `{"active":false}`)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /categories/{id} = %d, want 405", method, rec.Code)
		}
	}
	if after := f.categoryRow(t, catV7); after != before {
		t.Errorf("a refused method changed the row:\n%s\n%s", before, after)
	}
}

// The id's shapes, through the binder (#106): the first {id} route for a
// Category, so the same table as a Pocket's, cut to what differs by id.
func TestUpdateCategoryPathParameterShapes(t *testing.T) {
	const invalid = `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}`
	for _, tc := range []struct {
		name, target string
		status       int
		body         string
		unchanged    bool
	}{
		{"%2541 binds %41", "/api/categories/%2541", 400, invalid, true},
		{"%2F inside the id", "/api/categories/" + catV7 + "%2F", 400, invalid, true},
		{"a semicolon", "/api/categories/a;b", 400, invalid, true},
		{"%00", "/api/categories/a%00b", 400, invalid, true},
		{"not a uuid", "/api/categories/not-a-uuid", 400, invalid, true},
		{"a trailing slash", "/api/categories/" + catV7 + "/", 404, "404 page not found", true},
		{"an empty parameter", "/api/categories/", 404, "404 page not found", true},
		{"upper case", "/api/categories/" + strings.ToUpper(catV7), 200, "", false},
		{"percent-encoded first digit", "/api/categories/%30192f0a0-0000-7000-8002-000000000001", 200, "", false},
		{"32 hex digits", "/api/categories/" + strings.ReplaceAll(catV7, "-", ""), 200, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBudgetFixture(t)
			f.createRent(t)
			before := f.categoryRow(t, catV7)

			rec := f.doAt(http.MethodPatch, tc.target, `{"active":false}`)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body.String())
			}
			if tc.body != "" && strings.TrimSpace(rec.Body.String()) != tc.body {
				t.Errorf("body = %s, want %s", rec.Body.String(), tc.body)
			}
			after := f.categoryRow(t, catV7)
			if tc.unchanged && after != before {
				t.Errorf("the row changed:\n%s\n%s", before, after)
			}
			if !tc.unchanged && after == before {
				t.Errorf("the row did not change: the id was not bound as the same Category")
			}
		})
	}
}

// ---- PATCH /budget: allocations --------------------------------------------

func TestPatchBudgetSetsAllocationsAndAnswersTheWholeBudgetThroughTheRouter(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	if rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catV7b+`","category_display_name":"Food","bucket":"daily"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}

	rec := f.do(http.MethodPatch, `{"monthly_budget":"9000000","allocations":[{"category_id":"`+catV7+`","allocation":"3500000"},{"category_id":"`+catV7b+`","allocation":"2000000.5"}]}`)
	want := `{"allocations":[{"allocation":"3500000.0000","category_id":"` + catV7 + `"},{"allocation":"2000000.5000","category_id":"` + catV7b + `"}],` +
		`"commitments_budget":"3500000.0000","daily_pool":"5500000.0000","monthly_budget":"9000000.0000",` +
		`"period":{"ends_on":"2026-11-30","starts_on":"2026-11-01","year_month":"2026-11"},"unallocated":"3499999.5000"}`
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != want {
		t.Errorf("patch = %d %s\nwant     200 %s", rec.Code, got, want)
	}

	// null clears a Daily Category and is refused for a Commitment.
	rec = f.do(http.MethodPatch, `{"allocations":[{"category_id":"`+catV7b+`","allocation":null}]}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), catV7b) {
		t.Errorf("null for a Daily Category = %d %s, want 200 without it", rec.Code, rec.Body.String())
	}
	before := f.writtenCategories(t)
	rec = f.do(http.MethodPatch, `{"allocations":[{"category_id":"`+catV7+`","allocation":null}]}`)
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusBadRequest || got != `{"args":{"field":"allocation","rule":"required"},"code":"VALIDATION"}` {
		t.Errorf("null for a Commitment = %d %s, want 400 VALIDATION allocation required", rec.Code, got)
	}
	if after := f.writtenCategories(t); after != before {
		t.Errorf("a refused request wrote:\n%s\n%s", before, after)
	}
}

func TestPatchBudgetRefusesEveryAllocationShapeTheContractDoesNotAllow(t *testing.T) {
	pattern := func(field string) string {
		return `{"code":"VALIDATION","args":{"field":"` + field + `","rule":"pattern"}}`
	}
	required := func(field string) string {
		return `{"code":"VALIDATION","args":{"field":"` + field + `","rule":"required"}}`
	}
	const invalid = `{"code":"INVALID_JSON_BODY"}`
	item := func(allocation string) string {
		return `{"allocations":[{"category_id":"` + catV7 + `","allocation":` + allocation + `}]}`
	}
	for _, tc := range []struct {
		name, body, want string
	}{
		// The amount spells as a Monthly Budget does: one rule, MoneyInput's.
		{"exponent notation", item(`"3e6"`), pattern("allocations.0.allocation")},
		{"a negative", item(`"-1"`), pattern("allocations.0.allocation")},
		{"five decimals: refused, never rounded", item(`"1.00001"`), pattern("allocations.0.allocation")},
		{"a thousands separator", item(`"1,000"`), pattern("allocations.0.allocation")},
		{"a leading space", item(`" 5"`), pattern("allocations.0.allocation")},
		{"17 integer digits", item(`"10000000000000000"`), pattern("allocations.0.allocation")},
		{"the empty string", item(`""`), pattern("allocations.0.allocation")},
		{"a JSON number", item(`3000000`), invalid},
		{"a boolean", item(`true`), invalid},
		// An absent key is not a null: it is a contract failure.
		{"allocation absent", `{"allocations":[{"category_id":"` + catV7 + `"}]}`, required("allocations.0.allocation")},
		{"category_id absent", `{"allocations":[{"allocation":"1"}]}`, required("allocations.0.category_id")},
		{"the second item's amount", `{"allocations":[{"category_id":"` + catV7 + `","allocation":"1"},{"category_id":"` + catV7b + `","allocation":"x"}]}`, pattern("allocations.1.allocation")},
		{"the least field first", `{"allocations":[{"category_id":"` + catV7 + `","allocation":"1.00001"}],"monthly_budget":"x"}`, pattern("allocations.0.allocation")},
		{"category_id not a uuid", `{"allocations":[{"category_id":"not-a-uuid","allocation":"1"}]}`, pattern("allocations.0.category_id")},
		{"category_id a v4", `{"allocations":[{"category_id":"0192f0a0-0000-4000-8002-000000000001","allocation":"1"}]}`, pattern("allocations.0.category_id")},
		{"category_id null", `{"allocations":[{"category_id":null,"allocation":"1"}]}`, invalid},
		{"an unknown property of an item", `{"allocations":[{"category_id":"` + catV7 + `","allocation":"1","x":1}]}`, invalid},
		{"null for the list", `{"allocations":null}`, invalid},
		{"an object for the list", `{"allocations":{}}`, invalid},
		{"a null item", `{"allocations":[null]}`, invalid},
		{"a string item", `{"allocations":["1"]}`, invalid},
		{"a bad item and an unknown property", `{"allocations":[{"category_id":"` + catV7 + `","allocation":"-1"}],"x":1}`, invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBudgetFixture(t)
			f.createRent(t)
			before := f.writtenCategories(t)

			rec := f.do(http.MethodPatch, tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tc.want {
				t.Errorf("body = %s, want %s", got, tc.want)
			}
			if after := f.writtenCategories(t); after != before {
				t.Errorf("a refused request wrote:\n%s\n%s", before, after)
			}
		})
	}
}

func TestPatchBudgetAcceptsEveryAllocationSpellingTheContractAllows(t *testing.T) {
	for allocation, want := range map[string]string{
		`"0"`:                     "0.0000",
		`"5"`:                     "5.0000",
		`"5.5"`:                   "5.5000",
		`"0.0001"`:                "0.0001",
		`"1234567890123456"`:      "1234567890123456.0000",
		`"0000000000000005"`:      "5.0000",
		`"9999999999999999.9999"`: "9999999999999999.9999",
	} {
		f := newBudgetFixture(t)
		f.createRent(t)
		rec := f.do(http.MethodPatch, `{"allocations":[{"category_id":"`+catV7+`","allocation":`+allocation+`}]}`)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"allocation":"`+want+`"`) {
			t.Errorf("allocation %s = %d %s, want 200 with %s", allocation, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestPatchBudgetHandlerRefusalsOnTheWire(t *testing.T) {
	f := newBudgetFixture(t)
	f.createRent(t)
	other := f.tdb.CreateHousehold(t, "Other")
	theirs := f.tdb.CreateCategory(t, other, "Theirs", "commitment")
	before := f.writtenCategories(t)

	for name, tc := range map[string]struct{ body, want string }{
		"a repeated category_id": {
			`{"allocations":[{"category_id":"` + catV7 + `","allocation":"1"},{"category_id":"` + catV7 + `","allocation":"2"}]}`,
			`{"args":{"field":"category_id","rule":"unique"},"code":"VALIDATION"}`,
		},
		"an unknown category_id": {
			`{"allocations":[{"category_id":"0192f0a0-0000-7000-8002-0000000000ff","allocation":"1"}]}`,
			`{"args":{"field":"category_id","rule":"invalid"},"code":"VALIDATION"}`,
		},
		"another Household's category_id": {
			`{"allocations":[{"category_id":"` + theirs.String() + `","allocation":"1"}]}`,
			`{"args":{"field":"category_id","rule":"invalid"},"code":"VALIDATION"}`,
		},
		"another Household's, with null": {
			`{"allocations":[{"category_id":"` + theirs.String() + `","allocation":null}]}`,
			`{"args":{"field":"category_id","rule":"invalid"},"code":"VALIDATION"}`,
		},
	} {
		rec := f.do(http.MethodPatch, tc.body)
		if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusBadRequest || got != tc.want {
			t.Errorf("%s = %d %s, want 400 %s", name, rec.Code, got, tc.want)
		}
	}
	if after := f.writtenCategories(t); after != before {
		t.Errorf("a refused request wrote:\n%s\n%s", before, after)
	}
}
