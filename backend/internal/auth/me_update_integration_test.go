package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/httperr"
)

// Each writable field changes, the answer is the whole Me, and GET /me agrees.
func TestUpdateMeEachFieldUpdatesAndMeReflectsIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  api.UpdateMeRequest
		check func(t *testing.T, me api.Me)
	}{
		{"user_display_name", api.UpdateMeRequest{UserDisplayName: ptr("Dewi")}, func(t *testing.T, me api.Me) {
			if me.UserDisplayName != "Dewi" {
				t.Errorf("user_display_name = %q", me.UserDisplayName)
			}
		}},
		{"locale", api.UpdateMeRequest{Locale: ptr(api.IdID)}, func(t *testing.T, me api.Me) {
			if me.Locale != api.IdID {
				t.Errorf("locale = %q", me.Locale)
			}
		}},
		{"time_zone", api.UpdateMeRequest{TimeZone: ptr("Asia/Makassar")}, func(t *testing.T, me api.Me) {
			if me.TimeZone != "Asia/Makassar" {
				t.Errorf("time_zone = %q", me.TimeZone)
			}
		}},
		{"appearance", api.UpdateMeRequest{Appearance: ptr(api.Dark)}, func(t *testing.T, me api.Me) {
			if me.Appearance != api.Dark {
				t.Errorf("appearance = %q", me.Appearance)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			m := h.seedMemberAlone(t, "a@example.com")
			ctx := h.ctx(t, m, "198.51.100.1")

			got := h.updatedMe(ctx, t, tc.body)
			tc.check(t, got)

			// A second read, from the row as it now stands.
			resp, err := h.auth.GetMe(h.ctx(t, m, "198.51.100.1"), api.GetMeRequestObject{})
			if err != nil {
				t.Fatal(err)
			}
			if me := api.Me(resp.(api.GetMe200JSONResponse)); me != got {
				t.Errorf("GET /me = %+v, want what the PATCH answered, %+v", me, got)
			}
		})
	}
}

