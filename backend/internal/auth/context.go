package auth

import (
	"context"

	"github.com/kerti/afloat/backend/internal/db"
)

// userContextKey is unexported and of its own type, so nothing outside this
// package can collide with it or forge a User onto a request.
type userContextKey struct{}

// WithUser attaches the authenticated User to a request context.
func WithUser(ctx context.Context, user db.User) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

// UserFromContext returns the authenticated User, if the request had a valid
// session. Every Household-scoped query takes its household_id from here — and
// still filters in SQL, never in middleware alone (BOOTSTRAP.md §4).
func UserFromContext(ctx context.Context) (db.User, bool) {
	user, ok := ctx.Value(userContextKey{}).(db.User)
	return user, ok
}

type sessionUnresolvedKey struct{}

// withSessionUnresolved marks a request whose session cookie could not be
// resolved because the lookup itself failed, as distinct from a cookie that was
// absent or that the database answered was dead.
func withSessionUnresolved(ctx context.Context) context.Context {
	return context.WithValue(ctx, sessionUnresolvedKey{}, true)
}

// SessionUnresolved reports whether SessionMiddleware presented a session
// cookie it could not judge: the session or User lookup failed with anything
// but pgx.ErrNoRows. It is false for no cookie and for a dead, expired or
// orphaned session, which are answers about the session; this is the absence
// of one, so a route that needs a session owes the caller a 500, not a 401, and
// the cookie is left alone either way (#27).
func SessionUnresolved(ctx context.Context) bool {
	v, _ := ctx.Value(sessionUnresolvedKey{}).(bool)
	return v
}
