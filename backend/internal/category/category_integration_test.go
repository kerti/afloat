package category_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/category"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// The tests here talk to a real Postgres and none is parallel (ADR-0004): they
// share one database that each test empties. The concurrent ones start their own
// goroutines, which is the point of them. Every one pins the clock, so the
// Period Day an allocation is dated is asserted on exact dates.

// 10:00 on 14 Nov 2026 in Jakarta, the User's zone.
const day14 = "2026-11-14T10:00:00+07:00"

type fixture struct {
	tdb      *testutil.TestDB
	now      *time.Time
	mu       *sync.Mutex
	handlers *category.Handlers
	budget   *budget.Handlers
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return newFixtureWith(t, nil, nil)
}

// newFixtureWith wraps the real Querier and Beginner, so a test can break one
// call.
func newFixtureWith(t *testing.T, wrapQ func(db.Querier) db.Querier, wrapTx func(budget.Beginner) budget.Beginner) fixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	start, err := time.Parse(time.RFC3339, day14)
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{tdb: tdb, now: &start, mu: &sync.Mutex{}}
	var q db.Querier = tdb.Queries
	if wrapQ != nil {
		q = wrapQ(q)
	}
	var tx budget.Beginner = tdb.Pool
	if wrapTx != nil {
		tx = wrapTx(tx)
	}
	f.handlers = category.New(category.Deps{Querier: q, Beginner: tx, Now: f.clock})
	f.budget = budget.New(budget.Deps{Querier: tdb.Queries, Beginner: tdb.Pool, Now: f.clock})
	return f
}

func (f fixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.now
}

func (f fixture) setClock(t *testing.T, rfc3339 string) {
	t.Helper()
	at, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.now = at
}

// member creates a Household with one User and returns the Household's id and
// the context of a request that User's session produced.
func (f fixture) member(t *testing.T, name, email string) (pgtype.UUID, context.Context) {
	t.Helper()
	householdID := f.tdb.CreateHousehold(t, name)
	return householdID, f.userIn(t, householdID, email)
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

func newID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

// amount is a request's money-or-null property: nil leaves it out, which for an
// allocation means the same as null.
func amount(s *string) nullable.Nullable[string] {
	if s == nil {
		return nil
	}
	return nullable.NewNullableWithValue(*s)
}

// create sends a create; allocation nil sends none.
func (f fixture) create(ctx context.Context, t *testing.T, id uuid.UUID, name string, bucket api.Bucket, allocation *string) api.CreateCategoryResponseObject {
	t.Helper()
	resp, err := f.handlers.CreateCategory(ctx, api.CreateCategoryRequestObject{
		Body: &api.CreateCategoryRequest{Id: id, CategoryDisplayName: name, Bucket: bucket, Allocation: amount(allocation)},
	})
	if err != nil {
		t.Fatalf("CreateCategory returned an error: %v", err)
	}
	return resp
}

func (f fixture) list(ctx context.Context, t *testing.T) []api.Category {
	t.Helper()
	resp, err := f.handlers.ListCategories(ctx, api.ListCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListCategories returned an error: %v", err)
	}
	got, is := resp.(api.ListCategories200JSONResponse)
	if !is {
		t.Fatalf("ListCategories = %#v, want 200", resp)
	}
	return got.Items
}

func (f fixture) getBudget(ctx context.Context, t *testing.T) api.Budget {
	t.Helper()
	resp, err := f.budget.GetBudget(ctx, api.GetBudgetRequestObject{})
	if err != nil {
		t.Fatalf("GetBudget returned an error: %v", err)
	}
	got, is := resp.(api.GetBudget200JSONResponse)
	if !is {
		t.Fatalf("GetBudget = %#v, want 200", resp)
	}
	return api.Budget(got)
}

// row is the Category's whole row as JSON: every column, byte for byte.
func (f fixture) row(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT to_jsonb(c)::text FROM categories c WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read category: %v", err)
	}
	return s
}

func (f fixture) scalar(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
	return s
}

func (f fixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// allocations is every allocation row as "year_month effective_on name amount".
func (f fixture) allocations(t *testing.T) string {
	t.Helper()
	return f.scalar(t, `SELECT coalesce(string_agg(a.year_month || ' ' || a.effective_on || ' ' || c.category_display_name || ' ' || coalesce(a.allocation::text, 'none'),
	                                       '; ' ORDER BY a.year_month, a.effective_on, c.category_display_name), '')
		FROM allocations a JOIN categories c ON c.id = a.category_id`)
}

func (f fixture) allocationRows(t *testing.T) string {
	t.Helper()
	return f.scalar(t, `SELECT coalesce(string_agg(to_jsonb(a)::text, E'\n' ORDER BY year_month, effective_on, category_id), '') FROM allocations a`)
}

func (f fixture) markers(t *testing.T) string {
	t.Helper()
	return f.scalar(t, `SELECT coalesce(string_agg(year_month, ',' ORDER BY year_month), '') FROM budgets`)
}

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tdb.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
}

func assertCreated(t *testing.T, resp api.CreateCategoryResponseObject) api.Category {
	t.Helper()
	got, is := resp.(api.CreateCategory201JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 201", resp)
	}
	return api.Category(got)
}

func assertReplayed(t *testing.T, resp api.CreateCategoryResponseObject) api.Category {
	t.Helper()
	got, is := resp.(api.CreateCategory200JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 200", resp)
	}
	return api.Category(got)
}

func assertConflict(t *testing.T, resp api.CreateCategoryResponseObject, code api.ErrorCode) {
	t.Helper()
	got, is := resp.(api.CreateCategory409JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 409 %s", resp, code)
	}
	if got.Code != code || got.Args != nil {
		t.Errorf("409 = %s %v, want %s with no args", got.Code, got.Args, code)
	}
}

func assertValidation(t *testing.T, resp api.CreateCategoryResponseObject, field, rule string) {
	t.Helper()
	got, is := resp.(api.CreateCategory400JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 400", resp)
	}
	if got.Code != api.VALIDATION {
		t.Fatalf("code = %s, want VALIDATION", got.Code)
	}
	args := *got.Args
	if args["field"] != field || args["rule"] != rule {
		t.Errorf("args = %v, want field %s rule %s", args, field, rule)
	}
}

func assertInternal(t *testing.T, resp api.CreateCategoryResponseObject) {
	t.Helper()
	got, is := resp.(api.CreateCategory500JSONResponse)
	if !is || got.Code != api.INTERNAL {
		t.Fatalf("response = %#v, want 500 INTERNAL", resp)
	}
}

