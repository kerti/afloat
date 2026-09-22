package auth_test

import (
	"context"
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

	if me.HouseholdDisplayName != "Our Household" {
		t.Errorf("household = %q, want Our Household", me.HouseholdDisplayName)
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

	if me.ReportingCurrency != "IDR" {
		t.Errorf("reporting_currency = %q, want IDR", me.ReportingCurrency)
	}
	if me.PeriodStartDay != 1 {
		t.Errorf("period_start_day = %d, want 1", me.PeriodStartDay)
	}
	if me.DayStartsAt != "04:00" {
		t.Errorf("day_starts_at = %q, want 04:00", me.DayStartsAt)
	}
	if me.Locale != api.EnGB {
		t.Errorf("locale = %q, want en-GB", me.Locale)
	}
	if me.TimeZone != "Asia/Jakarta" {
		t.Errorf("time_zone = %q, want Asia/Jakarta", me.TimeZone)
	}
	if me.AllowanceMode != api.Adaptive {
		t.Errorf("allowance_mode = %q, want adaptive", me.AllowanceMode)
	}
	// Null means "not supplied", which is a different statement from zero.
	if me.ExpectedMonthlyIncome != nil {
		t.Errorf("expected_monthly_income = %v, want absent", *me.ExpectedMonthlyIncome)
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

	if me.ExpectedMonthlyIncome == nil {
		t.Fatal("expected_monthly_income is absent, want a string")
	}
	if got := *me.ExpectedMonthlyIncome; got != "25000000.0000" {
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

	if me.ExpectedMonthlyIncome == nil {
		t.Fatal("expected_monthly_income is absent, want a string")
	}
	if got := *me.ExpectedMonthlyIncome; got != "1234.5600" {
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
