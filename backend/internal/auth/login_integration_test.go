package auth_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
)

const goodPassword = "kucing oranye di atap"

func (h harness) seedCredentialedUser(t *testing.T, email string) (pgtype.UUID, pgtype.UUID) {
	t.Helper()
	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, email, "Test User")

	hash, err := auth.HashPassword(context.Background(), goodPassword)
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
			Email:    email,
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

// The backoff window is measured against the database's clock end to end: the
// remaining time the query returns, not a subtraction against the app's own
// clock. A fixed app clock running two hours ahead of the container must
// neither shorten Retry-After to something untruthful nor, worse, let the
// skew push the computed remainder negative and silently let the request
// through while the database still considers the window active (#25).
func TestBackoffRemainderIsTrueUnderAppClockSkew(t *testing.T) {
	skewed := func() time.Time { return time.Now().Add(2 * time.Hour) }
	h := newHarness(t, skewed)
	h.seedCredentialedUser(t, "a@example.com")
	ctx := ipContext("198.51.100.40")

	// Arm the backoff, then widen the window well past this test's own
	// runtime so the assertion below cannot race the real 1s default.
	h.login(ctx, t, "a@example.com", "wrong")
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE login_attempts SET backoff_until = now() + interval '2 minutes'`); err != nil {
		t.Fatalf("widen backoff: %v", err)
	}

	resp := h.login(ctx, t, "a@example.com", goodPassword)
	got, is := resp.(api.LocalLogin429JSONResponse)
	if !is {
		t.Fatalf("response under a +2h app clock = %T, want 429 (the DB still considers the window active)", resp)
	}
	if got.Headers.RetryAfter == nil {
		t.Fatal("Retry-After is nil")
	}
	// ~120s, computed by the database's own clock. A wide but bounded band:
	// tight enough to catch the app-clock-subtraction bug (which would report
	// a Retry-After hours off, or let the request through instead of 429ing),
	// loose enough not to flake on the DB round trip.
	if ra := *got.Headers.RetryAfter; ra < 90 || ra > 130 {
		t.Errorf("Retry-After = %d, want close to 120 (the DB-measured remainder, unaffected by the app clock's 2h skew)", ra)
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

// #33: N >> the cap concurrent logins, half a wrong password against a real
// account and half an unknown address, so the real hash and the dummy hash
// share the one cap. Every request answers 401, none 500, and the peak
// in flight is exactly the cap.
func TestLoginConcurrencyBoundsArgon2AndAnswersEveryRequest(t *testing.T) {
	h := newHarness(t, time.Now)
	const n = 20
	// One account per request: the first failure on a shared address sets its
	// backoff, and any request reading it after that would 429 instead.
	for i := 0; i < n; i += 2 {
		h.seedCredentialedUser(t, fmt.Sprintf("known-%d@example.com", i))
	}
	auth.ResetArgonPeakInFlightForTest()

	type result struct {
		resp api.LocalLoginResponseObject
		err  error
	}
	results := make([]result, n)

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// A fresh IP each, or the per-IP backoff would 429 the second
			// arrival and mask what this checks.
			ip := fmt.Sprintf("198.51.100.%d", 150+i)
			email := fmt.Sprintf("known-%d@example.com", i)
			if i%2 == 1 {
				email = fmt.Sprintf("nobody-%d@example.com", i)
			}
			resp, err := h.auth.LocalLogin(ipContext(ip), api.LocalLoginRequestObject{
				Body: &api.LocalLoginJSONRequestBody{
					Email:    email,
					Password: "wrong password entirely",
				},
			})
			results[i] = result{resp, err}
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Errorf("request %d: LocalLogin returned an error: %v", i, r.err)
			continue
		}
		if _, is := r.resp.(api.LocalLogin401JSONResponse); !is {
			t.Errorf("request %d = %T, want 401", i, r.resp)
		}
	}
	if got, want := auth.ArgonPeakInFlightForTest(), auth.ArgonConcurrencyCapForTest(); got != want {
		t.Errorf("peak concurrent Argon2 calls = %d, want %d", got, want)
	}
}

// waitForArgonQueue returns once n callers are queued for an Argon2 permit, so
// a test acts on the wait itself rather than on a guess at how long the reads
// before it take.
func waitForArgonQueue(t *testing.T, n int32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for auth.ArgonWaitingForTest() != n {
		if time.Now().After(deadline) {
			t.Fatalf("%d callers queued for an Argon2 permit, want %d", auth.ArgonWaitingForTest(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// loginInBackground starts a login and returns where its answer will arrive.
func (h harness) loginInBackground(ctx context.Context, email, password string) <-chan api.LocalLoginResponseObject {
	answer := make(chan api.LocalLoginResponseObject, 1)
	go func() {
		resp, _ := h.auth.LocalLogin(ctx, api.LocalLoginRequestObject{
			Body: &api.LocalLoginJSONRequestBody{
				Email:    email,
				Password: password,
			},
		})
		answer <- resp
	}()
	return answer
}

// passedDeadline is a context whose cancel reads as a passed deadline. The
// test picks the moment, so the deadline lands in the permit wait and not in
// a database read before it.
type passedDeadline struct{ context.Context }

func (c passedDeadline) Err() error {
	if c.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logged bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })
	return &logged
}

func (h harness) loginAttemptRows(t *testing.T) int {
	t.Helper()
	var rows int
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM login_attempts`).Scan(&rows); err != nil {
		t.Fatalf("query: %v", err)
	}
	return rows
}

