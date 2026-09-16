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
func (tdb *TestDB) CreateHousehold(t *testing.T, displayName string) pgtype.UUID {
	t.Helper()
	id := NewUUID(t)
	_, err := tdb.Pool.Exec(context.Background(),
		`INSERT INTO households (id, display_name) VALUES ($1, $2)`, id, displayName)
	if err != nil {
		t.Fatalf("testutil: create household: %v", err)
	}
	return id
}

// CreateUser inserts a User into a Household and returns its id.
func (tdb *TestDB) CreateUser(t *testing.T, householdID pgtype.UUID, email, displayName string) pgtype.UUID {
	t.Helper()
	id := NewUUID(t)
	_, err := tdb.Pool.Exec(context.Background(),
		`INSERT INTO users (id, household_id, email, display_name) VALUES ($1, $2, $3, $4)`,
		id, householdID, email, displayName)
	if err != nil {
		t.Fatalf("testutil: create user: %v", err)
	}
	return id
}
