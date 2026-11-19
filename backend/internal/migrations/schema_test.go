package migrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kerti/afloat/backend/internal/testutil"
)

// The schema's own rules, asserted against the migrated database (#12). A
// refusal must name the SQLSTATE and the constraint it expects: a typo in a
// statement errors too, and only an exact match tells the two apart.
//
// Applying, editing in place and rolling back stay in
// scripts/test-migrations.sh, which drives the goose CLI.

const (
	checkViolation  = "23514"
	uniqueViolation = "23505"
	fkViolation     = "23503"

	hh = "019212e0-0000-7000-8000-000000000001"
	u1 = "019212e0-0000-7000-8000-000000000002"
	u2 = "019212e0-0000-7000-8000-000000000003"
)

// seeded is a clean database holding one Household and one User with a
// credential: what most statements below need to exist first.
func seeded(t *testing.T) *testutil.TestDB {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	mustExec(t, tdb, `INSERT INTO households (id, display_name) VALUES ('`+hh+`', 'Test Household')`)
	mustExec(t, tdb, `INSERT INTO users (id, household_id, email, display_name) VALUES ('`+u1+`', '`+hh+`', 'a@example.com', 'A')`)
	mustExec(t, tdb, `INSERT INTO credentials (user_id, password_hash) VALUES ('`+u1+`', 'x')`)
	return tdb
}

func mustExec(t *testing.T, tdb *testutil.TestDB, sql string) {
	t.Helper()
	if _, err := tdb.Pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
}

func scalar(t *testing.T, tdb *testutil.TestDB, sql string) string {
	t.Helper()
	var s string
	if err := tdb.Pool.QueryRow(context.Background(), sql).Scan(&s); err != nil {
		t.Fatalf("%s\n-> %v", sql, err)
	}
	return s
}

func TestSchemaRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, sql, code, constraint string
	}{
		{"period_start_day 29", `INSERT INTO households (id, display_name, period_start_day) VALUES (gen_random_uuid(), 'x', 29)`,
			checkViolation, "households_period_start_day_range"},
		{"period_start_day 0", `INSERT INTO households (id, display_name, period_start_day) VALUES (gen_random_uuid(), 'x', 0)`,
			checkViolation, "households_period_start_day_range"},
		{"lower-case reporting_currency", `INSERT INTO households (id, display_name, reporting_currency) VALUES (gen_random_uuid(), 'x', 'idr')`,
			checkViolation, "households_reporting_currency_iso4217"},
		{"negative expected income", `INSERT INTO households (id, display_name, expected_monthly_income) VALUES (gen_random_uuid(), 'x', -1)`,
			checkViolation, "households_expected_monthly_income_non_negative"},
		{"blank household display_name", `INSERT INTO households (id, display_name) VALUES (gen_random_uuid(), '   ')`,
			checkViolation, "households_display_name_not_blank"},
		{"unknown allowance_mode", `INSERT INTO households (id, display_name, allowance_mode) VALUES (gen_random_uuid(), 'x', 'strict')`,
			checkViolation, "households_allowance_mode_known"},
		{"unknown locale", `INSERT INTO users (id, household_id, email, display_name, locale) VALUES (gen_random_uuid(), '` + hh + `', 'q@example.com', 'x', 'fr-FR')`,
			checkViolation, "users_locale_known"},
		{"blank user display_name", `INSERT INTO users (id, household_id, email, display_name) VALUES (gen_random_uuid(), '` + hh + `', 'q@example.com', '  ')`,
			checkViolation, "users_display_name_not_blank"},
		{"live email taken, other case", `INSERT INTO users (id, household_id, email, display_name) VALUES (gen_random_uuid(), '` + hh + `', 'A@EXAMPLE.COM', 'dup')`,
			uniqueViolation, "users_email_idx"},
		{"household must exist", `INSERT INTO users (id, household_id, email, display_name) VALUES (gen_random_uuid(), gen_random_uuid(), 'z@example.com', 'x')`,
			fkViolation, "users_household_id_fkey"},
		{"session for a user that doesn't exist", `INSERT INTO sessions (id, user_id, expires_at) VALUES (repeat('a', 64), gen_random_uuid(), now())`,
			fkViolation, "sessions_user_id_fkey"},
		{"invitation into a household that doesn't exist", `INSERT INTO invitations (id, household_id, invited_email, token_hash, created_by, expires_at)
			VALUES (gen_random_uuid(), gen_random_uuid(), 's@example.com', repeat('b', 64), '` + u1 + `', now())`,
			fkViolation, "invitations_household_id_fkey"},
		{"invitation from a user that doesn't exist", `INSERT INTO invitations (id, household_id, invited_email, token_hash, created_by, expires_at)
			VALUES (gen_random_uuid(), '` + hh + `', 's@example.com', repeat('b', 64), gen_random_uuid(), now())`,
			fkViolation, "invitations_created_by_fkey"},
		{"second credential for one user", `INSERT INTO credentials (user_id, password_hash) VALUES ('` + u1 + `', 'y')`,
			uniqueViolation, "credentials_pkey"},
		{"negative failure_count", `INSERT INTO login_attempts (key, failure_count) VALUES ('email:x@example.com', -1)`,
			checkViolation, "login_attempts_failure_count_non_negative"},
		// Users are soft-deleted, so a live one always holds its Household.
		{"household delete under live users", `DELETE FROM households WHERE id = '` + hh + `'`,
			fkViolation, "users_household_id_fkey"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tdb := seeded(t)
			_, err := tdb.Pool.Exec(context.Background(), tc.sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("accepted (err %v), want %s on %s", err, tc.code, tc.constraint)
			}
			if pgErr.Code != tc.code || pgErr.ConstraintName != tc.constraint {
				t.Fatalf("refused with %s on %q (%s), want %s on %q",
					pgErr.Code, pgErr.ConstraintName, pgErr.Message, tc.code, tc.constraint)
			}
		})
	}
}

