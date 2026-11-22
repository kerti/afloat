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

-- Merge semantics (BOOTSTRAP.md §4): a NULL argument leaves its column alone,
-- in one statement, so a rename and a deactivation sent by two writers both
-- land. Both arguments are sqlc.narg so that a false for active is a value
-- and not "absent". Neither column is nullable, so coalesce cannot be mistaken
-- for "clear". A row that is another Household's, unknown or deleted is no row:
-- the caller answers all three NOT_FOUND. A 23505 on pockets_household_name_idx
-- is a rename into a held name.
--
-- updated_at is set here because the baseline has no trigger, and from the
-- database's own clock (BOOTSTRAP.md §5.1).
-- name: UpdatePocket :one
UPDATE pockets SET
    pocket_display_name = coalesce(sqlc.narg(pocket_display_name), pocket_display_name),
    active              = coalesce(sqlc.narg(active), active),
    updated_at          = now()
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL
RETURNING *;

-- A soft delete (BOOTSTRAP.md §4): the row stays. Idempotent on the caller's own
-- Pocket and keeping the FIRST deletion instant, in one statement: coalesce
-- leaves an existing deleted_at alone, and updated_at moves only when this
-- statement is the one that deleted (a repeat changes no column). A row that
-- is another Household's or unknown is no row, and the caller answers NOT_FOUND;
-- a deleted one of the caller's own is a row, which is what makes a repeat a
-- success. No hard-delete statement exists in this directory, on purpose.
--
-- The in-use guard (PRD R1, #121) is NOT here. When the Expenses slice adds
-- the expenses table, a Pocket that any Expense row references, live or
-- soft-deleted, must answer 409 POCKET_IN_USE from this very statement (a
-- NOT EXISTS in the WHERE, with the Expense write taking FOR SHARE on the
-- Pocket row), because a check-then-delete would race an Expense create under
-- READ COMMITTED. See issue #121, P8 point 3.
-- name: SoftDeletePocket :execrows
UPDATE pockets SET
    deleted_at = coalesce(deleted_at, now()),
    updated_at = CASE WHEN deleted_at IS NULL THEN now() ELSE updated_at END
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id);

-- The Undo of SoftDeletePocket: clears deleted_at and returns the row, active
-- as it was. Idempotent: a Pocket that is not deleted is a row and comes back
-- unchanged (updated_at moves only when this statement restored). A row that is
-- another Household's or unknown is no row. Re-entering the partial unique
-- index is what a held name trips: 23505 on pockets_household_name_idx, which
-- the caller answers NAME_TAKEN with the row still deleted. The index, not a
-- lookup, decides it, so two concurrent restores into one name cannot both
-- win and cannot 500.
-- name: RestorePocket :one
UPDATE pockets SET
    deleted_at = NULL,
    updated_at = CASE WHEN deleted_at IS NOT NULL THEN now() ELSE updated_at END
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id)
RETURNING *;
