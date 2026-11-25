package expense_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/expense"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// PATCH /expenses/{id} (#140 X9, R9) against a real Postgres, at the handler:
// the contract's own rules (a `null` on a property that cannot be cleared, an
// unknown property) belong to the router and are in internal/httpserver and the
// conformance suite.

func (f fixture) update(ctx context.Context, t *testing.T, id pgtype.UUID, req api.UpdateExpenseRequest) api.UpdateExpenseResponseObject {
	t.Helper()
	resp, err := f.handlers.UpdateExpense(ctx, api.UpdateExpenseRequestObject{Id: id.Bytes, Body: &req})
	if err != nil {
		t.Fatalf("UpdateExpense returned an error: %v", err)
	}
	return resp
}

func assertUpdated(t *testing.T, resp api.UpdateExpenseResponseObject) api.Expense {
	t.Helper()
	r, ok := resp.(api.UpdateExpense200JSONResponse)
	if !ok {
		t.Fatalf("UpdateExpense = %s, want 200", status(resp))
	}
	return api.Expense(r)
}

func amount(s string) *api.SignedMoneyInput { a := api.SignedMoneyInput(s); return &a }

func nullOf[T any]() nullable.Nullable[T] { return nullable.NewNullNullable[T]() }
func valueOf[T any](v T) nullable.Nullable[T] {
	return nullable.NewNullableWithValue(v)
}

// updatedAt reads the column the answer does not carry.
func (f fixture) updatedAt(t *testing.T, id pgtype.UUID) time.Time {
	t.Helper()
	var at time.Time
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT updated_at FROM expenses WHERE id = $1`, id).Scan(&at); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	return at
}

// long ago, so a bump is never mistaken for the fixture's own insert time.
func (f fixture) backdate(t *testing.T, id pgtype.UUID) time.Time {
	t.Helper()
	f.exec(t, `UPDATE expenses SET updated_at = '2020-01-02 03:04:05+00' WHERE id = $1`, id)
	return f.updatedAt(t, id)
}

// stored is a live Expense with every optional property set, so a test sees each
// one cleared, kept or replaced.
func (f fixture) stored(ctx context.Context, t *testing.T, pocket pgtype.UUID, category pgtype.UUID) (pgtype.UUID, api.Expense) {
	t.Helper()
	req := body(pocket)
	if category.Valid {
		req.CategoryId = idPtr(category)
	}
	req.OccurredOn = ptr("2026-11-20")
	req.OccurredTime = ptr("19:05")
	req.Description = ptr("Lunch")
	created := assertCreated(t, f.create(ctx, t, req))
	return pgtype.UUID{Bytes: created.Id, Valid: true}, created
}

// ---- merge, clear, absent ---------------------------------------------------------

// {} is a 200 that answers the Expense as it stands and writes nothing, not even
// updated_at.
func TestAnEmptyEditIsA200AndWritesNothing(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	id, created := f.stored(ctx, t, pocket, category)
	f.backdate(t, id)
	before := f.rows(t)

	got := assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{}))

	if wire(t, got) != wire(t, created) {
		t.Errorf("{} answered %s, want the stored %s", wire(t, got), wire(t, created))
	}
	if after := f.rows(t); after != before {
		t.Errorf("{} changed the row:\n%s\n%s", before, after)
	}
}

// A partial body changes only what it names; captured_at, the currency and the id
// are never among them.
func TestAPartialEditLeavesTheOtherFieldsAlone(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	id, created := f.stored(ctx, t, pocket, category)

	got := assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{Amount: amount("52000")}))

	want := created
	want.Amount = "52000.0000"
	if wire(t, got) != wire(t, want) {
		t.Errorf("answered %s, want %s", wire(t, got), wire(t, want))
	}
	if !got.CapturedAt.Equal(created.CapturedAt) || got.Currency != "IDR" || got.Id != created.Id {
		t.Errorf("captured_at, currency or id moved: %s", wire(t, got))
	}
}

func TestEveryEditableFieldIsEditable(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	otherPocket := f.tdb.CreatePocket(t, household, "Wallet")
	otherCategory := f.tdb.CreateCategory(t, household, "Fun", "daily")
	colleague := f.tdb.CreateUser(t, household, "b@example.com", "B")
	id, _ := f.stored(ctx, t, pocket, category)
	sole, joint := api.SoleOwner, api.Joint

	for name, tc := range map[string]struct {
		req   api.UpdateExpenseRequest
		check func(api.Expense) bool
	}{
		"a refund":               {api.UpdateExpenseRequest{Amount: amount("-25000")}, func(e api.Expense) bool { return e.Amount == "-25000.0000" }},
		"a decimal amount":       {api.UpdateExpenseRequest{Amount: amount("1.5")}, func(e api.Expense) bool { return e.Amount == "1.5000" }},
		"a future date":          {api.UpdateExpenseRequest{OccurredOn: ptr("2999-12-31")}, func(e api.Expense) bool { return e.OccurredOn.Format("2006-01-02") == "2999-12-31" }},
		"a time":                 {api.UpdateExpenseRequest{OccurredTime: valueOf("04:00")}, func(e api.Expense) bool { return e.OccurredTime != nil && *e.OccurredTime == "04:00" }},
		"another Pocket":         {api.UpdateExpenseRequest{PocketId: idPtr(otherPocket)}, func(e api.Expense) bool { return e.PocketId == otherPocket.Bytes }},
		"another Category":       {api.UpdateExpenseRequest{CategoryId: valueOf(uuid.UUID(otherCategory.Bytes))}, func(e api.Expense) bool { return *e.CategoryId == otherCategory.Bytes }},
		"a description, trimmed": {api.UpdateExpenseRequest{Description: valueOf("  Dinner  ")}, func(e api.Expense) bool { return *e.Description == "Dinner" }},
		"a sole owner": {api.UpdateExpenseRequest{Ownership: &sole, OwnerUserId: idPtr(colleague)}, func(e api.Expense) bool {
			return e.Ownership == api.SoleOwner && e.OwnerUserId != nil && *e.OwnerUserId == colleague.Bytes
		}},
		"joint again clears the owner": {api.UpdateExpenseRequest{Ownership: &joint}, func(e api.Expense) bool { return e.Ownership == api.Joint && e.OwnerUserId == nil }},
	} {
		got := assertUpdated(t, f.update(ctx, t, id, tc.req))
		if !tc.check(got) {
			t.Errorf("%s: answered %s", name, wire(t, got))
		}
		// And what the answer says is what is stored: the answer is the row.
		var stored string
		if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT to_jsonb(e)::text FROM expenses e WHERE id = $1`, id).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stored, got.Amount) {
			t.Errorf("%s: the stored row %s does not carry the answer's amount %s", name, stored, got.Amount)
		}
	}
	if n := f.count(t); n != 1 {
		t.Errorf("%d rows after edits, want 1", n)
	}
}

