package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kerti/afloat/backend/internal/db"
)

// Beginner is the slice of the pool this package needs to run a transaction.
// Narrow on purpose: it keeps the handlers testable without a pool, and makes
// the one place that needs atomicity obvious.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// errUserMissing is a write that matched no live User in the caller's
// Household: the session outlived the row. Not an outage, so it is logged apart.
var errUserMissing = errors.New("user missing")

// updateUserAndReadHousehold runs write, a single statement that returns the
// User row it changed, and the read of that User's Household in one
// transaction. It is how PATCH /me and change-email keep their rule: a 500
// changes nothing. The answer needs both rows, and a read that failed after a
// committed write would leave the caller told "failed" about a change that had
// landed. A failure at begin, write, read or commit rolls back, so the row is
// as it was.
//
// write must not be given a pool: it runs on the transaction's Queries. A write
// that matches no row is errUserMissing; any other failure is wrapped with the
// step it came from, and keeps its cause (a unique violation is still found by
// isEmailTaken).
func (h *Handlers) updateUserAndReadHousehold(ctx context.Context, write func(q *db.Queries) (db.User, error)) (db.User, db.Household, error) {
	tx, err := h.tx.Begin(ctx)
	if err != nil {
		return db.User{}, db.Household{}, fmt.Errorf("begin: %w", err)
	}
	// A no-op after Commit. Every other return rolls back, so a failure leaves
	// the User row exactly as it was. Detached from ctx so a cancelled request
	// still releases the connection.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := db.New(tx)

	row, err := write(q)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, db.Household{}, errUserMissing
	}
	if err != nil {
		return db.User{}, db.Household{}, fmt.Errorf("update user: %w", err)
	}
	household, err := q.GetHouseholdByID(ctx, row.HouseholdID)
	if err != nil {
		return db.User{}, db.Household{}, fmt.Errorf("look up household: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.User{}, db.Household{}, fmt.Errorf("commit: %w", err)
	}
	return row, household, nil
}
