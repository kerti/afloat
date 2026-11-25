# Codebase conventions

Tactical, load-bearing rules that don't rise to an ADR. `BOOTSTRAP.md` §4 is the authoritative source
for all of these — this file exists so an agent working a specific file doesn't have to re-read the
whole scaffold sheet. If the two ever disagree, `BOOTSTRAP.md` wins and this file is out of date.

- **Money:** `DECIMAL(20,4)` columns, strings on the wire. A JSON number or scientific notation is a
  bug. Worth its own conformance test rather than trusting review. Out: exactly four decimals
  (`StringFixed(4)`). In: an unsigned decimal string, at most 16 integer digits and 4 decimals, refused
  and never rounded (`MoneyInput`'s `pattern`); an Expense's amount is a `SignedMoneyInput` (a leading
  `-` is a refund) and zero is `VALIDATION {amount, nonzero}`. The Household's plan (income, the Monthly Budget,
  allocations) has no `currency` column: it is in `reporting_currency`.
- **Ids:** UUIDv7, client-generated, column type `uuid`. A row only the server creates and no request
  addresses by id (the Budget tables, allocations included) has a composite natural key and no `id`. Create is idempotent:
  `INSERT ... ON CONFLICT (id) DO NOTHING RETURNING *` + read-back, 200 on replay (the row as stored;
  the rest of the body is not compared), 201 on first write. The contract's `pattern` enforces the
  canonical dashed v7 spelling. An id held by another Household or by the caller's own deleted row is
  `409 ID_TAKEN`; a name-index `23505` is read back by id before it is `NAME_TAKEN` (a concurrent
  replay can raise it; so can any other unique index on the id, such as a Category's
  `UNIQUE (household_id, id)`, which `ON CONFLICT (id)` does not arbitrate). Only `ErrNoRows` means the
  id is held; apart from those clashes, any other error is `500`.
- **Names:** `<entity>_display_name` everywhere. One shared rule (`auth.CheckBoundedName`: trimmed,
  `required` / `max` / `pattern`), a per-entity cap (60 for Household, Pocket and Category). Unique among live
  rows via a partial index on `lower(name)`; a clash is `409 NAME_TAKEN`. Pin only ASCII case folds.
