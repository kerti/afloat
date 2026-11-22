-- Pockets (PRD S10). Tenant-scoped in SQL: every statement filters on the
-- authenticated User's household_id, never anything the request supplied
-- (BOOTSTRAP.md §4).

-- Every Pocket not deleted, inactive ones included. created_at then id: a
-- rename or a deactivation must not reshuffle the list, and the order is
-- independent of the database's collation.
-- name: ListPockets :many
SELECT * FROM pockets
WHERE household_id = sqlc.arg(household_id) AND deleted_at IS NULL
ORDER BY created_at, id;

-- ON CONFLICT (id) arbitrates the primary key only: a row back is a create, no
-- row is the id already being held (see GetLivePocket). A name clash is not
-- arbitrated and still raises 23505 on pockets_household_name_idx.
-- name: CreatePocket :one
INSERT INTO pockets (id, household_id, pocket_display_name)
VALUES (sqlc.arg(id), sqlc.arg(household_id), sqlc.arg(pocket_display_name))
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- The read-back of a create that found its id taken: a replay when the row is
-- the caller's own live Pocket, and no row for anything else (another
-- Household's, or a deleted one).
-- name: GetLivePocket :one
SELECT * FROM pockets
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL;
