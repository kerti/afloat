// Package system serves the two unauthenticated endpoints the contract tags
// `system`: liveness and the pre-auth provider list.
package system

import (
	"context"
	"log/slog"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// Handlers implements the system half of api.StrictServerInterface.
type Handlers struct {
	q       db.Querier
	version string

	localEnabled  bool
	googleEnabled bool
}

type Deps struct {
	Querier       db.Querier
	Version       string
	LocalEnabled  bool
	GoogleEnabled bool
}

func New(d Deps) *Handlers {
	return &Handlers{
		q:             d.Querier,
		version:       d.Version,
		localEnabled:  d.LocalEnabled,
		googleEnabled: d.GoogleEnabled,
	}
}

// GetHealth reports whether this instance can serve. It runs a real query
// rather than a bare ping: a pool that is exhausted, or pointed at a database
// this role cannot read, passes a ping and fails here — which is the difference
// between liveness and readiness.
//
// A failure is 503 with status degraded, never an error envelope: a health
// check is consumed by a probe, not by the frontend, and it should say what is
// wrong in the same shape whether or not it is well.
func (h *Handlers) GetHealth(ctx context.Context, _ api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	if _, err := h.q.Ping(ctx); err != nil {
		slog.Error("health: database unreachable", "err", err)
		return api.GetHealth503JSONResponse{
			Status:  api.Degraded,
			Version: &h.version,
		}, nil
	}
	return api.GetHealth200JSONResponse{
		Status:  api.Ok,
		Version: &h.version,
	}, nil
}

// GetAuthMethods is the pre-auth config surface: the client reads it before
// rendering a login screen and shows only what is live. Unauthenticated by
// design — it exposes which providers exist, which is not a secret, and the
// alternative is a login page that offers a button that cannot work.
func (h *Handlers) GetAuthMethods(_ context.Context, _ api.GetAuthMethodsRequestObject) (api.GetAuthMethodsResponseObject, error) {
	return api.GetAuthMethods200JSONResponse{
		Local:  h.localEnabled,
		Google: h.googleEnabled,
	}, nil
}
