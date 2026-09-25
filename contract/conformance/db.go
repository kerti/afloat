package conformance

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// StateTolerance is how far apart two backends' timestamps may sit once each
// is measured from its own snapshot's now(). The two runs are seconds apart,
// so absolute instants never match; what must match is how old each row is
// and how far ahead each expiry sits.
const StateTolerance = 2 * time.Second

// migrationHistory are the migration runners' own bookkeeping tables. They
// differ between the backends by design (goose for Go, Flyway for Kotlin -
// BOOTSTRAP.md §6), and neither is Afloat data.
var migrationHistory = []string{"goose_db_version", "flyway_schema_history"}

// DB is one backend's database, as the harness sees it: something to reset
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

// Snapshot is every Afloat table's rows, in a form two backends can be
// compared on: timestamps as seconds from the snapshot's own now(), every
// other column as its text form.
type Snapshot map[string][]SnapshotRow

type SnapshotRow map[string]SnapshotCell

type SnapshotCell struct {
	Text   *string
	Offset *float64 // seconds from now(); set for timestamp columns only
}

func (c SnapshotCell) String() string {
	switch {
	case c.Offset != nil:
		return fmt.Sprintf("now%+.1fs", *c.Offset)
	case c.Text != nil:
		return fmt.Sprintf("%q", *c.Text)
	}
	return "NULL"
}

// Snapshot reads every Afloat table, skipping the columns in masked - keyed
// "table.column" - which are permitted to differ.
func (d *DB) Snapshot(ctx context.Context, masked func(table, column string) bool) (Snapshot, error) {
	tables, err := d.tables(ctx)
	if err != nil {
		return nil, err
	}
	snap := Snapshot{}
	for _, t := range tables {
		cols, err := d.conn.Query(ctx, `
			SELECT column_name, data_type FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1
			ORDER BY ordinal_position`, t)
		if err != nil {
			return nil, err
		}
		type col struct{ name, typ string }
		cs, err := pgx.CollectRows(cols, func(r pgx.CollectableRow) (col, error) {
			var c col
			return c, r.Scan(&c.name, &c.typ)
		})
		if err != nil {
			return nil, err
		}
		var exprs []string
		var kept []col
		for _, c := range cs {
			if masked(t, c.name) {
				continue
			}
			id := pgx.Identifier{c.name}.Sanitize()
			if strings.HasPrefix(c.typ, "timestamp") {
				exprs = append(exprs, fmt.Sprintf("extract(epoch FROM (%s - now()))::float8", id))
			} else {
				exprs = append(exprs, id+"::text")
			}
			kept = append(kept, c)
		}
		if len(kept) == 0 {
			continue
		}
		rows, err := d.conn.Query(ctx, "SELECT "+strings.Join(exprs, ", ")+" FROM "+pgx.Identifier{t}.Sanitize())
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", t, err)
		}
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				rows.Close()
				return nil, err
			}
			row := SnapshotRow{}
			for i, v := range vals {
				var cell SnapshotCell
				switch v := v.(type) {
				case string:
					cell.Text = &v
				case float64:
					cell.Offset = &v
				}
				row[kept[i].name] = cell
			}
			snap[t] = append(snap[t], row)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		sort.Slice(snap[t], func(i, j int) bool { return snap[t][i].key() < snap[t][j].key() })
	}
	return snap, nil
}

// key orders rows by their text columns only, so two backends' rows pair up
// however far apart in time they were written.
func (r SnapshotRow) key() string {
	names := make([]string, 0, len(r))
	for n := range r {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		if c := r[n]; c.Offset == nil {
			b.WriteString(c.String())
			b.WriteByte(0)
		}
	}
	return b.String()
}

func (r SnapshotRow) String() string {
	names := make([]string, 0, len(r))
	for n := range r {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = n + "=" + r[n].String()
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// SnapshotParity is failure mode 2 for persisted state: the rows both
// backends left behind, compared table by table and cell by cell.
func SnapshotParity(a, b Snapshot) []string {
	var bad []string
	tables := map[string]bool{}
	for t := range a {
		tables[t] = true
	}
	for t := range b {
		tables[t] = true
	}
	sorted := make([]string, 0, len(tables))
	for t := range tables {
		sorted = append(sorted, t)
	}
	sort.Strings(sorted)
	for _, t := range sorted {
		ar, br := a[t], b[t]
		if len(ar) != len(br) {
			bad = append(bad, fmt.Sprintf("table %s: go has %d row(s), kotlin %d\n  go:     %v\n  kotlin: %v", t, len(ar), len(br), ar, br))
			continue
		}
		for i := range ar {
			if !ar[i].equal(br[i]) {
				bad = append(bad, fmt.Sprintf("table %s row %d differs\n  go:     %v\n  kotlin: %v", t, i, ar[i], br[i]))
			}
		}
	}
	return bad
}

func (r SnapshotRow) equal(o SnapshotRow) bool {
	if len(r) != len(o) {
		return false
	}
	for n, c := range r {
		d, ok := o[n]
		if !ok {
			return false
		}
		switch {
		case c.Offset != nil && d.Offset != nil:
			diff := time.Duration((*c.Offset - *d.Offset) * float64(time.Second))
			if diff < -StateTolerance || diff > StateTolerance {
				return false
			}
		case c.Text != nil && d.Text != nil:
			if *c.Text != *d.Text {
				return false
			}
		case c.Offset != nil || d.Offset != nil || c.Text != nil || d.Text != nil:
			return false // one side NULL
		}
	}
	return true
}