// Absent leaves a clearable field alone, null clears it, a value sets it: three
// states for each of the three.
func TestAClearableFieldIsAbsentNullOrAValue(t *testing.T) {
	type state struct {
		name string
		req  func(newCategory pgtype.UUID) api.UpdateExpenseRequest
	}
	for field, cases := range map[string]struct {
		states []state
		read   func(api.Expense) string
		stored string // what the Expense holds before
		after  map[string]string
	}{
		"occurred_time": {
			read:   func(e api.Expense) string { return ptrOr(e.OccurredTime) },
			stored: "19:05",
			states: []state{
				{"absent", func(pgtype.UUID) api.UpdateExpenseRequest { return api.UpdateExpenseRequest{Amount: amount("2")} }},
				{"null", func(pgtype.UUID) api.UpdateExpenseRequest {
					return api.UpdateExpenseRequest{OccurredTime: nullOf[string]()}
				}},
				{"value", func(pgtype.UUID) api.UpdateExpenseRequest {
					return api.UpdateExpenseRequest{OccurredTime: valueOf("07:30")}
				}},
			},
			after: map[string]string{"absent": "19:05", "null": "", "value": "07:30"},
		},
		"category_id": {
			read:   func(e api.Expense) string { return idOr(e.CategoryId) },
			stored: "stored",
			states: []state{
				{"absent", func(pgtype.UUID) api.UpdateExpenseRequest { return api.UpdateExpenseRequest{Amount: amount("2")} }},
				{"null", func(pgtype.UUID) api.UpdateExpenseRequest {
					return api.UpdateExpenseRequest{CategoryId: nullOf[uuid.UUID]()}
				}},
				{"value", func(c pgtype.UUID) api.UpdateExpenseRequest {
					return api.UpdateExpenseRequest{CategoryId: valueOf(uuid.UUID(c.Bytes))}
				}},
			},
			after: map[string]string{"absent": "stored", "null": "", "value": "new"},
		},
		"description": {
			read:   func(e api.Expense) string { return ptrOr(e.Description) },
			stored: "Lunch",
			states: []state{
				{"absent", func(pgtype.UUID) api.UpdateExpenseRequest { return api.UpdateExpenseRequest{Amount: amount("2")} }},
				{"null", func(pgtype.UUID) api.UpdateExpenseRequest {
					return api.UpdateExpenseRequest{Description: nullOf[string]()}
				}},
				{"value", func(pgtype.UUID) api.UpdateExpenseRequest {
					return api.UpdateExpenseRequest{Description: valueOf("Dinner")}
				}},
			},
			after: map[string]string{"absent": "Lunch", "null": "", "value": "Dinner"},
		},
	} {
		for _, st := range cases.states {
			t.Run(field+" "+st.name, func(t *testing.T) {
				f := newFixture(t)
				household, pocket, ctx := f.member(t, "Home", "a@example.com")
				category := f.tdb.CreateCategory(t, household, "Food", "daily")
				fresh := f.tdb.CreateCategory(t, household, "Fun", "daily")
				id, _ := f.stored(ctx, t, pocket, category)

				got := assertUpdated(t, f.update(ctx, t, id, st.req(fresh)))

				want := cases.after[st.name]
				switch want {
				case "stored":
					want = category.String()
				case "new":
					want = fresh.String()
				}
				if field == "category_id" {
					want = strings.ToLower(want)
				}
				if v := cases.read(got); v != want {
					t.Errorf("%s: %s = %q, want %q", st.name, field, v, want)
				}
				// A cleared field is omitted from the wire, never null.
				if st.name == "null" && strings.Contains(wire(t, got), `"`+field+`"`) {
					t.Errorf("a cleared %s is on the wire: %s", field, wire(t, got))
				}
			})
		}
	}
}

func ptrOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func idOr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// ---- the body's own rules ---------------------------------------------------------

func TestAnEditRefusesWhatACreateRefuses(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	owner := f.tdb.CreateUser(t, household, "b@example.com", "B")
	id, _ := f.stored(ctx, t, pocket, f.tdb.CreateCategory(t, household, "Food", "daily"))
	before := f.rows(t)
	sole, joint := api.SoleOwner, api.Joint

	for name, tc := range map[string]struct {
		req  api.UpdateExpenseRequest
		want string
	}{
		"zero":                     {api.UpdateExpenseRequest{Amount: amount("0")}, "VALIDATION amount nonzero"},
		"zero decimals":            {api.UpdateExpenseRequest{Amount: amount("0.0000")}, "VALIDATION amount nonzero"},
		"negative zero":            {api.UpdateExpenseRequest{Amount: amount("-0")}, "VALIDATION amount nonzero"},
		"not a date":               {api.UpdateExpenseRequest{OccurredOn: ptr("2026-02-30")}, "VALIDATION occurred_on invalid"},
		"year zero":                {api.UpdateExpenseRequest{OccurredOn: ptr("0000-01-01")}, "VALIDATION occurred_on invalid"},
		"a bad time":               {api.UpdateExpenseRequest{OccurredTime: valueOf("25:00")}, "VALIDATION occurred_time pattern"},
		"a blank text":             {api.UpdateExpenseRequest{Description: valueOf("   ")}, "VALIDATION description required"},
		"an empty text":            {api.UpdateExpenseRequest{Description: valueOf("")}, "VALIDATION description required"},
		"too long a text":          {api.UpdateExpenseRequest{Description: valueOf(strings.Repeat("a", 201))}, "VALIDATION description max"},
		"a NUL":                    {api.UpdateExpenseRequest{Description: valueOf("a\x00b")}, "VALIDATION description pattern"},
		"sole with no User":        {api.UpdateExpenseRequest{Ownership: &sole}, "VALIDATION owner_user_id invalid"},
		"a User with no ownership": {api.UpdateExpenseRequest{OwnerUserId: idPtr(owner)}, "VALIDATION owner_user_id invalid"},
		"joint with a User":        {api.UpdateExpenseRequest{Ownership: &joint, OwnerUserId: idPtr(owner)}, "VALIDATION owner_user_id invalid"},
		"a User who is not live":   {api.UpdateExpenseRequest{Ownership: &sole, OwnerUserId: idPtr(testutil.NewUUID(t))}, "VALIDATION owner_user_id invalid"},
	} {
		if got := status(f.update(ctx, t, id, tc.req)); got != tc.want {
			t.Errorf("%s answered %s, want %s", name, got, tc.want)
		}
	}
	if after := f.rows(t); after != before {
		t.Errorf("a refused edit changed the row:\n%s\n%s", before, after)
	}

	// 200 code points is the cap, counted in code points.
	for _, text := range []string{strings.Repeat("a", 200), strings.Repeat("\U0001F6F6", 200), "  padded  "} {
		assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{Description: valueOf(text)}))
	}
}

