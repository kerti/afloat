package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
)

// atTheGate registers an address and returns the context of a request carrying
// the handshake cookie it was given, plus the cookie's value.
func (h harness) atTheGate(t *testing.T, ip, email string, opts ...func(*registration)) (context.Context, string) {
	t.Helper()
	r := registration{email: email, password: registerPassword}
	for _, o := range opts {
		o(&r)
	}
	cookie := handshakeCookie(t, h.register(ipContext(ip), t, r))
	return auth.WithOnboardingTokenForTest(ipContext(ip), cookie.Value), cookie.Value
}

func (h harness) options(ctx context.Context, t *testing.T) api.GetOnboardingOptionsResponseObject {
	t.Helper()
	resp, err := h.auth.GetOnboardingOptions(ctx, api.GetOnboardingOptionsRequestObject{})
	if err != nil {
		t.Fatalf("GetOnboardingOptions returned an error: %v", err)
	}
	return resp
}

// commit is a found choice with a valid first Pocket name, "Cash".
func (h harness) commit(ctx context.Context, t *testing.T, found bool, name string) api.CommitOnboardingChoiceResponseObject {
	t.Helper()
	return h.commitPocket(ctx, t, found, name, "Cash")
}

func (h harness) commitPocket(ctx context.Context, t *testing.T, found bool, name, pocketName string) api.CommitOnboardingChoiceResponseObject {
	t.Helper()
	resp, err := h.auth.CommitOnboardingChoice(ctx, api.CommitOnboardingChoiceRequestObject{
		Body: &api.CommitOnboardingChoiceJSONRequestBody{Found: found, HouseholdDisplayName: name, PocketDisplayName: pocketName},
	})
	if err != nil {
		t.Fatalf("CommitOnboardingChoice returned an error: %v", err)
	}
	return resp
}

func (h harness) pocketCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM pockets`).Scan(&n); err != nil {
		t.Fatalf("count pockets: %v", err)
	}
	return n
}

// visit writes a response the way the generated strict handler does and
// returns what the client would receive.
func visit(t *testing.T, resp api.CommitOnboardingChoiceResponseObject) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitCommitOnboardingChoiceResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	return rec
}

func setCookies(rec *httptest.ResponseRecorder) map[string]*http.Cookie {
	out := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		out[c.Name] = c
	}
	return out
}

func assertCommitCode(t *testing.T, resp api.CommitOnboardingChoiceResponseObject, status int, code api.ErrorCode) *httptest.ResponseRecorder {
	t.Helper()
	rec := visit(t, resp)
	if rec.Code != status {
		t.Fatalf("status = %d (%s), want %d %s", rec.Code, rec.Body.String(), status, code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"`+string(code)+`"`) {
		t.Errorf("body = %s, want code %s", rec.Body.String(), code)
	}
	return rec
}

