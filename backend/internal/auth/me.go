package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/kerti/afloat/backend/internal/api"
)

// GetMe returns the signed-in User and their Household's settings — the
// client's first call after login and its session probe on load.
//
// One call rather than two because every screen needs both and they change
// rarely; the cost is that a Household settings change invalidates it.
func (h *Handlers) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return api.GetMe401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	// Scoped by the session's own household_id — read from the authenticated
	// User, never from anything the request supplied.
	household, err := h.q.GetHouseholdByID(ctx, user.HouseholdID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// A User whose Household is gone cannot be served coherently.
			slog.Error("me: household missing for user", "household_id", user.HouseholdID)
		} else {
			logQueryError("me: look up household", err)
		}
		return internalError[api.GetMeResponseObject]()
	}

	return api.GetMe200JSONResponse(meResponse(user, household)), nil
}

// Logout destroys the session and clears the cookie.
//
// Idempotent by contract: calling it without a session is a success, so a
// client clearing a stale cookie never has to special-case the result.
func (h *Handlers) Logout(ctx context.Context, _ api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	// Revocation IS the row delete — sessions are instance-local auth state and
	// exempt from the soft-delete rule (BOOTSTRAP.md §4).
	if token := sessionTokenFrom(ctx); token != "" {
		if err := h.q.DeleteSession(ctx, HashToken(token)); err != nil {
			logQueryError("logout: delete session", err)
			return internalError[api.LogoutResponseObject]()
		}
	}

	// Cleared unconditionally, including when there was no session: the point is
	// that the client stops holding a token, and a stale cookie is exactly the
	// case where it has one the server does not.
	cleared := h.ClearedSessionCookie().String()
	return api.Logout204Response{
		Headers: api.Logout204ResponseHeaders{SetCookie: &cleared},
	}, nil
}
