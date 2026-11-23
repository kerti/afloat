package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/kerti/afloat/backend/internal/api"
)

// The Category operations through the whole stack: the session check, the spec
// validator and the generated binding, over a real database and a pinned clock
// (10:00 on 14 Nov 2026 in Jakarta). The handlers' own behaviour is tested in
// the category package; this is what only the router decides.

const (
	catID1 = "0192f0a0-0000-7000-8002-000000000001"
	catID2 = "0192f0a0-0000-7000-8002-000000000002"
)

// writtenCategories is every Category, Budget and allocation row, so "nothing
// was written" is a byte comparison.
func (f budgetFixture) writtenCategories(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT (SELECT coalesce(string_agg(to_jsonb(c)::text, ';'), '') FROM categories c) || '|' ||
		        (SELECT coalesce(string_agg(to_jsonb(b)::text, ';'), '') FROM budgets b) || '|' ||
		        (SELECT coalesce(string_agg(to_jsonb(a)::text, ';'), '') FROM allocations a)`).Scan(&s); err != nil {
		t.Fatalf("read categories: %v", err)
	}
	return s
}

func TestCreateAndListCategoriesThroughTheRouter(t *testing.T) {
	f := newBudgetFixture(t)

	rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catID1+`","category_display_name":"Rent","bucket":"commitment","allocation":"3000000"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	want := `{"active":true,"bucket":"commitment","category_display_name":"Rent","id":"` + catID1 + `"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("created body = %s, want %s", got, want)
	}

	rec = f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catID2+`","category_display_name":"Groceries","bucket":"daily"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	rec = f.doAt(http.MethodGet, "/api/categories", "")
	wantList := `{"items":[` + want + `,{"active":true,"bucket":"daily","category_display_name":"Groceries","id":"` + catID2 + `"}]}`
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != wantList {
		t.Errorf("list = %d %s, want 200 %s", rec.Code, got, wantList)
	}

	// The allocation is on the Budget, with the figures it moves.
	rec = f.do(http.MethodGet, "")
	wantBudget := `{"allocations":[{"allocation":"3000000.0000","category_id":"` + catID1 + `"}],"commitments_budget":"3000000.0000",` +
		`"period":{"ends_on":"2026-11-30","starts_on":"2026-11-01","year_month":"2026-11"}}`
	if got := strings.TrimSpace(rec.Body.String()); got != wantBudget {
		t.Errorf("budget = %s, want %s", got, wantBudget)
	}
}

func TestAnEmptyHouseholdsCategoriesAreItemsAndNotNull(t *testing.T) {
	f := newBudgetFixture(t)
	rec := f.doAt(http.MethodGet, "/api/categories", "")
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != `{"items":[]}` {
		t.Errorf("list = %d %s, want 200 {\"items\":[]}", rec.Code, got)
	}
}

func TestACategoryReplayAndConflictsOnTheWire(t *testing.T) {
	f := newBudgetFixture(t)
	body := `{"id":"` + catID1 + `","category_display_name":"Rent","bucket":"commitment","allocation":"3000000"}`
	if rec := f.doAt(http.MethodPost, "/api/categories", body); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	before := f.writtenCategories(t)

	// A replay: 200, the stored Category, nothing applied (a different allocation too).
	rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catID1+`","category_display_name":"Other","bucket":"daily","allocation":"1"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"category_display_name":"Rent"`) {
		t.Errorf("replay = %d %s, want 200 with the stored Category", rec.Code, rec.Body.String())
	}
	// The same id in upper case is the same Category.
	rec = f.doAt(http.MethodPost, "/api/categories", strings.Replace(body, catID1, strings.ToUpper(catID1), 1))
	if rec.Code != http.StatusOK {
		t.Errorf("upper-case replay = %d, want 200", rec.Code)
	}
	// A name clash is NAME_TAKEN whatever the bucket.
	rec = f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catID2+`","category_display_name":"rent","bucket":"daily"}`)
	if rec.Code != http.StatusConflict || strings.TrimSpace(rec.Body.String()) != `{"code":"NAME_TAKEN"}` {
		t.Errorf("name clash = %d %s, want 409 NAME_TAKEN", rec.Code, rec.Body.String())
	}
	if after := f.writtenCategories(t); after != before {
		t.Errorf("replays and conflicts wrote:\n%s\n%s", before, after)
	}
}