func wireOf(t *testing.T, resp api.CreateCategoryResponseObject) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitCreateCategoryResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

// ---- the Pocket table of rows, for Categories ------------------------------

func TestCreateAnswers201ThenAReplayAnswers200(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)

	created := assertCreated(t, f.create(ctx, t, id, "Groceries", api.Daily, nil))
	if created.Id != id || created.CategoryDisplayName != "Groceries" || created.Bucket != api.Daily || !created.Active {
		t.Errorf("created = %+v, want the id, the name, the bucket and active", created)
	}
	before := f.row(t, id)

	replayed := assertReplayed(t, f.create(ctx, t, id, "Groceries", api.Daily, nil))
	if replayed != created {
		t.Errorf("replay = %+v, want %+v", replayed, created)
	}
	if got := f.row(t, id); got != before {
		t.Errorf("row changed by a replay:\n%s\n%s", before, got)
	}
	if n := f.count(t, "categories"); n != 1 {
		t.Errorf("categories = %d, want 1", n)
	}
}

// The body of a replay is neither compared nor applied: the answer is the
// Category as stored, so a retry after another member's rename sees the rename,
// and a different bucket in the body changes nothing.
func TestAReplayWithADifferentBodyAnswersTheStoredCategoryAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Groceries", api.Daily, nil))
	f.exec(t, `UPDATE categories SET category_display_name = 'Food', active = false WHERE id = $1`, id)
	before := f.row(t, id)

	got := assertReplayed(t, f.create(ctx, t, id, "Rent", api.Commitment, ptr("3000000")))
	if got.CategoryDisplayName != "Food" || got.Bucket != api.Daily || got.Active {
		t.Errorf("replay = %+v, want the stored Food, daily, inactive", got)
	}
	if after := f.row(t, id); after != before {
		t.Errorf("row changed by a replay:\n%s\n%s", before, after)
	}
}

// A replay applies no allocation (BOOTSTRAP.md §4): not for a Category that
// had none, and not a different one for a Category that had one.
func TestAReplayAppliesNoAllocation(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	plain, planned := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, plain, "Groceries", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, planned, "Rent", api.Commitment, ptr("3000000")))
	before := f.allocationRows(t)

	assertReplayed(t, f.create(ctx, t, plain, "Groceries", api.Daily, ptr("500")))
	assertReplayed(t, f.create(ctx, t, planned, "Rent", api.Commitment, ptr("9999999")))
	assertReplayed(t, f.create(ctx, t, planned, "Rent", api.Commitment, ptr("0")))

	if after := f.allocationRows(t); after != before {
		t.Errorf("allocations changed by a replay:\n%s\n%s", before, after)
	}
	if v := f.allocations(t); v != "2026-11 2026-11-14 Rent 3000000.0000" {
		t.Errorf("allocations = %q", v)
	}
}

func TestAReplayWhoseBodyNameIsAnotherCategorysAnswers200NotNameTaken(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	first, second := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, first, "Food", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, second, "Transport", api.Daily, nil))

	got := assertReplayed(t, f.create(ctx, t, second, "food", api.Daily, nil))
	if got.CategoryDisplayName != "Transport" {
		t.Errorf("replay = %+v, want the stored Transport", got)
	}
}

// A deleted id is never reused, and answers what another Household's id answers,
// byte for byte.
func TestAnIdHeldByADeletedOrForeignCategoryIsIDTakenInOneAnswer(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, foreign := f.member(t, "Other", "b@example.com")

	deleted := newID(t)
	assertCreated(t, f.create(ctx, t, deleted, "Old", api.Daily, nil))
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, deleted)
	deletedBefore := f.row(t, deleted)

	theirs := newID(t)
	assertCreated(t, f.create(foreign, t, theirs, "Theirs", api.Daily, nil))
	theirsBefore := f.row(t, theirs)

	for _, withAllocation := range []*string{nil, ptr("1000")} {
		own := f.create(ctx, t, deleted, "Old", api.Daily, withAllocation)
		other := f.create(ctx, t, theirs, "Mine", api.Daily, withAllocation)
		assertConflict(t, own, api.IDTAKEN)
		assertConflict(t, other, api.IDTAKEN)
		ownStatus, ownBody := wireOf(t, own)
		otherStatus, otherBody := wireOf(t, other)
		if ownStatus != otherStatus || ownBody != otherBody {
			t.Errorf("own-deleted answers %d %s, foreign answers %d %s; want one answer", ownStatus, ownBody, otherStatus, otherBody)
		}
	}
	if got := f.row(t, deleted); got != deletedBefore {
		t.Errorf("deleted row changed:\n%s\n%s", deletedBefore, got)
	}
	if got := f.row(t, theirs); got != theirsBefore {
		t.Errorf("the other Household's row changed:\n%s\n%s", theirsBefore, got)
	}
	if n := f.count(t, "categories"); n != 2 {
		t.Errorf("categories = %d, want 2: a refused create writes nothing", n)
	}
	if n := f.count(t, "allocations"); n != 0 {
		t.Errorf("allocations = %d, want 0: a refused create writes none", n)
	}
}

// A body that is invalid is refused whatever the id holds.
func TestAnInvalidBodyIsRefusedBeforeTheIdIsConsidered(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, foreign := f.member(t, "Other", "b@example.com")
	mine, theirs, deleted := newID(t), newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, mine, "Food", api.Daily, nil))
	assertCreated(t, f.create(foreign, t, theirs, "Theirs", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, deleted, "Old", api.Daily, nil))
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, deleted)

	for name, id := range map[string]uuid.UUID{"free": newID(t), "own": mine, "foreign": theirs, "own and deleted": deleted} {
		t.Run(name, func(t *testing.T) {
			assertValidation(t, f.create(ctx, t, id, "   ", api.Daily, nil), "category_display_name", "required")
			// A Commitment with no allocation is refused before the id too.
			assertValidation(t, f.create(ctx, t, id, "Rent", api.Commitment, nil), "allocation", "required")
		})
	}
}

// The name rule is checked before the allocation: a body with both wrong is the
// name's 400.
func TestTheNameRuleIsCheckedBeforeTheAllocationRule(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	assertValidation(t, f.create(ctx, t, newID(t), "  ", api.Commitment, nil), "category_display_name", "required")
	assertValidation(t, f.create(ctx, t, newID(t), strings.Repeat("x", 61), api.Commitment, nil), "category_display_name", "max")
}

