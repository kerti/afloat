-- Categories (PRD S8). Tenant-scoped in SQL: every statement filters on the
-- authenticated User's household_id, never anything the request supplied
-- (BOOTSTRAP.md §4). A Category carries no allocation: that is the Budget's
-- (queries/budget.sql).

-- Every Category not deleted, inactive ones included, the two buckets
-- together. created_at then id: a rename or a deactivation must not reshuffle
-- the list, and the order is independent of the database's collation.
-- name: ListCategories :many
SELECT * FROM categories
WHERE household_id = sqlc.arg(household_id) AND deleted_at IS NULL
ORDER BY created_at, id;

-- ON CONFLICT (id) arbitrates the primary key only: a row back is a create, no
-- row is the id already being held (see GetLiveCategory). A name clash is not
-- arbitrated and still raises 23505 on categories_household_name_idx.
-- name: CreateCategory :one
INSERT INTO categories (id, household_id, category_display_name, bucket)
VALUES (sqlc.arg(id), sqlc.arg(household_id), sqlc.arg(category_display_name), sqlc.arg(bucket))
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- The read-back of a create that found its id taken: a replay when the row is
-- the caller's own live Category, and no row for anything else (another
-- Household's, or a deleted one).
-- name: GetLiveCategory :one
SELECT * FROM categories
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL;
