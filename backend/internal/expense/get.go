package expense

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
)

// GetExpense answers one live Expense of the caller's Household, in the shape
// the list and the edit answer (#140 X1). The statement filters on household_id
// in SQL, and an id that is unknown, another Household's or deleted is no row:
// the one `404`, so a foreign id is never told from a missing one. The shape of
// the id is the binder's (400 before this runs), as on every {id} route.
func (h *Handlers) GetExpense(ctx context.Context, request api.GetExpenseRequestObject) (api.GetExpenseResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return api.GetExpense401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	row, err := h.q.GetLiveExpense(ctx, db.GetLiveExpenseParams{
		ID:          pgtype.UUID{Bytes: request.Id, Valid: true},
		HouseholdID: user.HouseholdID,
	})
	switch {
	case err == nil:
		return api.GetExpense200JSONResponse(response(row)), nil
	case errors.Is(err, pgx.ErrNoRows):
		return api.GetExpense404JSONResponse{
			NotFoundJSONResponse: api.NotFoundJSONResponse{Code: api.NOTFOUND},
		}, nil
	default:
		logFailure("get expense", err)
		return api.GetExpense500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
}
