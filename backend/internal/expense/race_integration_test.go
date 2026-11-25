package expense_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/category"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/expense"
	"github.com/kerti/afloat/backend/internal/household"
	"github.com/kerti/afloat/backend/internal/pocket"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// The three guards (POCKET_IN_USE, CATEGORY_IN_USE, CURRENCY_LOCKED) are one
// recipe (BOOTSTRAP.md §4): the guarded write locks the protected row, asks
// whether an Expense exists in a SEPARATE later statement, then writes; and every
// Expense create share-locks the protected row in the statement that checks it is
// live. Each is proven here against the interleaving the recipe exists to
// prevent, by holding one transaction at a chosen statement and starting the
// other, and each has a red proof kept beside it: the same scenario with one
// side's locks ALL taken off, which must leave a deleted Pocket or Category
// referenced by an Expense, or an Expense in a currency the Household no longer
// has.
//
// What is and is not proven red lock by lock. The Category guard rests on the
// Category lock alone and the currency guard on the Household lock alone, so
// each of those is red without its lock. The Pocket guard has TWO layers: every
// removal takes the Household (FOR NO KEY UPDATE) before the Pocket, and every
// create takes the Household (FOR SHARE) before the Pocket, so the Household pair
// already serialises a Pocket delete against a create, and the Pocket pair does
// too. Stripping only one layer therefore stays green, which the last tests here
// state (TestThePocketGuardHoldsOnEitherLayerAlone); only stripping both is red.
// The Pocket lock is kept anyway because it is the lock the recipe names for the
// protected row, and a writer that does not take the Household first (a restore
// or an edit that re-points an Expense, in the slices that follow) relies on it
// alone.

// gate holds a transaction at a statement until the test opens it.
type gate struct {
	reached chan struct{}
	release chan struct{}
	once    sync.Once
	open    sync.Once
}

func newGate() *gate { return &gate{reached: make(chan struct{}), release: make(chan struct{})} }

// pause is called by the held transaction: it announces it is here and waits.
func (g *gate) pause() {
	g.once.Do(func() { close(g.reached) })
	select {
	case <-g.release:
	case <-time.After(10 * time.Second):
	}
}

func (g *gate) waitReached(t *testing.T) {
	t.Helper()
	select {
	case <-g.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the held transaction never reached its statement")
	}
}

func (g *gate) let() { g.open.Do(func() { close(g.release) }) }

// wrappingPool is a Beginner for every handler (auth's and budget's) whose
// transactions pass through wrap.
type wrappingPool struct {
	*pgxpool.Pool
	wrap func(db.DBTX) db.DBTX
}

func (p wrappingPool) Begin(ctx context.Context) (pgx.Tx, error) {
	return wrappingBeginner{Beginner: p.Pool, wrap: p.wrap}.Begin(ctx)
}

// strip removes a lock clause from the statements named, which is what a "no
// lock" variant is. It panics when the clause is not there, so a rewrite of the
// SQL cannot quietly turn a red proof into a green one.
func strip(sql, clause string, statements ...string) string {
	for _, name := range statements {
		if strings.Contains(sql, marker(name)) {
			if !strings.Contains(sql, clause) {
				panic(name + " no longer carries " + clause + ": the red proof's marker is stale")
			}
			return strings.Replace(sql, clause, "", 1)
		}
	}
	return sql
}

// world is a Household with a Pocket and a Category to guard, a Pocket to keep
// (so the guarded one is never the last), and every handler a guard involves,
// each on its own wrapped transactions.
type world struct {
	tdb        *testutil.TestDB
	household  pgtype.UUID
	pocket     pgtype.UUID
	keeper     pgtype.UUID
	category   pgtype.UUID
	ctx        context.Context
	expenses   *expense.Handlers
	pockets    *pocket.Handlers
	categories *category.Handlers
	households *household.Handlers
}

