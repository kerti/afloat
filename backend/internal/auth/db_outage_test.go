package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
// query" scenario can be exercised without faking the whole database. The
// failure is errOutage unless err says otherwise.
type failingQuerier struct {
	db.Querier

	failGetUserByEmail        bool
	failGetCredentialByUserID bool
	failGetUserByID           bool
	failGetLiveSession        bool
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

func (f failingQuerier) GetLiveSession(ctx context.Context, arg db.GetLiveSessionParams) (db.Session, error) {
	if f.failGetLiveSession {
		return db.Session{}, f.failure()
	}
	return f.Querier.GetLiveSession(ctx, arg)
}

func (f failingQuerier) GetUserByID(ctx context.Context, id pgtype.UUID) (db.User, error) {
	if f.failGetUserByID {
		return db.User{}, f.failure()
	}
	return f.Querier.GetUserByID(ctx, id)
}

// #23: a database outage during either of resolve's lookups is an
// infrastructure failure, not a statement about this account's credentials. It
// must answer 500, and — unlike a real INVALID_CREDENTIALS — must not burn a
// backoff slot the account did nothing to earn.
func TestLoginAnswers500OnDatabaseOutageDuringALookupAndDoesNotRecordFailure(t *testing.T) {
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
			if out := logged.String(); strings.Count(out, `level=WARN msg="`+tc.msg+`"`) != 1 ||
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
			if out := logged.String(); strings.Count(out, `level=WARN msg="`+tc.msg+`"`) != 1 ||
				strings.Contains(out, "level=ERROR") {
				t.Errorf("want one Warn for %q and no Error; logged:\n%s", tc.msg, out)
			}
		})
	}
}

// GetMe is the client's session probe on every load, so a client that leaves
// mid-request is as common there as in SessionMiddleware: Warn, not Error, on
// GetMe's household lookup and on Logout's delete. Both still answer 500.
func TestGetMeAndLogoutLogAClientLeavingAsAWarning(t *testing.T) {
	gone := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}

	for name, tc := range map[string]struct {
		call func(h harness, user db.User, token string) (any, error)
		msg  string
	}{
		"me": {
			call: func(h harness, user db.User, _ string) (any, error) {
				return h.auth.GetMe(auth.WithUser(gone(), user), api.GetMeRequestObject{})
			},
			msg: "me: look up household",
		},
		"logout": {
			call: func(h harness, _ db.User, token string) (any, error) {
				return h.auth.Logout(auth.ContextForTest(gone(), "198.51.100.52", "", token), api.LogoutRequestObject{})
			},
			msg: "logout: delete session",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			householdID := h.tdb.CreateHousehold(t, "Test Household")
			userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
			user, err := h.tdb.Queries.GetUserByID(context.Background(), userID)
			if err != nil {
				t.Fatalf("GetUserByID: %v", err)
			}
			cookie, err := h.auth.IssueSession(context.Background(), userID, "")
			if err != nil {
				t.Fatalf("IssueSession: %v", err)
			}
			logged := captureLog(t)

			resp, err := tc.call(h, user, cookie.Value)
			if err != nil {
				t.Fatalf("call: %v", err)
			}

			switch resp.(type) {
			case api.GetMe500JSONResponse, api.Logout500JSONResponse:
			default:
				t.Fatalf("response = %T, want 500", resp)
			}
			if out := logged.String(); strings.Count(out, `level=WARN msg="`+tc.msg+`"`) != 1 ||
				strings.Contains(out, "level=ERROR") {
				t.Errorf("want one Warn for %q and no Error; logged:\n%s", tc.msg, out)
			}
		})
	}
}

// SessionMiddleware tells a session it could not read from one it read and
// found dead, because sessionRequired answers them differently: 500 for the
// first, 401 for the rest. Only the first leaves the cookie alone and sets the
// flag; ErrNoRows on either lookup clears the cookie and does not; no cookie is
// neither.
func TestSessionMiddlewareFlagsOnlyAnUnreadableSession(t *testing.T) {
	for name, tc := range map[string]struct {
		cookie        string // "valid", "unknown", "" for none
		fail          failingQuerier
		softDelete    bool
		wantUnresolve bool
		wantCleared   bool
	}{
		"no cookie":                    {cookie: ""},
		"session lookup outage":        {cookie: "valid", fail: failingQuerier{failGetLiveSession: true}, wantUnresolve: true},
		"user lookup outage":           {cookie: "valid", fail: failingQuerier{failGetUserByID: true}, wantUnresolve: true},
		"session lookup wrapped error": {cookie: "valid", fail: failingQuerier{failGetLiveSession: true, err: fmt.Errorf("get session: %w", context.DeadlineExceeded)}, wantUnresolve: true},
		"unknown or expired session":   {cookie: "unknown", wantCleared: true},
		"session outlived its user":    {cookie: "valid", softDelete: true, wantCleared: true},
		"healthy session":              {cookie: "valid"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			householdID := h.tdb.CreateHousehold(t, "Test Household")
			userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
			issued, err := h.auth.IssueSession(context.Background(), userID, "")
			if err != nil {
				t.Fatalf("IssueSession: %v", err)
			}
			if tc.softDelete {
				if _, err := h.tdb.Pool.Exec(context.Background(),
					`UPDATE users SET deleted_at = now() WHERE id = $1`, userID); err != nil {
					t.Fatalf("soft-delete user: %v", err)
				}
			}
			token := map[string]string{"valid": issued.Value, "unknown": "never-issued", "": ""}[tc.cookie]
			tc.fail.Querier = h.tdb.Queries
			h = h.withQuerier(tc.fail)

			var unresolved bool
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				unresolved = auth.SessionUnresolved(r.Context())
			})
			req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			if token != "" {
				req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
			}
			rec := httptest.NewRecorder()
			h.auth.SessionMiddleware(next).ServeHTTP(rec, req)

			if unresolved != tc.wantUnresolve {
				t.Errorf("SessionUnresolved = %v, want %v", unresolved, tc.wantUnresolve)
			}
			if got := clearsCookie(rec); got != tc.wantCleared {
				t.Errorf("clears the cookie = %v, want %v", got, tc.wantCleared)
			}
			if tc.wantUnresolve && len(rec.Result().Cookies()) != 0 {
				t.Errorf("Set-Cookie = %v, want none: an unreadable session must never sign anyone out", rec.Result().Cookies())
			}
		})
	}
}
