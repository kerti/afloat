package auth_test

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
)

func TestGetMeWithoutASessionIsUnauthorized(t *testing.T) {
	h := newHarness(t, time.Now)

	resp, err := h.auth.GetMe(context.Background(), api.GetMeRequestObject{})
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	got, is := resp.(api.GetMe401JSONResponse)
	if !is {
		t.Fatalf("response = %T, want 401", resp)
	}
	if got.Code != api.UNAUTHORIZED {
		t.Errorf("code = %v, want UNAUTHORIZED", got.Code)
	}
}

// The tenancy check that matters: /me must report the caller's OWN Household,
// resolved from the authenticated User, never from anything the request
// supplied. A fake Querier cannot prove this — there has to be a second
// Household in the database to be wrongly returned.
func TestGetMeReportsTheCallersOwnHousehold(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := context.Background()

	ourHousehold := h.tdb.CreateHousehold(t, "Our Household")
	ourUser := h.tdb.CreateUser(t, ourHousehold, "ours@example.com", "Ours")

	// A second tenant that must never appear in the response.
	theirHousehold := h.tdb.CreateHousehold(t, "Their Household")
	h.tdb.CreateUser(t, theirHousehold, "theirs@example.com", "Theirs")

	user, err := h.tdb.Queries.GetUserByID(ctx, ourUser)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}

	resp, err := h.auth.GetMe(auth.WithUser(ctx, user), api.GetMeRequestObject{})
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	me, is := resp.(api.GetMe200JSONResponse)
	if !is {
		t.Fatalf("response = %T, want 200", resp)
	}

	if me.Household.HouseholdDisplayName != "Our Household" {
		t.Errorf("household = %q, want Our Household", me.Household.HouseholdDisplayName)
	}
	if me.Email != "ours@example.com" {
		t.Errorf("email = %q, want ours@example.com", me.Email)
	}
}

// Defaults come back as PRD S3/S4 specify, and day_starts_at is HH:MM — the
// contract's shape, not a timestamp with an invented date on it.
func TestGetMeProjectsHouseholdSettings(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := context.Background()

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")

	user, err := h.tdb.Queries.GetUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	resp, err := h.auth.GetMe(auth.WithUser(ctx, user), api.GetMeRequestObject{})
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	me := resp.(api.GetMe200JSONResponse)

	if me.Household.ReportingCurrency != "IDR" {
		t.Errorf("reporting_currency = %q, want IDR", me.Household.ReportingCurrency)
	}
	if me.Household.PeriodStartDay != 1 {
		t.Errorf("period_start_day = %d, want 1", me.Household.PeriodStartDay)
	}
	if me.Household.DayStartsAt != "04:00" {
		t.Errorf("day_starts_at = %q, want 04:00", me.Household.DayStartsAt)
	}
	if me.Locale != api.EnGB {
		t.Errorf("locale = %q, want en-GB", me.Locale)
	}
	if me.TimeZone != "Asia/Jakarta" {
		t.Errorf("time_zone = %q, want Asia/Jakarta", me.TimeZone)
	}
	if me.Household.AllowanceMode != api.Adaptive {
		t.Errorf("allowance_mode = %q, want adaptive", me.Household.AllowanceMode)
	}
	// Null means "not supplied", which is a different statement from zero.
	if me.Household.ExpectedMonthlyIncome != nil {
		t.Errorf("expected_monthly_income = %v, want absent", *me.Household.ExpectedMonthlyIncome)
	}
}

// Money is a STRING on the wire (BOOTSTRAP.md §4), and must not arrive as a
// JSON number or in scientific notation.
func TestGetMeSerialisesMoneyAsAString(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := context.Background()

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	if _, err := h.tdb.Pool.Exec(ctx,
		`UPDATE households SET expected_monthly_income = 25000000.0000 WHERE id = $1`, householdID); err != nil {
		t.Fatalf("set income: %v", err)
	}
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")

	user, err := h.tdb.Queries.GetUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	resp, err := h.auth.GetMe(auth.WithUser(ctx, user), api.GetMeRequestObject{})
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	me := resp.(api.GetMe200JSONResponse)

	if me.Household.ExpectedMonthlyIncome == nil {
		t.Fatal("expected_monthly_income is absent, want a string")
	}
	if got := *me.Household.ExpectedMonthlyIncome; got != "25000000.0000" {
		t.Errorf("expected_monthly_income = %q, want 25000000.0000", got)
	}
}

// A whole-number amount must not be mistaken for the only shape money takes:
// a fractional value has to round-trip at the storage scale too, so trailing-
// zero trimming (fixed above) can't silently start passing again for one case
// while breaking the other.
func TestGetMeSerialisesFractionalMoneyAtStorageScale(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := context.Background()

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	if _, err := h.tdb.Pool.Exec(ctx,
		`UPDATE households SET expected_monthly_income = 1234.5600 WHERE id = $1`, householdID); err != nil {
		t.Fatalf("set income: %v", err)
	}
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")

	user, err := h.tdb.Queries.GetUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	resp, err := h.auth.GetMe(auth.WithUser(ctx, user), api.GetMeRequestObject{})
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	me := resp.(api.GetMe200JSONResponse)

	if me.Household.ExpectedMonthlyIncome == nil {
		t.Fatal("expected_monthly_income is absent, want a string")
	}
	if got := *me.Household.ExpectedMonthlyIncome; got != "1234.5600" {
		t.Errorf("expected_monthly_income = %q, want 1234.5600", got)
	}
}