// newWorld builds the handlers. createWrap wraps the Expense create's
// statements, removalWrap the guarded writes' (a Pocket's delete, a Category's,
// the currency change).
func newWorld(t *testing.T, createWrap, removalWrap func(db.DBTX) db.DBTX) world {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	f := fixture{tdb: tdb}
	hh := tdb.CreateHousehold(t, "Home")
	w := world{
		tdb:       tdb,
		household: hh,
		pocket:    tdb.CreatePocket(t, hh, "Cash"),
		keeper:    tdb.CreatePocket(t, hh, "Keeper"),
		category:  tdb.CreateCategory(t, hh, "Food", "daily"),
		ctx:       f.userIn(t, hh, "a@example.com"),
	}
	now := func() time.Time { return wednesday }
	w.expenses = expense.New(expense.Deps{Querier: tdb.Queries, Beginner: wrappingPool{tdb.Pool, createWrap}, Now: now})
	w.pockets = pocket.New(pocket.Deps{Querier: tdb.Queries, Beginner: wrappingPool{tdb.Pool, removalWrap}})
	w.categories = category.New(category.Deps{Querier: tdb.Queries, Beginner: wrappingPool{tdb.Pool, removalWrap}, Now: now})
	w.households = household.New(household.Deps{Querier: tdb.Queries, Beginner: wrappingPool{tdb.Pool, removalWrap}})
	return w
}

func (w world) create(t *testing.T, req api.CreateExpenseRequest) string {
	t.Helper()
	resp, err := w.expenses.CreateExpense(w.ctx, api.CreateExpenseRequestObject{Body: &req})
	if err != nil {
		t.Errorf("CreateExpense returned an error: %v", err)
	}
	return status(resp)
}

func identity(tx db.DBTX) db.DBTX { return tx }

// guard is one protected row: what an Expense create names, the write that is
// guarded, the statement the write is held after, and what must be true when
// both are done.
type guard struct {
	name string
	// create is the request that names the protected row.
	create func(w world) api.CreateExpenseRequest
	// remove runs the guarded write and answers its status.
	remove func(t *testing.T, w world) string
	// heldAfter is the removal's statement that the red proof for "the Expense
	// came second" holds it after: the EXISTS.
	heldAfter string
	// removalLocks are the removal's locking statements, whose clause the red
	// proof strips.
	removalLocks  []string
	removalClause string
	// createLocks are the create's, which carry FOR SHARE.
	createLocks []string
	// violated says what is wrong with the data, or "" when it is right.
	violated func(t *testing.T, w world) string
	// What each interleaving must answer.
	createFirst  outcome // the Expense create got there first
	removalFirst outcome // the guarded write got there first
}

type outcome struct{ create, removal string }

func scalarInt(t *testing.T, w world, sql string) int {
	t.Helper()
	var n int
	if err := w.tdb.Pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
	return n
}

func removalStatus(resp any) string {
	switch r := resp.(type) {
	case api.DeletePocket204Response, api.DeleteCategory204Response:
		return "204"
	case api.DeletePocket409JSONResponse:
		return string(r.Code)
	case api.DeletePocket404JSONResponse, api.DeleteCategory404JSONResponse:
		return "404"
	case api.DeleteCategory409JSONResponse:
		return string(r.Code)
	case api.UpdateHousehold200JSONResponse:
		return "200"
	case api.UpdateHousehold409JSONResponse:
		return string(r.Code)
	default:
		return fmt.Sprintf("unexpected %T", resp)
	}
}