func TestANewCategoryIsActiveAndTheNameIsStoredTrimmed(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	got := assertCreated(t, f.create(ctx, t, id, "  Groceries \t", api.Daily, nil))
	if got.CategoryDisplayName != "Groceries" || !got.Active {
		t.Errorf("created = %+v", got)
	}
	if name := f.scalar(t, `SELECT category_display_name FROM categories WHERE id = $1`, id); name != "Groceries" {
		t.Errorf("stored name = %q, want it trimmed", name)
	}
}

// Names are unique across both buckets while not deleted, active or inactive,
// compared case-insensitively and trimmed, and per Household.
func TestNamesAreUniqueAcrossBucketsWithinAHouseholdWhileNotDeleted(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, other := f.member(t, "Other", "b@example.com")
	first := newID(t)
	assertCreated(t, f.create(ctx, t, first, "Insurance", api.Commitment, ptr("500000")))

	for _, tc := range []struct {
		name   string
		bucket api.Bucket
		alloc  *string
	}{
		{"insurance", api.Commitment, ptr("1")},
		{"INSURANCE", api.Daily, nil},     // the other bucket is the same name
		{"  Insurance  ", api.Daily, nil}, // compared trimmed
		{"InSuRaNcE", api.Daily, ptr("1")},
	} {
		assertConflict(t, f.create(ctx, t, newID(t), tc.name, tc.bucket, tc.alloc), api.NAMETAKEN)
	}
	// An inactive one still holds its name.
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, first)
	assertConflict(t, f.create(ctx, t, newID(t), "Insurance", api.Daily, nil), api.NAMETAKEN)
	// Another Household's does not.
	assertCreated(t, f.create(other, t, newID(t), "Insurance", api.Commitment, ptr("1")))
	// A deleted one releases it.
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, first)
	assertCreated(t, f.create(ctx, t, newID(t), "insurance", api.Daily, nil))
}

// A refused name writes nothing: no Category, no allocation, no marker, and the
// id stays free.
func TestARefusedNameWritesNothingAndTheIdStaysFree(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	assertCreated(t, f.create(ctx, t, newID(t), "Rent", api.Commitment, ptr("3000000")))
	allocationsBefore, markersBefore := f.allocationRows(t), f.markers(t)

	id := newID(t)
	assertConflict(t, f.create(ctx, t, id, "rent", api.Commitment, ptr("9")), api.NAMETAKEN)

	if n := f.count(t, "categories"); n != 1 {
		t.Errorf("categories = %d, want 1", n)
	}
	if f.allocationRows(t) != allocationsBefore || f.markers(t) != markersBefore {
		t.Error("a refused name changed the Budget")
	}
	assertCreated(t, f.create(ctx, t, id, "Rent 2", api.Commitment, ptr("1")))
}

// ---- the Commitment's allocation (PRD S8, #130 C9) -------------------------

func TestACommitmentWithoutAnAllocationIsRefusedAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)

	assertValidation(t, f.create(ctx, t, id, "Rent", api.Commitment, nil), "allocation", "required")

	if f.count(t, "categories") != 0 || f.count(t, "allocations") != 0 || f.markers(t) != "" {
		t.Error("a refused Commitment wrote something")
	}
	assertCreated(t, f.create(ctx, t, id, "Rent", api.Commitment, ptr("1")))
}

func TestACommitmentAllocatedZeroIsAccepted(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	assertCreated(t, f.create(ctx, t, newID(t), "Retired gym", api.Commitment, ptr("0")))
	if v := f.allocations(t); v != "2026-11 2026-11-14 Retired gym 0.0000" {
		t.Errorf("allocations = %q: zero is an allocation", v)
	}
}

func TestADailyCategoryMayHaveNoAllocationOrOne(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	assertCreated(t, f.create(ctx, t, newID(t), "Groceries", api.Daily, nil))
	if n := f.count(t, "allocations"); n != 0 || f.markers(t) != "" {
		t.Errorf("a Daily Category with none wrote allocations %d, markers %q: it is the single statement a Pocket's is", n, f.markers(t))
	}
	assertCreated(t, f.create(ctx, t, newID(t), "Transport", api.Daily, ptr("1500000")))
	if v := f.allocations(t); v != "2026-11 2026-11-14 Transport 1500000.0000" {
		t.Errorf("allocations = %q", v)
	}
}

// The allocation is dated the writer's Today, the Period Day that ends at the
// Household's day_starts_at and not at midnight, and lands in the Period Today
// belongs to.
func TestTheAllocationIsDatedTheWritersTodayInTheirOwnZone(t *testing.T) {
	for _, tc := range []struct {
		name, now, zone, wantRow string
	}{
		{"midday", "2026-11-14T10:00:00+07:00", "Asia/Jakarta", "2026-11 2026-11-14"},
		{"03:59:59 is still yesterday", "2026-11-15T03:59:59+07:00", "Asia/Jakarta", "2026-11 2026-11-14"},
		{"04:00 starts the day", "2026-11-15T04:00:00+07:00", "Asia/Jakarta", "2026-11 2026-11-15"},
		{"the last day of a Period", "2026-12-01T03:59:59+07:00", "Asia/Jakarta", "2026-11 2026-11-30"},
		{"the first day of the next", "2026-12-01T04:00:00+07:00", "Asia/Jakarta", "2026-12 2026-12-01"},
		{"another zone, the same instant", "2026-12-01T04:00:00+07:00", "America/Los_Angeles", "2026-11 2026-11-30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			_, ctx := f.member(t, "Home", "a@example.com")
			f.exec(t, `UPDATE users SET time_zone = $1`, tc.zone)
			ctx = f.refresh(ctx, t)
			f.setClock(t, tc.now)

			assertCreated(t, f.create(ctx, t, newID(t), "Rent", api.Commitment, ptr("3000000")))

			if v := f.allocations(t); v != tc.wantRow+" Rent 3000000.0000" {
				t.Errorf("allocations = %q, want %q", v, tc.wantRow+" Rent 3000000.0000")
			}
		})
	}
}

// refresh re-reads the context's User, after a test changed the row.
func (f fixture) refresh(ctx context.Context, t *testing.T) context.Context {
	t.Helper()
	user, _ := auth.UserFromContext(ctx)
	fresh, err := f.tdb.Queries.GetUserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	return auth.WithUser(context.Background(), fresh)
}

