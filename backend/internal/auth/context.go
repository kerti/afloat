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
