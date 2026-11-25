package expense_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/expense"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// The tests here talk to a real Postgres and none is parallel (ADR-0004): they
// share one database that each test empties. The concurrent ones start their own
// goroutines, which is the point of them. The clock is injected: a request's
// Today is never read from the machine's.

// wednesday is 2026-11-25 14:00 in Jakarta (UTC+7): an ordinary afternoon on the
// Period Day 2026-11-25 for a Household that rolls over at 04:00.
var wednesday = time.Date(2026, 11, 25, 7, 0, 0, 0, time.UTC)

type fixture struct {
	tdb      *testutil.TestDB
	handlers *expense.Handlers
	now      *time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return newFixtureWith(t, func(tx db.DBTX) db.DBTX { return tx })
}

// newFixtureWith runs the handlers on a database whose statements, in or out of a
// transaction, pass through wrap, so a test can pause, rewrite or break one.
func newFixtureWith(t *testing.T, wrap func(db.DBTX) db.DBTX) fixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	now := wednesday
	return fixture{
		tdb: tdb,
		handlers: expense.New(expense.Deps{
			Querier:  db.New(wrap(tdb.Pool)),
			Beginner: wrappingBeginner{Beginner: tdb.Pool, wrap: wrap},
			Now:      func() time.Time { return now },
		}),
		now: &now,
	}
}

// wrappingBeginner hands the handlers transactions whose statements pass through
// wrap, as the Querier's do.
type wrappingBeginner struct {
	auth.Beginner
	wrap func(db.DBTX) db.DBTX
}

func (b wrappingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return wrappedTx{Tx: tx, wrapped: b.wrap(tx)}, nil
}

type wrappedTx struct {
	pgx.Tx
	wrapped db.DBTX
}

func (t wrappedTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.wrapped.Exec(ctx, sql, args...)
}

func (t wrappedTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.wrapped.Query(ctx, sql, args...)
}

func (t wrappedTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.wrapped.QueryRow(ctx, sql, args...)
}

// member creates a Household with one User and a Pocket, and returns the
// Household's id, the Pocket's and the context of a request that User's session
// produced.
func (f fixture) member(t *testing.T, name, email string) (household, pocket pgtype.UUID, ctx context.Context) {
	t.Helper()
	household = f.tdb.CreateHousehold(t, name)
	pocket = f.tdb.CreatePocket(t, household, "Cash")
	return household, pocket, f.userIn(t, household, email)
}

func (f fixture) userIn(t *testing.T, householdID pgtype.UUID, email string) context.Context {
	t.Helper()
	userID := f.tdb.CreateUser(t, householdID, email, "A")
	user, err := f.tdb.Queries.GetUserByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	return auth.WithUser(context.Background(), user)
}

func ptr[T any](v T) *T { return &v }

func idPtr(id pgtype.UUID) *uuid.UUID {
	u := uuid.UUID(id.Bytes)
	return &u
}

// body is the smallest valid create: a fresh id, an amount and a Pocket.
func body(pocket pgtype.UUID) api.CreateExpenseRequest {
	return api.CreateExpenseRequest{Id: uuid.Must(uuid.NewV7()), Amount: "25000", PocketId: pocket.Bytes}
}

func (f fixture) create(ctx context.Context, t *testing.T, req api.CreateExpenseRequest) api.CreateExpenseResponseObject {
	t.Helper()
	resp, err := f.handlers.CreateExpense(ctx, api.CreateExpenseRequestObject{Body: &req})
	if err != nil {
		t.Fatalf("CreateExpense returned an error: %v", err)
	}
	return resp
}

