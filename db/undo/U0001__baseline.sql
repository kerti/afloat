-- Undo for V0001__baseline.sql.
--
-- NOT a Flyway location (BOOTSTRAP.md §7): Flyway's U__ undo migrations are a
-- paid feature, so these files sit outside the scanned path deliberately.
-- `flyway undo` is not available and is not meant to be. This file exists for
-- one reason — to be concatenated into the goose Down section by
-- `make gen-goose-migrations`.
--
-- Reverse order of creation, so foreign keys drop before their targets.

DROP TABLE IF EXISTS login_attempts;
DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS households;