// The failure order is the contract's, then the name, then the Commitment's
// allocation, and each refusal writes nothing: not the Category, not the
// allocation, not the Period's marker.
func TestCreateCategoryRefusals(t *testing.T) {
	id := `"id":"` + catID1 + `"`
	validation := func(field, rule string) string {
		return `{"code":"VALIDATION","args":{"field":"` + field + `","rule":"` + rule + `"}}`
	}
	invalid := `{"code":"INVALID_JSON_BODY"}`
	for _, tc := range []struct {
		name, body, want string
	}{
		// The name rule.
		{"a blank name", `{` + id + `,"category_display_name":"   ","bucket":"daily"}`, validation("category_display_name", "required")},
		{"an empty name", `{` + id + `,"category_display_name":"","bucket":"daily"}`, validation("category_display_name", "required")},
		{"61 code points", `{` + id + `,"category_display_name":"` + strings.Repeat("a", 61) + `","bucket":"daily"}`, validation("category_display_name", "max")},
		{"beyond the outer bound", `{` + id + `,"category_display_name":"` + strings.Repeat("a", 256) + `","bucket":"daily"}`, validation("category_display_name", "max")},
		{"a missing name", `{` + id + `,"bucket":"daily"}`, validation("category_display_name", "required")},
		{"a missing bucket", `{` + id + `,"category_display_name":"Rent"}`, validation("bucket", "required")},
		{"a missing id", `{"category_display_name":"Rent","bucket":"daily"}`, validation("id", "required")},
		{"a v4 id", `{"id":"4b5e1b0e-3a0f-4c5b-8d6f-0a1b2c3d4e5f","category_display_name":"Rent","bucket":"daily"}`, validation("id", "pattern")},
		{"the nil UUID", `{"id":"00000000-0000-0000-0000-000000000000","category_display_name":"Rent","bucket":"daily"}`, validation("id", "pattern")},
		// A Commitment's allocation, after the name.
		{"a Commitment with no allocation", `{` + id + `,"category_display_name":"Rent","bucket":"commitment"}`, validation("allocation", "required")},
		{"a Commitment with a null allocation", `{` + id + `,"category_display_name":"Rent","bucket":"commitment","allocation":null}`, validation("allocation", "required")},
		{"a blank name beats a missing allocation", `{` + id + `,"category_display_name":" ","bucket":"commitment"}`, validation("category_display_name", "required")},
		// Not the declared shape.
		{"a bucket outside the enum", `{` + id + `,"category_display_name":"Rent","bucket":"weekly"}`, invalid},
		{"a bucket in the wrong case", `{` + id + `,"category_display_name":"Rent","bucket":"Daily"}`, invalid},
		{"a null bucket", `{` + id + `,"category_display_name":"Rent","bucket":null}`, invalid},
		{"a null name", `{` + id + `,"category_display_name":null,"bucket":"daily"}`, invalid},
		{"a numeric name", `{` + id + `,"category_display_name":5,"bucket":"daily"}`, invalid},
		{"active is not a property of a create", `{` + id + `,"category_display_name":"Rent","bucket":"daily","active":false}`, invalid},
		{"household_id is not a property", `{` + id + `,"category_display_name":"Rent","bucket":"daily","household_id":"0192f0a0-0000-7000-8000-000000000002"}`, invalid},
		{"deleted_at is not a property", `{` + id + `,"category_display_name":"Rent","bucket":"daily","deleted_at":null}`, invalid},
		{"a Pocket's name is not a Category's", `{` + id + `,"pocket_display_name":"Rent","bucket":"daily"}`, invalid},
		{"malformed JSON", `{` + id + `,`, invalid},
		{"an array body", `[]`, invalid},
		{"a JSON null body", `null`, invalid},
		{"an empty body", ``, invalid},
		// A bad body beats a bad allocation, "whatever else is wrong with it".
		{"an unknown property and a bad allocation", `{` + id + `,"category_display_name":"Rent","bucket":"daily","allocation":"-1","x":1}`, invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBudgetFixture(t)
			before := f.writtenCategories(t)

			rec := f.doAt(http.MethodPost, "/api/categories", tc.body, func(r *http.Request) { r.Header.Set("Content-Type", "application/json") })

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			if !sameJSON(t, rec.Body.String(), tc.want) {
				t.Errorf("body = %s, want %s", strings.TrimSpace(rec.Body.String()), tc.want)
			}
			if after := f.writtenCategories(t); after != before {
				t.Errorf("a refused request wrote: %q -> %q", before, after)
			}
		})
	}
}

