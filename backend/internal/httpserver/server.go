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
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/category"
	"github.com/kerti/afloat/backend/internal/expense"
	"github.com/kerti/afloat/backend/internal/household"
	"github.com/kerti/afloat/backend/internal/pocket"
	"github.com/kerti/afloat/backend/internal/system"
)

// Server implements api.StrictServerInterface. The generator emits one
// interface for the whole contract, so one type has to satisfy it.
//
// Delegation is explicit rather than by embedding: two packages would otherwise
// both contribute a type named Handlers, and promotion would silently decide
// which one won.
type Server struct {
	system    *system.Handlers
	auth      *auth.Handlers
	household *household.Handlers
	pocket    *pocket.Handlers
	budget    *budget.Handlers
	category  *category.Handlers
	expense   *expense.Handlers
}

type Deps struct {
	System    *system.Handlers
	Auth      *auth.Handlers
	Household *household.Handlers
	Pocket    *pocket.Handlers
	Budget    *budget.Handlers
	Category  *category.Handlers
	Expense   *expense.Handlers

	// Web serves the built frontend (web.Handler, BOOTSTRAP.md §8) on every
	// GET and HEAD outside the API. Nil in a binary built without one: the
	// server is then the API alone, as in tests and the conformance suite.
	Web http.Handler

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

func (s *Server) LocalRegister(ctx context.Context, r api.LocalRegisterRequestObject) (api.LocalRegisterResponseObject, error) {
	return s.auth.LocalRegister(ctx, r)
}

func (s *Server) GetOnboardingOptions(ctx context.Context, r api.GetOnboardingOptionsRequestObject) (api.GetOnboardingOptionsResponseObject, error) {
	return s.auth.GetOnboardingOptions(ctx, r)
}

func (s *Server) CommitOnboardingChoice(ctx context.Context, r api.CommitOnboardingChoiceRequestObject) (api.CommitOnboardingChoiceResponseObject, error) {
	return s.auth.CommitOnboardingChoice(ctx, r)
}

func (s *Server) Logout(ctx context.Context, r api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	return s.auth.Logout(ctx, r)
}

func (s *Server) GetMe(ctx context.Context, r api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	return s.auth.GetMe(ctx, r)
}

func (s *Server) UpdateMe(ctx context.Context, r api.UpdateMeRequestObject) (api.UpdateMeResponseObject, error) {
	return s.auth.UpdateMe(ctx, r)
}

func (s *Server) ChangeLocalPassword(ctx context.Context, r api.ChangeLocalPasswordRequestObject) (api.ChangeLocalPasswordResponseObject, error) {
	return s.auth.ChangeLocalPassword(ctx, r)
}

func (s *Server) ChangeLocalEmail(ctx context.Context, r api.ChangeLocalEmailRequestObject) (api.ChangeLocalEmailResponseObject, error) {
	return s.auth.ChangeLocalEmail(ctx, r)
}

func (s *Server) UpdateHousehold(ctx context.Context, r api.UpdateHouseholdRequestObject) (api.UpdateHouseholdResponseObject, error) {
	return s.household.UpdateHousehold(ctx, r)
}

func (s *Server) ListPockets(ctx context.Context, r api.ListPocketsRequestObject) (api.ListPocketsResponseObject, error) {
	return s.pocket.ListPockets(ctx, r)
}

func (s *Server) CreatePocket(ctx context.Context, r api.CreatePocketRequestObject) (api.CreatePocketResponseObject, error) {
	return s.pocket.CreatePocket(ctx, r)
}

func (s *Server) UpdatePocket(ctx context.Context, r api.UpdatePocketRequestObject) (api.UpdatePocketResponseObject, error) {
	return s.pocket.UpdatePocket(ctx, r)
}

func (s *Server) DeletePocket(ctx context.Context, r api.DeletePocketRequestObject) (api.DeletePocketResponseObject, error) {
	return s.pocket.DeletePocket(ctx, r)
}

func (s *Server) RestorePocket(ctx context.Context, r api.RestorePocketRequestObject) (api.RestorePocketResponseObject, error) {
	return s.pocket.RestorePocket(ctx, r)
}

func (s *Server) ListCategories(ctx context.Context, r api.ListCategoriesRequestObject) (api.ListCategoriesResponseObject, error) {
	return s.category.ListCategories(ctx, r)
}

func (s *Server) CreateCategory(ctx context.Context, r api.CreateCategoryRequestObject) (api.CreateCategoryResponseObject, error) {
	return s.category.CreateCategory(ctx, r)
}

func (s *Server) UpdateCategory(ctx context.Context, r api.UpdateCategoryRequestObject) (api.UpdateCategoryResponseObject, error) {
	return s.category.UpdateCategory(ctx, r)
}

func (s *Server) DeleteCategory(ctx context.Context, r api.DeleteCategoryRequestObject) (api.DeleteCategoryResponseObject, error) {
	return s.category.DeleteCategory(ctx, r)
}

func (s *Server) RestoreCategory(ctx context.Context, r api.RestoreCategoryRequestObject) (api.RestoreCategoryResponseObject, error) {
	return s.category.RestoreCategory(ctx, r)
}

func (s *Server) GetBudget(ctx context.Context, r api.GetBudgetRequestObject) (api.GetBudgetResponseObject, error) {
	return s.budget.GetBudget(ctx, r)
}

func (s *Server) UpdateBudget(ctx context.Context, r api.UpdateBudgetRequestObject) (api.UpdateBudgetResponseObject, error) {
	return s.budget.UpdateBudget(ctx, r)
}

func (s *Server) ListExpenses(ctx context.Context, r api.ListExpensesRequestObject) (api.ListExpensesResponseObject, error) {
	return s.expense.ListExpenses(ctx, r)
}

func (s *Server) CreateExpense(ctx context.Context, r api.CreateExpenseRequestObject) (api.CreateExpenseResponseObject, error) {
	return s.expense.CreateExpense(ctx, r)
}

func (s *Server) GetExpense(ctx context.Context, r api.GetExpenseRequestObject) (api.GetExpenseResponseObject, error) {
	return s.expense.GetExpense(ctx, r)
}

func (s *Server) UpdateExpense(ctx context.Context, r api.UpdateExpenseRequestObject) (api.UpdateExpenseResponseObject, error) {
	return s.expense.UpdateExpense(ctx, r)
}

func (s *Server) DeleteExpense(ctx context.Context, r api.DeleteExpenseRequestObject) (api.DeleteExpenseResponseObject, error) {
	return s.expense.DeleteExpense(ctx, r)
}

func (s *Server) RestoreExpense(ctx context.Context, r api.RestoreExpenseRequestObject) (api.RestoreExpenseResponseObject, error) {
	return s.expense.RestoreExpense(ctx, r)
}

// New builds the router: the API under the contract's base path (basePath,
// /api today), with the middleware every request passes through.
//
// Middleware is written as func(http.Handler) http.Handler and stays free of
// chi types, so the stdlib exit ADR-0001 describes remains cheap.
func New(d Deps) http.Handler {
	srv := &Server{system: d.System, auth: d.Auth, household: d.Household, pocket: d.Pocket, budget: d.Budget, category: d.Category, expense: d.Expense}

	r := chi.NewRouter()
	// First, so every response this router produces carries the id and the
	// fixed header set (issue #26), including ones later middleware rejects,
	// and so requestLogger can read the id from context.
	r.Use(requestID)
	r.Use(securityHeaders)
	// Before anything routes: a percent-encoded path is its decoded route
	// (#56).
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
	// and so before chi's own method-not-allowed handling and the API group's
	// disabled-login gate - see optionsRefused (#66).
	r.Use(optionsRefused)

	// Everything below is the API's alone. A static file needs no body cap,
	// CSRF guard or session lookup - and a session lookup per asset would be a
	// database round trip per file on every cold load.
	r.Group(func(r chi.Router) {
		mountAPI(r, srv, d)
	})

	if d.Web != nil {
		r.Get("/*", d.Web.ServeHTTP)
		r.Head("/*", d.Web.ServeHTTP)
	}

	return r
}

func mountAPI(r chi.Router, srv *Server, d Deps) {
	// oapi-codegen's generated HandlerWithOptions (api.gen.go) mounts every
	// operation unconditionally — one r.Post/r.Get call per route, inlined
	// in a function we don't own — with no per-operation option to skip
	// mounting and no per-route middleware hook (ChiServerOptions.Middlewares
	// applies to every operation alike). Hand-editing that file is out
	// (BOOTSTRAP.md §6; CI regenerates and diffs it away). So the local
	// provider's four routes (login, register, change-password and
	// change-email) are gated ahead of the
	// generated mux instead: any request whose path matches is answered before
	// CSRF or session middleware ever run, so a disabled provider gives chi's
	// ordinary 404 with no session, handshake or backoff row written.
	// Being in the API group, it runs once chi has routed the request to the
	// /api mount: a method chi does not know at all (BREW) never gets that
	// far and answers chi's 405 - exactly what an unregistered /api path
	// answers to it, so the gate stays indistinguishable on every method.
	// Enabled comes from d.System.LocalEnabled() — the same value
	// GetAuthMethods reports — so the gate cannot drift from what
	// /auth/methods tells the client.
	r.Use(disabledLocalAuth404(d.System.LocalEnabled()))
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
	// Resolves a session into a User but never rejects: a public route must
	// still be reachable with a stale cookie. Enforcement is sessionRequired,
	// per operation, below.
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
	// point). RFC 9110 §9.1 requires HEAD of a general-purpose server (#66).
	apiRouter := chi.NewRouter()
	apiRouter.Use(headAsGet)
	required := contractSessionRequirements()
	r.Mount(basePath, api.HandlerWithOptions(strict, api.ChiServerOptions{
		BaseRouter:       apiRouter,
		ErrorHandlerFunc: paramErrorHandlerFor(required),
		// Per-operation, not r.Use above: these only ever run once chi has
		// matched a request to a route the contract declares, so an unknown
		// path or method keeps chi's own 404/405. Order matters:
		// operationMiddlewaresFor says why.
		Middlewares: operationMiddlewaresFor(required),
	}))
}