// Appearance is a plain merge field: it lands in the row, and every other
// field, locale and time zone included, leaves it alone. The default is the
// schema's, so a User nobody has asked is on `system`.
func TestUpdateMeAppearanceLandsInTheRowAndOnlyMovesWithItsOwnPatch(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")
	stored := func() string {
		t.Helper()
		var got string
		if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT appearance FROM users WHERE id = $1`, m.id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := stored(); got != "system" {
		t.Fatalf("a new User's appearance = %q, want system", got)
	}
	for _, want := range []api.Appearance{api.Dark, api.Light, api.System} {
		if me := h.updatedMe(ctx, t, api.UpdateMeRequest{Appearance: &want}); me.Appearance != want {
			t.Errorf("answer appearance = %q, want %q", me.Appearance, want)
		}
		if got := stored(); got != string(want) {
			t.Errorf("stored appearance = %q, want %q", got, want)
		}
	}

	h.updatedMe(ctx, t, api.UpdateMeRequest{Appearance: ptr(api.Dark)})
	for _, other := range []api.UpdateMeRequest{
		{UserDisplayName: ptr("Renamed")},
		{Locale: ptr(api.IdID)},
		{TimeZone: ptr("Asia/Makassar")},
		{},
	} {
		if me := h.updatedMe(ctx, t, other); me.Appearance != api.Dark {
			t.Errorf("a PATCH without appearance (%+v) answered %q, want dark", other, me.Appearance)
		}
		if got := stored(); got != "dark" {
			t.Errorf("a PATCH without appearance (%+v) left %q stored, want dark", other, got)
		}
	}
}

// A second User in the same Household keeps their own appearance.
func TestUpdateMeAppearanceDoesNotReachAHouseholdMember(t *testing.T) {
	h := newHarness(t, time.Now)
	shared := h.tdb.CreateHousehold(t, "Shared")
	actor := h.seedMember(t, shared, "actor@example.com", goodPassword)
	sibling := h.seedMember(t, shared, "sibling@example.com", goodPassword)
	if _, err := h.tdb.Pool.Exec(context.Background(), `UPDATE users SET appearance = 'light' WHERE id = $1`, sibling.id); err != nil {
		t.Fatal(err)
	}
	before := h.userRow(t, sibling.id)

	h.updatedMe(h.ctx(t, actor, "198.51.100.1"), t, api.UpdateMeRequest{Appearance: ptr(api.Dark)})

	if after := h.userRow(t, sibling.id); after != before {
		t.Errorf("the sibling's row changed:\nbefore %s\nafter  %s", before, after)
	}
	var theirs string
	if err := h.tdb.Pool.QueryRow(context.Background(), `SELECT appearance FROM users WHERE id = $1`, sibling.id).Scan(&theirs); err != nil || theirs != "light" {
		t.Errorf("sibling appearance = %q (%v), want light", theirs, err)
	}
}

// A partial body leaves every other column, created_at included, alone, and
// updated_at moves because the baseline has no trigger.
func TestUpdateMeAPartialBodyLeavesTheRestAlone(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE users SET locale = 'id-ID', time_zone = 'Asia/Makassar',
		   created_at = now() - interval '10 days', updated_at = now() - interval '10 days' WHERE id = $1`, m.id); err != nil {
		t.Fatal(err)
	}
	ctx := h.ctx(t, m, "198.51.100.1")
	before := h.userRow(t, m.id)

	h.updatedMe(ctx, t, api.UpdateMeRequest{UserDisplayName: ptr("Renamed")})

	var same bool
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT (to_jsonb(u) - 'updated_at' - 'user_display_name') = ($2::jsonb - 'updated_at' - 'user_display_name')
		        AND u.updated_at > now() - interval '1 minute' AND u.user_display_name = 'Renamed'
		 FROM users u WHERE id = $1`, m.id, before).Scan(&same); err != nil || !same {
		t.Errorf("a partial PATCH changed a column it should not have, or did not move updated_at (%v)\nbefore %s\nafter  %s", err, before, h.userRow(t, m.id))
	}
}

// `{}` is a valid no-op: 200, the User as they are.
func TestUpdateMeAnEmptyBodyChangesNothing(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")
	before := h.userRow(t, m.id)

	got := h.updatedMe(ctx, t, api.UpdateMeRequest{})
	if got.Email != "a@example.com" || got.UserDisplayName != "Name of a@example.com" {
		t.Errorf("answer = %+v, want the User as they are", got)
	}
	var same bool
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT (to_jsonb(u) - 'updated_at') = ($2::jsonb - 'updated_at') FROM users u WHERE id = $1`, m.id, before).Scan(&same); err != nil || !same {
		t.Errorf("a no-op PATCH changed a column other than updated_at (%v)", err)
	}
}

