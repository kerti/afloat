// Package dashboard answers what the Household's Budget and Expenses add up to
// for the Budget Period it is in (PRD D1 to D9, issue #155): one read, computed
// on the server, from one snapshot. Handlers live with their domain
// (docs/adr/go/0001).
//
// The dependency runs one way: this package imports auth, budget and period
// (and, once it computes a figure, trajectory, which stays pure); none of them
// imports this one. Expenses are read only here and only through PeriodSpend,
// so every figure that reads them agrees about one Expense.
package dashboard

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
)

// Handlers implements the dashboard's half of api.StrictServerInterface.
type Handlers struct {
	q   db.Querier
	tx  budget.Beginner
	now func() time.Time
}

type Deps struct {
	Querier  db.Querier
	Beginner budget.Beginner
	// Now is the clock. Production passes time.Now; a test pins an instant, so
	// Today is asserted on exact dates. Nil means time.Now.
	Now func() time.Time
}

func New(d Deps) *Handlers {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Handlers{q: d.Querier, tx: d.Beginner, now: now}
}

// GetDashboard answers the dashboard of the Period the caller is in now. In
// this order: the User's Household and Position (budget.LocateHousehold, one
// read), the Period brought into being if no read has yet
// (budget.EnsurePeriod, committed in a transaction of its own, as GET /budget
// does, so a Household that carried a Budget forward is never told it has
// none), then everything the answer holds read from one snapshot. The
// Household is the authenticated User's, taken from the context and never from
// the request, and every statement filters on it in SQL.
//
// Only what the merged PRs of #155 render is on the wire (R5): so far the
// currency. PeriodSpend and ListAllocationChangeDays are written and tested but
// feed nothing here yet.
func (h *Handlers) GetDashboard(ctx context.Context, _ api.GetDashboardRequestObject) (api.GetDashboardResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not a read with a zero
		// household_id.
		return api.GetDashboard401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	at, _, err := budget.LocateHousehold(ctx, h.q, h.now, user)
	if err != nil {
		slog.Error("get dashboard: resolve the period", "err", err)
		return internalError(), nil
	}
	if err := budget.EnsurePeriod(ctx, h.q, h.tx, user.HouseholdID, at.Period); err != nil {
		slog.Error("get dashboard: ensure the period", "err", err)
		return internalError(), nil
	}
	out, err := h.readSnapshot(ctx, user.HouseholdID)
	if err != nil {
		// Never a figure, a name or an amount in the log.
		slog.Error("get dashboard", "err", err)
		return internalError(), nil
	}
	return api.GetDashboard200JSONResponse(out), nil
}

// readSnapshot answers the dashboard from one snapshot: REPEATABLE READ gives
// every statement of the transaction the first one's view, so the figures
// built from several statements cannot disagree about one Expense or one
// Budget edit that committed between them, and READ ONLY means it can never
// fail to serialise or hold a lock a writer would wait on. The same shape as
// GET /budget's, taken rather than shared: budget's is unexported, and ten
// lines are cheaper than an interface.
func (h *Handlers) readSnapshot(ctx context.Context, householdID pgtype.UUID) (api.Dashboard, error) {
	tx, err := h.tx.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return api.Dashboard{}, fmt.Errorf("begin: %w", err)
	}
	// Nothing was written, so ending it is the same either way; the rollback is
	// detached from ctx so a cancelled request still releases the connection.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	return read(ctx, db.New(tx), householdID)
}

// read is the answer, from one snapshot. The currency is read here, in the
// snapshot, and not taken from the Household row Locate read before the Period
// was ensured: that read is outside the snapshot, and the currency the figures
// are stated in must be the one they were read under.
func read(ctx context.Context, q db.Querier, householdID pgtype.UUID) (api.Dashboard, error) {
	household, err := q.GetHouseholdByID(ctx, householdID)
	if err != nil {
		return api.Dashboard{}, fmt.Errorf("look up household: %w", err)
	}
	return api.Dashboard{Currency: household.ReportingCurrency}, nil
}

// internalError is this package's own 500: auth's panics for a response type it
// does not list, and this one is not on it.
func internalError() api.GetDashboardResponseObject {
	return api.GetDashboard500JSONResponse{
		InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
	}
}
