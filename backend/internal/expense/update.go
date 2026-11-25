package expense

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
)

// errNotFound is an Expense that is unknown, another Household's or deleted:
// one answer, so a foreign id is never told from a missing one.
var errNotFound = errors.New("no such expense")

// UpdateExpense corrects an Expense (BOOTSTRAP.md §4 "Updates are PATCH", #140
// X9, R9). It is the answer to a retried capture that was edited in between: a
// replayed POST answers the stored row and applies nothing, so the client
// PATCHes the Expense to what is on screen.
//
// The body's own rules are judged first, before the database is asked anything,
// so a foreign id with a bad body answers what an unknown one does. Then one
// transaction of statements in this order:
//
//  1. Read the Expense FOR NO KEY UPDATE, scoped by household_id. No row is
//     `404`, the one answer for unknown, another Household's and deleted.
//  2. Lock, FOR SHARE and in the create's order (Household, Pocket, Category,
//     owner), each reference the body names AND that differs from what is
//     stored, each with its liveness predicate in the locking statement. A
//     reference left as stored is not looked at, so the amount of an Expense in
//     a Pocket deactivated since can still be fixed. Refusals are collected and
//     the least (field, rule) is reported, as at creation.
//  3. One UPDATE, which merges column by column and leaves updated_at alone when
//     nothing changed.
//
// The shared locks are what POCKET_IN_USE and CATEGORY_IN_USE rely on in the
// other direction: a delete that locked the Pocket first is waited for, and an
// edit that came first is seen by the delete's EXISTS. The Expense's own row
// lock is for step 2's comparison with the stored value (see
// GetLiveExpenseForUpdate); it is taken first, so it is never held while
// waiting on a row a removal holds and needs nothing of an Expense.
//
// Nothing here logs an amount or a description.
func (h *Handlers) UpdateExpense(ctx context.Context, request api.UpdateExpenseRequestObject) (api.UpdateExpenseResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return api.UpdateExpense401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil || (body.Ownership != nil && !body.Ownership.Valid()) {
		// Unreachable: the contract's body is required and ownership is an enum,
		// and the validator refuses both first.
		return api.UpdateExpense400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	edit, bad := checkEdit(body)
	if bad != nil {
		return api.UpdateExpense400JSONResponse{BadRequestJSONResponse: badRequest(*bad)}, nil
	}

	id := pgtype.UUID{Bytes: request.Id, Valid: true}
	var row db.Expense
	err := h.inTx(ctx, func(q *db.Queries) error {
		stored, err := q.GetLiveExpenseForUpdate(ctx, db.GetLiveExpenseForUpdateParams{ID: id, HouseholdID: user.HouseholdID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return errNotFound
		case err != nil:
			return opaque("read the expense", err)
		}

		if f, err := lockChanged(ctx, q, user.HouseholdID, stored, edit); err != nil {
			return err
		} else if f != nil {
			return refusal{*f}
		}

		params := edit.params
		params.ID, params.HouseholdID = id, user.HouseholdID
		row, err = q.UpdateExpense(ctx, params)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// The row is locked and was read a statement ago, so this is not a
			// race, but the answer for no row is the same either way.
			return errNotFound
		case err != nil:
			return opaque("update", err)
		}
		return nil
	})

	var refused refusal
	switch {
	case err == nil:
		return api.UpdateExpense200JSONResponse(response(row)), nil
	case errors.Is(err, errNotFound):
		return api.UpdateExpense404JSONResponse{
			NotFoundJSONResponse: api.NotFoundJSONResponse{Code: api.NOTFOUND},
		}, nil
	case errors.As(err, &refused):
		return api.UpdateExpense400JSONResponse{BadRequestJSONResponse: badRequest(refused.failure)}, nil
	default:
		logFailure("update expense", err)
		return api.UpdateExpense500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
}

// edit is an UpdateExpenseRequest once its own rules have passed: the
// statement's arguments, and the references that need judging against the
// database if they turn out to differ from what is stored.
type edit struct {
	params db.UpdateExpenseParams
	// pocket and category are set when the body sent a value (not null, not
	// absent); owner when it sent a sole_owner pair.
	pocket, category, owner pgtype.UUID
}

// checkEdit is every rule of an edit's body that the contract cannot state,
// judged before the database is asked anything. All that fail are collected and
// the least (field, rule) is reported. An absent property is not judged.
func checkEdit(body *api.UpdateExpenseRequest) (edit, *failure) {
	var e edit
	var failures []failure

	if body.Amount != nil {
		amount, err := decimal.NewFromString(string(*body.Amount))
		switch {
		case err != nil:
			failures = append(failures, failure{"amount", "pattern"})
		case amount.IsZero():
			failures = append(failures, failure{"amount", ruleNonzero})
		default:
			e.params.Amount = decimal.NullDecimal{Decimal: amount, Valid: true}
		}
	}

	if body.OccurredOn != nil {
		date, err := period.ParseDate(*body.OccurredOn)
		if err != nil || date.Year < 1 {
			failures = append(failures, failure{"occurred_on", "invalid"})
		} else {
			e.params.OccurredOn = pgtype.Date{Time: date.Time(), Valid: true}
		}
	}

	if body.OccurredTime.IsSpecified() {
		e.params.SetOccurredTime = true
		if !body.OccurredTime.IsNull() {
			d, err := period.ParseDayStartsAt(body.OccurredTime.MustGet())
			if err != nil {
				failures = append(failures, failure{"occurred_time", "pattern"})
			} else {
				e.params.OccurredTime = pgtype.Time{Microseconds: d.Microseconds(), Valid: true}
			}
		}
	}

	if body.PocketId != nil {
		e.pocket = pgtype.UUID{Bytes: *body.PocketId, Valid: true}
		e.params.PocketID = e.pocket
	}

	if body.CategoryId.IsSpecified() {
		e.params.SetCategoryID = true
		if !body.CategoryId.IsNull() {
			e.category = pgtype.UUID{Bytes: body.CategoryId.MustGet(), Valid: true}
			e.params.CategoryID = e.category
		}
	}

	if body.Description.IsSpecified() {
		e.params.SetDescription = true
		if !body.Description.IsNull() {
			text, rule := auth.CheckBoundedName(body.Description.MustGet(), maxDescriptionCodePoints)
			if rule != "" {
				failures = append(failures, failure{"description", rule})
			} else {
				e.params.Description = &text
			}
		}
	}

	// ownership and owner_user_id are a pair, as at creation (#140 R6, X12), with
	// one difference: absent is "unchanged" here, where at creation it is joint.
	// Neither sent changes neither; sole_owner needs a User; joint takes none and
	// clears the owner; an owner with no ownership is half a pair. Whether the
	// User is the Household's is judged against the database.
	switch {
	case body.Ownership == nil && body.OwnerUserId == nil:
	case body.Ownership != nil && *body.Ownership == api.SoleOwner && body.OwnerUserId != nil:
		ownership := string(api.SoleOwner)
		e.params.Ownership = &ownership
		e.params.SetOwnerUserID = true
		e.owner = pgtype.UUID{Bytes: *body.OwnerUserId, Valid: true}
		e.params.OwnerUserID = e.owner
	case body.Ownership != nil && *body.Ownership == api.Joint && body.OwnerUserId == nil:
		ownership := string(api.Joint)
		e.params.Ownership = &ownership
		e.params.SetOwnerUserID = true
	default:
		failures = append(failures, failure{"owner_user_id", "invalid"})
	}

	if len(failures) > 0 {
		f := least(failures)
		return edit{}, &f
	}
	return e, nil
}

// lockChanged share-locks the references the edit moves to a different row than
// the stored one and judges them, in the create's order: the Household (only
// when something needs locking), then the Pocket, the Category and the owner.
// It answers the least failure, or nil.
func lockChanged(ctx context.Context, q *db.Queries, household pgtype.UUID, stored db.Expense, e edit) (*failure, error) {
	pocket := e.pocket.Valid && e.pocket != stored.PocketID
	category := e.category.Valid && e.category != stored.CategoryID
	owner := e.owner.Valid && e.owner != stored.OwnerUserID
	if !pocket && !category && !owner {
		return nil, nil
	}

	if _, err := q.LockHouseholdShared(ctx, household); err != nil {
		return nil, opaque("lock the household", err)
	}
	var refused []failure
	if pocket {
		if f, err := lockReference("pocket_id", func() (bool, error) {
			return q.LockLivePocketShared(ctx, db.LockLivePocketSharedParams{ID: e.pocket, HouseholdID: household})
		}); err != nil {
			return nil, err
		} else if f != nil {
			refused = append(refused, *f)
		}
	}
	if category {
		if f, err := lockReference("category_id", func() (bool, error) {
			return q.LockLiveCategoryShared(ctx, db.LockLiveCategorySharedParams{ID: e.category, HouseholdID: household})
		}); err != nil {
			return nil, err
		} else if f != nil {
			refused = append(refused, *f)
		}
	}
	if owner {
		_, err := q.LockLiveUserShared(ctx, db.LockLiveUserSharedParams{ID: e.owner, HouseholdID: household})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			refused = append(refused, failure{"owner_user_id", "invalid"})
		case err != nil:
			return nil, opaque("lock the owner", err)
		}
	}
	if len(refused) > 0 {
		f := least(refused)
		return &f, nil
	}
	return nil, nil
}
