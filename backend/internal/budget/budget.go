// Package budget owns the Household's Budget for the Budget Period it is in
// (PRD S9, S12, S13): reading it, changing its Monthly Budget and its
// Categories' allocations, and bringing a Period into being on its first read. Handlers live with their domain
// (docs/adr/go/0001).
//
// The dependency runs one way: this package imports auth, for the User in the
// request context, and period, for the arithmetic; neither imports this one.
package budget

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
)

// Beginner is the slice of the pool this package needs to run a transaction,
// as auth's is: it keeps the handlers testable without a pool and makes the
// places that need atomicity or one snapshot obvious. Begin is for a write;
// BeginTx is for the one read that must see a single snapshot (see GetBudget).
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Handlers implements the Budget half of api.StrictServerInterface.
type Handlers struct {
	q   db.Querier
	tx  Beginner
	now func() time.Time
}

type Deps struct {
	Querier  db.Querier
	Beginner Beginner
	// Now is the clock. Production passes time.Now; a test pins an instant, so
	// the Period Day arithmetic is asserted on exact dates. Nil means time.Now.
	Now func() time.Time
}

func New(d Deps) *Handlers {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Handlers{q: d.Querier, tx: d.Beginner, now: now}
}

// GetBudget answers the Budget of the Period the caller is in now. The first
// request to find the Period missing brings it into being (PRD S13), in one
// transaction with what it carries forward; every later one only reads. The
// answer is always read from one snapshot (readSnapshot). The
// Household is the authenticated User's, taken from the context and never from
// the request, and every statement filters on it in SQL.
func (h *Handlers) GetBudget(ctx context.Context, _ api.GetBudgetRequestObject) (api.GetBudgetResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not a read with a zero
		// household_id.
		return api.GetBudget401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	where, err := h.where(ctx, user)
	if err != nil {
		slog.Error("get budget: resolve the period", "err", err)
		return getInternalError(), nil
	}

	exists, err := h.q.BudgetExists(ctx, db.BudgetExistsParams{HouseholdID: user.HouseholdID, YearMonth: where.Period.YearMonth})
	if err != nil {
		slog.Error("get budget: look up the period", "err", err)
		return getInternalError(), nil
	}
	if !exists {
		// Only the marker and its copy, committed together. The answer is read
		// afterwards, below, as every answer is.
		err = h.inTx(ctx, func(q *db.Queries) error {
			return Materialise(ctx, q, user.HouseholdID, where.Period)
		})
		if err != nil {
			slog.Error("get budget: materialise the period", "err", err)
			return getInternalError(), nil
		}
	}
	out, err := h.readSnapshot(ctx, user.HouseholdID, where)
	if err != nil {
		slog.Error("get budget", "err", err)
		return getInternalError(), nil
	}
	return api.GetBudget200JSONResponse(out), nil
}

// readSnapshot answers the Budget from one snapshot. The answer is built from
// several statements (the Monthly Budget's versions, then the allocations as of
// a day the versions decide), and a concurrent edit that commits between two of
// them would otherwise be half in the answer: the old Monthly Budget with the
// new allocations, a Budget that never existed. REPEATABLE READ gives every
// statement of the transaction the first one's snapshot, and READ ONLY means it
// can never fail to serialise or hold a lock a writer would wait on.
func (h *Handlers) readSnapshot(ctx context.Context, householdID pgtype.UUID, at Position) (api.Budget, error) {
	tx, err := h.tx.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return api.Budget{}, fmt.Errorf("begin: %w", err)
	}
	// Nothing was written, so ending it is the same either way; the rollback is
	// detached from ctx so a cancelled request still releases the connection.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	return read(ctx, db.New(tx), householdID, at)
}