// A create that carries an allocation is a first read of the Period (#130 C5):
// it brings the Period into being, carrying the earlier one forward, in the
// same transaction.
func TestACreateWithAnAllocationMaterialisesThePeriodAndCarriesTheLastForward(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-10-10T10:00:00+07:00")
	rent := newID(t)
	assertCreated(t, f.create(ctx, t, rent, "Rent", api.Commitment, ptr("3000000")))
	if _, err := f.budget.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{MonthlyBudget: ptr("9000000")}}); err != nil {
		t.Fatal(err)
	}

	f.setClock(t, "2026-11-03T10:00:00+07:00")
	assertCreated(t, f.create(ctx, t, newID(t), "School", api.Commitment, ptr("500000")))

	if got := f.markers(t); got != "2026-10,2026-11" {
		t.Errorf("budgets = %q, want November materialised by the create", got)
	}
	if v := f.allocations(t); v != "2026-10 2026-10-10 Rent 3000000.0000; 2026-11 2026-11-01 Rent 3000000.0000; 2026-11 2026-11-03 School 500000.0000" {
		t.Errorf("allocations = %q: want the copy dated the 1st and the new one dated the 3rd", v)
	}
	got := f.getBudget(ctx, t)
	if got.CommitmentsBudget != "3500000.0000" || *got.DailyPool != "5500000.0000" || len(got.Allocations) != 2 {
		t.Errorf("Budget: commitments %s pool %s allocations %d", got.CommitmentsBudget, *got.DailyPool, len(got.Allocations))
	}
}

// A Commitment moves the Daily Pool and a Daily allocation only Unallocated,
// through the whole round trip: create, then read.
func TestACommitmentsAllocationMovesThePoolAndADailyOnesOnlyUnallocated(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	if _, err := f.budget.UpdateBudget(ctx, api.UpdateBudgetRequestObject{Body: &api.UpdateBudgetJSONRequestBody{MonthlyBudget: ptr("9000000")}}); err != nil {
		t.Fatal(err)
	}

	assertCreated(t, f.create(ctx, t, newID(t), "Rent", api.Commitment, ptr("3000000")))
	got := f.getBudget(ctx, t)
	if got.CommitmentsBudget != "3000000.0000" || *got.DailyPool != "6000000.0000" || *got.Unallocated != "6000000.0000" {
		t.Errorf("after a Commitment: %s / %s / %s, want 3000000.0000 / 6000000.0000 / 6000000.0000", got.CommitmentsBudget, *got.DailyPool, *got.Unallocated)
	}
	assertCreated(t, f.create(ctx, t, newID(t), "Groceries", api.Daily, ptr("2000000")))
	got = f.getBudget(ctx, t)
	if got.CommitmentsBudget != "3000000.0000" || *got.DailyPool != "6000000.0000" || *got.Unallocated != "4000000.0000" {
		t.Errorf("after a Daily one: %s / %s / %s, want 3000000.0000 / 6000000.0000 / 4000000.0000", got.CommitmentsBudget, *got.DailyPool, *got.Unallocated)
	}
	// Money is a string at four decimals, however it was typed.
	for _, a := range got.Allocations {
		if !strings.HasSuffix(a.Allocation, ".0000") {
			t.Errorf("allocation %q is not four decimals", a.Allocation)
		}
	}
}

// The allocation is the exact amount: four decimals in, four out.
func TestTheAllocationIsStoredExactly(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	for _, tc := range []struct{ in, want string }{
		{"0", "0.0000"}, {"5", "5.0000"}, {"5.5", "5.5000"}, {"0.0001", "0.0001"},
		{"9999999999999999.9999", "9999999999999999.9999"}, {"1000000", "1000000.0000"},
	} {
		id := newID(t)
		assertCreated(t, f.create(ctx, t, id, "C "+tc.in, api.Daily, ptr(tc.in)))
		if got := f.scalar(t, `SELECT allocation::text FROM allocations WHERE category_id = $1`, id); got != tc.want {
			t.Errorf("%s stored as %s, want %s", tc.in, got, tc.want)
		}
	}
}

// ---- the list --------------------------------------------------------------

func TestAnEmptyHouseholdListsAnEmptyNonNilSlice(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	if got := f.list(ctx, t); got == nil || len(got) != 0 {
		t.Errorf("list = %#v, want an empty non-nil slice", got)
	}
}

func TestTheListIsInCreationOrderBothBucketsTogetherAndOnlyTheHouseholdsLiveOnes(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, other := f.member(t, "Other", "b@example.com")
	zeta, alpha, gone := newID(t), newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, zeta, "Zeta", api.Commitment, ptr("1")))
	assertCreated(t, f.create(ctx, t, alpha, "Alpha", api.Daily, nil))
	assertCreated(t, f.create(ctx, t, gone, "Gone", api.Daily, nil))
	assertCreated(t, f.create(other, t, newID(t), "Theirs", api.Daily, nil))
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, gone)
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, zeta)

	got := f.list(ctx, t)
	want := []api.Category{
		{Id: zeta, CategoryDisplayName: "Zeta", Bucket: api.Commitment, Active: false},
		{Id: alpha, CategoryDisplayName: "Alpha", Bucket: api.Daily, Active: true},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("list = %+v, want %+v", got, want)
	}

	// A rename never moves a Category in the order.
	f.exec(t, `UPDATE categories SET category_display_name = 'Aardvark', updated_at = now() WHERE id = $1`, zeta)
	if got := f.list(ctx, t); got[0].Id != zeta {
		t.Errorf("a rename moved the Category: %+v", got)
	}
}

func TestEqualCreationInstantsOrderByID(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	low := uuid.MustParse("019212e0-0000-7000-8000-000000000001")
	high := uuid.MustParse("019212e0-0000-7000-8000-000000000002")
	f.exec(t, `INSERT INTO categories (id, household_id, category_display_name, bucket, created_at) VALUES
		($1, $3, 'B', 'daily', '2026-01-01T00:00:00Z'), ($2, $3, 'A', 'daily', '2026-01-01T00:00:00Z')`, high, low, householdID)
	if got := f.list(ctx, t); got[0].Id != low || got[1].Id != high {
		t.Errorf("list = %+v, want the tie broken by id", got)
	}
}

func TestTwoUsersInOneHouseholdSeeTheSameCategories(t *testing.T) {
	f := newFixture(t)
	householdID, first := f.member(t, "Home", "a@example.com")
	second := f.userIn(t, householdID, "b@example.com")
	id := newID(t)
	assertCreated(t, f.create(first, t, id, "Rent", api.Commitment, ptr("1")))

	if got := f.list(second, t); len(got) != 1 || got[0].Id != id {
		t.Errorf("the second member sees %+v, want the first's Category", got)
	}
	// And a retry from the other member is a replay of the same Category.
	assertReplayed(t, f.create(second, t, id, "Rent", api.Commitment, ptr("1")))
}