// status is what an answer is, in the shape the tests compare: the code and, for
// a VALIDATION, the field and rule.
func status(resp any) string {
	var code api.ErrorCode
	var args *map[string]interface{}
	switch r := resp.(type) {
	case api.CreateExpense200JSONResponse:
		return "200"
	case api.CreateExpense201JSONResponse:
		return "201"
	case api.CreateExpense400JSONResponse:
		code, args = r.Code, r.Args
	case api.CreateExpense401JSONResponse:
		return "401"
	case api.CreateExpense409JSONResponse:
		code = r.Code
	case api.CreateExpense500JSONResponse:
		return "500"
	case api.UpdateExpense200JSONResponse:
		return "200"
	case api.UpdateExpense400JSONResponse:
		code, args = r.Code, r.Args
	case api.UpdateExpense401JSONResponse:
		return "401"
	case api.UpdateExpense404JSONResponse:
		code = r.Code
	case api.UpdateExpense500JSONResponse:
		return "500"
	case api.GetExpense200JSONResponse:
		return "200"
	case api.GetExpense400JSONResponse:
		code, args = r.Code, r.Args
	case api.GetExpense401JSONResponse:
		return "401"
	case api.GetExpense404JSONResponse:
		code = r.Code
	case api.GetExpense500JSONResponse:
		return "500"
	case api.ListExpenses200JSONResponse:
		return "200"
	case api.ListExpenses400JSONResponse:
		code, args = r.Code, r.Args
	case api.ListExpenses401JSONResponse:
		return "401"
	case api.ListExpenses500JSONResponse:
		return "500"
	default:
		return fmt.Sprintf("unexpected %T", resp)
	}
	if args != nil {
		return fmt.Sprintf("%s %s %s", code, (*args)["field"], (*args)["rule"])
	}
	return string(code)
}

func wire(t *testing.T, resp any) string {
	t.Helper()
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertCreated(t *testing.T, resp api.CreateExpenseResponseObject) api.Expense {
	t.Helper()
	r, ok := resp.(api.CreateExpense201JSONResponse)
	if !ok {
		t.Fatalf("CreateExpense = %s, want 201", status(resp))
	}
	return api.Expense(r)
}

func assertReplayed(t *testing.T, resp api.CreateExpenseResponseObject) api.Expense {
	t.Helper()
	r, ok := resp.(api.CreateExpense200JSONResponse)
	if !ok {
		t.Fatalf("CreateExpense = %s, want 200", status(resp))
	}
	return api.Expense(r)
}

func assertStatus(t *testing.T, resp any, want string) {
	t.Helper()
	if got := status(resp); got != want {
		t.Fatalf("answered %s, want %s", got, want)
	}
}

func (f fixture) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM expenses`).Scan(&n); err != nil {
		t.Fatalf("count expenses: %v", err)
	}
	return n
}

// rows is every Expense as JSON, by id: every column, byte for byte.
func (f fixture) rows(t *testing.T) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(to_jsonb(e)::text, E'\n' ORDER BY id), '') FROM expenses e`).Scan(&s); err != nil {
		t.Fatalf("read expenses: %v", err)
	}
	return s
}

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tdb.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
}

// ---- the create ---------------------------------------------------------------

func TestACreateAnswers201WithEverythingTheServerResolves(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	req := body(pocket)

	got := assertCreated(t, f.create(ctx, t, req))

	if got.Id != req.Id || got.PocketId != pocket.Bytes {
		t.Errorf("id/pocket = %v/%v", got.Id, got.PocketId)
	}
	// Four decimals, always: StringFixed(4), never String().
	if got.Amount != "25000.0000" {
		t.Errorf("amount = %q, want 25000.0000", got.Amount)
	}
	// Stamped by the server from the Household's reporting_currency.
	if got.Currency != "IDR" {
		t.Errorf("currency = %q, want IDR", got.Currency)
	}
	// Absent occurred_on is the writer's Today: 14:00 in Jakarta on the 25th.
	if got.OccurredOn.Format("2006-01-02") != "2026-11-25" {
		t.Errorf("occurred_on = %s, want 2026-11-25", got.OccurredOn.Format("2006-01-02"))
	}
	if got.Ownership != api.Joint || got.OwnerUserId != nil {
		t.Errorf("ownership = %s owner %v, want joint with none", got.Ownership, got.OwnerUserId)
	}
	if got.CapturedAt.IsZero() || got.CapturedAt.Location() != time.UTC {
		t.Errorf("captured_at = %v, want a UTC instant", got.CapturedAt)
	}
	// Optional properties are omitted when empty, never null.
	w := wire(t, got)
	for _, key := range []string{"occurred_time", "category_id", "description", "owner_user_id", "deleted_at"} {
		if strings.Contains(w, `"`+key+`"`) {
			t.Errorf("%s is on the wire for an Expense that has none: %s", key, w)
		}
	}
	if n := f.count(t); n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
}

