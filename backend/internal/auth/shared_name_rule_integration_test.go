package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/household"
	"github.com/kerti/afloat/backend/internal/pocket"
)

// One table of names, run through every surface that names a Household or a
// Pocket: founding, editing the Household, creating a Pocket. They share
// auth.CheckBoundedName (BOOTSTRAP.md §4's Names bullet), so none can store a
// name another would refuse. A surface that grew its own rule would split from
// the others on one of these rows.
func TestOneNameRuleAcrossFoundingHouseholdEditAndPocketCreate(t *testing.T) {
	h := newHarness(t, time.Now)

	other := h.tdb.CreateHousehold(t, "Existing")
	user, err := h.tdb.Queries.GetUserByID(context.Background(), h.tdb.CreateUser(t, other, "member@example.com", "M"))
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	member := auth.WithUser(context.Background(), user)
	households := household.New(household.Deps{Querier: h.tdb.Queries})
	pockets := pocket.New(pocket.Deps{Querier: h.tdb.Queries})

	sixty := strings.Repeat("\U0001F6F6", 60)
	for i, tc := range []struct {
		name, input, stored, rule string
	}{
		{"plain", "Rumah", "Rumah", ""},
		{"trimmed", "  Rumah  ", "Rumah", ""},
		{"60 code points of 4 bytes", sixty, sixty, ""},
		{"60 code points, padded", " " + sixty + " ", sixty, ""},
		{"an invisible character inside a visible name is kept", "Rumah\u200b \u202e Dewi", "Rumah\u200b \u202e Dewi", ""},
		{"empty", "", "", "required"},
		{"spaces", "   \t ", "", "required"},
		{"no-break space only", "\u00a0\u00a0", "", "required"},
		{"zero-width characters only", "\u200b\u200c\u200d", "\u200b\u200c\u200d", "required"},
		{"word joiner and BOM among spaces", " \u2060 \ufeff ", "\u2060 \ufeff", "required"},
		{"61 code points", strings.Repeat("a", 61), "", "max"},
		{"61 code points of 4 bytes", strings.Repeat("\U0001F6F6", 61), "", "max"},
		{"61 code points once padded", " " + strings.Repeat("a", 61) + " ", "", "max"},
		{"NUL", "Ru\x00mah", "", "pattern"},
		{"NUL beyond the cap is the cap's", strings.Repeat("a", 61) + "\x00", "", "max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The same name in two rows would clash on a Pocket, so each row
			// starts with none.
			_, err := h.tdb.Pool.Exec(context.Background(), `DELETE FROM pockets`)
			if err != nil {
				t.Fatalf("clear pockets: %v", err)
			}

			foundGot, foundRule := found(t, h, i, tc.input)
			editGot, editRule := edit(member, t, households, tc.input)
			createGot, createRule := create(member, t, pockets, tc.input)

			for surface, rule := range map[string]string{"founding": foundRule, "household edit": editRule, "pocket create": createRule} {
				if rule != tc.rule {
					t.Errorf("%s: rule = %q, want %q", surface, rule, tc.rule)
				}
			}
			if tc.rule != "" {
				return
			}
			for surface, got := range map[string]string{"founding": foundGot, "household edit": editGot, "pocket create": createGot} {
				if got != tc.stored {
					t.Errorf("%s: stored %q, want %q", surface, got, tc.stored)
				}
			}
		})
	}
}

// found runs the commit and returns the stored name or the rule refused.
func found(t *testing.T, h harness, i int, input string) (stored, rule string) {
	t.Helper()
	ctx, _ := h.atTheGate(t, fmt.Sprintf("198.51.100.%d", 100+i), fmt.Sprintf("founder%d@example.com", i))
	resp := h.commit(ctx, t, true, input)
	switch r := resp.(type) {
	case api.CommitOnboardingChoice400JSONResponse:
		return "", ruleOf(t, "household_display_name", r.Args)
	default:
		rec := visit(t, resp)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("founding: status %d (%s)", rec.Code, rec.Body.String())
		}
		if err := h.tdb.Pool.QueryRow(context.Background(),
			`SELECT household_display_name FROM households h JOIN users u ON u.household_id = h.id WHERE u.email = $1`,
			fmt.Sprintf("founder%d@example.com", i)).Scan(&stored); err != nil {
			t.Fatalf("read founded household: %v", err)
		}
		return stored, ""
	}
}

func edit(ctx context.Context, t *testing.T, h *household.Handlers, input string) (stored, rule string) {
	t.Helper()
	resp, err := h.UpdateHousehold(ctx, api.UpdateHouseholdRequestObject{Body: &api.UpdateHouseholdRequest{HouseholdDisplayName: &input}})
	if err != nil {
		t.Fatalf("UpdateHousehold: %v", err)
	}
	switch r := resp.(type) {
	case api.UpdateHousehold200JSONResponse:
		return r.HouseholdDisplayName, ""
	case api.UpdateHousehold400JSONResponse:
		return "", ruleOf(t, "household_display_name", r.Args)
	}
	t.Fatalf("household edit: %T", resp)
	return "", ""
}

func create(ctx context.Context, t *testing.T, p *pocket.Handlers, input string) (stored, rule string) {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	resp, err := p.CreatePocket(ctx, api.CreatePocketRequestObject{Body: &api.CreatePocketRequest{Id: id, PocketDisplayName: input}})
	if err != nil {
		t.Fatalf("CreatePocket: %v", err)
	}
	switch r := resp.(type) {
	case api.CreatePocket201JSONResponse:
		return r.PocketDisplayName, ""
	case api.CreatePocket400JSONResponse:
		return "", ruleOf(t, "pocket_display_name", r.Args)
	}
	t.Fatalf("pocket create: %T", resp)
	return "", ""
}

func ruleOf(t *testing.T, field string, args *map[string]interface{}) string {
	t.Helper()
	if args == nil || (*args)["field"] != field {
		t.Fatalf("args = %v, want field %s", args, field)
	}
	return (*args)["rule"].(string)
}
