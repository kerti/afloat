package category_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// UpdateCategory against a real Postgres, no t.Parallel (ADR-0004): the table of
// UpdatePocket's rows, plus what only a Category has, an allocation that a
// rename or a deactivation must never move and a bucket that can never change.

func (f fixture) update(ctx context.Context, t *testing.T, id uuid.UUID, name *string, active *bool) api.UpdateCategoryResponseObject {
	t.Helper()
	resp, err := f.handlers.UpdateCategory(ctx, api.UpdateCategoryRequestObject{
		Id:   id,
		Body: &api.UpdateCategoryRequest{CategoryDisplayName: name, Active: active},
	})
	if err != nil {
		t.Fatalf("UpdateCategory returned an error: %v", err)
	}
	return resp
}

func assertUpdated(t *testing.T, resp api.UpdateCategoryResponseObject) api.Category {
	t.Helper()
	got, is := resp.(api.UpdateCategory200JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 200", resp)
	}
	return api.Category(got)
}

func assertNotFound(t *testing.T, resp api.UpdateCategoryResponseObject) {
	t.Helper()
	got, is := resp.(api.UpdateCategory404JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 404 NOT_FOUND", resp)
	}
	if got.Code != api.NOTFOUND || got.Args != nil {
		t.Errorf("404 = %s %v, want NOT_FOUND with no args", got.Code, got.Args)
	}
}

func assertUpdateStatus(t *testing.T, resp api.UpdateCategoryResponseObject, want string) {
	t.Helper()
	if got := updateStatus(resp); got != want {
		t.Errorf("status = %s, want %s", got, want)
	}
}

func updateStatus(resp api.UpdateCategoryResponseObject) string {
	switch r := resp.(type) {
	case api.UpdateCategory200JSONResponse:
		return "200"
	case api.UpdateCategory400JSONResponse:
		return "400"
	case api.UpdateCategory401JSONResponse:
		return "401"
	case api.UpdateCategory404JSONResponse:
		return "404 " + string(r.Code)
	case api.UpdateCategory409JSONResponse:
		return "409 " + string(r.Code)
	case api.UpdateCategory500JSONResponse:
		return "500"
	}
	return fmt.Sprintf("%T", resp)
}

func wireOfUpdate(t *testing.T, resp api.UpdateCategoryResponseObject) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitUpdateCategoryResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func TestEachFieldUpdatesAndAPartialBodyLeavesTheOtherTheBucketAndCreatedAtAlone(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Rent", api.Commitment, ptr("3000000")))
	var updated0, created0 string
	updated0 = f.scalar(t, `SELECT updated_at::text FROM categories WHERE id = $1`, id)
	created0 = f.scalar(t, `SELECT created_at::text FROM categories WHERE id = $1`, id)

	got := assertUpdated(t, f.update(ctx, t, id, ptr("  Housing "), nil))
	if got.CategoryDisplayName != "Housing" || !got.Active || got.Id != id || got.Bucket != api.Commitment {
		t.Errorf("rename = %+v, want Housing, still active, still a Commitment", got)
	}
	got = assertUpdated(t, f.update(ctx, t, id, nil, ptr(false)))
	if got.CategoryDisplayName != "Housing" || got.Active {
		t.Errorf("deactivate = %+v, want the name kept and active false", got)
	}
	got = assertUpdated(t, f.update(ctx, t, id, nil, ptr(true)))
	if !got.Active {
		t.Errorf("reactivate = %+v, want active", got)
	}
	got = assertUpdated(t, f.update(ctx, t, id, ptr("Home"), ptr(false)))
	if got.CategoryDisplayName != "Home" || got.Active || got.Bucket != api.Commitment {
		t.Errorf("both = %+v, want Home inactive", got)
	}

	if created := f.scalar(t, `SELECT created_at::text FROM categories WHERE id = $1`, id); created != created0 {
		t.Error("created_at changed")
	}
	if updated := f.scalar(t, `SELECT updated_at::text FROM categories WHERE id = $1`, id); updated == updated0 {
		t.Errorf("updated_at = %v, want it moved", updated)
	}
}

// The trap in the brief: a Go bool zero value is not "absent". A deactivation
// is a *bool pointing at false, and it must land.
func TestADeactivationLandsAndIsNotMistakenForAbsent(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))

	assertUpdated(t, f.update(ctx, t, id, nil, ptr(false)))
	if f.scalar(t, `SELECT active::text FROM categories WHERE id = $1`, id) != "false" {
		t.Fatal("active = true after {\"active\": false}")
	}
	if got := assertUpdated(t, f.update(ctx, t, id, nil, ptr(false))); got.Active {
		t.Errorf("repeat = %+v", got)
	}
	if got := assertUpdated(t, f.update(ctx, t, id, nil, nil)); got.Active || got.CategoryDisplayName != "Food" {
		t.Errorf("empty body = %+v, want the Category as it stands", got)
	}
}

