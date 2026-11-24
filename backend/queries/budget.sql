-- Budgets (PRD S9, S12, S13). Tenant-scoped in SQL: every statement filters on
-- the authenticated User's household_id, never anything the request supplied
-- (BOOTSTRAP.md §4). year_month is the Budget Period's label, derived by the
-- period package from the User's Today; nothing here works out a date.

-- Whether the Period has been materialised. A request that finds it writes
-- nothing, so only the first request after a boundary pays for the copy.
-- name: BudgetExists :one
SELECT EXISTS (
    SELECT 1 FROM budgets
    WHERE household_id = sqlc.arg(household_id) AND year_month = sqlc.arg(year_month)
);

-- The materialisation marker. ON CONFLICT DO NOTHING, not a SELECT then an
-- INSERT: a row back means THIS request is the materialiser and copies; no row
-- means another request is or was, and this one reads what it committed. Two
-- concurrent first reads queue on the primary key's index entry, the second
-- resumes only once the first has committed or rolled back, and a rollback
-- makes the second the materialiser. The marker and its copy are one
-- transaction (see CopyMonthlyBudgetForward): a marker committed alone would be
-- a Period that looks materialised and is empty.
-- name: InsertBudget :one
INSERT INTO budgets (household_id, year_month)
VALUES (sqlc.arg(household_id), sqlc.arg(year_month))
ON CONFLICT (household_id, year_month) DO NOTHING
RETURNING year_month;

-- Where a Period being materialised copies from (PRD S12): the nearest EARLIER
-- Period that holds a Monthly Budget or an allocation, else the nearest LATER
-- one (reachable only after a period_start_day change re-labelled Today, #130
-- C3), else no row (the HAVING: an aggregate would otherwise answer one NULL),
-- which is the Household's first Period. A marker alone is
-- not a source, and a Period whose allocations were all cleared still is (its
-- latest state is "none"). The target is never its own source: the comparisons
-- are strict, and the marker was just created in this transaction.
--
-- With a category_id the question is the same for ONE Category (a restore carrying
-- a Category the Period's copy skipped while it was deleted, #130 C8): the nearest
-- earlier Period that holds an allocation row FOR IT, a Monthly Budget or another
-- Category's row not counting. The Period's copy may have come from a Period that
-- never held the Category (it was deleted there), and "as if it was never deleted"
-- is the Period that did.
--
-- This is the ONE statement that chooses a source, for the Monthly Budget and the
-- allocations alike, whole Period or one Category. A standing default (Q-17) or an
-- annual plan (Q-20) would be consulted here, first, and no stored row would change.
-- name: CopySource :one
SELECT coalesce(max(b.year_month) FILTER (WHERE b.year_month < sqlc.arg(year_month)),
                min(b.year_month) FILTER (WHERE b.year_month > sqlc.arg(year_month)))::text AS year_month
FROM budgets b
WHERE b.household_id = sqlc.arg(household_id) AND b.year_month <> sqlc.arg(year_month)
  AND CASE WHEN sqlc.narg(category_id)::uuid IS NULL
        THEN (EXISTS (SELECT 1 FROM monthly_budgets m WHERE m.household_id = b.household_id AND m.year_month = b.year_month)
           OR EXISTS (SELECT 1 FROM allocations x WHERE x.household_id = b.household_id AND x.year_month = b.year_month))
        ELSE EXISTS (SELECT 1 FROM allocations x WHERE x.household_id = b.household_id AND x.year_month = b.year_month
                       AND x.category_id = sqlc.narg(category_id)::uuid)
      END
HAVING count(*) > 0;

-- Carries the Monthly Budget's latest version from the source into the Period
-- being materialised, dated the Period's first day (PRD S12). Nothing when the
-- source holds none.
-- name: CopyMonthlyBudgetForward :execrows
INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget)
SELECT m.household_id, sqlc.arg(year_month), sqlc.arg(effective_on), m.monthly_budget
FROM monthly_budgets m
WHERE m.household_id = sqlc.arg(household_id) AND m.year_month = sqlc.arg(source)
ORDER BY m.effective_on DESC
LIMIT 1;

-- Carries every allocation's latest state from the source into the Period being
-- materialised, dated the Period's first day: only the non-null ones (a Category
-- whose latest row is "none" has nothing to carry), and only Categories that
-- are not deleted, inactive ones included, so deactivating changes no figure
-- (#130 C8). FOR SHARE on the Category rows it reads, so a delete (which takes
-- FOR UPDATE on the row) and this copy cannot both succeed against one Category.
--
-- With a category_id it carries that Category alone, which is what a restore does
-- (the source then comes from CopySource with the same category_id): the same
-- rule, so there is one place that decides what a Period starts with. A Category
-- that already has ANY row in the Period is left alone (nothing to carry: it was
-- deleted and restored within the Period, or the copy already took it), and ON
-- CONFLICT DO NOTHING on the natural key covers a copy and a restore that meet on
-- the same first-day row, so the second writes nothing and fails nothing.
-- name: CopyAllocationsForward :execrows
INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation)
SELECT c.household_id, sqlc.arg(year_month), sqlc.arg(effective_on), c.id, a.allocation
FROM (SELECT DISTINCT ON (s.category_id) s.category_id, s.allocation
      FROM allocations s
      WHERE s.household_id = sqlc.arg(household_id) AND s.year_month = sqlc.arg(source)
      ORDER BY s.category_id, s.effective_on DESC) a
JOIN categories c ON c.id = a.category_id AND c.household_id = sqlc.arg(household_id) AND c.deleted_at IS NULL
WHERE a.allocation IS NOT NULL
  AND (sqlc.narg(category_id)::uuid IS NULL OR c.id = sqlc.narg(category_id)::uuid)
  AND NOT EXISTS (SELECT 1 FROM allocations e
                  WHERE e.household_id = c.household_id AND e.year_month = sqlc.arg(year_month) AND e.category_id = c.id)
FOR SHARE OF c
ON CONFLICT (household_id, year_month, effective_on, category_id) DO NOTHING;

-- The allocations in force on a day, for the Categories that are live, active or
-- inactive, non-null only (a Category with none is absent). The day is the one
-- budget.InForceDay works out, passed in: the as-of rule is written once, in Go.
-- bucket rides along because the figures sum by it. The order follows the
-- Category list's, so the wire is stable.
-- name: AllocationsAsOf :many
SELECT c.id AS category_id, c.bucket, a.allocation
FROM (SELECT DISTINCT ON (s.category_id) s.category_id, s.allocation
      FROM allocations s
      WHERE s.household_id = sqlc.arg(household_id) AND s.year_month = sqlc.arg(year_month)
        AND s.effective_on <= sqlc.arg(as_of)
      ORDER BY s.category_id, s.effective_on DESC) a
JOIN categories c ON c.id = a.category_id AND c.household_id = sqlc.arg(household_id) AND c.deleted_at IS NULL
WHERE a.allocation IS NOT NULL
ORDER BY c.created_at, c.id;

-- One statement per field, keyed by the version's date and Category, so two
-- writers of different Categories both land and a second change on the same
-- Period Day overwrites the first. effective_on is the writer's Today. The
-- Category must be the Household's own and live, checked and share-locked in the
-- same statement: zero rows means it is not.
-- name: UpsertAllocation :execrows
INSERT INTO allocations (household_id, year_month, effective_on, category_id, allocation)
SELECT c.household_id, sqlc.arg(year_month), sqlc.arg(effective_on), c.id, sqlc.narg(allocation)
FROM categories c
WHERE c.id = sqlc.arg(category_id) AND c.household_id = sqlc.arg(household_id) AND c.deleted_at IS NULL
FOR SHARE OF c
ON CONFLICT (household_id, year_month, effective_on, category_id)
DO UPDATE SET allocation = EXCLUDED.allocation, updated_at = now();

-- Every version of the Period's Monthly Budget, oldest first. A Period holds at
-- most one per day it was edited, so the list is short, and the as-of rule
-- (budget.MonthlyBudgetAsOf) is one Go function over it rather than a second copy in SQL.
-- One statement, so the version in force and the first version, which the rule
-- needs, come from the same snapshot.
-- name: ListMonthlyBudgetVersions :many
SELECT effective_on, monthly_budget
FROM monthly_budgets
WHERE household_id = sqlc.arg(household_id) AND year_month = sqlc.arg(year_month)
ORDER BY effective_on;

-- One statement per field, keyed by the version's date, so two writers of
-- different fields both land (BOOTSTRAP.md §4) and a second change on the same
-- Period Day overwrites the first. effective_on is the writer's Today.
-- name: UpsertMonthlyBudget :exec
INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget)
VALUES (sqlc.arg(household_id), sqlc.arg(year_month), sqlc.arg(effective_on), sqlc.arg(monthly_budget))
ON CONFLICT (household_id, year_month, effective_on)
DO UPDATE SET monthly_budget = EXCLUDED.monthly_budget, updated_at = now();
