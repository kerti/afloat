package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// PATCH /pockets/{id} through the whole stack: the session check, the binder,
// the spec validator and the handler, over a real database (#121, PR 2).

func (f pocketFixture) doAt(method, target, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
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

func (f pocketFixture) patch(id, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.doAt(http.MethodPatch, "/api/pockets/"+id, body, mutate...)
}

// seed inserts a Pocket of the fixture's Household under a fixed id.
func (f pocketFixture) seed(t *testing.T, id, name string) {
	t.Helper()
	if _, err := f.tdb.Pool.Exec(context.Background(),
		`INSERT INTO pockets (id, household_id, pocket_display_name) VALUES ($1, $2, $3)`, id, f.householdID, name); err != nil {
		t.Fatalf("seed pocket: %v", err)
	}
}

// row is the whole row as JSON: every column, byte for byte.
func (f pocketFixture) row(t *testing.T, id string) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT to_jsonb(p)::text FROM pockets p WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read pocket: %v", err)
	}
	return s
}

func body(rec *httptest.ResponseRecorder) string { return strings.TrimSpace(rec.Body.String()) }

func TestUpdatePocketRenamesAndTogglesActiveThroughTheRouter(t *testing.T) {
	f := newPocketFixture(t)
	f.seed(t, pocketV7, "OVO")

	for _, tc := range []struct{ name, body, want string }{
		{"a rename", `{"pocket_display_name":"  Gopay "}`, `{"active":true,"id":"` + pocketV7 + `","pocket_display_name":"Gopay"}`},
		{"a deactivation keeps the name", `{"active":false}`, `{"active":false,"id":"` + pocketV7 + `","pocket_display_name":"Gopay"}`},
		{"repeating it is a no-op", `{"active":false}`, `{"active":false,"id":"` + pocketV7 + `","pocket_display_name":"Gopay"}`},
		{"an inactive Pocket can be renamed", `{"pocket_display_name":"Dana"}`, `{"active":false,"id":"` + pocketV7 + `","pocket_display_name":"Dana"}`},
		{"a reactivation", `{"active":true}`, `{"active":true,"id":"` + pocketV7 + `","pocket_display_name":"Dana"}`},
		{"both in one body", `{"pocket_display_name":"LinkAja","active":false}`, `{"active":false,"id":"` + pocketV7 + `","pocket_display_name":"LinkAja"}`},
		{"an empty body is a no-op", `{}`, `{"active":false,"id":"` + pocketV7 + `","pocket_display_name":"LinkAja"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.patch(pocketV7, tc.body)
			if rec.Code != http.StatusOK || body(rec) != tc.want {
				t.Errorf("answer = %d %s, want 200 %s", rec.Code, body(rec), tc.want)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
		})
	}

	// An inactive Pocket is still listed (the client filters), in place.
	f.seed(t, pocketV7b, "Zeta")
	rec := f.do(http.MethodGet, "")
	want := `{"items":[{"active":false,"id":"` + pocketV7 + `","pocket_display_name":"LinkAja"},{"active":true,"id":"` + pocketV7b + `","pocket_display_name":"Zeta"}]}`
	if body(rec) != want {
		t.Errorf("list = %s, want %s", body(rec), want)
	}
}

// A name that differs only in case from the Pocket's own is a rename, not a
// clash; one held by another Pocket is NAME_TAKEN, inactive or not.
func TestUpdatePocketRenameAgainstTheNameRule(t *testing.T) {
	f := newPocketFixture(t)
	f.seed(t, pocketV7, "OVO")
	f.seed(t, pocketV7b, "Gopay")

	if rec := f.patch(pocketV7, `{"pocket_display_name":"ovo"}`); rec.Code != http.StatusOK || !strings.Contains(body(rec), `"pocket_display_name":"ovo"`) {
		t.Errorf("a rename to its own name in another case = %d %s, want 200", rec.Code, body(rec))
	}
	before := f.row(t, pocketV7)
	if rec := f.patch(pocketV7, `{"pocket_display_name":" GOPAY "}`); rec.Code != http.StatusConflict || body(rec) != `{"code":"NAME_TAKEN"}` {
		t.Errorf("a held name = %d %s, want 409 NAME_TAKEN", rec.Code, body(rec))
	}
	// Nothing changed, the other field of the same body included.
	if rec := f.patch(pocketV7, `{"pocket_display_name":"gopay","active":false}`); rec.Code != http.StatusConflict {
		t.Errorf("a held name beside a deactivation = %d, want 409", rec.Code)
	}
	if after := f.row(t, pocketV7); after != before {
		t.Errorf("a refused rename changed the row:\n%s\n%s", before, after)
	}

	// An inactive Pocket still holds its name.
	if rec := f.patch(pocketV7b, `{"active":false}`); rec.Code != http.StatusOK {
		t.Fatalf("deactivate = %d", rec.Code)
	}
	if rec := f.patch(pocketV7, `{"pocket_display_name":"gopay"}`); rec.Code != http.StatusConflict {
		t.Errorf("a name held by an inactive Pocket = %d, want 409", rec.Code)
	}
	// A deleted Pocket releases its name.
	if _, err := f.tdb.Pool.Exec(context.Background(), `UPDATE pockets SET deleted_at = now() WHERE id = $1`, pocketV7b); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rec := f.patch(pocketV7, `{"pocket_display_name":"gopay"}`); rec.Code != http.StatusOK {
		t.Errorf("a name whose holder is deleted = %d %s, want 200", rec.Code, body(rec))
	}
}

// Every refusal answers 400 and leaves the row as it was. Contract rules first
// (INVALID_JSON_BODY, then VALIDATION), then the handler's name rule.
func TestUpdatePocketRefusals(t *testing.T) {
	long256 := strings.Repeat("a", 256)
	for _, tc := range []struct {
		name, body string
		code       string
		args       map[string]any
	}{
		{"name over the outer bound", `{"pocket_display_name":"` + long256 + `"}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "max"}},
		{"blank name", `{"pocket_display_name":"  "}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "required"}},
		{"an empty name", `{"pocket_display_name":""}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "required"}},
		{"61 code points", `{"pocket_display_name":"` + strings.Repeat("a", 61) + `"}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "max"}},
		{"NUL in the name", `{"pocket_display_name":"O\u0000VO"}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "pattern"}},
		{"a bad name beside a deactivation", `{"pocket_display_name":" ","active":false}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "required"}},

		{"null name", `{"pocket_display_name":null}`, "INVALID_JSON_BODY", nil},
		{"null active", `{"active":null}`, "INVALID_JSON_BODY", nil},
		{"active as a string", `{"active":"false"}`, "INVALID_JSON_BODY", nil},
		{"active as a number", `{"active":0}`, "INVALID_JSON_BODY", nil},
		{"name as a number", `{"pocket_display_name":5}`, "INVALID_JSON_BODY", nil},
		{"id", `{"id":"` + pocketV7b + `"}`, "INVALID_JSON_BODY", nil},
		{"household_id", `{"household_id":"` + pocketV7b + `"}`, "INVALID_JSON_BODY", nil},
		{"deleted_at", `{"deleted_at":null}`, "INVALID_JSON_BODY", nil},
		{"created_at", `{"created_at":"2026-01-01T00:00:00Z"}`, "INVALID_JSON_BODY", nil},
		{"the bare name", `{"name":"OVO"}`, "INVALID_JSON_BODY", nil},
		{"an unknown property beside a valid one", `{"active":false,"colour":"red"}`, "INVALID_JSON_BODY", nil},
		{"a bad name beside an unknown property", `{"pocket_display_name":" ","colour":"red"}`, "INVALID_JSON_BODY", nil},
		{"malformed JSON", `{"active":`, "INVALID_JSON_BODY", nil},
		{"not an object", `[]`, "INVALID_JSON_BODY", nil},
		{"JSON null", `null`, "INVALID_JSON_BODY", nil},
		{"no body", ``, "INVALID_JSON_BODY", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPocketFixture(t)
			f.seed(t, pocketV7, "OVO")
			before := f.row(t, pocketV7)

			rec := f.patch(pocketV7, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, body(rec))
			}
			var got struct {
				Code string         `json:"code"`
				Args map[string]any `json:"args"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body %s: %v", body(rec), err)
			}
			if got.Code != tc.code {
				t.Errorf("code = %s, want %s (body %s)", got.Code, tc.code, body(rec))
			}
			if len(tc.args) > 0 && (got.Args["field"] != tc.args["field"] || got.Args["rule"] != tc.args["rule"]) {
				t.Errorf("args = %v, want %v", got.Args, tc.args)
			}
			if after := f.row(t, pocketV7); after != before {
				t.Errorf("a refused update changed the row:\n%s\n%s", before, after)
			}
		})
	}
}

