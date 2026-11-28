package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/api"
)

// #137: updated_at moves only when a column's value does. The row is backdated,
// so "unchanged" cannot pass by the clock not having ticked.
func TestUpdateMeThatChangesNothingLeavesUpdatedAtAlone(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE users SET updated_at = '2020-01-01 00:00:00+00' WHERE id = $1`, m.id); err != nil {
		t.Fatal(err)
	}
	stored := func() string {
		t.Helper()
		var s string
		if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT updated_at::text FROM users WHERE id = $1`, m.id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	want := stored()

	me := h.updatedMe(ctx, t, api.UpdateMeRequest{})
	if got := stored(); got != want {
		t.Errorf("an empty PATCH moved updated_at: %s, want %s", got, want)
	}
	resend := h.updatedMe(ctx, t, api.UpdateMeRequest{
		UserDisplayName: ptr(me.UserDisplayName),
		Locale:          ptr(me.Locale),
		TimeZone:        ptr(me.TimeZone),
		Appearance:      ptr(me.Appearance),
	})
	if resend != me {
		t.Errorf("resend answered %+v, want %+v", resend, me)
	}
	if got := stored(); got != want {
		t.Errorf("a PATCH of the stored values moved updated_at: %s, want %s", got, want)
	}

	h.updatedMe(ctx, t, api.UpdateMeRequest{UserDisplayName: ptr("Dewi")})
	if got := stored(); got == want {
		t.Errorf("a real change left updated_at at %s", got)
	}
}