// UpdateBudget changes the current Period's Monthly Budget and allocations.
// Merge semantics (BOOTSTRAP.md §4): a field left out is not touched and `{}` is
// a no-op that still answers the Budget, and still materialises a Period nobody
// had read. Allocations change the current Period only (#130 R1, R2): earlier
// Periods are never touched, and the following ones carry the latest state
// forward when first read.
//
// A change is a version dated the writer's Today. One upsert on its key, so a
// second change the same Period Day overwrites the first and two writers of
// different fields both land; the Period before the change is never redrawn.
// What can be refused is refused before anything is written (checkAllocations).
// Materialising, the Monthly Budget and every allocation are one transaction, so
// a failure at any of them changes nothing. The answer is read afterwards, from
// a snapshot of its own, the one GET reads from (readSnapshot): the maintainer's
// ruling on #130, so that no answer can be half of a concurrent edit. A failure
// of that read answers 500 with the change committed; the save stands, and
// repeating the request is safe (a PATCH of absolute values).
func (h *Handlers) UpdateBudget(ctx context.Context, request api.UpdateBudgetRequestObject) (api.UpdateBudgetResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return api.UpdateBudget401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil {
		// Unreachable: the contract's body is required, and the validator
		// refuses an absent one first.
		return api.UpdateBudget400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	var monthly *decimal.Decimal
	if body.MonthlyBudget != nil {
		d, err := decimal.NewFromString(*body.MonthlyBudget)
		if err != nil {
			// Unreachable: the contract's pattern admits only what parses.
			return api.UpdateBudget400JSONResponse{
				BadRequestJSONResponse: api.BadRequestJSONResponse{
					Code: api.VALIDATION,
					Args: &map[string]interface{}{"field": "monthly_budget", "rule": "pattern"},
				},
			}, nil
		}
		monthly = &d
	}

	changes, refusal, err := h.checkAllocations(ctx, user, body.Allocations)
	if err != nil {
		slog.Error("update budget: check the allocations", "err", err)
		return updateInternalError(), nil
	}
	if refusal != nil {
		return refusal, nil
	}

	where, err := h.where(ctx, user)
	if err != nil {
		slog.Error("update budget: resolve the period", "err", err)
		return updateInternalError(), nil
	}

	err = h.inTx(ctx, func(q *db.Queries) error {
		if err := Materialise(ctx, q, user.HouseholdID, where.Period); err != nil {
			return err
		}
		if monthly != nil {
			if err := q.UpsertMonthlyBudget(ctx, db.UpsertMonthlyBudgetParams{
				HouseholdID:   user.HouseholdID,
				YearMonth:     where.Period.YearMonth,
				EffectiveOn:   pgDate(where.Today),
				MonthlyBudget: *monthly,
			}); err != nil {
				return fmt.Errorf("upsert monthly budget: %w", err)
			}
		}
		for _, c := range changes {
			if err := ChangeAllocation(ctx, q, user.HouseholdID, where, c.categoryID, c.amount); err != nil {
				return err
			}
		}
		return nil
	})
	switch {
	case errors.Is(err, ErrCategoryNotLive):
		// A Category that was live when it was checked is not now (deleted in
		// between), and the transaction has rolled back. The same answer as
		// one that was never live.
		return updateValidation("category_id", "invalid"), nil
	case err != nil:
		// Never the request's body or an amount in the log.
		slog.Error("update budget", "err", err)
		return updateInternalError(), nil
	}
	out, err := h.readSnapshot(ctx, user.HouseholdID, where)
	if err != nil {
		// The change is committed. Not a rollback, and not a 200 either: the
		// caller is told the answer failed and may repeat the request.
		slog.Error("update budget: read the answer", "err", err)
		return updateInternalError(), nil
	}
	return api.UpdateBudget200JSONResponse(out), nil
}

// allocationChange is one checked item of a request: a Category and what it is
// now planned at, none being an invalid NullDecimal.
type allocationChange struct {
	categoryID pgtype.UUID
	amount     decimal.NullDecimal
}

// checkAllocations is the handler's half of the request's rules, run after the
// contract's and before anything is written, so a refused request changes
// nothing, not even by materialising a Period. A category_id listed twice is
// `unique`; then, item by item in the order sent, a Category that is not a live
// one of this Household is `invalid` (unknown, another Household's and deleted
// are one answer), and `null` for a Commitment is `required` (PRD S8: a
// Commitment always has an allocation; the bucket is fixed at creation, so what
// is read here cannot change before the write). The checked changes come back
// sorted by Category id, the order the transaction writes them in: two requests
// that touch the same Categories in opposite orders would otherwise each hold
// a row the other is waiting on. Exactly one of changes, refusal and err is
// meaningful.
func (h *Handlers) checkAllocations(ctx context.Context, user db.User, items *[]api.AllocationChange) ([]allocationChange, api.UpdateBudgetResponseObject, error) {
	if items == nil || len(*items) == 0 {
		return nil, nil, nil
	}

	seen := make(map[[16]byte]bool, len(*items))
	for _, item := range *items {
		if seen[item.CategoryId] {
			return nil, updateValidation("category_id", "unique"), nil
		}
		seen[item.CategoryId] = true
	}

	categories, err := h.q.ListCategories(ctx, user.HouseholdID)
	if err != nil {
		return nil, nil, fmt.Errorf("list categories: %w", err)
	}
	bucketOf := make(map[[16]byte]string, len(categories))
	for _, c := range categories {
		bucketOf[c.ID.Bytes] = c.Bucket
	}

	changes := make([]allocationChange, 0, len(*items))
	for _, item := range *items {
		bucket, live := bucketOf[item.CategoryId]
		if !live {
			return nil, updateValidation("category_id", "invalid"), nil
		}
		change := allocationChange{categoryID: pgtype.UUID{Bytes: item.CategoryId, Valid: true}}
		if item.Allocation == nil {
			if api.Bucket(bucket) == api.Commitment {
				return nil, updateValidation("allocation", "required"), nil
			}
		} else {
			d, err := decimal.NewFromString(*item.Allocation)
			if err != nil {
				// Unreachable: the contract's pattern admits only what parses.
				return nil, updateValidation("allocation", "pattern"), nil
			}
			change.amount = decimal.NullDecimal{Decimal: d, Valid: true}
		}
		changes = append(changes, change)
	}
	slices.SortFunc(changes, func(a, b allocationChange) int {
		return bytes.Compare(a.categoryID.Bytes[:], b.categoryID.Bytes[:])
	})
	return changes, nil, nil
}

// Position is the Household's current position: the Period Day it is on and the
// Budget Period that day belongs to. The Category package, which writes an
// allocation in its own transaction, asks for it through Locate.
type Position struct {
	Today  period.Date
	Period period.Period
}

// where is Locate on the handlers' own querier and clock.
func (h *Handlers) where(ctx context.Context, user db.User) (Position, error) {
	return Locate(ctx, h.q, h.now, user)
}

// Locate works out Today from the User's own time zone and the Household's
// day_starts_at, and the Period it falls in from the Household's current
// period_start_day. The only place a request's date is derived (BOOTSTRAP.md
// §4): the client never sends one and never computes one.
func Locate(ctx context.Context, q db.Querier, now func() time.Time, user db.User) (Position, error) {
	household, err := q.GetHouseholdByID(ctx, user.HouseholdID)
	if err != nil {
		return Position{}, fmt.Errorf("look up household: %w", err)
	}
	loc, err := time.LoadLocation(user.TimeZone)
	if err != nil {
		return Position{}, fmt.Errorf("load time zone: %w", err)
	}
	if !household.DayStartsAt.Valid {
		return Position{}, errors.New("household has no day_starts_at")
	}
	dayStartsAt := time.Duration(household.DayStartsAt.Microseconds) * time.Microsecond

	today := period.Today(now(), loc, dayStartsAt)
	p, err := period.Of(today, int(household.PeriodStartDay))
	if err != nil {
		return Position{}, err
	}
	return Position{Today: today, Period: p}, nil
}

// inTx runs fn in a transaction, committed only if fn returns nil.
func (h *Handlers) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := h.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// A no-op after Commit. Every other return rolls back. Detached from ctx so
	// a cancelled request still releases the connection.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(db.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Materialise brings a Budget Period into being (PRD S12, S13), idempotently
// and safely under concurrency: the marker insert is ON CONFLICT DO NOTHING, and
// only the request that actually inserted it copies. A request that loses the
// race waits on the winner's uncommitted marker, then finds it and does
// nothing, and reads the winner's committed copy. It must run in the same
// transaction as everything that depends on the marker, so no request ever
// sees a marker without its copy.
//
// What is carried forward, and from where, is CopySource's: the one place that
// chooses a source, for the Monthly Budget and the allocations alike.
func Materialise(ctx context.Context, q *db.Queries, householdID pgtype.UUID, p period.Period) error {
	_, err := q.InsertBudget(ctx, db.InsertBudgetParams{HouseholdID: householdID, YearMonth: p.YearMonth})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("insert budget: %w", err)
	}
	source, err := q.CopySource(ctx, db.CopySourceParams{HouseholdID: householdID, YearMonth: p.YearMonth})
	if errors.Is(err, pgx.ErrNoRows) {
		// The Household's first Period: nothing to carry, which is what its
		// first read answers.
		return nil
	}
	if err != nil {
		return fmt.Errorf("choose the copy source: %w", err)
	}
	if _, err := q.CopyMonthlyBudgetForward(ctx, db.CopyMonthlyBudgetForwardParams{
		HouseholdID: householdID,
		YearMonth:   p.YearMonth,
		EffectiveOn: pgDate(p.StartsOn),
		Source:      source,
	}); err != nil {
		return fmt.Errorf("copy monthly budget forward: %w", err)
	}
	if _, err := q.CopyAllocationsForward(ctx, db.CopyAllocationsForwardParams{
		HouseholdID: householdID,
		YearMonth:   p.YearMonth,
		EffectiveOn: pgDate(p.StartsOn),
		Source:      source,
	}); err != nil {
		return fmt.Errorf("copy allocations forward: %w", err)
	}
	return nil
}

