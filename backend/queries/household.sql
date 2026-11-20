-- Household settings (PRD S3). Tenant-scoped in SQL: the id is the
-- authenticated User's household_id, never anything the request supplied
-- (BOOTSTRAP.md §4).

-- Merge semantics (BOOTSTRAP.md §4): a NULL argument leaves its column alone,
-- in one statement, so two writers of different fields both land. A
-- read-modify-write would let the second overwrite the first's field. None of
-- the three columns is nullable, so coalesce cannot be mistaken for "clear".
--
-- updated_at is set here because the baseline has no trigger, and from the
-- database's own clock (BOOTSTRAP.md §5.1).
-- name: UpdateHousehold :one
UPDATE households SET
    household_display_name = coalesce(sqlc.narg(household_display_name), household_display_name),
    reporting_currency     = coalesce(sqlc.narg(reporting_currency), reporting_currency),
    period_start_day       = coalesce(sqlc.narg(period_start_day), period_start_day),
    updated_at             = now()
WHERE id = sqlc.arg(household_id) AND deleted_at IS NULL
RETURNING *;