// The answer is built from the row the UPDATE returned, not from the User the
// session middleware read before the write: a field the request did not name
// shows what another writer has since made it.
func TestUpdateMeAnswersFromTheRowItWrote(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1") // the User as read now: time_zone Asia/Jakarta
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE users SET time_zone = 'Asia/Makassar' WHERE id = $1`, m.id); err != nil {
		t.Fatal(err)
	}

	got := h.updatedMe(ctx, t, api.UpdateMeRequest{Locale: ptr(api.IdID)})
	if got.TimeZone != "Asia/Makassar" {
		t.Errorf("time_zone = %q, want Asia/Makassar: the answer was built from the stale User in the context", got.TimeZone)
	}
}

func TestUpdateMeTheNameIsStoredTrimmed(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	got := h.updatedMe(h.ctx(t, m, "198.51.100.1"), t, api.UpdateMeRequest{UserDisplayName: ptr("\t Dewi Putri  \n")})
	if got.UserDisplayName != "Dewi Putri" {
		t.Errorf("user_display_name = %q, want it trimmed", got.UserDisplayName)
	}
}

// The handler's rules, each with the exact (field, rule) it reports, and a
// refusal writes nothing, a valid field beside the bad one included.
func TestUpdateMeHandlerRules(t *testing.T) {
	for _, tc := range []struct {
		name, field, rule string
		body              api.UpdateMeRequest
	}{
		{"blank name", "user_display_name", "required", api.UpdateMeRequest{UserDisplayName: ptr("")}},
		{"whitespace only", "user_display_name", "required", api.UpdateMeRequest{UserDisplayName: ptr(" \t ")}},
		{"zero-width only", "user_display_name", "required", api.UpdateMeRequest{UserDisplayName: ptr("\u200b\u200c\u200d")}},
		{"NUL", "user_display_name", "pattern", api.UpdateMeRequest{UserDisplayName: ptr("De\x00wi")}},
		{"not a time zone", "time_zone", "timezone", api.UpdateMeRequest{TimeZone: ptr("Mars/Base")}},
		{"valid name, bad zone", "time_zone", "timezone", api.UpdateMeRequest{UserDisplayName: ptr("Fine"), TimeZone: ptr("Mars/Base")}},
		{"valid locale, bad zone", "time_zone", "timezone", api.UpdateMeRequest{Locale: ptr(api.IdID), TimeZone: ptr("Mars/Base")}},
		// The least wire field name, then rule, whichever check found it first.
		{"time_zone beats user_display_name", "time_zone", "timezone", api.UpdateMeRequest{UserDisplayName: ptr(" "), TimeZone: ptr("Mars/Base")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			m := h.seedMemberAlone(t, "a@example.com")
			ctx := h.ctx(t, m, "198.51.100.1")
			before := h.userRow(t, m.id)

			assertUpdateMeValidation(t, h.updateMe(ctx, t, tc.body), tc.field, tc.rule)
			if after := h.userRow(t, m.id); after != before {
				t.Errorf("a refused PATCH wrote: %s -> %s", before, after)
			}
		})
	}
}

// Registration and editing run one rule for a User's name: each name goes
// through both and they must agree on whether it is stored, as what, and on the
// rule it breaks. The one difference is deliberate: a blank name is `required`
// when editing, and defaulted to the email's local part at registration, where
// the field is optional.
func TestUpdateMeAndRegistrationAgreeOnEveryName(t *testing.T) {
	for _, tc := range []struct {
		name, input     string
		wantStored      string
		wantRule        string // "" when accepted
		registerDefault bool   // registration substitutes the email's local part
	}{
		{"plain", "Dewi Putri", "Dewi Putri", "", false},
		{"padded", "  Dewi \t", "Dewi", "", false},
		{"255 code points", strings.Repeat("a", 255), strings.Repeat("a", 255), "", false},
		{"255 four-byte code points", strings.Repeat("\U0001F6F6", 255), strings.Repeat("\U0001F6F6", 255), "", false},
		{"zero-width char kept inside a visible name", "Dewi\u200b", "Dewi\u200b", "", false},
		{"bidi control kept inside a visible name", "Dewi \u202e Putri", "Dewi \u202e Putri", "", false},
		{"blank", "", "", "required", true},
		{"spaces", "   \t ", "", "required", true},
		{"zero-width only", "\u200b\u200c\u200d", "", "required", false},
		{"no-break space only", "\u00a0\u00a0", "", "required", true},
		{"word joiner and BOM among spaces", " \u2060 \ufeff ", "", "required", false},
		{"NUL", "De\x00wi", "", "pattern", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, time.Now)

			// Registration. The handshake carries the name the commit stores.
			resp := h.register(ipContext("198.51.100.77"), t, registration{email: "reg@example.com", password: registerPassword, userDisplayName: ptr(tc.input)})
			var regStored, regRule string
			switch r := resp.(type) {
			case api.LocalRegister204Response:
				if err := h.tdb.Pool.QueryRow(context.Background(),
					`SELECT user_display_name FROM onboarding_handshakes`).Scan(&regStored); err != nil {
					t.Fatal(err)
				}
			case api.LocalRegister400JSONResponse:
				args := *r.Args
				if args["field"] != "user_display_name" {
					t.Errorf("registration named field %v, want user_display_name", args["field"])
				}
				regRule = fmt.Sprint(args["rule"])
			default:
				t.Fatalf("registration answered %#v", resp)
			}

			// Editing.
			m := h.seedMemberAlone(t, "a@example.com")
			var editStored, editRule string
			switch r := h.updateMe(h.ctx(t, m, "198.51.100.1"), t, api.UpdateMeRequest{UserDisplayName: ptr(tc.input)}).(type) {
			case api.UpdateMe200JSONResponse:
				editStored = r.UserDisplayName
			case api.UpdateMe400JSONResponse:
				args := *r.Args
				if args["field"] != "user_display_name" {
					t.Errorf("editing named field %v, want user_display_name", args["field"])
				}
				editRule = fmt.Sprint(args["rule"])
			default:
				t.Fatalf("editing answered %#v", r)
			}

			wantRegStored, wantRegRule := tc.wantStored, tc.wantRule
			if tc.registerDefault {
				wantRegStored, wantRegRule = "reg", ""
			}
			if regRule != wantRegRule || editRule != tc.wantRule {
				t.Errorf("rule: registration %q, editing %q; want %q, %q", regRule, editRule, wantRegRule, tc.wantRule)
			}
			if regStored != wantRegStored || editStored != tc.wantStored {
				t.Errorf("stored: registration %q, editing %q; want %q, %q", regStored, editStored, wantRegStored, tc.wantStored)
			}
		})
	}
}

// contract/testdata/time_zones.json is what `time_zone` accepts, and it is read
// through the validator and through the handler, so neither can drift from it.
func TestUpdateMeTimeZonesFollowTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../contract/testdata/time_zones.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Valid   []string `json:"valid"`
		Invalid []string `json:"invalid"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	if len(fx.Valid) == 0 || len(fx.Invalid) == 0 {
		t.Fatal("fixture has no rows")
	}

	v := httperr.Validator()
	for _, z := range fx.Valid {
		if v.Var(z, "timezone") != nil {
			t.Errorf("validator rejects %q, which the fixture accepts", z)
		}
	}
	for _, z := range fx.Invalid {
		if v.Var(z, "timezone") == nil {
			t.Errorf("validator accepts %q, which the fixture refuses", z)
		}
	}

	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")
	for _, z := range fx.Valid {
		if got := h.updatedMe(ctx, t, api.UpdateMeRequest{TimeZone: ptr(z)}).TimeZone; got != z {
			t.Errorf("time_zone = %q, want %q stored as sent", got, z)
		}
	}
	for _, z := range fx.Invalid {
		before := h.userRow(t, m.id)
		assertUpdateMeValidation(t, h.updateMe(ctx, t, api.UpdateMeRequest{TimeZone: ptr(z)}), "time_zone", "timezone")
		if h.userRow(t, m.id) != before {
			t.Errorf("a refused zone %q wrote", z)
		}
	}
}

