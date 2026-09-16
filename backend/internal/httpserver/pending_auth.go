package httpserver

import (
	"context"

	"github.com/kerti/afloat/backend/internal/api"
)

// pendingAuth answers the three authentication operations before the auth
// package exists (§11 step 5 is split: this is the skeleton half).
//
// These are NOT stubs returning a fake status. Each is the truthful degenerate
// answer for a server with no sessions and no credential rows:
//
//   - login always fails, because no credential exists to match. The contract's
//     own rule applies — one INVALID_CREDENTIALS for every failure mode, so this
//     is indistinguishable from a wrong password, which is the point.
//   - /me has no session, because none can be issued yet. 401 is correct.
//   - logout is documented as idempotent: succeeding with no session is the
//     specified behaviour, not a shortcut.
//
// The next slice replaces this file with internal/auth. If it is still here
// when credentials exist, login is refusing valid passwords.
type pendingAuth struct{}

func (pendingAuth) LocalLogin(_ context.Context, _ api.LocalLoginRequestObject) (api.LocalLoginResponseObject, error) {
	return api.LocalLogin401JSONResponse{
		UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{
			Code: api.INVALIDCREDENTIALS,
		},
	}, nil
}

func (pendingAuth) GetMe(_ context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	return api.GetMe401JSONResponse{
		UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{
			Code: api.UNAUTHORIZED,
		},
	}, nil
}

func (pendingAuth) Logout(_ context.Context, _ api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	return api.Logout204Response{}, nil
}
