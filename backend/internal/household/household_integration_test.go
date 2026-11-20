package household_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/household"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// The tests here talk to a real Postgres and none is parallel (ADR-0004): they
// share one database that each test empties.

type fixture struct {
	tdb      *testutil.TestDB
	handlers *household.Handlers
	auth     *auth.Handlers
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return newFixtureWith(t, nil)
}

// newFixtureWith wraps the real Querier, so a test can break one call.
func newFixtureWith(t *testing.T, wrap func(db.Querier) db.Querier) fixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	var q db.Querier = tdb.Queries
	if wrap != nil {
		q = wrap(q)
	}
	return fixture{
		tdb:      tdb,
		handlers: household.New(household.Deps{Querier: q}),
		auth: auth.New(auth.Deps{
			Querier:            tdb.Queries,
			Beginner:           tdb.Pool,
			SessionTTL:         time.Hour,
			SessionMaxLifetime: 2 * time.Hour,
			CookieSecure:       true,
			FirstBackoff:       time.Second,
			MaxBackoff:         time.Minute,
		}),
	}
}

// member creates a Household with one User and returns the context of a request
// that User's session produced.
func (f fixture) member(t *testing.T, name, email string) (pgtype.UUID, context.Context) {
	t.Helper()
	householdID := f.tdb.CreateHousehold(t, name)
	userID := f.tdb.CreateUser(t, householdID, email, "A")
	user, err := f.tdb.Queries.GetUserByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	return householdID, auth.WithUser(context.Background(), user)
}

func (f fixture) patch(ctx context.Context, t *testing.T, body api.UpdateHouseholdRequest) api.UpdateHouseholdResponseObject {
	t.Helper()
	resp, err := f.handlers.UpdateHousehold(ctx, api.UpdateHouseholdRequestObject{Body: &body})
	if err != nil {
		t.Fatalf("UpdateHousehold returned an error: %v", err)
	}
	return resp
}

func (f fixture) ok(ctx context.Context, t *testing.T, body api.UpdateHouseholdRequest) api.Household {
	t.Helper()
	resp := f.patch(ctx, t, body)
	got, is := resp.(api.UpdateHousehold200JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 200", resp)
	}
	return api.Household(got)
}

// row is the Household's whole row as JSON: every column, byte for byte.
func (f fixture) row(t *testing.T, id pgtype.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT to_jsonb(h)::text FROM households h WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read household: %v", err)
	}
	return s
}

func (f fixture) me(ctx context.Context, t *testing.T) api.Me {
	t.Helper()
	resp, err := f.auth.GetMe(ctx, api.GetMeRequestObject{})
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	me, is := resp.(api.GetMe200JSONResponse)
	if !is {
		t.Fatalf("GetMe = %T, want 200", resp)
	}
	return api.Me(me)
}

func ptr[T any](v T) *T { return &v }

func assertValidation(t *testing.T, resp api.UpdateHouseholdResponseObject, field, rule string) {
	t.Helper()
	got, is := resp.(api.UpdateHousehold400JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 400", resp)
	}
	if got.Code != api.VALIDATION {
		t.Fatalf("code = %s, want VALIDATION", got.Code)
	}
	args := *got.Args
	if args["field"] != field || args["rule"] != rule {
		t.Errorf("args = %v, want field %s rule %s", args, field, rule)
	}
}

// Each writable field changes, the answer is the whole Household, and /me
// reports it under `household`.
func TestEachFieldUpdatesAndMeReflectsIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  api.UpdateHouseholdRequest
		check func(t *testing.T, h api.Household)
	}{
		{"household_display_name", api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("Rumah Kita")}, func(t *testing.T, h api.Household) {
			if h.HouseholdDisplayName != "Rumah Kita" {
				t.Errorf("household_display_name = %q", h.HouseholdDisplayName)
			}
		}},
		{"reporting_currency", api.UpdateHouseholdRequest{ReportingCurrency: ptr("USD")}, func(t *testing.T, h api.Household) {
			if h.ReportingCurrency != "USD" {
				t.Errorf("reporting_currency = %q", h.ReportingCurrency)
			}
		}},
		{"period_start_day", api.UpdateHouseholdRequest{PeriodStartDay: ptr(25)}, func(t *testing.T, h api.Household) {
			if h.PeriodStartDay != 25 {
				t.Errorf("period_start_day = %d", h.PeriodStartDay)
			}
		}},
		{"period_start_day at both ends", api.UpdateHouseholdRequest{PeriodStartDay: ptr(28)}, func(t *testing.T, h api.Household) {
			if h.PeriodStartDay != 28 {
				t.Errorf("period_start_day = %d", h.PeriodStartDay)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			_, ctx := f.member(t, "Home", "a@example.com")

			got := f.ok(ctx, t, tc.body)
			tc.check(t, got)

			me := f.me(ctx, t)
			if me.Household != got {
				t.Errorf("/me household = %+v, want what the PATCH answered, %+v", me.Household, got)
			}
		})
	}
}

