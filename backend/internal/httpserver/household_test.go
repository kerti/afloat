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
	"github.com/kerti/afloat/backend/internal/household"
	"github.com/kerti/afloat/backend/internal/system"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// householdFixture is the whole stack over a real database, with one signed-in
// User, so PATCH /household is exercised through the session check, the
// spec validator and the generated binding rather than as a bare handler.
type householdFixture struct {
	tdb         *testutil.TestDB
	srv         http.Handler
	householdID pgtype.UUID
	cookie      string
}

func newHouseholdFixture(t *testing.T) householdFixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	householdID := tdb.CreateHousehold(t, "Home")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "A")

	const token = "a-session-token-for-the-household-tests"
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
		Household:      household.New(household.Deps{Querier: tdb.Queries}),
		HandlerTimeout: testHandlerTimeout,
	})
	return householdFixture{tdb: tdb, srv: srv, householdID: householdID, cookie: auth.SessionCookieName + "=" + token}
}

// patch sends body as PATCH /api/household with the session cookie.
func (f householdFixture) patch(body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/api/household", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Cookie", f.cookie)
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

// snapshot is the Household's row as JSON, every column, so "unchanged" means
// byte for byte.
func (f householdFixture) snapshot(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT to_jsonb(h)::text FROM households h WHERE id = $1`, f.householdID).Scan(&s); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return s
}

func TestPatchHouseholdThroughTheRouter(t *testing.T) {
	f := newHouseholdFixture(t)

	rec := f.patch(`{"household_display_name":"  Rumah Kita ","reporting_currency":"USD","period_start_day":25}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v", err)
	}
	for k, want := range map[string]any{
		"household_display_name": "Rumah Kita", // trimmed
		"reporting_currency":     "USD",
		"period_start_day":       float64(25),
		"day_starts_at":          "04:00",
		"allowance_mode":         "adaptive",
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if _, present := got["expected_monthly_income"]; present {
		t.Error("expected_monthly_income present for a Household with none")
	}
	if got["id"] == nil {
		t.Error("id missing")
	}
}

