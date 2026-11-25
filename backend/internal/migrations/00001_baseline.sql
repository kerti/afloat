-- Baseline: identity and authentication (BOOTSTRAP.md §11 step 3), plus the
-- domain tables as their slices land in step 9, edited in place until the
-- freeze (BOOTSTRAP.md §7.1).
-- Households, Users, credentials, sessions, onboarding handshakes, invitations,
-- login backoff, Pockets, Categories, Budgets, Expenses. A dev database already
-- at version 1 does not gain a table or a constraint added here, so run
-- `make db-reset` after pulling one.
--
-- Canonical and hand-written: the binary embeds it, goose applies it, and sqlc
-- reads its Up section as the schema (BOOTSTRAP.md §7).
--
-- Conventions this file is bound by (BOOTSTRAP.md §4):
--   * Primary keys are UUIDv7 and CLIENT-generated, so no column carries a
--     DEFAULT. A server-side gen_random_uuid() would be a v4 and would let a
--     missing client id pass silently.
--   * Money is DECIMAL(20,4).
--   * Soft delete on Household-scoped domain data. Instance-local auth state
--     (credentials, sessions, onboarding_handshakes, invitations,
--     login_attempts) hard-deletes:
--     revocation IS the row delete. A version log no operation deletes from
--     (budgets, monthly_budgets, allocations) has no deleted_at: a column nothing sets is
--     a filter every read must remember.
--   * Rows only the server creates, never addressed by id on the wire (the
--     Budget tables, allocations included), have composite natural keys and no id column. The first
--     rule above is about rows a client creates.

-- +goose Up
-- ---------------------------------------------------------------------------
-- households
-- ---------------------------------------------------------------------------

CREATE TABLE households (
    id                      uuid PRIMARY KEY,
    household_display_name  text NOT NULL,
    -- PRD S3 defaults. They are declared here as well as in the application so
    -- a row inserted by a migration or a fixture gets the same Household as one
    -- inserted by onboarding.
    reporting_currency      text NOT NULL DEFAULT 'IDR',
    period_start_day        integer NOT NULL DEFAULT 1,
    -- Local wall-clock time a Period Day rolls over. Not timestamptz: it is a
    -- recurring time of day, resolved against each User's time_zone server-side
    -- (BOOTSTRAP.md §4 — the frontend never derives a Period Day).
    day_starts_at           time NOT NULL DEFAULT '04:00',
    -- Nullable: the Household may never supply one (PRD S5). The sole field
    -- anything outside Afloat may write (VISION.md §5).
    expected_monthly_income numeric(20,4),
    allowance_mode          text NOT NULL DEFAULT 'adaptive',
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    deleted_at              timestamptz,

    CONSTRAINT households_household_display_name_not_blank
        CHECK (btrim(household_display_name) <> ''),
    CONSTRAINT households_reporting_currency_iso4217
        CHECK (reporting_currency ~ '^[A-Z]{3}$'),
    -- 28 is the highest day that exists in every month; 29-31 would silently
    -- skip or clamp a Period in February.
    CONSTRAINT households_period_start_day_range
        CHECK (period_start_day BETWEEN 1 AND 28),
    CONSTRAINT households_allowance_mode_known
        CHECK (allowance_mode IN ('adaptive', 'fixed')),
    -- Zero income is a real answer; negative is not.
    CONSTRAINT households_expected_monthly_income_non_negative
        CHECK (expected_monthly_income IS NULL OR expected_monthly_income >= 0)
);

-- ---------------------------------------------------------------------------
-- users
-- ---------------------------------------------------------------------------

CREATE TABLE users (
    id                uuid PRIMARY KEY,
    -- A User belongs to exactly one Household and the binding is irreversible
    -- (PRD S1). All members have equal access — there is no role column, and
    -- deliberately no founder flag: PRD S2 has no permission tiers to hang on it.
    household_id      uuid NOT NULL REFERENCES households (id),
    email             text NOT NULL,
    user_display_name text NOT NULL,
    locale            text NOT NULL DEFAULT 'en-GB',
    time_zone         text NOT NULL DEFAULT 'Asia/Jakarta',
    -- Light or dark, per User (PRD S4); 'system' follows the device. Not the
    -- palette (CONTEXT: Appearance).
    appearance        text NOT NULL DEFAULT 'system',
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz,

    CONSTRAINT users_user_display_name_not_blank
        CHECK (btrim(user_display_name) <> ''),
    -- Both locales are real from day one (PRD N12a). Widening this list is what
    -- adding a third language costs in the database.
    CONSTRAINT users_locale_known
        CHECK (locale IN ('en-GB', 'id-ID')),
    CONSTRAINT users_appearance_known
        CHECK (appearance IN ('system', 'light', 'dark'))
);

