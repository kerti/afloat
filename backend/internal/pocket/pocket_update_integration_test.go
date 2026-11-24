package pocket_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// UpdatePocket against a real Postgres, no t.Parallel (ADR-0004).

func (f fixture) update(ctx context.Context, t *testing.T, id uuid.UUID, name *string, active *bool) api.UpdatePocketResponseObject {
	t.Helper()
	resp, err := f.handlers.UpdatePocket(ctx, api.UpdatePocketRequestObject{
		Id:   id,
		Body: &api.UpdatePocketRequest{PocketDisplayName: name, Active: active},
	})
	if err != nil {
		t.Fatalf("UpdatePocket returned an error: %v", err)
	}
	return resp
}

func ptr[T any](v T) *T { return &v }

func assertUpdated(t *testing.T, resp api.UpdatePocketResponseObject) api.Pocket {
	t.Helper()
	got, is := resp.(api.UpdatePocket200JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 200", resp)
	}
	return api.Pocket(got)
}

func assertNotFound(t *testing.T, resp api.UpdatePocketResponseObject) {
	t.Helper()
	got, is := resp.(api.UpdatePocket404JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 404 NOT_FOUND", resp)
	}
	if got.Code != api.NOTFOUND || got.Args != nil {
		t.Errorf("404 = %s %v, want NOT_FOUND with no args", got.Code, got.Args)
	}
}

func assertUpdateStatus(t *testing.T, resp api.UpdatePocketResponseObject, want string) {
	t.Helper()
	if got := updateStatus(resp); got != want {
		t.Errorf("status = %s, want %s", got, want)
	}
}

func updateStatus(resp api.UpdatePocketResponseObject) string {
	switch r := resp.(type) {
	case api.UpdatePocket200JSONResponse:
		return "200"
	case api.UpdatePocket400JSONResponse:
		return "400"
	case api.UpdatePocket401JSONResponse:
		return "401"
	case api.UpdatePocket404JSONResponse:
		return "404 " + string(r.Code)
	case api.UpdatePocket409JSONResponse:
		return "409 " + string(r.Code)
	case api.UpdatePocket500JSONResponse:
		return "500"
	}
	return fmt.Sprintf("%T", resp)
}