// Queued past the handler's deadline for a permit, the password was never
// checked: not a 401, and not a failure for the backoff to count.
func TestLoginAnswers500AndRecordsNoFailureWhenNoArgonPermitComesFree(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")

	release := auth.HoldArgonPermitsForTest()
	defer release()

	// The deadline would bound the backoff read too, and a read that overran
	// it would also answer 500. The log says which wait ran out.
	logged := captureLog(t)

	ctx, cancel := context.WithCancel(ipContext("198.51.100.201"))
	defer cancel()
	answer := h.loginInBackground(passedDeadline{ctx}, "a@example.com", "wrong password entirely")
	waitForArgonQueue(t, 1)
	cancel()
	resp := <-answer

	if _, is := resp.(api.LocalLogin500JSONResponse); !is {
		t.Fatalf("response = %T, want 500", resp)
	}
	if out := logged.String(); !strings.Contains(out, `level=ERROR msg="login: wait for argon2 permit"`) {
		t.Errorf("the 500 did not come from the permit wait; logged:\n%s", out)
	}
	if rows := h.loginAttemptRows(t); rows != 0 {
		t.Errorf("%d login_attempts rows after a login that never checked its password, want 0", rows)
	}
}

// A client that leaves while queued is no fault of the server's: still a 500
// and no failure recorded, but logged at Warn, so a stream of dropped
// connections does not read as a stream of errors.
func TestLoginLogsAClientLeavingWhileQueuedAsAWarning(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")

	release := auth.HoldArgonPermitsForTest()
	defer release()

	logged := captureLog(t)

	// Cancelled, not timed out: what net/http does when the client goes away.
	ctx, cancel := context.WithCancel(ipContext("198.51.100.202"))
	defer cancel()
	answer := h.loginInBackground(ctx, "a@example.com", "wrong password entirely")
	waitForArgonQueue(t, 1)
	cancel()
	resp := <-answer

	if _, is := resp.(api.LocalLogin500JSONResponse); !is {
		t.Fatalf("response = %T, want 500", resp)
	}
	if out := logged.String(); !strings.Contains(out, `level=WARN msg="login: client left while waiting for an argon2 permit"`) ||
		strings.Contains(out, "level=ERROR") {
		t.Errorf("want one Warn for the client leaving and no Error; logged:\n%s", out)
	}
	if rows := h.loginAttemptRows(t); rows != 0 {
		t.Errorf("%d login_attempts rows after a login that never checked its password, want 0", rows)
	}
}

