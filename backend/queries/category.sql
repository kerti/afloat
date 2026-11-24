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

-- The first statement of a delete, in the delete's own transaction: it takes the
-- Category's row lock (FOR UPDATE, which conflicts with the FOR SHARE every
-- allocation write holds, and with the key-share an allocation insert takes) and
-- answers whether the row is deleted already. The guard that follows
-- (CategoryHeldElsewhere) must be a SEPARATE, LATER statement: under READ
-- COMMITTED a statement's snapshot is taken when it starts, so a guard folded
-- into the UPDATE's WHERE would have been evaluated before the lock wait ended
-- and would not see the allocation committed during it (BOOTSTRAP.md §4, #130
-- C8). Another Household's or an unknown id is no row. This is the one place
-- that reads a deleted Category on purpose.
-- name: LockCategory :one
SELECT deleted_at FROM categories
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id)
FOR UPDATE;

-- The in-use guard (#130 C8, R9): does the Category hold an allocation, a
-- non-null amount, in any Budget Period but the current one? Any row counts, not
-- only the latest of a Period: a Category an earlier Period held an amount for
-- is part of that Period's record, even if a later version cleared it. Run
-- after LockCategory, in the same transaction, and never inside the UPDATE.
-- name: CategoryHeldElsewhere :one
SELECT EXISTS (
    SELECT 1 FROM allocations
    WHERE household_id = sqlc.arg(household_id)
      AND category_id = sqlc.arg(category_id)
      AND year_month <> sqlc.arg(year_month)
      AND allocation IS NOT NULL
);

-- A soft delete (BOOTSTRAP.md §4), as SoftDeletePocket: the row stays, the first
-- deleted_at is kept, and updated_at moves only for the statement that did the
-- work. Called only after LockCategory and a clear guard. It touches no
-- allocation: a delete drops the Category out of every figure because reads join
-- live Categories, and a restore brings the figures back exactly.
-- name: SoftDeleteCategory :execrows
UPDATE categories SET
    deleted_at = coalesce(deleted_at, now()),
    updated_at = CASE WHEN deleted_at IS NULL THEN now() ELSE updated_at END
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id);

-- The Undo of SoftDeleteCategory, as RestorePocket: clears deleted_at and
-- returns the row, active as it was. Idempotent (a live Category is a row and
-- comes back unchanged). Another Household's or an unknown id is no row.
-- Re-entering the partial unique name index is what a held name trips: 23505 on
-- categories_household_name_idx, answered NAME_TAKEN with the row still deleted,
-- decided by the index and never by a lookup. This statement writes no
-- allocation: the handler carries the Category forward after it, in the same
-- transaction (budget.CarryCategoryForward).
-- name: RestoreCategory :one
UPDATE categories SET
    deleted_at = NULL,
    updated_at = CASE WHEN deleted_at IS NOT NULL THEN now() ELSE updated_at END
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id)
RETURNING *;