// sameJSON compares two JSON documents by value: the validator's answers and the
// handlers' are written by different encoders and order their keys differently.
func sameJSON(t *testing.T, got, want string) bool {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("not JSON: %s", got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("not JSON: %s", want)
	}
	return reflect.DeepEqual(g, w)
}

// The money spelling table, for an allocation (#130 C10): the same rule as the
// Monthly Budget's, and a null where a Commitment needs a value is the
// Commitment rule's, not a shape error.
func TestCreateCategoryRefusesEveryAllocationSpellingTheContractDoesNotAllow(t *testing.T) {
	validation := `{"code":"VALIDATION","args":{"field":"allocation","rule":"pattern"}}`
	invalid := `{"code":"INVALID_JSON_BODY"}`
	for _, bucket := range []string{"commitment", "daily"} {
		for _, tc := range []struct {
			name, value, want string
		}{
			{"exponent notation", `"9e6"`, validation},
			{"an upper-case exponent", `"9E6"`, validation},
			{"a negative", `"-1"`, validation},
			{"a negative zero", `"-0"`, validation},
			{"a plus sign", `"+5"`, validation},
			{"five decimals: refused, never rounded", `"1.00001"`, validation},
			{"a leading space", `" 5"`, validation},
			{"a trailing space", `"5 "`, validation},
			{"a thousands separator", `"1,000"`, validation},
			{"a decimal comma", `"1,5"`, validation},
			{"17 integer digits", `"10000000000000000"`, validation},
			{"the empty string", `""`, validation},
			{"a leading point", `".5"`, validation},
			{"a trailing point", `"5."`, validation},
			{"hexadecimal", `"0x10"`, validation},
			{"a currency symbol", `"Rp5"`, validation},
			{"a JSON number", `9000000`, invalid},
			{"a JSON number in exponent notation", `9e6`, invalid},
			{"a boolean", `true`, invalid},
			{"an object", `{"amount":"5"}`, invalid},
		} {
			t.Run(bucket+" "+tc.name, func(t *testing.T) {
				f := newBudgetFixture(t)
				before := f.writtenCategories(t)

				rec := f.doAt(http.MethodPost, "/api/categories",
					`{"id":"`+catID1+`","category_display_name":"Rent","bucket":"`+bucket+`","allocation":`+tc.value+`}`)

				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
				}
				if !sameJSON(t, rec.Body.String(), tc.want) {
					t.Errorf("body = %s, want %s", strings.TrimSpace(rec.Body.String()), tc.want)
				}
				if after := f.writtenCategories(t); after != before {
					t.Errorf("a refused request wrote: %q -> %q", before, after)
				}
			})
		}
	}
}