// No User in the context is the contract's 401, never an UPDATE with a zero id
// (the router's sessionRequired answers first; this is the defensive branch).
func TestUpdateMeNoUserInContextIsUnauthorized(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	before := h.userRow(t, m.id)

	resp := h.updateMe(context.Background(), t, api.UpdateMeRequest{UserDisplayName: ptr("Hijack")})
	got, is := resp.(api.UpdateMe401JSONResponse)
	if !is || got.Code != api.UNAUTHORIZED {
		t.Fatalf("response = %#v, want 401 UNAUTHORIZED", resp)
	}
	if h.userRow(t, m.id) != before {
		t.Error("an unauthenticated PATCH wrote")
	}
}

// A 500 changes nothing. The UPDATE and the Household read the answer needs are
// one transaction, so an outage at its begin, the UPDATE, the read or the commit
// answers 500 INTERNAL, says nothing about the cause, and leaves the users row
// byte for byte as it was. The read and the commit fail with the UPDATE already
// run: were the read outside the transaction, the name would have changed under
// a 500.
func TestUpdateMeAFailureInsideTheTransactionChangesNothing(t *testing.T) {
	for name, fail := range writeThenAnswerFailures {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, time.Now)
			m := h.seedMemberAlone(t, "a@example.com")
			before := h.userRow(t, m.id)
			fail.Beginner = h.tdb.Pool
			h = h.withBeginner(fail)

			resp := h.updateMe(h.ctx(t, m, "198.51.100.1"), t, api.UpdateMeRequest{UserDisplayName: ptr("New")})
			got, is := resp.(api.UpdateMe500JSONResponse)
			if !is {
				t.Fatalf("response = %#v, want 500", resp)
			}
			if got.Code != api.INTERNAL || got.Args != nil {
				t.Errorf("body = %+v, want only the INTERNAL code", got.InternalErrorJSONResponse)
			}
			if h.userRow(t, m.id) != before {
				t.Error("a 500 left the users row changed")
			}
		})
	}
}

