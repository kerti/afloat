-- Authentication queries. Instance-local auth state hard-deletes: revocation IS
-- the row delete (BOOTSTRAP.md §4).

-- Email is the handle, never the identity. Matched case-insensitively against
-- the same expression the unique index uses, so a lookup cannot disagree with
-- what the index considers a duplicate. Soft-deleted Users are invisible here.
-- name: GetUserByEmail :one
SELECT * FROM users
WHERE lower(email) = lower(sqlc.arg(email)) AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetHouseholdByID :one
SELECT * FROM households
WHERE id = $1 AND deleted_at IS NULL;

-- A User with no row here is DORMANT: present, owns data, cannot yet
-- authenticate. A legitimate state, so this returning no rows is not an error.
-- name: GetCredentialByUserID :one
SELECT * FROM credentials WHERE user_id = $1;

-- name: UpsertCredential :exec
INSERT INTO credentials (user_id, password_hash)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET password_hash = EXCLUDED.password_hash, updated_at = now();

-- sessions.id is the SHA-256 of the bearer token, never the token. Callers hash
-- before every read and write.
--
-- Both lifetimes are enforced here rather than in Go, so a caller cannot forget
-- one: expires_at is the sliding window, created_at the absolute cap that stops
-- a stolen cookie living forever on repeated use (BOOTSTRAP.md §5.1).
-- name: GetLiveSession :one
SELECT * FROM sessions
WHERE id = $1
  AND expires_at > now()
  AND created_at > now() - sqlc.arg(max_lifetime)::interval;

-- name: CreateSession :one
INSERT INTO sessions (id, user_id, expires_at, user_agent)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = now(), expires_at = $2
WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;

-- Revoke every session a User holds. Run on a password change, inside the same
-- transaction, so the change boots any other session before the new one is
-- minted — the "reset because compromised" guarantee.
-- name: DeleteSessionsForUser :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= now();

-- Login backoff lives in the database, not process memory, because there are
-- two backends and an in-memory limiter would diverge where contract
-- conformance cannot see it (BOOTSTRAP.md §5.1).
--
-- Returns the remaining seconds, computed by the database's own clock, not a
-- raw timestamp for Go to subtract against its own clock: any instant the
-- database also evaluates (here, backoff_until > now()) comes from the
-- database (BOOTSTRAP.md §5.1, #25). Coalesced to 0 so "no active backoff"
-- (the aggregate over zero matching rows) is a plain zero, not a NULL scan.
-- name: GetLoginBackoff :one
SELECT coalesce(extract(epoch FROM (max(backoff_until) - now())), 0)::float8 AS remaining_seconds
FROM login_attempts
WHERE key = ANY(sqlc.arg(keys)::text[]) AND backoff_until > now();

-- name: RecordLoginFailure :exec
INSERT INTO login_attempts (key, failure_count, backoff_until)
VALUES ($1, 1, now() + sqlc.arg(first_backoff)::interval)
ON CONFLICT (key) DO UPDATE SET
    failure_count = login_attempts.failure_count + 1,
    -- Exponential, capped: 2^n seconds from the first failure, never longer
    -- than the cap. Backoff, never a hard lockout.
    backoff_until = now() + least(
        sqlc.arg(first_backoff)::interval * pow(2, login_attempts.failure_count),
        sqlc.arg(max_backoff)::interval
    ),
    updated_at = now();

-- name: ClearLoginAttempts :exec
DELETE FROM login_attempts WHERE key = ANY(sqlc.arg(keys)::text[]);
