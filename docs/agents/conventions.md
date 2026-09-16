# Codebase conventions

Tactical, load-bearing rules that don't rise to an ADR. `BOOTSTRAP.md` §4 is the authoritative source
for all of these — this file exists so an agent working a specific file doesn't have to re-read the
whole scaffold sheet. If the two ever disagree, `BOOTSTRAP.md` wins and this file is out of date.

- **Money:** `DECIMAL(20,4)` columns, strings on the wire. Configure Jackson explicitly on the Kotlin
  side — Spring's default emits JSON numbers (or scientific notation) for `BigDecimal`, which silently
  breaks parity with Go's string encoding. Worth its own conformance test rather than trusting review.
- **Ids:** UUIDv7, client-generated, column type `uuid`. Create is idempotent:
  `INSERT ... ON CONFLICT (id) DO NOTHING` + read-back, 200 on replay, 201 on first write.
  - Kotlin/Spring Data JPA footgun: a non-null assigned `@Id` makes `isNew()` false, so `save()` calls
    `merge()` — a SELECT before every INSERT. Implement `Persistable<UUID>` with a transient `isNew`
    flag.
- **Tenancy:** `household_id` filtered in SQL on every Household-scoped query, never only in
  middleware.
- **Dates:** `occurred_on` is a `date` (the Period Day); `captured_at` is `timestamptz`; everything
  else that's an instant is `timestamptz`. Period Day (`day_starts_at` + per-User `time_zone`) is
  computed server-side only — if the frontend also derives it, the two disagree at 03:59.
- **Soft delete everywhere:** nullable `deleted_at`. No hard-delete endpoint. Scoped to
  Household-scoped domain data — instance-local auth state (`sessions`, `credentials`,
  `invitations`) is exempt and hard-deletes, because session revocation *is* the row delete.
- **Constraints:** `CHECK (amount <> 0)` on expenses. Negative amounts are valid (refunds with no
  recorded original); zero is not.
- **Error envelope:** one shape across both backends, identical to Balances ADR-0027:
  `{"code": "SCREAMING_SNAKE", "args": {...}}`, no `message` field, `args` values JSON primitives
  only. Codes, not messages — the frontend localises. `VALIDATION` carries `{field, rule}` and
  reports the first failing field only.
- **Auth:** Argon2id (`m=19456`, `t=2`, `p=1`, salt 16, key 32) as a PHC string; a hash written by
  either backend must verify in the other. Session tokens hashed at rest (the cookie keeps the
  plaintext). Sliding TTL with an absolute cap, refreshed on a threshold rather than every request.
  Login backoff lives in Postgres, not process memory — two backends, so an in-memory limiter
  diverges where contract conformance can't see it. `BOOTSTRAP.md` §5.1 has the reasoning.
- **Contract:** `contract/openapi.yaml` is hand-written and authoritative. Regenerate Go server
  interfaces, Kotlin interfaces, and TypeScript types from it; never hand-edit generated output. CI
  runs the generators and `git diff --exit-code`.
- **Trajectory:** computed server-side in both backends. The client formats and floors for display but
  derives nothing. `contract/testdata/trajectory.json` is the shared fixture both backends' unit tests
  read — a formula change updates the fixture and both backends' tests together.
- **Migrations, pre-release:** a schema change **edits `V0001__baseline.sql` and `U0001` in place**
  and regenerates — it does not add `V0002`. That freedom ends at the first database nobody can drop
  (your own Household's, long before any tag), not at a version number. Always reset both databases
  after an in-place edit (`make db-reset`): Flyway refuses to start on the checksum mismatch, but
  goose has no checksum, skips the file, and silently leaves Go on the old schema — both observed,
  and asserted by `make test-migration-runners`. `BOOTSTRAP.md` §7.1.
- **Migrations:** `db/migrations/V####__name.sql` is canonical (Flyway-native, up only). `db/undo/`
  holds the goose-Down source, generated into `backend/internal/migrations/` by
  `make gen-goose-migrations` — never hand-edit the generated goose file. See `BOOTSTRAP.md` §7 for why
  one shared directory doesn't work across Flyway and goose.
- **i18n:** `en-GB` and `id-ID` both real from day one, centralized strings, no hardcoded UI copy.
