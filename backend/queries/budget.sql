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

-- Carries the Monthly Budget into a Period being materialised (PRD S12): the
-- latest version of the nearest EARLIER Period that has one, else of the nearest
-- LATER one (reachable only after a period_start_day change re-labelled Today,
-- #130 C3), else nothing, which is the Household's first Period. The copy is
-- dated the Period's first day. The target is never its own source: the
-- comparisons are strict, and the marker was just created in this transaction.
--
-- The source choice is this one statement. A standing default (Q-17) would be
-- consulted here, first, and no stored row would change.
-- name: CopyMonthlyBudgetForward :execrows
INSERT INTO monthly_budgets (household_id, year_month, effective_on, monthly_budget)
SELECT m.household_id, sqlc.arg(year_month), sqlc.arg(effective_on), m.monthly_budget
FROM monthly_budgets m
WHERE m.household_id = sqlc.arg(household_id)
  AND m.year_month = coalesce(
        (SELECT max(e.year_month) FROM monthly_budgets e
          WHERE e.household_id = sqlc.arg(household_id) AND e.year_month < sqlc.arg(year_month)),
        (SELECT min(l.year_month) FROM monthly_budgets l
          WHERE l.household_id = sqlc.arg(household_id) AND l.year_month > sqlc.arg(year_month)))
ORDER BY m.effective_on DESC
LIMIT 1;

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
