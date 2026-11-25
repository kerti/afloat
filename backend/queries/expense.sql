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