func TestEveryOptionalPropertyIsStoredAndReturned(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	req := body(pocket)
	req.CategoryId = idPtr(category)
	req.OccurredOn = ptr("2026-11-20")
	req.OccurredTime = ptr("19:05")
	req.Description = ptr("  Lunch with Dana  ")

	got := assertCreated(t, f.create(ctx, t, req))

	if got.CategoryId == nil || *got.CategoryId != category.Bytes {
		t.Errorf("category_id = %v", got.CategoryId)
	}
	if got.OccurredOn.Format("2006-01-02") != "2026-11-20" {
		t.Errorf("occurred_on = %s", got.OccurredOn.Format("2006-01-02"))
	}
	if got.OccurredTime == nil || *got.OccurredTime != "19:05" {
		t.Errorf("occurred_time = %v", got.OccurredTime)
	}
	// Trimmed, as a name is.
	if got.Description == nil || *got.Description != "Lunch with Dana" {
		t.Errorf("description = %v, want it trimmed", got.Description)
	}
}

// A replay answers the Expense as stored now, whatever the second body says, and
// writes nothing.
func TestAReplayIs200WithTheStoredExpenseAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	first := body(pocket)
	created := assertCreated(t, f.create(ctx, t, first))
	before := f.rows(t)

	again := assertReplayed(t, f.create(ctx, t, first))
	if wire(t, again) != wire(t, created) {
		t.Errorf("replay differs from the 201:\n%s\n%s", wire(t, created), wire(t, again))
	}

	// A different body under the same id: neither compared nor applied.
	other := f.tdb.CreatePocket(t, household, "Wallet")
	changed := api.CreateExpenseRequest{Id: first.Id, Amount: "-1", PocketId: other.Bytes, Description: ptr("different")}
	stored := assertReplayed(t, f.create(ctx, t, changed))
	if stored.Amount != "25000.0000" || stored.PocketId != pocket.Bytes || stored.Description != nil {
		t.Errorf("a replay with another body answered %s, want the stored Expense", wire(t, stored))
	}
	if after := f.rows(t); after != before {
		t.Errorf("a replay changed the rows:\n%s\n%s", before, after)
	}
}

// An id the caller's own Household holds as a deleted row, or another
// Household's, is one answer.
func TestAnIdHeldByAnythingButALiveOwnExpenseIsIDTaken(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	otherHousehold := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, otherHousehold, "Theirs")
	theirs := f.tdb.CreateExpense(t, otherHousehold, theirPocket, testutil.ExpenseOpts{})
	deleted := f.tdb.CreateExpense(t, household, pocket, testutil.ExpenseOpts{Deleted: true})
	before := f.rows(t)

	for name, id := range map[string]pgtype.UUID{"another Household's": theirs, "the caller's own deleted": deleted} {
		req := body(pocket)
		req.Id = id.Bytes
		resp := f.create(ctx, t, req)
		assertStatus(t, resp, "ID_TAKEN")
		if got := wire(t, resp); got != `{"code":"ID_TAKEN"}` {
			t.Errorf("%s: wire = %s, want {\"code\":\"ID_TAKEN\"} with no args", name, got)
		}
	}
	if after := f.rows(t); after != before {
		t.Error("an ID_TAKEN changed a row")
	}
}

// A body that is invalid is refused whatever the id holds: a taken id with a bad
// body answers what a free id with a bad body does.
func TestABadBodyIsRefusedWhateverTheIdHolds(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	held := f.tdb.CreateExpense(t, household, pocket, testutil.ExpenseOpts{})

	bad := body(pocket)
	bad.Amount = "0"
	free := f.create(ctx, t, bad)
	bad.Id = held.Bytes
	taken := f.create(ctx, t, bad)

	assertStatus(t, free, "VALIDATION amount nonzero")
	if wire(t, free) != wire(t, taken) {
		t.Errorf("a taken id answered %s, a free one %s", wire(t, taken), wire(t, free))
	}
}

// A retry after the Pocket (or Category) was deactivated meets the stored
// Expense: its references are not looked at again.
func TestAReplayAfterTheReferencesWentInactiveIsStill200(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	req := body(pocket)
	req.CategoryId = idPtr(category)
	assertCreated(t, f.create(ctx, t, req))

	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, pocket)
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, category)

	assertReplayed(t, f.create(ctx, t, req))
	// A new Expense naming either is refused, which is the contrast.
	fresh := body(pocket)
	assertStatus(t, f.create(ctx, t, fresh), "VALIDATION pocket_id inactive")
}

// ---- the amount ---------------------------------------------------------------

