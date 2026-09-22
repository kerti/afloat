// Package httpserver owns the chi router, the middleware stack and the mount
// points (docs/adr/go/0001). Handlers themselves live with their domain.
package httpserver

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/system"
)

// Server implements api.StrictServerInterface. The generator emits one
// interface for the whole contract, so one type has to satisfy it.
//
// Delegation is explicit rather than by embedding: two packages would otherwise
// both contribute a type named Handlers, and promotion would silently decide
// which one won.
type Server struct {
	system *system.Handlers
	auth   *auth.Handlers
}

type Deps struct {
	System *system.Handlers
	Auth   *auth.Handlers
}

func (s *Server) GetHealth(ctx context.Context, r api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	return s.system.GetHealth(ctx, r)
}

func (s *Server) GetAuthMethods(ctx context.Context, r api.GetAuthMethodsRequestObject) (api.GetAuthMethodsResponseObject, error) {
	return s.system.GetAuthMethods(ctx, r)
}

func (s *Server) LocalLogin(ctx context.Context, r api.LocalLoginRequestObject) (api.LocalLoginResponseObject, error) {
	return s.auth.LocalLogin(ctx, r)
}

func (s *Server) Logout(ctx context.Context, r api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	return s.auth.Logout(ctx, r)
}

func (s *Server) GetMe(ctx context.Context, r api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	return s.auth.GetMe(ctx, r)
}

// New builds the router: the API under /api, with the middleware every request
// passes through.
//
// Middleware is written as func(http.Handler) http.Handler and stays free of
// chi types, so the stdlib exit ADR-0001 describes remains cheap.
func New(d Deps) http.Handler {
	srv := &Server{system: d.System, auth: d.Auth}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// middleware.RealIP is deliberately NOT mounted. It rewrites RemoteAddr from
	// X-Forwarded-For, which nothing strips in a self-hosted deployment with no
	// proxy in front — so an attacker would choose their own rate-limit key and
	// the per-IP login backoff would stop existing.
	//
	// recoverer (middleware.go) turns a handler panic into the shared envelope
	// instead of killing the process and dropping every in-flight request with
	// it. Not chi's middleware.Recoverer: that writes a bare 500 with no body
	// and no Content-Type (#29).
	r.Use(recoverer)
	r.Use(requestLogger)
	// A body limit on every JSON route. Balances caps only its file uploads,
	// which leaves an unbounded decode everywhere else.
	r.Use(maxBodyBytes(1 << 20))
	r.Use(middleware.Timeout(30 * time.Second))
	// Second CSRF layer, behind SameSite=Lax.
	r.Use(crossSiteGuard)
	// Carries the client IP, User-Agent and presented token into the handler
	// context: strict handlers receive only a context, not the request.
	r.Use(auth.RequestContextMiddleware)
	// Resolves a session into a User but never rejects. The generated wrapper
	// mounts every route the same way, so authentication is enforced by each
	// handler asking for its User rather than by route-level grouping — which
	// means a new authenticated endpoint cannot be added without deciding.
	r.Use(d.Auth.SessionMiddleware)

	// The contract's servers entry is /api, so the generated routes mount under
	// it rather than carrying the prefix in every path.
	//
	// Both option structs are supplied rather than taking HandlerFromMux and
	// NewStrictHandler: their defaults answer with http.Error, i.e. text/plain
	// carrying an English message, on every path that fails before a handler
	// runs. See errors.go.
	strict := api.NewStrictHandlerWithOptions(srv, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestErrorHandler,
		ResponseErrorHandlerFunc: responseErrorHandler,
	})
	r.Mount("/api", api.HandlerWithOptions(strict, api.ChiServerOptions{
		BaseRouter:       chi.NewRouter(),
		ErrorHandlerFunc: paramErrorHandler,
		// Per-operation, not r.Use above: this only ever runs once chi has
		// matched a request to a route the contract declares (openapi_validate.go).
		Middlewares: []api.MiddlewareFunc{openapiRequestValidator()},
	}))

	return r
}