// An id that is unknown, another Household's or deleted answers one 404, byte
// for byte, for every body: a request does not confirm what it must not.
func TestUpdatePocketNotFoundIsOneAnswer(t *testing.T) {
	f := newPocketFixture(t)
	other := f.tdb.CreateHousehold(t, "Other")
	f.tdb.CreatePocket(t, other, "Theirs")
	var theirs string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT id::text FROM pockets WHERE household_id = $1`, other).Scan(&theirs); err != nil {
		t.Fatalf("read: %v", err)
	}
	f.seed(t, pocketV7, "Gone")
	if _, err := f.tdb.Pool.Exec(context.Background(), `UPDATE pockets SET deleted_at = now() WHERE id = $1`, pocketV7); err != nil {
		t.Fatalf("delete: %v", err)
	}
	theirsBefore, deletedBefore := f.row(t, theirs), f.row(t, pocketV7)

	ids := map[string]string{
		"unknown": "0192f0a0-0000-7000-8000-0000000000ff",
		"foreign": theirs,
		"deleted": pocketV7,
	}
	for _, tc := range []struct{ name, body string }{
		{"a rename", `{"pocket_display_name":"Mine"}`},
		{"a deactivation", `{"active":false}`},
		{"an empty body", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answers := map[string]string{}
			for kind, id := range ids {
				rec := f.patch(id, tc.body)
				if rec.Code != http.StatusNotFound || body(rec) != `{"code":"NOT_FOUND"}` {
					t.Errorf("%s id = %d %s, want 404 NOT_FOUND", kind, rec.Code, body(rec))
				}
				answers[kind] = rec.Header().Get("Content-Type") + "|" + rec.Body.String()
			}
			if answers["unknown"] != answers["foreign"] || answers["unknown"] != answers["deleted"] {
				t.Errorf("the three answers differ: %v", answers)
			}
		})
	}
	// Validation comes before the lookup, so a bad body answers the same for
	// all three, and none is distinguishable.
	var seen []string
	for _, id := range ids {
		rec := f.patch(id, `{"pocket_display_name":" "}`)
		seen = append(seen, rec.Body.String())
		if rec.Code != http.StatusBadRequest {
			t.Errorf("a bad body on %s = %d, want 400", id, rec.Code)
		}
	}
	if seen[0] != seen[1] || seen[1] != seen[2] {
		t.Errorf("a bad body answered differently by id kind: %q", seen)
	}
	if got := f.row(t, theirs); got != theirsBefore {
		t.Errorf("the other Household's row changed:\n%s\n%s", theirsBefore, got)
	}
	if got := f.row(t, pocketV7); got != deletedBefore {
		t.Errorf("the deleted row changed:\n%s\n%s", deletedBefore, got)
	}
}

func TestUpdatePocketWithoutASessionIs401BeforeAnythingElse(t *testing.T) {
	f := newPocketFixture(t)
	f.seed(t, pocketV7, "OVO")
	before := f.row(t, pocketV7)
	noCookie := func(r *http.Request) { r.Header.Del("Cookie") }

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"a valid request":   f.patch(pocketV7, `{"active":false}`, noCookie),
		"a bad body":        f.patch(pocketV7, `{"active":`, noCookie),
		"a bad id":          f.patch("not-a-uuid", `{"active":false}`, noCookie),
		"a bad id and body": f.patch("not-a-uuid", `{"active":`, noCookie),
		"an unknown id":     f.patch("0192f0a0-0000-7000-8000-0000000000ff", `{}`, noCookie),
	} {
		if rec.Code != http.StatusUnauthorized || body(rec) != `{"code":"UNAUTHORIZED"}` {
			t.Errorf("%s = %d %s, want 401 UNAUTHORIZED", name, rec.Code, body(rec))
		}
	}
	if after := f.row(t, pocketV7); after != before {
		t.Errorf("an unauthenticated update changed the row:\n%s\n%s", before, after)
	}
}

// #106 item 1, against the first {id} route: every shape of a path parameter,
// through the real router and the generated binder. The answers were observed
// and are pinned here, and none was a surprise: the shapes #56 calls "a path
// that does not exist" on a route with no parameter (`%2F`, `;`, `%5C`, `%00`,
// `..`) reach the binder on this one and are refused as a malformed id (400),
// so #65's and #106's rulings do not split. Every row leaves the Pocket as it
// was: a mis-decoded id is the hazard, and a write is what it would hurt.
func TestUpdatePocketPathParameterShapes(t *testing.T) {
	const invalid = `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}`
	const updated = `{"active":false,"id":"` + pocketV7 + `","pocket_display_name":"OVO"}`

	for _, tc := range []struct {
		name, target string
		method       string
		status       int
		body         string // empty: assert nothing about it
		unchanged    bool
	}{
		// Decoded once: the first decode of %25 is a percent sign, and what
		// that leaves is not a uuid.
		{"%2541 binds %41", "/api/pockets/%2541", "PATCH", 400, invalid, true},
		{"%25 binds %", "/api/pockets/%25", "PATCH", 400, invalid, true},
		{"100%25 binds 100%", "/api/pockets/100%25", "PATCH", 400, invalid, true},
		{"%2F inside the id", "/api/pockets/" + pocketV7 + "%2F", "PATCH", 400, invalid, true},
		{"%2f lower case", "/api/pockets/a%2fb", "PATCH", 400, invalid, true},
		{"a semicolon", "/api/pockets/a;b", "PATCH", 400, invalid, true},
		{"%3B", "/api/pockets/a%3Bb", "PATCH", 400, invalid, true},
		{"%2e%2e", "/api/pockets/%2e%2e", "PATCH", 400, invalid, true},
		{"%5C", "/api/pockets/a%5Cb", "PATCH", 400, invalid, true},
		{"%00", "/api/pockets/a%00b", "PATCH", 400, invalid, true},
		{"not a uuid", "/api/pockets/not-a-uuid", "PATCH", 400, invalid, true},

		// No route: a bare 404, never the envelope.
		{"a trailing slash", "/api/pockets/" + pocketV7 + "/", "PATCH", 404, "404 page not found", true},
		{"an empty parameter", "/api/pockets/", "PATCH", 404, "404 page not found", true},

		// There is no GET on this path, so HEAD has nothing to answer it.
		{"HEAD", "/api/pockets/" + pocketV7, "HEAD", 405, "", true},

		// The spellings uuid.Parse accepts name the same Pocket, and are
		// accepted: refusing them would take a path pattern, which would also
		// refuse the percent-encoded spelling of a valid id (see PocketId).
		{"upper case", "/api/pockets/" + strings.ToUpper(pocketV7), "PATCH", 200, updated, false},
		{"percent-encoded first digit", "/api/pockets/%30192f0a0-0000-7000-8000-000000000001", "PATCH", 200, updated, false},
		{"urn:uuid: prefixed", "/api/pockets/urn:uuid:" + pocketV7, "PATCH", 200, updated, false},
		{"braced", "/api/pockets/%7B" + pocketV7 + "%7D", "PATCH", 200, updated, false},
		{"32 hex digits", "/api/pockets/" + strings.ReplaceAll(pocketV7, "-", ""), "PATCH", 200, updated, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPocketFixture(t)
			f.seed(t, pocketV7, "OVO")
			before := f.row(t, pocketV7)

			rec := f.doAt(tc.method, tc.target, `{"active":false}`)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, body(rec))
			}
			if tc.body != "" && body(rec) != tc.body {
				t.Errorf("body = %s, want %s", body(rec), tc.body)
			}
			after := f.row(t, pocketV7)
			if tc.unchanged && after != before {
				t.Errorf("the row changed:\n%s\n%s", before, after)
			}
			if !tc.unchanged && after == before {
				t.Errorf("the row did not change: the id was not bound as the same Pocket")
			}
		})
	}
}

// The id's other-Household shape: another Household's id, spelled any way, is
// the same NOT_FOUND as an unknown one.
func TestUpdatePocketTenancyThroughTheRouter(t *testing.T) {
	f := newPocketFixture(t)
	other := f.tdb.CreateHousehold(t, "Other")
	f.tdb.CreatePocket(t, other, "Theirs")
	var theirs string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT id::text FROM pockets WHERE household_id = $1`, other).Scan(&theirs); err != nil {
		t.Fatalf("read: %v", err)
	}
	before := f.row(t, theirs)

	for _, id := range []string{theirs, strings.ToUpper(theirs), "urn:uuid:" + theirs} {
		if rec := f.patch(id, `{"pocket_display_name":"Mine","active":false}`); rec.Code != http.StatusNotFound || body(rec) != `{"code":"NOT_FOUND"}` {
			t.Errorf("%s = %d %s, want 404 NOT_FOUND", id, rec.Code, body(rec))
		}
	}
	if after := f.row(t, theirs); after != before {
		t.Errorf("the other Household's row changed:\n%s\n%s", before, after)
	}
}