func (h harness) foundingTables(t *testing.T) string {
	t.Helper()
	var s string
	if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM households) || '/' || (SELECT count(*) FROM users) || '/' ||
		(SELECT count(*) FROM credentials) || '/' || (SELECT count(*) FROM sessions) || '/' ||
		(SELECT count(*) FROM onboarding_handshakes)`).Scan(&s); err != nil {
		t.Fatalf("count: %v", err)
	}
	return s // households/users/credentials/sessions/handshakes
}

func TestOnboardingOptionsOfALiveHandshake(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.50", "Dewi@Example.com", func(r *registration) {
		r.userDisplayName = ptr("Dewi Putri")
	})

	got, is := h.options(ctx, t).(api.GetOnboardingOptions200JSONResponse)
	if !is {
		t.Fatalf("response = %T, want 200", h.options(ctx, t))
	}
	if got.Email != "dewi@example.com" || got.UserDisplayName != "Dewi Putri" || !got.FoundingOpen {
		t.Errorf("options = %+v, want dewi@example.com / Dewi Putri / founding_open", got)
	}
}

func TestOnboardingOptionsReportFoundingClosed(t *testing.T) {
	h := newHarness(t, time.Now).withFoundingDisabled()
	ctx, _ := h.atTheGate(t, "198.51.100.51", "a@example.com")

	got, is := h.options(ctx, t).(api.GetOnboardingOptions200JSONResponse)
	if !is {
		t.Fatalf("response = %T, want 200", h.options(ctx, t))
	}
	if got.FoundingOpen {
		t.Error("founding_open = true with FOUNDING_DISABLED set")
	}
}

// No handshake, an unknown one, and an expired one are one answer: 401. A cookie
// that was sent is cleared, so the client stops presenting it; none was sent,
// none is set.
func TestOnboardingOptionsWithoutALiveHandshakeIs401(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, token := h.atTheGate(t, "198.51.100.52", "a@example.com")
	if _, err := h.tdb.Pool.Exec(context.Background(), `UPDATE onboarding_handshakes SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire: %v", err)
	}
	_ = token

	for _, tc := range []struct {
		name      string
		ctx       context.Context
		wantClear bool
	}{
		{"no cookie", ipContext("198.51.100.53"), false},
		{"unknown token", auth.WithOnboardingTokenForTest(ipContext("198.51.100.54"), "never-issued"), true},
		{"expired handshake", ctx, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.options(tc.ctx, t)
			got, is := resp.(api.GetOnboardingOptions401JSONResponse)
			if !is {
				t.Fatalf("response = %T, want 401", resp)
			}
			if got.Body.Code != api.UNAUTHORIZED {
				t.Errorf("code = %v, want UNAUTHORIZED", got.Body.Code)
			}
			if !tc.wantClear {
				if got.Headers.SetCookie != nil {
					t.Errorf("Set-Cookie = %q with no cookie presented, want none", *got.Headers.SetCookie)
				}
				return
			}
			if got.Headers.SetCookie == nil {
				t.Fatal("a dead handshake cookie was not cleared")
			}
			c, err := http.ParseSetCookie(*got.Headers.SetCookie)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if c.Name != auth.OnboardingCookieName || c.Value != "" || c.MaxAge >= 0 || !c.Expires.Before(time.Now()) {
				t.Errorf("clearing cookie = %+v, want %s expired", c, auth.OnboardingCookieName)
			}
		})
	}
}