func TestACreateWithoutAUserIs401AndWritesNothing(t *testing.T) {
	f := newFixture(t)
	resp, err := f.handlers.CreateCategory(context.Background(), api.CreateCategoryRequestObject{
		Body: &api.CreateCategoryRequest{Id: newID(t), CategoryDisplayName: "Rent", Bucket: api.Daily},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, is := resp.(api.CreateCategory401JSONResponse); !is || got.Code != api.UNAUTHORIZED {
		t.Fatalf("response = %#v, want 401 UNAUTHORIZED", resp)
	}
	if n := f.count(t, "categories"); n != 0 {
		t.Errorf("categories = %d, want 0", n)
	}
	lresp, err := f.handlers.ListCategories(context.Background(), api.ListCategoriesRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if got, is := lresp.(api.ListCategories401JSONResponse); !is || got.Code != api.UNAUTHORIZED {
		t.Fatalf("list response = %#v, want 401", lresp)
	}
}

func TestANilBodyOrAnUnknownBucketIsInvalidJSONBody(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	for name, body := range map[string]*api.CreateCategoryRequest{
		"nil":            nil,
		"unknown bucket": {Id: newID(t), CategoryDisplayName: "x", Bucket: "weekly"},
	} {
		resp, err := f.handlers.CreateCategory(ctx, api.CreateCategoryRequestObject{Body: body})
		if err != nil {
			t.Fatal(err)
		}
		if got, is := resp.(api.CreateCategory400JSONResponse); !is || got.Code != api.INVALIDJSONBODY {
			t.Errorf("%s: response = %#v, want 400 INVALID_JSON_BODY", name, resp)
		}
	}
	if n := f.count(t, "categories"); n != 0 {
		t.Errorf("categories = %d, want 0", n)
	}
}

// ---- tenancy ---------------------------------------------------------------

// snapshotOf is every column of a Household's Categories, Budget markers and
// allocations, byte for byte.
func (f fixture) snapshotOf(t *testing.T, householdName string) string {
	t.Helper()
	hh := `(SELECT id FROM households WHERE household_display_name = '` + householdName + `')`
	return strings.Join([]string{
		f.scalar(t, `SELECT coalesce(string_agg(to_jsonb(c)::text, E'\n' ORDER BY id), '') FROM categories c WHERE household_id = `+hh),
		f.scalar(t, `SELECT coalesce(string_agg(to_jsonb(b)::text, E'\n' ORDER BY year_month), '') FROM budgets b WHERE household_id = `+hh),
		f.scalar(t, `SELECT coalesce(string_agg(to_jsonb(a)::text, E'\n' ORDER BY year_month, effective_on, category_id), '') FROM allocations a WHERE household_id = `+hh),
	}, "\n--\n")
}

// Two Households. Everything one does leaves every column of the other's
// Categories, Budgets and allocations byte-identical; the other's id answers what
// an own deleted id answers; and the other's allocations are on nobody's Budget
// but theirs. Proven red against statements whose WHERE lost household_id (see
// the PR).
func TestTenancyNothingCrossesHouseholds(t *testing.T) {
	f := newFixture(t)
	_, alice := f.member(t, "Alice", "a@example.com")
	_, bob := f.member(t, "Bob", "b@example.com")
	rent, food, gone := newID(t), newID(t), newID(t)
	assertCreated(t, f.create(alice, t, rent, "Rent", api.Commitment, ptr("3000000")))
	assertCreated(t, f.create(alice, t, food, "Food", api.Daily, ptr("1000000")))
	assertCreated(t, f.create(alice, t, gone, "Gone", api.Daily, ptr("5")))
	f.exec(t, `UPDATE categories SET active = false WHERE id = $1`, food)
	f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, gone)
	before := f.snapshotOf(t, "Alice")

	t.Run("Bob's list holds none of Alice's", func(t *testing.T) {
		if got := f.list(bob, t); len(got) != 0 {
			t.Fatalf("Bob sees %+v, want nothing", got)
		}
	})
	t.Run("Bob's Budget lists none of Alice's allocations and counts none", func(t *testing.T) {
		got := f.getBudget(bob, t)
		if len(got.Allocations) != 0 || got.CommitmentsBudget != "0.0000" {
			t.Fatalf("Bob's Budget = %+v, want none of Alice's", got)
		}
	})
	t.Run("Bob creating with each of Alice's ids is ID_TAKEN, with or without an allocation, and Alice's rows are untouched", func(t *testing.T) {
		for _, id := range []uuid.UUID{rent, food, gone} {
			assertConflict(t, f.create(bob, t, id, "Mine", api.Daily, nil), api.IDTAKEN)
			assertConflict(t, f.create(bob, t, id, "Mine", api.Commitment, ptr("1")), api.IDTAKEN)
		}
		if after := f.snapshotOf(t, "Alice"); after != before {
			t.Errorf("Alice's rows changed:\n%s\n%s", before, after)
		}
		if n := f.count(t, "categories"); n != 3 {
			t.Errorf("categories = %d, want 3", n)
		}
	})
	t.Run("a foreign live id answers what an own deleted id answers", func(t *testing.T) {
		bobDeleted := newID(t)
		assertCreated(t, f.create(bob, t, bobDeleted, "Old", api.Daily, nil))
		f.exec(t, `UPDATE categories SET deleted_at = now() WHERE id = $1`, bobDeleted)
		foreignStatus, foreignBody := wireOf(t, f.create(bob, t, rent, "x", api.Daily, nil))
		ownStatus, ownBody := wireOf(t, f.create(bob, t, bobDeleted, "x", api.Daily, nil))
		if foreignStatus != ownStatus || foreignBody != ownBody {
			t.Errorf("foreign %d %s, own deleted %d %s", foreignStatus, foreignBody, ownStatus, ownBody)
		}
	})
	t.Run("Bob's own create, with an allocation, lands in his Household, shares Alice's name freely and leaves her rows alone", func(t *testing.T) {
		assertCreated(t, f.create(bob, t, newID(t), "Rent", api.Commitment, ptr("777")))
		got := f.getBudget(bob, t)
		if got.CommitmentsBudget != "777.0000" || len(got.Allocations) != 1 {
			t.Errorf("Bob's Budget = %+v, want only his own", got)
		}
		if after := f.snapshotOf(t, "Alice"); after != before {
			t.Errorf("Alice's rows changed:\n%s\n%s", before, after)
		}
	})
	t.Run("Alice sees her own", func(t *testing.T) {
		if got := f.list(alice, t); len(got) != 2 {
			t.Errorf("Alice sees %+v, want her two live Categories", got)
		}
		b := f.getBudget(alice, t)
		if len(b.Allocations) != 2 || b.CommitmentsBudget != "3000000.0000" {
			t.Errorf("Alice's Budget = %+v, want her two allocations (the deleted one gone)", b)
		}
	})
}

// household_id comes from the session. A body cannot name one: the contract
// refuses it as an unknown property, and the handler has no field to read.
func TestACreateLandsInTheCallersHousehold(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	f.member(t, "Other", "b@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Rent", api.Commitment, ptr("1")))

	for _, query := range []string{
		`SELECT household_id FROM categories WHERE id = $1`,
		`SELECT household_id FROM allocations WHERE category_id = $1`,
	} {
		var got pgtype.UUID
		if err := f.tdb.Pool.QueryRow(context.Background(), query, id).Scan(&got); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if got != householdID {
			t.Errorf("%s: household_id = %v, want the caller's %v", query, got, householdID)
		}
	}
	if got := f.scalar(t, `SELECT count(*)::text FROM budgets WHERE household_id <> $1`, householdID); got != "0" {
		t.Errorf("the other Household has %s budgets rows, want none", got)
	}
}

// ---- concurrency -----------------------------------------------------------

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

func statusOf(resp api.CreateCategoryResponseObject) string {
	switch r := resp.(type) {
	case api.CreateCategory201JSONResponse:
		return "201"
	case api.CreateCategory200JSONResponse:
		return "200"
	case api.CreateCategory409JSONResponse:
		return "409 " + string(r.Code)
	case api.CreateCategory500JSONResponse:
		return "500"
	case api.CreateCategory400JSONResponse:
		return "400"
	}
	return fmt.Sprintf("%T", resp)
}

func tally(results []string) map[string]int {
	counts := map[string]int{}
	for _, r := range results {
		counts[r]++
	}
	return counts
}

// N POSTs of one id, with and without an allocation: exactly one 201, the rest
// 200, one Category, one allocation row and one marker. Proven red against a
// SELECT-then-INSERT implementation (#130 C9).
func TestConcurrentCreatesOfOneIDCreateOneCategoryAndOneAllocation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		bucket      api.Bucket
		allocation  *string
		wantAllocs  int
		wantMarkers string
	}{
		{"without an allocation", api.Daily, nil, 0, ""},
		{"with an allocation", api.Commitment, ptr("3000000"), 1, "2026-11"},
		{"a Daily one with an allocation", api.Daily, ptr("1000"), 1, "2026-11"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			_, ctx := f.member(t, "Home", "a@example.com")
			const n = 24

			for round := range 4 {
				f.exec(t, `DELETE FROM allocations`)
				f.exec(t, `DELETE FROM budgets`)
				f.exec(t, `DELETE FROM categories`)
				id := newID(t)
				name := fmt.Sprintf("Rent %d", round)
				results := race(n, func(int) string { return statusOf(f.create(ctx, t, id, name, tc.bucket, tc.allocation)) })
				counts := tally(results)
				if counts["201"] != 1 || counts["200"] != n-1 {
					t.Fatalf("round %d: statuses = %v, want one 201 and %d 200", round, counts, n-1)
				}
				if got := f.count(t, "categories"); got != 1 {
					t.Fatalf("round %d: categories = %d, want 1", round, got)
				}
				if got := f.count(t, "allocations"); got != tc.wantAllocs {
					t.Fatalf("round %d: allocations = %d, want %d", round, got, tc.wantAllocs)
				}
				if got := f.markers(t); got != tc.wantMarkers {
					t.Fatalf("round %d: budgets = %q, want %q", round, got, tc.wantMarkers)
				}
			}
		})
	}
}

