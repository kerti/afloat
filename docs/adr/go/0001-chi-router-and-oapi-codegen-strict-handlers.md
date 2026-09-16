# chi as the HTTP router, with oapi-codegen strict handlers

`draft`

The Go backend routes with **`github.com/go-chi/chi/v5`**, and consumes
`contract/openapi.yaml` through **`oapi-codegen`'s chi server + strict-handler mode**. The two are one
decision: the generator emits router-shaped code, so picking a router picks a generator mode.

## Why a router at all

Go 1.22 gave `net/http.ServeMux` method-aware patterns and path wildcards, which covers Afloat's
routing needs outright — the contract has no route that stdlib patterns cannot express. The argument
for chi is not routing; it is that **chi's `Middleware` type is `func(http.Handler) http.Handler`**,
the stdlib signature, and the ecosystem of middleware Afloat will actually use (`RequestID`,
`RealIP`, `Recoverer`, plus our own session, CSRF and tenancy layers) composes as ordinary handlers
either way — but stdlib gives no `Group`/`With` for applying a subset of them to a subset of routes.

Afloat needs exactly that: `/health` and `/auth/methods` are unauthenticated, everything else sits
behind session resolution. With stdlib that is hand-rolled wrapping at each registration or a second
mux; with chi it is `r.Group`. Small, but it is the shape of every route we add from step 9 onward.

**This is not a strong decision.** Migrating to stdlib later is mechanical, because chi is used for
routing and grouping only — no chi-specific request context, no render helpers, no chi-typed
middleware. It is recorded so the next person knows it was considered rather than defaulted into.

## oapi-codegen: strict handlers, not the raw server interface

`oapi-codegen` can emit a plain `ServerInterface` (handlers take `http.ResponseWriter` and
`*http.Request`) or **strict handlers** (handlers take a typed request struct and return a typed
response union; the generated wrapper does binding, and writes the status and body).

Afloat takes strict handlers:

- **The contract is the point.** With the raw interface, a handler is free to write a 200 the spec
  never declared, and nothing catches it. With strict handlers the response types *are* the declared
  responses — an undeclared status is a compile error, not a conformance-test failure later.
- **It pushes the money rule into the type system.** `DECIMAL(20,4)` as a string on the wire
  (`BOOTSTRAP.md` §4) is generated as a `string` field; a handler cannot accidentally emit a JSON
  number, which is the exact failure the Kotlin side has to be configured out of.
- **Request binding stops being hand-written.** JSON decode, path and query parameters, and the
  `INVALID_JSON_BODY` path are the generator's problem, uniformly, rather than a block copied into
  every handler.

The cost is a layer of generated indirection and less control over the raw response — acceptable,
because the one place Afloat genuinely needs raw `http.ResponseWriter` access is setting the session
cookie, and strict handlers expose response headers for exactly that.

## Consequences

- `backend/internal/httpserver` owns the chi router, the middleware stack and the mount points.
  Handlers live with their domain (`internal/auth`, later `internal/expenses`), not in one package.
- Generated code lands in `backend/internal/api`, is committed, and is never hand-edited
  (`BOOTSTRAP.md` §6). `make check` regenerates and diffs, like the goose migrations.
- Middleware is written as `func(http.Handler) http.Handler` and must stay free of chi types, so the
  stdlib exit stays cheap.
- The Kotlin track's equivalent choice is Spring's own, and is not bound by this ADR. Only the
  contract binds both.
