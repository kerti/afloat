package pocket_test

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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/pocket"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// The tests here talk to a real Postgres and none is parallel (ADR-0004): they
// share one database that each test empties. The concurrent ones start their own
// goroutines, which is the point of them.

type fixture struct {
	tdb      *testutil.TestDB
	handlers *pocket.Handlers
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return newFixtureWith(t, nil)
}

// newFixtureWith wraps the real Querier, so a test can break one call.
func newFixtureWith(t *testing.T, wrap func(db.Querier) db.Querier) fixture {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	var q db.Querier = tdb.Queries
	if wrap != nil {
		q = wrap(q)
	}
	return fixture{tdb: tdb, handlers: pocket.New(pocket.Deps{Querier: q})}
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

func (f fixture) create(ctx context.Context, t *testing.T, id uuid.UUID, name string) api.CreatePocketResponseObject {
	t.Helper()
	resp, err := f.handlers.CreatePocket(ctx, api.CreatePocketRequestObject{
		Body: &api.CreatePocketRequest{Id: id, PocketDisplayName: name},
	})
	if err != nil {
		t.Fatalf("CreatePocket returned an error: %v", err)
	}
	return resp
}

func (f fixture) list(ctx context.Context, t *testing.T) []api.Pocket {
	t.Helper()
	resp, err := f.handlers.ListPockets(ctx, api.ListPocketsRequestObject{})
	if err != nil {
		t.Fatalf("ListPockets returned an error: %v", err)
	}
	got, is := resp.(api.ListPockets200JSONResponse)
	if !is {
		t.Fatalf("ListPockets = %#v, want 200", resp)
	}
	return got.Items
}

// row is the Pocket's whole row as JSON: every column, byte for byte.
func (f fixture) row(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT to_jsonb(p)::text FROM pockets p WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read pocket: %v", err)
	}
	return s
}

func (f fixture) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM pockets`).Scan(&n); err != nil {
		t.Fatalf("count pockets: %v", err)
	}
	return n
}

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.tdb.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
}

func assertCreated(t *testing.T, resp api.CreatePocketResponseObject) api.Pocket {
	t.Helper()
	got, is := resp.(api.CreatePocket201JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 201", resp)
	}
	return api.Pocket(got)
}

func assertReplayed(t *testing.T, resp api.CreatePocketResponseObject) api.Pocket {
	t.Helper()
	got, is := resp.(api.CreatePocket200JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 200", resp)
	}
	return api.Pocket(got)
}

func assertConflict(t *testing.T, resp api.CreatePocketResponseObject, code api.ErrorCode) {
	t.Helper()
	got, is := resp.(api.CreatePocket409JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 409 %s", resp, code)
	}
	if got.Code != code || got.Args != nil {
		t.Errorf("409 = %s %v, want %s with no args", got.Code, got.Args, code)
	}
}

func assertValidation(t *testing.T, resp api.CreatePocketResponseObject, field, rule string) {
	t.Helper()
	got, is := resp.(api.CreatePocket400JSONResponse)
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

func assertInternal(t *testing.T, resp api.CreatePocketResponseObject) {
	t.Helper()
	got, is := resp.(api.CreatePocket500JSONResponse)
	if !is || got.Code != api.INTERNAL {
		t.Fatalf("response = %#v, want 500 INTERNAL", resp)
	}
}

func TestCreateAnswers201ThenAReplayAnswers200(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)

	created := assertCreated(t, f.create(ctx, t, id, "OVO"))
	if created.Id != id || created.PocketDisplayName != "OVO" || !created.Active {
		t.Errorf("created = %+v, want the id, the name and active", created)
	}
	before := f.row(t, id)

	replayed := assertReplayed(t, f.create(ctx, t, id, "OVO"))
	if replayed != created {
		t.Errorf("replay = %+v, want %+v", replayed, created)
	}
	if got := f.row(t, id); got != before {
		t.Errorf("row changed by a replay:\n%s\n%s", before, got)
	}
	if n := f.count(t); n != 1 {
		t.Errorf("pockets = %d, want 1", n)
	}
}

// The body of a replay is neither compared nor applied: the answer is the
// Pocket as stored, so a retry after another member's rename sees the rename.
func TestAReplayWithADifferentNameAnswersTheStoredPocketAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	f.exec(t, `UPDATE pockets SET pocket_display_name = 'Gopay', active = false WHERE id = $1`, id)
	before := f.row(t, id)

	got := assertReplayed(t, f.create(ctx, t, id, "Dana"))
	if got.PocketDisplayName != "Gopay" || got.Active {
		t.Errorf("replay = %+v, want the stored name Gopay and active false", got)
	}
	if after := f.row(t, id); after != before {
		t.Errorf("row changed by a replay:\n%s\n%s", before, after)
	}
}

// A replay of an id that exists never reaches the name index: Postgres checks
// the arbiter first.
func TestAReplayWhoseBodyNameIsAnotherPocketsAnswers200NotNameTaken(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	first, second := newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, first, "OVO"))
	assertCreated(t, f.create(ctx, t, second, "Gopay"))

	got := assertReplayed(t, f.create(ctx, t, second, "ovo"))
	if got.PocketDisplayName != "Gopay" {
		t.Errorf("replay = %+v, want the stored Gopay", got)
	}
}

func TestAReplayOfAnInactivePocketAnswers200WithActiveFalse(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1 AND household_id = $2`, id, householdID)

	if got := assertReplayed(t, f.create(ctx, t, id, "OVO")); got.Active {
		t.Errorf("replay = %+v, want active false", got)
	}
}