// A partial body leaves every other column, created_at included, alone.
func TestAPartialBodyLeavesTheRestAlone(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")
	if _, err := f.tdb.Pool.Exec(context.Background(),
		`UPDATE households SET reporting_currency = 'EUR', period_start_day = 7, day_starts_at = '05:30',
		   allowance_mode = 'fixed', expected_monthly_income = 1234.5600,
		   created_at = now() - interval '10 days', updated_at = now() - interval '10 days' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	type cols struct {
		Name, Currency, DayStartsAt, Mode string
		Day                               int
		Income                            string
		Created, Updated                  time.Time
		Deleted                           *time.Time
	}
	read := func() (c cols) {
		t.Helper()
		if err := f.tdb.Pool.QueryRow(context.Background(), `
			SELECT household_display_name, reporting_currency, period_start_day, day_starts_at::text, allowance_mode,
			       expected_monthly_income::text, created_at, updated_at, deleted_at
			FROM households WHERE id = $1`, id).
			Scan(&c.Name, &c.Currency, &c.Day, &c.DayStartsAt, &c.Mode, &c.Income, &c.Created, &c.Updated, &c.Deleted); err != nil {
			t.Fatal(err)
		}
		return c
	}
	before := read()

	got := f.ok(ctx, t, api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("Renamed")})

	after := read()
	want := before
	want.Name = "Renamed"
	want.Updated = after.Updated // moves, and is not on the wire
	if after != want {
		t.Errorf("row = %+v, want %+v", after, want)
	}
	if !after.Updated.After(before.Updated) {
		t.Errorf("updated_at = %v, want later than %v: the baseline has no trigger, so the query sets it", after.Updated, before.Updated)
	}
	if got.ReportingCurrency != "EUR" || got.PeriodStartDay != 7 || got.DayStartsAt != "05:30" || got.AllowanceMode != api.Fixed {
		t.Errorf("answer = %+v, want the untouched settings as they are", got)
	}
	if got.ExpectedMonthlyIncome == nil || *got.ExpectedMonthlyIncome != "1234.5600" {
		t.Errorf("expected_monthly_income = %v, want 1234.5600 (a 4dp string)", got.ExpectedMonthlyIncome)
	}
}

// `fixed` round-trips: the schema, the projection and /me agree on the word.
func TestFixedAllowanceModeRoundTrips(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")
	if _, err := f.tdb.Pool.Exec(context.Background(), `UPDATE households SET allowance_mode = 'fixed' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if got := f.ok(ctx, t, api.UpdateHouseholdRequest{}).AllowanceMode; got != api.Fixed {
		t.Errorf("PATCH answered allowance_mode = %q, want fixed", got)
	}
	if got := f.me(ctx, t).Household.AllowanceMode; got != api.Fixed {
		t.Errorf("/me allowance_mode = %q, want fixed", got)
	}
}

// `{}` is a valid no-op: 200, the Household as it is.
func TestAnEmptyBodyChangesNothing(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")
	before := f.row(t, id)

	got := f.ok(ctx, t, api.UpdateHouseholdRequest{})
	if got != f.me(ctx, t).Household {
		t.Errorf("answer %+v differs from /me", got)
	}
	// Only updated_at may move; compare everything else.
	var same bool
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT to_jsonb(h) - 'updated_at' = $2::jsonb - 'updated_at' FROM households h WHERE id = $1`, id, before).Scan(&same); err != nil || !same {
		t.Errorf("a no-op PATCH changed a column other than updated_at (%v)", err)
	}
}

func TestTheNameIsStoredTrimmed(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	if got := f.ok(ctx, t, api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("\t Rumah Kita  \n")}).HouseholdDisplayName; got != "Rumah Kita" {
		t.Errorf("household_display_name = %q, want it trimmed", got)
	}
}

// The handler's rules, with the exact (field, rule) each reports, and a
// refusal writes nothing, a valid field beside the bad one included.
func TestHandlerRules(t *testing.T) {
	for _, tc := range []struct {
		name, field, rule string
		body              api.UpdateHouseholdRequest
	}{
		{"blank name", "household_display_name", "required", api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("")}},
		{"whitespace only", "household_display_name", "required", api.UpdateHouseholdRequest{HouseholdDisplayName: ptr(" \t ")}},
		{"zero-width only", "household_display_name", "required", api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("\u200b\u200c\u200d")}},
		{"61 code points", "household_display_name", "max", api.UpdateHouseholdRequest{HouseholdDisplayName: ptr(strings.Repeat("a", 61))}},
		{"NUL", "household_display_name", "pattern", api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("Ru\x00mah")}},
		{"not a currency", "reporting_currency", "iso4217", api.UpdateHouseholdRequest{ReportingCurrency: ptr("ABC")}},
		{"valid name, bad currency", "reporting_currency", "iso4217", api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("Fine"), ReportingCurrency: ptr("ABC")}},
		{"valid day, bad currency", "reporting_currency", "iso4217", api.UpdateHouseholdRequest{PeriodStartDay: ptr(3), ReportingCurrency: ptr("ABC")}},
		// The least wire field name, then rule, whichever check found it first.
		{"name beats currency", "household_display_name", "required", api.UpdateHouseholdRequest{HouseholdDisplayName: ptr(" "), ReportingCurrency: ptr("ABC")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			id, ctx := f.member(t, "Home", "a@example.com")
			before := f.row(t, id)

			assertValidation(t, f.patch(ctx, t, tc.body), tc.field, tc.rule)
			if after := f.row(t, id); after != before {
				t.Errorf("a refused PATCH wrote: %s -> %s", before, after)
			}
		})
	}
}

// Real ISO 4217 codes are accepted, not just the default.
func TestAnISO4217CodeIsAccepted(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	for _, code := range []string{"USD", "EUR", "JPY", "SGD", "IDR"} {
		if got := f.ok(ctx, t, api.UpdateHouseholdRequest{ReportingCurrency: ptr(code)}).ReportingCurrency; got != code {
			t.Errorf("reporting_currency = %q, want %q", got, code)
		}
	}
}

// Founding and editing run one rule: the same names, the same answers. Each
// name goes through the real founding commit and through the PATCH, and the
// two must agree on whether it is stored, as what, and on the rule it breaks.
func TestFoundingAndEditingAgreeOnEveryName(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		wantStored  string
		wantRule    string // "" when accepted
	}{
		{"plain", "Rumah Kita", "Rumah Kita", ""},
		{"padded", "  Rumah Kita \t", "Rumah Kita", ""},
		{"60 code points", strings.Repeat("a", 60), strings.Repeat("a", 60), ""},
		{"60 four-byte code points", strings.Repeat("\U0001F6F6", 60), strings.Repeat("\U0001F6F6", 60), ""},
		{"61 only because of padding is accepted", " " + strings.Repeat("a", 60) + " ", strings.Repeat("a", 60), ""},
		{"zero-width char kept inside a visible name", "Dewi\u200b", "Dewi\u200b", ""},
		{"bidi control kept inside a visible name", "Dewi \u202e Putri", "Dewi \u202e Putri", ""},
		{"blank", "", "", "required"},
		{"spaces", "   \t ", "", "required"},
		{"no-break space only", "\u00a0\u00a0", "", "required"},
		{"zero-width only", "\u200b\u200c\u200d", "", "required"},
		{"word joiner and BOM among spaces", " \u2060 \ufeff ", "", "required"},
		{"61 code points", strings.Repeat("a", 61), "", "max"},
		{"61 four-byte code points", strings.Repeat("\U0001F6F6", 61), "", "max"},
		{"61 once padded", " " + strings.Repeat("a", 61) + " ", "", "max"},
		{"NUL", "Ru\x00mah", "", "pattern"},
		// A NUL beats the length cap only when the name is short enough to
		// reach it: the order is required, max, pattern.
		{"NUL in a 61 code point name is max first", strings.Repeat("a", 60) + "\x00", "", "max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)

			// Founding. A handshake is opened directly: the commit is what is
			// under test, not registration's hashing.
			const token = "a-handshake-token-for-the-name-rule-test"
			if _, err := f.tdb.Queries.CreateOnboardingHandshake(context.Background(), db.CreateOnboardingHandshakeParams{
				ID: auth.HashToken(token), Email: "new@example.com", UserDisplayName: "New", Locale: "en-GB", Ttl: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
			}); err != nil {
				t.Fatalf("open handshake: %v", err)
			}
			ctx := atTheGate(token)
			resp, err := f.auth.CommitOnboardingChoice(ctx, api.CommitOnboardingChoiceRequestObject{
				Body: &api.CommitOnboardingChoiceJSONRequestBody{Found: true, HouseholdDisplayName: tc.input},
			})
			if err != nil {
				t.Fatalf("commit: %v", err)
			}
			foundedAs, foundRule := foundingAnswer(t, f, resp)

			// Editing.
			_, editCtx := f.member(t, "Home", "a@example.com")
			editResp := f.patch(editCtx, t, api.UpdateHouseholdRequest{HouseholdDisplayName: ptr(tc.input)})
			editedAs, editRule := editingAnswer(t, editResp)

			if foundRule != tc.wantRule || editRule != tc.wantRule {
				t.Errorf("rule: founding %q, editing %q, want %q", foundRule, editRule, tc.wantRule)
			}
			if foundedAs != tc.wantStored || editedAs != tc.wantStored {
				t.Errorf("stored: founding %q, editing %q, want %q", foundedAs, editedAs, tc.wantStored)
			}
		})
	}
}

// atTheGate is the context a request carrying the handshake cookie gets from
// the real middleware.
func atTheGate(token string) context.Context {
	var ctx context.Context
	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/choice", nil)
	req.AddCookie(&http.Cookie{Name: auth.OnboardingCookieName, Value: token})
	auth.RequestContextMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), req)
	return ctx
}

// foundingAnswer is what the commit stored for the new Household, or the rule it
// refused the name with, and that the field it named is the wire's.
func foundingAnswer(t *testing.T, f fixture, resp api.CommitOnboardingChoiceResponseObject) (stored, rule string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitCommitOnboardingChoiceResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	switch rec.Code {
	case 204:
		if err := f.tdb.Pool.QueryRow(context.Background(),
			`SELECT household_display_name FROM households`).Scan(&stored); err != nil {
			t.Fatalf("read founded household: %v", err)
		}
		return stored, ""
	case 400:
		bad := resp.(api.CommitOnboardingChoice400JSONResponse)
		args := *bad.Args
		if args["field"] != "household_display_name" {
			t.Errorf("founding named field %v, want household_display_name", args["field"])
		}
		return "", fmt.Sprint(args["rule"])
	}
	t.Fatalf("founding answered %d: %s", rec.Code, rec.Body.String())
	return "", ""
}

func editingAnswer(t *testing.T, resp api.UpdateHouseholdResponseObject) (stored, rule string) {
	t.Helper()
	switch r := resp.(type) {
	case api.UpdateHousehold200JSONResponse:
		return r.HouseholdDisplayName, ""
	case api.UpdateHousehold400JSONResponse:
		args := *r.Args
		if args["field"] != "household_display_name" {
			t.Errorf("editing named field %v, want household_display_name", args["field"])
		}
		return "", fmt.Sprint(args["rule"])
	}
	t.Fatalf("editing answered %#v", resp)
	return "", ""
}

// No User in the context is the contract's 401, never an UPDATE with a zero
// household_id (the router's sessionRequired answers first; this is the
// defensive branch).
func TestNoUserInContextIsUnauthorized(t *testing.T) {
	f := newFixture(t)
	id, _ := f.member(t, "Home", "a@example.com")
	before := f.row(t, id)

	resp := f.patch(context.Background(), t, api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("Hijack")})
	got, is := resp.(api.UpdateHousehold401JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 401", resp)
	}
	if got.Code != api.UNAUTHORIZED {
		t.Errorf("code = %s", got.Code)
	}
	if f.row(t, id) != before {
		t.Error("an unauthenticated PATCH wrote")
	}
}

type failingUpdate struct{ db.Querier }

func (failingUpdate) UpdateHousehold(context.Context, db.UpdateHouseholdParams) (db.Household, error) {
	return db.Household{}, errors.New("connection reset by peer")
}

// A database failure is 500 INTERNAL, and says nothing about the cause.
func TestADatabaseFailureIsInternalAndWritesNothing(t *testing.T) {
	f := newFixtureWith(t, func(q db.Querier) db.Querier { return failingUpdate{q} })
	id, ctx := f.member(t, "Home", "a@example.com")
	before := f.row(t, id)

	resp := f.patch(ctx, t, api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("New")})
	got, is := resp.(api.UpdateHousehold500JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 500", resp)
	}
	if got.Code != api.INTERNAL || got.Args != nil {
		t.Errorf("body = %+v, want only the INTERNAL code", got.InternalErrorJSONResponse)
	}
	if f.row(t, id) != before {
		t.Error("a failed PATCH wrote")
	}
}

// A soft-deleted Household is not updated, and not revived: the UPDATE matches
// no row, which is answered 500 as GetMe answers a User with no Household.
func TestASoftDeletedHouseholdIsNeitherUpdatedNorRevived(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")
	if _, err := f.tdb.Pool.Exec(context.Background(), `UPDATE households SET deleted_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	before := f.row(t, id)

	if _, is := f.patch(ctx, t, api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("Zombie")}).(api.UpdateHousehold500JSONResponse); !is {
		t.Fatal("want 500 for a soft-deleted Household")
	}
	if f.row(t, id) != before {
		t.Error("a soft-deleted Household was written")
	}
}

