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

// DeleteExpense soft-deletes an Expense (BOOTSTRAP.md §4 "Soft delete", #140
// X10, R11): the row stays with deleted_at set, in the Recycle Bin, which never
// empties. It is gone from the live list, from GET and from PATCH (404), and
// still counts as using its Pocket and Category.
//
// One statement, scoped by household_id in SQL, and no transaction or lock:
// nothing references an Expense, so there is no guard to take. Idempotent on the
// caller's own Expense and keeping the first deletion instant, so deleting a
// deleted one is 204 again and changes no column. An id that is unknown or
// another Household's is the one NOT_FOUND. The Household is the authenticated
// User's, taken from the context and never from the request.
func (h *Handlers) DeleteExpense(ctx context.Context, request api.DeleteExpenseRequestObject) (api.DeleteExpenseResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not an UPDATE with a zero
		// household_id.
		return api.DeleteExpense401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	n, err := h.q.SoftDeleteExpense(ctx, db.SoftDeleteExpenseParams{
		ID:          pgtype.UUID{Bytes: request.Id, Valid: true},
		HouseholdID: user.HouseholdID,
	})
	switch {
	case err != nil:
		logFailure("delete expense", err)
		return api.DeleteExpense500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	case n == 0:
		return api.DeleteExpense404JSONResponse{
			NotFoundJSONResponse: api.NotFoundJSONResponse{Code: api.NOTFOUND},
		}, nil
	default:
		return api.DeleteExpense204Response{}, nil
	}
}

// errBrokenReference is a deleted Expense whose Pocket or Category is deleted,
// which the guards (POCKET_IN_USE, CATEGORY_IN_USE) make impossible.
var errBrokenReference = errors.New("a deleted expense references a deleted row")

// RestoreExpense is the Undo of DeleteExpense (#140 X10, X11): it clears
// deleted_at and answers the Expense, every other column as it was.
// Idempotent: a live Expense is 200 as it stands, with nothing written. An id
// that is unknown or another Household's is the one NOT_FOUND.
//
// There is no refusal on what the Expense references. A Pocket or Category
// deactivated since is no reason (history keeps its label, and only a NEW
// reference is judged inactive, #140 R10, R11 "no new restore refusal"), and one
// that was deleted cannot be, since a deleted Expense counts against the delete.
// The restore still takes the two rows FOR SHARE, as every writer of a
// referencing row does, so it cannot bring back a reference to a row deleted
// while the Expense was in the bin; were one found deleted, the invariant that
// forbids it is broken and the answer is 500 with nothing written, never a
// quiet resurrection. It is one transaction of statements in this order:
//
//  1. GetExpenseForUpdate: the Expense in any state, FOR NO KEY UPDATE, scoped
//     by household_id. No row is the one 404. A live one is answered as it
//     stands, and nothing else is looked at.
//  2. LockLivePocketShared, then LockLiveCategoryShared when it has one: the
//     create's own statements, in the create's order (the Pocket before the
//     Category). There is no Household lock: a restore reads no currency, and
//     nothing it writes is counted by a guard that takes the Household.
//  3. RestoreExpense.
//
// Nothing a removal holds ever waits on an Expense row (a delete of a Pocket or
// Category only reads expenses), so taking the Expense first adds no cycle.
func (h *Handlers) RestoreExpense(ctx context.Context, request api.RestoreExpenseRequestObject) (api.RestoreExpenseResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return api.RestoreExpense401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	id := pgtype.UUID{Bytes: request.Id, Valid: true}
	var row db.Expense
	err := h.inTx(ctx, func(q *db.Queries) error {
		stored, err := q.GetExpenseForUpdate(ctx, db.GetExpenseForUpdateParams{ID: id, HouseholdID: user.HouseholdID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return errNotFound
		case err != nil:
			return opaque("read the expense", err)
		}
		if !stored.DeletedAt.Valid {
			row = stored
			return nil
		}

		if _, err := q.LockLivePocketShared(ctx, db.LockLivePocketSharedParams{ID: stored.PocketID, HouseholdID: user.HouseholdID}); errors.Is(err, pgx.ErrNoRows) {
			return errBrokenReference
		} else if err != nil {
			return opaque("lock the pocket", err)
		}
		if stored.CategoryID.Valid {
			if _, err := q.LockLiveCategoryShared(ctx, db.LockLiveCategorySharedParams{ID: stored.CategoryID, HouseholdID: user.HouseholdID}); errors.Is(err, pgx.ErrNoRows) {
				return errBrokenReference
			} else if err != nil {
				return opaque("lock the category", err)
			}
		}

		row, err = q.RestoreExpense(ctx, db.RestoreExpenseParams{ID: id, HouseholdID: user.HouseholdID})
		if err != nil {
			// The row is locked and was read deleted a statement ago, so no
			// row is not a race: a statement that disagrees with its own lock
			// is a failure, and so is any other error.
			return opaque("restore", err)
		}
		return nil
	})

	switch {
	case err == nil:
		return api.RestoreExpense200JSONResponse(response(row)), nil
	case errors.Is(err, errNotFound):
		return api.RestoreExpense404JSONResponse{
			NotFoundJSONResponse: api.NotFoundJSONResponse{Code: api.NOTFOUND},
		}, nil
	default:
		logFailure("restore expense", err)
		return api.RestoreExpense500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
}