// A deleted id is never reused: an id names one Pocket for ever. And it answers
// what another Household's id answers, byte for byte.
func TestAnIdHeldByADeletedOrForeignPocketIsIDTakenInOneAnswer(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, foreign := f.member(t, "Other", "b@example.com")

	deleted := newID(t)
	assertCreated(t, f.create(ctx, t, deleted, "Old"))
	f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, deleted)
	deletedBefore := f.row(t, deleted)

	theirs := newID(t)
	assertCreated(t, f.create(foreign, t, theirs, "Theirs"))
	theirsBefore := f.row(t, theirs)

	own := f.create(ctx, t, deleted, "Old")
	other := f.create(ctx, t, theirs, "Mine")
	assertConflict(t, own, api.IDTAKEN)
	assertConflict(t, other, api.IDTAKEN)

	ownStatus, ownBody := wireOf(t, own)
	otherStatus, otherBody := wireOf(t, other)
	if ownStatus != otherStatus || ownBody != otherBody {
		t.Errorf("own-deleted answers %d %s, foreign answers %d %s; want one answer", ownStatus, ownBody, otherStatus, otherBody)
	}
	if got := f.row(t, deleted); got != deletedBefore {
		t.Errorf("deleted row changed:\n%s\n%s", deletedBefore, got)
	}
	if got := f.row(t, theirs); got != theirsBefore {
		t.Errorf("the other Household's row changed:\n%s\n%s", theirsBefore, got)
	}
	if n := f.count(t); n != 2 {
		t.Errorf("pockets = %d, want 2: a refused create writes nothing", n)
	}
}

func wireOf(t *testing.T, resp api.CreatePocketResponseObject) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitCreatePocketResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

// A body that is invalid is refused whatever the id holds.
func TestAnInvalidBodyIsRefusedBeforeTheIdIsConsidered(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, foreign := f.member(t, "Other", "b@example.com")
	mine, theirs, deleted := newID(t), newID(t), newID(t)
	assertCreated(t, f.create(ctx, t, mine, "OVO"))
	assertCreated(t, f.create(foreign, t, theirs, "Theirs"))
	assertCreated(t, f.create(ctx, t, deleted, "Old"))
	f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, deleted)

	for name, id := range map[string]uuid.UUID{"free": newID(t), "own": mine, "foreign": theirs, "own and deleted": deleted} {
		t.Run(name, func(t *testing.T) {
			assertValidation(t, f.create(ctx, t, id, "   "), "pocket_display_name", "required")
		})
	}
}