-- Soft-delete-aware: a deleted User's address frees up for reuse, and two
-- deleted rows may share one. Email is the handle and the invitation-match key,
-- never the identity — users.id is.
CREATE UNIQUE INDEX users_email_idx
    ON users (lower(email))
    WHERE deleted_at IS NULL;

CREATE INDEX users_household_id_idx
    ON users (household_id)
    WHERE deleted_at IS NULL;

-- The target of expenses' composite foreign key (owner_user_id): an Expense
-- naming another Household's User as its owner is impossible by construction.
-- Added with the Expenses slice (#140) by an in-place edit (BOOTSTRAP.md §7.1).
ALTER TABLE users
    ADD CONSTRAINT users_household_id_id_key UNIQUE (household_id, id);

-- ---------------------------------------------------------------------------
-- credentials
-- ---------------------------------------------------------------------------

-- Users carry no credential column (BOOTSTRAP.md §5). Keeping the hash in its
-- own table is what makes Google OAuth later an additive provider rather than a
-- migration of the user model, and it makes "never serialise the secret"
-- structural rather than a per-column thing to remember.
--
-- A User with no row here is DORMANT: present, owns data, cannot yet
-- authenticate. That is a legitimate state (an invited member who has not set a
-- password), so "reachable" is an application invariant, not a CHECK.
CREATE TABLE credentials (
    user_id       uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- Argon2id PHC string: $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>.
    -- Salt and cost parameters travel inside it, so they retune with no
    -- migration and old hashes keep verifying against their own recorded cost
    -- (BOOTSTRAP.md §5.1).
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- sessions
-- ---------------------------------------------------------------------------

CREATE TABLE sessions (
    -- SHA-256 hex of the bearer token, never the token. The cookie carries the
    -- plaintext; a database leak yields nothing usable. Callers hash before
    -- every read and write.
    id           text PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Read, not decorative: the absolute lifetime is checked against this, so a
    -- sliding window cannot keep a stolen cookie alive forever (BOOTSTRAP.md
    -- §5.1).
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    user_agent   text
);

-- Supports revoke-all-for-user, which a password reset runs before minting the
-- replacement session.
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- Supports the expired-session sweep.
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- ---------------------------------------------------------------------------
-- onboarding_handshakes
-- ---------------------------------------------------------------------------

-- The gap between registering a credential and having an account (BOOTSTRAP.md
-- §5.1). Registration validates and hashes, then parks the would-be credential
-- here; the User, the Household and the credentials row are created together
-- when the person commits at the gate, in one transaction that claims this row
-- with DELETE ... RETURNING. So there is no household_id: nothing exists yet to
-- point at, and a later join choice will add its own columns, not reshape these.
--
-- Instance-local auth state, so it hard-deletes like sessions. Liveness is
-- decided by expires_at against the database's own clock, never the app's.
CREATE TABLE onboarding_handshakes (
    -- SHA-256 hex of the cookie's token, like sessions.id: the row carries a
    -- password hash, so a database leak must not yield a presentable token.
    id                text PRIMARY KEY,
    -- Already trimmed and lower-cased by the registration handler.
    email             text NOT NULL,
    user_display_name text NOT NULL,
    locale            text NOT NULL,
    -- Argon2id PHC string. Nullable for the day a Google identity opens a
    -- handshake with no password to hash.
    password_hash     text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    expires_at        timestamptz NOT NULL,

    CONSTRAINT onboarding_handshakes_locale_known
        CHECK (locale IN ('en-GB', 'id-ID'))
);

-- Supports the expired-handshake sweep.
CREATE INDEX onboarding_handshakes_expires_at_idx ON onboarding_handshakes (expires_at);

-- ---------------------------------------------------------------------------
-- invitations
-- ---------------------------------------------------------------------------

-- A one-time link generated and shown in the UI; no email in MVP (PRD S2b).
-- The token model is nonetheless built as though email will deliver it later,
-- so nothing here assumes the founder reads it off a screen.
-- Both foreign keys CASCADE, like credentials and sessions and for the same
-- reason: this is instance-local auth state, so it should never be what blocks
-- a hard delete. Users are soft-deleted in normal operation, so a DELETE here is
-- always something deliberate — test teardown, or an erasure path if Q-14 is
-- ever answered — and a pending invitation is not worth wedging either.
CREATE TABLE invitations (
    id            uuid PRIMARY KEY,
    household_id  uuid NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    -- Checked against the accepting identity, so forwarding the link to someone
    -- else does not let them join the Household.
    invited_email text NOT NULL,
    -- SHA-256 hex of the link token, for the same reason sessions.id is hashed.
    token_hash    text NOT NULL UNIQUE,
    created_by    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    -- Non-null means consumed. Single-use.
    accepted_at   timestamptz
);

CREATE INDEX invitations_household_id_idx ON invitations (household_id);

-- ---------------------------------------------------------------------------
-- login_attempts
-- ---------------------------------------------------------------------------

-- Login backoff state, in the database rather than process memory
-- (BOOTSTRAP.md §5.1): a table survives a restart, and is shared by every
-- process serving the instance.
CREATE TABLE login_attempts (
    -- Namespaced key: 'ip:<addr>' or 'email:<lowercased address>'. One table for
    -- both, because the limiter treats them identically and a request checks the
    -- pair in a single read.
    key           text PRIMARY KEY,
    failure_count integer NOT NULL DEFAULT 0,
    -- Null means no active backoff. Backoff, never a hard lockout: a lockout on
    -- a self-hosted household app locks the household out of its own data.
    backoff_until timestamptz,
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT login_attempts_failure_count_non_negative
        CHECK (failure_count >= 0)
);

-- Supports the sweep that drops entries whose backoff has elapsed.
CREATE INDEX login_attempts_backoff_until_idx
    ON login_attempts (backoff_until)
    WHERE backoff_until IS NOT NULL;

-- ---------------------------------------------------------------------------
-- pockets
-- ---------------------------------------------------------------------------

-- A practical container the Household spends from (PRD S10, CONTEXT.md): a
-- name, and no balance, currency or kind (PRD S11). Belongs to the Household,
-- never to a User (PRD S2).
CREATE TABLE pockets (
    id                  uuid PRIMARY KEY,
    household_id        uuid NOT NULL REFERENCES households (id),
    pocket_display_name text NOT NULL,
    -- Deactivated is not deleted: an inactive Pocket stays in the list, keeps
    -- labelling its Expenses and keeps its name. deleted_at is the delete.
    active              boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    deleted_at          timestamptz,

    CONSTRAINT pockets_pocket_display_name_not_blank
        CHECK (btrim(pocket_display_name) <> '')
);

-- Soft-delete-aware, like users_email_idx: a deleted Pocket releases its name,
-- an inactive one keeps it. Also serves the list's household_id and live
-- filter; a Household holds a handful, so no second index.
CREATE UNIQUE INDEX pockets_household_name_idx
    ON pockets (household_id, lower(pocket_display_name))
    WHERE deleted_at IS NULL;

-- The target of expenses' composite foreign key, as categories has it: an
-- Expense naming another Household's Pocket is impossible by construction.
-- A second unique index on the id, which ON CONFLICT (id) does not arbitrate:
-- a concurrent replay of one id can trip it (BOOTSTRAP.md §4).
ALTER TABLE pockets
    ADD CONSTRAINT pockets_household_id_id_key UNIQUE (household_id, id);

-- ---------------------------------------------------------------------------
-- categories
-- ---------------------------------------------------------------------------

-- A household-defined bucket an Expense is classified into (PRD S8,
-- CONTEXT.md): a name and a bucket, and no allocation: what is planned for a
-- Category is a value of each Budget Period (allocations below), not a column
-- here. Belongs to the Household, never to a User (PRD S2).
CREATE TABLE categories (
    id                    uuid PRIMARY KEY,
    household_id          uuid NOT NULL REFERENCES households (id),
    category_display_name text NOT NULL,
    -- Fixed at creation (#130 C7): a flip would move the Category's allocation
    -- between the Commitments Budget and Unallocated in every Period at once.
    bucket                text NOT NULL,
    -- Deactivated is not deleted: an inactive Category stays in the list, keeps
    -- labelling its Expenses, keeps its name and keeps its allocation.
    active                boolean NOT NULL DEFAULT true,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    deleted_at            timestamptz,

    CONSTRAINT categories_category_display_name_not_blank
        CHECK (btrim(category_display_name) <> ''),
    CONSTRAINT categories_bucket_known
        CHECK (bucket IN ('daily', 'commitment')),
    -- The target of allocations' composite foreign key: an allocation naming
    -- another Household's Category is impossible by construction.
    CONSTRAINT categories_household_id_id_key UNIQUE (household_id, id)
);

-- Soft-delete-aware, like pockets_household_name_idx: a deleted Category
-- releases its name, an inactive one keeps it, and the name is unique across
-- both buckets (#130 C7).
CREATE UNIQUE INDEX categories_household_name_idx
    ON categories (household_id, lower(category_display_name))
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- budgets, monthly_budgets, allocations
-- ---------------------------------------------------------------------------

-- One Budget per Household per Budget Period (CONTEXT, PRD S12, S13). The row
-- is the materialisation marker: it commits in the same transaction as whatever
-- is copied into the Period, so a Period that looks materialised never is
-- empty. year_month is the YYYY-MM of the calendar month in which the Period
-- starts; the window is derived from households.period_start_day each time and
-- stored nowhere (#113 D1). The server creates these; no request names one, so
-- there is no id and no deleted_at (BOOTSTRAP.md §4).
CREATE TABLE budgets (
    household_id uuid NOT NULL REFERENCES households (id),
    year_month   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (household_id, year_month),
    CONSTRAINT budgets_year_month_shape
        CHECK (year_month ~ '^[0-9]{4}-(0[1-9]|1[0-2])$')
);

-- The Monthly Budget, as a version log within its Period (CONTEXT §Mid-period
-- Budget changes). The value on a day is the row with the greatest effective_on
-- on or before it. A write is an upsert on the key, so a second change on the
-- same Period Day overwrites the first. No currency column: the amount is in the
-- Household's reporting_currency, as expected_monthly_income is (PRD N3).
CREATE TABLE monthly_budgets (
    household_id   uuid NOT NULL,
    year_month     text NOT NULL,
    effective_on   date NOT NULL,
    monthly_budget numeric(20,4) NOT NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (household_id, year_month, effective_on),
    FOREIGN KEY (household_id, year_month) REFERENCES budgets (household_id, year_month),
    -- A plan is not a refund: zero is a real answer, negative is not.
    CONSTRAINT monthly_budgets_monthly_budget_non_negative
        CHECK (monthly_budget >= 0)
);

-- What a Category is planned at, as a version log within its Period, like
-- monthly_budgets: the value on a day is the latest row on or before it, per
-- Category, and a NULL allocation is "none from that day". No currency (PRD N3)
-- and no deleted_at (nothing deletes one). The composite foreign key onto
-- categories is what keeps a Household's allocations on its own Categories.
CREATE TABLE allocations (
    household_id uuid NOT NULL,
    year_month   text NOT NULL,
    effective_on date NOT NULL,
    category_id  uuid NOT NULL,
    allocation   numeric(20,4),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (household_id, year_month, effective_on, category_id),
    FOREIGN KEY (household_id, year_month) REFERENCES budgets (household_id, year_month),
    FOREIGN KEY (household_id, category_id) REFERENCES categories (household_id, id),
    CONSTRAINT allocations_allocation_non_negative
        CHECK (allocation IS NULL OR allocation >= 0)
);

-- ---------------------------------------------------------------------------
-- expenses
-- ---------------------------------------------------------------------------

-- What the Household spent (PRD E1 to E10, CONTEXT.md). Client-generated id, as
-- every row a client creates (BOOTSTRAP.md §4). Nothing here is derived: no
-- Period label, no total, no usage counter (PRD N5, CLAUDE.md rule 6).
--
-- occurred_on is the Period Day the Expense belongs to and is stored as a date:
-- period_start_day re-cuts every window and rewrites no row. occurred_time is a
-- wall-clock time with no zone (R6, #140), optional. captured_at is the
-- instant the server stamped, never sent by a client.
CREATE TABLE expenses (
    id            uuid PRIMARY KEY,
    household_id  uuid NOT NULL REFERENCES households (id),
    occurred_on   date NOT NULL,
    occurred_time time,
    captured_at   timestamptz NOT NULL DEFAULT now(),
    -- Signed: a negative amount is a refund with no recorded original (PRD E7,
    -- Q-07). Zero is not an Expense.
    amount        numeric(20,4) NOT NULL,
    -- Stamped by the server from the Household's reporting_currency at the
    -- create and never changed (PRD N3).
    currency      text NOT NULL,
    -- Every Expense has a Pocket (#140 R1); a Category is optional (PRD E4).
    pocket_id     uuid NOT NULL,
    category_id   uuid,
    description   text,
    -- Stored and on the wire, with no UI until a second member exists (#140 R6).
    ownership     text NOT NULL DEFAULT 'joint',
    owner_user_id uuid,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz,

    -- Composite foreign keys, so a reference to another Household's row is
    -- impossible by construction. No ON DELETE: no row is ever hard-deleted,
    -- and a CASCADE would be a trap (#121 P8.1). MATCH SIMPLE leaves a null
    -- category_id or owner_user_id unchecked, which is the intent.
    FOREIGN KEY (household_id, pocket_id)     REFERENCES pockets    (household_id, id),
    FOREIGN KEY (household_id, category_id)   REFERENCES categories (household_id, id),
    FOREIGN KEY (household_id, owner_user_id) REFERENCES users      (household_id, id),
    CONSTRAINT expenses_amount_nonzero
        CHECK (amount <> 0),
    CONSTRAINT expenses_currency_iso4217
        CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT expenses_description_not_blank
        CHECK (description IS NULL OR btrim(description) <> ''),
    CONSTRAINT expenses_ownership_known
        CHECK (ownership IN ('joint', 'sole_owner')),
    -- Balances' shape: a sole owner names a User, and a joint Expense names none.
    CONSTRAINT expenses_owner_matches_ownership
        CHECK ((ownership = 'sole_owner') = (owner_user_id IS NOT NULL))
);

-- The seven indexes of #140 R7, all here so the later slices (Correct an
-- Expense, the Recycle Bin) need no second db-reset. The list's effective time
-- depends on mutable settings, so it is not indexed: the date is, and Postgres
-- orders within a day. Every query on this table ships its plan in its PR
-- (BOOTSTRAP.md §4).

-- The live list, and later the trajectory's Period-window sums.
CREATE INDEX expenses_household_live_idx
    ON expenses (household_id, occurred_on DESC, captured_at DESC, id DESC)
    WHERE deleted_at IS NULL;

-- The uncategorised filter and count (#140 R8).
CREATE INDEX expenses_household_uncategorised_idx
    ON expenses (household_id, occurred_on DESC, id DESC)
    WHERE deleted_at IS NULL AND category_id IS NULL;

-- The last-used Pocket (#140 R3).
CREATE INDEX expenses_household_captured_idx
    ON expenses (household_id, captured_at DESC)
    WHERE deleted_at IS NULL;

-- The Recycle Bin (#140 R11).
CREATE INDEX expenses_household_bin_idx
    ON expenses (household_id, deleted_at DESC)
    WHERE deleted_at IS NOT NULL;

-- POCKET_IN_USE and LAST_POCKET's guards, which count live and deleted rows,
-- and the composite foreign key.
CREATE INDEX expenses_household_pocket_idx
    ON expenses (household_id, pocket_id);

-- CATEGORY_IN_USE's guard and the composite foreign key.
CREATE INDEX expenses_household_category_idx
    ON expenses (household_id, category_id)
    WHERE category_id IS NOT NULL;

-- The composite foreign key to users.
CREATE INDEX expenses_household_owner_idx
    ON expenses (household_id, owner_user_id)
    WHERE owner_user_id IS NOT NULL;

-- +goose Down
-- Reverse order of creation, so foreign keys drop before their targets.

DROP TABLE IF EXISTS expenses;
DROP TABLE IF EXISTS allocations;
DROP TABLE IF EXISTS monthly_budgets;
DROP TABLE IF EXISTS budgets;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS pockets;
DROP TABLE IF EXISTS login_attempts;
DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS onboarding_handshakes;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS households;