// PRD S8 and #130 C8: a rename and a deactivation change a label and a flag,
// never a figure. A retired Commitment keeps reducing the Daily Pool until its
// allocation is set to 0.
func TestARenameAndADeactivationMoveNoFigureAndNoAllocation(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	rent, food := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(ctx, t, food, "Food", api.Daily, ptr("2000000")))
	if _, err := f.budget.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{MonthlyBudget: ptr("9000000")}}); err != nil {
		t.Fatal(err)
	}
	budgetBefore := f.getBudget(ctx, t)
	allocationsBefore := f.allocationRows(t)

	assertUpdated(t, f.update(ctx, t, rent, ptr("Housing"), ptr(false)))
	assertUpdated(t, f.update(ctx, t, food, nil, ptr(false)))
	assertUpdated(t, f.update(ctx, t, food, ptr("Groceries"), ptr(true)))

	before, _ := json.Marshal(budgetBefore)
	after, _ := json.Marshal(f.getBudget(ctx, t))
	if !bytes.Equal(before, after) {
		t.Errorf("a rename or a deactivation moved the Budget:\n%s\n%s", before, after)
	}
	if now := f.allocationRows(t); now != allocationsBefore {
		t.Errorf("a rename or a deactivation wrote an allocation:\n%s\n%s", allocationsBefore, now)
	}
}

func TestAnInactiveCategoryCanBeRenamedAndStaysInTheListInCreationOrder(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	first, second := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, first, "Zeta", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, second, "Alpha", api.Commitment, ptr("1")))

	assertUpdated(t, f.update(ctx, t, first, nil, ptr(false)))
	assertUpdated(t, f.update(ctx, t, first, ptr("Aardvark"), nil))

	got := f.list(ctx, t)
	want := []api.Category{
		{Id: first, CategoryDisplayName: "Aardvark", Bucket: api.Daily, Active: false},
		{Id: second, CategoryDisplayName: "Alpha", Bucket: api.Commitment, Active: true},
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
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))
	before := f.row(t, id)

	for _, tc := range []struct {
		name, input, rule string
	}{
		{"blank", "   ", "required"},
		{"invisible only", "\u200b\u200d", "required"},
		{"61 code points", strings.Repeat("a", 61), "max"},
		{"a NUL", "Fo\x00od", "pattern"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := f.update(ctx, t, id, ptr(tc.input), ptr(false))
			got, is := resp.(api.UpdateCategory400JSONResponse)
			if !is || got.Code != api.VALIDATION {
				t.Fatalf("response = %#v, want 400 VALIDATION", resp)
			}
			if args := *got.Args; args["field"] != "category_display_name" || args["rule"] != tc.rule {
				t.Errorf("args = %v, want category_display_name %s", args, tc.rule)
			}
			if after := f.row(t, id); after != before {
				t.Errorf("a refused update changed the row (the active half of the body included):\n%s\n%s", before, after)
			}
		})
	}
	// Exactly 60 code points is fine, as is a Cf character inside a visible name.
	assertUpdated(t, f.update(ctx, t, id, ptr(strings.Repeat("é", 60)), nil))
	assertUpdated(t, f.update(ctx, t, id, ptr("Fo\u200bod"), nil))
}

// Names are unique across both buckets (#130 C7), active or inactive, and a
// deleted Category releases its own.
func TestRenameIntoAHeldNameIsNameTakenAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, foreign := f.member(t, "Other", "b@example.com")
	mine, held, inactive, deleted := newID(t), newID(t), newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, mine, "Food", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, held, "Rent", api.Commitment, ptr("1")))
	assertCreated(t, f.create(ctx, t, inactive, "Fun", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, deleted, "Cash", api.Daily, nil))
	assertCreated(t, f.create(foreign, t, newID(t), "Theirs", api.Daily, nil))
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, inactive)
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, deleted)
	before := f.row(t, mine)

	t.Run("another live Category's name, in the other bucket, another case and padded", func(t *testing.T) {
		assertUpdateStatus(t, f.update(ctx, t, mine, ptr("  rENT "), ptr(false)), "409 NAME_TAKEN")
	})
	t.Run("an inactive Category still holds its name", func(t *testing.T) {
		assertUpdateStatus(t, f.update(ctx, t, mine, ptr("fun"), nil), "409 NAME_TAKEN")
	})
	if after := f.row(t, mine); after != before {
		t.Errorf("a refused rename changed the row:\n%s\n%s", before, after)
	}
	t.Run("its own name in another case is not a clash", func(t *testing.T) {
		if got := assertUpdated(t, f.update(ctx, t, mine, ptr("food"), nil)); got.CategoryDisplayName != "food" {
			t.Errorf("name = %q", got.CategoryDisplayName)
		}
	})
	t.Run("a deleted Category's name is free", func(t *testing.T) {
		assertUpdated(t, f.update(ctx, t, mine, ptr("cash"), nil))
	})
	t.Run("another Household's name is free", func(t *testing.T) {
		assertUpdated(t, f.update(ctx, t, mine, ptr("Theirs"), nil))
	})
	t.Run("reactivating an inactive Category is not a name event", func(t *testing.T) {
		assertUpdated(t, f.update(ctx, t, inactive, nil, ptr(true)))
	})
}

// ---- tenancy and NOT_FOUND -------------------------------------------------

// An id that is unknown, another Household's or deleted answers one NOT_FOUND,
// the same bytes, for every body (`{}` included: a no-op must not confirm
// another Household's id), and writes nothing. Proven red against an UPDATE
// whose WHERE lost household_id.
func TestUnknownForeignAndDeletedIdsAreOneNotFound(t *testing.T) {
	f := newFixture(t)
	_, alice := f.member(t, "Alice", "a@example.com")
	_, bob := f.member(t, "Bob", "b@example.com")
	theirs, theirsInactive, mineDeleted := newID(t), newID(t), newID(t)
	assertCreated(t, f.create(bob, t, theirs, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(bob, t, theirsInactive, "Fun", api.Daily, nil))
	assertCreated(t, f.create(alice, t, mineDeleted, "Gone", api.Daily, nil))
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, theirsInactive)
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, mineDeleted)
	snapshot := func() []string {
		return []string{f.row(t, theirs), f.row(t, theirsInactive), f.row(t, mineDeleted), f.allocationRows(t)}
	}
	before := snapshot()

	ids := map[string]uuid.UUID{
		"unknown":                  newID(t),
		"another Household's":      theirs,
		"another's, inactive":      theirsInactive,
		"the caller's own deleted": mineDeleted,
	}
	for _, body := range []struct {
		name         string
		categoryName *string
		active       *bool
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
				resp := f.update(alice, t, id, body.categoryName, body.active)
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
			t.Errorf("state %d changed:\n%s\n%s", i, before[i], after[i])
		}
	}
	if got := f.list(bob, t); len(got) != 2 || got[0].CategoryDisplayName != "Rent" || !got[0].Active || got[1].Active {
		t.Errorf("Bob's list = %+v, want his two Categories as they were", got)
	}
}

// A refused rename that would clash with a Household's name is not a way to
// learn another Household's names: the name index is per Household.
func TestAForeignIdNeverReachesTheNameIndex(t *testing.T) {
	f := newFixture(t)
	_, alice := f.member(t, "Alice", "a@example.com")
	_, bob := f.member(t, "Bob", "b@example.com")
	assertCreated(t, f.create(alice, t, newID(t), "Food", api.Daily, nil))
	theirs := newID(t)
	assertCreated(t, f.create(bob, t, theirs, "Fun", api.Daily, nil))

	assertNotFound(t, f.update(alice, t, theirs, ptr("Food"), nil))
}

// PRD S2: any member may change any Category, and sees the other's edit.
func TestTwoUsersInOneHouseholdEditTheSameCategory(t *testing.T) {
	f := newFixture(t)
	householdID, first := f.member(t, "Home", "a@example.com")
	second := f.userIn(t, householdID, "b@example.com")
	id := newID(t)
	assertCreated(t, f.create(first, t, id, "Food", api.Daily, nil))

	assertUpdated(t, f.update(second, t, id, ptr("Groceries"), ptr(false)))
	if got := f.list(first, t); len(got) != 1 || got[0].CategoryDisplayName != "Groceries" || got[0].Active {
		t.Errorf("first User sees %+v, want the second's edit", got)
	}
}

func TestAnUpdateWithoutAUserIs401AndWritesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Food", api.Daily, nil))
	before := f.row(t, id)

	assertUpdateStatus(t, f.update(context.Background(), t, id, ptr("Groceries"), ptr(false)), "401")
	if after := f.row(t, id); after != before {
		t.Errorf("row changed:\n%s\n%s", before, after)
	}
}

func TestAnUpdateWithoutABodyIs400(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	resp, err := f.handlers.UpdateCategory(ctx, api.UpdateCategoryRequestObject{Id: newID(t)})
	if err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	if got, is := resp.(api.UpdateCategory400JSONResponse); !is || got.Code != api.INVALIDJSONBODY {
		t.Fatalf("response = %#v, want 400 INVALID_JSON_BODY", resp)
	}
}

// ---- concurrency -----------------------------------------------------------

// Concurrent PATCHes of the name and of active both land. Proven red against a
// read-modify-write implementation, which loses one of the two.
func TestConcurrentPatchesOfDifferentFieldsBothLand(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")

	for round := range 20 {
		id := newID(t)
		assertCreated(t, f.create(ctx, t, id, fmt.Sprintf("Food %d", round), api.Daily, nil))
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
		name := f.scalar(t, `SELECT category_display_name FROM categories WHERE id = $1`, id)
		active := f.scalar(t, `SELECT active::text FROM categories WHERE id = $1`, id)
		if name != renamed || active != "false" {
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
		assertCreated(t, f.create(ctx, t, a, fmt.Sprintf("A %d", round), api.Daily, nil))
		assertCreated(t, f.create(ctx, t, b, fmt.Sprintf("B %d", round), api.Commitment, ptr("1")))
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
	update func(context.Context, db.UpdateCategoryParams) (db.Category, error)
}

func (b updateBreakable) UpdateCategory(ctx context.Context, p db.UpdateCategoryParams) (db.Category, error) {
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
		{"a 23505 on the primary key", &pgconn.PgError{Code: "23505", ConstraintName: "categories_pkey"}, "500"},
		{"a 23505 on the name index", &pgconn.PgError{Code: "23505", ConstraintName: "categories_household_name_idx"}, "409 NAME_TAKEN"},
		{"ErrNoRows", pgx.ErrNoRows, "404 NOT_FOUND"},
		{"ErrNoRows wrapped", fmt.Errorf("scan: %w", pgx.ErrNoRows), "404 NOT_FOUND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, func(q db.Querier) db.Querier {
				return updateBreakable{Querier: q, update: func(context.Context, db.UpdateCategoryParams) (db.Category, error) {
					return db.Category{}, tc.err
				}}
			}, nil)
			_, ctx := f.member(t, "Home", "a@example.com")
			assertUpdateStatus(t, f.update(ctx, t, newID(t), ptr("Food"), nil), tc.want)
		})
	}
}

// What reaches the statement is the User's Household and the request's id,
// trimmed name and active, and nothing else.
func TestTheStatementCarriesTheSessionsHouseholdAndTheTrimmedName(t *testing.T) {
	var got db.UpdateCategoryParams
	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return updateBreakable{Querier: q, update: func(_ context.Context, p db.UpdateCategoryParams) (db.Category, error) {
			got = p
			return db.Category{}, pgx.ErrNoRows
		}}
	}, nil)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	f.update(ctx, t, id, ptr("  Food "), ptr(false))

	if got.HouseholdID != householdID {
		t.Errorf("household_id = %v, want the caller's %v", got.HouseholdID, householdID)
	}
	if got.ID.Bytes != id {
		t.Errorf("id = %v, want %v", got.ID.Bytes, id)
	}
	if got.CategoryDisplayName == nil || *got.CategoryDisplayName != "Food" {
		t.Errorf("name = %v, want the trimmed Food", got.CategoryDisplayName)
	}
	if got.Active == nil || *got.Active {
		t.Errorf("active = %v, want a pointer to false", got.Active)
	}
}

// A Category's name is user content, and nothing logs a body.
func TestAnUpdateFailureLogsNoCategoryName(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return updateBreakable{Querier: q, update: func(context.Context, db.UpdateCategoryParams) (db.Category, error) {
			return db.Category{}, errOutage
		}}
	}, nil)
	_, ctx := f.member(t, "Home", "a@example.com")
	assertUpdateStatus(t, f.update(ctx, t, newID(t), ptr("Secret Savings Name"), nil), "500")

	if buf.Len() == 0 {
		t.Fatal("nothing was logged; the 500 should leave a trace")
	}
	if strings.Contains(buf.String(), "Secret Savings Name") {
		t.Errorf("a Category's name reached the log: %s", buf.String())
	}
}