// Tenancy: a PATCH as one Household leaves every column of another
// byte-identical. A second tenant has to exist for a missing WHERE to show.
func TestAPatchNeverTouchesAnotherHousehold(t *testing.T) {
	f := newFixture(t)
	_, ours := f.member(t, "Ours", "ours@example.com")
	theirs, _ := f.member(t, "Theirs", "theirs@example.com")
	if _, err := f.tdb.Pool.Exec(context.Background(),
		`UPDATE households SET expected_monthly_income = 5000.0000, allowance_mode = 'fixed' WHERE id = $1`, theirs); err != nil {
		t.Fatal(err)
	}
	before := f.row(t, theirs)

	for _, body := range []api.UpdateHouseholdRequest{
		{HouseholdDisplayName: ptr("Taken over")},
		{ReportingCurrency: ptr("USD")},
		{PeriodStartDay: ptr(28)},
		{HouseholdDisplayName: ptr("All"), ReportingCurrency: ptr("EUR"), PeriodStartDay: ptr(2)},
		{},
	} {
		f.ok(ours, t, body)
		if after := f.row(t, theirs); after != before {
			t.Fatalf("PATCH %+v as another Household changed this one:\nbefore %s\nafter  %s", body, before, after)
		}
	}
}

// Merge: concurrent PATCHes of different fields all land. A read-modify-write
// writes back the field it read, so two overlapping ones lose each other's;
// one UPDATE with coalesce cannot. Repeated, because a race is a probability.
func TestConcurrentPatchesOfDifferentFieldsAllLand(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")

	const rounds = 40
	for round := range rounds {
		name := fmt.Sprintf("Round %d", round)
		currency := []string{"USD", "EUR", "JPY", "SGD"}[round%4]
		// None of these equals what the row is reset to below, so a field that
		// was lost shows as the reset value.
		day := round%27 + 2
		if _, err := f.tdb.Pool.Exec(context.Background(),
			`UPDATE households SET household_display_name = 'reset', reporting_currency = 'IDR', period_start_day = 1 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, body := range []api.UpdateHouseholdRequest{
			{HouseholdDisplayName: &name},
			{ReportingCurrency: &currency},
			{PeriodStartDay: &day},
		} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				// Not f.patch: its Fatalf must not run off the test's goroutine.
				resp, err := f.handlers.UpdateHousehold(ctx, api.UpdateHouseholdRequestObject{Body: &body})
				if _, is := resp.(api.UpdateHousehold200JSONResponse); err != nil || !is {
					t.Errorf("PATCH %+v = (%#v, %v), want a 200", body, resp, err)
				}
			}()
		}
		close(start)
		wg.Wait()

		var gotName, gotCurrency string
		var gotDay int
		if err := f.tdb.Pool.QueryRow(context.Background(),
			`SELECT household_display_name, reporting_currency, period_start_day FROM households WHERE id = $1`, id).Scan(&gotName, &gotCurrency, &gotDay); err != nil {
			t.Fatal(err)
		}
		if gotName != name || gotCurrency != currency || gotDay != day {
			t.Fatalf("round %d: row = %q / %q / %d, want %q / %q / %d: a concurrent PATCH lost a field",
				round, gotName, gotCurrency, gotDay, name, currency, day)
		}
	}
}