func TestCommitFoundsTheHouseholdAndIssuesTheSession(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, token := h.atTheGate(t, "198.51.100.55", "Dewi@Example.com", func(r *registration) {
		r.userDisplayName = ptr("Dewi Putri")
		r.locale = ptr("id-ID")
	})
	var handshakeHash string
	if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT password_hash FROM onboarding_handshakes`).Scan(&handshakeHash); err != nil {
		t.Fatalf("read handshake: %v", err)
	}

	resp := h.commit(ctx, t, true, "  Rumah Tangga Dewi  ")
	rec := visit(t, resp)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s), want 204", rec.Code, rec.Body.String())
	}

	// Two Set-Cookie lines: the session, and the handshake expired.
	cookies := setCookies(rec)
	session, cleared := cookies[auth.SessionCookieName], cookies[auth.OnboardingCookieName]
	if session == nil || session.Value == "" || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.MaxAge <= 0 {
		t.Errorf("session cookie = %+v, want a live HttpOnly Secure Lax session cookie", session)
	}
	if cleared == nil || cleared.Value != "" || cleared.MaxAge >= 0 || !cleared.Expires.Before(time.Now()) ||
		!cleared.HttpOnly || !cleared.Secure || cleared.SameSite != http.SameSiteLaxMode || cleared.Path != "/" {
		t.Errorf("handshake cookie = %+v, want it expired, HttpOnly Secure Lax on /", cleared)
	}

	if got := h.foundingTables(t); got != "1/1/1/1/0" {
		t.Fatalf("households/users/credentials/sessions/handshakes = %s, want 1/1/1/1/0", got)
	}

	var (
		householdID, userID pgtype.UUID
		hhName, currency    string
		email, name, locale string
		zone                string
		credUser            pgtype.UUID
		credHash            string
		sessionUser         pgtype.UUID
	)
	if err := h.tdb.Pool.QueryRow(context.Background(), `
		SELECT h.id, h.household_display_name, h.reporting_currency, u.id, u.email, u.user_display_name, u.locale, u.time_zone,
		       c.user_id, c.password_hash, s.user_id
		FROM households h
		JOIN users u ON u.household_id = h.id
		JOIN credentials c ON c.user_id = u.id
		JOIN sessions s ON s.id = $1`, auth.HashToken(session.Value)).
		Scan(&householdID, &hhName, &currency, &userID, &email, &name, &locale, &zone, &credUser, &credHash, &sessionUser); err != nil {
		t.Fatalf("read the founded account: %v", err)
	}
	if hhName != "Rumah Tangga Dewi" {
		t.Errorf("household name = %q, want it trimmed", hhName)
	}
	if currency != "IDR" || zone != "Asia/Jakarta" {
		t.Errorf("currency %q, time zone %q, want the schema's defaults IDR / Asia/Jakarta", currency, zone)
	}
	if email != "dewi@example.com" || name != "Dewi Putri" || locale != "id-ID" {
		t.Errorf("user = %q / %q / %q, want the handshake's email, user_display_name and locale", email, name, locale)
	}
	if credUser != userID || sessionUser != userID {
		t.Error("the credential and the session do not belong to the new User")
	}
	if credHash != handshakeHash {
		t.Error("the credential is not the hash the registration stored")
	}

	// The server mints both ids, and they are UUIDv7 (BOOTSTRAP.md §4).
	for name, id := range map[string]pgtype.UUID{"household": householdID, "user": userID} {
		if v := id.Bytes[6] >> 4; v != 7 {
			t.Errorf("%s id %x is version %d, want 7", name, id.Bytes, v)
		}
	}

	// The first Pocket (#140 R2): one, in the new Household, active, named as
	// sent but trimmed, with a server-minted UUIDv7 of its own.
	var (
		pocketID, pocketHousehold pgtype.UUID
		pocketName                string
		pocketActive              bool
		pocketDeleted             bool
	)
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT id, household_id, pocket_display_name, active, deleted_at IS NOT NULL FROM pockets`).
		Scan(&pocketID, &pocketHousehold, &pocketName, &pocketActive, &pocketDeleted); err != nil {
		t.Fatalf("read the founded Pocket (want exactly one): %v", err)
	}
	if pocketHousehold != householdID || pocketName != "Cash" || !pocketActive || pocketDeleted {
		t.Errorf("pocket = household %x %q active=%v deleted=%v, want the new Household's, \"Cash\", active, live", pocketHousehold.Bytes, pocketName, pocketActive, pocketDeleted)
	}
	if v := pocketID.Bytes[6] >> 4; v != 7 || pocketID == householdID || pocketID == userID {
		t.Errorf("pocket id %x is version %d (or reuses another id), want its own UUIDv7", pocketID.Bytes, v)
	}

	// The handshake is spent: the same cookie is now a stale one.
	if _, is := h.options(ctx, t).(api.GetOnboardingOptions401JSONResponse); !is {
		t.Error("a committed handshake still answers options")
	}
	_ = token

	// And the person can sign in with the password they registered.
	if _, is := h.login(ipContext("198.51.100.56"), t, "DEWI@example.com", registerPassword).(api.LocalLogin204Response); !is {
		t.Error("the founded account does not sign in with the registered password")
	}
}

// Two concurrent commits on one handshake cannot both found. The claim is
// DELETE ... RETURNING inside the founding transaction: the second blocks on the
// row, finds it gone, and answers 401 having written nothing. Balances runs
// these as separate statements, and a double submit there leaves an orphan
// Household.
func TestConcurrentDoubleCommitFoundsExactlyOnce(t *testing.T) {
	for round := range 5 {
		h := newHarness(t, time.Now)
		ctx, _ := h.atTheGate(t, "198.51.100.60", "race@example.com")

		const racers = 8
		answers := make(chan api.CommitOnboardingChoiceResponseObject, racers)
		var ready, wg sync.WaitGroup
		start := make(chan struct{})
		ready.Add(racers)
		for range racers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ready.Done()
				<-start
				answers <- h.commit(ctx, t, true, "Rumah")
			}()
		}
		ready.Wait()
		close(start)
		wg.Wait()
		close(answers)

		var created, unauthorized int
		for resp := range answers {
			switch visit(t, resp).Code {
			case http.StatusNoContent:
				created++
			case http.StatusUnauthorized:
				unauthorized++
			default:
				t.Errorf("round %d: a racing commit answered %T, want 204 or 401", round, resp)
			}
		}
		if created != 1 || unauthorized != racers-1 {
			t.Errorf("round %d: %d created, %d unauthorized; want exactly 1 and %d", round, created, unauthorized, racers-1)
		}
		if got := h.foundingTables(t); got != "1/1/1/1/0" {
			t.Errorf("round %d: households/users/credentials/sessions/handshakes = %s, want 1/1/1/1/0 (an orphan Household is the bug)", round, got)
		}
		if n := h.pocketCount(t); n != 1 {
			t.Errorf("round %d: pockets = %d, want exactly 1", round, n)
		}
	}
}

