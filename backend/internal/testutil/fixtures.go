package testutil

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// NewUUID returns a UUIDv7, the project's primary-key shape (BOOTSTRAP.md §4).
func NewUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("testutil: new uuid: %v", err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// CreateHousehold inserts a Household and returns its id. Tests that need two
// tenants call it twice: the harness pre-seeds nothing, because a pre-seeded
// tenant is a fixture everyone quietly depends on.
func (tdb *TestDB) CreateHousehold(t *testing.T, householdDisplayName string) pgtype.UUID {
	t.Helper()
	id := NewUUID(t)
	_, err := tdb.Pool.Exec(context.Background(),
		`INSERT INTO households (id, household_display_name) VALUES ($1, $2)`, id, householdDisplayName)
	if err != nil {
		t.Fatalf("testutil: create household: %v", err)
	}
	return id
}

// CreateUser inserts a User into a Household and returns its id.
func (tdb *TestDB) CreateUser(t *testing.T, householdID pgtype.UUID, email, userDisplayName string) pgtype.UUID {
	t.Helper()
	id := NewUUID(t)
	_, err := tdb.Pool.Exec(context.Background(),
		`INSERT INTO users (id, household_id, email, user_display_name) VALUES ($1, $2, $3, $4)`,
		id, householdID, email, userDisplayName)
	if err != nil {
		t.Fatalf("testutil: create user: %v", err)
	}
	return id
}

// CreatePocket inserts an active, live Pocket into a Household and returns its
// id. A test that needs an inactive or deleted one updates the row.
func (tdb *TestDB) CreatePocket(t *testing.T, householdID pgtype.UUID, pocketDisplayName string) pgtype.UUID {
	t.Helper()
	id := NewUUID(t)
	_, err := tdb.Pool.Exec(context.Background(),
		`INSERT INTO pockets (id, household_id, pocket_display_name) VALUES ($1, $2, $3)`,
		id, householdID, pocketDisplayName)
	if err != nil {
		t.Fatalf("testutil: create pocket: %v", err)
	}
	return id
}

// CreateCategory inserts an active, live Category of the given bucket
// ("daily" or "commitment") into a Household and returns its id. A test that
// needs an inactive or deleted one updates the row; one that needs an allocation
// inserts it, or goes through the API.
func (tdb *TestDB) CreateCategory(t *testing.T, householdID pgtype.UUID, categoryDisplayName, bucket string) pgtype.UUID {
	t.Helper()
	id := NewUUID(t)
	_, err := tdb.Pool.Exec(context.Background(),
		`INSERT INTO categories (id, household_id, category_display_name, bucket) VALUES ($1, $2, $3, $4)`,
		id, householdID, categoryDisplayName, bucket)
	if err != nil {
		t.Fatalf("testutil: create category: %v", err)
	}
	return id
}

// ExpenseOpts are the columns a test may set on CreateExpense. The zero value
// is a live, uncategorised Expense of 25000 on 2026-11-23, in IDR.
type ExpenseOpts struct {
	// Category is the Expense's Category; the zero (invalid) UUID is none.
	Category pgtype.UUID
	// Deleted puts the Expense in the Recycle Bin.
	Deleted bool
}

// CreateExpense inserts an Expense on a Pocket of a Household and returns its
// id. It writes the row directly, as the other fixtures do, so a test of a guard
// needs no handler to have an Expense to guard against.
func (tdb *TestDB) CreateExpense(t *testing.T, householdID, pocketID pgtype.UUID, opts ExpenseOpts) pgtype.UUID {
	t.Helper()
	id := NewUUID(t)
	_, err := tdb.Pool.Exec(context.Background(),
		`INSERT INTO expenses (id, household_id, occurred_on, amount, currency, pocket_id, category_id, deleted_at)
		 VALUES ($1, $2, '2026-11-23', 25000, 'IDR', $3, $4, CASE WHEN $5::boolean THEN now() END)`,
		id, householdID, pocketID, opts.Category, opts.Deleted)
	if err != nil {
		t.Fatalf("testutil: create expense: %v", err)
	}
	return id
}
