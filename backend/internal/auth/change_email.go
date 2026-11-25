package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// ChangeLocalEmail changes the signed-in User's sign-in address, behind their
// current password. The change takes effect at once and is not verified: there
// is no mail transport (PRD §4). No session is touched, because a session is
// keyed on the User's id and not on the address; what stops a stolen cookie
// from swapping the handle is the current-password gate.
//
// Order (BOOTSTRAP.md §5.1.1): the shape, then the same proof as a password
// change (one function, one key), and only then the write, so an address's
// state is never revealed to a caller who has not proved the password. There is
// no lookup beforehand: the unique index is the race-free answer, and the hash
// has already been paid for.
//
// A 500 changes nothing: the UPDATE and the Household read run in one
// transaction (updateUserAndReadHousehold).
func (h *Handlers) ChangeLocalEmail(ctx context.Context, request api.ChangeLocalEmailRequestObject) (api.ChangeLocalEmailResponseObject, error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		// Unreachable through the router (see UpdateMe); kept for the same reason.
		return api.ChangeLocalEmail401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil {
		return api.ChangeLocalEmail400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}
	email := normalizeEmail(body.Email)

	proof, refused := h.proveCurrentPassword(ctx, "change email", user.ID, body.CurrentPassword)
	switch refused.kind {
	case refusedThrottled:
		retryAfter := refused.retryAfter
		return api.ChangeLocalEmail429JSONResponse{
			TooManyRequestsJSONResponse: api.TooManyRequestsJSONResponse{
				Body:    api.Error{Code: api.TOOMANYATTEMPTS},
				Headers: api.TooManyRequestsResponseHeaders{RetryAfter: &retryAfter},
			},
		}, nil
	case refusedWrong:
		return api.ChangeLocalEmail403JSONResponse{
			ForbiddenJSONResponse: api.ForbiddenJSONResponse{Code: api.INVALIDCURRENTPASSWORD},
		}, nil
	case refusedInternal:
		return internalError[api.ChangeLocalEmailResponseObject]()
	}
	defer proof.close()
	// Nothing more to hash. The password is proved, so its failed guesses are
	// forgotten whether or not the address turns out to be free.
	proof.release()
	proof.clear()

	// The write and the Household read the answer needs are one transaction, on
	// the proof's context, so a 500 from here changes nothing: the address is
	// never moved under a caller who was told it failed.
	row, household, err := h.updateUserAndReadHousehold(proof.ctx, func(q *db.Queries) (db.User, error) {
		return q.UpdateUserEmail(proof.ctx, db.UpdateUserEmailParams{
			Email:       email,
			UserID:      user.ID,
			HouseholdID: user.HouseholdID,
		})
	})
	switch {
	case isEmailTaken(err):
		// Nothing changed: the unique violation aborted the transaction, which
		// rolled back. The caller's own address, in any case, is their own row and
		// no conflict; a soft-deleted User's is free (the index is partial).
		return api.ChangeLocalEmail409JSONResponse{
			ConflictJSONResponse: api.ConflictJSONResponse{Code: api.EMAILTAKEN},
		}, nil
	case errors.Is(err, errUserMissing):
		slog.Error("change email: user missing", "user_id", user.ID)
		return internalError[api.ChangeLocalEmailResponseObject]()
	case err != nil:
		logQueryError("change email", err)
		return internalError[api.ChangeLocalEmailResponseObject]()
	}
	me, err := h.meResponse(row, household)
	if err != nil {
		slog.Error("change email: work out today", "err", err)
		return internalError[api.ChangeLocalEmailResponseObject]()
	}
	return api.ChangeLocalEmail200JSONResponse(me), nil
}
