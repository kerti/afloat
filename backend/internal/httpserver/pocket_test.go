package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/pocket"
	"github.com/kerti/afloat/backend/internal/system"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// pocketFixture is the whole stack over a real database, with one signed-in
// User, so the Pocket operations are exercised through the session check, the
// spec validator and the generated binding rather than as bare handlers.
type pocketFixture struct {
	tdb         *testutil.TestDB
	srv         http.Handler
	householdID pgtype.UUID
	cookie      string
}

const (
	pocketV7  = "0192f0a0-0000-7000-8000-000000000001"
	pocketV7b = "0192f0a0-0000-7000-8000-000000000002"
)

func newPocketFixture(t *testing.T) pocketFixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	householdID := tdb.CreateHousehold(t, "Home")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "A")

	const token = "a-session-token-for-the-pocket-tests"
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
			Querier:            tdb.Queries,
			Beginner:           tdb.Pool,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
		Pocket:         pocket.New(pocket.Deps{Querier: tdb.Queries}),
		HandlerTimeout: testHandlerTimeout,
	})
	return pocketFixture{tdb: tdb, srv: srv, householdID: householdID, cookie: auth.SessionCookieName + "=" + token}
}

func (f pocketFixture) do(method, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/pockets", strings.NewReader(body))
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

func (f pocketFixture) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM pockets`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestCreateAndListPocketsThroughTheRouter(t *testing.T) {
	f := newPocketFixture(t)

	rec := f.do(http.MethodPost, `{"id":"`+pocketV7+`","pocket_display_name":"  OVO "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got, want := strings.TrimSpace(rec.Body.String()), `{"active":true,"id":"`+pocketV7+`","pocket_display_name":"OVO"}`; got != want {
		t.Errorf("created body = %s, want %s", got, want)
	}

	// The replay: 200, the stored Pocket, whatever the body says.
	rec = f.do(http.MethodPost, `{"id":"`+pocketV7+`","pocket_display_name":"Something else"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pocket_display_name":"OVO"`) {
		t.Errorf("replay = %d %s, want 200 with the stored OVO", rec.Code, rec.Body.String())
	}

	rec = f.do(http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got, want := strings.TrimSpace(rec.Body.String()), `{"items":[{"active":true,"id":"`+pocketV7+`","pocket_display_name":"OVO"}]}`; got != want {
		t.Errorf("list body = %s, want %s", got, want)
	}
}

func TestAnEmptyHouseholdsListIsItemsAndNotNull(t *testing.T) {
	f := newPocketFixture(t)
	rec := f.do(http.MethodGet, "")
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != `{"items":[]}` {
		t.Errorf("list = %d %s, want 200 {\"items\":[]}", rec.Code, got)
	}
}

// Every refusal answers 400 and writes nothing, whatever the id.
func TestCreatePocketRefusals(t *testing.T) {
	long256 := strings.Repeat("a", 256)
	for _, tc := range []struct {
		name, body string
		code       string
		args       map[string]any
	}{
		// The id's pattern: a canonical dashed UUIDv7, RFC variant.
		{"a v4 id", `{"id":"6ba7b810-9dad-41d1-80b4-00c04fd430c8","pocket_display_name":"OVO"}`, "VALIDATION", map[string]any{"field": "id", "rule": "pattern"}},
		{"the nil UUID", `{"id":"00000000-0000-0000-0000-000000000000","pocket_display_name":"OVO"}`, "VALIDATION", map[string]any{"field": "id", "rule": "pattern"}},
		{"a v7 with the wrong variant", `{"id":"0192f0a0-0000-7000-c000-000000000001","pocket_display_name":"OVO"}`, "VALIDATION", map[string]any{"field": "id", "rule": "pattern"}},
		{"32 hex digits", `{"id":"0192f0a0000070008000000000000001","pocket_display_name":"OVO"}`, "VALIDATION", map[string]any{"field": "id", "rule": "pattern"}},
		{"urn:uuid: prefixed", `{"id":"urn:uuid:` + pocketV7 + `","pocket_display_name":"OVO"}`, "VALIDATION", map[string]any{"field": "id", "rule": "pattern"}},
		{"braced", `{"id":"{` + pocketV7 + `}","pocket_display_name":"OVO"}`, "VALIDATION", map[string]any{"field": "id", "rule": "pattern"}},
		{"not a uuid", `{"id":"not-a-uuid","pocket_display_name":"OVO"}`, "VALIDATION", map[string]any{"field": "id", "rule": "pattern"}},
		// The name's contract rule and the handler's.
		{"name over the outer bound", `{"id":"` + pocketV7 + `","pocket_display_name":"` + long256 + `"}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "max"}},
		{"blank name", `{"id":"` + pocketV7 + `","pocket_display_name":"  "}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "required"}},
		{"61 code points", `{"id":"` + pocketV7 + `","pocket_display_name":"` + strings.Repeat("a", 61) + `"}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "max"}},
		{"NUL in the name", `{"id":"` + pocketV7 + `","pocket_display_name":"O\u0000VO"}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "pattern"}},
		// Contract rules before handler rules: the id's pattern is reported even
		// though the name is also bad.
		{"bad id and blank name", `{"id":"not-a-uuid","pocket_display_name":""}`, "VALIDATION", map[string]any{"field": "id", "rule": "pattern"}},

		// A required property left out is a rule of the contract.
		{"no id", `{"pocket_display_name":"OVO"}`, "VALIDATION", map[string]any{"field": "id", "rule": "required"}},
		{"no name", `{"id":"` + pocketV7 + `"}`, "VALIDATION", map[string]any{"field": "pocket_display_name", "rule": "required"}},

		// Not an instance of the declared shape.
		{"id as a number", `{"id":5,"pocket_display_name":"OVO"}`, "INVALID_JSON_BODY", nil},
		{"null name", `{"id":"` + pocketV7 + `","pocket_display_name":null}`, "INVALID_JSON_BODY", nil},
		{"name as a number", `{"id":"` + pocketV7 + `","pocket_display_name":5}`, "INVALID_JSON_BODY", nil},
		// A new Pocket is always active, and a body names no Household.
		{"active", `{"id":"` + pocketV7 + `","pocket_display_name":"OVO","active":false}`, "INVALID_JSON_BODY", nil},
		{"household_id", `{"id":"` + pocketV7 + `","pocket_display_name":"OVO","household_id":"` + pocketV7b + `"}`, "INVALID_JSON_BODY", nil},
		{"deleted_at", `{"id":"` + pocketV7 + `","pocket_display_name":"OVO","deleted_at":null}`, "INVALID_JSON_BODY", nil},
		{"created_at", `{"id":"` + pocketV7 + `","pocket_display_name":"OVO","created_at":"2026-01-01T00:00:00Z"}`, "INVALID_JSON_BODY", nil},
		{"the bare name", `{"id":"` + pocketV7 + `","name":"OVO"}`, "INVALID_JSON_BODY", nil},
		{"malformed JSON", `{"id":`, "INVALID_JSON_BODY", nil},
		{"not an object", `[]`, "INVALID_JSON_BODY", nil},
		{"JSON null", `null`, "INVALID_JSON_BODY", nil},
		{"no body", ``, "INVALID_JSON_BODY", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPocketFixture(t)
			rec := f.do(http.MethodPost, tc.body)
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
			if n := f.count(t); n != 0 {
				t.Errorf("a refused create wrote %d row(s)", n)
			}
		})
	}
}

