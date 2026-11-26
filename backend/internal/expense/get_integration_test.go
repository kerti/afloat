package expense_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// GET /expenses/{id} (#140 X1) against a real Postgres, at the handler. The
// shape of the id is the binder's and is proven through the router
// (internal/httpserver) and the conformance suite.

func (f fixture) get(ctx context.Context, t *testing.T, id pgtype.UUID) api.GetExpenseResponseObject {
	t.Helper()
	resp, err := f.handlers.GetExpense(ctx, api.GetExpenseRequestObject{Id: id.Bytes})
	if err != nil {
		t.Fatalf("GetExpense returned an error: %v", err)
	}
	return resp
}

// The caller's own Expense answers as the create, the edit and the list do: the
// same wire shape, every optional property present when set.
func TestGetAnswersTheCallersOwnExpenseAsTheOtherReadsDo(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	id, created := f.stored(ctx, t, pocket, category)

	resp := f.get(ctx, t, id)
	assertStatus(t, resp, "200")
	got := api.Expense(resp.(api.GetExpense200JSONResponse))
	if wire(t, got) != wire(t, created) {
		t.Errorf("GET answered %s, want the create's %s", wire(t, got), wire(t, created))
	}
	listed := f.list(ctx, t, nil, nil).Items
	if len(listed) != 1 || wire(t, listed[0]) != wire(t, got) {
		t.Errorf("the list's row %v differs from GET's %s", listed, wire(t, got))
	}
}

// An Expense with nothing optional set omits every optional property: never
// null, as on every other answer.
func TestGetOmitsWhatIsEmptyAndKeepsARefundsSign(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	req := body(pocket)
	req.Amount = "-450000"
	req.OccurredOn = ptr("2026-11-20")
	created := assertCreated(t, f.create(ctx, t, req))

	resp := f.get(ctx, t, pgtype.UUID{Bytes: created.Id, Valid: true})
	assertStatus(t, resp, "200")
	got := wire(t, resp)
	for _, absent := range []string{"occurred_time", "category_id", "description", "owner_user_id", "deleted_at"} {
		if strings.Contains(got, `"`+absent+`"`) {
			t.Errorf("GET carries %s: %s", absent, got)
		}
	}
	if strings.Contains(got, "null") {
		t.Errorf("GET carries a null: %s", got)
	}
	if !strings.Contains(got, `"amount":"-450000.0000"`) {
		t.Errorf("GET = %s, want the refund's sign at four decimals", got)
	}
}

// Unknown, another Household's and deleted are one answer, byte for byte, and
// nothing is written.
func TestGetOfAnIdThatIsNotTheCallersLiveExpenseIsOne404(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
	theirs := f.tdb.CreateExpense(t, other, theirPocket, testutil.ExpenseOpts{})
	deleted := f.tdb.CreateExpense(t, household, pocket, testutil.ExpenseOpts{Deleted: true})
	before := f.rows(t)

	var answers []string
	for name, id := range map[string]pgtype.UUID{"unknown": testutil.NewUUID(t), "foreign": theirs, "deleted": deleted} {
		resp := f.get(ctx, t, id)
		if status(resp) != "NOT_FOUND" {
			t.Errorf("%s: answered %s, want NOT_FOUND", name, status(resp))
		}
		answers = append(answers, wire(t, resp))
	}
	for _, a := range answers {
		if a != `{"code":"NOT_FOUND"}` {
			t.Errorf("answers differ or carry args: %v", answers)
		}
	}
	if after := f.rows(t); after != before {
		t.Errorf("a GET changed a row:\n%s\n%s", before, after)
	}

	// The other Household reads its own, so the 404 above is about the caller and
	// not about the id.
	theirCtx := f.userIn(t, other, "b@example.com")
	assertStatus(t, f.get(theirCtx, t, theirs), "200")
}

// A deleted Expense is gone to GET and back once it is restored.
func TestGetSeesAnEditAndNotASoftDelete(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	id, _ := f.stored(ctx, t, pocket, pgtype.UUID{})

	assertUpdated(t, f.update(ctx, t, id, api.UpdateExpenseRequest{Amount: amount("52000"), Description: nullOf[string]()}))
	got := api.Expense(f.get(ctx, t, id).(api.GetExpense200JSONResponse))
	if got.Amount != "52000.0000" || got.Description != nil {
		t.Errorf("GET after an edit = %s", wire(t, got))
	}

	assertStatus(t, f.del(ctx, t, id), "204")
	assertStatus(t, f.get(ctx, t, id), "NOT_FOUND")
	assertStatus(t, f.restore(ctx, t, id), "200")
	assertStatus(t, f.get(ctx, t, id), "200")
}

func TestGetWithoutASessionIs401(t *testing.T) {
	f := newFixture(t)
	resp, err := f.handlers.GetExpense(context.Background(), api.GetExpenseRequestObject{Id: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, resp, "401")
}
