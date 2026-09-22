package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

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
			name:      "unknown property",
			body:      `{"email":"a@example.com","password":"a valid password","admin":true}`,
			wantField: "admin",
			wantRule:  "unknown",
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

// A body the JSON decoder can parse but whose root isn't an object at all is
// not a field failing a constraint — it's the same "did not decode as what
// the contract declares" bucket as truncated or malformed JSON.
func TestLoginRequestValidationRootTypeMismatchIsInvalidJSONBody(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(`["not","an","object"]`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertEnvelope(t, rec, "INVALID_JSON_BODY")
}

// spyQuerier records whether GetUserByEmail was reached, standing in for
// "did the handler start doing Argon2 work". verify() (login.go) calls
// VerifyPassword unconditionally once it gets past the lookup — for both a
// found and an unknown address, by design (enumeration resistance) — so a
// GetUserByEmail that was never called is proof the handler, and therefore
// Argon2, never ran at all.
type spyQuerier struct {
	db.Querier
	called *bool
}

func (s spyQuerier) GetUserByEmail(context.Context, string) (db.User, error) {
	*s.called = true
	return db.User{}, pgx.ErrNoRows
}

// The oversize cases must be rejected before any Argon2 work happens — an
// unauthenticated caller must not get to choose how much data is hashed
// (#18's acceptance criteria, BOOTSTRAP.md §5.1's length cap).
func TestLoginDoesNotHashAnOversizePassword(t *testing.T) {
	called := false
	q := spyQuerier{called: &called}
	srv := New(Deps{
		System: system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: true}),
		Auth: auth.New(auth.Deps{
			Querier:            q,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
	})

	body := `{"email":"a@example.com","password":"` + strings.Repeat("a", 4097) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	assertValidationArgs(t, rec, "password", "max")

	if called {
		t.Error("GetUserByEmail was called for an oversize password; the handler ran before validation rejected it, so Argon2 hashed the whole thing")
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
	hash, err := auth.HashPassword(password)
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