var guards = []guard{
	{
		name: "POCKET_IN_USE",
		create: func(w world) api.CreateExpenseRequest {
			return api.CreateExpenseRequest{Id: uuid.Must(uuid.NewV7()), Amount: "25000", PocketId: w.pocket.Bytes}
		},
		remove: func(t *testing.T, w world) string {
			resp, err := w.pockets.DeletePocket(w.ctx, api.DeletePocketRequestObject{Id: w.pocket.Bytes})
			if err != nil {
				t.Errorf("DeletePocket: %v", err)
			}
			return removalStatus(resp)
		},
		heldAfter:     "PocketHasExpenses",
		removalLocks:  []string{"LockHousehold", "LockPocket"},
		removalClause: "FOR NO KEY UPDATE",
		createLocks:   []string{lockHouseholdShared, lockPocketShared},
		violated: func(t *testing.T, w world) string {
			if n := scalarInt(t, w, `SELECT count(*) FROM expenses e JOIN pockets p ON p.id = e.pocket_id WHERE p.deleted_at IS NOT NULL`); n > 0 {
				return fmt.Sprintf("%d Expense(s) reference a deleted Pocket", n)
			}
			return ""
		},
		createFirst:  outcome{create: "201", removal: "POCKET_IN_USE"},
		removalFirst: outcome{create: "VALIDATION pocket_id invalid", removal: "204"},
	},
	{
		name: "CATEGORY_IN_USE",
		create: func(w world) api.CreateExpenseRequest {
			c := uuid.UUID(w.category.Bytes)
			return api.CreateExpenseRequest{Id: uuid.Must(uuid.NewV7()), Amount: "25000", PocketId: w.pocket.Bytes, CategoryId: &c}
		},
		remove: func(t *testing.T, w world) string {
			resp, err := w.categories.DeleteCategory(w.ctx, api.DeleteCategoryRequestObject{Id: w.category.Bytes})
			if err != nil {
				t.Errorf("DeleteCategory: %v", err)
			}
			return removalStatus(resp)
		},
		heldAfter:     "CategoryHasExpenses",
		removalLocks:  []string{"LockCategory"},
		removalClause: "FOR UPDATE",
		createLocks:   []string{lockCategoryShared},
		violated: func(t *testing.T, w world) string {
			if n := scalarInt(t, w, `SELECT count(*) FROM expenses e JOIN categories c ON c.id = e.category_id WHERE c.deleted_at IS NOT NULL`); n > 0 {
				return fmt.Sprintf("%d Expense(s) reference a deleted Category", n)
			}
			return ""
		},
		createFirst:  outcome{create: "201", removal: "CATEGORY_IN_USE"},
		removalFirst: outcome{create: "VALIDATION category_id invalid", removal: "204"},
	},
	{
		name: "CURRENCY_LOCKED",
		create: func(w world) api.CreateExpenseRequest {
			return api.CreateExpenseRequest{Id: uuid.Must(uuid.NewV7()), Amount: "25000", PocketId: w.pocket.Bytes}
		},
		remove: func(t *testing.T, w world) string {
			resp, err := w.households.UpdateHousehold(w.ctx, api.UpdateHouseholdRequestObject{
				Body: &api.UpdateHouseholdRequest{ReportingCurrency: ptr("USD")},
			})
			if err != nil {
				t.Errorf("UpdateHousehold: %v", err)
			}
			return removalStatus(resp)
		},
		heldAfter:     "HouseholdHasExpenses",
		removalLocks:  []string{"LockHousehold"},
		removalClause: "FOR NO KEY UPDATE",
		createLocks:   []string{lockHouseholdShared},
		violated: func(t *testing.T, w world) string {
			if n := scalarInt(t, w, `SELECT count(*) FROM expenses e JOIN households h ON h.id = e.household_id WHERE e.currency <> h.reporting_currency`); n > 0 {
				return fmt.Sprintf("%d Expense(s) in a currency the Household no longer has", n)
			}
			return ""
		},
		createFirst:  outcome{create: "201", removal: "CURRENCY_LOCKED"},
		removalFirst: outcome{create: "201", removal: "200"},
	},
}

// createWrap holds the create's transaction just before its INSERT, with every
// lock it takes already held, and optionally takes those locks off.
func createWrap(g *gate, noLocks []string) func(db.DBTX) db.DBTX {
	return func(tx db.DBTX) db.DBTX {
		h := hooked{DBTX: tx}
		if len(noLocks) > 0 {
			h.rewrite = func(sql string) string { return strip(sql, "FOR SHARE", noLocks...) }
		}
		if g != nil {
			h.before = func(sql string) {
				if strings.Contains(sql, marker(createQuery)) {
					g.pause()
				}
			}
		}
		return h
	}
}