// Two creates of one name under different ids, with and without an allocation:
// one wins, the other is NAME_TAKEN, and never a 500. Proven red against a
// lookup-then-insert (#130 R8).
func TestConcurrentCreatesOfOneNameAnswerOneCreateAndTheRestNameTaken(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bucket     api.Bucket
		allocation *string
	}{
		{"without an allocation", api.Daily, nil},
		{"with an allocation", api.Commitment, ptr("1000")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			_, ctx := f.member(t, "Home", "a@example.com")

			for round := range 8 {
				name := fmt.Sprintf("Rent %d", round)
				results := race(2, func(i int) string {
					n := name
					if i == 1 {
						n = strings.ToLower(name) // the case-insensitive clash
					}
					return statusOf(f.create(ctx, t, newID(t), n, tc.bucket, tc.allocation))
				})
				counts := tally(results)
				if counts["201"] != 1 || counts["409 NAME_TAKEN"] != 1 {
					t.Fatalf("round %d: statuses = %v, want one 201 and one 409 NAME_TAKEN", round, counts)
				}
			}
			if tc.allocation != nil {
				if got := f.count(t, "allocations"); got != 8 {
					t.Errorf("allocations = %d, want 8: only the winners wrote one", got)
				}
			}
		})
	}
}

// N creates of different Categories into a Period nobody has opened: one
// marker, one allocation each, no 500, and the Period they all materialise is
// carried forward exactly once.
func TestConcurrentCreatesIntoAFreshPeriodMaterialiseItOnce(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.setClock(t, "2026-10-10T10:00:00+07:00")
	assertCreated(t, f.create(ctx, t, newID(t), "Rent", api.Commitment, ptr("3000000")))
	f.setClock(t, "2026-11-10T10:00:00+07:00")
	const n = 16

	results := race(n, func(i int) string {
		return statusOf(f.create(ctx, t, newID(t), fmt.Sprintf("C%d", i), api.Daily, ptr("1000")))
	})

	if counts := tally(results); counts["201"] != n {
		t.Fatalf("statuses = %v, want %d 201", counts, n)
	}
	if got := f.markers(t); got != "2026-10,2026-11" {
		t.Errorf("budgets = %q, want one marker per Period", got)
	}
	// The October rent copied once, plus one allocation per create.
	if got := f.count(t, "allocations"); got != 1+1+n {
		t.Errorf("allocations = %d, want %d", got, 1+1+n)
	}
	if got := f.scalar(t, `SELECT count(*)::text FROM allocations WHERE year_month = '2026-11' AND effective_on = '2026-11-01'`); got != "1" {
		t.Errorf("copies of the October rent = %s, want exactly 1", got)
	}
}

// ---- failure ---------------------------------------------------------------

// breakable overrides the calls a test wants to fail, and records nothing else.
type breakable struct {
	db.Querier
	create func(context.Context, db.CreateCategoryParams) (db.Category, error)
	get    func(context.Context, db.GetLiveCategoryParams) (db.Category, error)
	list   func(context.Context, pgtype.UUID) ([]db.Category, error)
}

