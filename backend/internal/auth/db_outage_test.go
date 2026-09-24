package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
)

// errOutage stands in for a database outage: neither pgx.ErrNoRows nor
// anything else callers are meant to special-case, so a test that leaks it
// unhandled into a 401 or a cleared cookie is caught immediately.
var errOutage = errors.New("db_outage_test: simulated outage")

// failingQuerier wraps a real Querier and fails exactly the calls the test
// names, delegating everything else — so a "database is down for this one
// query" scenario can be exercised without faking the whole database.
type failingQuerier struct {
	db.Querier

	failGetUserByEmail        bool
	failGetCredentialByUserID bool
	failGetUserByID           bool
}

func (f failingQuerier) GetUserByEmail(ctx context.Context, email string) (db.User, error) {
	if f.failGetUserByEmail {
		return db.User{}, errOutage
	}
	return f.Querier.GetUserByEmail(ctx, email)
}

func (f failingQuerier) GetCredentialByUserID(ctx context.Context, userID pgtype.UUID) (db.Credential, error) {
	if f.failGetCredentialByUserID {
		return db.Credential{}, errOutage
	}
	return f.Querier.GetCredentialByUserID(ctx, userID)
}

func (f failingQuerier) GetUserByID(ctx context.Context, id pgtype.UUID) (db.User, error) {
	if f.failGetUserByID {
		return db.User{}, errOutage
	}
	return f.Querier.GetUserByID(ctx, id)
}

// withQuerier swaps h's Handlers for one wired to q, reusing h's existing
// TestDB — a fresh newHarness call would re-truncate the tables and erase
// whatever the test already seeded.
func (h harness) withQuerier(q db.Querier) harness {
	h.auth = auth.New(auth.Deps{
		Querier:            q,
		Beginner:           h.tdb.Pool,
		SessionTTL:         testTTL,
		SessionMaxLifetime: testMaxLifetime,
		CookieSecure:       true,
		Now:                h.now,
	})
	return h
}

// #23: a database outage during either credential lookup is an
// infrastructure failure, not a statement about this account's credentials. It
// must answer 500, and — unlike a real INVALID_CREDENTIALS — must not burn a
// backoff slot the account did nothing to earn.
func TestLoginAnswers500OnDatabaseOutageDuringCredentialLookupAndDoesNotRecordFailure(t *testing.T) {
	for name, fail := range map[string]failingQuerier{
		"user lookup":       {failGetUserByEmail: true},
		"credential lookup": {failGetCredentialByUserID: true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			h.seedCredentialedUser(t, "a@example.com")
			fail.Querier = h.tdb.Queries
			h = h.withQuerier(fail)

			resp := h.login(ipContext("198.51.100.50"), t, "a@example.com", goodPassword)

			got, is := resp.(api.LocalLogin500JSONResponse)
			if !is {
				t.Fatalf("response = %T, want 500", resp)
			}
			if got.Code != api.INTERNAL {
				t.Errorf("code = %v, want INTERNAL", got.Code)
			}
			if rows := h.loginAttemptRows(t); rows != 0 {
				t.Errorf("login_attempts rows = %d, want 0 — an outage must not leave a backoff window behind", rows)
			}
		})
	}
}

// #27: a database outage resolving the session's User must leave the cookie
// alone. Only pgx.ErrNoRows (the session outliving a soft-deleted User) may
// clear it; anything else logs and lets the request proceed unauthenticated.
func TestSessionMiddlewareLeavesCookieAloneOnDatabaseOutageDuringUserLookup(t *testing.T) {
	h := newHarness(t, time.Now)
	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")

	cookie, err := h.auth.IssueSession(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	h = h.withQuerier(failingQuerier{
		Querier:         h.tdb.Queries,
		failGetUserByID: true,
	})

	_, ok, rec := h.resolve(t, cookie.Value)
	if ok {
		t.Error("a database outage resolving the User still produced an authenticated request")
	}
	if clearsCookie(rec) {
		t.Error("a database outage cleared the session cookie; only pgx.ErrNoRows may do that")
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("Set-Cookie header present = %v, want none", rec.Result().Cookies())
	}
}