// removalWrap holds the guarded write's transaction just after its EXISTS, with
// the lock it took held, and optionally takes those locks off.
func removalWrap(g *gate, heldAfter string, clause string, noLocks []string) func(db.DBTX) db.DBTX {
	return func(tx db.DBTX) db.DBTX {
		h := hooked{DBTX: tx}
		if len(noLocks) > 0 {
			h.rewrite = func(sql string) string { return strip(sql, clause, noLocks...) }
		}
		if g != nil {
			h.after = func(sql string) {
				if strings.Contains(sql, marker(heldAfter)) {
					g.pause()
				}
			}
		}
		return h
	}
}

// result is what one interleaving did.
type result struct {
	create, removal string
	// followerBlocked says the transaction that came second could not finish
	// while the first was held: the lock doing its work.
	followerBlocked bool
	violation       string
}

// collect waits for a goroutine's answer, saying whether it was there in time.
func within(ch <-chan string, d time.Duration) (string, bool) {
	select {
	case s := <-ch:
		return s, true
	case <-time.After(d):
		return "", false
	}
}

const blockedFor = 400 * time.Millisecond

// holdCreate holds an Expense create with its locks held, starts the guarded
// write, and then lets the create finish.
func (g guard) holdCreate(t *testing.T, createNoLocks bool) result {
	t.Helper()
	if createNoLocks {
		return g.holdCreateWithout(t, g.createLocks)
	}
	return g.holdCreateWithout(t, nil)
}

// holdCreateWithout is holdCreate with the named locking statements of the create
// run without their lock clause.
func (g guard) holdCreateWithout(t *testing.T, noLocks []string) result {
	t.Helper()
	held := newGate()
	w := newWorld(t, createWrap(held, noLocks), identity)

	created := make(chan string, 1)
	go func() { created <- w.create(t, g.create(w)) }()
	held.waitReached(t)

	removed := make(chan string, 1)
	go func() { removed <- g.remove(t, w) }()
	removal, finished := within(removed, blockedFor)

	held.let()
	create, _ := within(created, 10*time.Second)
	if !finished {
		removal, _ = within(removed, 10*time.Second)
	}
	return result{create: create, removal: removal, followerBlocked: !finished, violation: g.violated(t, w)}
}

// holdRemoval holds the guarded write after its EXISTS, with its locks held,
// starts an Expense create, and then lets the write finish.
func (g guard) holdRemoval(t *testing.T, removalNoLocks bool) result {
	t.Helper()
	if removalNoLocks {
		return g.holdRemovalWithout(t, g.removalLocks)
	}
	return g.holdRemovalWithout(t, nil)
}

// holdRemovalWithout is holdRemoval with the named locking statements of the
// guarded write run without their lock clause.
func (g guard) holdRemovalWithout(t *testing.T, noLocks []string) result {
	t.Helper()
	held := newGate()
	w := newWorld(t, identity, removalWrap(held, g.heldAfter, g.removalClause, noLocks))

	removed := make(chan string, 1)
	go func() { removed <- g.remove(t, w) }()
	held.waitReached(t)

	created := make(chan string, 1)
	go func() { created <- w.create(t, g.create(w)) }()
	create, finished := within(created, blockedFor)

	held.let()
	removal, _ := within(removed, 10*time.Second)
	if !finished {
		create, _ = within(created, 10*time.Second)
	}
	return result{create: create, removal: removal, followerBlocked: !finished, violation: g.violated(t, w)}
}