func (b breakable) CreateCategory(ctx context.Context, p db.CreateCategoryParams) (db.Category, error) {
	if b.create != nil {
		return b.create(ctx, p)
	}
	return b.Querier.CreateCategory(ctx, p)
}

func (b breakable) GetLiveCategory(ctx context.Context, p db.GetLiveCategoryParams) (db.Category, error) {
	if b.get != nil {
		return b.get(ctx, p)
	}
	return b.Querier.GetLiveCategory(ctx, p)
}

func (b breakable) ListCategories(ctx context.Context, h pgtype.UUID) ([]db.Category, error) {
	if b.list != nil {
		return b.list(ctx, h)
	}
	return b.Querier.ListCategories(ctx, h)
}

var errOutage = errors.New("the database went away")

// Only ErrNoRows means "conflict": any other error is a 500, whichever of the
// two statements raised it, and it is never read as an id being taken.
func TestAnErrorThatIsNotNoRowsIsA500AndNeverAConflict(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(db.Querier) db.Querier
	}{
		{"the insert fails", func(q db.Querier) db.Querier {
			return breakable{Querier: q, create: func(context.Context, db.CreateCategoryParams) (db.Category, error) {
				return db.Category{}, errOutage
			}}
		}},
		{"the insert fails with a 23505 on another index", func(q db.Querier) db.Querier {
			return breakable{Querier: q, create: func(context.Context, db.CreateCategoryParams) (db.Category, error) {
				return db.Category{}, &pgconn.PgError{Code: "23505", ConstraintName: "something_else_key"}
			}}
		}},
		{"the read-back fails", func(q db.Querier) db.Querier {
			return breakable{Querier: q, get: func(context.Context, db.GetLiveCategoryParams) (db.Category, error) {
				return db.Category{}, errOutage
			}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, tc.wrap, nil)
			householdID, ctx := f.member(t, "Home", "a@example.com")
			taken := newID(t)
			f.exec(t, `INSERT INTO categories (id, household_id, category_display_name, bucket) VALUES ($1, $2, 'Held', 'daily')`, taken, householdID)

			assertInternal(t, f.create(ctx, t, taken, "Food", api.Daily, nil))
			if n := f.count(t, "categories"); n != 1 {
				t.Errorf("categories = %d, want the 1 that was there", n)
			}
		})
	}
}

func TestAListFailureIsA500(t *testing.T) {
	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return breakable{Querier: q, list: func(context.Context, pgtype.UUID) ([]db.Category, error) { return nil, errOutage }}
	}, nil)
	_, ctx := f.member(t, "Home", "a@example.com")
	resp, err := f.handlers.ListCategories(ctx, api.ListCategoriesRequestObject{})
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if got, is := resp.(api.ListCategories500JSONResponse); !is || got.Code != api.INTERNAL {
		t.Fatalf("response = %#v, want 500 INTERNAL", resp)
	}
}

func TestOnlyErrNoRowsFromTheInsertGoesToTheReadBack(t *testing.T) {
	read := false
	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return breakable{
			Querier: q,
			create: func(context.Context, db.CreateCategoryParams) (db.Category, error) {
				return db.Category{}, pgx.ErrNoRows
			},
			get: func(context.Context, db.GetLiveCategoryParams) (db.Category, error) {
				read = true
				return db.Category{}, pgx.ErrNoRows
			},
		}
	}, nil)
	_, ctx := f.member(t, "Home", "a@example.com")
	assertConflict(t, f.create(ctx, t, newID(t), "Food", api.Daily, nil), api.IDTAKEN)
	if !read {
		t.Error("ErrNoRows from the insert did not reach the read-back")
	}
}

// A name-index 23505 is NAME_TAKEN unless the id now names a live Category of
// the caller's Household, which is a concurrent replay losing the race.
func TestANameConflictIsReadBackBeforeItIsNameTaken(t *testing.T) {
	nameClash := func(context.Context, db.CreateCategoryParams) (db.Category, error) {
		return db.Category{}, &pgconn.PgError{Code: "23505", ConstraintName: "categories_household_name_idx"}
	}
	for _, tc := range []struct {
		name string
		get  func(context.Context, db.GetLiveCategoryParams) (db.Category, error)
		want string
	}{
		{"the id names a live Category: a replay", func(_ context.Context, p db.GetLiveCategoryParams) (db.Category, error) {
			return db.Category{ID: p.ID, CategoryDisplayName: "Food", Bucket: "daily", Active: true}, nil
		}, "200"},
		{"no such Category: another one holds the name", func(context.Context, db.GetLiveCategoryParams) (db.Category, error) {
			return db.Category{}, pgx.ErrNoRows
		}, "409 NAME_TAKEN"},
		{"the read-back fails", func(context.Context, db.GetLiveCategoryParams) (db.Category, error) {
			return db.Category{}, errOutage
		}, "500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, func(q db.Querier) db.Querier {
				return breakable{Querier: q, create: nameClash, get: tc.get}
			}, nil)
			_, ctx := f.member(t, "Home", "a@example.com")
			if got := statusOf(f.create(ctx, t, newID(t), "Food", api.Daily, nil)); got != tc.want {
				t.Errorf("status = %s, want %s", got, tc.want)
			}
		})
	}
}

// The second unique index on the id, (household_id, id), is not ON CONFLICT's
// arbiter: a concurrent replay of one id can trip over it. The read-back decides,
// as for a name clash: the id names a live Category of the Household (a replay,
// 200), or nothing is there to replay (ID_TAKEN), or the read-back fails (500).
func TestAnIDKeyClashIsReadBackBeforeItIsAnything(t *testing.T) {
	clash := func(context.Context, db.CreateCategoryParams) (db.Category, error) {
		return db.Category{}, &pgconn.PgError{Code: "23505", ConstraintName: "categories_household_id_id_key"}
	}
	for _, tc := range []struct {
		name string
		get  func(context.Context, db.GetLiveCategoryParams) (db.Category, error)
		want string
	}{
		{"the id names a live Category: a replay", func(_ context.Context, p db.GetLiveCategoryParams) (db.Category, error) {
			return db.Category{ID: p.ID, CategoryDisplayName: "Food", Bucket: "daily", Active: true}, nil
		}, "200"},
		{"nothing to replay: the id is held", func(context.Context, db.GetLiveCategoryParams) (db.Category, error) {
			return db.Category{}, pgx.ErrNoRows
		}, "409 ID_TAKEN"},
		{"the read-back fails", func(context.Context, db.GetLiveCategoryParams) (db.Category, error) {
			return db.Category{}, errOutage
		}, "500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, func(q db.Querier) db.Querier {
				return breakable{Querier: q, create: clash, get: tc.get}
			}, nil)
			_, ctx := f.member(t, "Home", "a@example.com")
			if got := statusOf(f.create(ctx, t, newID(t), "Food", api.Daily, nil)); got != tc.want {
				t.Errorf("status = %s, want %s", got, tc.want)
			}
		})
	}
}

