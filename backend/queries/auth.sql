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

-- Compare-and-swap, for a password change: the new hash is written only if the
-- stored one is still the hash the current password was verified against. Zero
-- rows means another change landed since, so two concurrent submissions of the
-- same form cannot both win, and the loser's session revocation never runs.
-- Not UpsertCredential, which would let both through.
-- name: SwapCredentialPasswordHash :execrows
UPDATE credentials
SET password_hash = sqlc.arg(new_password_hash), updated_at = now()
WHERE user_id = sqlc.arg(user_id) AND password_hash = sqlc.arg(verified_password_hash);

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
-- transaction as the credential write and before the caller's new session is
-- minted, so the change boots every other session, and the one that made the
-- request, before the new one exists — the "reset because compromised"
-- guarantee (BOOTSTRAP.md §5.1).
-- name: DeleteSessionsForUser :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= now();

-- Login backoff lives in the database, not process memory, so it survives a
-- restart and is shared by every process serving the instance (BOOTSTRAP.md
-- §5.1).
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
    -- than the cap. Backoff, never a hard lockout. The exponent stops one past
    -- the first doubling that reaches the cap: unclamped, the product
    -- overflows interval at failure 45 at 1s, the statement fails, the row
    -- stops updating and its key is never throttled again.
    backoff_until = now() + least(
        sqlc.arg(first_backoff)::interval * pow(2, least(
            login_attempts.failure_count,
            ceil(log(2, extract(epoch FROM sqlc.arg(max_backoff)::interval)
                / extract(epoch FROM sqlc.arg(first_backoff)::interval)))::int + 1
        )),
        sqlc.arg(max_backoff)::interval
    ),
    updated_at = now();

-- name: ClearLoginAttempts :exec
DELETE FROM login_attempts WHERE key = ANY(sqlc.arg(keys)::text[]);

-- Onboarding handshakes (BOOTSTRAP.md §5.1): the gap between registering a
-- credential and having an account. Instance-local auth state, so it
-- hard-deletes. id is the SHA-256 of the cookie's token, like sessions.id.

-- The expiry is the database's own now() plus the TTL, and every read below
-- tests it against the same clock (BOOTSTRAP.md §5.1: an instant the database
-- evaluates comes from the database).
-- name: CreateOnboardingHandshake :one
INSERT INTO onboarding_handshakes (id, email, user_display_name, locale, password_hash, expires_at)
VALUES (
    sqlc.arg(id), sqlc.arg(email), sqlc.arg(user_display_name), sqlc.arg(locale),
    sqlc.arg(password_hash), now() + sqlc.arg(ttl)::interval
)
RETURNING *;

-- A read, not a claim: it lets a request be refused (400, 403) without
-- spending the handshake. The commit's own claim is ClaimOnboardingHandshake.
-- name: GetLiveOnboardingHandshake :one
SELECT * FROM onboarding_handshakes
WHERE id = $1 AND expires_at > now();

-- The commit's claim. Run inside the founding transaction: a second concurrent
-- commit on the same handshake blocks on this row's lock until the first
-- commits, then finds it gone and gets no row, so only one of them can found.
-- name: ClaimOnboardingHandshake :one
DELETE FROM onboarding_handshakes
WHERE id = $1 AND expires_at > now()
RETURNING *;

-- name: DeleteExpiredOnboardingHandshakes :exec
DELETE FROM onboarding_handshakes WHERE expires_at <= now();

-- Founding. Ids are minted by the server, UUIDv7 (BOOTSTRAP.md §4): founding is
-- an online auth flow, not an offline-capable write. Defaults the schema owns
-- (currency, period start, time zone) are left to it.
-- name: CreateHousehold :one
INSERT INTO households (id, household_display_name)
VALUES ($1, $2)
RETURNING *;

-- name: CreateUser :one
INSERT INTO users (id, household_id, email, user_display_name, locale)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- Plain INSERT, not UpsertCredential: a founding never overwrites one.
-- name: CreateCredential :exec
INSERT INTO credentials (user_id, password_hash)
VALUES ($1, $2);