// Behind a flood, the login that gets a permit is the one about to run out of
// time. Its hash crosses the deadline, and on the request's ctx the failure
// write then failed: a checked guess answered 401 with nothing recorded, so
// the backoff never grew. A login holding its permit must finish.
func TestLoginHoldingItsPermitRecordsItsFailurePastTheDeadline(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")

	release := auth.HoldArgonPermitsForTest()
	defer release()
	auth.ResetArgonPeakInFlightForTest()

	ctx, cancel := context.WithCancel(ipContext("198.51.100.204"))
	defer cancel()
	answer := h.loginInBackground(passedDeadline{ctx}, "a@example.com", "wrong password entirely")
	waitForArgonQueue(t, 1)
	release()
	// HoldArgonPermitsForTest counts nothing in flight, so the peak reaching 1
	// is the login taking its permit. The deadline passes there, ahead of the
	// second backoff read, the hash and the failure write.
	deadline := time.Now().Add(10 * time.Second)
	for auth.ArgonPeakInFlightForTest() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the login never took its Argon2 permit")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	resp := <-answer

	if _, is := resp.(api.LocalLogin401JSONResponse); !is {
		t.Fatalf("response = %T, want 401", resp)
	}
	// One row per key: email: and ip:.
	if rows := h.loginAttemptRows(t); rows != 2 {
		t.Errorf("%d login_attempts rows after a checked wrong password, want 2", rows)
	}
}

// reportedDeadline reports a deadline but ends only when its parent does, so
// the deadline can pass while a login is still queued and the login still
// take its permit after it.
type reportedDeadline struct {
	context.Context
	deadline time.Time
}

func (c reportedDeadline) Deadline() (time.Time, bool) { return c.deadline, true }

// middleware.Timeout gives every real login a deadline, which none of the
// tests above carry. Holding its permit, a login gets the handler's budget
// again, counted from the permit. Here the request's deadline has passed by
// then: bounded by it, or by no budget at all, the second backoff read fails
// and the login answers 500.
func TestLoginHoldingItsPermitGetsTheHandlerBudgetAgain(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")

	release := auth.HoldArgonPermitsForTest()
	defer release()

	// Ample for the read, the hash and two writes under -race on a loaded runner.
	deadline := time.Now().Add(time.Second)
	answer := h.loginInBackground(reportedDeadline{ipContext("198.51.100.205"), deadline}, "a@example.com", "wrong password entirely")
	waitForArgonQueue(t, 1)
	time.Sleep(time.Until(deadline) + 10*time.Millisecond)
	release()
	resp := <-answer

	if _, is := resp.(api.LocalLogin401JSONResponse); !is {
		t.Fatalf("response = %T, want 401", resp)
	}
	if rows := h.loginAttemptRows(t); rows != 2 {
		t.Errorf("%d login_attempts rows after a checked wrong password, want 2", rows)
	}
}

// A burst on one account passes the first backoff read before any of it has
// failed, and queues. Read only there, the backoff never applied: every
// queued guess was checked. Read again under the permit, the first failures
// throttle the rest, so at most the cap's worth are checked.
func TestLoginBurstOnOneAccountIsThrottledUnderThePermit(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")

	release := auth.HoldArgonPermitsForTest()
	defer release()

	const n = 20
	answers := make([]<-chan api.LocalLoginResponseObject, n)
	for i := range n {
		answers[i] = h.loginInBackground(ipContext("198.51.100.203"), "a@example.com", "wrong password entirely")
	}
	// Every one past the first read, so none of them saw a failure there.
	waitForArgonQueue(t, n)
	release()

	var checked, throttled int
	for i, answer := range answers {
		switch resp := (<-answer).(type) {
		case api.LocalLogin401JSONResponse:
			checked++
		case api.LocalLogin429JSONResponse:
			throttled++
		default:
			t.Errorf("request %d = %T, want 401 or 429", i, resp)
		}
	}
	if limit := int(auth.ArgonConcurrencyCapForTest()); checked < 1 || checked > limit {
		t.Errorf("%d of %d queued guesses were checked, want 1 to %d; %d throttled", checked, n, limit, throttled)
	}
}