func TestANewPocketIsActive(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))

	var active bool
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT active FROM pockets WHERE id = $1`, id).Scan(&active); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !active {
		t.Error("a new Pocket is not active")
	}
}

func TestTheNameIsStoredTrimmed(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	got := assertCreated(t, f.create(ctx, t, newID(t), "  OVO  "))
	if got.PocketDisplayName != "OVO" {
		t.Errorf("name = %q, want OVO", got.PocketDisplayName)
	}
}

// Names are unique among a Household's Pockets that are not deleted, active or
// inactive, case-insensitively (ASCII only: lower() follows the database's
// locale, so no row pins a non-ASCII fold). A deleted Pocket releases its name,
// and another Household may hold it.
func TestNamesAreUniqueWithinAHouseholdWhileNotDeleted(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, foreign := f.member(t, "Other", "b@example.com")
	held := newID(t)
	assertCreated(t, f.create(ctx, t, held, "OVO"))

	t.Run("the same name in another case", func(t *testing.T) {
		assertConflict(t, f.create(ctx, t, newID(t), "ovo"), api.NAMETAKEN)
	})
	t.Run("the same name padded", func(t *testing.T) {
		assertConflict(t, f.create(ctx, t, newID(t), "  OVO "), api.NAMETAKEN)
	})
	t.Run("another Household may hold it", func(t *testing.T) {
		assertCreated(t, f.create(foreign, t, newID(t), "OVO"))
	})
	t.Run("an inactive Pocket still holds its name", func(t *testing.T) {
		f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, held)
		assertConflict(t, f.create(ctx, t, newID(t), "ovo"), api.NAMETAKEN)
	})
	t.Run("a deleted Pocket releases its name", func(t *testing.T) {
		f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, held)
		assertCreated(t, f.create(ctx, t, newID(t), "ovo"))
	})
}

func TestARefusedNameWritesNothingAndTheIdStaysFree(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	assertCreated(t, f.create(ctx, t, newID(t), "OVO"))
	id := newID(t)

	assertConflict(t, f.create(ctx, t, id, "ovo"), api.NAMETAKEN)
	if n := f.count(t); n != 1 {
		t.Fatalf("pockets = %d, want 1", n)
	}
	assertCreated(t, f.create(ctx, t, id, "Gopay"))
}

func TestACreateWithoutAUserIs401AndWritesNothing(t *testing.T) {
	f := newFixture(t)
	resp := f.create(context.Background(), t, newID(t), "OVO")
	got, is := resp.(api.CreatePocket401JSONResponse)
	if !is || got.Code != api.UNAUTHORIZED {
		t.Fatalf("response = %#v, want 401 UNAUTHORIZED", resp)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("pockets = %d, want 0", n)
	}
	if _, err := f.handlers.ListPockets(context.Background(), api.ListPocketsRequestObject{}); err != nil {
		t.Fatalf("ListPockets: %v", err)
	}
}

func TestAListWithoutAUserIs401(t *testing.T) {
	f := newFixture(t)
	resp, err := f.handlers.ListPockets(context.Background(), api.ListPocketsRequestObject{})
	if err != nil {
		t.Fatalf("ListPockets: %v", err)
	}
	if got, is := resp.(api.ListPockets401JSONResponse); !is || got.Code != api.UNAUTHORIZED {
		t.Fatalf("response = %#v, want 401 UNAUTHORIZED", resp)
	}
}

// ---- list ------------------------------------------------------------------

func TestAnEmptyHouseholdListsAnEmptyNonNilSlice(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	items := f.list(ctx, t)
	if items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want an empty non-nil slice", items)
	}

	rec := httptest.NewRecorder()
	if err := (api.ListPockets200JSONResponse{Items: items}).VisitListPocketsResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"items":[]}` {
		t.Errorf("body = %s, want {\"items\":[]}", got)
	}
}