// The least (field, rule) is the one reported, and a body that fails is
// refused whole: the valid half is not applied.
func TestTheLeastFailureOfAnEditIsReportedAndNothingIsApplied(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, _ := f.stored(ctx, t, pocket, f.tdb.CreateCategory(t, household, "Food", "daily"))
	before := f.rows(t)

	resp := f.update(ctx, t, id, api.UpdateExpenseRequest{
		Amount: amount("0"), OccurredOn: ptr("2026-02-30"), Description: valueOf(" "), OccurredTime: valueOf("07:30"),
	})
	assertStatus(t, resp, "VALIDATION amount nonzero")
	resp = f.update(ctx, t, id, api.UpdateExpenseRequest{OccurredOn: ptr("2026-02-30"), Description: valueOf(" "), OccurredTime: valueOf("07:30")})
	assertStatus(t, resp, "VALIDATION description required")

	// References: category_id sorts before pocket_id.
	resp = f.update(ctx, t, id, api.UpdateExpenseRequest{PocketId: idPtr(testutil.NewUUID(t)), CategoryId: valueOf(uuid.New())})
	assertStatus(t, resp, "VALIDATION category_id invalid")

	if after := f.rows(t); after != before {
		t.Errorf("a refused edit changed the row:\n%s\n%s", before, after)
	}
}

// ---- tenancy and the one 404 ------------------------------------------------------

// An unknown id, another Household's and a deleted one answer the same bytes,
// for every body, {} included.
func TestAnIdThatIsNotTheCallersLiveExpenseIsOne404(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
	theirs := f.tdb.CreateExpense(t, other, theirPocket, testutil.ExpenseOpts{})
	deleted := f.tdb.CreateExpense(t, household, pocket, testutil.ExpenseOpts{Deleted: true})
	before := f.rows(t)

	for name, req := range map[string]api.UpdateExpenseRequest{
		"{}":                {},
		"a change":          {Amount: amount("1")},
		"a clear":           {CategoryId: nullOf[uuid.UUID](), Description: nullOf[string]()},
		"a reference":       {PocketId: idPtr(pocket)},
		"a foreign pocket":  {PocketId: idPtr(theirPocket)},
		"an unknown pocket": {PocketId: idPtr(testutil.NewUUID(t))},
	} {
		var answers []string
		for _, id := range []pgtype.UUID{testutil.NewUUID(t), theirs, deleted} {
			resp := f.update(ctx, t, id, req)
			assertStatus(t, resp, "NOT_FOUND")
			answers = append(answers, wire(t, resp))
		}
		if answers[0] != answers[1] || answers[0] != answers[2] || answers[0] != `{"code":"NOT_FOUND"}` {
			t.Errorf("%s: the three differ or carry args: %v", name, answers)
		}
	}
	if after := f.rows(t); after != before {
		t.Errorf("an edit of an id that is not the caller's changed a row:\n%s\n%s", before, after)
	}
}

// Validation before lookup: a foreign id with a bad body answers what an unknown
// id with that body does, and what the caller's own id does.
func TestABadBodyIsRefusedWhateverTheIdHoldsOnAnEdit(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	mine := f.tdb.CreateExpense(t, household, pocket, testutil.ExpenseOpts{})
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirs := f.tdb.CreateExpense(t, other, f.tdb.CreatePocket(t, other, "Theirs"), testutil.ExpenseOpts{})

	var answers []string
	for _, id := range []pgtype.UUID{testutil.NewUUID(t), theirs, mine} {
		resp := f.update(ctx, t, id, api.UpdateExpenseRequest{Amount: amount("0")})
		assertStatus(t, resp, "VALIDATION amount nonzero")
		answers = append(answers, wire(t, resp))
	}
	if answers[0] != answers[1] || answers[0] != answers[2] {
		t.Errorf("the three differ: %v", answers)
	}
}

// Every operation run as one Household leaves every column of the other's
// Expenses byte-identical, and an Expense of the other's is never moved onto the
// caller's Pocket.
func TestAnEditNeverTouchesAnotherHouseholdsExpenses(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
	theirs := f.tdb.CreateExpense(t, other, theirPocket, testutil.ExpenseOpts{})
	f.backdate(t, theirs)
	before := f.rows(t)

	for _, req := range []api.UpdateExpenseRequest{
		{}, {Amount: amount("9")}, {PocketId: idPtr(pocket)}, {CategoryId: nullOf[uuid.UUID]()}, {Description: valueOf("mine now")},
	} {
		assertStatus(t, f.update(ctx, t, theirs, req), "NOT_FOUND")
	}
	if after := f.rows(t); after != before {
		t.Errorf("another Household's row changed:\n%s\n%s", before, after)
	}
}

// ---- references -------------------------------------------------------------------

