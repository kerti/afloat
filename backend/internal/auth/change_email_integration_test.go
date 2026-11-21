package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
)

func changedEmail(t *testing.T, resp api.ChangeLocalEmailResponseObject) api.Me {
	t.Helper()
	me, is := resp.(api.ChangeLocalEmail200JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 200", resp)
	}
	return api.Me(me)
}

// Success updates the row and answers the Me, leaves every session exactly as it
// was, and sets no cookie.
func TestChangeEmailUpdatesTheRowAndLeavesEverySessionAlone(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "old@example.com")
	callers := h.signIn(t, m)
	otherDevice := h.signIn(t, m)
	sessions := h.sessionRows(t, m.id)

	me := changedEmail(t, h.changeEmail(h.ctxWithToken(t, m, "198.51.100.1", callers), t, "new@example.com", goodPassword))

	if me.Email != "new@example.com" || me.Id != [16]byte(m.id.Bytes) {
		t.Errorf("answer = %+v, want the same User with the new address", me)
	}
	var stored string
	if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT email FROM users WHERE id = $1`, m.id).Scan(&stored); err != nil || stored != "new@example.com" {
		t.Errorf("stored email = %q (%v)", stored, err)
	}
	if h.sessionRows(t, m.id) != sessions || !h.live(t, callers) || !h.live(t, otherDevice) {
		t.Error("an email change touched a session")
	}
	// The handler's answer type has no header to carry a cookie, and the
	// credential is untouched.
	if _, is := h.login(ipContext("198.51.100.2"), t, "new@example.com", goodPassword).(api.LocalLogin204Response); !is {
		t.Error("login by the new address does not work")
	}
	if _, is := h.login(ipContext("198.51.100.3"), t, "old@example.com", goodPassword).(api.LocalLogin401JSONResponse); !is {
		t.Error("login by the old address still works")
	}
}

// Trimmed and lower-cased before it is stored, as registration stores it, so
// Me.email may differ in case from what was typed.
func TestChangeEmailIsStoredTrimmedAndLowerCased(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "old@example.com")

	me := changedEmail(t, h.changeEmail(h.ctx(t, m, "198.51.100.1"), t, "  New.Name@Example.COM \t", goodPassword))
	if me.Email != "new.name@example.com" {
		t.Errorf("email = %q, want it trimmed and lower-cased", me.Email)
	}
}

// An address a live User holds is 409 EMAIL_TAKEN, in any case, and nothing
// changes.
func TestChangeEmailAnAddressAlreadyHeldIsEmailTaken(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedMemberAlone(t, "taken@example.com")
	m := h.seedMemberAlone(t, "old@example.com")
	user, credential := h.userRow(t, m.id), h.credentialRow(t, m.id)

	for _, email := range []string{"taken@example.com", "TAKEN@Example.com", " taken@example.com "} {
		resp := h.changeEmail(h.ctx(t, m, "198.51.100.1"), t, email, goodPassword)
		got, is := resp.(api.ChangeLocalEmail409JSONResponse)
		if !is || got.Code != api.EMAILTAKEN {
			t.Fatalf("%q: response = %#v, want 409 EMAIL_TAKEN", email, resp)
		}
		if h.userRow(t, m.id) != user || h.credentialRow(t, m.id) != credential {
			t.Errorf("%q: a refused change wrote", email)
		}
	}
}

// The caller's own address, in another case, is their own row: no conflict.
func TestChangeEmailTheCallersOwnAddressInAnotherCaseIsFine(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "me@example.com")

	me := changedEmail(t, h.changeEmail(h.ctx(t, m, "198.51.100.1"), t, "ME@Example.com", goodPassword))
	if me.Email != "me@example.com" {
		t.Errorf("email = %q", me.Email)
	}
}

// A soft-deleted User's address is free: the unique index is partial.
func TestChangeEmailASoftDeletedUsersAddressIsFree(t *testing.T) {
	h := newHarness(t, time.Now)
	gone := h.seedMemberAlone(t, "gone@example.com")
	if _, err := h.tdb.Pool.Exec(context.Background(), `UPDATE users SET deleted_at = now() WHERE id = $1`, gone.id); err != nil {
		t.Fatal(err)
	}
	m := h.seedMemberAlone(t, "old@example.com")

	me := changedEmail(t, h.changeEmail(h.ctx(t, m, "198.51.100.1"), t, "gone@example.com", goodPassword))
	if me.Email != "gone@example.com" {
		t.Errorf("email = %q", me.Email)
	}
}

// The address's state is revealed only to a caller who has proved the password:
// a wrong password with a taken address is 403, never 409.
func TestChangeEmailAWrongPasswordWithATakenAddressIsForbiddenNotConflict(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedMemberAlone(t, "taken@example.com")
	m := h.seedMemberAlone(t, "old@example.com")
	user := h.userRow(t, m.id)

	resp := h.changeEmail(h.ctx(t, m, "198.51.100.1"), t, "taken@example.com", "not the current password")
	if got, is := resp.(api.ChangeLocalEmail403JSONResponse); !is || got.Code != api.INVALIDCURRENTPASSWORD {
		t.Fatalf("response = %#v, want 403 INVALID_CURRENT_PASSWORD", resp)
	}
	if h.userRow(t, m.id) != user {
		t.Error("a refused change wrote")
	}
	if n := h.failureCount(t, currentPasswordKey(m)); n != 1 {
		t.Errorf("failures = %d, want 1", n)
	}
}

// Inside the window: 429 with Retry-After, and no hash runs.
func TestChangeEmailInsideTheBackoffWindowIsThrottledWithoutHashing(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "old@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")
	h.changeEmail(ctx, t, "new@example.com", "wrong")

	release := auth.HoldArgonPermitsForTest()
	defer release()
	timed, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp := h.changeEmail(timed, t, "new@example.com", goodPassword)
	got, is := resp.(api.ChangeLocalEmail429JSONResponse)
	if !is || got.Body.Code != api.TOOMANYATTEMPTS || got.Headers.RetryAfter == nil || *got.Headers.RetryAfter < 1 {
		t.Fatalf("response = %#v, want 429 with Retry-After", resp)
	}
}

// A success clears the key, and so does a proved password whose address then
// turns out to be taken: the password was right.
func TestChangeEmailAProvedPasswordClearsTheKey(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedMemberAlone(t, "taken@example.com")
	m := h.seedMemberAlone(t, "old@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")

	h.changeEmail(ctx, t, "new@example.com", "wrong")
	h.expireBackoff(t)
	changedEmail(t, h.changeEmail(ctx, t, "new@example.com", goodPassword))
	if n := h.failureCount(t, currentPasswordKey(m)); n != 0 {
		t.Errorf("failures = %d after a success, want the key cleared", n)
	}
}

func TestChangeEmailNoCredentialIsAWrongPassword(t *testing.T) {
	h := newHarness(t, time.Now)
	householdID := h.tdb.CreateHousehold(t, "Dormant")
	id := h.tdb.CreateUser(t, householdID, "dormant@example.com", "Dormant")
	m := member{householdID: householdID, id: id}

	resp := h.changeEmail(h.ctx(t, m, "198.51.100.1"), t, "new@example.com", goodPassword)
	if got, is := resp.(api.ChangeLocalEmail403JSONResponse); !is || got.Code != api.INVALIDCURRENTPASSWORD {
		t.Fatalf("response = %#v, want 403 INVALID_CURRENT_PASSWORD", resp)
	}
}

func TestChangeEmailNoUserInContextIsUnauthorized(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedMemberAlone(t, "a@example.com")
	resp := h.changeEmail(context.Background(), t, "new@example.com", goodPassword)
	if got, is := resp.(api.ChangeLocalEmail401JSONResponse); !is || got.Code != api.UNAUTHORIZED {
		t.Fatalf("response = %#v, want 401 UNAUTHORIZED", resp)
	}
}

// An outage reading the credential or the backoff is 500 and records no
// failure. The write and the Household read are the next test's.
func TestChangeEmailAnOutageBeforeTheWriteIs500AndRecordsNothing(t *testing.T) {
	for name, broken := range map[string]func(q breakingQuerier) breakingQuerier{
		"credential read": func(q breakingQuerier) breakingQuerier { q.failGetCredential = true; return q },
		"backoff read":    func(q breakingQuerier) breakingQuerier { q.failGetBackoff = true; return q },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			m := h.seedMemberAlone(t, "old@example.com")
			callers := h.signIn(t, m)
			sessions := h.sessionRows(t, m.id)
			h = h.withQuerier(broken(breakingQuerier{Querier: h.tdb.Queries}))

			resp := h.changeEmail(h.ctx(t, m, "198.51.100.1"), t, "new@example.com", goodPassword)
			got, is := resp.(api.ChangeLocalEmail500JSONResponse)
			if !is || got.Code != api.INTERNAL || got.Args != nil {
				t.Fatalf("response = %#v, want 500 INTERNAL", resp)
			}
			if n := h.loginAttemptRows(t); n != 0 {
				t.Errorf("login_attempts rows = %d, want 0", n)
			}
			if h.sessionRows(t, m.id) != sessions || !h.live(t, callers) {
				t.Error("a failed change touched a session")
			}
		})
	}
}

// A 500 changes nothing. The write and the Household read the answer needs are
// one transaction, so an outage at its begin, its UPDATE, the read or the
// commit leaves the users row byte for byte as it was, and no session touched.
// The read and the commit fail with the UPDATE already run: were the read
// outside the transaction, the address would have moved under a 500.
func TestChangeEmailAFailureInsideTheTransactionChangesNothing(t *testing.T) {
	for name, fail := range writeThenAnswerFailures {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			m := h.seedMemberAlone(t, "old@example.com")
			callers := h.signIn(t, m)
			user, credential, sessions := h.userRow(t, m.id), h.credentialRow(t, m.id), h.sessionRows(t, m.id)
			fail.Beginner = h.tdb.Pool
			h = h.withBeginner(fail)

			resp := h.changeEmail(h.ctx(t, m, "198.51.100.1"), t, "new@example.com", goodPassword)
			got, is := resp.(api.ChangeLocalEmail500JSONResponse)
			if !is || got.Code != api.INTERNAL || got.Args != nil {
				t.Fatalf("response = %#v, want 500 INTERNAL", resp)
			}
			if h.userRow(t, m.id) != user {
				t.Error("a 500 left the users row changed")
			}
			if h.credentialRow(t, m.id) != credential || h.sessionRows(t, m.id) != sessions || !h.live(t, callers) {
				t.Error("a failed change touched the credential or a session")
			}
			// The password was proved before the transaction, so its failed
			// guesses are forgotten whatever the write did.
			if n := h.loginAttemptRows(t); n != 0 {
				t.Errorf("login_attempts rows = %d, want 0", n)
			}
		})
	}
}
