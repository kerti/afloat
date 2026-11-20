package auth_test

import (
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/api"
)

// Tenancy: each of the three operations, run as one User, leaves every column
// of everyone else's users, credentials and sessions rows, and of every
// Household, byte-identical. The fixture is built so that a missing WHERE
// shows:
//
//   - a second User in the SAME Household, so a statement filtered on
//     household_id alone rewrites a member it should not;
//   - a User in another Household;
//   - both of them holding the SAME stored password hash as the actor, so the
//     credential swap's compare-and-swap would match their rows too if its
//     user_id filter were gone.
func TestUserSettingsNeverTouchAnotherUser(t *testing.T) {
	h := newHarness(t, time.Now)
	shared := h.tdb.CreateHousehold(t, "Shared")
	actor := h.seedMember(t, shared, "actor@example.com", goodPassword)
	sibling := h.seedMember(t, shared, "sibling@example.com", goodPassword)
	stranger := h.seedMemberAlone(t, "stranger@example.com")

	// One stored hash for all three.
	for _, other := range []member{sibling, stranger} {
		if _, err := h.tdb.Pool.Exec(t.Context(),
			`UPDATE credentials SET password_hash = (SELECT password_hash FROM credentials WHERE user_id = $1) WHERE user_id = $2`,
			actor.id, other.id); err != nil {
			t.Fatal(err)
		}
	}
	callers := h.signIn(t, actor)
	h.signIn(t, sibling)
	h.signIn(t, sibling)
	h.signIn(t, stranger)

	othersState := func() string {
		return h.snapshot(t, `
			SELECT jsonb_build_object(
			  'users',       (SELECT coalesce(jsonb_agg(to_jsonb(u) ORDER BY id), '[]') FROM users u WHERE id <> $1),
			  'credentials', (SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY user_id), '[]') FROM credentials c WHERE user_id <> $1),
			  'sessions',    (SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY id), '[]') FROM sessions s WHERE user_id <> $1),
			  'households',  (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id), '[]') FROM households x)
			)::text`, actor.id)
	}
	before := othersState()
	ctx := h.ctxWithToken(t, actor, "198.51.100.1", callers)

	check := func(what string) {
		t.Helper()
		if after := othersState(); after != before {
			t.Fatalf("%s as one User changed someone else's rows:\nbefore %s\nafter  %s", what, before, after)
		}
	}

	for _, body := range []api.UpdateMeRequest{
		{UserDisplayName: ptr("Taken over")},
		{Locale: ptr(api.IdID)},
		{TimeZone: ptr("America/New_York")},
		{UserDisplayName: ptr("All"), Locale: ptr(api.EnGB), TimeZone: ptr("Asia/Makassar")},
		{},
	} {
		h.updatedMe(ctx, t, body)
		check("PATCH /me")
	}

	changedEmail(t, h.changeEmail(ctx, t, "actor.new@example.com", goodPassword))
	check("change-email")

	changedPassword(t, h.changePassword(ctx, t, goodPassword, newPassword))
	check("change-password")
}
