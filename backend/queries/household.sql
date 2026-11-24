-- Household settings (PRD S3). Tenant-scoped in SQL: the id is the
-- authenticated User's household_id, never anything the request supplied
-- (BOOTSTRAP.md §4).

-- Merge semantics (BOOTSTRAP.md §4): a NULL argument leaves its column alone,
-- in one statement, so two writers of different fields both land. A
-- read-modify-write would let the second overwrite the first's field. None of
-- the first three columns is nullable, so coalesce cannot be mistaken for "clear".
--
-- A column that null must clear (expected_monthly_income, PRD S7) cannot use
-- coalesce, which reads a NULL argument as "leave alone". It takes an explicit
-- set flag beside its value: the handler sets the flag when the property was
-- sent, and the value is then NULL for a clear. Same one statement, same
-- last-write-wins per field; the pattern for every later clearable column.
--
-- updated_at is set here because the baseline has no trigger, and from the
-- database's own clock (BOOTSTRAP.md §5.1).
-- name: UpdateHousehold :one
UPDATE households SET
    household_display_name = coalesce(sqlc.narg(household_display_name), household_display_name),
    reporting_currency     = coalesce(sqlc.narg(reporting_currency), reporting_currency),
    period_start_day       = coalesce(sqlc.narg(period_start_day), period_start_day),
    expected_monthly_income = CASE WHEN sqlc.arg(set_expected_monthly_income)::boolean
                                   THEN sqlc.narg(expected_monthly_income)::numeric(20,4)
                                   ELSE expected_monthly_income END,
    updated_at             = now()
WHERE id = sqlc.arg(household_id) AND deleted_at IS NULL
RETURNING *;