// Only a reference that is sent and different is judged. The amount of an
// Expense whose Pocket (or Category) was deactivated since can still be fixed,
// and sending the stored reference back changes nothing and is not refused.
func TestAReferenceLeftAsStoredIsNotLookedAtAgain(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	id, _ := f.stored(ctx, t, pocket, category)
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, pocket)
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, category)

	got := assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{Amount: amount("52000")}))
	if got.Amount != "52000.0000" || got.PocketId != pocket.Bytes {
		t.Errorf("answered %s", wire(t, got))
	}
	// The stored references, sent again, beside a change.
	got = assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{
		Amount: amount("1"), PocketId: idPtr(pocket), CategoryId: valueOf(uuid.UUID(category.Bytes)),
	}))
	if got.Amount != "1.0000" || got.PocketId != pocket.Bytes || *got.CategoryId != category.Bytes {
		t.Errorf("answered %s", wire(t, got))
	}
	// A new reference to the same inactive rows is what is refused: the contrast.
	other := f.tdb.CreatePocket(t, household, "Wallet")
	moved := assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{PocketId: idPtr(other)}))
	if moved.PocketId != other.Bytes {
		t.Fatalf("moved to %s", wire(t, moved))
	}
	assertStatus(t, f.update(ctx, t, id, api.UpdateExpenseRequest{PocketId: idPtr(pocket)}), "VALIDATION pocket_id inactive")
}

func TestAChangedReferenceIsJudgedAsAtCreation(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, _ := f.stored(ctx, t, pocket, f.tdb.CreateCategory(t, household, "Food", "daily"))
	otherHousehold := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, otherHousehold, "Theirs")
	theirCategory := f.tdb.CreateCategory(t, otherHousehold, "Theirs", "daily")
	gonePocket := f.tdb.CreatePocket(t, household, "Gone")
	goneCategory := f.tdb.CreateCategory(t, household, "Gone", "daily")
	idlePocket := f.tdb.CreatePocket(t, household, "Idle")
	idleCategory := f.tdb.CreateCategory(t, household, "Idle", "daily")
	f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, gonePocket)
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, goneCategory)
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, idlePocket)
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, idleCategory)
	before := f.rows(t)

	// Unknown, another Household's and deleted: the same bytes.
	var pockets, categories []string
	for _, p := range []pgtype.UUID{testutil.NewUUID(t), theirPocket, gonePocket} {
		resp := f.update(ctx, t, id, api.UpdateExpenseRequest{PocketId: idPtr(p)})
		assertStatus(t, resp, "VALIDATION pocket_id invalid")
		pockets = append(pockets, wire(t, resp))
	}
	for _, c := range []pgtype.UUID{testutil.NewUUID(t), theirCategory, goneCategory} {
		resp := f.update(ctx, t, id, api.UpdateExpenseRequest{CategoryId: valueOf(uuid.UUID(c.Bytes))})
		assertStatus(t, resp, "VALIDATION category_id invalid")
		categories = append(categories, wire(t, resp))
	}
	if pockets[0] != pockets[1] || pockets[0] != pockets[2] || categories[0] != categories[1] || categories[0] != categories[2] {
		t.Errorf("the three differ: %v %v", pockets, categories)
	}
	// The caller's own inactive row says so (R10: a Category as a Pocket).
	assertStatus(t, f.update(ctx, t, id, api.UpdateExpenseRequest{PocketId: idPtr(idlePocket)}), "VALIDATION pocket_id inactive")
	assertStatus(t, f.update(ctx, t, id, api.UpdateExpenseRequest{CategoryId: valueOf(uuid.UUID(idleCategory.Bytes))}), "VALIDATION category_id inactive")
	// Both at once: the least, category_id.
	assertStatus(t, f.update(ctx, t, id, api.UpdateExpenseRequest{PocketId: idPtr(idlePocket), CategoryId: valueOf(uuid.UUID(idleCategory.Bytes))}), "VALIDATION category_id inactive")
	// A refused reference writes nothing, the valid half of the body included.
	assertStatus(t, f.update(ctx, t, id, api.UpdateExpenseRequest{Amount: amount("7"), PocketId: idPtr(idlePocket)}), "VALIDATION pocket_id inactive")
	if after := f.rows(t); after != before {
		t.Errorf("a refused edit changed the row:\n%s\n%s", before, after)
	}
}

