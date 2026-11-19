package conformance

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

// migrationHistory is goose's own bookkeeping table, which is not Afloat data
// and must survive a reset.
var migrationHistory = []string{"goose_db_version"}

// DB is the backend's database, as the harness sees it: something to reset
// to the seed before a case, to move with a given step, and to read after.
type DB struct {
	conn *pgx.Conn
	// admin is a connection to the maintenance database, which is where a
	// database's own connections are refused and restored from: a connection
	// to the database itself would be refused along with the backend's.
	admin *pgx.Conn
	name  string
}

func OpenDB(ctx context.Context, url string) (*DB, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, err
	}
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		_ = conn.Close(ctx)
		return nil, err
	}
	name := cfg.Database
	cfg.Database = "postgres"
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("maintenance database: %w", err)
	}
	d := &DB{conn: conn, admin: admin, name: name}
	// A run killed mid-outage leaves the database refusing connections; the
	// next run starts by undoing it.
	return d, d.Restore(ctx)
}

func (d *DB) Close(ctx context.Context) {
	_ = d.conn.Close(ctx)
	_ = d.admin.Close(ctx)
}

// Outage makes the backend's database unreachable from the backend, the way
// a database that has gone away is: every connection it holds is terminated
// and every new one refused. The harness's own connection is spared, so a
// case can still read the rows once the database is back.
func (d *DB) Outage(ctx context.Context) error {
	db := pgx.Identifier{d.name}.Sanitize()
	if _, err := d.admin.Exec(ctx, "ALTER DATABASE "+db+" WITH ALLOW_CONNECTIONS false"); err != nil {
		return err
	}
	_, err := d.admin.Exec(ctx, `
		SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE datname = $1 AND pid <> $2`, d.name, d.conn.PgConn().PID())
	return err
}

// Restore ends an Outage. Safe to call when there is none.
func (d *DB) Restore(ctx context.Context) error {
	_, err := d.admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{d.name}.Sanitize()+" WITH ALLOW_CONNECTIONS true")
	return err
}

func (d *DB) tables(ctx context.Context) ([]string, error) {
	rows, err := d.conn.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		  AND NOT (table_name = ANY($1))
		ORDER BY table_name`, migrationHistory)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Reset empties every Afloat table and applies the seed, so each case starts
// from the same rows on both backends whatever ran before it. Tables are
// discovered rather than listed, so a migration that adds one cannot leave it
// holding the previous case's rows.
func (d *DB) Reset(ctx context.Context, seedFile string) error {
	tables, err := d.tables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	seed, err := os.ReadFile(seedFile) //nolint:gosec // fixed path from the caller
	if err != nil {
		return err
	}
	quoted := make([]string, len(tables))
	for i, t := range tables {
		quoted[i] = pgx.Identifier{t}.Sanitize()
	}
	if _, err := d.conn.Exec(ctx, "TRUNCATE "+strings.Join(quoted, ", ")+" CASCADE"); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	if _, err := d.conn.Exec(ctx, string(seed)); err != nil {
		return fmt.Errorf("seed %s: %w", seedFile, err)
	}
	return nil
}

// Exec runs a given step's SQL. No arguments, so pgx uses the simple
// protocol and a step may hold several statements.
func (d *DB) Exec(ctx context.Context, sql string) error {
	_, err := d.conn.Exec(ctx, sql)
	return err
}

// Query runs an expect.rows query and returns its rows as JSON-shaped maps.
// Only text, integers, booleans and NULL come back; anything else is refused
// with the column named, so a case casts it in SQL rather than leaning on how
// a driver happens to render a timestamp or a numeric.
func (d *DB) Query(ctx context.Context, sql string) ([]map[string]any, error) {
	rows, err := d.conn.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(vals))
		for i, v := range vals {
			name := fields[i].Name
			switch v := v.(type) {
			case nil, string, bool:
				row[name] = v
			case int16:
				row[name] = float64(v)
			case int32:
				row[name] = float64(v)
			case int64:
				row[name] = float64(v)
			default:
				return nil, fmt.Errorf("column %q is %T; cast it to text, an integer or boolean in the query", name, v)
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
