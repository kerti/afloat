package household_test

import (
	"context"
	"testing"

	"github.com/kerti/afloat/backend/internal/api"
)

// #137: updated_at moves only when a column's value does. The row is backdated,
// so "unchanged" cannot pass by the clock not having ticked.
func TestAPatchThatChangesNothingLeavesUpdatedAtAlone(t *testing.T) {
	f := newFixture(t)
	id, ctx := f.member(t, "Home", "a@example.com")
	if _, err := f.tdb.Pool.Exec(context.Background(),
		`UPDATE households SET expected_monthly_income = 1234.5600, updated_at = '2020-01-01 00:00:00+00' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	stored := func() string {
		t.Helper()
		var s string
		if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT updated_at::text FROM households WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	want := stored()

	before := f.ok(ctx, t, api.UpdateHouseholdRequest{})
	if got := stored(); got != want {
		t.Errorf("an empty PATCH moved updated_at: %s, want %s", got, want)
	}
	resend := f.ok(ctx, t, api.UpdateHouseholdRequest{
		HouseholdDisplayName:  ptr(before.HouseholdDisplayName),
		ReportingCurrency:     ptr(before.ReportingCurrency),
		PeriodStartDay:        ptr(before.PeriodStartDay),
		AllowanceMode:         ptr(before.AllowanceMode),
		ExpectedMonthlyIncome: income("1234.5600"),
	})
	if resend.HouseholdDisplayName != before.HouseholdDisplayName || resend.ReportingCurrency != before.ReportingCurrency ||
		resend.PeriodStartDay != before.PeriodStartDay || resend.AllowanceMode != before.AllowanceMode ||
		resend.ExpectedMonthlyIncome == nil || *resend.ExpectedMonthlyIncome != "1234.5600" {
		t.Errorf("resend answered %+v, want the row as it was", resend)
	}
	if got := stored(); got != want {
		t.Errorf("a PATCH of the stored values moved updated_at: %s, want %s", got, want)
	}

	f.ok(ctx, t, api.UpdateHouseholdRequest{HouseholdDisplayName: ptr("Renamed")})
	if got := stored(); got == want {
		t.Errorf("a real change left updated_at at %s", got)
	}
}
