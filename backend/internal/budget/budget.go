// Package budget owns the Household's Budget for the Budget Period it is in
// (PRD S9, S12, S13): reading it, changing its Monthly Budget, and bringing a
// Period into being on its first read. Handlers live with their domain
// (docs/adr/go/0001).
//
// The dependency runs one way: this package imports auth, for the User in the
// request context, and period, for the arithmetic; neither imports this one.
package budget

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
// two places that need atomicity obvious.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
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

	exists, err := h.q.BudgetExists(ctx, db.BudgetExistsParams{HouseholdID: user.HouseholdID, YearMonth: where.period.YearMonth})
	if err != nil {
		slog.Error("get budget: look up the period", "err", err)
		return getInternalError(), nil
	}
	var out api.Budget
	if exists {
		out, err = read(ctx, h.q, user.HouseholdID, where)
	} else {
		err = h.inTx(ctx, func(q *db.Queries) error {
			if err := materialise(ctx, q, user.HouseholdID, where.period); err != nil {
				return err
			}
			out, err = read(ctx, q, user.HouseholdID, where)
			return err
		})
	}
	if err != nil {
		slog.Error("get budget", "err", err)
		return getInternalError(), nil
	}
	return api.GetBudget200JSONResponse(out), nil
}

// UpdateBudget changes the current Period's Monthly Budget. Merge semantics
// (BOOTSTRAP.md §4): a field left out is not touched and `{}` is a no-op that
// still answers the Budget, and still materialises a Period nobody had read.
//
// A change is a version dated the writer's Today. One upsert on its key, so a
// second change the same Period Day overwrites the first and two writers of
// different fields both land; the Period before the change is never redrawn.
// Materialising, the write and the read that answers are one transaction, so a
// failure at any step changes nothing.
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

	where, err := h.where(ctx, user)
	if err != nil {
		slog.Error("update budget: resolve the period", "err", err)
		return updateInternalError(), nil
	}

	var out api.Budget
	err = h.inTx(ctx, func(q *db.Queries) error {
		if err := materialise(ctx, q, user.HouseholdID, where.period); err != nil {
			return err
		}
		if monthly != nil {
			if err := q.UpsertMonthlyBudget(ctx, db.UpsertMonthlyBudgetParams{
				HouseholdID:   user.HouseholdID,
				YearMonth:     where.period.YearMonth,
				EffectiveOn:   pgDate(where.today),
				MonthlyBudget: *monthly,
			}); err != nil {
				return fmt.Errorf("upsert monthly budget: %w", err)
			}
		}
		out, err = read(ctx, q, user.HouseholdID, where)
		return err
	})
	if err != nil {
		// Never the request's body or an amount in the log.
		slog.Error("update budget", "err", err)
		return updateInternalError(), nil
	}
	return api.UpdateBudget200JSONResponse(out), nil
}

// whereAndWhen is the Household's current position: the Period Day it is on
// and the Budget Period that day belongs to.
type whereAndWhen struct {
	today  period.Date
	period period.Period
}

// where works out Today from the User's own time zone and the Household's
// day_starts_at, and the Period it falls in from the Household's current
// period_start_day. The only place a request's date is derived (BOOTSTRAP.md
// §4): the client never sends one and never computes one.
func (h *Handlers) where(ctx context.Context, user db.User) (whereAndWhen, error) {
	household, err := h.q.GetHouseholdByID(ctx, user.HouseholdID)
	if err != nil {
		return whereAndWhen{}, fmt.Errorf("look up household: %w", err)
	}
	loc, err := time.LoadLocation(user.TimeZone)
	if err != nil {
		return whereAndWhen{}, fmt.Errorf("load time zone: %w", err)
	}
	if !household.DayStartsAt.Valid {
		return whereAndWhen{}, errors.New("household has no day_starts_at")
	}
	dayStartsAt := time.Duration(household.DayStartsAt.Microseconds) * time.Microsecond

	today := period.Today(h.now(), loc, dayStartsAt)
	p, err := period.Of(today, int(household.PeriodStartDay))
	if err != nil {
		return whereAndWhen{}, err
	}
	return whereAndWhen{today: today, period: p}, nil
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

// materialise brings a Budget Period into being (PRD S12, S13), idempotently
// and safely under concurrency: the marker insert is ON CONFLICT DO NOTHING, and
// only the request that actually inserted it copies. A request that loses the
// race waits on the winner's uncommitted marker, then finds it and does
// nothing, and reads the winner's committed copy. It must run in the same
// transaction as everything that depends on the marker, so no request ever
// sees a marker without its copy.
//
// What is carried forward, and from where, is CopyMonthlyBudgetForward's: the
// one place that chooses a source.
func materialise(ctx context.Context, q *db.Queries, householdID pgtype.UUID, p period.Period) error {
	_, err := q.InsertBudget(ctx, db.InsertBudgetParams{HouseholdID: householdID, YearMonth: p.YearMonth})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("insert budget: %w", err)
	}
	if _, err := q.CopyMonthlyBudgetForward(ctx, db.CopyMonthlyBudgetForwardParams{
		HouseholdID: householdID,
		YearMonth:   p.YearMonth,
		EffectiveOn: pgDate(p.StartsOn),
	}); err != nil {
		return fmt.Errorf("copy monthly budget forward: %w", err)
	}
	return nil
}

// read answers the Budget as of Today. Everything comes from one statement's
// snapshot of the Monthly Budget's versions; the figures are computed here, and
// the client derives none (PRD N5a).
func read(ctx context.Context, q db.Querier, householdID pgtype.UUID, at whereAndWhen) (api.Budget, error) {
	rows, err := q.ListMonthlyBudgetVersions(ctx, db.ListMonthlyBudgetVersionsParams{
		HouseholdID: householdID,
		YearMonth:   at.period.YearMonth,
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
	monthly, has := MonthlyBudgetAsOf(versions, at.today)

	// No Category exists yet, so there is nothing to allocate: the Commitments
	// Budget and the Daily allocations are both nothing. The slice that adds
	// Categories feeds their sums in here.
	f := ComputeFigures(monthly, has, decimal.Zero, decimal.Zero)

	out := api.Budget{
		Period: api.BudgetPeriod{
			YearMonth: at.period.YearMonth,
			StartsOn:  openapi_types.Date{Time: at.period.StartsOn.Time()},
			EndsOn:    openapi_types.Date{Time: at.period.EndsOn.Time()},
		},
		Allocations:       []api.Allocation{},
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

func updateInternalError() api.UpdateBudgetResponseObject {
	return api.UpdateBudget500JSONResponse{
		InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
	}
}
