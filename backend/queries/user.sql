-- User settings (PRD S4). A User changes only their own row, so every statement
-- here filters on BOTH ids in SQL: users is Household-scoped, and a query that
-- filtered on household_id alone would rewrite every member's row once a
-- Household has more than one (BOOTSTRAP.md §4). Both ids come from the
-- authenticated User, never from the request.

-- Merge semantics (BOOTSTRAP.md §4): a NULL argument leaves its column alone,
-- in one statement, so two writers of different fields both land. None of the
-- three columns is nullable, so coalesce cannot be mistaken for "clear".
--
-- updated_at is set here because the baseline has no trigger, and from the
-- database's own clock (BOOTSTRAP.md §5.1).
-- name: UpdateUser :one
UPDATE users SET
    user_display_name = coalesce(sqlc.narg(user_display_name), user_display_name),
    locale            = coalesce(sqlc.narg(locale), locale),
    time_zone         = coalesce(sqlc.narg(time_zone), time_zone),
    updated_at        = now()
WHERE id = sqlc.arg(user_id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL
RETURNING *;

-- The sign-in handle. The unique index users_email_idx (lower(email), live rows
-- only) is the race-free answer to "is it taken": a violation is EMAIL_TAKEN,
-- and there is no lookup beforehand for it to race (BOOTSTRAP.md §5.1.1).
-- name: UpdateUserEmail :one
UPDATE users SET
    email      = sqlc.arg(email),
    updated_at = now()
WHERE id = sqlc.arg(user_id) AND household_id = sqlc.arg(household_id) AND deleted_at IS NULL
RETURNING *;
