package auth

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Beginner is the slice of the pool this package needs to run a transaction.
// Narrow on purpose: it keeps the handlers testable without a pool, and makes
// the one place that needs atomicity obvious.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}
