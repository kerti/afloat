package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/jackc/pgx/v5"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/system"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// The table from #18/#21: every request-body constraint the contract declares
// for POST /api/auth/local/login, driven over the REAL router — chi's mux, the
// generated decode, the spec-validating middleware and the handler — rather
// than constructing an api.LocalLoginRequestObject directly and calling the
// handler, which is exactly the gap #18 names in login_integration_test.go:36
// (decode, bind and validation all sit upstream of that call).
// additionalProperties: false is not here: an unknown property is an
// undecodable body, not a constraint (the test below). Kotlin's LoginSpec
// drives the same bodies ("answers each body exactly as Go does").
//
// fakeQuerier is enough for every case here: a request that fails validation
// never reaches the handler, so nothing calls the database at all.
func TestLoginRequestValidation(t *testing.T) {
	srv := newTestServer()

	oversizePassword := strings.Repeat("a", 4097)
	oversizeEmail := strings.Repeat("a", 310) + "@example.com" // 322 chars, over the 320 maxLength

	for _, tc := range []struct {
		name      string
		body      string
		wantField string
		wantRule  string
	}{
		{
			name:      "password absent",
			body:      `{"email":"a@example.com"}`,
			wantField: "password",
			wantRule:  "required",
		},
		{
			name:      "email absent",
			body:      `{"password":"a valid password"}`,
			wantField: "email",
			wantRule:  "required",
		},
		{
			name:      "password empty",
			body:      `{"email":"a@example.com","password":""}`,
			wantField: "password",
			wantRule:  "min",
		},
		{
			name:      "password over 4096 characters",
			body:      `{"email":"a@example.com","password":"` + oversizePassword + `"}`,
			wantField: "password",
			wantRule:  "max",
		},
		{
			name:      "email over 320 characters",
			body:      `{"email":"` + oversizeEmail + `","password":"a valid password"}`,
			wantField: "email",
			wantRule:  "max",
		},
		{
			name:      "email is not a valid address",
			body:      `{"email":"not-an-email","password":"a valid password"}`,
			wantField: "email",
			wantRule:  "email",
		},
		{
			// kin-openapi checks present properties before required ones, so
			// it finds password's first; Kotlin reports the least field name.
			name:      "email absent and password empty",
			body:      `{"password":""}`,
			wantField: "email",
			wantRule:  "required",
		},
		{
			name:      "email invalid and password empty",
			body:      `{"email":"not-an-email","password":""}`,
			wantField: "email",
			wantRule:  "email",
		},
		{
			name:      "empty object",
			body:      `{}`,
			wantField: "email",
			wantRule:  "required",
		},
		{
			name:      "email invalid and password absent",
			body:      `{"email":"not-an-email"}`,
			wantField: "email",
			wantRule:  "email",
		},
		{
			name:      "email empty",
			body:      `{"email":"","password":"a valid password"}`,
			wantField: "email",
			wantRule:  "email",
		},
		{
			name:      "email only whitespace",
			body:      `{"email":"   ","password":"a valid password"}`,
			wantField: "email",
			wantRule:  "email",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			assertValidationArgs(t, rec, tc.wantField, tc.wantRule)
		})
	}
}

// A body the JSON decoder can parse but that is not an instance of the
// declared shape is not a field failing a constraint: it is the same "did not
// decode as what the contract declares" bucket as malformed JSON, and the
// bucket Kotlin's Jackson decode puts every one of these in (LoginSpec). It
// wins over a constraint failure elsewhere in the body, because in Kotlin the
// decode fails before Bean Validation ever runs.
func TestLoginRequestValidationUndecodableBodyIsInvalidJSONBody(t *testing.T) {
	srv := newTestServer()

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "root is not an object", body: `["not","an","object"]`},
		{name: "field of the wrong type", body: `{"email":5,"password":"a valid password"}`},
		{name: "field is null", body: `{"email":null,"password":"a valid password"}`},
		{name: "unknown property", body: `{"email":"a@example.com","password":"a valid password","admin":true}`},
		{name: "unknown property beside an invalid field", body: `{"email":"not-an-email","password":"a valid password","zzz":1}`},
		{name: "unknown property beside an absent field", body: `{"password":"","admin":true}`},
		{name: "null beside an absent field", body: `{"password":null}`},
		{name: "boolean where a string belongs", body: `{"email":"a@example.com","password":true}`},
		{name: "malformed JSON", body: `{"email": `},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			assertEnvelope(t, rec, "INVALID_JSON_BODY")
			if strings.Contains(rec.Body.String(), "args") {
				t.Errorf("body = %s, want no args", rec.Body.String())
			}
		})
	}
}

