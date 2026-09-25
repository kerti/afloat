package httpserver

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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
		{
			// The padding is trimmed for the format check on this path too, so
			// the absent field is the only failure.
			name:      "email padded and password absent",
			body:      `{"email":"  a@example.com  "}`,
			wantField: "password",
			wantRule:  "required",
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
		name        string
		body        string
		contentType string
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
		{name: "data after the JSON value", body: `{"email":"a@example.com","password":"a valid password"} x`},
		{name: "a second JSON value", body: `{"email":"a@example.com","password":"a valid password"}{}`},
		{name: "data after the JSON value beside an absent field", body: `{"password":"a valid password"} x`},
		{name: "not UTF-8", body: `{"email":"a@example.com","password":"a valid ` + "\xff" + `password"}`},
		{name: "overlong UTF-8", body: `{"email":"a@example.com","password":"a valid` + "\xc0\xa0" + `password"}`},
		{name: "overlong UTF-8 beside an absent field", body: `{"password":"a valid` + "\xc0\xa0" + `password"}`},
		{name: "byte order mark", body: "\xef\xbb\xbf" + `{"email":"a@example.com","password":"a valid password"}`},
		{name: "UTF-16", body: utf16LE(`{"email":"a@example.com","password":"a valid password"}`)},
		// application/json has no charset parameter (RFC 8259 §11): a body is
		// UTF-8 whatever the header claims.
		{name: "not UTF-8, declared as Latin-1", body: `{"email":"a@example.com","password":"a valid ` + "\xff" + `password"}`, contentType: "application/json; charset=ISO-8859-1"},
		{name: "not declared as JSON", body: `{"email":"a@example.com","password":"a valid password"}`, contentType: "text/plain"},
		// Spring's JSON converter reads any +json type; the contract declares
		// application/json alone.
		{name: "declared as another JSON type", body: `{"email":"a@example.com","password":"a valid password"}`, contentType: "application/vnd.api+json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", cmp.Or(tc.contentType, "application/json"))

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

// A media type is case-insensitive and may have whitespace around it (RFC 9110
// §8.3.1), and a body is UTF-8 whatever charset is claimed, even one no
// decoder knows: each of these is JSON, read and validated, as Kotlin reads it
// (LoginSpec "reads every spelling of the JSON media type as Go does").
func TestLoginReadsEverySpellingOfTheJSONMediaType(t *testing.T) {
	srv := newTestServer()

	for _, contentType := range []string{
		"Application/JSON",
		"APPLICATION/JSON; CHARSET=UTF-8",
		"application/json ; charset=utf-8",
		"\tapplication/json\t;charset=utf-8",
		"application/json; charset=utf-16",
		"application/json; charset=bogus",
	} {
		t.Run(contentType, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(`{"email":"not-an-email","password":"a valid password"}`))
			req.Header.Set("Content-Type", contentType)

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			assertValidationArgs(t, rec, "email", "email")
		})
	}
}