// An upper-case UUIDv7 passes the pattern and is stored as the same id.
func TestAnUpperCaseV7IDIsTheSamePocketAsItsLowerCase(t *testing.T) {
	f := newPocketFixture(t)
	upper := strings.ToUpper(pocketV7)
	if rec := f.do(http.MethodPost, `{"id":"`+upper+`","pocket_display_name":"OVO"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s), want 201", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodPost, `{"id":"`+pocketV7+`","pocket_display_name":"OVO"}`); rec.Code != http.StatusOK {
		t.Errorf("replay under the lower-case spelling = %d (%s), want 200", rec.Code, rec.Body.String())
	}
}

func TestCreatePocketConflictsOnTheWire(t *testing.T) {
	f := newPocketFixture(t)
	other := f.tdb.CreateHousehold(t, "Other")
	f.tdb.CreatePocket(t, other, "Theirs")
	var theirsID string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT id::text FROM pockets WHERE household_id = $1`, other).Scan(&theirsID); err != nil {
		t.Fatalf("read: %v", err)
	}

	if rec := f.do(http.MethodPost, `{"id":"`+pocketV7+`","pocket_display_name":"OVO"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	for name, tc := range map[string]struct{ body, want string }{
		"a foreign id": {`{"id":"` + theirsID + `","pocket_display_name":"Mine"}`, `{"code":"ID_TAKEN"}`},
		"a taken name": {`{"id":"` + pocketV7b + `","pocket_display_name":"ovo"}`, `{"code":"NAME_TAKEN"}`},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(http.MethodPost, tc.body)
			if rec.Code != http.StatusConflict || strings.TrimSpace(rec.Body.String()) != tc.want {
				t.Errorf("answer = %d %s, want 409 %s", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

func TestPocketOperationsWithoutASessionAre401BeforeTheBodyIsRead(t *testing.T) {
	f := newPocketFixture(t)
	noCookie := func(r *http.Request) { r.Header.Del("Cookie") }
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"list":                   f.do(http.MethodGet, "", noCookie),
		"create":                 f.do(http.MethodPost, `{"id":"`+pocketV7+`","pocket_display_name":"OVO"}`, noCookie),
		"create with a bad body": f.do(http.MethodPost, `{"id":`, noCookie),
	} {
		if rec.Code != http.StatusUnauthorized || strings.TrimSpace(rec.Body.String()) != `{"code":"UNAUTHORIZED"}` {
			t.Errorf("%s = %d %s, want 401 UNAUTHORIZED", name, rec.Code, rec.Body.String())
		}
	}
	if n := f.count(t); n != 0 {
		t.Errorf("pockets = %d, want 0", n)
	}
}
