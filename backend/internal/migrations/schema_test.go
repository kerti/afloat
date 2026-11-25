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

	hh      = "019212e0-0000-7000-8000-000000000001"
	hhOther = "019212e0-0000-7000-8000-0000000000bb"
	u1      = "019212e0-0000-7000-8000-000000000002"
	u2      = "019212e0-0000-7000-8000-000000000003"
	p1      = "019212e0-0000-7000-8000-000000000011"
	c1      = "019212e0-0000-7000-8000-000000000021"
	c2      = "019212e0-0000-7000-8000-000000000022"
	e1      = "019212e0-0000-7000-8000-000000000031"
	e2      = "019212e0-0000-7000-8000-000000000032"
)

// categoryInsert is the INSERT of a live, active Category of the given bucket.
func categoryInsert(id, household, name, bucket string) string {
	return `INSERT INTO categories (id, household_id, category_display_name, bucket) VALUES ('` + id + `', '` + household + `', '` + name + `', '` + bucket + `')`
}

// allocationInsert is the INSERT of one allocation row, for Period 2026-11.
func allocationInsert(household, effectiveOn, category, amount string) string {
	return `INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation) VALUES ('` + household + `', '2026-11', '` + effectiveOn + `', '` + category + `', ` + amount + `)`
}

// pocketInsert is the INSERT of a live, active Pocket.
func pocketInsert(id, household, name string) string {
	return `INSERT INTO pockets (id, household_id, pocket_display_name) VALUES ('` + id + `', '` + household + `', '` + name + `')`
}

// expenseInsert is the INSERT of a live Expense on Pocket p1 for 2026-11-23,
// with the given amount and any extra columns, which come as two parallel
// strings, the names and the SQL values, each starting with a comma (or both
// empty).
func expenseInsert(id, household, amount, extraCols, extraVals string) string {
	return `INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id` + extraCols + `)
		VALUES ('` + id + `', '` + household + `', '2026-11-23', ` + amount + `, 'IDR', '` + p1 + `'` + extraVals + `)`
}

// budgetInsert is the INSERT of a Budget row, the marker a Monthly Budget hangs
// from.
func budgetInsert(yearMonth string) string {
	return `INSERT INTO budgets (household_id, year_month) VALUES ('` + hh + `', '` + yearMonth + `')`
}