func TestZeroIsRefusedInEverySpelling(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	for _, amount := range []string{"0", "0.0000", "-0", "-0.0", "00", "0.0"} {
		req := body(pocket)
		req.Amount = api.SignedMoneyInput(amount)
		assertStatus(t, f.create(ctx, t, req), "VALIDATION amount nonzero")
	}
	if n := f.count(t); n != 0 {
		t.Errorf("%d rows after refused zeros", n)
	}
}

func TestSignedAmountsAreStoredAndAnsweredAtFourDecimals(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	for in, want := range map[string]string{
		"25000":                  "25000.0000",
		"-25000":                 "-25000.0000", // a refund keeps its sign
		"0.0001":                 "0.0001",
		"-0.0001":                "-0.0001",
		"1.5":                    "1.5000",
		"-1.50":                  "-1.5000",
		"9999999999999999.9999":  "9999999999999999.9999",
		"-9999999999999999.9999": "-9999999999999999.9999",
		"007":                    "7.0000",
	} {
		req := body(pocket)
		req.Amount = api.SignedMoneyInput(in)
		got := assertCreated(t, f.create(ctx, t, req))
		if got.Amount != want {
			t.Errorf("%s answered %q, want %q", in, got.Amount, want)
		}
		// And stored at the scale the column holds.
		var stored string
		if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT amount::text FROM expenses WHERE id = $1`, req.Id).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != want {
			t.Errorf("%s stored as %q, want %q", in, stored, want)
		}
	}
}

// ---- the date -----------------------------------------------------------------

// An absent occurred_on is the writer's Today (the User's zone, the Household's
// day_starts_at), across the rollover and for a User in another zone.
func TestAnAbsentDateIsTheWritersToday(t *testing.T) {
	for _, tc := range []struct {
		name, zone string
		now        time.Time
		want       string
	}{
		{"03:59 Jakarta is still yesterday", "Asia/Jakarta", time.Date(2026, 11, 25, 20, 59, 0, 0, time.UTC), "2026-11-25"},
		{"04:00 Jakarta is the new day", "Asia/Jakarta", time.Date(2026, 11, 25, 21, 0, 0, 0, time.UTC), "2026-11-26"},
		{"another zone, on its own clock", "America/New_York", time.Date(2026, 11, 25, 8, 59, 0, 0, time.UTC), "2026-11-24"},
		{"another zone, after its rollover", "America/New_York", time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC), "2026-11-25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			household, pocket, ctx := f.member(t, "Home", "a@example.com")
			f.exec(t, `UPDATE users SET time_zone = $1 WHERE household_id = $2`, tc.zone, household)
			user, _ := auth.UserFromContext(ctx)
			user.TimeZone = tc.zone
			ctx = auth.WithUser(ctx, user)
			*f.now = tc.now

			got := assertCreated(t, f.create(ctx, t, body(pocket)))
			if d := got.OccurredOn.Format("2006-01-02"); d != tc.want {
				t.Errorf("occurred_on = %s, want %s", d, tc.want)
			}
		})
	}
}

// A date the person chose is stored as sent, today's included, and the future
// with no cap.
func TestAChosenDateIsStoredAsSent(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	for _, date := range []string{"2026-11-25", "2026-11-26", "2999-12-31", "2000-02-29", "0001-01-01", "9999-12-31"} {
		req := body(pocket)
		req.OccurredOn = ptr(date)
		got := assertCreated(t, f.create(ctx, t, req))
		if d := got.OccurredOn.Format("2006-01-02"); d != date {
			t.Errorf("%s was stored as %s", date, d)
		}
	}
}

func TestADateThatIsNotARealDayIsInvalidAndNeverA500(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	for _, date := range []string{"2026-02-30", "2026-13-01", "2026-00-10", "2026-11-00", "2026-11-31", "2025-02-29", "0000-01-01", "0000-12-31"} {
		req := body(pocket)
		req.OccurredOn = ptr(date)
		assertStatus(t, f.create(ctx, t, req), "VALIDATION occurred_on invalid")
	}
	if n := f.count(t); n != 0 {
		t.Errorf("%d rows after refused dates", n)
	}
}

func TestATimeIsStoredAsGiven(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	for _, tm := range []string{"00:00", "01:30", "04:00", "23:59"} {
		req := body(pocket)
		req.OccurredTime = ptr(tm)
		got := assertCreated(t, f.create(ctx, t, req))
		if got.OccurredTime == nil || *got.OccurredTime != tm {
			t.Errorf("%s answered %v", tm, got.OccurredTime)
		}
	}
}

// ---- the description ----------------------------------------------------------

func TestTheDescriptionFollowsTheSharedTextRule(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	for text, want := range map[string]string{
		"":                                "VALIDATION description required",
		"   ":                             "VALIDATION description required",
		"\u200b\u200c":                    "VALIDATION description required", // only format characters
		strings.Repeat("a", 201):          "VALIDATION description max",
		strings.Repeat("\U0001F6F6", 201): "VALIDATION description max",
		"nul\x00byte":                     "VALIDATION description pattern",
		strings.Repeat("a", 200):          "201",
		strings.Repeat("\U0001F6F6", 200): "201", // code points, not bytes
		"  padded  ":                      "201",
	} {
		req := body(pocket)
		req.Description = ptr(text)
		if got := status(f.create(ctx, t, req)); got != want {
			t.Errorf("%.30q answered %s, want %s", text, got, want)
		}
	}
}

// ---- the references -----------------------------------------------------------

// An unknown, another Household's and a deleted Pocket answer the same bytes: a
// foreign id is never told from a missing one.
func TestAPocketThatIsNotTheCallersLiveOneIsOneAnswer(t *testing.T) {
	f := newFixture(t)
	household, _, ctx := f.member(t, "Home", "a@example.com")
	theirs := f.tdb.CreatePocket(t, f.tdb.CreateHousehold(t, "Elsewhere"), "Theirs")
	gone := f.tdb.CreatePocket(t, household, "Gone")
	f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, gone)

	var answers []string
	for _, id := range []pgtype.UUID{testutil.NewUUID(t), theirs, gone} {
		resp := f.create(ctx, t, body(id))
		assertStatus(t, resp, "VALIDATION pocket_id invalid")
		answers = append(answers, wire(t, resp))
	}
	if answers[0] != answers[1] || answers[0] != answers[2] {
		t.Errorf("the three differ: %v", answers)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("%d rows after refused references", n)
	}
}

func TestAnInactivePocketIsInactiveAndAnInactiveCategoryIsToo(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")

	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, pocket)
	assertStatus(t, f.create(ctx, t, body(pocket)), "VALIDATION pocket_id inactive")
	f.exec(t, `UPDATE pockets SET active = true WHERE id = $1`, pocket)

	// R10: a Category is treated exactly as a Pocket.
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, category)
	req := body(pocket)
	req.CategoryId = idPtr(category)
	assertStatus(t, f.create(ctx, t, req), "VALIDATION category_id inactive")
	f.exec(t, `UPDATE categories SET active = true WHERE id = $1`, category)
	assertCreated(t, f.create(ctx, t, req))
}

func TestACategoryThatIsNotTheCallersLiveOneIsOneAnswer(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	theirs := f.tdb.CreateCategory(t, f.tdb.CreateHousehold(t, "Elsewhere"), "Theirs", "daily")
	gone := f.tdb.CreateCategory(t, household, "Gone", "daily")
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, gone)

	var answers []string
	for _, id := range []pgtype.UUID{testutil.NewUUID(t), theirs, gone} {
		req := body(pocket)
		req.CategoryId = idPtr(id)
		resp := f.create(ctx, t, req)
		assertStatus(t, resp, "VALIDATION category_id invalid")
		answers = append(answers, wire(t, resp))
	}
	if answers[0] != answers[1] || answers[0] != answers[2] {
		t.Errorf("the three differ: %v", answers)
	}
}

// ownership and owner_user_id are a pair (#140 R6, X12).
func TestOwnershipIsAPair(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	user, _ := auth.UserFromContext(ctx)
	owner := uuid.UUID(user.ID.Bytes)
	colleague := f.tdb.CreateUser(t, household, "b@example.com", "B")
	foreign := f.tdb.CreateUser(t, f.tdb.CreateHousehold(t, "Elsewhere"), "c@example.com", "C")
	deleted := f.tdb.CreateUser(t, household, "d@example.com", "D")
	f.exec(t, `UPDATE users SET deleted_at = now() WHERE id = $1`, deleted)

	with := func(ownership *api.Ownership, id *uuid.UUID) api.CreateExpenseRequest {
		req := body(pocket)
		req.Ownership, req.OwnerUserId = ownership, id
		return req
	}
	sole, joint := ptr(api.SoleOwner), ptr(api.Joint)

	// Both: a live User of the Household.
	for _, id := range []uuid.UUID{owner, colleague.Bytes} {
		got := assertCreated(t, f.create(ctx, t, with(sole, ptr(id))))
		if got.Ownership != api.SoleOwner || got.OwnerUserId == nil || *got.OwnerUserId != id {
			t.Errorf("sole_owner answered %s", wire(t, got))
		}
	}
	// Neither, or joint alone: joint.
	for _, req := range []api.CreateExpenseRequest{with(nil, nil), with(joint, nil)} {
		if got := assertCreated(t, f.create(ctx, t, req)); got.Ownership != api.Joint || got.OwnerUserId != nil {
			t.Errorf("answered %s, want joint with no owner", wire(t, got))
		}
	}
	// Half a pair, or a User who is not the Household's: one answer.
	for name, req := range map[string]api.CreateExpenseRequest{
		"sole_owner with no owner":   with(sole, nil),
		"an owner with no ownership": with(nil, ptr(owner)),
		"joint with an owner":        with(joint, ptr(owner)),
		"another Household's User":   with(sole, idPtr(foreign)),
		"a deleted User":             with(sole, idPtr(deleted)),
		"a User that does not exist": with(sole, ptr(uuid.New())),
	} {
		resp := f.create(ctx, t, req)
		if got := status(resp); got != "VALIDATION owner_user_id invalid" {
			t.Errorf("%s answered %s, want VALIDATION owner_user_id invalid", name, got)
		}
	}
}

// The least (field, rule) is reported, whichever check found it first, and a
// refused body writes nothing.
func TestTheLeastFailureIsTheOneReported(t *testing.T) {
	f := newFixture(t)
	_, _, ctx := f.member(t, "Home", "a@example.com")
	missingPocket := testutil.NewUUID(t)
	missingCategory := testutil.NewUUID(t)

	req := body(missingPocket)
	req.Amount = "0"
	req.OccurredOn = ptr("2026-02-30")
	req.Description = ptr(" ")
	assertStatus(t, f.create(ctx, t, req), "VALIDATION amount nonzero")

	req = body(missingPocket)
	req.OccurredOn = ptr("2026-02-30")
	req.Description = ptr(" ")
	assertStatus(t, f.create(ctx, t, req), "VALIDATION description required")

	// References: category_id sorts before pocket_id, so it is the one reported
	// although the Pocket is locked first.
	req = body(missingPocket)
	req.CategoryId = idPtr(missingCategory)
	assertStatus(t, f.create(ctx, t, req), "VALIDATION category_id invalid")

	// The body's own failures are judged before any reference: a foreign Pocket
	// beside a bad body answers the bad body, as an unknown one would.
	theirs := f.tdb.CreatePocket(t, f.tdb.CreateHousehold(t, "Elsewhere"), "Theirs")
	req = body(theirs)
	req.Amount = "0"
	assertStatus(t, f.create(ctx, t, req), "VALIDATION amount nonzero")
	if n := f.count(t); n != 0 {
		t.Errorf("%d rows after refused requests", n)
	}
}

// An Expense in an inactive Pocket or Category stays what it was, and a deleted
// Pocket or Category named by nothing is no business of an existing Expense.
func TestTenancyEveryColumnOfAnotherHouseholdsExpensesIsUntouched(t *testing.T) {
	f := newFixture(t)
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
	f.tdb.CreateExpense(t, other, theirPocket, testutil.ExpenseOpts{})
	f.tdb.CreateExpense(t, other, theirPocket, testutil.ExpenseOpts{Deleted: true})
	before := f.rows(t)

	assertCreated(t, f.create(ctx, t, body(pocket)))
	// Another Household's Pocket is refused, and nothing of theirs moves.
	assertStatus(t, f.create(ctx, t, body(theirPocket)), "VALIDATION pocket_id invalid")

	for _, line := range strings.Split(before, "\n") {
		if !strings.Contains(f.rows(t), line) {
			t.Errorf("another Household's row changed: %s", line)
		}
	}
	var mine int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM expenses WHERE household_id = $1`, other).Scan(&mine); err != nil {
		t.Fatal(err)
	}
	if mine != 2 {
		t.Errorf("the other Household holds %d rows, want 2", mine)
	}
}