func TestDuplicateInvitationTokenIsRefused(t *testing.T) {
	tdb := seeded(t)
	insert := `INSERT INTO invitations (id, household_id, invited_email, token_hash, created_by, expires_at)
		VALUES (gen_random_uuid(), '` + hh + `', 's@example.com', repeat('b', 64), '` + u1 + `', now() + interval '72 hours')`
	mustExec(t, tdb, insert)
	_, err := tdb.Pool.Exec(context.Background(), insert)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation || pgErr.ConstraintName != "invitations_token_hash_key" {
		t.Fatalf("got %v, want %s on invitations_token_hash_key", err, uniqueViolation)
	}
}

func TestSchemaAccepts(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"period_start_day 28", `INSERT INTO households (id, display_name, period_start_day) VALUES (gen_random_uuid(), 'x', 28)`},
		{"zero expected income", `INSERT INTO households (id, display_name, expected_monthly_income) VALUES (gen_random_uuid(), 'x', 0)`},
		{"session", `INSERT INTO sessions (id, user_id, expires_at) VALUES (repeat('a', 64), '` + u1 + `', now() + interval '30 days')`},
		{"login_attempts row", `INSERT INTO login_attempts (key, failure_count, backoff_until) VALUES ('ip:203.0.113.7', 3, now() + interval '4 seconds')`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mustExec(t, seeded(t), tc.sql)
		})
	}
}

// PRD §5's defaults are the schema's, not the backend's.
func TestSchemaDefaults(t *testing.T) {
	tdb := seeded(t)
	if got := scalar(t, tdb, `SELECT reporting_currency || ' / ' || period_start_day || ' / ' || day_starts_at || ' / ' || allowance_mode
		FROM households WHERE id = '`+hh+`'`); got != "IDR / 1 / 04:00:00 / adaptive" {
		t.Errorf("household defaults = %q", got)
	}
	if got := scalar(t, tdb, `SELECT locale || ' / ' || time_zone FROM users WHERE id = '`+u1+`'`); got != "en-GB / Asia/Jakarta" {
		t.Errorf("user defaults = %q", got)
	}
}

// The email index ignores soft-deleted Users: an address is free again once
// its holder is deleted, however many deleted Users shared it.
func TestSoftDeletedUsersReleaseTheirEmail(t *testing.T) {
	tdb := seeded(t)
	mustExec(t, tdb, `INSERT INTO users (id, household_id, email, display_name) VALUES ('`+u2+`', '`+hh+`', 'b@example.com', 'B')`)
	mustExec(t, tdb, `UPDATE users SET deleted_at = now() WHERE id IN ('`+u1+`', '`+u2+`')`)
	mustExec(t, tdb, `UPDATE users SET email = 'same@example.com' WHERE id IN ('`+u1+`', '`+u2+`')`)
	mustExec(t, tdb, `INSERT INTO users (id, household_id, email, display_name) VALUES (gen_random_uuid(), '`+hh+`', 'same@example.com', 'live')`)
	mustExec(t, tdb, `UPDATE users SET deleted_at = NULL, email = 'a@example.com' WHERE id = '`+u1+`'`)
}

// A hard DELETE of a User is always deliberate (test teardown, or erasure if
// Q-14 is ever answered); instance-local auth state must never block one.
func TestHardDeletingAUserCascadesItsAuthState(t *testing.T) {
	tdb := seeded(t)
	mustExec(t, tdb, `INSERT INTO sessions (id, user_id, expires_at) VALUES (repeat('a', 64), '`+u1+`', now() + interval '30 days')`)
	mustExec(t, tdb, `INSERT INTO invitations (id, household_id, invited_email, token_hash, created_by, expires_at)
		VALUES (gen_random_uuid(), '`+hh+`', 's@example.com', repeat('b', 64), '`+u1+`', now() + interval '72 hours')`)

	mustExec(t, tdb, `DELETE FROM users WHERE id = '`+u1+`'`)

	if got := scalar(t, tdb, `SELECT (SELECT count(*) FROM credentials) || '/' || (SELECT count(*) FROM sessions) || '/' || (SELECT count(*) FROM invitations)`); got != "0/0/0" {
		t.Errorf("credentials/sessions/invitations left = %s, want 0/0/0", got)
	}
}
