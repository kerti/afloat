-- Expenses (PRD E1 to E10). Tenant-scoped in SQL: every statement filters on the
-- authenticated User's household_id, never anything the request supplied
-- (BOOTSTRAP.md §4). No statement here hard-deletes, on purpose.

-- ---- the create ------------------------------------------------------------
--
-- A create is one transaction of statements in this order (BOOTSTRAP.md §4,
-- "Idempotent create"; #140 X4): GetLiveExpense, then LockHouseholdShared,
-- LockLivePocketShared, LockLiveCategoryShared and LockLiveUserShared as the
-- body needs them, then CreateExpense. The locks are in that order, the
-- Household first, for every writer, so none can wait on a row another holds
-- while holding one it wants.
--
-- Each lock is its own statement because a nullable reference cannot be locked
-- through an outer join, and because "no row" is the refusal: the liveness
-- predicate is in the locking statement, so a writer that comes after a delete
-- finds the row deleted and refuses, and one that came first is seen by the
-- delete's guard.

-- The read-back of a create: the caller's own live Expense is a replay, and no
-- row is anything else (another Household's, or a deleted one).
-- name: GetLiveExpense :one
SELECT * FROM expenses
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL;

-- FOR SHARE, so that the reporting_currency read here is the one the Expense is
-- stamped with and the Household's currency cannot change under it: the
-- currency change takes this row FOR NO KEY UPDATE (LockHousehold), which
-- conflicts with FOR SHARE, and counts the Expenses in a later statement.
-- Shared locks do not conflict with each other, so concurrent creates in one
-- Household never wait on one another. FOR SHARE and not FOR KEY SHARE, on
-- purpose: only the stronger one conflicts with FOR NO KEY UPDATE.
-- name: LockHouseholdShared :one
SELECT reporting_currency FROM households
WHERE id = sqlc.arg(household_id) AND deleted_at IS NULL
FOR SHARE;

-- The Pocket an Expense names, locked FOR SHARE in the statement that checks it
-- is the caller's own and live (BOOTSTRAP.md §4). active says whether it is
-- still offered for new capture. No row is unknown, another Household's or
-- deleted: one answer. FOR SHARE conflicts with the FOR NO KEY UPDATE a Pocket's
-- delete holds (LockPocket), so a create after the delete finds the Pocket
-- deleted, and one before it is seen by PocketHasExpenses.
-- name: LockLivePocketShared :one
SELECT active FROM pockets
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL
FOR SHARE;

-- The same for a Category, against LockCategory's FOR UPDATE.
-- name: LockLiveCategoryShared :one
SELECT active FROM categories
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL
FOR SHARE;

-- The same for the User an Expense is attributed to (ownership).
-- name: LockLiveUserShared :one
SELECT id FROM users
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL
FOR SHARE;

-- ON CONFLICT (id) arbitrates the primary key only: a row back is a create, and
-- no row is the id already being held (a concurrent replay that has committed in
-- the meantime), which the caller reads back. The references were checked and
-- locked before this statement, so no row here never means "a reference failed"
-- (#140 X4: an INSERT ... SELECT that checked its references would make
-- ErrNoRows mean two things). captured_at, created_at and updated_at are the
-- database's own clock. currency is the Household's, read under the lock above.
-- name: CreateExpense :one
INSERT INTO expenses (
    id, household_id, occurred_on, occurred_time, amount, currency,
    pocket_id, category_id, description, ownership, owner_user_id
) VALUES (
    sqlc.arg(id), sqlc.arg(household_id), sqlc.arg(occurred_on), sqlc.narg(occurred_time),
    sqlc.arg(amount), sqlc.arg(currency), sqlc.arg(pocket_id), sqlc.narg(category_id),
    sqlc.narg(description), sqlc.arg(ownership), sqlc.narg(owner_user_id)
)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- ---- the edit -------------------------------------------------------------
--
-- A PATCH is one transaction of statements in this order (BOOTSTRAP.md §4,
-- "Updates are PATCH"; #140 X9): GetLiveExpenseForUpdate, then, for each
-- reference the body CHANGES, LockHouseholdShared, LockLivePocketShared,
-- LockLiveCategoryShared and LockLiveUserShared in that order (the create's, so
-- no writer waits on a row another holds while holding one it wants), then
-- UpdateExpense. A reference the body leaves as stored is not looked at.

-- The Expense being edited, row-locked for the transaction. The merge itself
-- does not need it: UpdateExpense is one statement of coalesce and flags, so two
-- writers of different columns both land. The lock is for the decision made
-- BEFORE it, "is this reference different from the stored one": without it a
-- concurrent edit could move the Expense off a Pocket (leaving that Pocket
-- deletable) and a delete of the Pocket could commit between this read and the
-- write, which would then "leave the Pocket alone" and re-attach a deleted one.
-- FOR NO KEY UPDATE conflicts with another edit's and with nothing else an
-- Expense row is read by (it has no foreign keys pointing at it).
-- name: GetLiveExpenseForUpdate :one
SELECT * FROM expenses
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL
FOR NO KEY UPDATE;

-- Merge semantics: a NULL argument leaves a column alone (coalesce) where the
-- column cannot be cleared, and a column that can (occurred_time, category_id,
-- description, owner_user_id) takes an explicit set flag beside its value, which
-- is NULL for a clear. One statement, so two writers of different columns both
-- land and the last write to one wins; never a read-modify-write.
--
-- updated_at moves only when something does (#140 R9; the other PATCHes' no-op
-- is #137's): the row-wise IS DISTINCT FROM compares what is stored with what the
-- SET list is about to store, NULLs included. The statement still answers the
-- row on a no-op, so no row can only mean "not the caller's, or deleted".
-- name: UpdateExpense :one
UPDATE expenses SET
    amount        = coalesce(sqlc.narg(amount)::numeric(20,4), amount),
    occurred_on   = coalesce(sqlc.narg(occurred_on)::date, occurred_on),
    occurred_time = CASE WHEN sqlc.arg(set_occurred_time)::boolean
                         THEN sqlc.narg(occurred_time)::time ELSE occurred_time END,
    pocket_id     = coalesce(sqlc.narg(pocket_id)::uuid, pocket_id),
    category_id   = CASE WHEN sqlc.arg(set_category_id)::boolean
                         THEN sqlc.narg(category_id)::uuid ELSE category_id END,
    description   = CASE WHEN sqlc.arg(set_description)::boolean
                         THEN sqlc.narg(description)::text ELSE description END,
    ownership     = coalesce(sqlc.narg(ownership)::text, ownership),
    owner_user_id = CASE WHEN sqlc.arg(set_owner_user_id)::boolean
                         THEN sqlc.narg(owner_user_id)::uuid ELSE owner_user_id END,
    updated_at    = CASE WHEN (amount, occurred_on, occurred_time, pocket_id, category_id, description, ownership, owner_user_id)
                              IS DISTINCT FROM (
                                  coalesce(sqlc.narg(amount)::numeric(20,4), amount),
                                  coalesce(sqlc.narg(occurred_on)::date, occurred_on),
                                  CASE WHEN sqlc.arg(set_occurred_time)::boolean
                                       THEN sqlc.narg(occurred_time)::time ELSE occurred_time END,
                                  coalesce(sqlc.narg(pocket_id)::uuid, pocket_id),
                                  CASE WHEN sqlc.arg(set_category_id)::boolean
                                       THEN sqlc.narg(category_id)::uuid ELSE category_id END,
                                  CASE WHEN sqlc.arg(set_description)::boolean
                                       THEN sqlc.narg(description)::text ELSE description END,
                                  coalesce(sqlc.narg(ownership)::text, ownership),
                                  CASE WHEN sqlc.arg(set_owner_user_id)::boolean
                                       THEN sqlc.narg(owner_user_id)::uuid ELSE owner_user_id END
                              )
                         THEN now() ELSE updated_at END
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL
RETURNING *;

-- ---- the list --------------------------------------------------------------
--
-- Newest first, keyset-paged (#140 R7). The order is declared here and never
-- left to the database: occurred_on DESC, then the effective time DESC, then
-- captured_at DESC, then id DESC, so a page boundary is exact.
--
-- The effective time (time_key) is computed on read and never stored or shown:
--   1. occurred_time, when the Expense has one;
--   2. else, when it was captured on that same Period Day, the time of day it
--      was captured at, in the caller's time zone;
--   3. else the start of the day.
-- Every time is an offset into its Period Day, (t - day_starts_at) mod 24h, so
-- with a 04:00 start 01:30 sorts after 23:00: time minus interval wraps. The
-- Period Day of a capture is its wall clock less day_starts_at, the date part,
-- which is period.Today's rule (wall clock, never elapsed time, so a DST jump
-- cannot move where the day ends).
--
-- The cursor is the last row's (occurred_on, time_key, captured_at, id). The
-- occurred_on bound lets the index (household_id, occurred_on DESC, captured_at
-- DESC, id DESC) serve the page: Postgres sorts within a day (incremental sort),
-- since the key depends on mutable settings and is not indexed. page_size is
-- the page plus one, so the caller can tell a last page from a full one.
-- name: ListExpenses :many
SELECT sqlc.embed(e), k.time_key::time AS time_key
FROM expenses e
JOIN households h ON h.id = e.household_id
CROSS JOIN LATERAL (
    SELECT CASE
        WHEN e.occurred_time IS NOT NULL
            THEN e.occurred_time - (h.day_starts_at - time '00:00')
        WHEN ((e.captured_at AT TIME ZONE sqlc.arg(time_zone)::text) - (h.day_starts_at - time '00:00'))::date = e.occurred_on
            THEN (e.captured_at AT TIME ZONE sqlc.arg(time_zone)::text)::time - (h.day_starts_at - time '00:00')
        ELSE time '00:00'
    END AS time_key
) k
WHERE e.household_id = sqlc.arg(household_id)
  AND e.deleted_at IS NULL
  AND e.occurred_on <= coalesce(sqlc.narg(after_occurred_on)::date, 'infinity'::date)
  AND (
        sqlc.narg(after_occurred_on)::date IS NULL
        OR e.occurred_on < sqlc.narg(after_occurred_on)::date
        OR (k.time_key, e.captured_at, e.id)
           < (sqlc.narg(after_time_key)::time, sqlc.narg(after_captured_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY e.occurred_on DESC, k.time_key DESC, e.captured_at DESC, e.id DESC
LIMIT sqlc.arg(page_size);

-- ---- the uncategorised list and its count -----------------------------------
--
-- GET /expenses?uncategorised=true (#140 R8): ListExpenses with one more
-- predicate, `category_id IS NULL`, and everything else the same (the order, the
-- effective time and the keyset), so a cursor from either list resumes in the
-- other. A statement of its own and not a boolean argument on ListExpenses, on
-- purpose: the predicate has to be a literal in the text for the planner to
-- choose the partial index (household_id, occurred_on DESC, id DESC) WHERE
-- deleted_at IS NULL AND category_id IS NULL (expenses_household_uncategorised_idx),
-- which a prepared statement's generic plan, with the flag as a parameter, cannot
-- promise. Keep the two bodies in step.
-- name: ListUncategorisedExpenses :many
SELECT sqlc.embed(e), k.time_key::time AS time_key
FROM expenses e
JOIN households h ON h.id = e.household_id
CROSS JOIN LATERAL (
    SELECT CASE
        WHEN e.occurred_time IS NOT NULL
            THEN e.occurred_time - (h.day_starts_at - time '00:00')
        WHEN ((e.captured_at AT TIME ZONE sqlc.arg(time_zone)::text) - (h.day_starts_at - time '00:00'))::date = e.occurred_on
            THEN (e.captured_at AT TIME ZONE sqlc.arg(time_zone)::text)::time - (h.day_starts_at - time '00:00')
        ELSE time '00:00'
    END AS time_key
) k
WHERE e.household_id = sqlc.arg(household_id)
  AND e.deleted_at IS NULL
  AND e.category_id IS NULL
  AND e.occurred_on <= coalesce(sqlc.narg(after_occurred_on)::date, 'infinity'::date)
  AND (
        sqlc.narg(after_occurred_on)::date IS NULL
        OR e.occurred_on < sqlc.narg(after_occurred_on)::date
        OR (k.time_key, e.captured_at, e.id)
           < (sqlc.narg(after_time_key)::time, sqlc.narg(after_captured_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY e.occurred_on DESC, k.time_key DESC, e.captured_at DESC, e.id DESC
LIMIT sqlc.arg(page_size);

-- How many live Expenses of the Household have no Category: the figure behind
-- "Needs a category (N)", independent of the page and the filter. Derived on
-- every read (N5), never stored. Served by the same partial index.
-- name: CountUncategorisedExpenses :one
SELECT count(*) FROM expenses
WHERE household_id = sqlc.arg(household_id)
  AND deleted_at IS NULL
  AND category_id IS NULL;

-- ---- delete, the Recycle Bin and restore ----------------------------------
--
-- A soft delete: the row stays and the bin never empties (#140 R11). No
-- statement in this file hard-deletes, on purpose.

-- The delete is ONE statement, no transaction and no lock: nothing references
-- an Expense, so no guard applies and nothing needs protecting. Idempotent on
-- the caller's own Expense and keeping the FIRST deletion instant, as a
-- Pocket's: coalesce leaves an existing deleted_at alone, and updated_at moves
-- only when this statement is the one that deleted (a repeat changes no
-- column). A row that is another Household's or unknown is no row (0 rows
-- affected), and the caller answers NOT_FOUND; a deleted one of the caller's own
-- is a row, which is what makes a repeat a success. A concurrent PATCH holds
-- the row FOR NO KEY UPDATE; this UPDATE waits for it and then runs on the
-- row's new version, which is the delete the caller asked for.
-- name: SoftDeleteExpense :execrows
UPDATE expenses SET
    deleted_at = coalesce(deleted_at, now()),
    updated_at = CASE WHEN deleted_at IS NULL THEN now() ELSE updated_at END
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id);

-- The Recycle Bin (GET /expenses?deleted=true): only the Household's deleted
-- Expenses, deleted_at DESC then id DESC, keyset-paged. The order is declared,
-- and ends in id so a page boundary is exact over rows deleted in one instant.
-- deleted_at is a timestamptz and Postgres keeps microseconds, which is all the
-- cursor carries.
--
-- The keyset is one row comparison, (deleted_at, id) < (cursor), with an absent
-- cursor standing for the top of the order (infinity and the greatest uuid). It is
-- NOT written as `deleted_at < x OR id < y`, which looks equivalent and is not
-- cheap: the planner turns that OR into a BitmapOr that includes a scan of the
-- primary key on `id < y` alone, and reads every deleted row below the cursor
-- before it sorts them, a cost that grows with a bin that never empties. As a row
-- comparison the planner takes the bound on deleted_at from it, walks
-- expenses_household_bin_idx (household_id, deleted_at DESC) WHERE deleted_at IS
-- NOT NULL in order, and sorts only the ties on id (incremental sort); the plans
-- are in the PR. `deleted_at IS NOT NULL` is a literal in the text, so the planner
-- can prove the partial index's predicate. page_size is the page plus one.
-- name: ListDeletedExpenses :many
SELECT * FROM expenses
WHERE household_id = sqlc.arg(household_id)
  AND deleted_at IS NOT NULL
  AND (deleted_at, id) < (
        coalesce(sqlc.narg(after_deleted_at)::timestamptz, 'infinity'::timestamptz),
        coalesce(sqlc.narg(after_id)::uuid, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid)
  )
ORDER BY deleted_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- A restore is one transaction of statements in this order (#140 X10, X11):
-- GetExpenseForUpdate, then, only when the Expense is deleted,
-- LockLivePocketShared and LockLiveCategoryShared (the create's own
-- statements, the Pocket first), then RestoreExpense. There is no Household
-- lock: a restore reads no currency (the Expense already carries its own) and
-- changes no Pocket or Category, and the Household's row is only taken by a
-- writer that needs what it protects (the currency, the active-Pocket count).
--
-- The Expense row is locked first, as an edit's is, and nothing a removal holds
-- ever waits on an Expense row (a Pocket's or Category's delete only READS
-- expenses, in its EXISTS), so the order adds no cycle.

-- The Expense in ANY state, row-locked for the transaction, scoped by
-- household_id. This is the one read of a deleted Expense besides the bin, on
-- purpose: restoring a live one is the idempotent success and a deleted one is
-- what is being restored. No row is unknown or another Household's. FOR NO KEY
-- UPDATE conflicts with an edit's lock and another restore's, and with nothing
-- else an Expense row is read by (it has no foreign keys pointing at it).
-- name: GetExpenseForUpdate :one
SELECT * FROM expenses
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id)
FOR NO KEY UPDATE;

-- The Undo of SoftDeleteExpense: clears deleted_at and returns the row, every
-- other column as it was. Called only for a row GetExpenseForUpdate found
-- deleted, with its references locked, so no row here would be a statement that
-- disagrees with its own lock. updated_at is the database's clock and moves
-- because this statement did the work.
-- name: RestoreExpense :one
UPDATE expenses SET
    deleted_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NOT NULL
RETURNING *;

-- ---- the default Pocket ----------------------------------------------------

-- The Pocket capture offers first (#140 R3): that of the Household's most recent
-- live Expense by captured_at whose Pocket is active and live. Derived on read;
-- no row is no default. Served by (household_id, captured_at DESC) WHERE live.
-- name: LastUsedPocket :one
SELECT e.pocket_id
FROM expenses e
JOIN pockets p ON p.household_id = e.household_id AND p.id = e.pocket_id
WHERE e.household_id = sqlc.arg(household_id)
  AND e.deleted_at IS NULL
  AND p.active
  AND p.deleted_at IS NULL
ORDER BY e.captured_at DESC, e.id DESC
LIMIT 1;

-- ---- the guards ------------------------------------------------------------
--
-- Each is a SEPARATE statement, run after the protected row's lock was granted
-- and never folded into a write's WHERE (BOOTSTRAP.md §4: under READ COMMITTED
-- an UPDATE that waited for a lock does not re-run its subquery). Deleted rows
-- count: the Recycle Bin never empties, and a deleted Expense still labels its
-- Pocket and Category and is in the currency it was recorded in.

-- Does any Expense, live or deleted, reference the Pocket? POCKET_IN_USE.
-- Served by (household_id, pocket_id).
-- name: PocketHasExpenses :one
SELECT EXISTS (
    SELECT 1 FROM expenses
    WHERE household_id = sqlc.arg(household_id) AND pocket_id = sqlc.arg(pocket_id)
);

-- Does any Expense, live or deleted, reference the Category? CATEGORY_IN_USE.
-- Served by (household_id, category_id) WHERE category_id IS NOT NULL.
-- name: CategoryHasExpenses :one
SELECT EXISTS (
    SELECT 1 FROM expenses
    WHERE household_id = sqlc.arg(household_id) AND category_id = sqlc.arg(category_id)
);

-- Does the Household hold any Expense, live or deleted? CURRENCY_LOCKED.
-- Served by the leading household_id of any of the table's indexes.
-- name: HouseholdHasExpenses :one
SELECT EXISTS (
    SELECT 1 FROM expenses WHERE household_id = sqlc.arg(household_id)
);