func wireOfUpdate(t *testing.T, resp api.UpdatePocketResponseObject) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitUpdatePocketResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func (f fixture) columns(t *testing.T, id uuid.UUID) (name string, active bool, updated pgtype.Timestamptz, created pgtype.Timestamptz) {
	t.Helper()
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT pocket_display_name, active, updated_at, created_at FROM pockets WHERE id = $1`, id).Scan(&name, &active, &updated, &created); err != nil {
		t.Fatalf("read pocket: %v", err)
	}
	return
}

func TestEachFieldUpdatesAndAPartialBodyLeavesTheOtherAndCreatedAtAlone(t *testing.T) {
	f := newFixture(t)
	ctx := f.memberWithKeeper(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	_, _, updated0, created0 := f.columns(t, id)

	got := assertUpdated(t, f.update(ctx, t, id, ptr("  Gopay "), nil))
	if got.PocketDisplayName != "Gopay" || !got.Active || got.Id != id {
		t.Errorf("rename = %+v, want Gopay, still active", got)
	}
	got = assertUpdated(t, f.update(ctx, t, id, nil, ptr(false)))
	if got.PocketDisplayName != "Gopay" || got.Active {
		t.Errorf("deactivate = %+v, want the name kept and active false", got)
	}
	got = assertUpdated(t, f.update(ctx, t, id, nil, ptr(true)))
	if !got.Active {
		t.Errorf("reactivate = %+v, want active", got)
	}
	got = assertUpdated(t, f.update(ctx, t, id, ptr("Dana"), ptr(false)))
	if got.PocketDisplayName != "Dana" || got.Active {
		t.Errorf("both = %+v, want Dana inactive", got)
	}

	name, active, updated, created := f.columns(t, id)
	if name != "Dana" || active {
		t.Errorf("row = %q %v, want Dana false", name, active)
	}
	if created != created0 {
		t.Error("created_at changed")
	}
	if !updated.Time.After(updated0.Time) {
		t.Errorf("updated_at = %v, want later than %v", updated.Time, updated0.Time)
	}
}

// The trap in the brief: a Go bool zero value is not "absent". A deactivation
// is a *bool pointing at false, and it must land.
func TestADeactivationLandsAndIsNotMistakenForAbsent(t *testing.T) {
	f := newFixture(t)
	ctx := f.memberWithKeeper(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))

	assertUpdated(t, f.update(ctx, t, id, nil, ptr(false)))
	if _, active, _, _ := f.columns(t, id); active {
		t.Fatal("active = true after {\"active\": false}")
	}
	// Repeating it is 200 and unchanged; so is an empty body.
	if got := assertUpdated(t, f.update(ctx, t, id, nil, ptr(false))); got.Active {
		t.Errorf("repeat = %+v", got)
	}
	if got := assertUpdated(t, f.update(ctx, t, id, nil, nil)); got.Active || got.PocketDisplayName != "OVO" {
		t.Errorf("empty body = %+v, want the Pocket as it stands", got)
	}
}

func TestAnInactivePocketCanBeRenamedAndStaysInTheList(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	first, second := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, first, "Zeta"))
	assertCreated(t, f.create(ctx, t, second, "Alpha"))

	assertUpdated(t, f.update(ctx, t, first, nil, ptr(false)))
	assertUpdated(t, f.update(ctx, t, first, ptr("Aardvark"), nil))

	got := f.list(ctx, t)
	want := []api.Pocket{
		{Id: first, PocketDisplayName: "Aardvark", Active: false},
		{Id: second, PocketDisplayName: "Alpha", Active: true},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("list = %+v, want %+v (inactive kept, creation order unmoved)", got, want)
	}
}

// ---- the name rule ---------------------------------------------------------

func TestRenameAppliesTheSharedNameRule(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	before := f.row(t, id)

	for _, tc := range []struct {
		name, input, rule string
	}{
		{"blank", "   ", "required"},
		{"invisible only", "\u200b\u200d", "required"},
		{"61 code points", strings.Repeat("a", 61), "max"},
		{"a NUL", "O\x00VO", "pattern"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := f.update(ctx, t, id, ptr(tc.input), ptr(false))
			got, is := resp.(api.UpdatePocket400JSONResponse)
			if !is || got.Code != api.VALIDATION {
				t.Fatalf("response = %#v, want 400 VALIDATION", resp)
			}
			if args := *got.Args; args["field"] != "pocket_display_name" || args["rule"] != tc.rule {
				t.Errorf("args = %v, want pocket_display_name %s", args, tc.rule)
			}
			if after := f.row(t, id); after != before {
				t.Errorf("a refused update changed the row (the active half of the body included):\n%s\n%s", before, after)
			}
		})
	}
	// Exactly 60 code points is fine, as is a Cf character inside a visible name.
	assertUpdated(t, f.update(ctx, t, id, ptr(strings.Repeat("é", 60)), nil))
	assertUpdated(t, f.update(ctx, t, id, ptr("O\u200bVO"), nil))
}

func TestRenameIntoAHeldNameIsNameTakenAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, foreign := f.member(t, "Other", "b@example.com")
	mine, held, inactive, deleted := newID(t), newID(t), newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, mine, "OVO"))
	assertCreated(t, f.create(ctx, t, held, "Gopay"))
	assertCreated(t, f.create(ctx, t, inactive, "Dana"))
	assertCreated(t, f.create(ctx, t, deleted, "Cash"))
	assertCreated(t, f.create(foreign, t, newID(t), "Theirs"))
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, inactive)
	f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, deleted)
	before := f.row(t, mine)

	t.Run("another live Pocket's name, in another case and padded", func(t *testing.T) {
		assertUpdateStatus(t, f.update(ctx, t, mine, ptr("  gOPAY "), ptr(false)), "409 NAME_TAKEN")
	})
	t.Run("an inactive Pocket still holds its name", func(t *testing.T) {
		assertUpdateStatus(t, f.update(ctx, t, mine, ptr("dana"), nil), "409 NAME_TAKEN")
	})
	if after := f.row(t, mine); after != before {
		t.Errorf("a refused rename changed the row:\n%s\n%s", before, after)
	}
	t.Run("its own name in another case is not a clash", func(t *testing.T) {
		if got := assertUpdated(t, f.update(ctx, t, mine, ptr("ovo"), nil)); got.PocketDisplayName != "ovo" {
			t.Errorf("name = %q", got.PocketDisplayName)
		}
	})
	t.Run("a deleted Pocket's name is free", func(t *testing.T) {
		assertUpdated(t, f.update(ctx, t, mine, ptr("cash"), nil))
	})
	t.Run("another Household's name is free", func(t *testing.T) {
		assertUpdated(t, f.update(ctx, t, mine, ptr("Theirs"), nil))
	})
	// Reactivating never touches the name index.
	t.Run("reactivating an inactive Pocket is not a name event", func(t *testing.T) {
		assertUpdated(t, f.update(ctx, t, inactive, nil, ptr(true)))
	})
}

// ---- tenancy and NOT_FOUND -------------------------------------------------

// An id that is unknown, another Household's or deleted answers one NOT_FOUND,
// the same bytes, for every body (`{}` included: a no-op must not confirm
// another Household's id), and writes nothing. Proven red against an UPDATE
// whose WHERE lost household_id (see the PR).
func TestUnknownForeignAndDeletedIdsAreOneNotFound(t *testing.T) {
	f := newFixture(t)
	_, alice := f.member(t, "Alice", "a@example.com")
	_, bob := f.member(t, "Bob", "b@example.com")
	theirs, theirsInactive, mineDeleted := newID(t), newID(t), newID(t)
	assertCreated(t, f.create(bob, t, theirs, "OVO"))
	assertCreated(t, f.create(bob, t, theirsInactive, "Dana"))
	assertCreated(t, f.create(alice, t, mineDeleted, "Gone"))
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, theirsInactive)
	f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, mineDeleted)
	snapshot := func() []string { return []string{f.row(t, theirs), f.row(t, theirsInactive), f.row(t, mineDeleted)} }
	before := snapshot()

	ids := map[string]uuid.UUID{
		"unknown":                  newID(t),
		"another Household's":      theirs,
		"another's, inactive":      theirsInactive,
		"the caller's own deleted": mineDeleted,
	}
	for _, body := range []struct {
		name       string
		pocketName *string
		active     *bool
	}{
		{"a rename", ptr("Mine"), nil},
		{"a deactivation", nil, ptr(false)},
		{"a reactivation", nil, ptr(true)},
		{"an empty body", nil, nil},
	} {
		t.Run(body.name, func(t *testing.T) {
			var wantStatus int
			var wantBody string
			for kind, id := range ids {
				resp := f.update(alice, t, id, body.pocketName, body.active)
				assertNotFound(t, resp)
				status, bytes := wireOfUpdate(t, resp)
				if wantBody == "" {
					wantStatus, wantBody = status, bytes
				}
				if status != wantStatus || bytes != wantBody {
					t.Errorf("%s answers %d %s, others %d %s", kind, status, bytes, wantStatus, wantBody)
				}
			}
		})
	}
	after := snapshot()
	for i := range before {
		if after[i] != before[i] {
			t.Errorf("row %d changed:\n%s\n%s", i, before[i], after[i])
		}
	}
	// Bob still sees exactly his own, untouched.
	if got := f.list(bob, t); len(got) != 2 || got[0].PocketDisplayName != "OVO" || !got[0].Active || got[1].Active {
		t.Errorf("Bob's list = %+v, want his two Pockets as they were", got)
	}
}

// A refused rename that would clash with a Household's name is not a way to
// learn another Household's names: the name index is per Household.
func TestAForeignIdNeverReachesTheNameIndex(t *testing.T) {
	f := newFixture(t)
	_, alice := f.member(t, "Alice", "a@example.com")
	_, bob := f.member(t, "Bob", "b@example.com")
	assertCreated(t, f.create(alice, t, newID(t), "OVO"))
	theirs := newID(t)
	assertCreated(t, f.create(bob, t, theirs, "Gopay"))

	// Alice renaming Bob's Pocket to a name Alice holds: 404, not 409.
	assertNotFound(t, f.update(alice, t, theirs, ptr("OVO"), nil))
}

// PRD S2: any member may change any Pocket, and sees the other's edit.
func TestTwoUsersInOneHouseholdEditTheSamePocket(t *testing.T) {
	f := newFixture(t)
	householdID, first := f.member(t, "Home", "a@example.com")
	f.tdb.CreatePocket(t, householdID, "Keeper")
	second := f.userIn(t, householdID, "b@example.com")
	id := newID(t)
	assertCreated(t, f.create(first, t, id, "OVO"))

	assertUpdated(t, f.update(second, t, id, ptr("Dana"), ptr(false)))
	if got := f.list(first, t); len(got) != 2 || got[1].PocketDisplayName != "Dana" || got[1].Active {
		t.Errorf("first User sees %+v, want the second's edit", got)
	}
}

func TestAnUpdateWithoutAUserIs401AndWritesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	before := f.row(t, id)

	assertUpdateStatus(t, f.update(context.Background(), t, id, ptr("Dana"), ptr(false)), "401")
	if after := f.row(t, id); after != before {
		t.Errorf("row changed:\n%s\n%s", before, after)
	}
}

func TestAnUpdateWithoutABodyIs400(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	resp, err := f.handlers.UpdatePocket(ctx, api.UpdatePocketRequestObject{Id: newID(t)})
	if err != nil {
		t.Fatalf("UpdatePocket: %v", err)
	}
	if got, is := resp.(api.UpdatePocket400JSONResponse); !is || got.Code != api.INVALIDJSONBODY {
		t.Fatalf("response = %#v, want 400 INVALID_JSON_BODY", resp)
	}
}

// ---- concurrency -----------------------------------------------------------

// Concurrent PATCHes of the name and of active both land. Proven red against a
// read-modify-write implementation, which loses one of the two (see the PR).
func TestConcurrentPatchesOfDifferentFieldsBothLand(t *testing.T) {
	f := newFixture(t)
	ctx := f.memberWithKeeper(t, "Home", "a@example.com")

	for round := range 20 {
		id := newID(t)
		assertCreated(t, f.create(ctx, t, id, fmt.Sprintf("OVO %d", round)))
		renamed := fmt.Sprintf("Renamed %d", round)
		results := race(2, func(i int) string {
			if i == 0 {
				return updateStatus(f.update(ctx, t, id, &renamed, nil))
			}
			return updateStatus(f.update(ctx, t, id, nil, ptr(false)))
		})
		if results[0] != "200" || results[1] != "200" {
			t.Fatalf("round %d: statuses = %v, want 200 and 200", round, results)
		}
		name, active, _, _ := f.columns(t, id)
		if name != renamed || active {
			t.Fatalf("round %d: row = %q active=%v, want %q inactive: a write was lost", round, name, active, renamed)
		}
	}
}

// Two renames into one name: one wins, the other is NAME_TAKEN, never a 500.
func TestConcurrentRenamesIntoOneNameAnswerOneUpdateAndOneNameTaken(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")

	for round := range 8 {
		a, b := newID(t), newID(t)
		assertCreated(t, f.create(ctx, t, a, fmt.Sprintf("A %d", round)))
		assertCreated(t, f.create(ctx, t, b, fmt.Sprintf("B %d", round)))
		target := fmt.Sprintf("Target %d", round)
		results := race(2, func(i int) string {
			id, name := a, target
			if i == 1 {
				id, name = b, strings.ToUpper(target)
			}
			return updateStatus(f.update(ctx, t, id, &name, nil))
		})
		counts := map[string]int{}
		for _, r := range results {
			counts[r]++
		}
		if counts["200"] != 1 || counts["409 NAME_TAKEN"] != 1 {
			t.Fatalf("round %d: statuses = %v, want one 200 and one 409 NAME_TAKEN", round, counts)
		}
	}
}

// ---- failure ---------------------------------------------------------------

type updateBreakable struct {
	db.Querier
	update func(context.Context, db.UpdatePocketParams) (db.Pocket, error)
}

func (b updateBreakable) UpdatePocket(ctx context.Context, p db.UpdatePocketParams) (db.Pocket, error) {
	return b.update(ctx, p)
}

func TestAnUpdateErrorThatIsNotNoRowsOrTheNameIndexIsA500(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"an outage", errOutage, "500"},
		{"a 23505 on another index", &pgconn.PgError{Code: "23505", ConstraintName: "something_else_key"}, "500"},
		{"a 23505 on the primary key", &pgconn.PgError{Code: "23505", ConstraintName: "pockets_pkey"}, "500"},
		{"a 23505 on the name index", &pgconn.PgError{Code: "23505", ConstraintName: "pockets_household_name_idx"}, "409 NAME_TAKEN"},
		{"ErrNoRows", pgx.ErrNoRows, "404 NOT_FOUND"},
		{"ErrNoRows wrapped", fmt.Errorf("scan: %w", pgx.ErrNoRows), "404 NOT_FOUND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, func(q db.Querier) db.Querier {
				return updateBreakable{Querier: q, update: func(context.Context, db.UpdatePocketParams) (db.Pocket, error) {
					return db.Pocket{}, tc.err
				}}
			})
			_, ctx := f.member(t, "Home", "a@example.com")
			assertUpdateStatus(t, f.update(ctx, t, newID(t), ptr("OVO"), nil), tc.want)
		})
	}
}

// What reaches the statement is the User's Household and the request's id,
// trimmed name and active, and nothing else. A request that leaves the Pocket
// active is the one statement; `active: false` runs in a transaction and is
// checked in pocket_last_integration_test.go.
func TestTheStatementCarriesTheSessionsHouseholdAndTheTrimmedName(t *testing.T) {
	var got db.UpdatePocketParams
	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return updateBreakable{Querier: q, update: func(_ context.Context, p db.UpdatePocketParams) (db.Pocket, error) {
			got = p
			return db.Pocket{}, pgx.ErrNoRows
		}}
	})
	householdID, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	f.update(ctx, t, id, ptr("  OVO "), ptr(true))

	if got.HouseholdID != householdID {
		t.Errorf("household_id = %v, want the caller's %v", got.HouseholdID, householdID)
	}
	if got.ID.Bytes != id {
		t.Errorf("id = %v, want %v", got.ID.Bytes, id)
	}
	if got.PocketDisplayName == nil || *got.PocketDisplayName != "OVO" {
		t.Errorf("name = %v, want the trimmed OVO", got.PocketDisplayName)
	}
	if got.Active == nil || !*got.Active {
		t.Errorf("active = %v, want a pointer to true", got.Active)
	}
}

// ADR-0003: a Pocket's name is user content, and nothing logs a body.
func TestAnUpdateFailureLogsNoPocketName(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return updateBreakable{Querier: q, update: func(context.Context, db.UpdatePocketParams) (db.Pocket, error) {
			return db.Pocket{}, errOutage
		}}
	})
	_, ctx := f.member(t, "Home", "a@example.com")
	assertUpdateStatus(t, f.update(ctx, t, newID(t), ptr("Secret Savings Name"), nil), "500")

	if buf.Len() == 0 {
		t.Fatal("nothing was logged; the 500 should leave a trace")
	}
	if strings.Contains(buf.String(), "Secret Savings Name") {
		t.Errorf("a Pocket's name reached the log: %s", buf.String())
	}
}