// utf16LE encodes an ASCII string as UTF-16LE, which Jackson would detect
// and decode if Kotlin let it.
func utf16LE(ascii string) string {
	var b strings.Builder
	for i := range len(ascii) {
		b.WriteByte(ascii[i])
		b.WriteByte(0)
	}
	return b.String()
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
			name: "parameter breaking two rules",
			err: &openapi3filter.RequestError{Parameter: &openapi3.Parameter{Name: "cursor"}, Err: openapi3.MultiError{
				&openapi3.SchemaError{SchemaField: "pattern"},
				&openapi3.SchemaError{SchemaField: "maxLength"},
			}},
			wantStatus: http.StatusBadRequest,
			wantCode:   "VALIDATION",
			wantField:  "cursor",
			wantRule:   "max",
		},
		{
			// Kotlin's field for a date-time is an OffsetDateTime, which a bad
			// value fails to decode into.
			name:       "format other than email",
			err:        &openapi3filter.RequestError{RequestBody: &openapi3.RequestBody{}, Err: dateTimeFormatError(t)},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_JSON_BODY",
		},
		{
			name:       "error carrying no failure",
			err:        openapi3.MultiError{},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL",
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

// dateTimeFormatError is kin-openapi's own error for a body field breaking
// `format: date-time`: a SchemaError's JSON pointer can only be set by the
// visit itself.
func dateTimeFormatError(t *testing.T) error {
	t.Helper()
	schema := openapi3.NewObjectSchema().WithProperty("at", openapi3.NewDateTimeSchema())
	err := schema.VisitJSON(map[string]any{"at": "yesterday"}, openapi3.MultiErrors())
	if err == nil {
		t.Fatal("kin-openapi accepted a bad date-time")
	}
	return err
}

// `format: email` answers every address in the shared fixture exactly as
// Kotlin's TrimmedEmailValidator does (TrimmedEmailValidatorSpec).
func TestEmailFormatMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../contract/testdata/email.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture struct {
		Valid   []string `json:"valid"`
		Invalid []string `json:"invalid"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(fixture.Valid) == 0 || len(fixture.Invalid) == 0 {
		t.Fatal("fixture has an empty list")
	}
	for _, address := range fixture.Valid {
		if !isEmailAddress(address) {
			t.Errorf("%q rejected, fixture says valid", address)
		}
	}
	for _, address := range fixture.Invalid {
		if isEmailAddress(address) {
			t.Errorf("%q accepted, fixture says invalid", address)
		}
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

// postLoginToSpy sends body to a server whose Querier is an emailLookupSpy,
// reporting whether the handler got as far as the user lookup.
func postLoginToSpy(body string) (rec *httptest.ResponseRecorder, reachedLookup bool) {
	return postLoginToSpyAs("application/json", body)
}

func postLoginToSpyAs(contentType, body string) (rec *httptest.ResponseRecorder, reachedLookup bool) {
	q := emailLookupSpy{called: &reachedLookup}
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

	req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec, reachedLookup
}

// The oversize cases must be rejected before any Argon2 work happens — an
// unauthenticated caller must not get to choose how much data is hashed
// (#18's acceptance criteria, BOOTSTRAP.md §5.1's length cap).
func TestLoginDoesNotHashAnOversizePassword(t *testing.T) {
	rec, called := postLoginToSpy(`{"email":"a@example.com","password":"` + strings.Repeat("a", 4097) + `"}`)

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

// maxLength counts characters as JSON Schema does, one per code point, not one
// per UTF-16 unit: kin-openapi's loop counts runes, whatever its comment says,
// and Kotlin's @Size is re-bound to count the same way (LoginSpec "rejects an
// over-long email or password"). An upgrade that starts counting UTF-16 units
// fails the first case.
func TestLoginCountsPasswordLengthInCodePoints(t *testing.T) {
	const emoji = "\U0001F600" // two UTF-16 units, four UTF-8 bytes

	rec, called := postLoginToSpy(`{"email":"a@example.com","password":"` + strings.Repeat(emoji, 4096) + `"}`)
	if !called {
		t.Errorf("4096 code points did not reach the handler: status %d (body %s)", rec.Code, rec.Body.String())
	}

	rec, called = postLoginToSpy(`{"email":"a@example.com","password":"` + strings.Repeat(emoji, 4097) + `"}`)
	if called {
		t.Error("4097 code points reached the handler")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	assertValidationArgs(t, rec, "password", "max")
}

// A body declared as Latin-1 is still read as UTF-8, so 4096 "é" are 4096
// characters, not the 8192 a Latin-1 reading makes of their bytes (Kotlin's
// Utf8CharsetFilter; LoginSpec "rejects an over-long email or password").
func TestLoginReadsABodyDeclaredLatin1AsUTF8(t *testing.T) {
	rec, called := postLoginToSpyAs("application/json; charset=ISO-8859-1",
		`{"email":"a@example.com","password":"`+strings.Repeat("é", 4096)+`"}`)
	if !called {
		t.Errorf("4096 characters declared as Latin-1 did not reach the handler: status %d (body %s)", rec.Code, rec.Body.String())
	}
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

	// A newline is padding net/mail refuses, which the generated
	// openapi_types.Email once rejected after this format check had passed.
	body := `{"email":"\n  A@Example.COM\t","password":"` + password + `"}`
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
