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

	// HandlerTimeout bounds how long a handler may run before middleware.Timeout
	// cuts it off. It is config.Config.WriteTimeout (HTTP_WRITE_TIMEOUT) — issue
	// #30 folded the two rather than inventing a second variable that would
	// need to agree with the first.
	HandlerTimeout time.Duration
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

// New builds the router: the API under the contract's base path (basePath,
// /api today), with the middleware every request passes through.
//
// Middleware is written as func(http.Handler) http.Handler and stays free of
// chi types, so the stdlib exit ADR-0001 describes remains cheap.
func New(d Deps) http.Handler {
	srv := &Server{system: d.System, auth: d.Auth}

	r := chi.NewRouter()
	// First, so every response this router produces carries the id and the
	// fixed header set (issue #26), including ones later middleware rejects,
	// and so requestLogger can read the id from context.
	r.Use(requestID)
	r.Use(securityHeaders)
	// Before anything routes: a percent-encoded path is its decoded route
	// (#56), as Spring has always read it.
	r.Use(routeOnDecodedPath)
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
	// Every OPTIONS request is answered before it reaches routing at all,
	// including the disabled-login gate below and chi's own method-not-allowed
	// handling — see optionsRefused (#66).
	r.Use(optionsRefused)
	// oapi-codegen's generated HandlerWithOptions (api.gen.go) mounts every
	// operation unconditionally — one r.Post/r.Get call per route, inlined
	// in a function we don't own — with no per-operation option to skip
	// mounting and no per-route middleware hook (ChiServerOptions.Middlewares
	// applies to every operation alike). Hand-editing that file is out
	// (BOOTSTRAP.md §6; CI regenerates and diffs it away). So this one route
	// is gated ahead of the generated mux instead: any request whose path
	// matches is answered before CSRF or session middleware ever run, so a
	// disabled login gives chi's ordinary 404 with no session row written.
	// Enabled comes from d.System.LocalEnabled() — the same value
	// GetAuthMethods reports — so the gate cannot drift from what
	// /auth/methods tells the client.
	r.Use(disabledLocalLogin404(d.System.LocalEnabled()))
	// A body limit on every JSON route. Balances caps only its file uploads,
	// which leaves an unbounded decode everywhere else.
	r.Use(maxBodyBytes(1 << 20))
	// The bare literal this used to read is gone (#30): HandlerTimeout is
	// config.Config.WriteTimeout, and main.go derives http.Server.WriteTimeout
	// from the same value plus a grace, so operators set both with one
	// variable and the cut-off handler's answer still reaches the client.
	r.Use(middleware.Timeout(d.HandlerTimeout))
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

	// The generated routes mount under the contract's servers entry (basePath)
	// rather than carrying the prefix in every path.
	//
	// Both option structs are supplied rather than taking HandlerFromMux and
	// NewStrictHandler: their defaults answer with http.Error, i.e. text/plain
	// carrying an English message, on every path that fails before a handler
	// runs. See errors.go.
	strict := api.NewStrictHandlerWithOptions(srv, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestErrorHandler,
		ResponseErrorHandlerFunc: responseErrorHandler,
	})
	// A base router of our own, not the bare chi.NewRouter() HandlerWithOptions
	// would otherwise default to, so headAsGet can sit in ITS middleware stack
	// and route HEAD against this router's own tree of the contract's actual
	// operations, not the outer r's (whose tree holds only the /api mount
	// point). RFC 9110 §9.1 requires HEAD of a general-purpose server; Spring
	// already answers it (#66).
	apiRouter := chi.NewRouter()
	apiRouter.Use(headAsGet)
	r.Mount(basePath, api.HandlerWithOptions(strict, api.ChiServerOptions{
		BaseRouter:       apiRouter,
		ErrorHandlerFunc: paramErrorHandler,
		// Per-operation, not r.Use above: this only ever runs once chi has
		// matched a request to a route the contract declares (openapi_validate.go).
		Middlewares: []api.MiddlewareFunc{openapiRequestValidator()},
	}))

	return r
}