// The address can be taken between register and commit. The unique violation
// on users_email_idx is 409 EMAIL_TAKEN, and the whole founding rolls back: the
// Household inserted ahead of the failing User must not be left behind.
func TestCommitWithTheAddressTakenSinceRegisteringRollsBackEverything(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.61", "Taken@Example.com")
	// Taken since, in another case: the index folds it.
	h.tdb.CreateUser(t, h.tdb.CreateHousehold(t, "Someone else"), "taken@example.COM", "Someone")
	before := h.foundingTables(t) // 1 household, 1 user, no credential, no session, the handshake

	rec := assertCommitCode(t, h.commit(ctx, t, true, "Rumah"), http.StatusConflict, api.EMAILTAKEN)
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("a refused commit set cookies: %v", rec.Result().Cookies())
	}

	if got := h.foundingTables(t); got != before || got != "1/1/0/0/1" {
		t.Errorf("households/users/credentials/sessions/handshakes = %s, was %s; want 1/1/0/0/1 (no orphan Household, no credential, no session)", got, before)
	}
	if n := h.pocketCount(t); n != 0 {
		t.Errorf("pockets = %d, want 0: the Pocket is inserted last in the founding transaction and rolls back with the rest", n)
	}
}

// The first Pocket is the last statement of the founding transaction, after the
// Household, the User and the credential. A failure there must undo all three:
// no orphan Household that nobody can sign in to, and the handshake not spent,
// so the person can try again.
func TestAFailedFirstPocketInsertRollsBackTheWholeFounding(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.62", "Pocketless@Example.com")
	h = h.withBeginner(failingTxBeginner{Beginner: h.tdb.Pool, failOn: "-- name: CreatePocket"})
	if got := h.foundingTables(t); got != "0/0/0/0/1" {
		t.Fatalf("before = %s, want 0/0/0/0/1 (the handshake only)", got)
	}

	rec := assertCommitCode(t, h.commit(ctx, t, true, "Rumah"), http.StatusInternalServerError, api.INTERNAL)
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("a failed founding set cookies: %v", rec.Result().Cookies())
	}

	// households/users/credentials/sessions/handshakes: the Household, the User
	// and the credential were inserted ahead of the Pocket and are gone again.
	if got := h.foundingTables(t); got != "0/0/0/0/1" {
		t.Errorf("households/users/credentials/sessions/handshakes = %s, want 0/0/0/0/1 (nothing founded, the handshake preserved)", got)
	}
	if n := h.pocketCount(t); n != 0 {
		t.Errorf("pockets = %d, want 0", n)
	}

	// And the preserved handshake still founds, once the outage is over.
	h = h.withBeginner(h.tdb.Pool)
	if rec := visit(t, h.commit(ctx, t, true, "Rumah")); rec.Code != http.StatusNoContent {
		t.Fatalf("the retry = %d (%s), want 204", rec.Code, rec.Body.String())
	}
	if got := h.foundingTables(t); got != "1/1/1/1/0" || h.pocketCount(t) != 1 {
		t.Errorf("after the retry: %s, pockets %d; want 1/1/1/1/0 and 1", got, h.pocketCount(t))
	}
}

func TestCommitWhileFoundingIsDisabledIs403AndSpendsNothing(t *testing.T) {
	h := newHarness(t, time.Now).withFoundingDisabled()
	ctx, _ := h.atTheGate(t, "198.51.100.62", "a@example.com")

	assertCommitCode(t, h.commit(ctx, t, true, "Rumah"), http.StatusForbidden, api.FOUNDINGDISABLED)
	if got := h.foundingTables(t); got != "0/0/0/0/1" {
		t.Errorf("households/users/credentials/sessions/handshakes = %s, want 0/0/0/0/1", got)
	}

	// An absent handshake is answered first, so a stranger learns nothing about
	// the instance's posture from a 403.
	assertCommitCode(t, h.commit(ipContext("198.51.100.63"), t, true, "Rumah"), http.StatusUnauthorized, api.UNAUTHORIZED)
}