// seeded is a clean database holding one Household and one User with a
// credential: what most statements below need to exist first.
func seeded(t *testing.T) *testutil.TestDB {
	t.Helper()
	tdb := testutil.NewTestDB(t)
	mustExec(t, tdb, `INSERT INTO households (id, household_display_name) VALUES ('`+hh+`', 'Test Household')`)
	mustExec(t, tdb, `INSERT INTO users (id, household_id, email, user_display_name) VALUES ('`+u1+`', '`+hh+`', 'a@example.com', 'A')`)
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
		{"period_start_day 29", `INSERT INTO households (id, household_display_name, period_start_day) VALUES (gen_random_uuid(), 'x', 29)`,
			checkViolation, "households_period_start_day_range"},
		{"period_start_day 0", `INSERT INTO households (id, household_display_name, period_start_day) VALUES (gen_random_uuid(), 'x', 0)`,
			checkViolation, "households_period_start_day_range"},
		{"lower-case reporting_currency", `INSERT INTO households (id, household_display_name, reporting_currency) VALUES (gen_random_uuid(), 'x', 'idr')`,
			checkViolation, "households_reporting_currency_iso4217"},
		{"negative expected income", `INSERT INTO households (id, household_display_name, expected_monthly_income) VALUES (gen_random_uuid(), 'x', -1)`,
			checkViolation, "households_expected_monthly_income_non_negative"},
		{"blank household_display_name", `INSERT INTO households (id, household_display_name) VALUES (gen_random_uuid(), '   ')`,
			checkViolation, "households_household_display_name_not_blank"},
		{"unknown allowance_mode", `INSERT INTO households (id, household_display_name, allowance_mode) VALUES (gen_random_uuid(), 'x', 'strict')`,
			checkViolation, "households_allowance_mode_known"},
		// The spelling this schema once had; CONTEXT and PRD §4 say `fixed`.
		{"retired allowance_mode flat", `INSERT INTO households (id, household_display_name, allowance_mode) VALUES (gen_random_uuid(), 'x', 'flat')`,
			checkViolation, "households_allowance_mode_known"},
		{"unknown locale", `INSERT INTO users (id, household_id, email, user_display_name, locale) VALUES (gen_random_uuid(), '` + hh + `', 'q@example.com', 'x', 'fr-FR')`,
			checkViolation, "users_locale_known"},
		{"unknown appearance", `INSERT INTO users (id, household_id, email, user_display_name, appearance) VALUES (gen_random_uuid(), '` + hh + `', 'q@example.com', 'x', 'auto')`,
			checkViolation, "users_appearance_known"},
		{"an appearance in the wrong case", `UPDATE users SET appearance = 'Dark' WHERE id = '` + u1 + `'`,
			checkViolation, "users_appearance_known"},
		{"blank user_display_name", `INSERT INTO users (id, household_id, email, user_display_name) VALUES (gen_random_uuid(), '` + hh + `', 'q@example.com', '  ')`,
			checkViolation, "users_user_display_name_not_blank"},
		{"live email taken, other case", `INSERT INTO users (id, household_id, email, user_display_name) VALUES (gen_random_uuid(), '` + hh + `', 'A@EXAMPLE.COM', 'dup')`,
			uniqueViolation, "users_email_idx"},
		{"household must exist", `INSERT INTO users (id, household_id, email, user_display_name) VALUES (gen_random_uuid(), gen_random_uuid(), 'z@example.com', 'x')`,
			fkViolation, "users_household_id_fkey"},
		{"session for a user that doesn't exist", `INSERT INTO sessions (id, user_id, expires_at) VALUES (repeat('a', 64), gen_random_uuid(), now())`,
			fkViolation, "sessions_user_id_fkey"},
		{"invitation into a household that doesn't exist", `INSERT INTO invitations (id, household_id, invited_email, token_hash, created_by, expires_at)
			VALUES (gen_random_uuid(), gen_random_uuid(), 's@example.com', repeat('b', 64), '` + u1 + `', now())`,
			fkViolation, "invitations_household_id_fkey"},
		{"invitation from a user that doesn't exist", `INSERT INTO invitations (id, household_id, invited_email, token_hash, created_by, expires_at)
			VALUES (gen_random_uuid(), '` + hh + `', 's@example.com', repeat('b', 64), gen_random_uuid(), now())`,
			fkViolation, "invitations_created_by_fkey"},
		{"handshake with an unknown locale", `INSERT INTO onboarding_handshakes (id, email, user_display_name, locale, expires_at) VALUES (repeat('c', 64), 'h@example.com', 'H', 'fr-FR', now())`,
			checkViolation, "onboarding_handshakes_locale_known"},
		{"second handshake under one token", `INSERT INTO onboarding_handshakes (id, email, user_display_name, locale, expires_at) VALUES (repeat('c', 64), 'h@example.com', 'H', 'en-GB', now()), (repeat('c', 64), 'i@example.com', 'I', 'en-GB', now())`,
			uniqueViolation, "onboarding_handshakes_pkey"},
		{"second credential for one user", `INSERT INTO credentials (user_id, password_hash) VALUES ('` + u1 + `', 'y')`,
			uniqueViolation, "credentials_pkey"},
		{"negative failure_count", `INSERT INTO login_attempts (key, failure_count) VALUES ('email:x@example.com', -1)`,
			checkViolation, "login_attempts_failure_count_non_negative"},
		{"blank pocket_display_name", `INSERT INTO pockets (id, household_id, pocket_display_name) VALUES (gen_random_uuid(), '` + hh + `', '   ')`,
			checkViolation, "pockets_pocket_display_name_not_blank"},
		{"second Pocket under the same name, other case", `INSERT INTO pockets (id, household_id, pocket_display_name) VALUES ('` + p1 + `', '` + hh + `', 'OVO'), (gen_random_uuid(), '` + hh + `', 'ovo')`,
			uniqueViolation, "pockets_household_name_idx"},
		// Inactive is not deleted: the Pocket is still on screen and still
		// labels history, so it keeps its name.
		{"name held by an inactive Pocket", `INSERT INTO pockets (id, household_id, pocket_display_name, active) VALUES ('` + p1 + `', '` + hh + `', 'OVO', false);
			INSERT INTO pockets (id, household_id, pocket_display_name) VALUES (gen_random_uuid(), '` + hh + `', 'ovo')`,
			uniqueViolation, "pockets_household_name_idx"},
		{"Pocket for a Household that doesn't exist", `INSERT INTO pockets (id, household_id, pocket_display_name) VALUES (gen_random_uuid(), gen_random_uuid(), 'x')`,
			fkViolation, "pockets_household_id_fkey"},
		{"a Budget for a Household that doesn't exist", `INSERT INTO budgets (household_id, year_month) VALUES (gen_random_uuid(), '2026-11')`,
			fkViolation, "budgets_household_id_fkey"},
		{"a second Budget for one Household and Period", `INSERT INTO budgets (household_id, year_month) VALUES ('` + hh + `', '2026-11'), ('` + hh + `', '2026-11')`,
			uniqueViolation, "budgets_pkey"},
		{"a malformed year_month", `INSERT INTO budgets (household_id, year_month) VALUES ('` + hh + `', '2026-1')`,
			checkViolation, "budgets_year_month_shape"},
		{"a year_month with month 13", `INSERT INTO budgets (household_id, year_month) VALUES ('` + hh + `', '2026-13')`,
			checkViolation, "budgets_year_month_shape"},
		{"a year_month with month 00", `INSERT INTO budgets (household_id, year_month) VALUES ('` + hh + `', '2026-00')`,
			checkViolation, "budgets_year_month_shape"},
		{"a year_month with a date's day on it", `INSERT INTO budgets (household_id, year_month) VALUES ('` + hh + `', '2026-11-01')`,
			checkViolation, "budgets_year_month_shape"},
		{"a negative Monthly Budget", budgetInsert("2026-11") + `;
			INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ('` + hh + `', '2026-11', '2026-11-01', -0.0001)`,
			checkViolation, "monthly_budgets_monthly_budget_non_negative"},
		{"a Monthly Budget for a Period with no Budget row", `INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ('` + hh + `', '2026-11', '2026-11-01', 1)`,
			fkViolation, "monthly_budgets_household_id_year_month_fkey"},
		{"a Monthly Budget under another Household's Budget", budgetInsert("2026-11") + `;
			INSERT INTO households (id, household_display_name) VALUES ('` + hhOther + `', 'Other');
			INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ('` + hhOther + `', '2026-11', '2026-11-01', 1)`,
			fkViolation, "monthly_budgets_household_id_year_month_fkey"},
		{"two Monthly Budget versions on one day", budgetInsert("2026-11") + `;
			INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ('` + hh + `', '2026-11', '2026-11-01', 1), ('` + hh + `', '2026-11', '2026-11-01', 2)`,
			uniqueViolation, "monthly_budgets_pkey"},
		// A Monthly Budget that is more than the type holds is refused, not rounded.
		{"a Monthly Budget past DECIMAL(20,4)", budgetInsert("2026-11") + `;
			INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ('` + hh + `', '2026-11', '2026-11-01', 100000000000000000)`,
			"22003", ""},
		{"blank category_display_name", `INSERT INTO categories (id, household_id, category_display_name, bucket) VALUES (gen_random_uuid(), '` + hh + `', '   ', 'daily')`,
			checkViolation, "categories_category_display_name_not_blank"},
		{"an unknown bucket", categoryInsert(c1, hh, "Rent", "weekly"),
			checkViolation, "categories_bucket_known"},
		{"a bucket in the wrong case", categoryInsert(c1, hh, "Rent", "Daily"),
			checkViolation, "categories_bucket_known"},
		{"second Category under the same name, other case", categoryInsert(c1, hh, "Rent", "commitment") + `;` + categoryInsert(c2, hh, "rent", "commitment"),
			uniqueViolation, "categories_household_name_idx"},
		// The name is unique across both buckets: one real thing, one row.
		{"the same name in the other bucket", categoryInsert(c1, hh, "Insurance", "commitment") + `;` + categoryInsert(c2, hh, "insurance", "daily"),
			uniqueViolation, "categories_household_name_idx"},
		// Inactive is not deleted: the Category is still on screen and still holds
		// its allocation, so it keeps its name.
		{"name held by an inactive Category", `INSERT INTO categories (id, household_id, category_display_name, bucket, active) VALUES ('` + c1 + `', '` + hh + `', 'Rent', 'commitment', false);` + categoryInsert(c2, hh, "rent", "commitment"),
			uniqueViolation, "categories_household_name_idx"},
		{"a second Category under one id", categoryInsert(c1, hh, "Rent", "commitment") + `;` + categoryInsert(c1, hh, "Food", "daily"),
			uniqueViolation, "categories_pkey"},
		{"Category for a Household that doesn't exist", `INSERT INTO categories (id, household_id, category_display_name, bucket) VALUES (gen_random_uuid(), gen_random_uuid(), 'x', 'daily')`,
			fkViolation, "categories_household_id_fkey"},
		{"a negative allocation", categoryInsert(c1, hh, "Rent", "commitment") + `;` + budgetInsert("2026-11") + `;` + allocationInsert(hh, "2026-11-01", c1, "-0.0001"),
			checkViolation, "allocations_allocation_non_negative"},
		{"an allocation for a Period with no Budget row", categoryInsert(c1, hh, "Rent", "commitment") + `;` + allocationInsert(hh, "2026-11-01", c1, "1"),
			fkViolation, "allocations_household_id_year_month_fkey"},
		{"an allocation for a Category that doesn't exist", budgetInsert("2026-11") + `;` + allocationInsert(hh, "2026-11-01", c1, "1"),
			fkViolation, "allocations_household_id_category_id_fkey"},
		// The composite foreign key: the Household's own Budget row and another
		// Household's Category, which a plain category_id key would accept.
		{"an allocation for another Household's Category", budgetInsert("2026-11") + `;
			INSERT INTO households (id, household_display_name) VALUES ('` + hhOther + `', 'Other');` + categoryInsert(c1, hhOther, "Theirs", "commitment") + `;` + allocationInsert(hh, "2026-11-01", c1, "1"),
			fkViolation, "allocations_household_id_category_id_fkey"},
		{"two allocations for one Category on one day", categoryInsert(c1, hh, "Rent", "commitment") + `;` + budgetInsert("2026-11") + `;` + allocationInsert(hh, "2026-11-01", c1, "1") + `;` + allocationInsert(hh, "2026-11-01", c1, "2"),
			uniqueViolation, "allocations_pkey"},
		{"an allocation past DECIMAL(20,4)", categoryInsert(c1, hh, "Rent", "commitment") + `;` + budgetInsert("2026-11") + `;` + allocationInsert(hh, "2026-11-01", c1, "100000000000000000"),
			"22003", ""},
		// Expenses (#140). Every refusal is named by SQLSTATE and constraint.
		{"a zero amount", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "0", "", ""),
			checkViolation, "expenses_amount_nonzero"},
		{"a zero amount written with decimals", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "0.0000", "", ""),
			checkViolation, "expenses_amount_nonzero"},
		{"an amount that rounds to zero is stored as zero and refused", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "0.00004", "", ""),
			checkViolation, "expenses_amount_nonzero"},
		{"a lower-case currency", pocketInsert(p1, hh, "OVO") + `;` + `INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id) VALUES ('` + e1 + `', '` + hh + `', '2026-11-23', 1, 'idr', '` + p1 + `')`,
			checkViolation, "expenses_currency_iso4217"},
		{"a two-letter currency", pocketInsert(p1, hh, "OVO") + `;` + `INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id) VALUES ('` + e1 + `', '` + hh + `', '2026-11-23', 1, 'ID', '` + p1 + `')`,
			checkViolation, "expenses_currency_iso4217"},
		{"a blank description", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", ", description", ", '   '"),
			checkViolation, "expenses_description_not_blank"},
		{"an unknown ownership", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", ", ownership", ", 'shared'"),
			checkViolation, "expenses_ownership_known"},
		{"a sole_owner with no owner", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", ", ownership", ", 'sole_owner'"),
			checkViolation, "expenses_owner_matches_ownership"},
		{"a joint Expense with an owner", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", ", owner_user_id", ", '"+u1+"'"),
			checkViolation, "expenses_owner_matches_ownership"},
		{"an Expense with no Pocket", `INSERT INTO expenses (id, household_id, occurred_on, amount, currency) VALUES ('` + e1 + `', '` + hh + `', '2026-11-23', 1, 'IDR')`,
			"23502", ""},
		{"an Expense on a Pocket that doesn't exist", expenseInsert(e1, hh, "1", "", ""),
			fkViolation, "expenses_household_id_pocket_id_fkey"},
		// The composite foreign keys: the Household's own id beside another
		// Household's row, which a single-column key would accept.
		{"an Expense on another Household's Pocket", `INSERT INTO households (id, household_display_name) VALUES ('` + hhOther + `', 'Other');` +
			pocketInsert(p1, hhOther, "Theirs") + `;` + expenseInsert(e1, hh, "1", "", ""),
			fkViolation, "expenses_household_id_pocket_id_fkey"},
		{"an Expense in another Household's Category", `INSERT INTO households (id, household_display_name) VALUES ('` + hhOther + `', 'Other');` +
			pocketInsert(p1, hh, "OVO") + `;` + categoryInsert(c1, hhOther, "Theirs", "daily") + `;` + expenseInsert(e1, hh, "1", ", category_id", ", '"+c1+"'"),
			fkViolation, "expenses_household_id_category_id_fkey"},
		{"an Expense owned by another Household's User", `INSERT INTO households (id, household_display_name) VALUES ('` + hhOther + `', 'Other');
			INSERT INTO users (id, household_id, email, user_display_name) VALUES ('` + u2 + `', '` + hhOther + `', 'b@example.com', 'B');` +
			pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", ", ownership, owner_user_id", ", 'sole_owner', '"+u2+"'"),
			fkViolation, "expenses_household_id_owner_user_id_fkey"},
		{"a second Expense under one id", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", "", "") + `;` + expenseInsert(e1, hh, "2", "", ""),
			uniqueViolation, "expenses_pkey"},
		{"an amount past DECIMAL(20,4)", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "100000000000000000", "", ""),
			"22003", ""},
		{"a second Pocket under one (household, id)", pocketInsert(p1, hh, "OVO") + `;` + pocketInsert(p1, hh, "Other name"),
			uniqueViolation, "pockets_pkey"},
		// No row is ever hard-deleted, and nothing cascades: a referenced Pocket,
		// Category or User cannot be removed from under an Expense.
		{"a hard delete of a Pocket an Expense references", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", "", "") + `;DELETE FROM pockets WHERE id = '` + p1 + `'`,
			fkViolation, "expenses_household_id_pocket_id_fkey"},
		{"a hard delete of a Category an Expense references", pocketInsert(p1, hh, "OVO") + `;` + categoryInsert(c1, hh, "Food", "daily") + `;` + expenseInsert(e1, hh, "1", ", category_id", ", '"+c1+"'") + `;DELETE FROM categories WHERE id = '` + c1 + `'`,
			fkViolation, "expenses_household_id_category_id_fkey"},
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
		{"period_start_day 28", `INSERT INTO households (id, household_display_name, period_start_day) VALUES (gen_random_uuid(), 'x', 28)`},
		{"allowance_mode fixed", `INSERT INTO households (id, household_display_name, allowance_mode) VALUES (gen_random_uuid(), 'x', 'fixed')`},
		{"zero expected income", `INSERT INTO households (id, household_display_name, expected_monthly_income) VALUES (gen_random_uuid(), 'x', 0)`},
		{"session", `INSERT INTO sessions (id, user_id, expires_at) VALUES (repeat('a', 64), '` + u1 + `', now() + interval '30 days')`},
		// No password: the handshake a Google identity will open.
		{"handshake without a password hash", `INSERT INTO onboarding_handshakes (id, email, user_display_name, locale, expires_at) VALUES (repeat('c', 64), 'h@example.com', 'H', 'id-ID', now() + interval '15 minutes')`},
		{"the same Pocket name in another Household", `INSERT INTO households (id, household_display_name) VALUES ('019212e0-0000-7000-8000-0000000000aa', 'Other');
			INSERT INTO pockets (id, household_id, pocket_display_name) VALUES (gen_random_uuid(), '` + hh + `', 'OVO'), (gen_random_uuid(), '019212e0-0000-7000-8000-0000000000aa', 'OVO')`},
		{"a name whose earlier holder is soft-deleted", `INSERT INTO pockets (id, household_id, pocket_display_name, deleted_at) VALUES (gen_random_uuid(), '` + hh + `', 'OVO', now());
			INSERT INTO pockets (id, household_id, pocket_display_name) VALUES (gen_random_uuid(), '` + hh + `', 'ovo')`},
		{"two soft-deleted Pockets under one name", `INSERT INTO pockets (id, household_id, pocket_display_name, deleted_at) VALUES (gen_random_uuid(), '` + hh + `', 'OVO', now()), (gen_random_uuid(), '` + hh + `', 'OVO', now())`},
		{"a Monthly Budget of zero: a real answer", budgetInsert("2026-11") + `;
			INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ('` + hh + `', '2026-11', '2026-11-01', 0)`},
		{"the largest Monthly Budget the type holds", budgetInsert("2026-11") + `;
			INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ('` + hh + `', '2026-11', '2026-11-01', 9999999999999999.9999)`},
		{"two versions on different days of one Period", budgetInsert("2026-11") + `;
			INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ('` + hh + `', '2026-11', '2026-11-01', 1), ('` + hh + `', '2026-11', '2026-11-14', 2)`},
		{"the same Period for two Households", `INSERT INTO households (id, household_display_name) VALUES ('` + hhOther + `', 'Other');
			INSERT INTO budgets (household_id, year_month) VALUES ('` + hh + `', '2026-11'), ('` + hhOther + `', '2026-11')`},
		{"a December Period and the January after it", `INSERT INTO budgets (household_id, year_month) VALUES ('` + hh + `', '2026-12'), ('` + hh + `', '2027-01')`},
		{"the same Category name in another Household", `INSERT INTO households (id, household_display_name) VALUES ('` + hhOther + `', 'Other');` +
			categoryInsert(c1, hh, "Rent", "commitment") + `;` + categoryInsert(c2, hhOther, "Rent", "commitment")},
		{"a Category name whose earlier holder is soft-deleted", `INSERT INTO categories (id, household_id, category_display_name, bucket, deleted_at) VALUES ('` + c1 + `', '` + hh + `', 'Rent', 'commitment', now());` + categoryInsert(c2, hh, "rent", "daily")},
		{"two soft-deleted Categories under one name", `INSERT INTO categories (id, household_id, category_display_name, bucket, deleted_at) VALUES ('` + c1 + `', '` + hh + `', 'Rent', 'daily', now()), ('` + c2 + `', '` + hh + `', 'Rent', 'daily', now())`},
		{"an allocation of zero: how a retired Commitment is freed", categoryInsert(c1, hh, "Rent", "commitment") + `;` + budgetInsert("2026-11") + `;` + allocationInsert(hh, "2026-11-01", c1, "0")},
		{"a NULL allocation: none from that day", categoryInsert(c1, hh, "Rent", "commitment") + `;` + budgetInsert("2026-11") + `;` + allocationInsert(hh, "2026-11-01", c1, "NULL")},
		{"the largest allocation the type holds", categoryInsert(c1, hh, "Rent", "commitment") + `;` + budgetInsert("2026-11") + `;` + allocationInsert(hh, "2026-11-01", c1, "9999999999999999.9999")},
		{"an allocation for a Category with no Monthly Budget in the Period", categoryInsert(c1, hh, "Rent", "commitment") + `;` + budgetInsert("2026-11") + `;` + allocationInsert(hh, "2026-11-01", c1, "1")},
		{"two Categories' allocations on one day, and one Category's on two days", categoryInsert(c1, hh, "Rent", "commitment") + `;` + categoryInsert(c2, hh, "Food", "daily") + `;` + budgetInsert("2026-11") + `;` +
			allocationInsert(hh, "2026-11-01", c1, "1") + `;` + allocationInsert(hh, "2026-11-01", c2, "2") + `;` + allocationInsert(hh, "2026-11-14", c1, "3")},
		{"an allocation for a deleted Category: nothing in the schema forbids the log keeping it", `INSERT INTO categories (id, household_id, category_display_name, bucket, deleted_at) VALUES ('` + c1 + `', '` + hh + `', 'Rent', 'commitment', now());` + budgetInsert("2026-11") + `;` + allocationInsert(hh, "2026-11-01", c1, "1")},
		// Expenses (#140).
		{"an Expense with only what is required", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "25000", "", "")},
		{"a negative amount: a refund with no recorded original", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "-25000", "", "")},
		{"the smallest amount the type holds, either sign", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "0.0001", "", "") + `;` + expenseInsert(e2, hh, "-0.0001", "", "")},
		{"the largest amount the type holds", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "9999999999999999.9999", "", "")},
		{"a null Category, time and description", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", ", category_id, occurred_time, description", ", NULL, NULL, NULL")},
		{"an Expense with a Category, a time and a description", pocketInsert(p1, hh, "OVO") + `;` + categoryInsert(c1, hh, "Food", "daily") + `;` +
			expenseInsert(e1, hh, "1", ", category_id, occurred_time, description", ", '"+c1+"', '19:05', 'Lunch'")},
		{"a sole_owner naming a User of the Household", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", ", ownership, owner_user_id", ", 'sole_owner', '"+u1+"'")},
		{"an Expense dated in the future", pocketInsert(p1, hh, "OVO") + `;` + expenseInsert(e1, hh, "1", "", "") + `;UPDATE expenses SET occurred_on = '2999-01-01'`},
		{"an inactive, soft-deleted Pocket still referenced by a deleted Expense", `INSERT INTO pockets (id, household_id, pocket_display_name, active, deleted_at) VALUES ('` + p1 + `', '` + hh + `', 'OVO', false, now());` +
			expenseInsert(e1, hh, "1", ", deleted_at", ", now()")},
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
	if got := scalar(t, tdb, `SELECT appearance FROM users WHERE id = '`+u1+`'`); got != "system" {
		t.Errorf("user appearance default = %q, want system", got)
	}
	mustExec(t, tdb, pocketInsert(p1, hh, "OVO"))
	if got := scalar(t, tdb, `SELECT active || ' / ' || (deleted_at IS NULL)  FROM pockets WHERE id = '`+p1+`'`); got != "true / true" {
		t.Errorf("pocket defaults (active / deleted_at null) = %q", got)
	}
	mustExec(t, tdb, categoryInsert(c1, hh, "Rent", "commitment"))
	if got := scalar(t, tdb, `SELECT active || ' / ' || (deleted_at IS NULL) || ' / ' || (created_at IS NOT NULL) FROM categories WHERE id = '`+c1+`'`); got != "true / true / true" {
		t.Errorf("category defaults (active / deleted_at null / created_at set) = %q", got)
	}
}

// The Expense defaults are the schema's: the Household's own joint ownership, a
// capture instant, and nothing deleted.
func TestExpenseDefaults(t *testing.T) {
	tdb := seeded(t)
	mustExec(t, tdb, pocketInsert(p1, hh, "OVO"))
	mustExec(t, tdb, expenseInsert(e1, hh, "25000", "", ""))
	if got := scalar(t, tdb, `SELECT ownership || ' / ' || (owner_user_id IS NULL) || ' / ' || (deleted_at IS NULL) || ' / ' ||
		(captured_at IS NOT NULL) || ' / ' || (created_at IS NOT NULL) || ' / ' || (updated_at IS NOT NULL) || ' / ' ||
		(occurred_time IS NULL) || ' / ' || (category_id IS NULL) || ' / ' || (description IS NULL)
		FROM expenses WHERE id = '`+e1+`'`); got != "joint / true / true / true / true / true / true / true / true" {
		t.Errorf("expense defaults = %q", got)
	}
	// Stored at the scale, whatever scale it was written at, and a refund keeps
	// its sign.
	mustExec(t, tdb, expenseInsert(e2, hh, "-25000", "", ""))
	if got := scalar(t, tdb, `SELECT amount::text FROM expenses WHERE id = '`+e2+`'`); got != "-25000.0000" {
		t.Errorf("amount = %s, want -25000.0000", got)
	}
}

// The seven indexes of #140 R7 are all in the baseline, so the later slices
// need no second db-reset; a dropped or renamed one should fail here, not in a
// plan nobody reads. Their definitions are pinned as well: the partial
// predicates and the sort directions are what make each one serve its query.
func TestExpenseIndexes(t *testing.T) {
	tdb := seeded(t)
	for name, want := range map[string]string{
		"expenses_household_live_idx":          "(household_id, occurred_on DESC, captured_at DESC, id DESC) WHERE (deleted_at IS NULL)",
		"expenses_household_uncategorised_idx": "(household_id, occurred_on DESC, id DESC) WHERE ((deleted_at IS NULL) AND (category_id IS NULL))",
		"expenses_household_captured_idx":      "(household_id, captured_at DESC) WHERE (deleted_at IS NULL)",
		"expenses_household_bin_idx":           "(household_id, deleted_at DESC) WHERE (deleted_at IS NOT NULL)",
		"expenses_household_pocket_idx":        "(household_id, pocket_id)",
		"expenses_household_category_idx":      "(household_id, category_id) WHERE (category_id IS NOT NULL)",
		"expenses_household_owner_idx":         "(household_id, owner_user_id) WHERE (owner_user_id IS NOT NULL)",
	} {
		got := scalar(t, tdb, `SELECT coalesce(substring(indexdef from 'USING btree (.*)$'), 'MISSING')
			FROM (SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = '`+name+`'
			      UNION ALL SELECT 'USING btree MISSING' WHERE NOT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = '`+name+`')) x LIMIT 1`)
		if got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
}

// A deleted Category releases its name, and restoring it into a name another
// live Category now holds is refused by the same index.
func TestSoftDeletedCategoriesReleaseTheirName(t *testing.T) {
	tdb := seeded(t)
	mustExec(t, tdb, categoryInsert(c1, hh, "Rent", "commitment"))
	mustExec(t, tdb, `UPDATE categories SET deleted_at = now() WHERE id = '`+c1+`'`)
	mustExec(t, tdb, categoryInsert(c2, hh, "rent", "daily"))
	_, err := tdb.Pool.Exec(context.Background(), `UPDATE categories SET deleted_at = NULL WHERE id = '`+c1+`'`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation || pgErr.ConstraintName != "categories_household_name_idx" {
		t.Fatalf("restore into a taken name: got %v, want %s on categories_household_name_idx", err, uniqueViolation)
	}
}

// A deleted Pocket releases its name, and restoring it into a name another live
// Pocket now holds is refused by the same index.
func TestSoftDeletedPocketsReleaseTheirName(t *testing.T) {
	tdb := seeded(t)
	mustExec(t, tdb, pocketInsert(p1, hh, "OVO"))
	mustExec(t, tdb, `UPDATE pockets SET deleted_at = now() WHERE id = '`+p1+`'`)
	mustExec(t, tdb, pocketInsert("019212e0-0000-7000-8000-000000000012", hh, "ovo"))
	_, err := tdb.Pool.Exec(context.Background(), `UPDATE pockets SET deleted_at = NULL WHERE id = '`+p1+`'`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation || pgErr.ConstraintName != "pockets_household_name_idx" {
		t.Fatalf("restore into a taken name: got %v, want %s on pockets_household_name_idx", err, uniqueViolation)
	}
}

// The email index ignores soft-deleted Users: an address is free again once
// its holder is deleted, however many deleted Users shared it.
func TestSoftDeletedUsersReleaseTheirEmail(t *testing.T) {
	tdb := seeded(t)
	mustExec(t, tdb, `INSERT INTO users (id, household_id, email, user_display_name) VALUES ('`+u2+`', '`+hh+`', 'b@example.com', 'B')`)
	mustExec(t, tdb, `UPDATE users SET deleted_at = now() WHERE id IN ('`+u1+`', '`+u2+`')`)
	mustExec(t, tdb, `UPDATE users SET email = 'same@example.com' WHERE id IN ('`+u1+`', '`+u2+`')`)
	mustExec(t, tdb, `INSERT INTO users (id, household_id, email, user_display_name) VALUES (gen_random_uuid(), '`+hh+`', 'same@example.com', 'live')`)
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

// The Budget tables' shape is a set of rulings (#130 R11, R14): composite
// natural keys with no id, no deleted_at because nothing deletes from a version
// log, and no currency because a plan is in the Household's reporting_currency.
// A column added to one of them by a later edit should be a decision, so the
// columns are pinned.
func TestBudgetTablesHaveExactlyTheRuledColumns(t *testing.T) {
	tdb := seeded(t)
	for table, want := range map[string]string{
		"budgets":         "created_at,household_id,year_month",
		"monthly_budgets": "effective_on,household_id,monthly_budget,updated_at,year_month",
		"allocations":     "allocation,category_id,effective_on,household_id,updated_at,year_month",
	} {
		got := scalar(t, tdb, `SELECT string_agg(column_name, ',' ORDER BY column_name)
			FROM information_schema.columns WHERE table_schema = 'public' AND table_name = '`+table+`'`)
		if got != want {
			t.Errorf("%s columns = %s, want %s", table, got, want)
		}
	}
}

func TestBudgetDefaults(t *testing.T) {
	tdb := seeded(t)
	mustExec(t, tdb, budgetInsert("2026-11"))
	mustExec(t, tdb, `INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget) VALUES ('`+hh+`', '2026-11', '2026-11-01', 9000000)`)
	if got := scalar(t, tdb, `SELECT (created_at IS NOT NULL)::text FROM budgets`); got != "true" {
		t.Errorf("budgets.created_at defaulted = %s, want true", got)
	}
	// Stored at the scale, whatever scale it was written at.
	if got := scalar(t, tdb, `SELECT monthly_budget::text FROM monthly_budgets`); got != "9000000.0000" {
		t.Errorf("monthly_budget = %s, want 9000000.0000", got)
	}
	mustExec(t, tdb, categoryInsert(c1, hh, "Rent", "commitment"))
	mustExec(t, tdb, allocationInsert(hh, "2026-11-01", c1, "3000000"))
	if got := scalar(t, tdb, `SELECT allocation::text FROM allocations`); got != "3000000.0000" {
		t.Errorf("allocation = %s, want 3000000.0000", got)
	}
}