// A soft-deleted User is neither updated nor revived: the UPDATE matches no
// row, answered 500 as GetMe answers a User with no Household.
func TestUpdateMeASoftDeletedUserIsNeitherUpdatedNorRevived(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")
	if _, err := h.tdb.Pool.Exec(context.Background(), `UPDATE users SET deleted_at = now() WHERE id = $1`, m.id); err != nil {
		t.Fatal(err)
	}
	before := h.userRow(t, m.id)

	if _, is := h.updateMe(ctx, t, api.UpdateMeRequest{UserDisplayName: ptr("Zombie")}).(api.UpdateMe500JSONResponse); !is {
		t.Fatal("want 500 for a soft-deleted User")
	}
	if h.userRow(t, m.id) != before {
		t.Error("a soft-deleted User was written")
	}
}

// Merge: concurrent PATCHes of different fields all land. A read-modify-write
// writes back the field it read, so two overlapping ones lose each other's;
// one UPDATE with coalesce cannot. Repeated, because a race is a probability.
func TestUpdateMeConcurrentPatchesOfDifferentFieldsAllLand(t *testing.T) {
	h := newHarness(t, time.Now)
	m := h.seedMemberAlone(t, "a@example.com")
	ctx := h.ctx(t, m, "198.51.100.1")

	const rounds = 40
	for round := range rounds {
		name := fmt.Sprintf("Round %d", round)
		locale := []api.Locale{api.IdID, api.EnGB}[round%2]
		zone := []string{"Asia/Makassar", "Asia/Jayapura", "Europe/London", "America/New_York"}[round%4]
		// None of these equals what the row is reset to below, so a field that
		// was lost shows as the reset value.
		resetLocale := []string{"en-GB", "id-ID"}[round%2]
		if _, err := h.tdb.Pool.Exec(context.Background(),
			`UPDATE users SET user_display_name = 'reset', locale = $2, time_zone = 'Asia/Jakarta' WHERE id = $1`, m.id, resetLocale); err != nil {
			t.Fatal(err)
		}
		// The locale PATCH must differ from the reset to be seen.
		if string(locale) == resetLocale {
			locale = map[api.Locale]api.Locale{api.IdID: api.EnGB, api.EnGB: api.IdID}[locale]
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, body := range []api.UpdateMeRequest{
			{UserDisplayName: &name},
			{Locale: &locale},
			{TimeZone: &zone},
		} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				// Not h.updateMe: its Fatalf must not run off the test's goroutine.
				resp, err := h.auth.UpdateMe(ctx, api.UpdateMeRequestObject{Body: &body})
				if _, is := resp.(api.UpdateMe200JSONResponse); err != nil || !is {
					t.Errorf("PATCH %+v = (%#v, %v), want a 200", body, resp, err)
				}
			}()
		}
		close(start)
		wg.Wait()

		var gotName, gotLocale, gotZone string
		if err := h.tdb.Pool.QueryRow(context.Background(),
			`SELECT user_display_name, locale, time_zone FROM users WHERE id = $1`, m.id).Scan(&gotName, &gotLocale, &gotZone); err != nil {
			t.Fatal(err)
		}
		if gotName != name || gotLocale != string(locale) || gotZone != zone {
			t.Fatalf("round %d: row = %q / %q / %q, want %q / %q / %q: a concurrent PATCH lost a field",
				round, gotName, gotLocale, gotZone, name, locale, zone)
		}
	}
}
