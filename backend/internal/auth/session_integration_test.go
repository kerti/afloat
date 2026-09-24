package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/testutil"
)

const (
	testTTL         = 30 * 24 * time.Hour
	testMaxLifetime = 90 * 24 * time.Hour
)

type harness struct {
	tdb  *testutil.TestDB
	auth *auth.Handlers
	now  func() time.Time
}

func newHarness(t *testing.T, clock func() time.Time) harness {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	return harness{tdb: tdb, now: clock}.withQuerier(tdb.Queries)
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

// resolve runs a request through SessionMiddleware and reports whether a User
// came out the other side — which is the only thing callers care about.
func (h harness) resolve(t *testing.T, token string) (db.User, bool, *httptest.ResponseRecorder) {
	t.Helper()

	var got db.User
	var found bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, found = auth.UserFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	h.auth.SessionMiddleware(next).ServeHTTP(rec, req)
	return got, found, rec
}

func TestSessionTokenIsStoredHashed(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := context.Background()

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")

	cookie, err := h.auth.IssueSession(ctx, userID, "Go-Test")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	// The plaintext must not be anywhere in the table: a database leak has to
	// yield nothing that can be presented as a cookie.
	var count int
	if err := h.tdb.Pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE id = $1`, cookie.Value).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Error("the plaintext token is stored in sessions.id")
	}

	if err := h.tdb.Pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE id = $1`, auth.HashToken(cookie.Value)).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("found %d rows for the hashed token, want 1", count)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	h := newHarness(t, time.Now)
	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")

	cookie, err := h.auth.IssueSession(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	if cookie.Name != "afloat_session" {
		t.Errorf("name = %q, want afloat_session — a bare `session` collides with Balances behind one hostname", cookie.Name)
	}
	// Host-only: a Domain attribute would leak a demo session into preview
	// under a shared parent (BOOTSTRAP.md §5).
	if cookie.Domain != "" {
		t.Errorf("Domain = %q, want empty (host-only)", cookie.Domain)
	}
	if !cookie.HttpOnly {
		t.Error("HttpOnly is not set")
	}
	if !cookie.Secure {
		t.Error("Secure is not set")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want /", cookie.Path)
	}
}

// Max-Age is the attribute a browser prefers over Expires, so it is the one
// that decides how long a session survives on a client with a skewed clock —
// and it is the one the Kotlin backend must spell identically
// (SessionCookieFactory). Nothing asserted its VALUE until this test, so the
// two backends were free to round it differently.
//
// The clock is fixed and in the past: computed against time.Now() rather than
// h.now(), the whole TTL would already have elapsed and this would read 1.
func TestSessionCookieMaxAgeIsTheFullTTL(t *testing.T) {
	fixed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	h := newHarness(t, func() time.Time { return fixed })
	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")

	cookie, err := h.auth.IssueSession(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	if want := int(testTTL.Seconds()); cookie.MaxAge != want {
		t.Errorf("Max-Age = %d, want %d (the full SESSION_TTL)", cookie.MaxAge, want)
	}
	if !cookie.Expires.Equal(fixed.Add(testTTL)) {
		t.Errorf("Expires = %v, want %v", cookie.Expires, fixed.Add(testTTL))
	}
}

// The cleared cookie is the one both backends must agree on most literally: a
// browser that does not delete its copy keeps presenting a token the server
// can never honour. Go renders MaxAge < 0 as `Max-Age=0`, which is what
// Spring's ResponseCookie.maxAge(0) emits (LogoutSpec asserts the same value).
func TestClearedSessionCookieDeletesTheClientsCopy(t *testing.T) {
	h := newHarness(t, time.Now)

	cookie := h.auth.ClearedSessionCookie()

	if cookie.Value != "" {
		t.Errorf("Value = %q, want empty", cookie.Value)
	}
	if cookie.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative so the header reads Max-Age=0", cookie.MaxAge)
	}
	if got := cookie.String(); !strings.Contains(got, "Max-Age=0") {
		t.Errorf("Set-Cookie = %q, want it to carry Max-Age=0", got)
	}
}

func TestSessionResolvesToItsUser(t *testing.T) {
	h := newHarness(t, time.Now)
	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")

	cookie, err := h.auth.IssueSession(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	user, ok, _ := h.resolve(t, cookie.Value)
	if !ok {
		t.Fatal("a fresh session did not resolve")
	}
	if user.ID != userID {
		t.Errorf("resolved user = %v, want %v", user.ID, userID)
	}
}

func TestUnknownOrAbsentTokenResolvesToNobody(t *testing.T) {
	h := newHarness(t, time.Now)

	if _, ok, _ := h.resolve(t, ""); ok {
		t.Error("a request with no cookie resolved to a user")
	}
	if _, ok, rec := h.resolve(t, "not-a-real-token"); ok {
		t.Error("an unknown token resolved to a user")
	} else if !clearsCookie(rec) {
		t.Error("an unknown token did not clear the cookie; the browser keeps presenting it")
	}
}

// The sliding window: a session past its expiry is dead even though the
// absolute cap has not been reached.
func TestExpiredSessionIsRejected(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	h := newHarness(t, clock.Now)

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
	cookie, err := h.auth.IssueSession(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	// Push the row's expiry into the past. The clock seam moves Go's view of
	// time; the SQL compares against the database's now(), so the row has to
	// move too.
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE sessions SET expires_at = now() - interval '1 second' WHERE id = $1`,
		auth.HashToken(cookie.Value)); err != nil {
		t.Fatalf("expire session: %v", err)
	}

	if _, ok, _ := h.resolve(t, cookie.Value); ok {
		t.Error("an expired session resolved")
	}
}

// The absolute cap, which is the departure from Balances: a session that has
// been kept alive by continued use still dies at max lifetime. Without it a
// stolen cookie is valid forever.
func TestSessionDiesAtAbsoluteLifetimeDespiteBeingFresh(t *testing.T) {
	h := newHarness(t, time.Now)

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
	cookie, err := h.auth.IssueSession(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	// Created long ago, but refreshed moments ago — exactly the state an
	// attacker maintains by using the cookie.
	if _, err := h.tdb.Pool.Exec(context.Background(), `
		UPDATE sessions
		SET created_at = now() - $2::interval - interval '1 hour',
		    expires_at = now() + interval '30 days'
		WHERE id = $1`,
		auth.HashToken(cookie.Value), testMaxLifetime.String()); err != nil {
		t.Fatalf("age session: %v", err)
	}

	if _, ok, _ := h.resolve(t, cookie.Value); ok {
		t.Error("a session past its absolute lifetime resolved, despite a future expires_at")
	}
}

// The threshold touch: a fresh session must NOT be written on every request,
// which is what makes every GET a write in Balances.
func TestFreshSessionIsNotTouched(t *testing.T) {
	h := newHarness(t, time.Now)

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
	cookie, err := h.auth.IssueSession(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	before := sessionLastSeen(t, h.tdb, cookie.Value)
	if _, ok, rec := h.resolve(t, cookie.Value); !ok {
		t.Fatal("session did not resolve")
	} else if len(rec.Result().Cookies()) != 0 {
		t.Error("a fresh session re-set the cookie; it should not have been touched")
	}
	if after := sessionLastSeen(t, h.tdb, cookie.Value); !after.Equal(before) {
		t.Errorf("last_seen_at moved (%v -> %v) for a fresh session", before, after)
	}
}

// Past half its life, the window does slide — and the cookie must keep the
// PLAINTEXT token, or the next request's hash would never match this row.
func TestStaleSessionIsTouchedAndKeepsItsPlaintextToken(t *testing.T) {
	h := newHarness(t, time.Now)

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
	cookie, err := h.auth.IssueSession(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	// Just past the halfway point.
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE sessions SET expires_at = now() + interval '1 day' WHERE id = $1`,
		auth.HashToken(cookie.Value)); err != nil {
		t.Fatalf("age session: %v", err)
	}

	_, ok, rec := h.resolve(t, cookie.Value)
	if !ok {
		t.Fatal("session did not resolve")
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("a stale session was not refreshed")
	}
	if cookies[0].Value != cookie.Value {
		t.Error("the refreshed cookie changed value; the stored hash would no longer match")
	}

	var expiresAt time.Time
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT expires_at FROM sessions WHERE id = $1`, auth.HashToken(cookie.Value)).Scan(&expiresAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if time.Until(expiresAt) < testTTL-time.Hour {
		t.Errorf("expires_at = %v, want roughly now + %v", expiresAt, testTTL)
	}
}

// A session outliving its User — soft-deleted — must not resolve, and clears
// the cookie so the browser stops presenting it.
func TestSessionOfSoftDeletedUserDoesNotResolve(t *testing.T) {
	h := newHarness(t, time.Now)

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
	cookie, err := h.auth.IssueSession(context.Background(), userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE users SET deleted_at = now() WHERE id = $1`, userID); err != nil {
		t.Fatalf("soft-delete user: %v", err)
	}

	_, ok, rec := h.resolve(t, cookie.Value)
	if ok {
		t.Error("a soft-deleted User's session still resolved")
	}
	// The one GetUserByID error that does clear it: an outage must not (#27),
	// so the clear is conditional and needs pinning from this side too.
	if !clearsCookie(rec) {
		t.Error("a soft-deleted User's session did not clear the cookie; the browser keeps presenting it")
	}
}

// Revoking one User's sessions must not touch another's — the query is the
// "reset because compromised" guarantee, and it running too wide would log the
// whole Household out.
func TestDeleteSessionsForUserIsScopedToThatUser(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := context.Background()

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	alice := h.tdb.CreateUser(t, householdID, "alice@example.com", "Alice")
	bob := h.tdb.CreateUser(t, householdID, "bob@example.com", "Bob")

	aliceCookie, err := h.auth.IssueSession(ctx, alice, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	bobCookie, err := h.auth.IssueSession(ctx, bob, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	if err := h.tdb.Queries.DeleteSessionsForUser(ctx, alice); err != nil {
		t.Fatalf("DeleteSessionsForUser: %v", err)
	}

	if _, ok, _ := h.resolve(t, aliceCookie.Value); ok {
		t.Error("Alice's session survived revocation")
	}
	if _, ok, _ := h.resolve(t, bobCookie.Value); !ok {
		t.Error("Bob's session was revoked along with Alice's")
	}
}

func sessionLastSeen(t *testing.T, tdb *testutil.TestDB, token string) time.Time {
	t.Helper()
	var seen time.Time
	err := tdb.Pool.QueryRow(context.Background(),
		`SELECT last_seen_at FROM sessions WHERE id = $1`, auth.HashToken(token)).Scan(&seen)
	if err != nil {
		if err == pgx.ErrNoRows {
			t.Fatal("session row is gone")
		}
		t.Fatalf("query last_seen_at: %v", err)
	}
	return seen
}

func clearsCookie(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }
