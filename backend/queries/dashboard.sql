-- The dashboard's reads (#155). Tenant-scoped in SQL, as every statement is:
-- household_id from the authenticated User, never anything the request
-- supplied (BOOTSTRAP.md §4). Both run inside the dashboard's one snapshot.

-- The Period's spend, one statement (#155 T2.4): every live Expense whose
-- occurred_on is in the window, grouped by Period Day, Category and Pocket, so
-- Go can fold one result into every figure that reads Expenses and they cannot
-- disagree about one Expense. Only occurred_on decides the day and the Period;
-- captured_at is never read (CLAUDE.md rule 5). The window is inclusive at both
-- ends, so an Expense dated after the Period's last day is in no figure of it.
--
-- A refund is a negative amount and sums as one. No currency is read or
-- converted: every Expense is stamped with the Household's reporting_currency
-- and the currency lock keeps it so (an invariant the lock provides, not one this
-- statement checks).
--
-- bucket is 'daily' for an Expense with no Category (CONTEXT: an uncategorised
-- Expense is Daily), hence the LEFT JOIN. Neither join filters deleted_at on
-- the Category or Pocket: a delete is refused while an Expense references one
-- (CATEGORY_IN_USE, POCKET_IN_USE), so there is none to drop, and a filter that
-- ever did drop one would turn a Commitment's Expense into a Daily one without
-- a sound. Inactive Categories and Pockets count; active rides along for the
-- breakdowns. Both joins are on the composite key the foreign keys use.
-- name: PeriodSpend :many
SELECT e.occurred_on,
       e.category_id,
       e.pocket_id,
       coalesce(c.bucket, 'daily')::text AS bucket,
       c.category_display_name,
       c.active AS category_active,
       p.pocket_display_name,
       p.active AS pocket_active,
       sum(e.amount)::numeric AS amount
FROM expenses e
LEFT JOIN categories c ON c.household_id = e.household_id AND c.id = e.category_id
JOIN pockets p ON p.household_id = e.household_id AND p.id = e.pocket_id
WHERE e.household_id = sqlc.arg(household_id)
  AND e.deleted_at IS NULL
  AND e.occurred_on BETWEEN sqlc.arg(starts_on) AND sqlc.arg(ends_on)
GROUP BY e.occurred_on, e.category_id, e.pocket_id,
         c.bucket, c.category_display_name, c.active,
         p.pocket_display_name, p.active
ORDER BY e.occurred_on, e.category_id NULLS FIRST, e.pocket_id;

-- The days of the Period on which some allocation took effect: the primary
-- key's prefix (household_id, year_month, effective_on), read as a distinct
-- scan. The dashboard keeps those after the Period's first day and on or before
-- Today, asks AllocationsAsOf once for each, and lets equal neighbouring pools
-- merge, so a Daily Category's change (the pool is unchanged) is no kink. A
-- deleted Category's days are listed too and are harmless for the same reason:
-- AllocationsAsOf leaves its allocation out, so the pool does not move.
-- name: ListAllocationChangeDays :many
SELECT DISTINCT effective_on
FROM allocations
WHERE household_id = sqlc.arg(household_id) AND year_month = sqlc.arg(year_month)
ORDER BY effective_on;
