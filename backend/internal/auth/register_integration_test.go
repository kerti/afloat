package auth_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
)

const registerPassword = "kucing oranye di atap"

type registration struct {
	email, password string
	userDisplayName *string
	locale          *string
}

func (h harness) register(ctx context.Context, t *testing.T, r registration) api.LocalRegisterResponseObject {
	t.Helper()
	resp, err := h.auth.LocalRegister(ctx, api.LocalRegisterRequestObject{
		Body: &api.LocalRegisterJSONRequestBody{
			Email:           r.email,
			Password:        r.password,
			UserDisplayName: r.userDisplayName,
			Locale:          r.locale,
		},
	})
	if err != nil {
		t.Fatalf("LocalRegister returned an error: %v", err)
	}
	return resp
}

func (h harness) registerOK(ctx context.Context, t *testing.T, email string) *http.Cookie {
	t.Helper()
	return handshakeCookie(t, h.register(ctx, t, registration{email: email, password: registerPassword}))
}

// handshakeCookie is the cookie a 204 carries, parsed as a browser would.
func handshakeCookie(t *testing.T, resp api.LocalRegisterResponseObject) *http.Cookie {
	t.Helper()
	ok, is := resp.(api.LocalRegister204Response)
	if !is {
		t.Fatalf("response = %T (%+v), want 204", resp, resp)
	}
	if ok.Headers.SetCookie == nil {
		t.Fatal("register returned no Set-Cookie")
	}
	c, err := http.ParseSetCookie(*ok.Headers.SetCookie)
	if err != nil {
		t.Fatalf("parse Set-Cookie %q: %v", *ok.Headers.SetCookie, err)
	}
	return c
}

func (h harness) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func ptr[T any](v T) *T { return &v }

func assertBadRequest(t *testing.T, resp api.LocalRegisterResponseObject, code api.ErrorCode, args map[string]any) {
	t.Helper()
	got, is := resp.(api.LocalRegister400JSONResponse)
	if !is {
		t.Fatalf("response = %T (%+v), want 400", resp, resp)
	}
	if got.Code != code {
		t.Errorf("code = %v, want %v", got.Code, code)
	}
	if args == nil {
		if got.Args != nil {
			t.Errorf("args = %v, want none", *got.Args)
		}
		return
	}
	if got.Args == nil || len(*got.Args) != len(args) {
		t.Fatalf("args = %v, want %v", got.Args, args)
	}
	for k, v := range args {
		if (*got.Args)[k] != v {
			t.Errorf("args[%s] = %v, want %v", k, (*got.Args)[k], v)
		}
	}
}

