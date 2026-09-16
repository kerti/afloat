// Package postgres owns the connection pool and the migration run. It is
// hand-written; internal/db beside it is sqlc's output and is not
// (docs/adr/go/0002).
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/kerti/afloat/backend/internal/migrations"
)

// Connect opens the pool and verifies it can actually reach the database, so a
// bad DATABASE_URL fails at boot rather than at the first request.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// Migrate applies the embedded goose migrations. Idempotent, and run on every
// boot (BOOTSTRAP.md §3).
//
// goose wants a database/sql handle, which pgx provides through its stdlib
// shim. The handle is opened and closed here rather than kept: the application
// itself uses the pgx pool.
func Migrate(ctx context.Context, databaseURL string) error {
	conn, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Warn("close migration connection", "err", err)
		}
	}()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{})
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, conn, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// gooseLogger routes goose's output through slog instead of the standard
// logger, so boot output has one format (docs/adr/go/0003).
type gooseLogger struct{}

func (gooseLogger) Printf(format string, v ...any) {
	slog.Info(fmt.Sprintf(format, v...), "source", "goose")
}

func (gooseLogger) Fatalf(format string, v ...any) {
	slog.Error(fmt.Sprintf(format, v...), "source", "goose")
}

// Ensure the pgx stdlib driver is linked; goose reaches it by name.
var _ = stdlib.GetDefaultDriver
