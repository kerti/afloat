package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// errCredentialChanged means the credential was no longer the one the current
// password verified against when the swap ran: another change landed in
// between. It is answered as a wrong current password, which it now is.
var errCredentialChanged = errors.New("credential changed since the current password was verified")

// ChangeLocalPassword sets a new password for the signed-in User, behind their
// current one, and revokes every session they hold, the caller's included,
// minting a fresh one for the caller in the same transaction (BOOTSTRAP.md
// §5.1). The order is the point (§5.1.1), each step reachable only if the
// earlier ones passed:
//
//  1. the new password against the policy, before any hashing and before the
//     throttle, so a weak one costs nothing and records nothing;
//  2. the current password (proveCurrentPassword): throttle, credential,
//     permit, throttle again, verify;
//  3. the new password hashed under the same permit, which is then released and
//     the User's failed guesses cleared;
//  4. one transaction: compare-and-swap the credential, revoke every session,
//     insert the caller's new one.
//
// A new password equal to the current one is allowed: nothing in the PRD asks
// otherwise, and refusing it is enforcement.
func (h *Handlers) ChangeLocalPassword(ctx context.Context, request api.ChangeLocalPasswordRequestObject) (api.ChangeLocalPasswordResponseObject, error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		// Unreachable through the router (see UpdateMe); kept for the same reason.
		return api.ChangeLocalPassword401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil {
		return api.ChangeLocalPassword400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	if err := ValidatePasswordPolicy(body.NewPassword); err != nil {
		return api.ChangeLocalPassword400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{
				Code: api.WEAKPASSWORD,
				Args: &map[string]interface{}{"reason": weakPasswordReason(err)},
			},
		}, nil
	}

	proof, refused := h.proveCurrentPassword(ctx, "change password", user.ID, body.CurrentPassword)
	switch refused.kind {
	case refusedThrottled:
		retryAfter := refused.retryAfter
		return api.ChangeLocalPassword429JSONResponse{
			TooManyRequestsJSONResponse: api.TooManyRequestsJSONResponse{
				Body:    api.Error{Code: api.TOOMANYATTEMPTS},
				Headers: api.TooManyRequestsResponseHeaders{RetryAfter: &retryAfter},
			},
		}, nil
	case refusedWrong:
		return invalidCurrentPasswordForPasswordChange(), nil
	case refusedInternal:
		return internalError[api.ChangeLocalPasswordResponseObject]()
	}
	defer proof.close()

	newHash, err := hashHoldingPermit(body.NewPassword)
	if err != nil {
		slog.Error("change password: hash new password", "err", err)
		return internalError[api.ChangeLocalPasswordResponseObject]()
	}
	proof.release()
	proof.clear()

	token, expiresAt, err := h.rotateCredential(proof.ctx, user.ID, proof.hash, newHash, userAgentFrom(ctx))
	switch {
	case errors.Is(err, errCredentialChanged):
		return invalidCurrentPasswordForPasswordChange(), nil
	case err != nil:
		logQueryError("change password: rotate credential", err)
		return internalError[api.ChangeLocalPasswordResponseObject]()
	}

	// The contract declares Set-Cookie on this 204, so the generated response
	// carries it. The generated visitor writes it with Header().Set, which
	// replaces any refreshed-cookie header the session middleware added for the
	// OLD token on a session past half its life: the client must end up holding
	// this one.
	setCookie := h.sessionCookie(token, expiresAt).String()
	return api.ChangeLocalPassword204Response{
		Headers: api.ChangeLocalPassword204ResponseHeaders{SetCookie: &setCookie},
	}, nil
}

// rotateCredential is the password change's one transaction: swap the
// credential, revoke every session the User holds, mint the caller's new one.
// Revoking first and minting after, in one transaction, is §5.1's reset recipe:
// a thief holding this very browser's cookie holds the token being revoked, so
// "revoke the others" would leave exactly that session live.
//
// errCredentialChanged means the swap matched no row; the transaction rolled
// back, so no session was touched.
func (h *Handlers) rotateCredential(ctx context.Context, userID pgtype.UUID, verifiedHash, newHash, userAgent string) (string, time.Time, error) {
	tx, err := h.tx.Begin(ctx)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("begin: %w", err)
	}
	// A no-op after Commit. Every other return rolls back, so a failure leaves
	// the credential and every session exactly as they were.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := db.New(tx)

	swapped, err := q.SwapCredentialPasswordHash(ctx, db.SwapCredentialPasswordHashParams{
		NewPasswordHash:      newHash,
		UserID:               userID,
		VerifiedPasswordHash: verifiedHash,
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("swap credential: %w", err)
	}
	if swapped == 0 {
		return "", time.Time{}, errCredentialChanged
	}
	if err := q.DeleteSessionsForUser(ctx, userID); err != nil {
		return "", time.Time{}, fmt.Errorf("revoke sessions: %w", err)
	}
	token, expiresAt, err := h.createSessionWith(ctx, q, userID, userAgent)
	if err != nil {
		return "", time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", time.Time{}, fmt.Errorf("commit: %w", err)
	}
	return token, expiresAt, nil
}

func invalidCurrentPasswordForPasswordChange() api.ChangeLocalPasswordResponseObject {
	return api.ChangeLocalPassword403JSONResponse{
		ForbiddenJSONResponse: api.ForbiddenJSONResponse{Code: api.INVALIDCURRENTPASSWORD},
	}
}