// The owner is judged like the other references: only when it differs.
func TestAnOwnerIsJudgedOnlyWhenItChanges(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	owner := f.tdb.CreateUser(t, household, "b@example.com", "B")
	foreign := f.tdb.CreateUser(t, f.tdb.CreateHousehold(t, "Elsewhere"), "c@example.com", "C")
	sole := api.SoleOwner
	id := pgtype.UUID{Bytes: assertCreated(t, f.create(ctx, t, func() api.CreateExpenseRequest {
		r := body(pocket)
		r.Ownership, r.OwnerUserId = &sole, idPtr(owner)
		return r
	}())).Id, Valid: true}
	f.exec(t, `UPDATE users SET deleted_at = now() WHERE id = $1`, owner)

	// The stored owner left the Household; the amount can still be fixed and the
	// same pair sent again is not refused.
	assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{Amount: amount("52000")}))
	assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{Ownership: &sole, OwnerUserId: idPtr(owner)}))
	// Another User is judged: not the Household's, and the deleted one by another door.
	assertStatus(t, f.update(ctx, t, id, api.UpdateExpenseRequest{Ownership: &sole, OwnerUserId: idPtr(foreign)}), "VALIDATION owner_user_id invalid")
}

// ---- updated_at -------------------------------------------------------------------

// An edit that changes nothing does not move updated_at; one that changes
// anything does. Both answer the row.
func TestUpdatedAtMovesOnlyWhenSomethingDoes(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	// A second Expense, with nothing optional, for the clears of what is not there.
	bare := assertCreated(t, f.create(ctx, t, body(pocket)))
	bareID := pgtype.UUID{Bytes: bare.Id, Valid: true}
	id, _ := f.stored(ctx, t, pocket, category)
	joint := api.Joint

	for name, tc := range map[string]struct {
		id  pgtype.UUID
		req api.UpdateExpenseRequest
	}{
		"{}":                       {id, api.UpdateExpenseRequest{}},
		"the same amount":          {id, api.UpdateExpenseRequest{Amount: amount("25000")}},
		"the same amount, spelled": {id, api.UpdateExpenseRequest{Amount: amount("25000.0")}},
		"every stored value": {id, api.UpdateExpenseRequest{
			Amount: amount("25000"), OccurredOn: ptr("2026-11-20"), OccurredTime: valueOf("19:05"), PocketId: idPtr(pocket),
			CategoryId: valueOf(uuid.UUID(category.Bytes)), Description: valueOf("Lunch"), Ownership: &joint,
		}},
		"a clear of what is not there": {bareID, api.UpdateExpenseRequest{
			OccurredTime: nullOf[string](), CategoryId: nullOf[uuid.UUID](), Description: nullOf[string](),
		}},
	} {
		old := f.backdate(t, tc.id)
		before := f.rows(t)
		assertUpdated(t, f.update(ctx, t, tc.id, tc.req))
		if got := f.updatedAt(t, tc.id); !got.Equal(old) {
			t.Errorf("%s moved updated_at from %s to %s", name, old, got)
		}
		if after := f.rows(t); after != before {
			t.Errorf("%s changed the row:\n%s\n%s", name, before, after)
		}
	}

	for name, req := range map[string]api.UpdateExpenseRequest{
		"an amount":             {Amount: amount("25001")},
		"a date":                {OccurredOn: ptr("2026-11-21")},
		"a time":                {OccurredTime: valueOf("19:06")},
		"a clear of a time":     {OccurredTime: nullOf[string]()},
		"a clear of a text":     {Description: nullOf[string]()},
		"a clear of a Category": {CategoryId: nullOf[uuid.UUID]()},
	} {
		f.exec(t, `UPDATE expenses SET occurred_time = '19:05', category_id = $2, description = 'Lunch', amount = 25000, occurred_on = '2026-11-20' WHERE id = $1`, id, category)
		old := f.backdate(t, id)
		assertUpdated(t, f.update(ctx, t, id, req))
		if got := f.updatedAt(t, id); !got.After(old) {
			t.Errorf("%s did not move updated_at (%s)", name, got)
		}
	}
}

// ---- a failure leaves no trace of the request -------------------------------------

func TestAFailedEditLogsNeitherTheAmountNorTheDescription(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	leaky := &pgconn.PgError{
		Severity: "ERROR", Code: "23514", ConstraintName: "expenses_description_not_blank",
		Message: "violates check constraint on (Secret Description, 31337.0000)",
		Detail:  "Failing row contains (Secret Description, 31337.0000).",
	}
	f := newFixtureWith(t, func(tx db.DBTX) db.DBTX {
		return hooked{DBTX: tx, fail: func(sql string) error {
			if strings.Contains(sql, marker(updateQuery)) {
				return leaky
			}
			return nil
		}}
	})
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})

	assertStatus(t, f.update(ctx, t, id, api.UpdateExpenseRequest{Amount: amount("31337"), Description: valueOf("Secret Description")}), "500")

	want := `msg="update expense" sqlstate=23514 constraint=expenses_description_not_blank`
	if !strings.Contains(buf.String(), want) {
		t.Errorf("the log has no line %q: %s", want, buf.String())
	}
	for _, secret := range []string{"Secret Description", "31337", "err="} {
		if strings.Contains(buf.String(), secret) {
			t.Errorf("%q reached the log: %s", secret, buf.String())
		}
	}
}

// Without a session the handler answers 401 and touches nothing (the router
// answers it first; this is the net under it).
func TestAnEditWithoutASessionIs401(t *testing.T) {
	f := newFixture(t)
	resp, err := f.handlers.UpdateExpense(context.Background(), api.UpdateExpenseRequestObject{Id: uuid.New(), Body: &api.UpdateExpenseRequest{}})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, resp, "401")
}

// ---- the reconcile scenario, at the handler ---------------------------------------

// A lost response, a retry, an edit in between: the POST's replay answers the
// stored row and applies nothing; the PATCH is what puts the edit there.
func TestAReplayedCreateThenAnEditReconcilesToWhatIsOnScreen(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	first := body(pocket)
	first.Amount = "25000"
	assertCreated(t, f.create(ctx, t, first))

	retried := first
	retried.Amount = "52000"
	if got := assertReplayed(t, f.create(ctx, t, retried)); got.Amount != "25000.0000" {
		t.Fatalf("the replay answered %s, want the stored 25000.0000", got.Amount)
	}
	got := assertUpdated(t, f.update(ctx, t, pgtype.UUID{Bytes: first.Id, Valid: true}, api.UpdateExpenseRequest{Amount: amount("52000")}))
	if got.Amount != "52000.0000" {
		t.Errorf("the edit answered %s", got.Amount)
	}
	if n := f.count(t); n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
}

// ---- merge: two writers of different fields both land -----------------------------

// Concurrent PATCHes of different fields both land (BOOTSTRAP.md §4: last write
// wins per field, never a read-modify-write).
func TestConcurrentEditsOfDifferentFieldsBothLand(t *testing.T) {
	for round := range 5 {
		f := newFixture(t)
		household, pocket, ctx := f.member(t, "Home", "a@example.com")
		category := f.tdb.CreateCategory(t, household, "Food", "daily")
		id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})

		statuses := race(2, func(i int) string {
			if i == 0 {
				return status(f.update(ctx, t, id, api.UpdateExpenseRequest{Amount: amount("52000")}))
			}
			return status(f.update(ctx, t, id, api.UpdateExpenseRequest{CategoryId: valueOf(uuid.UUID(category.Bytes))}))
		})
		for _, s := range statuses {
			if s != "200" {
				t.Errorf("round %d: answered %s", round, s)
			}
		}
		var amountNow string
		var categoryNow *uuid.UUID
		if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT amount::text, category_id FROM expenses WHERE id = $1`, id).Scan(&amountNow, &categoryNow); err != nil {
			t.Fatal(err)
		}
		if amountNow != "52000.0000" || categoryNow == nil || *categoryNow != uuid.UUID(category.Bytes) {
			t.Errorf("round %d: amount %s, category %v: one edit was lost", round, amountNow, categoryNow)
		}
	}
}

// The statement merges by itself, whatever serialises the callers: the same
// race with the Expense's row lock taken off still lands both, which is what
// "one UPDATE of coalesce and flags" buys. The red proof beside it is the
// read-modify-write the rule forbids, run on the same interleaving: each
// writer reads the whole row, both wait until both have read, and each writes
// every column back. One edit is lost.
func TestTheStatementMergesWithoutTheRowLockAndAReadModifyWriteLosesAnEdit(t *testing.T) {
	noRowLock := func(tx db.DBTX) db.DBTX {
		return hooked{DBTX: tx, rewrite: func(sql string) string { return strip(sql, "FOR NO KEY UPDATE", "GetLiveExpenseForUpdate") }}
	}
	f := newFixtureWith(t, noRowLock)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")

	for round := range 5 {
		id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})
		race(2, func(i int) string {
			if i == 0 {
				return status(f.update(ctx, t, id, api.UpdateExpenseRequest{Amount: amount("52000")}))
			}
			return status(f.update(ctx, t, id, api.UpdateExpenseRequest{CategoryId: valueOf(uuid.UUID(category.Bytes))}))
		})
		var amountNow string
		var categoryNow *uuid.UUID
		if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT amount::text, category_id FROM expenses WHERE id = $1`, id).Scan(&amountNow, &categoryNow); err != nil {
			t.Fatal(err)
		}
		if amountNow != "52000.0000" || categoryNow == nil {
			t.Errorf("round %d: amount %s, category %v: the statement lost an edit", round, amountNow, categoryNow)
		}
	}

	// The red proof: a read-modify-write.
	id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})
	var mu sync.Mutex
	read, all := 0, make(chan struct{})
	race(2, func(i int) string {
		var amountNow string
		var categoryNow *uuid.UUID
		if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT amount::text, category_id FROM expenses WHERE id = $1`, id).Scan(&amountNow, &categoryNow); err != nil {
			return err.Error()
		}
		mu.Lock()
		read++
		if read == 2 {
			close(all)
		}
		mu.Unlock()
		<-all
		if i == 0 {
			amountNow = "52000"
		} else {
			c := uuid.UUID(category.Bytes)
			categoryNow = &c
		}
		if _, err := f.tdb.Pool.Exec(context.Background(), `UPDATE expenses SET amount = $2::numeric, category_id = $3 WHERE id = $1`, id, amountNow, categoryNow); err != nil {
			return err.Error()
		}
		return "ok"
	})
	var amountNow string
	var categoryNow *uuid.UUID
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT amount::text, category_id FROM expenses WHERE id = $1`, id).Scan(&amountNow, &categoryNow); err != nil {
		t.Fatal(err)
	}
	if amountNow == "52000.0000" && categoryNow != nil {
		t.Error("the read-modify-write kept both edits, so this is no proof that the merge statement is what holds them")
	}
}

