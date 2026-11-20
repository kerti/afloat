-- The rows every case starts from. The runner truncates
-- every Afloat table and applies this file before each case, so no case
-- depends on what ran before it.
--
-- Fixed UUIDv7s rather than generated ones, because /me echoes them and a case
-- asserts its body by value. Column defaults are left to the schema - a
-- Household's reporting_currency, a User's locale - so /me also pins that
-- the backend reads the defaults as the schema sets them.
--
-- Every seeded password is `conformance-correct-horse`. The hash is Argon2id
-- at the §5.1 cost (m=19456, t=2, p=1), minted once by the backend's
-- HashPassword, which verifies a PHC string by the parameters inside it.

INSERT INTO households (id, household_display_name) VALUES
    ('0192f0a0-0000-7000-8000-000000000001', 'Conformance Household');

INSERT INTO users (id, household_id, email, user_display_name) VALUES
    -- Can sign in.
    ('0192f0a0-0000-7000-8000-0000000000a1', '0192f0a0-0000-7000-8000-000000000001',
     'alice@example.com', 'Alice'),
    -- A User with no credential: signs in with nothing, and must be refused
    -- exactly like an unknown address (contract: localLogin).
    ('0192f0a0-0000-7000-8000-0000000000b1', '0192f0a0-0000-7000-8000-000000000001',
     'bob@example.com', 'Bob');

-- Soft-deleted, with a credential that would otherwise verify: a deleted User
-- is not a User (non-negotiable 4).
INSERT INTO users (id, household_id, email, user_display_name, deleted_at) VALUES
    ('0192f0a0-0000-7000-8000-0000000000c1', '0192f0a0-0000-7000-8000-000000000001',
     'carol@example.com', 'Carol', now());

INSERT INTO credentials (user_id, password_hash) VALUES
    ('0192f0a0-0000-7000-8000-0000000000a1',
     '$argon2id$v=19$m=19456,t=2,p=1$ONoBwoeQyx5Q6rcT+99mQQ$SwIz0ZviKsLDspuhSO7B6UvPvKMokTfAVA9i9RqMiUg'),
    ('0192f0a0-0000-7000-8000-0000000000c1',
     '$argon2id$v=19$m=19456,t=2,p=1$ONoBwoeQyx5Q6rcT+99mQQ$SwIz0ZviKsLDspuhSO7B6UvPvKMokTfAVA9i9RqMiUg');