func TestAnExpenseThatGetsThereFirstIsSeenByTheGuardedWrite(t *testing.T) {
	for _, g := range guards {
		t.Run(g.name, func(t *testing.T) {
			for round := range 3 {
				got := g.holdCreate(t, false)
				if !got.followerBlocked {
					t.Errorf("round %d: the guarded write did not wait for the Expense that held the lock", round)
				}
				if got.create != g.createFirst.create || got.removal != g.createFirst.removal {
					t.Errorf("round %d: create %s, write %s; want %s and %s", round, got.create, got.removal, g.createFirst.create, g.createFirst.removal)
				}
				if got.violation != "" {
					t.Errorf("round %d: %s", round, got.violation)
				}
			}
		})
	}
}

func TestAGuardedWriteThatGetsThereFirstIsSeenByTheExpense(t *testing.T) {
	for _, g := range guards {
		t.Run(g.name, func(t *testing.T) {
			for round := range 3 {
				got := g.holdRemoval(t, false)
				if !got.followerBlocked {
					t.Errorf("round %d: the Expense did not wait for the guarded write that held the lock", round)
				}
				if got.create != g.removalFirst.create || got.removal != g.removalFirst.removal {
					t.Errorf("round %d: create %s, write %s; want %s and %s", round, got.create, got.removal, g.removalFirst.create, g.removalFirst.removal)
				}
				if got.violation != "" {
					t.Errorf("round %d: %s", round, got.violation)
				}
			}
		})
	}
}

// The red proofs, kept. The same two interleavings with one side's locks taken
// off (the statements are run without their lock clause) leave the data wrong: a
// deleted Pocket or Category an Expense references, or an Expense in a currency the
// Household no longer has. If one ever stops failing, the lock is no longer what
// holds the invariant and the handler needs a rethink, not a quiet deletion.

func TestWithoutTheExpensesSharedLocksTheGuardedWriteDoesNotWaitAndTheDataIsWrong(t *testing.T) {
	for _, g := range guards {
		t.Run(g.name, func(t *testing.T) {
			got := g.holdCreate(t, true)
			if got.followerBlocked {
				t.Error("the guarded write waited although the Expense held no lock")
			}
			if got.violation == "" {
				t.Errorf("create %s, write %s: nothing is wrong, so the shared locks are not what the guard relies on", got.create, got.removal)
			}
		})
	}
}

func TestWithoutTheGuardedWritesLocksTheExpenseDoesNotWaitAndTheDataIsWrong(t *testing.T) {
	for _, g := range guards {
		t.Run(g.name, func(t *testing.T) {
			got := g.holdRemoval(t, true)
			if got.followerBlocked {
				t.Error("the Expense waited although the guarded write held no lock")
			}
			if got.violation == "" {
				t.Errorf("create %s, write %s: nothing is wrong, so the lock is not what the guard relies on", got.create, got.removal)
			}
		})
	}
}

// The Pocket guard has two layers, the Household lock and the Pocket's own (see
// the top of this file), so taking either one off, on either side, stays green:
// the other carries it. This is expected to PASS; it documents that stripping
// only LockLivePocketShared's FOR SHARE proves nothing about that lock, and is
// why the red proofs above strip every lock of a side.
func TestThePocketGuardHoldsOnEitherLayerAlone(t *testing.T) {
	g := guards[0]
	if g.name != "POCKET_IN_USE" {
		t.Fatal("guards[0] is no longer the Pocket guard")
	}
	for name, run := range map[string]func() result{
		"create without its Pocket lock":     func() result { return g.holdCreateWithout(t, []string{lockPocketShared}) },
		"create without its Household lock":  func() result { return g.holdCreateWithout(t, []string{lockHouseholdShared}) },
		"removal without its Pocket lock":    func() result { return g.holdRemovalWithout(t, []string{"LockPocket"}) },
		"removal without its Household lock": func() result { return g.holdRemovalWithout(t, []string{"LockHousehold"}) },
	} {
		got := run()
		if !got.followerBlocked || got.violation != "" {
			t.Errorf("%s: blocked %v, violation %q; the other layer was expected to carry the guard", name, got.followerBlocked, got.violation)
		}
	}
}