// ---- a failure leaves no trace of the request ---------------------------------

// An amount and a description are user content: nothing logs them, not even
// through a driver error whose Detail names the offending row.
func TestAFailureLogsNeitherTheAmountNorTheDescription(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	leaky := &pgconn.PgError{
		Severity: "ERROR", Code: "23514", ConstraintName: "expenses_description_not_blank",
		// The secrets are in the Message, which is what an error's text prints, so a
		// path that logged the text would leak them; only the SQLSTATE-only
		// branch does not.
		Message: "violates check constraint on (Secret Description, 31337.0000)",
		Detail:  "Failing row contains (Secret Description, 31337.0000).",
	}
	f := newFixtureWith(t, func(tx db.DBTX) db.DBTX {
		return hooked{DBTX: tx, fail: func(sql string) error {
			if strings.Contains(sql, marker(createQuery)) || strings.Contains(sql, marker(listQuery)) {
				return leaky
			}
			return nil
		}}
	})
	_, pocket, ctx := f.member(t, "Home", "a@example.com")
	req := body(pocket)
	req.Amount = "31337"
	req.Description = ptr("Secret Description")

	assertStatus(t, f.create(ctx, t, req), "500")
	resp, err := f.handlers.ListExpenses(ctx, api.ListExpensesRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, resp, "500")

	if buf.Len() == 0 {
		t.Fatal("nothing was logged; the 500 should leave a trace")
	}
	for _, secret := range []string{"Secret Description", "31337"} {
		if strings.Contains(buf.String(), secret) {
			t.Errorf("%q reached the log: %s", secret, buf.String())
		}
	}
	// The branch ran for both: the create's error goes through opaque() and the
	// list's comes straight from the Querier, and each is logged as SQLSTATE and
	// constraint only, never as text (`err=`).
	for _, msg := range []string{"create expense", "list expenses"} {
		want := `msg="` + msg + `" sqlstate=23514 constraint=expenses_description_not_blank`
		if !strings.Contains(buf.String(), want) {
			t.Errorf("the log has no line %q: %s", want, buf.String())
		}
	}
	if strings.Contains(buf.String(), "err=") {
		t.Errorf("a failure was logged as text: %s", buf.String())
	}
}

