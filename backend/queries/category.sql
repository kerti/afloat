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

-- Rename and/or (de)activate, merge-style, in one statement so a rename and a
-- deactivation sent by two writers both land. Both arguments are sqlc.narg so
-- that a false for active is a value and not "absent"; neither column is
-- nullable, so coalesce cannot be mistaken for "clear". bucket is not here:
-- it is fixed at creation (#130 C7). A row that is another Household's, unknown
-- or deleted is no row: the caller answers all three NOT_FOUND. A 23505 on
-- categories_household_name_idx is a rename into a held name. Deactivating
-- touches no allocation, so no figure moves.
--
-- updated_at is set here because the baseline has no trigger, and from the
-- database's own clock (BOOTSTRAP.md §5.1).
-- name: UpdateCategory :one
UPDATE categories SET
    category_display_name = coalesce(sqlc.narg(category_display_name), category_display_name),
    active                = coalesce(sqlc.narg(active), active),
    updated_at            = now()
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL
RETURNING *;