// The handshake and its cookie are all registration leaves behind: no User, no
// Household, no credential and no session exist until the person commits at the
// gate.
func TestRegisterOpensAHandshakeAndCreatesNoAccount(t *testing.T) {
	h := newHarness(t, time.Now)

	cookie := h.registerOK(ipContext("198.51.100.30"), t, "  New@Example.COM ")

	for _, table := range []string{"households", "users", "credentials", "sessions"} {
		if n := h.count(t, table); n != 0 {
			t.Errorf("%s rows after register = %d, want 0", table, n)
		}
	}

	// The cookie, as a browser will hold it.
	if cookie.Name != "afloat_onboarding" {
		t.Errorf("cookie name = %q, want afloat_onboarding", cookie.Name)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Errorf("cookie attributes = HttpOnly %v Secure %v SameSite %v Path %q Domain %q, want HttpOnly, Secure, Lax, /, host-only",
			cookie.HttpOnly, cookie.Secure, cookie.SameSite, cookie.Path, cookie.Domain)
	}
	if cookie.MaxAge != 15*60 {
		t.Errorf("Max-Age = %d, want 900 (the handshake's 15 minutes)", cookie.MaxAge)
	}

	var (
		id, email, userDisplayName, locale string
		hash                               *string
		ttl                                time.Duration
	)
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT id, email, user_display_name, locale, password_hash, expires_at - created_at FROM onboarding_handshakes`).
		Scan(&id, &email, &userDisplayName, &locale, &hash, &ttl); err != nil {
		t.Fatalf("read handshake: %v", err)
	}
	// Hashed at rest, like a session: the row holds a password hash, and a
	// leaked id would otherwise be a presentable cookie.
	if id != auth.HashToken(cookie.Value) || id == cookie.Value {
		t.Errorf("handshake id = %q; want the SHA-256 of the cookie value, never the value itself", id)
	}
	if email != "new@example.com" || userDisplayName != "new" || locale != "en-GB" {
		t.Errorf("handshake = email %q user_display_name %q locale %q, want new@example.com / new / en-GB", email, userDisplayName, locale)
	}
	if ttl != 15*time.Minute {
		t.Errorf("handshake TTL = %v, want 15m", ttl)
	}
	if hash == nil || !strings.HasPrefix(*hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("password_hash = %v, want an Argon2id PHC string", hash)
	}
	if !auth.VerifyForTest(registerPassword, *hash) {
		t.Error("the stored hash does not verify against the registered password")
	}
	if strings.Contains(*hash, registerPassword) {
		t.Error("the plaintext password reached the handshake row")
	}
}

func TestRegisterPasswordPolicy(t *testing.T) {
	h := newHarness(t, time.Now)

	for i, tc := range []struct {
		name, password, reason string
	}{
		{"empty", "", "min"},
		{"nine ASCII characters", "abcdefghi", "min"},
		// 18 bytes but 9 code points: length is counted in runes.
		{"nine runes in eighteen bytes", strings.Repeat("é", 9), "min"},
		{"in the list", "password1234", "breached"},
		{"in the list, other case", "PassWord1234", "breached"},
		// Added from Balances' list (common_passwords.txt's last section).
		{"from Balances' list", "changeme123", "breached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ip := ipContext("198.51.100." + string(rune('1'+i)) + "0")
			resp := h.register(ip, t, registration{email: "p@example.com", password: tc.password})
			assertBadRequest(t, resp, api.WEAKPASSWORD, map[string]any{"reason": tc.reason})
		})
	}
	if n := h.count(t, "onboarding_handshakes"); n != 0 {
		t.Errorf("handshakes after refused passwords = %d, want 0", n)
	}
	if n := h.count(t, "login_attempts"); n != 0 {
		t.Errorf("a refused password armed the backoff: %d login_attempts rows, want 0", n)
	}

	// The floor is ten, counted in runes, so ten two-byte runes pass.
	resp := h.register(ipContext("198.51.100.90"), t, registration{email: "p@example.com", password: strings.Repeat("é", 10)})
	handshakeCookie(t, resp)
}

// The policy runs before any hashing, so a weak password costs nothing: with
// every Argon2 permit held, a hash would queue until the context ends, and
// this answers at once.
func TestRegisterChecksThePolicyBeforeHashing(t *testing.T) {
	h := newHarness(t, time.Now)
	release := auth.HoldArgonPermitsForTest()
	defer release()

	done := make(chan api.LocalRegisterResponseObject, 1)
	go func() {
		done <- h.register(ipContext("198.51.100.31"), t, registration{email: "p@example.com", password: "short"})
	}()
	select {
	case resp := <-done:
		assertBadRequest(t, resp, api.WEAKPASSWORD, map[string]any{"reason": "min"})
	case <-time.After(5 * time.Second):
		t.Fatal("a weak password waited for an Argon2 permit; the policy must run before the hash")
	}
}

func TestRegisterDisplayNameAndLocale(t *testing.T) {
	h := newHarness(t, time.Now)

	for i, tc := range []struct {
		name            string
		userDisplayName *string
		locale          *string
		email           string
		wantName        string
		wantLocale      string
		wantRule        string // non-empty: refused as VALIDATION on user_display_name with this rule
	}{
		{name: "defaults to the local part", email: "dewi.putri+tag@example.com", wantName: "dewi.putri+tag", wantLocale: "en-GB"},
		{name: "blank defaults to the local part", userDisplayName: ptr("   \t "), email: "bayu@example.com", wantName: "bayu", wantLocale: "en-GB"},
		{name: "supplied and trimmed", userDisplayName: ptr("  Dewi Putri "), email: "d@example.com", wantName: "Dewi Putri", wantLocale: "en-GB"},
		{name: "id-ID is kept", locale: ptr("id-ID"), email: "e@example.com", wantName: "e", wantLocale: "id-ID"},
		{name: "en-GB is kept", locale: ptr("en-GB"), email: "f@example.com", wantName: "f", wantLocale: "en-GB"},
		{name: "unsupported locale falls back", locale: ptr("fr-FR"), email: "g@example.com", wantName: "g", wantLocale: "en-GB"},
		{name: "locale is not case-folded", locale: ptr("id-id"), email: "i@example.com", wantName: "i", wantLocale: "en-GB"},
		{name: "empty locale falls back", locale: ptr(""), email: "j@example.com", wantName: "j", wantLocale: "en-GB"},
		// Me.user_display_name is capped at 255; a defaulted name must not outgrow it.
		{name: "a long local part is cut at 255 code points", email: strings.Repeat("a", 300) + "@example.com",
			wantName: strings.Repeat("a", 255), wantLocale: "en-GB"},
		// Postgres TEXT holds no NUL: refused here rather than a 500 at the insert.
		{name: "NUL in a display name", userDisplayName: ptr("Dewi\x00"), email: "k@example.com", wantRule: "pattern"},
		// Only invisible characters (category Cf: zero-width space and joiners, word
		// joiner, BOM) is a name nobody can see: blank, not a default.
		{name: "zero-width characters only", userDisplayName: ptr("\u200b\u200c\u200d"), email: "l@example.com", wantRule: "required"},
		{name: "word joiner and BOM among spaces", userDisplayName: ptr(" \u2060 \ufeff "), email: "m@example.com", wantRule: "required"},
		// Not stripped from a visible name, and a bidi control inside one stays (Balances parity).
		{name: "a visible name keeps its zero-width character", userDisplayName: ptr("Dewi\u200b"), email: "n@example.com", wantName: "Dewi\u200b", wantLocale: "en-GB"},
		{name: "a bidi control inside a visible name is accepted", userDisplayName: ptr("Dewi \u202e Putri"), email: "o@example.com", wantName: "Dewi \u202e Putri", wantLocale: "en-GB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ipContext("203.0.113." + string(rune('a'+i)))
			resp := h.register(ctx, t, registration{email: tc.email, password: registerPassword, userDisplayName: tc.userDisplayName, locale: tc.locale})
			if tc.wantRule != "" {
				assertBadRequest(t, resp, api.VALIDATION, map[string]any{"field": "user_display_name", "rule": tc.wantRule})
				return
			}
			handshakeCookie(t, resp)
			var name, locale string
			if err := h.tdb.Pool.QueryRow(context.Background(),
				`SELECT user_display_name, locale FROM onboarding_handshakes WHERE email = lower($1)`, tc.email).Scan(&name, &locale); err != nil {
				t.Fatalf("read handshake: %v", err)
			}
			if name != tc.wantName || locale != tc.wantLocale {
				t.Errorf("user_display_name %q locale %q, want %q / %q", name, locale, tc.wantName, tc.wantLocale)
			}
		})
	}
}

// A live User holding the address is 409, whatever case it was stored in and
// whether or not it has a credential. A soft-deleted User's address is free
// again, as the unique index says.
func TestRegisterAnAddressAlreadyHeldIsEmailTaken(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "held@example.com")
	dormantHousehold := h.tdb.CreateHousehold(t, "Dormant")
	h.tdb.CreateUser(t, dormantHousehold, "dormant@example.com", "Dormant")
	goneHousehold := h.tdb.CreateHousehold(t, "Gone")
	h.tdb.CreateUser(t, goneHousehold, "gone@example.com", "Gone")
	if _, err := h.tdb.Pool.Exec(context.Background(), `UPDATE users SET deleted_at = now() WHERE email = 'gone@example.com'`); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}

	for i, email := range []string{"held@example.com", "  HELD@Example.com ", "dormant@example.com"} {
		resp := h.register(ipContext("192.0.2."+string(rune('1'+i))), t, registration{email: email, password: registerPassword})
		got, is := resp.(api.LocalRegister409JSONResponse)
		if !is {
			t.Fatalf("%q: response = %T, want 409", email, resp)
		}
		if got.Code != api.EMAILTAKEN {
			t.Errorf("%q: code = %v, want EMAIL_TAKEN", email, got.Code)
		}
	}
	if n := h.count(t, "onboarding_handshakes"); n != 0 {
		t.Errorf("handshakes after refused registrations = %d, want 0", n)
	}
	// Nothing was hashed, so nothing was recorded against the address.
	if n := h.count(t, "login_attempts"); n != 0 {
		t.Errorf("login_attempts rows = %d, want 0", n)
	}

	handshakeCookie(t, h.register(ipContext("192.0.2.9"), t, registration{email: "gone@example.com", password: registerPassword}))
}

// Registration hashes on unauthenticated input, so it has the per-IP backoff
// login has: a request that reaches the hash arms a window for its address, and
// a request inside one is a 429 with Retry-After, answered without a hash.
func TestRegisterBacksOffPerIP(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := ipContext("198.51.100.40")

	h.registerOK(ctx, t, "one@example.com")

	resp := h.register(ctx, t, registration{email: "two@example.com", password: registerPassword})
	got, is := resp.(api.LocalRegister429JSONResponse)
	if !is {
		t.Fatalf("second registration from one address = %T, want 429", resp)
	}
	if got.Body.Code != api.TOOMANYATTEMPTS {
		t.Errorf("code = %v, want TOO_MANY_ATTEMPTS", got.Body.Code)
	}
	if got.Headers.RetryAfter == nil || *got.Headers.RetryAfter < 1 {
		t.Errorf("Retry-After = %v, want at least 1", got.Headers.RetryAfter)
	}
	if n := h.count(t, "onboarding_handshakes"); n != 1 {
		t.Errorf("handshakes = %d, want 1: the throttled request must not open one", n)
	}

	// Another address is unaffected: the key is the address.
	h.registerOK(ipContext("198.51.100.41"), t, "three@example.com")

	// Backoff, never a lockout: the window passes.
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE login_attempts SET backoff_until = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire backoff: %v", err)
	}
	h.registerOK(ctx, t, "four@example.com")
}

// Registration's key is its own: sharing "ip:" would let a run of
// registrations lock one address out of signing in, and a run of failed logins
// out of registering.
func TestRegisterAndLoginBackoffsAreIndependent(t *testing.T) {
	h := newHarness(t, time.Now)
	h.seedCredentialedUser(t, "a@example.com")
	ctx := ipContext("198.51.100.42")

	h.registerOK(ctx, t, "new@example.com")
	if _, is := h.login(ctx, t, "a@example.com", goodPassword).(api.LocalLogin204Response); !is {
		t.Error("a registration throttled a login from the same address")
	}

	ctx = ipContext("198.51.100.43")
	h.login(ctx, t, "a@example.com", "wrong")
	if _, is := h.login(ctx, t, "a@example.com", "wrong").(api.LocalLogin429JSONResponse); !is {
		t.Fatal("setup: the failed login did not arm the login backoff")
	}
	h.registerOK(ctx, t, "other@example.com")
}

// The backoff is read a second time under the permit. Every request in a burst
// from one address passes the first read before any has been recorded; without
// the second, the whole queue is hashed and the window never applies (#33's
// reasoning, for registration). At most the cap's worth may get through.
func TestRegisterBurstFromOneAddressIsHeldToThePermitCap(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := ipContext("198.51.100.44")

	const burst = 12
	answers := make(chan api.LocalRegisterResponseObject, burst)
	var wg sync.WaitGroup
	for i := range burst {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers <- h.register(ctx, t, registration{email: "burst" + string(rune('a'+i)) + "@example.com", password: registerPassword})
		}()
	}
	wg.Wait()
	close(answers)

	var created, throttled int
	for resp := range answers {
		switch resp.(type) {
		case api.LocalRegister204Response:
			created++
		case api.LocalRegister429JSONResponse:
			throttled++
		default:
			t.Errorf("response = %T, want 204 or 429", resp)
		}
	}
	if created < 1 || created > int(auth.ArgonConcurrencyCapForTest()) {
		t.Errorf("%d of %d registrations from one address were hashed, want between 1 and the cap (%d)",
			created, burst, auth.ArgonConcurrencyCapForTest())
	}
	if created+throttled != burst {
		t.Errorf("created %d + throttled %d != %d", created, throttled, burst)
	}
	if n := h.count(t, "onboarding_handshakes"); n != created {
		t.Errorf("handshakes = %d, want %d", n, created)
	}
}

// Registration's hash shares the process-wide cap (#33). Distinct addresses, so
// the backoff is no help: only the semaphore holds this to the cap.
func TestRegisterHashesAreHeldToTheArgonCap(t *testing.T) {
	h := newHarness(t, time.Now)
	auth.ResetArgonPeakInFlightForTest()

	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := ipContext("203.0.113." + string(rune('A'+i)))
			h.register(ctx, t, registration{email: "cap" + string(rune('a'+i)) + "@example.com", password: registerPassword})
		}()
	}
	wg.Wait()

	if got, want := auth.ArgonPeakInFlightForTest(), auth.ArgonConcurrencyCapForTest(); got > want {
		t.Errorf("peak concurrent hashes = %d, want at most the cap, %d", got, want)
	}
	if n := h.count(t, "onboarding_handshakes"); n != 12 {
		t.Errorf("handshakes = %d, want 12", n)
	}
}

// Queued past the handler's deadline for a permit, nothing was hashed: not a
// handshake, and not an attempt for the backoff to count.
func TestRegisterAnswers500AndRecordsNothingWhenNoArgonPermitComesFree(t *testing.T) {
	h := newHarness(t, time.Now)
	release := auth.HoldArgonPermitsForTest()
	defer release()

	logged := captureLog(t)
	ctx, cancel := context.WithCancel(ipContext("198.51.100.45"))
	defer cancel()

	answer := make(chan api.LocalRegisterResponseObject, 1)
	go func() {
		answer <- h.register(passedDeadline{ctx}, t, registration{email: "q@example.com", password: registerPassword})
	}()
	waitForArgonQueue(t, 1)
	cancel()
	resp := <-answer

	if _, is := resp.(api.LocalRegister500JSONResponse); !is {
		t.Fatalf("response = %T, want 500", resp)
	}
	if out := logged.String(); !strings.Contains(out, `level=ERROR msg="register: wait for argon2 permit"`) {
		t.Errorf("the 500 did not come from the permit wait; logged:\n%s", out)
	}
	if n := h.count(t, "onboarding_handshakes") + h.loginAttemptRows(t); n != 0 {
		t.Errorf("%d handshake and backoff rows after a registration that never hashed, want 0", n)
	}
}

// A registration holding its permit finishes: the deadline passes while it
// hashes, and its attempt is still recorded and its handshake still opened,
// or a flood could switch the backoff off exactly as it could login's (#33).
func TestRegisterHoldingItsPermitRecordsItsAttemptPastTheDeadline(t *testing.T) {
	h := newHarness(t, time.Now)
	release := auth.HoldArgonPermitsForTest()
	defer release()
	auth.ResetArgonPeakInFlightForTest()

	ctx, cancel := context.WithCancel(ipContext("198.51.100.46"))
	defer cancel()
	answer := make(chan api.LocalRegisterResponseObject, 1)
	go func() {
		answer <- h.register(passedDeadline{ctx}, t, registration{email: "late@example.com", password: registerPassword})
	}()
	waitForArgonQueue(t, 1)
	release()
	deadline := time.Now().Add(10 * time.Second)
	for auth.ArgonPeakInFlightForTest() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the registration never took its Argon2 permit")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	resp := <-answer

	handshakeCookie(t, resp)
	if rows := h.loginAttemptRows(t); rows != 1 {
		t.Errorf("%d backoff rows after a hashed registration, want 1", rows)
	}
}

// Expired handshakes are swept on the way into a registration, so the table is
// bounded by one TTL's registrations rather than growing with every abandoned
// one. A live handshake is not touched.
func TestRegisterSweepsExpiredHandshakes(t *testing.T) {
	h := newHarness(t, time.Now)
	h.registerOK(ipContext("198.51.100.47"), t, "old@example.com")
	h.registerOK(ipContext("198.51.100.48"), t, "live@example.com")
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE onboarding_handshakes SET expires_at = now() - interval '1 second' WHERE email = 'old@example.com'`); err != nil {
		t.Fatalf("expire: %v", err)
	}

	h.registerOK(ipContext("198.51.100.49"), t, "new@example.com")

	var emails string
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT string_agg(email, ',' ORDER BY email) FROM onboarding_handshakes`).Scan(&emails); err != nil {
		t.Fatalf("query: %v", err)
	}
	if emails != "live@example.com,new@example.com" {
		t.Errorf("handshakes after the sweep = %s, want live@example.com,new@example.com", emails)
	}
}
