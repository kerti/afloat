// Package httpserver owns the chi router, the middleware stack and the mount
// points (docs/adr/go/0001). Handlers themselves live with their domain.
package httpserver

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/system"
)

// Server implements api.StrictServerInterface by composing the domain
// handlers. The generator emits one interface for the whole contract, so one
// type has to satisfy it; the methods delegate rather than doing work here.
type Server struct {
	*system.Handlers
	pendingAuth
}

type Deps struct {
	System *system.Handlers
}

// New builds the router: the API under /api, with the middleware every request
// passes through.
//
// Middleware is written as func(http.Handler) http.Handler and stays free of
// chi types, so the stdlib exit ADR-0001 describes remains cheap.
func New(d Deps) http.Handler {
	srv := &Server{Handlers: d.System}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	// Recoverer turns a handler panic into a 500 instead of killing the process
	// and dropping every in-flight request with it.
	r.Use(middleware.Recoverer)
	r.Use(requestLogger)
	// A body limit on every JSON route. Balances caps only its file uploads,
	// which leaves an unbounded decode on every other endpoint.
	r.Use(maxBodyBytes(1 << 20))
	r.Use(middleware.Timeout(30 * time.Second))

	// The contract's servers entry is /api, so the generated routes mount under
	// it rather than carrying the prefix in every path.
	r.Mount("/api", api.HandlerFromMux(api.NewStrictHandler(srv, nil), chi.NewRouter()))

	return r
}