func TestCommitWithoutALiveHandshakeIs401(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.64", "a@example.com")
	if _, err := h.tdb.Pool.Exec(context.Background(), `UPDATE onboarding_handshakes SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire: %v", err)
	}

	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"no cookie", ipContext("198.51.100.65")},
		{"unknown token", auth.WithOnboardingTokenForTest(ipContext("198.51.100.66"), "never-issued")},
		{"expired handshake", ctx},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertCommitCode(t, h.commit(tc.ctx, t, true, "Rumah"), http.StatusUnauthorized, api.UNAUTHORIZED)
			if got := h.foundingTables(t); got != "0/0/0/0/1" {
				t.Errorf("households/users/credentials/sessions/handshakes = %s, want 0/0/0/0/1: nothing created, the expired row not claimed", got)
			}
		})
	}
}

// The handshake can also expire between the read that lets a commit proceed and
// its claim. The claim carries the expiry test, in SQL, so it is the claim that
// refuses, and nothing is created.
func TestAnExpiredHandshakeCannotBeClaimed(t *testing.T) {
	h := newHarness(t, time.Now)
	_, token := h.atTheGate(t, "198.51.100.67", "a@example.com")
	if _, err := h.tdb.Pool.Exec(context.Background(), `UPDATE onboarding_handshakes SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire: %v", err)
	}

	if _, err := h.tdb.Queries.ClaimOnboardingHandshake(context.Background(), auth.HashToken(token)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claiming an expired handshake = %v, want no rows", err)
	}
	if got := h.foundingTables(t); got != "0/0/0/0/1" {
		t.Errorf("tables = %s, want 0/0/0/0/1", got)
	}
}

func TestCommitRefusesAnythingButAFoundChoiceWithoutSpendingTheHandshake(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.68", "a@example.com")

	for _, tc := range []struct {
		name  string
		found bool
		input string
		field string
		rule  string
	}{
		{"not a found choice", false, "Rumah", "found", "required"},
		{"blank name", true, "", "household_display_name", "required"},
		{"spaces only", true, "   \t ", "household_display_name", "required"},
		// U+00A0 is whitespace to Go's TrimSpace but not to Postgres's btrim: the
		// trim here is what keeps it from reaching a CHECK it would pass.
		{"no-break space only", true, "  ", "household_display_name", "required"},
		{"61 code points", true, strings.Repeat("a", 61), "household_display_name", "max"},
		{"61 code points of 4 bytes", true, strings.Repeat("\U0001F6F6", 61), "household_display_name", "max"},
		{"61 code points once padded", true, " " + strings.Repeat("a", 61) + " ", "household_display_name", "max"},
		{"NUL", true, "Ru\x00mah", "household_display_name", "pattern"},
		{"zero-width characters only", true, "\u200b\u200c\u200d", "household_display_name", "required"},
		{"word joiner and BOM among spaces", true, " \u2060 \ufeff ", "household_display_name", "required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := assertCommitCode(t, h.commit(ctx, t, tc.found, tc.input), http.StatusBadRequest, api.VALIDATION)
			if !strings.Contains(rec.Body.String(), `"field":"`+tc.field+`"`) || !strings.Contains(rec.Body.String(), `"rule":"`+tc.rule+`"`) {
				t.Errorf("body = %s, want field %s rule %s", rec.Body.String(), tc.field, tc.rule)
			}
			if got := h.foundingTables(t); got != "0/0/0/0/1" {
				t.Errorf("tables = %s, want 0/0/0/0/1: a refused choice must leave the handshake to try again", got)
			}
		})
	}
}