func TestLogoutRevokesTheSessionAndClearsTheCookie(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := context.Background()

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
	cookie, err := h.auth.IssueSession(ctx, userID, "")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}

	resp, err := h.auth.Logout(auth.ContextForTest(ctx, "198.51.100.40", "", cookie.Value), api.LogoutRequestObject{})
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	out, is := resp.(api.Logout204Response)
	if !is {
		t.Fatalf("response = %T, want 204", resp)
	}
	if out.Headers.SetCookie == nil {
		t.Fatal("logout did not clear the cookie")
	}

	// Revocation IS the row delete — sessions are exempt from soft delete.
	var count int
	if err := h.tdb.Pool.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE id = $1`, auth.HashToken(cookie.Value)).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Error("the session row survived logout")
	}
	if _, ok, _ := h.resolve(t, cookie.Value); ok {
		t.Error("the revoked token still resolves")
	}
}

// Idempotent by contract: a client clearing a stale cookie must not have to
// special-case the result.
func TestLogoutWithoutASessionSucceeds(t *testing.T) {
	h := newHarness(t, time.Now)

	resp, err := h.auth.Logout(auth.ContextForTest(context.Background(), "198.51.100.41", "", ""), api.LogoutRequestObject{})
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	out, is := resp.(api.Logout204Response)
	if !is {
		t.Fatalf("response = %T, want 204", resp)
	}
	if out.Headers.SetCookie == nil {
		t.Error("logout without a session did not clear the client's cookie")
	}
}

// The wire shape of #113: the User's fields, and the Household nested whole.
// Nothing of the Household stays at the top level, and no unqualified name field
// survives to mean two things.
func TestGetMeNestsTheHousehold(t *testing.T) {
	h := newHarness(t, time.Now)
	ctx := context.Background()

	householdID := h.tdb.CreateHousehold(t, "Test Household")
	userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
	user, err := h.tdb.Queries.GetUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	resp, err := h.auth.GetMe(auth.WithUser(ctx, user), api.GetMeRequestObject{})
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	wire, err := json.Marshal(resp.(api.GetMe200JSONResponse))
	if err != nil {
		t.Fatal(err)
	}
	var me map[string]json.RawMessage
	if err := json.Unmarshal(wire, &me); err != nil {
		t.Fatal(err)
	}
	if got, want := slices.Sorted(maps.Keys(me)), []string{"appearance", "email", "household", "id", "locale", "time_zone", "today", "user_display_name"}; !slices.Equal(got, want) {
		t.Errorf("Me keys = %v, want %v", got, want)
	}

	var household map[string]any
	if err := json.Unmarshal(me["household"], &household); err != nil {
		t.Fatal(err)
	}
	if got, want := slices.Sorted(maps.Keys(household)), []string{
		"allowance_mode", "day_starts_at", "household_display_name", "id", "period_start_day", "reporting_currency",
	}; !slices.Equal(got, want) {
		t.Errorf("Household keys = %v, want %v (expected_monthly_income is omitted when null)", got, want)
	}
	if household["id"] != householdID.String() {
		t.Errorf("household id = %v", household["id"])
	}
}

// today is the caller's current Period Day (#140 R15): the date on the wall clock
// of the User's own time zone, less one day while that clock still reads earlier
// than the Household's day_starts_at, the same period.Today every dating
// handler uses. The clock is injected.
func TestGetMeCarriesTheCallersToday(t *testing.T) {
	at := func(s string) time.Time {
		t.Helper()
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tc := range []struct {
		name, zone, dayStartsAt, now, want string
	}{
		// Asia/Jakarta is UTC+7 with no daylight saving.
		{"03:59 local is still yesterday", "Asia/Jakarta", "04:00", "2026-11-25T20:59:00Z", "2026-11-25"},
		{"04:00 local is the new day", "Asia/Jakarta", "04:00", "2026-11-25T21:00:00Z", "2026-11-26"},
		{"a midnight start has no wrap", "Asia/Jakarta", "00:00", "2026-11-25T17:00:00Z", "2026-11-26"},
		{"another zone, on its own wall clock", "America/New_York", "04:00", "2026-11-25T08:59:00Z", "2026-11-24"},
		{"another zone, after its rollover", "America/New_York", "04:00", "2026-11-25T09:00:00Z", "2026-11-25"},
		{"UTC", "UTC", "04:00", "2026-11-25T03:59:59Z", "2026-11-24"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func() time.Time { return at(tc.now) })
			ctx := context.Background()
			householdID := h.tdb.CreateHousehold(t, "Test Household")
			userID := h.tdb.CreateUser(t, householdID, "a@example.com", "A")
			if _, err := h.tdb.Pool.Exec(ctx, `UPDATE users SET time_zone = $1 WHERE id = $2`, tc.zone, userID); err != nil {
				t.Fatal(err)
			}
			if _, err := h.tdb.Pool.Exec(ctx, `UPDATE households SET day_starts_at = $1::time WHERE id = $2`, tc.dayStartsAt, householdID); err != nil {
				t.Fatal(err)
			}
			user, err := h.tdb.Queries.GetUserByID(ctx, userID)
			if err != nil {
				t.Fatalf("GetUserByID: %v", err)
			}
			resp, err := h.auth.GetMe(auth.WithUser(ctx, user), api.GetMeRequestObject{})
			if err != nil {
				t.Fatalf("GetMe: %v", err)
			}
			me, ok := resp.(api.GetMe200JSONResponse)
			if !ok {
				t.Fatalf("GetMe = %#v, want 200", resp)
			}
			if got := me.Today.Format("2006-01-02"); got != tc.want {
				t.Errorf("today = %s, want %s", got, tc.want)
			}
		})
	}
}