// CarryCategoryForward gives one Category the allocation a Period's carry-forward
// would have given it had it never been deleted (#130 C8, the maintainer's
// ruling on a restore): the latest state of the nearest earlier Period that holds
// an allocation for it, dated the Period's first day, when that state is an
// amount. Nothing when the Category already has any row in the Period (deleted
// and restored within it, or the copy took it), when no other Period holds one
// for it (a Daily Category that never had an allocation) or when its latest state
// is "none".
//
// It is Materialise's own rule, not a second one: the same two statements,
// CopySource and CopyAllocationsForward, with the Category named. The caller
// has materialised the Period in the same transaction (the allocations' foreign
// key needs the marker) and holds the Category's row lock.
func CarryCategoryForward(ctx context.Context, q *db.Queries, householdID pgtype.UUID, p period.Period, categoryID pgtype.UUID) error {
	source, err := q.CopySource(ctx, db.CopySourceParams{HouseholdID: householdID, YearMonth: p.YearMonth, CategoryID: categoryID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("choose the copy source: %w", err)
	}
	if _, err := q.CopyAllocationsForward(ctx, db.CopyAllocationsForwardParams{
		HouseholdID: householdID,
		YearMonth:   p.YearMonth,
		EffectiveOn: pgDate(p.StartsOn),
		Source:      source,
		CategoryID:  categoryID,
	}); err != nil {
		return fmt.Errorf("carry the category forward: %w", err)
	}
	return nil
}

// SetAllocation records a Category's allocation as a version dated the writer's
// Today in the Period they are in, which the caller has materialised in the same
// transaction: ChangeAllocation for an amount.
func SetAllocation(ctx context.Context, q *db.Queries, householdID pgtype.UUID, at Position, categoryID pgtype.UUID, amount decimal.Decimal) error {
	return ChangeAllocation(ctx, q, householdID, at, categoryID, decimal.NullDecimal{Decimal: amount, Valid: true})
}

// ChangeAllocation records what a Category is planned at as a version dated the
// writer's Today in the Period they are in, which the caller has materialised
// in the same transaction; an invalid amount is "none from that day". One upsert
// on its key: a second change the same Period Day overwrites the first, and two
// writers of different Categories both land. The statement checks the Category
// is the Household's own and live, and share-locks it; zero rows is a Category
// that is not.
func ChangeAllocation(ctx context.Context, q *db.Queries, householdID pgtype.UUID, at Position, categoryID pgtype.UUID, amount decimal.NullDecimal) error {
	n, err := q.UpsertAllocation(ctx, db.UpsertAllocationParams{
		HouseholdID: householdID,
		YearMonth:   at.Period.YearMonth,
		EffectiveOn: pgDate(at.Today),
		CategoryID:  categoryID,
		Allocation:  amount,
	})
	if err != nil {
		return fmt.Errorf("upsert allocation: %w", err)
	}
	if n == 0 {
		return ErrCategoryNotLive
	}
	return nil
}

// ErrCategoryNotLive is SetAllocation's answer for a Category that is unknown,
// another Household's or deleted: the three are one.
var ErrCategoryNotLive = errors.New("the Category is not a live Category of the Household")

// read answers the Budget as of Today. The figures are computed here, and the
// client derives none (PRD N5a). The Monthly Budget's versions come from one
// statement and the allocations from another, both read as of the one day
// InForceDay names, and every allocation figure is summed from the one list
// that is sent, so the list and the sums cannot disagree.
func read(ctx context.Context, q db.Querier, householdID pgtype.UUID, at Position) (api.Budget, error) {
	rows, err := q.ListMonthlyBudgetVersions(ctx, db.ListMonthlyBudgetVersionsParams{
		HouseholdID: householdID,
		YearMonth:   at.Period.YearMonth,
	})
	if err != nil {
		return api.Budget{}, fmt.Errorf("list monthly budget versions: %w", err)
	}
	versions := make([]Version, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, Version{
			EffectiveOn:   period.DateOf(row.EffectiveOn.Time),
			MonthlyBudget: row.MonthlyBudget,
		})
	}
	monthly, has := MonthlyBudgetAsOf(versions, at.Today)

	allocated, err := q.AllocationsAsOf(ctx, db.AllocationsAsOfParams{
		HouseholdID: householdID,
		YearMonth:   at.Period.YearMonth,
		AsOf:        pgDate(InForceDay(versions, at.Today)),
	})
	if err != nil {
		return api.Budget{}, fmt.Errorf("list allocations: %w", err)
	}
	allocations := make([]api.Allocation, 0, len(allocated))
	commitments, daily := decimal.Zero, decimal.Zero
	for _, row := range allocated {
		allocations = append(allocations, api.Allocation{
			CategoryId: openapi_types.UUID(row.CategoryID.Bytes),
			Allocation: money(row.Allocation.Decimal),
		})
		switch api.Bucket(row.Bucket) {
		case api.Commitment:
			commitments = commitments.Add(row.Allocation.Decimal)
		case api.Daily:
			daily = daily.Add(row.Allocation.Decimal)
		default:
			// The CHECK on categories.bucket makes this unreachable; failing
			// loudly beats a figure that quietly leaves a Category out.
			return api.Budget{}, fmt.Errorf("a Category has the bucket %q", row.Bucket)
		}
	}

	f := ComputeFigures(monthly, has, commitments, daily)

	out := api.Budget{
		Period: api.BudgetPeriod{
			YearMonth: at.Period.YearMonth,
			StartsOn:  openapi_types.Date{Time: at.Period.StartsOn.Time()},
			EndsOn:    openapi_types.Date{Time: at.Period.EndsOn.Time()},
		},
		Allocations:       allocations,
		CommitmentsBudget: money(f.CommitmentsBudget),
	}
	if f.HasMonthlyBudget {
		out.MonthlyBudget = ptr(money(f.MonthlyBudget))
		out.DailyPool = ptr(money(f.DailyPool))
		out.Unallocated = ptr(money(f.Unallocated))
	}
	return out, nil
}

// money is the wire spelling of an amount: exactly four decimals, never trimmed
// and never an exponent (BOOTSTRAP.md §4). StringFixed, not String, which strips
// trailing zeros.
func money(d decimal.Decimal) api.Money {
	return d.StringFixed(4)
}

func ptr[T any](v T) *T { return &v }

func pgDate(d period.Date) pgtype.Date {
	return pgtype.Date{Time: d.Time(), Valid: true}
}

// getInternalError and updateInternalError are this package's own 500s: auth's
// panics for a response type it does not list, and these are not on it.
func getInternalError() api.GetBudgetResponseObject {
	return api.GetBudget500JSONResponse{
		InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
	}
}

func updateValidation(field, rule string) api.UpdateBudgetResponseObject {
	return api.UpdateBudget400JSONResponse{
		BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: api.VALIDATION,
			Args: &map[string]interface{}{"field": field, "rule": rule},
		},
	}
}

func updateInternalError() api.UpdateBudgetResponseObject {
	return api.UpdateBudget500JSONResponse{
		InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
	}
}