// 60 code points is the cap, counted as code points: four-byte runes and a
// padded name both fit.
func TestCommitAcceptsAHouseholdNameOfExactlySixtyCodePoints(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.69", "a@example.com")
	name := strings.Repeat("\U0001F6F6", 60)

	if rec := visit(t, h.commit(ctx, t, true, "  "+name+"  ")); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s), want 204", rec.Code, rec.Body.String())
	}
	var got string
	if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT household_display_name FROM households`).Scan(&got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != name {
		t.Errorf("household name = %q, want the 60 code points trimmed", got)
	}
}

// An invisible character is not stripped from a name that has visible ones, and
// a bidi control inside a visible name stays (Balances parity).
func TestCommitKeepsInvisibleCharactersInAVisibleName(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.72", "a@example.com")
	name := "Rumah\u200b \u202e Dewi"

	if rec := visit(t, h.commit(ctx, t, true, name)); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s), want 204", rec.Code, rec.Body.String())
	}
	var got string
	if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT household_display_name FROM households`).Scan(&got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != name {
		t.Errorf("household name = %q, want %q unchanged", got, name)
	}
}

// Two People who register the same address both hold a handshake, and the first
// to commit wins: the second finds the address taken and nothing is created.
func TestTwoHandshakesForOneAddressFoundOnce(t *testing.T) {
	h := newHarness(t, time.Now)
	first, _ := h.atTheGate(t, "198.51.100.70", "same@example.com")
	second, _ := h.atTheGate(t, "198.51.100.71", "same@example.com")

	if rec := visit(t, h.commit(first, t, true, "First")); rec.Code != http.StatusNoContent {
		t.Fatalf("first commit = %d, want 204", rec.Code)
	}
	assertCommitCode(t, h.commit(second, t, true, "Second"), http.StatusConflict, api.EMAILTAKEN)
	if got := h.foundingTables(t); got != "1/1/1/1/1" {
		t.Errorf("tables = %s, want 1/1/1/1/1", got)
	}
}

// The first Pocket's name is validated with a Pocket's own rule, before the
// handshake is claimed, so a typo in it costs the person nothing (#140 R2).
func TestCommitRefusesABadPocketNameWithoutSpendingTheHandshake(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.80", "a@example.com")

	for _, tc := range []struct{ name, input, rule string }{
		{"blank", "", "required"},
		{"spaces only", "   \t ", "required"},
		{"zero-width characters only", "\u200b\u200c\u200d", "required"},
		{"61 code points", strings.Repeat("a", 61), "max"},
		{"61 code points of 4 bytes", strings.Repeat("\U0001F6F6", 61), "max"},
		{"NUL", "Ca\x00sh", "pattern"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := assertCommitCode(t, h.commitPocket(ctx, t, true, "Rumah", tc.input), http.StatusBadRequest, api.VALIDATION)
			if !strings.Contains(rec.Body.String(), `"field":"pocket_display_name"`) || !strings.Contains(rec.Body.String(), `"rule":"`+tc.rule+`"`) {
				t.Errorf("body = %s, want field pocket_display_name rule %s", rec.Body.String(), tc.rule)
			}
			if got := h.foundingTables(t); got != "0/0/0/0/1" || h.pocketCount(t) != 0 {
				t.Errorf("tables = %s, pockets = %d, want 0/0/0/0/1 and none: a refused choice leaves the handshake to try again", got, h.pocketCount(t))
			}
		})
	}
}

// Both names wrong: the Household's is the one reported, the order the fields
// have always been checked in, so a client that fixes what it is told reaches
// the Pocket's next.
func TestCommitReportsTheHouseholdNameBeforeThePocketName(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.81", "a@example.com")

	rec := assertCommitCode(t, h.commitPocket(ctx, t, true, "  ", "  "), http.StatusBadRequest, api.VALIDATION)
	if !strings.Contains(rec.Body.String(), `"field":"household_display_name"`) {
		t.Errorf("body = %s, want the Household's field first", rec.Body.String())
	}
}

// The Pocket's name is trimmed and may be 60 code points; a bidi control inside a
// visible name stays, as for a Household (the rule is one function).
func TestCommitStoresTheFirstPocketsNameTrimmed(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx, _ := h.atTheGate(t, "198.51.100.82", "a@example.com")
	name := strings.Repeat("\U0001F6F6", 60)

	if rec := visit(t, h.commitPocket(ctx, t, true, "Rumah", "  "+name+"  ")); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s), want 204", rec.Code, rec.Body.String())
	}
	var got string
	if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT pocket_display_name FROM pockets`).Scan(&got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != name {
		t.Errorf("pocket name = %q, want the 60 code points trimmed", got)
	}
}