// kin-openapi's SchemaError.Error() prints the offending value, and on this
// route the offending value can be the password.
func TestLoginRequestValidationDoesNotLogTheRejectedValue(t *testing.T) {
	logged := captureLog(t)
	srv := newTestServer()

	const secret = "kucing oranye di atap"
	for _, body := range []string{
		`["` + secret + `"]`,
		`{"email":"a@example.com","password":["` + secret + `"]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		srv.ServeHTTP(httptest.NewRecorder(), req)
	}

	if !strings.Contains(logged.String(), "request body rejected by contract") {
		t.Fatalf("nothing logged for a rejected body: %q", logged.String())
	}
	if strings.Contains(logged.String(), secret) {
		t.Errorf("log carries the rejected value: %q", logged.String())
	}
}

// The paths no login body reaches today, driven directly: a parameter that
// binds but breaks the contract, and an error that is no verdict on the
// request at all.
func TestWriteOpenAPIValidationErrorUnreachedPaths(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantField  string
		wantRule   string
	}{
		{
			name:       "required parameter absent",
			err:        openapi3.MultiError{&openapi3filter.RequestError{Parameter: &openapi3.Parameter{Name: "cursor"}, Err: openapi3filter.ErrInvalidRequired}},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION",
			wantField:  "cursor",
			wantRule:   "required",
		},
		{
			name:       "parameter over its maxLength",
			err:        &openapi3filter.RequestError{Parameter: &openapi3.Parameter{Name: "cursor"}, Err: &openapi3.SchemaError{SchemaField: "maxLength"}},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION",
			wantField:  "cursor",
			wantRule:   "max",
		},
		{
			name:       "route the contract's router could not find",
			err:        errors.New("no matching operation was found"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL",
		},
		{
			name:       "security requirement failed",
			err:        &openapi3filter.SecurityRequirementsError{},
			wantStatus: http.StatusUnauthorized,
			wantCode:   "UNAUTHORIZED",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			rec := httptest.NewRecorder()
			writeOpenAPIValidationError(req.Context(), tc.err, rec, req, nethttpmiddleware.ErrorHandlerOpts{})

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantField != "" {
				assertValidationArgs(t, rec, tc.wantField, tc.wantRule)
				return
			}
			assertEnvelope(t, rec, tc.wantCode)
		})
	}
}

// emailLookupSpy records whether GetUserByEmail was reached, standing in for
// "did the handler start doing Argon2 work". LocalLogin (login.go) reads the
// backoff, then looks the address up, then hashes — for both a found and an
// unknown address, by design (enumeration resistance) — so a GetUserByEmail
// that was never called is proof the handler, and therefore Argon2, never ran
// at all. GetLoginBackoff answers "no backoff" so a handler that did run gets
// as far as the lookup; unstubbed, it panicked first and the spy never fired.
type emailLookupSpy struct {
	db.Querier
	called *bool
}

func (emailLookupSpy) GetLoginBackoff(context.Context, []string) (float64, error) {
	return 0, pgx.ErrNoRows
}

func (s emailLookupSpy) GetUserByEmail(context.Context, string) (db.User, error) {
	*s.called = true
	return db.User{}, pgx.ErrNoRows
}

// The oversize cases must be rejected before any Argon2 work happens — an
// unauthenticated caller must not get to choose how much data is hashed
// (#18's acceptance criteria, BOOTSTRAP.md §5.1's length cap).
func TestLoginDoesNotHashAnOversizePassword(t *testing.T) {
	called := false
	q := emailLookupSpy{called: &called}
	srv := New(Deps{
		System: system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: true}),
		Auth: auth.New(auth.Deps{
			Querier:            q,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
		HandlerTimeout: testHandlerTimeout,
	})

	body := `{"email":"a@example.com","password":"` + strings.Repeat("a", 4097) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Ahead of the status check: a handler that ran goes on to fail at an
	// unstubbed query, and that 500's Fatalf would otherwise hide this.
	if called {
		t.Error("GetUserByEmail was called for an oversize password; the handler ran before validation rejected it, so Argon2 hashed the whole thing")
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	assertValidationArgs(t, rec, "password", "max")
}

// The other half of ruling #1 on #18: a padded address does not merely pass
// validation, it logs in — end to end, over HTTP, with a real database, so
// the format check's trim-then-validate and login.go's own normalizeEmail
// agree on the same outcome.
func TestLoginOverHTTPTrimsAPaddedEmailAndSucceeds(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	householdID := tdb.CreateHousehold(t, "Test Household")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "Test User")

	const password = "kucing oranye di atap"
	hash, err := auth.HashPassword(context.Background(), password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := tdb.Queries.UpsertCredential(context.Background(), db.UpsertCredentialParams{
		UserID:       userID,
		PasswordHash: hash,
	}); err != nil {
		t.Fatalf("UpsertCredential: %v", err)
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
		HandlerTimeout: testHandlerTimeout,
	})

	body := `{"email":"  A@Example.COM  ","password":"` + password + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for a padded, differently-cased address (body %s)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("login returned no Set-Cookie")
	}
}

// assertValidationArgs checks the VALIDATION envelope's {field, rule} shape,
// the same one httperr.WriteValidation produces.
func assertValidationArgs(t *testing.T, rec *httptest.ResponseRecorder, wantField, wantRule string) {
	t.Helper()
	assertEnvelope(t, rec, "VALIDATION")

	var body struct {
		Args struct {
			Field string `json:"field"`
			Rule  string `json:"rule"`
		} `json:"args"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Args.Field != wantField {
		t.Errorf("args.field = %q, want %q", body.Args.Field, wantField)
	}
	if body.Args.Rule != wantRule {
		t.Errorf("args.rule = %q, want %q", body.Args.Rule, wantRule)
	}
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logged bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })
	return &logged
}