// ---- the stale comparison ---------------------------------------------------------

// "Only a reference that differs from the stored one is judged" is decided from
// a read, so the read has to be of what the row is when the write lands. Three
// writers, one Expense on Pocket A: an edit that sends pocket_id A (so it takes
// no lock on A), another edit that moves the Expense to B, and a delete of A. If
// the first reads before the second commits and writes after the delete, the
// Expense references a deleted Pocket. The Expense's row lock makes the second
// wait for the first, so the delete sees the Expense still on A (POCKET_IN_USE).
func TestAnEditThatLeavesAReferenceAloneCannotReattachADeletedOne(t *testing.T) {
	scenario := func(t *testing.T, rowLock bool) (deleted, moved string, violated int) {
		t.Helper()
		held := newGate()
		first := func(tx db.DBTX) db.DBTX {
			h := hooked{DBTX: tx, before: func(sql string) {
				if strings.Contains(sql, marker(updateQuery)) {
					held.pause()
				}
			}}
			if !rowLock {
				h.rewrite = func(sql string) string { return strip(sql, "FOR NO KEY UPDATE", "GetLiveExpenseForUpdate") }
			}
			return h
		}
		w := newWorld(t, first, identity)
		other := expense.New(expense.Deps{Querier: w.tdb.Queries, Beginner: w.tdb.Pool, Now: func() time.Time { return wednesday }})
		onA := w.tdb.CreateExpense(t, w.household, w.pocket, testutil.ExpenseOpts{})

		firstDone := make(chan string, 1)
		go func() {
			firstDone <- w.edit(t, onA, api.UpdateExpenseRequest{PocketId: idPtr(w.pocket), Amount: amount("7")})
		}()
		held.waitReached(t)

		movedCh := make(chan string, 1)
		go func() {
			resp, _ := other.UpdateExpense(w.ctx, api.UpdateExpenseRequestObject{Id: onA.Bytes, Body: &api.UpdateExpenseRequest{PocketId: idPtr(w.keeper)}})
			movedCh <- status(resp)
		}()
		moved, movedFinished := within(movedCh, blockedFor)

		deletedCh := make(chan string, 1)
		go func() { deletedCh <- guards[0].remove(t, w) }()
		deleted, _ = within(deletedCh, 5*time.Second)

		held.let()
		within(firstDone, 10*time.Second)
		if !movedFinished {
			moved, _ = within(movedCh, 10*time.Second)
		}
		return deleted, moved, scalarInt(t, w, `SELECT count(*) FROM expenses e JOIN pockets p ON p.id = e.pocket_id WHERE p.deleted_at IS NOT NULL`)
	}

	t.Run("with the row lock", func(t *testing.T) {
		deleted, moved, violated := scenario(t, true)
		if violated != 0 || deleted != "POCKET_IN_USE" || moved != "200" {
			t.Errorf("delete %s, move %s, %d Expense(s) on a deleted Pocket; want POCKET_IN_USE, 200 and none", deleted, moved, violated)
		}
	})
	t.Run("without it the Expense ends on a deleted Pocket", func(t *testing.T) {
		deleted, moved, violated := scenario(t, false)
		if violated == 0 {
			t.Errorf("delete %s, move %s: nothing is wrong, so the row lock is not what holds this", deleted, moved)
		}
	})
}
