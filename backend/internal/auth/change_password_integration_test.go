package auth_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
)

const newPassword = "tikus putih di lumbung"

// Success: 204 with the new session's cookie, the new token live and every
// older one dead (the caller's own, and another device's), the new password
// signs in and the old does not.
func TestChangePasswordRevokesEverySessionAndMintsTheCallers(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	callers := h.signIn(t, m)
	otherDevice := h.signIn(t, m)
	ctx := h.ctxWithToken(t, m, "198.51.100.1", callers)

	cookie := changedPassword(t, h.changePassword(ctx, t, goodPassword, newPassword))

	if !h.live(t, cookie.Value) {
		t.Error("the new session does not resolve")
	}
	if h.live(t, callers) {
		t.Error("the caller's OLD session still resolves: the thief who copied this browser's cookie keeps it")
	}
	if h.live(t, otherDevice) {
		t.Error("another device's session still resolves")
	}
	if n := h.count(t, "sessions"); n != 1 {
		t.Errorf("sessions = %d, want exactly the new one", n)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.Domain != "" || cookie.MaxAge < 1 {
		t.Errorf("cookie = %+v, want the session cookie's attributes", cookie)
	}

	if _, is := h.login(ipContext("198.51.100.2"), t, m.email, newPassword).(api.LocalLogin204Response); !is {
		t.Error("the new password does not sign in")
	}
	if _, is := h.login(ipContext("198.51.100.3"), t, m.email, goodPassword).(api.LocalLogin401JSONResponse); !is {
		t.Error("the old password still signs in")
	}
}

// A new password equal to the current one is allowed: refusing it is
// enforcement the PRD does not ask for.
func TestChangePasswordToTheSamePasswordIsAllowed(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	changedPassword(t, h.changePassword(h.ctx(t, m, "198.51.100.1"), t, goodPassword, goodPassword))
}

func TestChangePasswordPolicy(t *testing.T) {
	for _, tc := range []struct{ name, password, reason string }{
		{"empty", "", "min"},
		{"nine ASCII characters", "abcdefghi", "min"},
		// 18 bytes but 9 code points: length is counted in runes.
		{"nine runes in eighteen bytes", strings.Repeat("é", 9), "min"},
		{"in the list", "password1234", "breached"},
		{"in the list, other case", "PassWord1234", "breached"},
		{"from Balances' list", "changeme123", "breached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			m := h.seedMemberAlone(t, "a@example.com")
			callers := h.signIn(t, m)
			credential := h.credentialRow(t, m.id)

			resp := h.changePassword(h.ctxWithToken(t, m, "198.51.100.1", callers), t, goodPassword, tc.password)
			got, is := resp.(api.ChangeLocalPassword400JSONResponse)
			if !is || got.Code != api.WEAKPASSWORD || got.Args == nil || (*got.Args)["reason"] != tc.reason {
				t.Fatalf("response = %#v, want 400 WEAK_PASSWORD reason %s", resp, tc.reason)
			}
			if h.credentialRow(t, m.id) != credential || !h.live(t, callers) {
				t.Error("a refused password changed the credential or a session")
			}
		})
	}

	t.Run("ten non-ASCII runes are accepted", func(t *testing.T) {
		h := newHarness(t, time.Now)
		m := h.seedMemberAlone(t, "a@example.com")
		changedPassword(t, h.changePassword(h.ctx(t, m, "198.51.100.1"), t, goodPassword, strings.Repeat("é", 10)))
	})
}