// ---- helpers shared by the race tests -------------------------------------------

// Statements are found by the name comment sqlc leaves at the head of each, so a
// reflow of the SQL under it does not silently stop a marker matching.
const (
	createQuery         = "CreateExpense"
	updateQuery         = "UpdateExpense"
	listQuery           = "ListExpenses"
	getLiveQuery        = "GetLiveExpense"
	lockHouseholdShared = "LockHouseholdShared"
	lockPocketShared    = "LockLivePocketShared"
	lockCategoryShared  = "LockLiveCategoryShared"
)

// marker is the head of a statement's SQL as sqlc writes it.
func marker(name string) string { return "-- name: " + name + " " }

// hooked wraps a database handle: rewrite changes a statement, fail refuses it,
// before runs ahead of it and after runs once it has finished (for a row, once
// it has been scanned). A test uses them to hold a transaction at a chosen
// statement, which is the only way to make a race happen on purpose.
type hooked struct {
	db.DBTX
	rewrite func(sql string) string
	fail    func(sql string) error
	before  func(sql string)
	after   func(sql string)
}

func (h hooked) prepare(sql string) (string, error) {
	if h.rewrite != nil {
		sql = h.rewrite(sql)
	}
	if h.fail != nil {
		if err := h.fail(sql); err != nil {
			return sql, err
		}
	}
	if h.before != nil {
		h.before(sql)
	}
	return sql, nil
}

func (h hooked) done(sql string) {
	if h.after != nil {
		h.after(sql)
	}
}

func (h hooked) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	sql, err := h.prepare(sql)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	tag, err := h.DBTX.Exec(ctx, sql, args...)
	h.done(sql)
	return tag, err
}

func (h hooked) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	sql, err := h.prepare(sql)
	if err != nil {
		return nil, err
	}
	rows, err := h.DBTX.Query(ctx, sql, args...)
	h.done(sql)
	return rows, err
}

func (h hooked) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	sql, err := h.prepare(sql)
	if err != nil {
		return errRow{err}
	}
	return doneRow{Row: h.DBTX.QueryRow(ctx, sql, args...), after: func() { h.done(sql) }}
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

type doneRow struct {
	pgx.Row
	after func()
}

func (r doneRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	r.after()
	return err
}

// race runs fn on n goroutines released together.
func race[T any](n int, fn func(i int) T) []T {
	out := make([]T, n)
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(n)
	done.Add(n)
	for i := range n {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			out[i] = fn(i)
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return out
}
