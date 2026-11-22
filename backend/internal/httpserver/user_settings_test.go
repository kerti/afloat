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

const (
	settingsToken    = "a-session-token-for-the-user-settings-tests"
	settingsPassword = "kucing oranye di atap"
	settingsNew      = "tikus putih di lumbung"
)

// settingsFixture is the whole stack over a real database with one signed-in
// User who holds a credential, so PATCH /me and the two credential-gated routes
// are exercised through the session check, the spec validator and the generated
// binding rather than as bare handlers.
type settingsFixture struct {
	tdb    *testutil.TestDB
	srv    http.Handler
	userID pgtype.UUID
}

func newSettingsFixture(t *testing.T) settingsFixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	householdID := tdb.CreateHousehold(t, "Home")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "A")

	hash, err := auth.HashPassword(context.Background(), settingsPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := tdb.Queries.UpsertCredential(context.Background(), db.UpsertCredentialParams{UserID: userID, PasswordHash: hash}); err != nil {
		t.Fatalf("UpsertCredential: %v", err)
	}
	f := settingsFixture{tdb: tdb, userID: userID}
	// A full TTL left, so the session middleware does not refresh (and so write)
	// the session on every request, which would make "nothing was written"
	// unassertable.
	f.openSession(t, settingsToken, time.Now().Add(30*24*time.Hour))

	f.srv = New(Deps{
		System: system.New(system.Deps{Querier: tdb.Queries, Version: "test", LocalEnabled: true}),
		Auth: auth.New(auth.Deps{
			Querier:            tdb.Queries,
			Beginner:           tdb.Pool,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
			FirstBackoff:       time.Second,
			MaxBackoff:         time.Minute,
		}),
		Household:      household.New(household.Deps{Querier: tdb.Queries}),
		HandlerTimeout: testHandlerTimeout,
	})
	return f
}

