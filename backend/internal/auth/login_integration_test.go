package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
)

const goodPassword = "kucing oranye di atap"

func (h harness) seedCredentialedUser(t *testing.T, email string) (pgtype.UUID, pgtype.UUID) {
	t.Helper()
	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, email, "Test User")

	hash, err := auth.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := h.tdb.Queries.UpsertCredential(context.Background(), db.UpsertCredentialParams{
		UserID:       userID,
		PasswordHash: hash,
	}); err != nil {
		t.Fatalf("UpsertCredential: %v", err)
	}
	return householdID, userID
}

func (h harness) login(ctx context.Context, t *testing.T, email, password string) api.LocalLoginResponseObject {
	t.Helper()
	resp, err := h.auth.LocalLogin(ctx, api.LocalLoginRequestObject{
		Body: &api.LocalLoginJSONRequestBody{
			Email:    openapi_types.Email(email),
			Password: password,
		},
	})
	if err != nil {
		t.Fatalf("LocalLogin returned an error: %v", err)
	}
	return resp
}

// A request context carrying a distinct IP, so per-IP backoff keys do not
// collide between tests sharing the database.
func ipContext(ip string) context.Context {
	return auth.ContextForTest(context.Background(), ip, "Go-Test", "")
}

func TestLoginSucceedsAndSetsASession(t *testing.T) {
	h := newHarness(t, time.Now)
	_, userID := h.seedCredentialedUser(t, "a@example.com")

	resp := h.login(ipContext("198.51.100.1"), t, "a@example.com", goodPassword)

	ok, is := resp.(api.LocalLogin204Response)
	if !is {
		t.Fatalf("response = %T, want 204", resp)
	}
	if ok.Headers.SetCookie == nil || *ok.Headers.SetCookie == "" {
		t.Fatal("login returned no Set-Cookie")
	}

	var count int
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("sessions for the user = %d, want 1", count)
	}
}

// Email is a handle, not an identity: a capitalised address is the same account.
func TestLoginIsCaseInsensitiveOnEmail(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")

	resp := h.login(ipContext("198.51.100.2"), t, "  A@ExAmPlE.CoM  ", goodPassword)
	if _, is := resp.(api.LocalLogin204Response); !is {
		t.Fatalf("response = %T, want 204 for a differently-cased address", resp)
	}
}

// One code for every failure mode. Splitting them — or letting one path 404
// while another 401s — enumerates accounts.
func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "known@example.com")

	// A dormant User: present, owns data, has never set a password.
	householdID := h.tdb.CreateHousehold(t, "Dormant Household")
	h.tdb.CreateUser(t, householdID, "dormant@example.com", "Dormant")

	for i, tc := range []struct{ name, email, password string }{
		{"unknown address", "nobody@example.com", goodPassword},
		{"dormant user, no credential", "dormant@example.com", goodPassword},
		{"known address, wrong password", "known@example.com", "wrong password entirely"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh IP per case: otherwise the second attempt trips backoff
			// and the test would pass for the wrong reason.
			ctx := ipContext("203.0.113." + string(rune('1'+i)))
			resp := h.login(ctx, t, tc.email, tc.password)

			got, is := resp.(api.LocalLogin401JSONResponse)
			if !is {
				t.Fatalf("response = %T, want 401", resp)
			}
			if got.Code != api.INVALIDCREDENTIALS {
				t.Errorf("code = %v, want INVALID_CREDENTIALS", got.Code)
			}
		})
	}
}

// The timing half of enumeration resistance: an unknown address must pay the
// same Argon2id cost as a real one, or response time answers the question the
// status code refuses to.
func TestUnknownAddressPaysTheHashingCost(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "known@example.com")

	known := time.Now()
	h.login(ipContext("198.51.100.10"), t, "known@example.com", "wrong password entirely")
	knownElapsed := time.Since(known)

	unknown := time.Now()
	h.login(ipContext("198.51.100.11"), t, "nobody@example.com", "wrong password entirely")
	unknownElapsed := time.Since(unknown)

	// Generous: this is about orders of magnitude, not precision. A skipped
	// hash returns in microseconds against tens of milliseconds.
	if unknownElapsed < knownElapsed/4 {
		t.Errorf("unknown address answered in %v against %v for a known one; the hash was skipped",
			unknownElapsed, knownElapsed)
	}
}

func TestLoginBacksOffAfterRepeatedFailures(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")
	ctx := ipContext("198.51.100.20")

	// First failure arms the backoff.
	if _, is := h.login(ctx, t, "a@example.com", "wrong").(api.LocalLogin401JSONResponse); !is {
		t.Fatal("first failure did not return 401")
	}

	resp := h.login(ctx, t, "a@example.com", "wrong")
	got, is := resp.(api.LocalLogin429JSONResponse)
	if !is {
		t.Fatalf("second attempt = %T, want 429", resp)
	}
	if got.Body.Code != api.TOOMANYATTEMPTS {
		t.Errorf("code = %v, want TOO_MANY_ATTEMPTS", got.Body.Code)
	}
	// Rounded up: a Retry-After of 0 invites an immediate retry inside the window.
	if got.Headers.RetryAfter == nil || *got.Headers.RetryAfter < 1 {
		t.Errorf("Retry-After = %v, want at least 1", got.Headers.RetryAfter)
	}
}

// Backoff, never a hard lockout: the correct password must work once the window
// passes, or a self-hosted household is locked out of its own data.
func TestBackoffExpiresAndTheCorrectPasswordWorks(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")
	ctx := ipContext("198.51.100.21")

	h.login(ctx, t, "a@example.com", "wrong")

	// Move the window into the past rather than sleeping for it.
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE login_attempts SET backoff_until = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire backoff: %v", err)
	}

	if _, is := h.login(ctx, t, "a@example.com", goodPassword).(api.LocalLogin204Response); !is {
		t.Error("the correct password was refused after the backoff window passed")
	}
}

// A success clears the counter, so an unlucky evening does not leave a
// household throttled for the rest of it.
func TestSuccessfulLoginClearsBackoff(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")
	ctx := ipContext("198.51.100.22")

	h.login(ctx, t, "a@example.com", "wrong")
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE login_attempts SET backoff_until = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire backoff: %v", err)
	}
	h.login(ctx, t, "a@example.com", goodPassword)

	var remaining int
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM login_attempts`).Scan(&remaining); err != nil {
		t.Fatalf("query: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d login_attempts rows survived a successful login, want 0", remaining)
	}
}

// Per-email as well as per-IP: per-IP alone lets an attacker spread across
// addresses, and this proves the email key is armed independently.
func TestBackoffAppliesPerEmailAcrossAddresses(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "target@example.com")

	h.login(ipContext("198.51.100.30"), t, "target@example.com", "wrong")

	resp := h.login(ipContext("198.51.100.31"), t, "target@example.com", "wrong")
	if _, is := resp.(api.LocalLogin429JSONResponse); !is {
		t.Errorf("a second address attacking one account = %T, want 429", resp)
	}
}