func TestCreateCategoryAcceptsEverySpellingTheContractAllows(t *testing.T) {
	for in, want := range map[string]string{
		"0":                     "0.0000",
		"0.0001":                "0.0001",
		"3000000":               "3000000.0000",
		"3000000.5":             "3000000.5000",
		"3000000.5000":          "3000000.5000",
		"0000000000000001":      "1.0000",
		"9999999999999999.9999": "9999999999999999.9999",
	} {
		t.Run(in, func(t *testing.T) {
			f := newBudgetFixture(t)
			rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catID1+`","category_display_name":"Rent","bucket":"commitment","allocation":"`+in+`"}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
			}
			if got := f.doAt(http.MethodGet, "/api/budget", "").Body.String(); !strings.Contains(got, `"allocation":"`+want+`"`) {
				t.Errorf("budget = %s, want the allocation %s", got, want)
			}
		})
	}
}

// A null allocation is "none" for a Daily Category and the Commitment rule's
// refusal for a Commitment.
func TestANullAllocationIsNoneForADailyCategory(t *testing.T) {
	f := newBudgetFixture(t)
	rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catID1+`","category_display_name":"Groceries","bucket":"daily","allocation":null}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if got := f.writtenCategories(t); !strings.HasSuffix(got, "||") {
		t.Errorf("a null allocation wrote a Budget row: %s", got)
	}
}

func TestCategoriesNeedASession(t *testing.T) {
	f := newBudgetFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		body := ""
		if method == http.MethodPost {
			body = `{"id":"` + catID1 + `","category_display_name":"Rent","bucket":"daily"}`
		}
		rec := f.doAt(method, "/api/categories", body, func(r *http.Request) { r.Header.Del("Cookie") })
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session = %d, want 401", method, rec.Code)
		}
	}
	// A refused body with no session is still 401: nothing is learned about the validator.
	rec := f.doAt(http.MethodPost, "/api/categories", `{"bucket":"nope"}`, func(r *http.Request) { r.Header.Del("Cookie") })
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a bad body without a session = %d, want 401", rec.Code)
	}
	if got := f.writtenCategories(t); got != "||" {
		t.Errorf("an unauthenticated request wrote: %q", got)
	}
}

func TestCategoriesCrossSitePostIsBlockedAndWritesNothing(t *testing.T) {
	f := newBudgetFixture(t)
	rec := f.doAt(http.MethodPost, "/api/categories", `{"id":"`+catID1+`","category_display_name":"Rent","bucket":"commitment","allocation":"1"}`,
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "CROSS_SITE_REQUEST_BLOCKED") {
		t.Errorf("cross-site POST = %d %s, want 403 CROSS_SITE_REQUEST_BLOCKED", rec.Code, rec.Body.String())
	}
	if got := f.writtenCategories(t); got != "||" {
		t.Errorf("a blocked request wrote: %q", got)
	}
}

// The collection serves GET, HEAD and POST; PATCH, PUT and DELETE belong to
// {id} and later slices, and are not served here.
func TestCategoriesCollectionServesOnlyGetHeadAndPost(t *testing.T) {
	f := newBudgetFixture(t)
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := f.doAt(method, "/api/categories", `{"category_display_name":"x"}`)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /categories = %d, want 405", method, rec.Code)
		}
	}
	if got := f.writtenCategories(t); got != "||" {
		t.Errorf("a refused method wrote: %q", got)
	}
}

// MoneyInputOrNull repeats MoneyInput's constraints (OpenAPI 3.0 ignores the
// siblings of a $ref), so the two must never drift: a Monthly Budget and an
// allocation are spelled by one rule. Only nullable (and the prose) may differ.
func TestMoneyInputOrNullDiffersFromMoneyInputOnlyInNullable(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	plain, orNull := *spec.Components.Schemas["MoneyInput"].Value, *spec.Components.Schemas["MoneyInputOrNull"].Value
	if plain.Pattern == "" {
		t.Fatal("MoneyInput carries no pattern")
	}
	if plain.Nullable || !orNull.Nullable {
		t.Errorf("nullable: MoneyInput %v, MoneyInputOrNull %v; want false and true", plain.Nullable, orNull.Nullable)
	}
	for _, s := range []*openapi3.Schema{&plain, &orNull} {
		s.Nullable = false
		s.Description = ""
		s.Example = nil
		s.Title = ""
		s.Extensions = nil
	}
	if !reflect.DeepEqual(plain, orNull) {
		t.Errorf("MoneyInput and MoneyInputOrNull differ beyond nullable and prose:\n%+v\n%+v", plain, orNull)
	}
}