func (f settingsFixture) openSession(t *testing.T, token string, expires time.Time) {
	t.Helper()
	if _, err := f.tdb.Queries.CreateSession(context.Background(), db.CreateSessionParams{
		ID: auth.HashToken(token), UserID: f.userID, ExpiresAt: pgtypeTimestamptz(expires),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}

func (f settingsFixture) send(method, path, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Cookie", auth.SessionCookieName+"="+settingsToken)
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func (f settingsFixture) patchMe(body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.send(http.MethodPatch, "/api/me", body, mutate...)
}

func (f settingsFixture) changePassword(body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.send(http.MethodPost, "/api/auth/local/change-password", body, mutate...)
}

func (f settingsFixture) changeEmail(body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.send(http.MethodPost, "/api/auth/local/change-email", body, mutate...)
}

// everything is the User's rows, credential and sessions as JSON, so
// "unchanged" means byte for byte.
func (f settingsFixture) everything(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(), `
		SELECT jsonb_build_object(
		  'user',       (SELECT to_jsonb(u) FROM users u WHERE id = $1),
		  'credential', (SELECT to_jsonb(c) FROM credentials c WHERE user_id = $1),
		  'sessions',   (SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY id), '[]') FROM sessions s WHERE user_id = $1),
		  'attempts',   (SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY key), '[]') FROM login_attempts a)
		)::text`, f.userID).Scan(&s); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return s
}

type wireError struct {
	Code string         `json:"code"`
	Args map[string]any `json:"args"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) wireError {
	t.Helper()
	var got wireError
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %s: %v", rec.Body.String(), err)
	}
	return got
}

func TestPatchMeThroughTheRouter(t *testing.T) {
	f := newSettingsFixture(t)

	rec := f.patchMe(`{"user_display_name":"  Dewi Putri ","locale":"id-ID","time_zone":"Asia/Makassar","appearance":"dark"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{
		"email":             "a@example.com",
		"user_display_name": "Dewi Putri", // trimmed
		"locale":            "id-ID",
		"time_zone":         "Asia/Makassar",
		"appearance":        "dark",
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if h, _ := got["household"].(map[string]any); h["household_display_name"] != "Home" {
		t.Errorf("household = %v, want the nested Household", got["household"])
	}
	for _, k := range []string{"password", "password_hash", "current_password"} {
		if _, present := got[k]; present {
			t.Errorf("the answer carries %q", k)
		}
	}
}

// Every refusal answers 400 and writes nothing, including a body whose other
// fields were fine: one UPDATE, all or nothing.
func TestPatchMeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       string
		args       map[string]any
	}{
		// The spec validator's rules.
		{"empty time_zone", `{"time_zone":""}`, "VALIDATION", map[string]any{"field": "time_zone", "rule": "min"}},
		{"time_zone over the outer bound", `{"time_zone":"` + strings.Repeat("a", 65) + `"}`, "VALIDATION", map[string]any{"field": "time_zone", "rule": "max"}},
		{"name over the outer bound", `{"user_display_name":"` + strings.Repeat("a", 256) + `"}`, "VALIDATION", map[string]any{"field": "user_display_name", "rule": "max"}},
		// The handler's.
		{"blank name", `{"user_display_name":""}`, "VALIDATION", map[string]any{"field": "user_display_name", "rule": "required"}},
		{"spaces-only name", `{"user_display_name":"   \t "}`, "VALIDATION", map[string]any{"field": "user_display_name", "rule": "required"}},
		{"zero-width-only name", `{"user_display_name":"\u200b\u200c\u200d"}`, "VALIDATION", map[string]any{"field": "user_display_name", "rule": "required"}},
		{"NUL in the name", `{"user_display_name":"De\u0000wi"}`, "VALIDATION", map[string]any{"field": "user_display_name", "rule": "pattern"}},
		{"not a time zone", `{"time_zone":"Mars/Base"}`, "VALIDATION", map[string]any{"field": "time_zone", "rule": "timezone"}},
		{"Local", `{"time_zone":"Local"}`, "VALIDATION", map[string]any{"field": "time_zone", "rule": "timezone"}},
		{"path traversal", `{"time_zone":"../etc/passwd"}`, "VALIDATION", map[string]any{"field": "time_zone", "rule": "timezone"}},
		{"trailing space", `{"time_zone":"Asia/Jakarta "}`, "VALIDATION", map[string]any{"field": "time_zone", "rule": "timezone"}},
		// Failure order: the least wire field name wins among handler rules.
		{"zone and name both bad", `{"user_display_name":"","time_zone":"Mars/Base"}`, "VALIDATION", map[string]any{"field": "time_zone", "rule": "timezone"}},
		{"valid name with a bad zone writes neither", `{"user_display_name":"New","time_zone":"Mars/Base"}`, "VALIDATION", map[string]any{"field": "time_zone", "rule": "timezone"}},
		// Contract rules are judged before handler rules (BOOTSTRAP.md \u00a75.1.1):
		// the zone's `min` is the contract's, the name's `required` the handler's.
		{"spec failure beats handler failure", `{"time_zone":"","user_display_name":" "}`, "VALIDATION", map[string]any{"field": "time_zone", "rule": "min"}},
		{"spec failure beats a handler failure on another field", `{"user_display_name":"` + strings.Repeat("a", 256) + `","time_zone":"Mars/Base"}`, "VALIDATION", map[string]any{"field": "user_display_name", "rule": "max"}},

		// Not an instance of the declared shape: INVALID_JSON_BODY.
		{"a locale outside the enum", `{"locale":"fr-FR"}`, "INVALID_JSON_BODY", nil},
		{"a locale in the wrong case", `{"locale":"en-gb"}`, "INVALID_JSON_BODY", nil},
		{"an empty locale", `{"locale":""}`, "INVALID_JSON_BODY", nil},
		{"null name", `{"user_display_name":null}`, "INVALID_JSON_BODY", nil},
		{"null locale", `{"locale":null}`, "INVALID_JSON_BODY", nil},
		{"null time_zone", `{"time_zone":null}`, "INVALID_JSON_BODY", nil},
		{"an appearance outside the enum", `{"appearance":"auto"}`, "INVALID_JSON_BODY", nil},
		{"an appearance in the wrong case", `{"appearance":"Dark"}`, "INVALID_JSON_BODY", nil},
		{"an empty appearance", `{"appearance":""}`, "INVALID_JSON_BODY", nil},
		{"null appearance", `{"appearance":null}`, "INVALID_JSON_BODY", nil},
		{"appearance as a number", `{"appearance":1}`, "INVALID_JSON_BODY", nil},
		{"a valid appearance beside a bad locale writes neither", `{"appearance":"dark","locale":"fr-FR"}`, "INVALID_JSON_BODY", nil},
		{"name as a number", `{"user_display_name":5}`, "INVALID_JSON_BODY", nil},
		{"time_zone as a number", `{"time_zone":7}`, "INVALID_JSON_BODY", nil},
		{"unknown property", `{"nickname":"x"}`, "INVALID_JSON_BODY", nil},
		// Not profile fields: each needs the current password, or is not the
		// User's to set.
		{"email", `{"email":"b@example.com"}`, "INVALID_JSON_BODY", nil},
		{"password", `{"password":"tikus putih di lumbung"}`, "INVALID_JSON_BODY", nil},
		{"new_password", `{"new_password":"tikus putih di lumbung"}`, "INVALID_JSON_BODY", nil},
		{"current_password", `{"current_password":"x"}`, "INVALID_JSON_BODY", nil},
		{"id", `{"id":"0192f0a0-0000-7000-8000-000000000001"}`, "INVALID_JSON_BODY", nil},
		{"household", `{"household":{"household_display_name":"x"}}`, "INVALID_JSON_BODY", nil},
		{"the bare name", `{"display_name":"x"}`, "INVALID_JSON_BODY", nil},
		{"malformed JSON", `{"locale":`, "INVALID_JSON_BODY", nil},
		{"not an object", `[]`, "INVALID_JSON_BODY", nil},
		{"JSON null", `null`, "INVALID_JSON_BODY", nil},
		{"no body", ``, "INVALID_JSON_BODY", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSettingsFixture(t)
			before := f.everything(t)

			rec := f.patchMe(tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			got := decodeError(t, rec)
			if got.Code != tc.code {
				t.Errorf("code = %s, want %s (body %s)", got.Code, tc.code, rec.Body.String())
			}
			if len(tc.args) > 0 && (got.Args["field"] != tc.args["field"] || got.Args["rule"] != tc.args["rule"]) {
				t.Errorf("args = %v, want %v", got.Args, tc.args)
			}
			if after := f.everything(t); after != before {
				t.Errorf("a refused request wrote:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

func TestPatchMeWantsAJSONBody(t *testing.T) {
	for name, contentType := range map[string]string{
		"text/plain":         "text/plain",
		"form encoded":       "application/x-www-form-urlencoded",
		"no Content-Type":    "",
		"JSON with a suffix": "application/merge-patch+json",
	} {
		t.Run(name, func(t *testing.T) {
			f := newSettingsFixture(t)
			before := f.everything(t)
			rec := f.patchMe(`{"user_display_name":"Dewi"}`, func(r *http.Request) {
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
			if f.everything(t) != before {
				t.Error("a refused request wrote")
			}
		})
	}
}

// Every operation here is protected without a `security` stanza of its own: the
// contract's global requirement does it. No cookie, an unknown token, an
// expired session and a handshake cookie are all 401 UNAUTHORIZED, before the
// body is looked at, and nothing is written, hashed or recorded.
func TestUserSettingsOperationsWithoutASessionAreUnauthorized(t *testing.T) {
	for _, op := range []struct {
		name, method, path, body string
	}{
		{"PATCH /me", http.MethodPatch, "/api/me", `{"user_display_name":"Hijack"}`},
		{"change-password", http.MethodPost, "/api/auth/local/change-password", `{"current_password":"` + settingsPassword + `","new_password":"` + settingsNew + `"}`},
		{"change-email", http.MethodPost, "/api/auth/local/change-email", `{"email":"b@example.com","current_password":"` + settingsPassword + `"}`},
		// An invalid body is still 401 first.
		{"PATCH /me with a bad body", http.MethodPatch, "/api/me", `{"nope":`},
		{"change-password with a bad body", http.MethodPost, "/api/auth/local/change-password", `{}`},
	} {
		for name, mutate := range map[string]func(*http.Request){
			"no cookie":      func(r *http.Request) { r.Header.Del("Cookie") },
			"unknown token":  func(r *http.Request) { r.Header.Set("Cookie", auth.SessionCookieName+"=nope") },
			"handshake only": func(r *http.Request) { r.Header.Set("Cookie", auth.OnboardingCookieName+"=nope") },
		} {
			t.Run(op.name+" "+name, func(t *testing.T) {
				f := newSettingsFixture(t)
				before := f.everything(t)
				rec := f.send(op.method, op.path, op.body, mutate)
				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
				}
				assertEnvelope(t, rec, "UNAUTHORIZED")
				if f.everything(t) != before {
					t.Error("an unauthenticated request wrote")
				}
			})
		}
	}

	t.Run("an expired session", func(t *testing.T) {
		f := newSettingsFixture(t)
		if _, err := f.tdb.Pool.Exec(context.Background(), `UPDATE sessions SET expires_at = now() - interval '1 minute'`); err != nil {
			t.Fatal(err)
		}
		before := f.everything(t)
		rec := f.changePassword(`{"current_password":"` + settingsPassword + `","new_password":"` + settingsNew + `"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if f.everything(t) != before {
			t.Error("an expired session wrote")
		}
	})
}

// The cross-site guard covers PATCH and both POSTs: a blocked request changes
// nothing, the credential included.
func TestUserSettingsOperationsFromAnotherSiteAreBlocked(t *testing.T) {
	cross := func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }
	for name, send := range map[string]func(f settingsFixture) *httptest.ResponseRecorder{
		"PATCH /me": func(f settingsFixture) *httptest.ResponseRecorder { return f.patchMe(`{"locale":"id-ID"}`, cross) },
		"change-password": func(f settingsFixture) *httptest.ResponseRecorder {
			return f.changePassword(`{"current_password":"`+settingsPassword+`","new_password":"`+settingsNew+`"}`, cross)
		},
		"change-email": func(f settingsFixture) *httptest.ResponseRecorder {
			return f.changeEmail(`{"email":"b@example.com","current_password":"`+settingsPassword+`"}`, cross)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSettingsFixture(t)
			before := f.everything(t)
			rec := send(f)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
			}
			assertEnvelope(t, rec, "CROSS_SITE_REQUEST_BLOCKED")
			if f.everything(t) != before {
				t.Error("a blocked request changed something")
			}
		})
	}
}

// The new routes answer 405 to every method they do not take, as every other
// route does, and /me gains PATCH without losing GET.
func TestUserSettingsRoutesAnswerOnlyTheirMethods(t *testing.T) {
	f := newSettingsFixture(t)
	before := f.everything(t)
	for _, tc := range []struct {
		path    string
		methods []string
	}{
		{"/api/auth/local/change-password", []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete}},
		{"/api/auth/local/change-email", []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete}},
		{"/api/me", []string{http.MethodPost, http.MethodPut, http.MethodDelete}},
	} {
		for _, method := range tc.methods {
			t.Run(method+" "+tc.path, func(t *testing.T) {
				if rec := f.send(method, tc.path, ""); rec.Code != http.StatusMethodNotAllowed {
					t.Errorf("status = %d, want 405", rec.Code)
				}
			})
		}
	}
	if rec := f.send(http.MethodGet, "/api/me", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/me = %d, want 200", rec.Code)
	}
	if f.everything(t) != before {
		t.Error("a wrong-method request changed something")
	}
}

// Change-password's wire shape, and the one thing a handler test cannot see:
// the response carries exactly ONE Set-Cookie, the new session's. The session
// here is past half its TTL, so the session middleware has already queued a
// refreshed cookie for the OLD token; the generated visitor's Header().Set must
// replace it, or the client keeps a token that was just revoked.
func TestChangePasswordThroughTheRouterSetsExactlyTheNewSessionCookie(t *testing.T) {
	f := newSettingsFixture(t)
	const stale = "a-session-past-half-its-life-for-the-test"
	f.openSession(t, stale, time.Now().Add(time.Hour)) // 1h left of 30 days
	other := "another-device-s-session-token"
	f.openSession(t, other, time.Now().Add(24*time.Hour))

	rec := f.changePassword(`{"current_password":"`+settingsPassword+`","new_password":"`+settingsNew+`"}`, func(r *http.Request) {
		r.Header.Set("Cookie", auth.SessionCookieName+"="+stale)
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie headers = %v, want exactly one", rec.Header().Values("Set-Cookie"))
	}
	c := cookies[0]
	if c.Name != auth.SessionCookieName || c.Value == stale || c.Value == "" || !c.HttpOnly || !c.Secure || c.MaxAge < 1 {
		t.Errorf("cookie = %+v, want a fresh session cookie, not the refreshed old one", c)
	}

	// The new token is the only live one, through the router.
	if rec := f.send(http.MethodGet, "/api/me", "", func(r *http.Request) { r.Header.Set("Cookie", auth.SessionCookieName+"="+c.Value) }); rec.Code != http.StatusOK {
		t.Errorf("GET /api/me with the new cookie = %d, want 200", rec.Code)
	}
	for _, old := range []string{stale, settingsToken, other} {
		if rec := f.send(http.MethodGet, "/api/me", "", func(r *http.Request) { r.Header.Set("Cookie", auth.SessionCookieName+"="+old) }); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET /api/me with a revoked session = %d, want 401", rec.Code)
		}
	}
}

func TestChangePasswordRefusalsThroughTheRouter(t *testing.T) {
	longNew := strings.Repeat("a", 1025)
	cur := `"current_password":"` + settingsPassword + `"`
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
		args       map[string]any
	}{
		{"missing current_password", `{"new_password":"` + settingsNew + `"}`, 400, "VALIDATION", map[string]any{"field": "current_password", "rule": "required"}},
		{"missing new_password", `{` + cur + `}`, 400, "VALIDATION", map[string]any{"field": "new_password", "rule": "required"}},
		{"empty current_password", `{"current_password":"","new_password":"` + settingsNew + `"}`, 400, "VALIDATION", map[string]any{"field": "current_password", "rule": "min"}},
		{"current_password over the bound", `{"current_password":"` + strings.Repeat("a", 4097) + `","new_password":"` + settingsNew + `"}`, 400, "VALIDATION", map[string]any{"field": "current_password", "rule": "max"}},
		{"new_password over the bound", `{` + cur + `,"new_password":"` + longNew + `"}`, 400, "VALIDATION", map[string]any{"field": "new_password", "rule": "max"}},
		{"a short new password is WEAK_PASSWORD, not a schema failure", `{` + cur + `,"new_password":"short"}`, 400, "WEAK_PASSWORD", map[string]any{"reason": "min"}},
		{"an empty new password is WEAK_PASSWORD", `{` + cur + `,"new_password":""}`, 400, "WEAK_PASSWORD", map[string]any{"reason": "min"}},
		{"a breached new password", `{` + cur + `,"new_password":"PassWord1234"}`, 400, "WEAK_PASSWORD", map[string]any{"reason": "breached"}},
		{"unknown property", `{` + cur + `,"new_password":"` + settingsNew + `","email":"x@example.com"}`, 400, "INVALID_JSON_BODY", nil},
		{"the old property name", `{"password":"` + settingsPassword + `","new_password":"` + settingsNew + `"}`, 400, "INVALID_JSON_BODY", nil},
		{"null current_password", `{"current_password":null,"new_password":"` + settingsNew + `"}`, 400, "INVALID_JSON_BODY", nil},
		{"no body", ``, 400, "INVALID_JSON_BODY", nil},
		// The current password is judged last, after the hash has been paid for.
		{"a wrong current password", `{"current_password":"not the password at all","new_password":"` + settingsNew + `"}`, 403, "INVALID_CURRENT_PASSWORD", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSettingsFixture(t)
			credential := f.credentialHash(t)

			rec := f.changePassword(tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body.String())
			}
			if len(rec.Result().Cookies()) != 0 {
				t.Errorf("a refusal set a cookie: %v", rec.Header().Values("Set-Cookie"))
			}
			got := decodeError(t, rec)
			if got.Code != tc.code {
				t.Errorf("code = %s, want %s", got.Code, tc.code)
			}
			if len(tc.args) > 0 && (got.Args["field"] != tc.args["field"] || got.Args["rule"] != tc.args["rule"] || got.Args["reason"] != tc.args["reason"]) {
				t.Errorf("args = %v, want %v", got.Args, tc.args)
			}
			if f.credentialHash(t) != credential {
				t.Error("a refusal changed the credential")
			}
			assertEnvelope(t, rec, tc.code)
		})
	}
}

func (f settingsFixture) credentialHash(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT password_hash FROM credentials WHERE user_id = $1`, f.userID).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// The throttle on the wire: 429 with a Retry-After header, and the same wrong
// password again is not hashed.
func TestChangePasswordThrottleThroughTheRouter(t *testing.T) {
	f := newSettingsFixture(t)
	body := `{"current_password":"not the password at all","new_password":"` + settingsNew + `"}`

	if rec := f.changePassword(body); rec.Code != http.StatusForbidden {
		t.Fatalf("first status = %d, want 403", rec.Code)
	}
	rec := f.changePassword(body)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", rec.Code)
	}
	assertEnvelope(t, rec, "TOO_MANY_ATTEMPTS")
	if rec.Header().Get("Retry-After") == "" || rec.Header().Get("Retry-After") == "0" {
		t.Errorf("Retry-After = %q, want at least 1", rec.Header().Get("Retry-After"))
	}
}

func TestChangeEmailThroughTheRouter(t *testing.T) {
	f := newSettingsFixture(t)
	rec := f.changeEmail(`{"email":"  New.Address@Example.COM ","current_password":"` + settingsPassword + `"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("change-email set a cookie: %v", rec.Header().Values("Set-Cookie"))
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["email"] != "new.address@example.com" {
		t.Errorf("email = %v, want it trimmed and lower-cased", got["email"])
	}
	if got["household"] == nil || got["id"] == nil {
		t.Errorf("answer = %v, want a whole Me", got)
	}
	// The same session still works: no session was touched.
	if rec := f.send(http.MethodGet, "/api/me", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/me after the change = %d, want 200", rec.Code)
	}
}

func TestChangeEmailRefusalsThroughTheRouter(t *testing.T) {
	cur := `"current_password":"` + settingsPassword + `"`
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
		args       map[string]any
	}{
		{"not an address", `{"email":"nobody",` + cur + `}`, 400, "VALIDATION", map[string]any{"field": "email", "rule": "email"}},
		{"an empty address", `{"email":"",` + cur + `}`, 400, "VALIDATION", map[string]any{"field": "email", "rule": "email"}},
		{"an address over the bound", `{"email":"` + strings.Repeat("a", 320) + `@example.com",` + cur + `}`, 400, "VALIDATION", map[string]any{"field": "email", "rule": "max"}},
		{"missing email", `{` + cur + `}`, 400, "VALIDATION", map[string]any{"field": "email", "rule": "required"}},
		{"missing current_password", `{"email":"b@example.com"}`, 400, "VALIDATION", map[string]any{"field": "current_password", "rule": "required"}},
		{"empty current_password", `{"email":"b@example.com","current_password":""}`, 400, "VALIDATION", map[string]any{"field": "current_password", "rule": "min"}},
		{"unknown property", `{"email":"b@example.com",` + cur + `,"user_display_name":"x"}`, 400, "INVALID_JSON_BODY", nil},
		{"no body", ``, 400, "INVALID_JSON_BODY", nil},
		{"a wrong current password", `{"email":"b@example.com","current_password":"not the password at all"}`, 403, "INVALID_CURRENT_PASSWORD", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSettingsFixture(t)
			var before string
			if tc.status == 400 {
				before = f.everything(t)
			}
			rec := f.changeEmail(tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body.String())
			}
			got := decodeError(t, rec)
			if got.Code != tc.code {
				t.Errorf("code = %s, want %s", got.Code, tc.code)
			}
			if len(tc.args) > 0 && (got.Args["field"] != tc.args["field"] || got.Args["rule"] != tc.args["rule"]) {
				t.Errorf("args = %v, want %v", got.Args, tc.args)
			}
			assertEnvelope(t, rec, tc.code)
			var email string
			if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT email FROM users WHERE id = $1`, f.userID).Scan(&email); err != nil || email != "a@example.com" {
				t.Errorf("email = %q (%v), want it unchanged", email, err)
			}
			if tc.status == 400 && f.everything(t) != before {
				t.Error("a refusal wrote (a failure recorded, or a row changed)")
			}
		})
	}
}
