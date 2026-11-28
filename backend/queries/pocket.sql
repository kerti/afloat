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
-- updated_at moves only when a column's value does (#137; the row-wise IS DISTINCT FROM
-- as in UpdateExpense), so a {} or a resend of the stored values is a no-op
-- (BOOTSTRAP.md §4), and from the database's own clock (BOOTSTRAP.md §5.1).
-- name: UpdatePocket :one
UPDATE pockets SET
    pocket_display_name = coalesce(sqlc.narg(pocket_display_name), pocket_display_name),
    active              = coalesce(sqlc.narg(active), active),
    updated_at          = CASE WHEN (pocket_display_name, active)
                                    IS DISTINCT FROM (
                                        coalesce(sqlc.narg(pocket_display_name), pocket_display_name),
                                        coalesce(sqlc.narg(active), active)
                                    )
                               THEN now() ELSE updated_at END
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
-- The guards are NOT here, and none may be folded in as a NOT EXISTS in the
-- WHERE: under READ COMMITTED an UPDATE that waits for a lock proceeds on the
-- row's new version without re-running its subquery, so a row committed during
-- the wait is never seen. The handler calls this last, in one transaction of
-- statements in this order (BOOTSTRAP.md §4): LockHousehold, LockPocket, then the
-- guards as SEPARATE later statements, whose snapshots are taken only after the
-- locks were granted: OtherActivePocketExists (409 LAST_POCKET) and
-- PocketHasExpenses (409 POCKET_IN_USE, any Expense, live or deleted), then this
-- write. Every Expense create share-locks the Household and then the Pocket
-- first (expense.sql), so an Expense that committed before the delete's guard is
-- seen by it, and one that comes after finds the Pocket deleted and is refused.
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

-- The Household's row lock, taken FIRST by every writer that can take a Pocket
-- out of the active set (delete, and PATCH with active = false): "a Household
-- always keeps one active Pocket" (PRD S10, R1 of #140) is a count over several
-- rows, and two removals of the last two would each count the other still there
-- and both write. FOR NO KEY UPDATE serialises them: the second waits here, and
-- its count, a later statement, starts after the first has committed
-- (BOOTSTRAP.md §4). NO KEY UPDATE and not UPDATE, on purpose: an insert that
-- carries an FK to this row takes FOR KEY SHARE on it, which FOR UPDATE would
-- block, and a writer already holding FOR SHARE on a Pocket would deadlock with
-- a removal waiting on that Pocket. NO KEY UPDATE still conflicts with FOR SHARE
-- and with itself, and not with KEY SHARE. No row is a Household that is gone,
-- which a live session cannot have.
--
-- It is also the lock of CURRENCY_LOCKED (PATCH /household, #140 R12), which
-- conflicts with the FOR SHARE every Expense create takes on this row while it
-- stamps the Household's currency (LockHouseholdShared). The lock order, for every
-- writer, is Household, then Pocket, then Category.
-- name: LockHousehold :one
SELECT id FROM households
WHERE id = sqlc.arg(household_id) AND deleted_at IS NULL
FOR NO KEY UPDATE;

-- The Pocket's own row lock, after LockHousehold, and what the writer decides
-- on: deleted or live, active or not. FOR NO KEY UPDATE for the reason given on
-- LockHousehold. Another Household's or an unknown id is
-- no row (404). This is the one read of a deleted Pocket on purpose, because
-- deleting a deleted one is the idempotent success and is never refused.
-- name: LockPocket :one
SELECT deleted_at, active FROM pockets
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id)
FOR NO KEY UPDATE;

-- Does the Household hold an active, live Pocket other than this one? A SEPARATE
-- statement run after LockHousehold, never part of the write (see the comment
-- on SoftDeletePocket). False means removing this Pocket would leave none:
-- 409 LAST_POCKET.
-- name: OtherActivePocketExists :one
SELECT EXISTS (
    SELECT 1 FROM pockets
    WHERE household_id = sqlc.arg(household_id)
      AND id <> sqlc.arg(id)
      AND active
      AND deleted_at IS NULL
);