// Every refusal answers 400 and writes nothing, including a body whose other
// fields were fine: one UPDATE, all or nothing.
func TestPatchHouseholdRefusals(t *testing.T) {
	long61 := strings.Repeat("a", 61)
	for _, tc := range []struct {
		name, body string
		code       string
		args       map[string]any
	}{
		// The spec validator's rules.
		{"day 0", `{"period_start_day":0}`, "VALIDATION", map[string]any{"field": "period_start_day", "rule": "min"}},
		{"day 29", `{"period_start_day":29}`, "VALIDATION", map[string]any{"field": "period_start_day", "rule": "max"}},
		{"day -1", `{"period_start_day":-1}`, "VALIDATION", map[string]any{"field": "period_start_day", "rule": "min"}},
		{"lower-case currency", `{"reporting_currency":"idr"}`, "VALIDATION", map[string]any{"field": "reporting_currency", "rule": "pattern"}},
		{"two-letter currency", `{"reporting_currency":"ID"}`, "VALIDATION", map[string]any{"field": "reporting_currency", "rule": "pattern"}},
		{"four-letter currency", `{"reporting_currency":"IDRR"}`, "VALIDATION", map[string]any{"field": "reporting_currency", "rule": "pattern"}},
		{"empty currency", `{"reporting_currency":""}`, "VALIDATION", map[string]any{"field": "reporting_currency", "rule": "pattern"}},
		{"name over the outer bound", `{"household_display_name":"` + strings.Repeat("a", 256) + `"}`, "VALIDATION", map[string]any{"field": "household_display_name", "rule": "max"}},
		// The handler's.
		{"blank name", `{"household_display_name":""}`, "VALIDATION", map[string]any{"field": "household_display_name", "rule": "required"}},
		{"spaces-only name", `{"household_display_name":"   \t "}`, "VALIDATION", map[string]any{"field": "household_display_name", "rule": "required"}},
		{"zero-width-only name", `{"household_display_name":"\u200b\u200c\u200d"}`, "VALIDATION", map[string]any{"field": "household_display_name", "rule": "required"}},
		{"61 code points", `{"household_display_name":"` + long61 + `"}`, "VALIDATION", map[string]any{"field": "household_display_name", "rule": "max"}},
		{"NUL in the name", `{"household_display_name":"Ru\u0000mah"}`, "VALIDATION", map[string]any{"field": "household_display_name", "rule": "pattern"}},
		{"not an ISO 4217 code", `{"reporting_currency":"ABC"}`, "VALIDATION", map[string]any{"field": "reporting_currency", "rule": "iso4217"}},
		// Failure order: the least wire field name wins, whichever check ran first.
		{"name and currency both bad", `{"reporting_currency":"ABC","household_display_name":""}`, "VALIDATION", map[string]any{"field": "household_display_name", "rule": "required"}},
		{"valid name with a bad currency writes neither", `{"household_display_name":"New","reporting_currency":"ABC"}`, "VALIDATION", map[string]any{"field": "reporting_currency", "rule": "iso4217"}},
		// Spec failures are reported before the handler's rules run.
		{"spec failure beats handler failure", `{"period_start_day":0,"household_display_name":""}`, "VALIDATION", map[string]any{"field": "period_start_day", "rule": "min"}},

		// Not an instance of the declared shape: INVALID_JSON_BODY.
		{"day as a string", `{"period_start_day":"5"}`, "INVALID_JSON_BODY", nil},
		{"day with a fraction", `{"period_start_day":5.5}`, "INVALID_JSON_BODY", nil},
		// kin-openapi's integer check accepts 5.0 and 5e0 (integral values), so
		// the spec validator passes them; the generated binding then cannot decode
		// them into an int, and requestErrorHandler answers the same code. Pinned
		// as #51 pinned its edge cases: both spellings are refused, whichever
		// layer does it.
		{"day as a number with a zero fraction", `{"period_start_day":5.0}`, "INVALID_JSON_BODY", nil},
		{"day in exponent notation", `{"period_start_day":5e0}`, "INVALID_JSON_BODY", nil},
		{"null name", `{"household_display_name":null}`, "INVALID_JSON_BODY", nil},
		{"null currency", `{"reporting_currency":null}`, "INVALID_JSON_BODY", nil},
		{"null day", `{"period_start_day":null}`, "INVALID_JSON_BODY", nil},
		{"name as a number", `{"household_display_name":5}`, "INVALID_JSON_BODY", nil},
		{"unknown property", `{"nickname":"x"}`, "INVALID_JSON_BODY", nil},
		// Settings a later slice writes (D4): not accepted yet.
		{"day_starts_at", `{"day_starts_at":"05:00"}`, "INVALID_JSON_BODY", nil},
		{"allowance_mode", `{"allowance_mode":"fixed"}`, "INVALID_JSON_BODY", nil},
		{"expected_monthly_income", `{"expected_monthly_income":"1.0000"}`, "INVALID_JSON_BODY", nil},
		{"id", `{"id":"0192f0a0-0000-7000-8000-000000000001"}`, "INVALID_JSON_BODY", nil},
		{"the old name for the Household's name", `{"display_name":"x"}`, "INVALID_JSON_BODY", nil},
		{"malformed JSON", `{"period_start_day":`, "INVALID_JSON_BODY", nil},
		{"not an object", `[]`, "INVALID_JSON_BODY", nil},
		{"JSON null", `null`, "INVALID_JSON_BODY", nil},
		{"no body", ``, "INVALID_JSON_BODY", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHouseholdFixture(t)
			before := f.snapshot(t)

			rec := f.patch(tc.body)
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
			if after := f.snapshot(t); after != before {
				t.Errorf("a refused request changed the row:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

// An exactly 60 code point name is fine, and so is one that is 61 only because
// of the padding the trim removes. The outer 255 bound counts the padding, the
// handler's 60 does not.
func TestPatchHouseholdNameLengthIsCountedAfterTheTrim(t *testing.T) {
	f := newHouseholdFixture(t)
	name := strings.Repeat("\U0001F6F6", 60) // 60 code points, 240 bytes
	padded := " " + name + " "

	b, _ := json.Marshal(map[string]string{"household_display_name": padded})
	rec := f.patch(string(b))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var stored string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT household_display_name FROM households WHERE id = $1`, f.householdID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != name {
		t.Errorf("stored %q, want the trimmed name", stored)
	}
}

// A bidi control inside a visible name is kept (BOOTSTRAP.md §5.1.1).
func TestPatchHouseholdKeepsFormatCharactersInAVisibleName(t *testing.T) {
	f := newHouseholdFixture(t)
	rec := f.patch(`{"household_display_name":"Dewi \u202e Putri\u200b"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var stored string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT household_display_name FROM households WHERE id = $1`, f.householdID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "Dewi \u202e Putri\u200b" {
		t.Errorf("stored %q", stored)
	}
}

func TestPatchHouseholdWithNoSessionIsUnauthorized(t *testing.T) {
	f := newHouseholdFixture(t)
	before := f.snapshot(t)

	for name, mutate := range map[string]func(*http.Request){
		"no cookie":      func(r *http.Request) { r.Header.Del("Cookie") },
		"unknown token":  func(r *http.Request) { r.Header.Set("Cookie", auth.SessionCookieName+"=nope") },
		"handshake only": func(r *http.Request) { r.Header.Set("Cookie", auth.OnboardingCookieName+"=nope") },
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.patch(`{"period_start_day":2}`, mutate)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}
			assertEnvelope(t, rec, "UNAUTHORIZED")
		})
	}
	if after := f.snapshot(t); after != before {
		t.Errorf("an unauthenticated request changed the row")
	}
}

func TestPatchHouseholdWithAnExpiredSessionIsUnauthorized(t *testing.T) {
	f := newHouseholdFixture(t)
	if _, err := f.tdb.Pool.Exec(context.Background(), `UPDATE sessions SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)

	rec := f.patch(`{"period_start_day":2}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	assertEnvelope(t, rec, "UNAUTHORIZED")
	if f.snapshot(t) != before {
		t.Error("an expired session changed the row")
	}
}

// The cross-site guard covers every non-safe method, so PATCH now (the
// existing guard cases are all POST).
func TestPatchHouseholdFromAnotherSiteIsBlocked(t *testing.T) {
	f := newHouseholdFixture(t)
	before := f.snapshot(t)

	rec := f.patch(`{"period_start_day":2}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	assertEnvelope(t, rec, "CROSS_SITE_REQUEST_BLOCKED")
	if f.snapshot(t) != before {
		t.Error("a blocked request changed the row")
	}
}

// There is no GET, no PUT and no DELETE on /household, and there never will be
// a hard delete (BOOTSTRAP.md §4). The router answers 405 for a route that
// exists under other methods, as for every other.
func TestHouseholdAnswersOnlyPatch(t *testing.T) {
	f := newHouseholdFixture(t)
	before := f.snapshot(t)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPost, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/household", nil)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.Header.Set("Cookie", f.cookie)
			rec := httptest.NewRecorder()
			f.srv.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s /api/household = %d, want 405", method, rec.Code)
			}
		})
	}
	if f.snapshot(t) != before {
		t.Error("a wrong-method request changed the row")
	}
	var live int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM households WHERE deleted_at IS NULL`).Scan(&live); err != nil || live != 1 {
		t.Errorf("live households = %d (%v), want 1", live, err)
	}
}

// The body is required and JSON: another media type, or none, is not a body the
// contract takes. Nothing is written.
func TestPatchHouseholdWantsAJSONBody(t *testing.T) {
	for name, contentType := range map[string]string{
		"text/plain":         "text/plain",
		"form encoded":       "application/x-www-form-urlencoded",
		"no Content-Type":    "",
		"not a media type":   "applİcation/json",
		"JSON with a suffix": "application/merge-patch+json",
	} {
		t.Run(name, func(t *testing.T) {
			f := newHouseholdFixture(t)
			before := f.snapshot(t)

			rec := f.patch(`{"household_display_name":"Rumah Kita"}`, func(r *http.Request) {
				if contentType == "" {
					r.Header.Del("Content-Type")
				} else {
					r.Header.Set("Content-Type", contentType)
				}
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			assertEnvelope(t, rec, "INVALID_JSON_BODY")
			if f.snapshot(t) != before {
				t.Error("a refused request changed the row")
			}
		})
	}
}

// Content-Type is a media type, so case and spacing do not matter, and a
// charset parameter is ignored (the same canonicalisation as every JSON route).
func TestPatchHouseholdAcceptsTheUsualJSONSpellings(t *testing.T) {
	for _, contentType := range []string{"application/json; charset=utf-8", "Application/JSON", "application/json ;charset=UTF-8"} {
		t.Run(contentType, func(t *testing.T) {
			f := newHouseholdFixture(t)
			rec := f.patch(`{"period_start_day":2}`, func(r *http.Request) { r.Header.Set("Content-Type", contentType) })
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}
