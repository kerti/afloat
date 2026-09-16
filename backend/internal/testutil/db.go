// Package testutil provides a real Postgres for integration tests
// (docs/adr/go/0004). Fakes cannot verify tenancy or session behaviour, which
// is most of what is worth testing here.
package testutil

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/postgres"
)

var (
	// One container per test binary: startup and the migration run are paid
	// once per package rather than once per test function.
	once      sync.Once
	sharedDB  *pgxpool.Pool
	sharedErr error
)

// TestDB is a migrated, empty database. Every call truncates, so each test
// opens on a clean schema without a wrapping transaction — the code under test
// is itself transactional and takes more than one pooled connection, either of
// which a BEGIN/ROLLBACK around it would mask or deadlock.
type TestDB struct {
	Pool    *pgxpool.Pool
	Queries *db.Queries
}

// NewTestDB starts the shared container on first call and returns a clean
// database. Skips the test if docker is unavailable, so `go test ./...` on a
// machine without it reports honestly rather than failing.
func NewTestDB(t *testing.T) *TestDB {
	t.Helper()

	once.Do(func() { sharedDB, sharedErr = startShared() })
	if sharedErr != nil {
		t.Skipf("testutil: no test database (%v)", sharedErr)
	}

	truncateAll(t, sharedDB)
	return &TestDB{Pool: sharedDB, Queries: db.New(sharedDB)}
}

func startShared() (*pgxpool.Pool, error) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("afloat_test"),
		tcpostgres.WithUsername("afloat"),
		tcpostgres.WithPassword("afloat"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("connection string: %w", err)
	}

	// The same runner and the same embedded FS the binary ships, so tests run a
	// bit-identical schema rather than a hand-maintained copy that drifts.
	if err := postgres.Migrate(ctx, dsn); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	pool, err := postgres.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

// truncateAll empties every application table. The list comes from the catalog,
// so a new migration's tables are swept with no change here — and goose's own
// ledger is left alone, or the next test would re-migrate.
func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = 'public' AND tablename <> 'goose_db_version'`)
	if err != nil {
		t.Fatalf("testutil: list tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("testutil: scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("testutil: iterate tables: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("testutil: no application tables found; did migrations run?")
	}

	stmt := "TRUNCATE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := pool.Exec(ctx, stmt); err != nil {
		t.Fatalf("testutil: truncate: %v", err)
	}
}