- **Path ids:** an `{id}` that is unknown, another Household's or deleted is one `404 NOT_FOUND` (no
  `args`, same bytes for every body, `{}` included); validation comes before the lookup. A path
  parameter is `format: uuid` with no `pattern` and no `maxLength` (#106). A `PATCH` boolean that a
  `false` must set is `sqlc.narg` and a `*bool`. Deactivating (`active`) is not a delete. Delete and
  restore alone see a deleted row: for them only an unknown or foreign id is the `404`.
- **A list of changes in a `PATCH`** (`PATCH /budget`'s `allocations`): an array of `{key, value}`, the
  value required and nullable (an absent key is a contract failure, never a clear). Checked in full
  before anything is written (repeat `unique`, then per item `invalid` for an unknown, foreign or deleted
  key, `required` for `null` on a Commitment), written in one transaction in key order so two requests
  cannot deadlock, each upsert checking tenancy and share-locking the row. An answer built from several
  statements is read from one `REPEATABLE READ`, `READ ONLY` snapshot (`GET /budget`, and `PATCH /budget` after its
  write commits: a failed read then is a `500` with the change saved).
- **Collections:** `{"items": [...]}`, empty is `{"items": []}` never `null`, ordered by `created_at, id`.
  The exception is the unbounded Expense list: keyset-paged (`limit` 1 to 100, opaque `after` and
  `next`, `next` omitted on the last page), an order ending in `id`, any computed sort key in the
  cursor. Every query on a growing table ships its plan (`EXPLAIN ANALYZE`) in the PR.
- **Tenancy:** `household_id` filtered in SQL on every Household-scoped query, never only in
  middleware.
- **Dates:** `occurred_on` is a `date` (the Period Day); `captured_at` is `timestamptz`; everything
  else that's an instant is `timestamptz`. Period Day (`day_starts_at` + per-User `time_zone`) is
  computed server-side only — if the frontend also derives it, the two disagree at 03:59.
- **Soft delete everywhere:** nullable `deleted_at`. No hard-delete endpoint. A version log with no delete operation (the Budget tables: budgets, monthly budgets, allocations) carries
  no `deleted_at`. Scoped to Household-scoped domain data — instance-local auth state (`sessions`, `credentials`,
  `invitations`) is exempt and hard-deletes, because session revocation *is* the row delete.
  `DELETE /<entity>/{id}` is soft (`204`), `POST /<entity>/{id}/restore` is its Undo (`200` with the
  entity); both idempotent on the caller's own row in one `household_id`-scoped statement, a repeat
  changes no column, and a delete keeps the first `deleted_at`. A restore into a held name is
  `409 NAME_TAKEN` (the unique index decides it, never a lookup). An entity that history references
  cannot be deleted while referenced (`409 <ENTITY>_IN_USE`, `POCKET_IN_USE` and `CATEGORY_IN_USE`, any
  Expense live or deleted; and `reporting_currency` is locked by any Expense, `409 CURRENCY_LOCKED`: lock the row
  `FOR UPDATE` (`FOR NO KEY UPDATE` for the Household and the Pocket, so a foreign-key insert is not
  blocked; BOOTSTRAP §4), run the `EXISTS` check as a **separate later statement**, then delete, all in one
  transaction, with every referencing write taking `FOR SHARE` on the row (an Expense create locks the Household, then the Pocket, then the Category, each its own statement). Never a `NOT EXISTS` in
  the `UPDATE`'s `WHERE`: under `READ COMMITTED` it does not see what committed during the lock wait;
  BOOTSTRAP §4). **A Household always keeps an active Pocket** (`409 LAST_POCKET`): the same recipe with the **Household's** row as the lock (`FOR NO KEY UPDATE` it, lock the Pocket, `EXISTS` another active live Pocket as a separate later statement, then write), taken by every writer that can shrink the active set (delete, and `PATCH` with `active: false`), `NOT_FOUND` decided first; BOOTSTRAP §4. A restore is "as if it was never deleted": it redoes, in its own transaction, what a carry-forward skipped (a Category's allocation, through the carry-forward's own SQL, `ON CONFLICT DO NOTHING`; BOOTSTRAP §4). Deactivating is not deleting.
- **Constraints:** `CHECK (amount <> 0)` on expenses. Negative amounts are valid (refunds with no
  recorded original); zero is not.
- **Error envelope:** one shape, identical to Balances ADR-0027:
  `{"code": "SCREAMING_SNAKE", "args": {...}}`, no `message` field, `args` values JSON primitives
  only. Codes, not messages — the frontend localises. `VALIDATION` carries `{field, rule}` and
  reports the first failing field only.
- **Auth:** Argon2id (`m=19456`, `t=2`, `p=1`, salt 16, key 32) as a PHC string, verified by the
  parameters inside it. Session tokens hashed at rest (the cookie keeps the plaintext). Sliding TTL
  with an absolute cap, refreshed on a threshold rather than every request. Login backoff lives in
  Postgres, not process memory, so it survives a restart. `BOOTSTRAP.md` §5.1 has the reasoning.
- **Contract:** `contract/openapi.yaml` is hand-written and authoritative. Regenerate the Go server
  interfaces and TypeScript types from it; never hand-edit generated output. `make check` regenerates
  into a scratch directory and diffs.
- **Trajectory:** computed server-side. The client formats and floors for display but derives
  nothing. `contract/testdata/trajectory.json` is the fixture the backend's unit tests read — a
  formula change updates the fixture and the tests together.
- **Migrations, pre-release:** a schema change **edits `00001_baseline.sql` in place** — it does not
  add `00002`. That freedom ends at the first database nobody can drop (your own Household's, long
  before any tag), not at a version number. Always reset the database after an in-place edit
  (`make db-reset`): goose has no checksum, skips the file, and silently leaves the database on the
  old schema — observed, and asserted by `make test-migrations`. `BOOTSTRAP.md` §7.1.
- **Migrations:** `backend/internal/migrations/#####_name.sql` is canonical and hand-written, goose
  format with `-- +goose Up` and `-- +goose Down` in one file. The binary embeds them; sqlc reads
  their Up sections as the schema. `BOOTSTRAP.md` §7.
- **i18n:** `en-GB` and `id-ID` both real from day one, centralized strings, no hardcoded UI copy.
- **Frontend UI and UX:** `ui-conventions.md`, beside this file.