// failingTx fails the statement it is told to, and rolls back what came before.
type failingTx struct {
	pgx.Tx
	failOn     string
	failCommit bool
}

type errRow struct{}

func (errRow) Scan(...any) error { return errOutage }

func (tx failingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.failOn != "" && strings.Contains(sql, tx.failOn) {
		return pgconn.CommandTag{}, errOutage
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func (tx failingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx.failOn != "" && strings.Contains(sql, tx.failOn) {
		return nil, errOutage
	}
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.failOn != "" && strings.Contains(sql, tx.failOn) {
		return errRow{}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx failingTx) Commit(ctx context.Context) error {
	if tx.failCommit {
		_ = tx.Rollback(ctx)
		return errOutage
	}
	return tx.Tx.Commit(ctx)
}

type breakingBeginner struct {
	budget.Beginner
	failOn     string
	failCommit bool
	failBegin  bool
}

func (b breakingBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	if b.failBegin {
		return nil, errOutage
	}
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failingTx{Tx: tx, failOn: b.failOn, failCommit: b.failCommit}, nil
}

// A failure at any step of the transaction leaves neither the Category, nor its
// allocation, nor the Period marker, answers 500, and logs no name and no
// amount. A failure of a later step is never read as "the id is held".
func TestAFailureAnywhereInTheCreateTransactionLeavesNothingBehind(t *testing.T) {
	for _, tc := range []struct {
		name string
		brk  breakingBeginner
	}{
		{"begin", breakingBeginner{failBegin: true}},
		{"the Category insert", breakingBeginner{failOn: "INSERT INTO categories"}},
		{"the Household lookup", breakingBeginner{failOn: "FROM households"}},
		{"the marker", breakingBeginner{failOn: "INSERT INTO budgets"}},
		{"the source choice", breakingBeginner{failOn: "FROM budgets b"}},
		{"the copy", breakingBeginner{failOn: "FROM allocations s"}},
		{"the allocation", breakingBeginner{failOn: "DO UPDATE SET allocation"}},
		{"commit", breakingBeginner{failCommit: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brk := tc.brk
			f := newFixtureWith(t, nil, func(b budget.Beginner) budget.Beginner { brk.Beginner = b; return brk })
			householdID, ctx := f.member(t, "Home", "a@example.com")
			// A source Period holds an allocation, so the copy has something to copy.
			old := f.tdb.CreateCategory(t, householdID, "Existing", "commitment")
			f.exec(t, `INSERT INTO budgets (household_id, year_month) VALUES ($1, '2026-10')`, householdID)
			f.exec(t, `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ($1, '2026-10', '2026-10-01', $2, 1)`, householdID, old)
			var buf bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })

			id := newID(t)
			assertInternal(t, f.create(ctx, t, id, "Secret Rent Name", api.Commitment, ptr("424242")))

			if n := f.count(t, "categories"); n != 1 {
				t.Errorf("categories = %d, want only the existing one", n)
			}
			if got := f.allocations(t); got != "2026-10 2026-10-01 Existing 1.0000" {
				t.Errorf("allocations = %q, want only the existing one", got)
			}
			if got := f.markers(t); got != "2026-10" {
				t.Errorf("budgets = %q: a failed create must leave no marker behind", got)
			}
			for _, secret := range []string{"Secret Rent Name", "424242", "a@example.com"} {
				if strings.Contains(buf.String(), secret) {
					t.Errorf("the log leaks %q:\n%s", secret, buf.String())
				}
			}
			if buf.Len() == 0 {
				t.Error("nothing was logged; the 500 should leave a trace")
			}
		})
	}
}

// A User whose time zone cannot be loaded fails after the Category insert; the
// Category goes with it (a step's failure is not "the id is held").
func TestAnUnknownTimeZoneRollsTheCategoryBackAndAnswers500(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	f.exec(t, `UPDATE users SET time_zone = 'Mars/Olympus'`)
	ctx = f.refresh(ctx, t)
	slog.SetDefault(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	assertInternal(t, f.create(ctx, t, newID(t), "Rent", api.Commitment, ptr("1")))
	if f.count(t, "categories") != 0 || f.count(t, "allocations") != 0 || f.markers(t) != "" {
		t.Error("a failed create left rows behind")
	}
	// Without an allocation there is no Period to work out, so no Today is needed.
	assertCreated(t, f.create(ctx, t, newID(t), "Food", api.Daily, nil))
}

// ADR-0003: a Category's name is user content, and nothing logs a body.
func TestAFailureLogsNoCategoryName(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return breakable{Querier: q,
			create: func(context.Context, db.CreateCategoryParams) (db.Category, error) { return db.Category{}, errOutage },
			list:   func(context.Context, pgtype.UUID) ([]db.Category, error) { return nil, errOutage },
		}
	}, nil)
	_, ctx := f.member(t, "Home", "a@example.com")
	assertInternal(t, f.create(ctx, t, newID(t), "Secret Savings Name", api.Daily, nil))
	if _, err := f.handlers.ListCategories(ctx, api.ListCategoriesRequestObject{}); err != nil {
		t.Fatalf("ListCategories: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("nothing was logged; the 500 should leave a trace")
	}
	if strings.Contains(buf.String(), "Secret Savings Name") {
		t.Errorf("a Category's name reached the log: %s", buf.String())
	}
}

func TestNewWithoutAClockUsesTheRealOne(t *testing.T) {
	f := newFixture(t)
	h := category.New(category.Deps{Querier: f.tdb.Queries, Beginner: f.tdb.Pool})
	_, ctx := f.member(t, "Home", "a@example.com")
	resp, err := h.CreateCategory(ctx, api.CreateCategoryRequestObject{
		Body: &api.CreateCategoryRequest{Id: newID(t), CategoryDisplayName: "Rent", Bucket: api.Commitment, Allocation: amount(ptr("1"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCreated(t, resp)
	if f.count(t, "allocations") != 1 {
		t.Error("the create with the real clock wrote no allocation")
	}
}
