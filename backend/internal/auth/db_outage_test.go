package auth_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// errOutage stands in for a database outage: neither pgx.ErrNoRows nor
// anything else callers are meant to special-case, so a test that leaks it
// unhandled into a 401 or a cleared cookie is caught immediately.
var errOutage = errors.New("db_outage_test: simulated outage")

// failingQuerier wraps a real Querier and fails exactly the calls the test
// names, delegating everything else — so a "database is down for this one
// query" scenario can be exercised without faking the whole database. The
// failure is errOutage unless err says otherwise.
type failingQuerier struct {
	db.Querier

	failGetUserByEmail        bool
	failGetCredentialByUserID bool
	failGetUserByID           bool
	err                       error
}

func (f failingQuerier) failure() error {
	if f.err != nil {
		return f.err
	}
	return errOutage
}

func (f failingQuerier) GetUserByEmail(ctx context.Context, email string) (db.User, error) {
	if f.failGetUserByEmail {
		return db.User{}, f.failure()
	}
	return f.Querier.GetUserByEmail(ctx, email)
}

func (f failingQuerier) GetCredentialByUserID(ctx context.Context, userID pgtype.UUID) (db.Credential, error) {
	if f.failGetCredentialByUserID {
		return db.Credential{}, f.failure()
	}
	return f.Querier.GetCredentialByUserID(ctx, userID)
}

func (f failingQuerier) GetUserByID(ctx context.Context, id pgtype.UUID) (db.User, error) {
	if f.failGetUserByID {
		return db.User{}, f.failure()
	}
	return f.Querier.GetUserByID(ctx, id)
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

// A client that leaves before the permit ends the lookups ahead of it, and is
// no more the server's fault there than in the permit wait: Warn, not Error,
// on either lookup that runs on the request's ctx. Still a 500 and no failure
// recorded, as for any lookup that did not answer.
func TestLoginLogsAClientLeavingDuringALookupAsAWarning(t *testing.T) {
	for name, tc := range map[string]struct {
		ctx  func(context.Context) context.Context
		fail failingQuerier
		msg  string
	}{
		// A real cancelled request: pgx gives back context.Canceled from the
		// first query, which is the backoff read.
		"backoff read": {
			ctx: func(ctx context.Context) context.Context {
				ctx, cancel := context.WithCancel(ctx)
				cancel()
				return ctx
			},
			msg: "login: read backoff",
		},
		// The backoff read answered and the client left during the next one.
		"user lookup": {
			ctx:  func(ctx context.Context) context.Context { return ctx },
			fail: failingQuerier{failGetUserByEmail: true, err: fmt.Errorf("get user: %w", context.Canceled)},
			msg:  "login: resolve credential",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			h.seedCredentialedUser(t, "a@example.com")
			tc.fail.Querier = h.tdb.Queries
			h = h.withQuerier(tc.fail)
			logged := captureLog(t)

			resp := h.login(tc.ctx(ipContext("198.51.100.51")), t, "a@example.com", goodPassword)

			if _, is := resp.(api.LocalLogin500JSONResponse); !is {
				t.Fatalf("response = %T, want 500", resp)
			}
			if out := logged.String(); !strings.Contains(out, `level=WARN msg="`+tc.msg+`"`) ||
				strings.Contains(out, "level=ERROR") {
				t.Errorf("want one Warn for %q and no Error; logged:\n%s", tc.msg, out)
			}
			if rows := h.loginAttemptRows(t); rows != 0 {
				t.Errorf("login_attempts rows = %d, want 0", rows)
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

// The same for SessionMiddleware, which runs on every authenticated request:
// a client that left mid-lookup logs Warn, not Error, on either lookup, and
// the cookie is left alone as for any lookup that did not answer (#27).
func TestSessionMiddlewareLogsAClientLeavingDuringALookupAsAWarning(t *testing.T) {
	for name, tc := range map[string]struct {
		ctx  func(context.Context) context.Context
		fail failingQuerier
		msg  string
	}{
		// A real cancelled request: pgx gives back context.Canceled from the
		// first query, which is the session lookup.
		"session lookup": {
			ctx: func(ctx context.Context) context.Context {
				ctx, cancel := context.WithCancel(ctx)
				cancel()
				return ctx
			},
			msg: "session lookup",
		},
		// The session lookup answered and the client left during the next one.
		"user lookup": {
			ctx:  func(ctx context.Context) context.Context { return ctx },
			fail: failingQuerier{failGetUserByID: true, err: fmt.Errorf("get user: %w", context.Canceled)},
			msg:  "session user lookup",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			householdID := h.tdb.CreateHousehold(t, "Test Household")
			userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
			cookie, err := h.auth.IssueSession(context.Background(), userID, "")
			if err != nil {
				t.Fatalf("IssueSession: %v", err)
			}
			tc.fail.Querier = h.tdb.Queries
			h = h.withQuerier(tc.fail)
			logged := captureLog(t)

			_, ok, rec := h.resolveWithContext(tc.ctx(context.Background()), t, cookie.Value)

			if ok {
				t.Error("a lookup that did not answer still produced an authenticated request")
			}
			if len(rec.Result().Cookies()) != 0 {
				t.Errorf("Set-Cookie header present = %v, want none", rec.Result().Cookies())
			}
			if out := logged.String(); !strings.Contains(out, `level=WARN msg="`+tc.msg+`"`) ||
				strings.Contains(out, "level=ERROR") {
				t.Errorf("want one Warn for %q and no Error; logged:\n%s", tc.msg, out)
			}
		})
	}
}