// A weak password costs nothing and records nothing: it is answered before the
// throttle is read and before any hashing, so it is answered even inside a
// backoff window and with every Argon2 permit held.
func TestChangePasswordAWeakPasswordIsAnsweredBeforeTheThrottleAndRecordsNothing(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")

	h.changePassword(ctx, t, "wrong current password", newPassword) // arms the window
	if got := h.failureCount(t, currentPasswordKey(m)); got != 1 {
		t.Fatalf("setup: failures = %d, want 1", got)
	}

	release := auth.HoldArgonPermitsForTest()
	defer release()
	done := make(chan api.ChangeLocalPasswordResponseObject, 1)
	go func() { done <- h.changePassword(ctx, t, goodPassword, "short") }()
	select {
	case resp := <-done:
		got, is := resp.(api.ChangeLocalPassword400JSONResponse)
		if !is || got.Code != api.WEAKPASSWORD {
			t.Fatalf("response = %#v, want 400 WEAK_PASSWORD, not the 429 the window would give", resp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a weak password waited: the policy must run before the throttle and the hash")
	}
	if got := h.failureCount(t, currentPasswordKey(m)); got != 1 {
		t.Errorf("failures = %d after a weak password, want it still 1", got)
	}
}

// A wrong current password is 403 and records one failure; the same wrong
// password again is 429 with Retry-After, and no hash runs.
func TestChangePasswordAWrongCurrentPasswordIsForbiddenAndBacksOff(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	callers := h.signIn(t, m)
	ctx := h.ctxWithToken(t, m, "198.51.100.1", callers)
	credential := h.credentialRow(t, m.id)

	resp := h.changePassword(ctx, t, "not the current password", newPassword)
	got, is := resp.(api.ChangeLocalPassword403JSONResponse)
	if !is || got.Code != api.INVALIDCURRENTPASSWORD || got.Args != nil {
		t.Fatalf("response = %#v, want 403 INVALID_CURRENT_PASSWORD with no args", resp)
	}
	if n := h.failureCount(t, currentPasswordKey(m)); n != 1 {
		t.Errorf("failures under current-password:<id> = %d, want 1", n)
	}
	if n := h.loginAttemptRows(t); n != 1 {
		t.Errorf("login_attempts rows = %d, want only the User's own key (no email: or ip: row)", n)
	}

	// Inside the window. With every permit held a hash would queue until the
	// context ends and answer 500, so a prompt 429 is proof that none ran.
	release := auth.HoldArgonPermitsForTest()
	defer release()
	timed, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp = h.changePassword(timed, t, "not the current password", newPassword)
	throttled, is := resp.(api.ChangeLocalPassword429JSONResponse)
	if !is || throttled.Body.Code != api.TOOMANYATTEMPTS {
		t.Fatalf("second attempt = %#v, want 429 TOO_MANY_ATTEMPTS", resp)
	}
	if throttled.Headers.RetryAfter == nil || *throttled.Headers.RetryAfter < 1 {
		t.Errorf("Retry-After = %v, want at least 1", throttled.Headers.RetryAfter)
	}
	if n := h.failureCount(t, currentPasswordKey(m)); n != 1 {
		t.Errorf("failures = %d, want the throttled attempt to record nothing", n)
	}
	if h.credentialRow(t, m.id) != credential || !h.live(t, callers) {
		t.Error("a refused change touched the credential or a session")
	}
}

// Even the RIGHT password is refused inside the window: it is backoff, so the
// answer is 429 until the window passes, and then the password works.
func TestChangePasswordBackoffAppliesToTheRightPasswordToo(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")

	h.changePassword(ctx, t, "wrong", newPassword)
	if _, is := h.changePassword(ctx, t, goodPassword, newPassword).(api.ChangeLocalPassword429JSONResponse); !is {
		t.Fatal("the right password inside the window was not 429")
	}
	h.expireBackoff(t)
	changedPassword(t, h.changePassword(ctx, t, goodPassword, newPassword))
}

// The key is the User's, and is shared by neither login's keys nor register's:
// a failed login does not throttle a change, and failed changes do not throttle
// login for the same address, from the same IP.
func TestChangePasswordItsBackoffAndLoginsAreIndependent(t *testing.T) {
	t.Run("a failed login does not throttle change-password", func(t *testing.T) {
		h := newHarness(t, time.Now)
		m := h.seedMemberAlone(t, "a@example.com")
		h.login(ipContext("198.51.100.1"), t, m.email, "wrong")
		if _, is := h.login(ipContext("198.51.100.1"), t, m.email, goodPassword).(api.LocalLogin429JSONResponse); !is {
			t.Fatal("setup: login is not throttled")
		}
		changedPassword(t, h.changePassword(h.ctx(t, m, "198.51.100.1"), t, goodPassword, newPassword))
	})

	t.Run("failed change-password attempts do not throttle login", func(t *testing.T) {
		h := newHarness(t, time.Now)
		m := h.seedMemberAlone(t, "a@example.com")
		ctx := h.ctx(t, m, "198.51.100.1")
		h.changePassword(ctx, t, "wrong", newPassword)
		h.changePassword(ctx, t, "wrong", newPassword)
		if _, is := h.login(ipContext("198.51.100.1"), t, m.email, goodPassword).(api.LocalLogin204Response); !is {
			t.Error("login for the same address and IP was refused after failed password changes")
		}
	})
}

// One key for both operations, so the two routes cannot double the guesses.
func TestChangeEmailFailuresCountTowardChangePasswordsKey(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")

	if _, is := h.changeEmail(ctx, t, "new@example.com", "wrong").(api.ChangeLocalEmail403JSONResponse); !is {
		t.Fatal("setup: a wrong password through change-email was not 403")
	}
	if _, is := h.changePassword(ctx, t, goodPassword, newPassword).(api.ChangeLocalPassword429JSONResponse); !is {
		t.Error("change-password was not throttled by a failure through change-email")
	}
	// And the other way round.
	h.expireBackoff(t)
	h.changePassword(ctx, t, "wrong", newPassword)
	if _, is := h.changeEmail(ctx, t, "new@example.com", goodPassword).(api.ChangeLocalEmail429JSONResponse); !is {
		t.Error("change-email was not throttled by a failure through change-password")
	}
	if n := h.failureCount(t, currentPasswordKey(m)); n != 2 {
		t.Errorf("failures = %d, want 2, one key for both operations", n)
	}
}

// A success clears the key, so an unlucky evening does not leave the User
// throttled.
func TestChangePasswordSuccessClearsTheKey(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")

	h.changePassword(ctx, t, "wrong", newPassword)
	h.expireBackoff(t)
	changedPassword(t, h.changePassword(ctx, t, goodPassword, newPassword))

	if n := h.loginAttemptRows(t); n != 0 {
		t.Errorf("login_attempts rows = %d after a success, want 0", n)
	}
}

// Backoff, never a lockout, and it grows on login's curve: a second failure
// after the first window waits longer.
func TestChangePasswordBackoffGrowsOnLoginsCurve(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")

	h.changePassword(ctx, t, "wrong", newPassword)
	h.expireBackoff(t)
	h.changePassword(ctx, t, "wrong", newPassword)

	var seconds float64
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT extract(epoch FROM (backoff_until - now())) FROM login_attempts WHERE key = $1`, currentPasswordKey(m)).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	// LOGIN_FIRST_BACKOFF is 1s in the harness and doubles: the second window is
	// about 2s, where a first failure's would be about 1s.
	if seconds < 1.2 || seconds > 2.0 {
		t.Errorf("window after the second failure = %.2fs, want about 2s", seconds)
	}
}

// A User with no credential row has no current password to give: answered as a
// wrong one, which counts as a failure, like login's dormant User.
func TestChangePasswordNoCredentialIsAWrongPassword(t *testing.T) {
	h := newHarness(t, time.Now)
	householdID := h.tdb.CreateHousehold(t, "Dormant")
	id := h.tdb.CreateUser(t, householdID, "dormant@example.com", "Dormant")
	m := member{householdID: householdID, id: id, email: "dormant@example.com"}
	callers := h.signIn(t, m)

	resp := h.changePassword(h.ctx(t, m, "198.51.100.1"), t, goodPassword, newPassword)
	if got, is := resp.(api.ChangeLocalPassword403JSONResponse); !is || got.Code != api.INVALIDCURRENTPASSWORD {
		t.Fatalf("response = %#v, want 403 INVALID_CURRENT_PASSWORD", resp)
	}
	if n := h.failureCount(t, currentPasswordKey(m)); n != 1 {
		t.Errorf("failures = %d, want 1", n)
	}
	if h.count(t, "credentials") != 0 || !h.live(t, callers) {
		t.Error("a refused change created a credential or revoked a session")
	}
}

// No User in the context is the contract's 401 (the router answers first; this
// is the defensive branch), and nothing is hashed or recorded.
func TestChangePasswordNoUserInContextIsUnauthorized(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedMemberAlone(t, "a@example.com")

	resp := h.changePassword(context.Background(), t, goodPassword, newPassword)
	if got, is := resp.(api.ChangeLocalPassword401JSONResponse); !is || got.Code != api.UNAUTHORIZED {
		t.Fatalf("response = %#v, want 401 UNAUTHORIZED", resp)
	}
	if h.loginAttemptRows(t) != 0 {
		t.Error("an unauthenticated change recorded a failure")
	}
}

// An outage reading the credential or the backoff says nothing about the
// password: 500, and no failure recorded for the User to be throttled by (#23).
func TestChangePasswordAnOutageBeforeTheVerifyIs500AndRecordsNothing(t *testing.T) {
	for name, broken := range map[string]func(q breakingQuerier) breakingQuerier{
		"credential read": func(q breakingQuerier) breakingQuerier { q.failGetCredential = true; return q },
		"backoff read":    func(q breakingQuerier) breakingQuerier { q.failGetBackoff = true; return q },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			m := h.seedMemberAlone(t, "a@example.com")
			callers := h.signIn(t, m)
			credential := h.credentialRow(t, m.id)
			h = h.withQuerier(broken(breakingQuerier{Querier: h.tdb.Queries}))

			resp := h.changePassword(h.ctx(t, m, "198.51.100.1"), t, goodPassword, newPassword)
			got, is := resp.(api.ChangeLocalPassword500JSONResponse)
			if !is || got.Code != api.INTERNAL {
				t.Fatalf("response = %#v, want 500 INTERNAL", resp)
			}
			if n := h.loginAttemptRows(t); n != 0 {
				t.Errorf("login_attempts rows = %d, want 0: an outage must not leave a backoff window behind", n)
			}
			if h.credentialRow(t, m.id) != credential || !h.live(t, callers) {
				t.Error("a failed change touched the credential or a session")
			}
		})
	}
}

// A failure inside the transaction rolls all of it back: 500, with the
// credential and every session exactly as they were. Each statement of the
// transaction is failed in turn, the later ones with the earlier ones already
// run.
func TestChangePasswordAFailureInsideTheTransactionChangesNothing(t *testing.T) {
	for name, failOn := range map[string]string{
		"credential swap":    "UPDATE credentials",
		"session revocation": "DELETE FROM sessions",
		"new session insert": "INSERT INTO sessions",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			m := h.seedMemberAlone(t, "a@example.com")
			callers := h.signIn(t, m)
			otherDevice := h.signIn(t, m)
			credential, sessions := h.credentialRow(t, m.id), h.sessionRows(t, m.id)
			h = h.withBeginner(failingTxBeginner{Beginner: h.tdb.Pool, failOn: failOn})

			resp := h.changePassword(h.ctxWithToken(t, m, "198.51.100.1", callers), t, goodPassword, newPassword)
			got, is := resp.(api.ChangeLocalPassword500JSONResponse)
			if !is || got.Code != api.INTERNAL {
				t.Fatalf("response = %#v, want 500 INTERNAL", resp)
			}
			if h.credentialRow(t, m.id) != credential {
				t.Error("the credential changed")
			}
			if h.sessionRows(t, m.id) != sessions || !h.live(t, callers) || !h.live(t, otherDevice) {
				t.Error("a session was revoked or added")
			}
		})
	}
}

// The context ending while queued for an Argon2 permit is 500 and records
// nothing: the password was never checked (#33).
func TestChangePasswordAnswers500AndRecordsNothingWhenNoArgonPermitComesFree(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	base, cancel := context.WithCancel(h.ctx(t, m, "198.51.100.1"))
	defer cancel()
	ctx := passedDeadline{base}

	release := auth.HoldArgonPermitsForTest()
	defer release()
	done := make(chan api.ChangeLocalPasswordResponseObject, 1)
	go func() { done <- h.changePassword(ctx, t, goodPassword, newPassword) }()
	waitForArgonQueue(t, 1)
	cancel()

	resp := <-done
	if _, is := resp.(api.ChangeLocalPassword500JSONResponse); !is {
		t.Fatalf("response = %#v, want 500", resp)
	}
	if n := h.loginAttemptRows(t); n != 0 {
		t.Errorf("login_attempts rows = %d, want 0", n)
	}
}

// Two concurrent changes carrying the same current password: exactly one wins,
// the other is 403, and the winner's session is live. The credential is
// compare-and-swapped, so the loser's transaction matches no row and rolls
// back before it can revoke the winner's session. A plain upsert lets both
// through and the second revokes the first's new session.
func TestChangePasswordConcurrentSubmissionsExactlyOneWins(t *testing.T) {
	const rounds = 5
	for round := range rounds {
		h := newHarness(t, time.Now)
		m := h.seedMemberAlone(t, "a@example.com")
		ctx := h.ctx(t, m, "198.51.100.1")
		passwords := []string{"first new passphrase " + string(rune('a'+round)), "second new passphrase " + string(rune('a'+round))}

		start := make(chan struct{})
		results := make([]api.ChangeLocalPasswordResponseObject, 2)
		var wg sync.WaitGroup
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				resp, err := h.auth.ChangeLocalPassword(ctx, api.ChangeLocalPasswordRequestObject{
					Body: &api.ChangeLocalPasswordJSONRequestBody{CurrentPassword: goodPassword, NewPassword: passwords[i]},
				})
				if err != nil {
					t.Errorf("ChangeLocalPassword: %v", err)
				}
				results[i] = resp
			}()
		}
		close(start)
		wg.Wait()

		wins, winner := 0, -1
		for i, r := range results {
			switch r.(type) {
			case api.ChangeLocalPassword204Response:
				wins++
				winner = i
			case api.ChangeLocalPassword403JSONResponse:
			default:
				t.Fatalf("round %d: result %d = %#v, want 204 or 403", round, i, r)
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: %d of 2 concurrent changes succeeded, want exactly 1", round, wins)
		}
		cookie := changedPassword(t, results[winner])
		if !h.live(t, cookie.Value) {
			t.Fatalf("round %d: the winner's new session is dead: the loser revoked it", round)
		}
		if _, is := h.login(ipContext("198.51.100.9"), t, m.email, passwords[winner]).(api.LocalLogin204Response); !is {
			t.Fatalf("round %d: the winner's password does not sign in", round)
		}
	}
}

// The throttle is read again under the permit, and a failure is recorded before
// the permit passes on: two wrong guesses queued together are not both checked.
// Both pass the first read, before either is recorded, and queue; the permit is
// then handed to them one at a time. The first is checked and recorded, and the
// second reads the throttle under the permit and is answered 429 unchecked.
func TestChangePasswordAFailureThrottlesTheCallerQueuedBehindIt(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")

	auth.TakeArgonPermitsForTest(int(auth.ArgonConcurrencyCapForTest()))
	held := int(auth.ArgonConcurrencyCapForTest())
	defer func() { auth.GiveBackArgonPermitsForTest(held) }()

	answers := make(chan api.ChangeLocalPasswordResponseObject, 2)
	for range 2 {
		go func() { answers <- h.changePassword(ctx, t, "wrong", newPassword) }()
	}
	waitForArgonQueue(t, 2)
	auth.GiveBackArgonPermitsForTest(1)
	held--

	var forbidden, throttled int
	for range 2 {
		switch (<-answers).(type) {
		case api.ChangeLocalPassword403JSONResponse:
			forbidden++
		case api.ChangeLocalPassword429JSONResponse:
			throttled++
		default:
			t.Fatal("want a 403 and a 429")
		}
	}
	if forbidden != 1 || throttled != 1 {
		t.Errorf("answers = %d x 403, %d x 429, want one of each: the queued caller was checked, not throttled", forbidden, throttled)
	}
	if n := h.failureCount(t, currentPasswordKey(m)); n != 1 {
		t.Errorf("failures = %d, want 1", n)
	}
}