// Creation order, stable under a rename or a deactivation, and the list carries
// the inactive and never the deleted or another Household's.
func TestTheListIsInCreationOrderAndOnlyTheHouseholdsLiveOnes(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	_, foreign := f.member(t, "Other", "b@example.com")

	// Names chosen so alphabetical order is the reverse of creation order.
	ids := []uuid.UUID{newID(t), newID(t), newID(t), newID(t)}
	for i, name := range []string{"Zeta", "Yankee", "Xray", "Whiskey"} {
		assertCreated(t, f.create(ctx, t, ids[i], name))
	}
	assertCreated(t, f.create(foreign, t, newID(t), "Theirs"))

	f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, ids[2])
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, ids[1])
	f.exec(t, `UPDATE pockets SET pocket_display_name = 'Aardvark', updated_at = now() WHERE id = $1`, ids[0])

	got := f.list(ctx, t)
	want := []api.Pocket{
		{Id: ids[0], PocketDisplayName: "Aardvark", Active: true},
		{Id: ids[1], PocketDisplayName: "Yankee", Active: false},
		{Id: ids[3], PocketDisplayName: "Whiskey", Active: true},
	}
	if len(got) != len(want) {
		t.Fatalf("items = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// created_at ties break on id, so the order is total.
func TestEqualCreationInstantsOrderByID(t *testing.T) {
	f := newFixture(t)
	householdID, ctx := f.member(t, "Home", "a@example.com")
	low := uuid.MustParse("019212e0-0000-7000-8000-000000000001")
	high := uuid.MustParse("019212e0-0000-7000-8000-000000000002")
	for _, id := range []uuid.UUID{high, low} {
		f.exec(t, `INSERT INTO pockets (id, household_id, pocket_display_name, created_at) VALUES ($1, $2, $3, '2026-01-01T00:00:00Z')`,
			id, householdID, id.String())
	}
	got := f.list(ctx, t)
	if len(got) != 2 || got[0].Id != low || got[1].Id != high {
		t.Errorf("items = %+v, want %s then %s", got, low, high)
	}
}

// PRD S2: Pockets belong to the Household and never to a User.
func TestTwoUsersInOneHouseholdSeeTheSamePockets(t *testing.T) {
	f := newFixture(t)
	householdID, first := f.member(t, "Home", "a@example.com")
	second := f.userIn(t, householdID, "b@example.com")
	id := newID(t)
	assertCreated(t, f.create(first, t, id, "OVO"))

	for name, ctx := range map[string]context.Context{"first": first, "second": second} {
		got := f.list(ctx, t)
		if len(got) != 1 || got[0].Id != id {
			t.Errorf("%s User sees %+v, want the one Pocket", name, got)
		}
	}
	assertReplayed(t, f.create(second, t, id, "OVO"))
	assertConflict(t, f.create(second, t, newID(t), "ovo"), api.NAMETAKEN)
}

// ---- tenancy ---------------------------------------------------------------

// Two Households. Each operation run as one leaves the other's Pockets
// byte-identical, and the other's id answers exactly what an id of an own
// deleted Pocket answers. Proven red against a statement whose WHERE lost
// household_id (see the PR).
func TestTenancyNothingCrossesHouseholds(t *testing.T) {
	f := newFixture(t)
	_, alice := f.member(t, "Alice", "a@example.com")
	_, bob := f.member(t, "Bob", "b@example.com")
	aliceID, aliceInactive, aliceDeleted := newID(t), newID(t), newID(t)
	assertCreated(t, f.create(alice, t, aliceID, "OVO"))
	assertCreated(t, f.create(alice, t, aliceInactive, "Dana"))
	assertCreated(t, f.create(alice, t, aliceDeleted, "Gopay"))
	f.exec(t, `UPDATE pockets SET active = false WHERE id = $1`, aliceInactive)
	f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, aliceDeleted)
	snapshot := func() []string {
		return []string{f.row(t, aliceID), f.row(t, aliceInactive), f.row(t, aliceDeleted)}
	}
	before := snapshot()

	t.Run("Bob's list holds none of Alice's", func(t *testing.T) {
		if got := f.list(bob, t); len(got) != 0 {
			t.Fatalf("Bob sees %+v, want nothing", got)
		}
	})
	t.Run("Bob creating with each of Alice's ids is ID_TAKEN and Alice's rows are untouched", func(t *testing.T) {
		for _, id := range []uuid.UUID{aliceID, aliceInactive, aliceDeleted} {
			assertConflict(t, f.create(bob, t, id, "Mine"), api.IDTAKEN)
		}
		after := snapshot()
		for i := range before {
			if after[i] != before[i] {
				t.Errorf("row %d changed:\n%s\n%s", i, before[i], after[i])
			}
		}
		if n := f.count(t); n != 3 {
			t.Errorf("pockets = %d, want 3", n)
		}
	})
	t.Run("a foreign live id answers what an own deleted id answers", func(t *testing.T) {
		_, bobDeleted := newID(t), newID(t)
		assertCreated(t, f.create(bob, t, bobDeleted, "Old"))
		f.exec(t, `UPDATE pockets SET deleted_at = now() WHERE id = $1`, bobDeleted)
		foreignStatus, foreignBody := wireOf(t, f.create(bob, t, aliceID, "x"))
		ownStatus, ownBody := wireOf(t, f.create(bob, t, bobDeleted, "x"))
		if foreignStatus != ownStatus || foreignBody != ownBody {
			t.Errorf("foreign %d %s, own deleted %d %s", foreignStatus, foreignBody, ownStatus, ownBody)
		}
	})
	t.Run("Alice sees her own", func(t *testing.T) {
		if got := f.list(alice, t); len(got) != 2 {
			t.Errorf("Alice sees %+v, want her two live Pockets", got)
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
	assertCreated(t, f.create(ctx, t, id, "OVO"))

	var got pgtype.UUID
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT household_id FROM pockets WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != householdID {
		t.Errorf("household_id = %v, want the caller's %v", got, householdID)
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

func statusOf(resp api.CreatePocketResponseObject) string {
	switch r := resp.(type) {
	case api.CreatePocket201JSONResponse:
		return "201"
	case api.CreatePocket200JSONResponse:
		return "200"
	case api.CreatePocket409JSONResponse:
		return "409 " + string(r.Code)
	case api.CreatePocket500JSONResponse:
		return "500"
	case api.CreatePocket400JSONResponse:
		return "400"
	}
	return fmt.Sprintf("%T", resp)
}

// N POSTs of one id: exactly one 201, the rest 200, one row. Proven red against
// a SELECT-then-INSERT implementation (see the PR).
func TestConcurrentCreatesOfOneIDCreateOneRow(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	const n = 24

	for round := range 5 {
		id := newID(t)
		name := fmt.Sprintf("OVO %d", round)
		results := race(n, func(int) string { return statusOf(f.create(ctx, t, id, name)) })
		counts := map[string]int{}
		for _, r := range results {
			counts[r]++
		}
		if counts["201"] != 1 || counts["200"] != n-1 {
			t.Fatalf("round %d: statuses = %v, want one 201 and %d 200", round, counts, n-1)
		}
		var rows int
		if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT count(*) FROM pockets WHERE id = $1`, id).Scan(&rows); err != nil {
			t.Fatalf("count: %v", err)
		}
		if rows != 1 {
			t.Fatalf("round %d: rows = %d, want 1", round, rows)
		}
	}
}

// Two creates of one name under different ids: one wins, the other is
// NAME_TAKEN, and never a 500. Proven red against a lookup-then-insert (see the
// PR).
func TestConcurrentCreatesOfOneNameAnswerOneCreateAndTheRestNameTaken(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")

	for round := range 8 {
		name := fmt.Sprintf("OVO %d", round)
		results := race(2, func(i int) string {
			n := name
			if i == 1 {
				n = strings.ToLower(name) // the case-insensitive clash
			}
			return statusOf(f.create(ctx, t, newID(t), n))
		})
		counts := map[string]int{}
		for _, r := range results {
			counts[r]++
		}
		if counts["201"] != 1 || counts["409 NAME_TAKEN"] != 1 {
			t.Fatalf("round %d: statuses = %v, want one 201 and one 409 NAME_TAKEN", round, counts)
		}
	}
}

// ---- failure ---------------------------------------------------------------

// breakable overrides the calls a test wants to fail, and records nothing else.
type breakable struct {
	db.Querier
	create func(context.Context, db.CreatePocketParams) (db.Pocket, error)
	get    func(context.Context, db.GetLivePocketParams) (db.Pocket, error)
	list   func(context.Context, pgtype.UUID) ([]db.Pocket, error)
}

func (b breakable) CreatePocket(ctx context.Context, p db.CreatePocketParams) (db.Pocket, error) {
	if b.create != nil {
		return b.create(ctx, p)
	}
	return b.Querier.CreatePocket(ctx, p)
}

func (b breakable) GetLivePocket(ctx context.Context, p db.GetLivePocketParams) (db.Pocket, error) {
	if b.get != nil {
		return b.get(ctx, p)
	}
	return b.Querier.GetLivePocket(ctx, p)
}

func (b breakable) ListPockets(ctx context.Context, h pgtype.UUID) ([]db.Pocket, error) {
	if b.list != nil {
		return b.list(ctx, h)
	}
	return b.Querier.ListPockets(ctx, h)
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
			return breakable{Querier: q, create: func(context.Context, db.CreatePocketParams) (db.Pocket, error) {
				return db.Pocket{}, errOutage
			}}
		}},
		{"the insert fails with a 23505 on another index", func(q db.Querier) db.Querier {
			return breakable{Querier: q, create: func(context.Context, db.CreatePocketParams) (db.Pocket, error) {
				return db.Pocket{}, &pgconn.PgError{Code: "23505", ConstraintName: "something_else_key"}
			}}
		}},
		{"the read-back fails", func(q db.Querier) db.Querier {
			return breakable{Querier: q, get: func(context.Context, db.GetLivePocketParams) (db.Pocket, error) {
				return db.Pocket{}, errOutage
			}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, tc.wrap)
			_, ctx := f.member(t, "Home", "a@example.com")
			taken := newID(t)
			f.exec(t, `INSERT INTO pockets (id, household_id, pocket_display_name) SELECT $1, id, 'Held' FROM households`, taken)

			assertInternal(t, f.create(ctx, t, taken, "OVO"))
			if n := f.count(t); n != 1 {
				t.Errorf("pockets = %d, want the 1 that was there", n)
			}
		})
	}
}

func TestAListFailureIsA500(t *testing.T) {
	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return breakable{Querier: q, list: func(context.Context, pgtype.UUID) ([]db.Pocket, error) { return nil, errOutage }}
	})
	_, ctx := f.member(t, "Home", "a@example.com")
	resp, err := f.handlers.ListPockets(ctx, api.ListPocketsRequestObject{})
	if err != nil {
		t.Fatalf("ListPockets: %v", err)
	}
	if got, is := resp.(api.ListPockets500JSONResponse); !is || got.Code != api.INTERNAL {
		t.Fatalf("response = %#v, want 500 INTERNAL", resp)
	}
}

// An unreadable pgx.ErrNoRows wrapped in context is still the conflict signal,
// and a replay is read back only for that.
func TestOnlyErrNoRowsFromTheInsertGoesToTheReadBack(t *testing.T) {
	read := false
	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return breakable{
			Querier: q,
			create:  func(context.Context, db.CreatePocketParams) (db.Pocket, error) { return db.Pocket{}, pgx.ErrNoRows },
			get: func(_ context.Context, _ db.GetLivePocketParams) (db.Pocket, error) {
				read = true
				return db.Pocket{}, pgx.ErrNoRows
			},
		}
	})
	_, ctx := f.member(t, "Home", "a@example.com")
	assertConflict(t, f.create(ctx, t, newID(t), "OVO"), api.IDTAKEN)
	if !read {
		t.Error("ErrNoRows from the insert did not reach the read-back")
	}
}

// ADR-0003: a Pocket's name is user content, and nothing logs a body.
func TestAFailureLogsNoPocketName(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	f := newFixtureWith(t, func(q db.Querier) db.Querier {
		return breakable{Querier: q,
			create: func(context.Context, db.CreatePocketParams) (db.Pocket, error) { return db.Pocket{}, errOutage },
			list:   func(context.Context, pgtype.UUID) ([]db.Pocket, error) { return nil, errOutage },
		}
	})
	_, ctx := f.member(t, "Home", "a@example.com")
	assertInternal(t, f.create(ctx, t, newID(t), "Secret Savings Name"))
	if _, err := f.handlers.ListPockets(ctx, api.ListPocketsRequestObject{}); err != nil {
		t.Fatalf("ListPockets: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("nothing was logged; the 500 should leave a trace")
	}
	if strings.Contains(buf.String(), "Secret Savings Name") {
		t.Errorf("a Pocket's name reached the log: %s", buf.String())
	}
}

// A name-index 23505 is NAME_TAKEN unless the id now names a live Pocket of the
// caller's Household, which is a concurrent replay losing the race (see
// CreatePocket). The read-back decides, and any error from it is a 500.
func TestANameConflictIsReadBackBeforeItIsNameTaken(t *testing.T) {
	nameClash := func(context.Context, db.CreatePocketParams) (db.Pocket, error) {
		return db.Pocket{}, &pgconn.PgError{Code: "23505", ConstraintName: "pockets_household_name_idx"}
	}
	for _, tc := range []struct {
		name string
		get  func(context.Context, db.GetLivePocketParams) (db.Pocket, error)
		want string
	}{
		{"the id names a live Pocket: a replay", func(_ context.Context, p db.GetLivePocketParams) (db.Pocket, error) {
			return db.Pocket{ID: p.ID, PocketDisplayName: "OVO", Active: true}, nil
		}, "200"},
		{"no such Pocket: another one holds the name", func(context.Context, db.GetLivePocketParams) (db.Pocket, error) {
			return db.Pocket{}, pgx.ErrNoRows
		}, "409 NAME_TAKEN"},
		{"the read-back fails", func(context.Context, db.GetLivePocketParams) (db.Pocket, error) {
			return db.Pocket{}, errOutage
		}, "500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureWith(t, func(q db.Querier) db.Querier {
				return breakable{Querier: q, create: nameClash, get: tc.get}
			})
			_, ctx := f.member(t, "Home", "a@example.com")
			if got := statusOf(f.create(ctx, t, newID(t), "OVO")); got != tc.want {
				t.Errorf("status = %s, want %s", got, tc.want)
			}
		})
	}
}
