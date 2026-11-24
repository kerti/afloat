# Afloat — Bootstrap Decisions

> **Status:** Ratified 13 September 2026. This is the scaffold sheet: every cross-cutting decision
> that must be true from the first commit, because it is expensive or impossible to change later.
>
> Product scope lives in [`PRD.md`](PRD.md); domain language and arithmetic in
> [`CONTEXT.md`](CONTEXT.md); the *why* in [`VISION.md`](VISION.md). Repo/architecture reasoning is in
> `docs/scratch/Decisions Draft.md`, which this document **corrects** on migrations (§7).
>
> Backend-specific technical choices belong in `docs/adr/go/`, not here. This document is for what
> the backend and the frontend must agree on, and for scaffold-wide conventions.

---

## 1. Identity and naming

Settled first because it gates every identifier in the project.

| Thing | Value |
|---|---|
| Product name | **Afloat** |
| Repository | `github.com/kerti/afloat` |
| Go module | `github.com/kerti/afloat/backend` |
| npm package | `afloat-frontend` (private) |
| Docker image | `afloat` |
| Database | `afloat` |
| Landing page | `afloat.kerti.dev` |
| Application | `app.afloat.kerti.dev` |
| Demo | `demo.afloat.kerti.dev` |
| Preview | `preview.afloat.kerti.dev` |

**Bare unless the name would be ambiguous in its own namespace.** The image and the database are
`afloat`: the frontend is embedded in the backend (§8), so there is one image, and one database. The
Go module is `.../backend` because the repository path is Go's convention. Hyphen where a separator
is needed; never in a Postgres identifier, where it would have to be double-quoted at every use site.

### Local ports

| Service | Port |
|---|---|
| Frontend (Vite dev server) | `5181` |
| Backend | `5182` |
| Postgres | `5184` |
| Postgres, throwaway test container | `5185` |
| Backend, conformance `local-disabled` server | `5186` |
| Backend, conformance `founding-disabled` server | `5187` |

Chosen against three constraints, which matter more than the specific numbers:

1. **Never a service's default.** `8080`, `5173`, `5432`, `3000`, `8000` are what every project
   reaches for first, which is exactly why they collide.
2. **Never `5000` or `7000`.** macOS binds both for AirPlay Receiver, and the resulting failure
   blames your app.
3. **Between 1024 and 32767.** Above that is the ephemeral range — 49152+ on macOS, 32768+ on Linux
   — where an outbound connection can transiently hold the port, so a bind fails at random and does
   not reproduce. Afloat's Postgres was briefly on `55432`: exactly this bug, waiting.

Contiguous because one fact is easier to hold than several, and `5181` sits above Vite's `5173` so
it still reads as the frontend.

**This is Afloat's own allocation and claims nothing about any other project.** No registry, no
reserved block, no assumption that a neighbouring repository has heard of this table. Afloat and
Balances are standalone (`VISION.md` §5); a genuinely shared convention would have to be written down
on both sides to be one at all, and that is not a coupling either app is asking for. Every port here
is overridable — `AFLOAT_DB_PORT`, `AFLOAT_TEST_DB_PORT` — so a collision with something else on the
machine is a variable, not a patch.

**No vanity import path.** Go's convention is the repository path; a vanity path
(`kerti.dev/afloat`) would couple `go get` to the landing page staying up, for no benefit.

**The app gets its own host.** The landing page owns the apex; if both lived there, the SPA fallback
would fight the marketing page for `/`.

**There is an "Afloat Budgeting" on the App Store.** Accepted. It is harmless for a self-hosted
household tool and a portfolio repository, and the name fits the product.

## 2. Repo layout

Per `docs/scratch/Decisions Draft.md` §3, with migrations corrected per §7 below:

```
afloat/
├── backend/                        # Go backend
│   └── internal/
│       ├── period/                 # Period Day and Budget Period arithmetic, pure — §6 (#130)
│       ├── budget/                 # the Budget of the current Period, its materialisation and its changes (#130)
│       ├── category/               # Categories: list, create (with the allocation one starts with), rename, deactivate or reactivate (#130)
│       ├── migrations/             # CANONICAL goose migrations, hand-written, //go:embed — §7
│       └── auth/common_passwords.txt  # the password denylist — §5.1
├── frontend/                       # React app
├── contract/
│   ├── openapi.yaml                # single source of truth for the API
│   ├── testdata/                   # fixtures the backend's tests read (trajectory, periods, budget…) — §6
│   └── conformance/                # black-box HTTP cases against the real backend
├── docs/{adr/go,brand,qa}/
├── scripts/                        # repo tooling the Makefile shells out to
├── docker-compose.yml              # one Postgres, one database
└── Makefile
```

## 3. Stack

Inherited wholesale from Balances-v2 where it exists there, which is most of it.

**Frontend** — React 19, Vite, TypeScript, Tailwind 4, shadcn/Radix, TanStack Query, react-router,
Recharts, i18next, Vitest, Playwright, MSW. Node 22 (`.nvmrc`). Mobile-first; installable PWA with an
app-shell service worker (no push in MVP — see §10).

**Backend** — Go 1.27.x. Migrations embedded via `//go:embed` and applied by the binary, mirroring
Balances. The IANA zone database is embedded too (`_ "time/tzdata"` in `cmd/afloat`, about 450 KB) as
a floor, not a pin: `time.LoadLocation` reads the host's zoneinfo first and falls back to the embedded
copy, so a `scratch` or distroless image, which has none, still accepts every valid `time_zone`. A host
with zoneinfo answers from it, so a case-insensitive filesystem (a Mac) also accepts `asia/jakarta`;
`contract/testdata/time_zones.json` pins only the rows every host agrees on.

**Database** — PostgreSQL, one instance, one database (`afloat`).

## 4. Cross-cutting data conventions

- **Primary keys:** UUIDv7, **client-generated**. Column type `uuid`. *One exception: founding.*
  The Household and the User that `POST /onboarding/choice` creates get **server-generated** UUIDv7s
  (minted in Go). Founding is an online authentication flow, not an offline-capable write: there is
  no queue to replay, and the handshake's single-use claim is what makes a double submit safe
  (§5.1.1). The rule above stands for every domain `POST`.
  *A clarification, not a second exception (#130, R14):* the rule is about **rows a client creates**.
  A row that **only the server creates, and no request ever addresses by id** (the Budget tables:
  `budgets`, `monthly_budgets` and `allocations`, which materialise on the first read of a Budget
  Period) has a **composite natural key** (`household_id`, `year_month`, and for a version log its
  `effective_on`, and for `allocations` its `category_id`) and no `id` column. Minting a UUIDv7 for
  it in Go would be a second server-minted key with nothing
  to ever name it by.
- **Idempotent create:** `POST` accepts the client-supplied id; a replay returns **200** with the
  existing resource, a first write returns **201**. Implemented as `INSERT ... ON CONFLICT (id) DO
  NOTHING RETURNING *` plus a read-back (#121, the first one; every later create copies it):
  - **The id is a UUIDv7 and the contract enforces it:** `format: uuid` plus a `pattern` for the
    canonical dashed form, version 7, RFC variant. kin-openapi does not check `format: uuid`, and the
    generated decode accepts four spellings, so without the pattern a `crypto.randomUUID()` (a v4) is
    stored and "UUIDv7" quietly is not one. A wrong one is `VALIDATION {field: id, rule: pattern}`.
    No database `CHECK` on the version.
  - **A replay answers the row as stored now, and the rest of the body is neither compared nor
    applied.** A retry after a lost `201` that meets another member's rename must not become an
    error. The read-back is scoped by id, `household_id` and `deleted_at IS NULL`; a live row of the
    caller's own Household, active or inactive, is the `200`.
  - **An id held by anything else is `409 ID_TAKEN`:** another Household's row, or the caller's own
    soft-deleted one, in one answer so a foreign id cannot be told from a deleted one. A deleted id
    is never reused: an id names one row for ever.
  - **A body that is invalid is refused whatever the id holds**, so a taken id with a bad name
    answers what a free id with a bad name does.
  - **Only `pgx.ErrNoRows` means "the id is held".** Apart from the name clash below, any other error
    from either statement is `500` with nothing written (§5.1).
  - **`ON CONFLICT (id)` arbitrates the primary key only.** A name clash still raises `23505` on the
    name index and is `NAME_TAKEN`, mapped by the constraint's name, never by the code alone. A
    replay of an id that is already committed never reaches the name index, but a *concurrent* replay
    can, when the first insert has not committed: the second passes the arbiter's pre-check, inserts
    its tuple and trips over the first's name-index entry. So a name-index `23505` is read back by
    id first: a live row of the caller's own Household under that id is the `200`, and only no row
    is `NAME_TAKEN`. Without it a double tap answers the loser "name taken" for its own Pocket.
    **The same holds for every other unique index on the id.** A Category's id is unique twice: the
    primary key and `UNIQUE (household_id, id)`, which the composite foreign key from `allocations`
    needs (#130). `ON CONFLICT (id)` arbitrates only the first, so a concurrent replay of one id can
    trip over the second (`23505` on `categories_household_id_id_key`), which is read back by id
    too:
    the live row is the `200`, no row is `ID_TAKEN`. Found by the concurrent-create test, as a
    `500`.
  - **No transaction and no detached context:** it is one statement, and retrying with the same id
    is the recovery.
- **Names are `<entity>_display_name`** on the wire, in SQL, in Go and in i18n keys (#113 D5, #121):
  `household_display_name`, `user_display_name`, `pocket_display_name`, `category_display_name`. A
  bare `name` would be one
  `VALIDATION` `args.field` meaning several things. **A bounded name follows one rule:** trimmed;
  nothing visible (blank, or only whitespace and Unicode Cf) is `required`; more than the entity's cap
  in code points is `max`; U+0000 is `pattern`; a Cf character inside a visible name is kept. It is one
  function (`auth.CheckBoundedName`), so no two entities can disagree about what a name is; only the
  cap differs (60 for a Household, a Pocket and a Category). The contract's `maxLength: 255` is an
  outer bound that
  counts padding. **A name may be unique among an entity's live rows** (a Pocket's and a Category's
  are, a Category's
  across both buckets): a partial
  unique index on `(household_id, lower(name)) WHERE deleted_at IS NULL`, so a deleted row releases its
  name and an inactive one keeps it. A clash is `409 NAME_TAKEN`, one entity-free code that every
  named entity shares. `lower()` follows the database's locale, so no fixture pins a non-ASCII fold.
- **Collections** are listed as an object, `{"items": [...]}`, never a bare array (so it can grow a
  `next` without breaking a client), empty as `{"items": []}` and never `null`. A small, bounded
  collection is not paginated, and the Household's Pockets are one. Order is declared, never left to
  the database: `ORDER BY created_at, id`, so a rename does not reshuffle a picker and the order does
  not depend on a collation. The list holds every row that is not soft-deleted; whether an inactive
  one appears is the entity's call, and the client filters what it already holds.
- **Money:** `DECIMAL(20,4)`, serialised as **strings** on the wire, at exactly the storage scale —
  4 decimal places, always, never trimmed and never exponent notation. `"25000000"` and `"2.5e7"` are
  both wrong for a `25000000.0000` row; `"25000000.0000"` is the only correct rendering. In Go, use
  `decimal.Decimal.StringFixed(4)`, not `.String()`, which strips trailing zeros. Worth an early
  conformance test.
  **On the way in** an amount is a `MoneyInput` (#130, R11): an unsigned decimal **string**, at most 16
  integer digits and 4 decimals, with no sign, exponent, separator or whitespace, enforced by the
  contract's `pattern` (`VALIDATION {rule: pattern}`). More than 4 decimals is refused, **never
  rounded**. A JSON number is `INVALID_JSON_BODY` (no scalar coercion). Out is a `Money`: the same
  number at exactly four decimals, and negative only where it is a difference.
- **Currency:** every monetary value carries its `currency` column even though the MVP UI is pinned to
  the Household's reporting currency. **The exception is the Household's plan** (#130, R11, PRD N3):
  `households.expected_monthly_income` and the Budget's amounts (`monthly_budgets.monthly_budget`
  and
  `allocations.allocation`) have no `currency` column and are in the Household's
  `reporting_currency`, because a plan is what reporting-currency spend is compared to. Changing
  `reporting_currency` relabels them and converts nothing.
- **Dates:** `occurred_on` is a `date` (the Period Day). `captured_at` is `timestamptz`. Everything
  else that is an instant is `timestamptz`. A Budget version's `effective_on` is a `date`, the writer's
  Today (#130).
- **Period Day is computed server-side only.** `day_starts_at` plus per-User `time_zone` is real
  arithmetic; if the frontend also derives it, the two disagree at 03:59.
- **Soft delete everywhere:** nullable `deleted_at`. Hard delete is not exposed. *A clarification
  (#130, R14):* the rule covers **anything that can be deleted**. A version log that has **no delete
  operation** (a Budget is never removed, and a changed amount is a newer version, not a delete:
  `budgets`, `monthly_budgets`, `allocations`) carries no `deleted_at`: a column nothing sets is a
  filter every read must remember. **Deactivating is not
  a delete** (#121, R1): a row that history references is retired with an `active` boolean, changed by
  an ordinary merge `PATCH`. It stays visible and in the list (the client filters), keeps its name and
  keeps labelling what it labels. Only a delete gives up the name and disappears.
  - *The wire* (#121, the first one; every later entity copies it): `DELETE /<entities>/{id}` sets
    `deleted_at` and answers `204`; the row stays, and the contract's description says so in plain
    words, because the verb is `DELETE` and the effect is not. `POST /<entities>/{id}/restore` clears
    it and answers `200` with the entity, `active` as it was. **Both are idempotent on the caller's
    own row**, each in one statement scoped by `household_id`: a delete keeps the **first**
    `deleted_at` (`coalesce(deleted_at, now())`), and a repeat of either changes no column at all
    (`updated_at` moves only for the statement that did the work). No confirmation: restore is the
    Undo. A deleted row is invisible to the list and to `PATCH` (`404`); only these two see it.
    Unknown or another Household's is `404 NOT_FOUND` for both, so a foreign id is never confirmed.
  - **A restore into a held name is `409 NAME_TAKEN`** and the row stays deleted. The partial unique
    index decides it, mapped by constraint name as a create's is, never a lookup before the
    `UPDATE`: two concurrent restores into one name answer one `200` and one `409`, never a `500`.
  - **An entity that history references cannot be deleted while it is referenced (#121, R1):**
    `409 POCKET_IN_USE` for a Pocket, soft-deleted Expenses included (built by the Expenses slice,
    which adds the referencing table; until then nothing can reference a Pocket, so the code is not
    in the contract), and `409 CATEGORY_IN_USE` for a Category (#130, PR 4), which today means an
    allocation, a non-null amount, in a Budget Period **other than the current one** (the Expenses
    slice extends it to any Expense, soft-deleted ones included). **The guard is not a `NOT EXISTS`
    in the delete's `WHERE`** (this bullet said so until #130): under `READ COMMITTED` an `UPDATE`
    whose target row is locked waits for the locker, then proceeds on the row's new version **without
    re-running its subquery**, so a referencing row committed during the wait is never seen. The
    shape that holds is one transaction of three statements, **in this order**: (1) `SELECT ... FOR
    UPDATE` of the entity's row (an unknown or another Household's id is no row, `404`; a row already
    deleted is the idempotent `204`, and the guard does not apply to it); (2) the `EXISTS` check, as a
    **separate statement**, which takes its snapshot only after the lock was granted and so sees
    everything committed by every writer that held the row; (3) the soft delete. Every write that
    inserts a referencing row takes `FOR SHARE` on the entity's row **in the same statement that
    verifies it is live** (`INSERT ... SELECT ... FROM categories ... FOR SHARE`), so a writer that
    comes after the delete finds it deleted and refuses, and one that came first is seen by the check.
    Lock order: the delete holds one row, so it cannot deadlock with the writers, which take their
    rows in key order (the sorted allocation writes, above). The test for a guard of this kind runs
    the same scenario against a single `UPDATE ... WHERE NOT EXISTS` and shows it lose
    (`category_delete_integration_test.go`).
  - **A restore is "as if it was never deleted"** (#130, the maintainer's ruling). It clears
    `deleted_at` and, **in the same transaction**, redoes what a carry-forward skipped while the row
    was deleted: it locks the row `FOR UPDATE` (as the delete does), clears `deleted_at`, brings the
    current Budget Period into being (`budget.Materialise`, idempotent) and carries the Category's
    allocation forward with `budget.CarryCategoryForward`. That is **the carry-forward's own rule, not
    a second one**: `CopySource` and `CopyAllocationsForward` take an optional `category_id`, so there
    is one place that decides what a Period starts with (the hook for a recurring default, Q-17, or an
    annual plan, Q-20). The source for one Category is the nearest earlier Period that holds an
    allocation row **for it** (the Period's own copy may have come from one that never did); its latest
    state is carried when it is an amount, dated the Period's first day. Nothing is written when the
    Category already has any row in the Period, when its latest state is "none", or when no other
    Period holds one. Writes to `allocations` that can meet (the first-read copy and a restore) are
    `ON CONFLICT (natural key) DO NOTHING` and the copy has a `NOT EXISTS`, so two rows or a `23505`
    cannot result. Lock order: the restore holds the Category's row and may wait on a materialiser's
    marker, never the reverse (a materialiser's copy reads only live Categories and the row being
    restored is deleted to every snapshot but the restore's own), so there is no cycle. A restore of a
    row that is not deleted writes nothing and materialises nothing.
  - The cross-site guard (§5) covers `DELETE` like every non-safe method; it was first exercised by
    `DELETE /pockets/{id}`, and a refused request deletes nothing.
  - *Scope: Household-scoped domain data.* Instance-local auth state — `sessions`, `credentials`,
    `invitations` — is exempt and hard-deletes. Session revocation **is** the row delete; a
    soft-deleted session is a live session that merely looks dead, and every read of the table would
    have to remember the filter. Taking this literally while writing `00001_baseline.sql` produces a
    logout that does not log anyone out.
- **Tenancy:** every query touching Household-scoped data filters `household_id` **in SQL**, not only
  in middleware.
- **Constraints:** `CHECK (amount <> 0)` on expenses — negatives are valid (refunds with no recorded
  original), zero is not.
- **Error envelope:** one shape, following Balances ADR-0027. It carries
  **codes, not messages**; the frontend localises.
- **Updates are `PATCH`, with merge semantics** (#113, the first one; every later update copies it).
  The body is JSON; an absent property is unchanged, an unknown property is `INVALID_JSON_BODY`
  (`additionalProperties: false`), and `{}` is a valid no-op. The answer is `200` with the whole
  resource. `null` clears a nullable field and is `INVALID_JSON_BODY` for any other, so a field that
  can be cleared needs a request type that tells absent from `null`, which oapi-codegen's default
  pointers cannot. **A clearable field** (#130, PR 5: `PATCH /household`'s
  `expected_monthly_income`, the first) is `nullable: true` in the contract (`$ref` the shared
  `MoneyInputOrNull` for money), which `nullable-type: true` in `oapi-codegen.yaml` generates as
  `nullable.Nullable[T]` (absent, `null` and a value are three states: `IsSpecified`, `IsNull`,
  `Get`); every nullable request property gets it, so none can be written as a pointer by mistake.
  In SQL it is an explicit flag beside the value, `col = CASE WHEN sqlc.arg(set_col)::boolean THEN
  sqlc.narg(col)::<type> ELSE col END`, the handler setting the flag when `IsSpecified()` and the
  value only when it is not `null`; `coalesce` stays the rule for a column `null` cannot clear.
  **Last write wins, per field:** no `If-Match`, no version column and no conflict code. Disjoint
  fields from two writers both land, because the update is one `UPDATE` with
  `coalesce(sqlc.narg(...), column)` (or the flagged `CASE` above), never a read-modify-write. A
  precondition header can be added later without breaking a client. The idempotent-create rule above
  concerns `POST` and a client-supplied id and does not apply; a `PATCH` of absolute values is
  idempotent on its own, so a retry is safe. A domain update is tenant-scoped in SQL (`WHERE id =
  <household_id from the session>`, never from the request), sets `updated_at` itself from the
  database's `now()` (no trigger does), and is rejected before it runs by the contract's own rules,
  then by the handler's. **A boolean that a `false` must be able to set is `sqlc.narg` and a Go
  `*bool`** (`PATCH /pockets/{id}`'s `active`): a plain `bool` zero value would read as "absent" and
  silently drop every deactivation. A row that the `WHERE` does not match is no row, and the answer
  is `404 NOT_FOUND` (next bullet).
- **A list of changes inside a `PATCH`** (#130, PR 3: `PATCH /budget`'s `allocations`) is an array of
  `{key, value}` items, not a map (OpenAPI 3.0.3 has no `patternProperties`). A value that may be cleared
  is **required and nullable** on the item (`allocation: null` is "none", an absent key is a contract
  `required` failure and never a clear), and a Category not listed is untouched. **Failure order:** the
  contract's rules (a nested failure names its path, `allocations.<index>.<property>`), then the handler's,
  all before anything is written, so a refused request writes nothing and does not even materialise a
  Period: a repeated key is `VALIDATION {field: category_id, rule: unique}`; then, item by item in the order
  sent (the handler's rules; the contract's own failure is the least by (field, rule) as a string, so with
  11 or more items `allocations.10` sorts before `allocations.2`), a key that is unknown, another Household's or deleted is `{category_id, invalid}` (one answer for
  the three, so a foreign id is never confirmed), and `null` for a Commitment is `{allocation, required}`.
  The write is **one transaction** (the Period's materialising, the Monthly Budget if sent, and every
  item; the answer is read after it commits, next bullet), one upsert per item, each checking the Category is the Household's own and live in
  the same statement and share-locking it; zero rows rolls everything back and answers the same `invalid`
  (a Category deleted between the check and the write). **Items are written in key order, whatever order
  they were sent in**: each upsert holds its row until the transaction ends, so two requests naming the same
  rows in opposite orders would each hold the one the other waits for next and Postgres would abort one.
- **A response built from more than one statement is read from one snapshot** (#130, PR 3). Under
  `READ COMMITTED` each statement sees what has committed by the time it starts, so a concurrent edit that
  lands between two of them leaves the answer half old and half new (a Monthly Budget from before an edit
  beside allocations from after it: a Budget that never existed). `GET /budget` therefore materialises
  its Period first, if it must, in a transaction of its own that commits, and then reads in a
  `REPEATABLE READ`, `READ ONLY` transaction (`budget.Beginner.BeginTx`), which can neither fail to serialise
  nor hold a lock a writer waits on. **`PATCH /budget` reads its answer the same way** (maintainer's ruling, #130):
  the write transaction commits, and the answer is then read in its own snapshot, by the same code as
  `GET`. The alternative, reading back inside the writing transaction, runs at `READ COMMITTED` (a
  `REPEATABLE READ` write would fail concurrent upserts of one key with `40001`), so a concurrent edit
  committing between the read's statements would show through and the answer could be a Budget that never
  existed. The cost, accepted: a failure of that read answers `500` although the change is saved. A `PATCH`
  of absolute values is idempotent, so repeating it is the recovery.
- **Tenancy on a path id** (#121, the first `{id}` route): an id that is **unknown, another
  Household's, or soft-deleted** answers one `404 NOT_FOUND`, a code with no `args`, the same bytes in
  every case and for every body, `{}` included (a no-op must not confirm another Household's id).
  The two operations that are *for* a deleted row, delete and restore, answer it for an unknown or
  another Household's id only: the caller's own deleted row is a row to them, and the idempotent
  success. Not
  `403`, which says "exists, not yours". The order in which a request can fail, each step reached only
  if the earlier ones passed: the cross-site guard (`403`), the session (`401`), the id's shape (`400`
  from the binder), the body (`400`: the contract's rules, then the handler's), the statement (`404`,
  `409`). **Validation before lookup**, so a foreign id with a bad body answers what an unknown one
  does. This `404` is the first with an envelope: a path that matches no route is still a bare `404`
  (#56). A path parameter is a `format: uuid` with **no `pattern` and no `maxLength`**: the validator
  checks a path on its encoded form and the binder reads the decoded one (#106), and a `pattern` would
  refuse `%30192f...`, the percent-encoded spelling of a valid id. The spellings `uuid.Parse` accepts
  (upper case, `urn:uuid:`, braces, 32 hex) name the same row and are accepted.

## 5. Authentication

**MVP: local email + password only.** Server-side session, session cookie.

Cookie attributes — **host-only** (no `Domain` attribute), `HttpOnly`, `Secure`, `SameSite=Lax`. This
is not a default worth drifting from: `demo.` and `preview.` sit under a shared parent, and a cookie
scoped to `.afloat.kerti.dev` would leak a demo session into preview.

**Session cookie rather than a bearer token, deliberately.** A cookie keeps the entire mechanism
server-side and the client sees one opaque value; revocation is a row delete. A JWT scheme adds claims,
signing and expiry to get right, and nothing in the OpenAPI contract checks any of that.

**Users carry no credential column.** Credentials live in their own table keyed by user. This is the
one design cost paid up front so that Google OAuth later is an additive provider rather than a
migration of the user model — and it is the shape Balances arrived at anyway (a "dormant member" is a
user row with no credential).

**No email in MVP.** Invitations are a one-time link generated and displayed in the UI, delivered by
whatever means the founder likes. Password reset is a CLI command on the instance.

### 5.1 Session and credential mechanics

Settled against Balances' implementation (its ADR-0017, ADR-0027, ADR-0039) so a household running
both apps meets the same login and the same session behaviour in each. Afloat still knows nothing
about Balances at runtime (`VISION.md` §5) — this is shared shape, not an integration. Where Afloat
departs, the reason is below; departures are the interesting part, so don't quietly re-converge.

**Password hashing: Argon2id**, `m=19456` KiB (19 MiB), `t=2`, `p=1`, 16-byte random salt, 32-byte
key. Stored as a PHC string (`$argon2id$v=19$m=...,t=...,p=...$salt$hash`), so the cost parameters
travel with each hash and can be retuned with no migration. These are OWASP's current floor and
Balances' exact values. A hash is verified by the parameters inside it, never by the current
constants — `contract/testdata/argon2.json` pins that against hashes from an independent
implementation as well as Go's own.

**Password policy is a floor only:** minimum length plus a common-password denylist. No composition
rules. Cap the maximum length too, and put a body-size limit on JSON routes — Balances caps only its
file-upload handlers. Both lengths are counted in **Unicode code points**, not bytes and not UTF-16
units: an Indonesian or any non-ASCII passphrase must not be penalised for encoding wider. Go counts
runes.

**The denylist is one file, `backend/internal/auth/common_passwords.txt`**, embedded in the binary.

**Session tokens are hashed at rest.** The cookie carries a 256-bit random, URL-safe value; the
`sessions` primary key is its SHA-256. A database leak yields nothing usable. Hash before every read
and write; the cookie keeps the plaintext, or the next lookup never matches.

**`sessions.user_agent`'s storage rule, in order (#70, on top of #32 item 4's original empty-is-NULL
rule): fold HTAB to SP and trim SP/HTAB from both ends; absent or empty is NULL; not valid UTF-8 is
NULL; otherwise truncated to 512 Unicode code points, on a code point boundary.** The backend may
not read a client-supplied header and hand it to the database unexamined: Go's `r.UserAgent()`
carries a header's raw bytes through untouched, and a Postgres `TEXT` column is UTF8-encoded, so an
invalid header used to fail the whole login with a 500 (`invalid byte sequence for encoding "UTF8":
0x85`) that had nothing to do with the password. The fold step exists because servers and proxies
unfold an obsolete header fold (RFC 7230 §3.2.4's obs-fold) differently: Go collapses
`CRLF 1*(SP/HTAB)` to a single SP; others drop only the CRLF and keep the fold's own whitespace, tab
included. Folding HTAB to SP and trimming both ends converges every measured case (an internal tab, a
fold with nothing before it, a fold with nothing after it) to the same stored string whichever hop did
the unfolding — a real inline tab a client meant literally is stored as a space, because past this
rewrite there is no byte left to tell the two apart by. `sanitizeUserAgent` applies the whole rule at
capture time, before the value ever reaches a session row; a raw-socket test (`obs_fold_test.go`)
exercises it end to end, since a client library that refuses to put a raw CR or LF in a header value —
as Go's own does — cannot drive the conformance harness through it.

**Sliding TTL, with an absolute cap — this is a departure.** Balances refreshes `expires_at` on every
authenticated request and never consults `created_at`, so a stolen cookie stays valid for as long as
the attacker keeps using it and the 30 days never arrive. Afloat keeps the sliding window and adds an
absolute lifetime checked against `created_at`. Re-authenticating a few times a year is not friction
worth a permanent session for.

**Touch the session on a threshold, not on every request — also a departure.** Balances issues an
`UPDATE` per authenticated request, which makes every `GET` a write on the one table read by every
request. Only refresh when more than half the TTL has elapsed: same observable behaviour, a fraction
of the writes, no row bloat. Capture is the hot path (PRD Q-11) and the PWA re-probes on every resume.

**Login must not enumerate accounts.** Unknown email, a User with no credential, and a wrong password
all return one `INVALID_CREDENTIALS`; the comparison is constant-time; and a request for an
address with no account still pays the full hashing cost, so timing cannot distinguish present from
absent either. All three, not two of three.

**A stored hash's cost is capped too: `m ≤ 65536` KiB and `t ≤ 10` (#68).** The PHC string carries its
own cost (above), which is what lets the current constants be retuned with no migration — but read
back unbounded, that same freedom lets a stored string alone dictate how much memory and time one
verify spends, worse once the concurrency cap below admits several such verifies at once. `65536` KiB
(64 MiB) and `10` iterations are headroom over the operating cost (`m=19456`, `t=2`) — about 3.4× the
memory and 5× the iterations — not x/crypto's absence of one. `parsePHC` refuses a stored hash above
either bound, through the same silent-log reject path as every other malformed PHC string (below). A
cost increase past either ceiling means raising the ceiling first, deliberately, rather than a stored
string quietly being allowed to cost more than the code that reads it expects.
`contract/testdata/argon2.json` carries accept rows at exactly `m=65536` and `t=10` and reject rows one
past each; the suite reads them.

**Concurrent Argon2id hashing is capped at 4, process-wide — a departure surfaced by
#33.** `m=19456` KiB is allocated *per hash in flight*, not once, so with no bound an unauthenticated
caller chooses the process's peak memory: 30 concurrent logins want `30 × 19 MiB ≈ 570 MiB`, and a Go
process that exceeds available memory is killed outright rather than degrading. Afloat's deployment target is a self-hosted box
(`VISION.md`), where that is the whole machine. At the cap, `4 × 19 MiB ≈ 76 MiB` of Argon2 memory
live at once — worst case `4 × 64 MiB = 256 MiB` if every hash in flight happened to be a stored hash
at the #68 ceiling. That is live memory, not the process's footprint: a finished hash's memory is reclaimed
only when the garbage collector runs, and Go's default `GOGC=100` lets the heap reach about twice its
live size first, so the peak can run to roughly double. Still bounded, which is the point. Retuning
Argon2id's cost parameter later must be checked against this arithmetic before it ships, or the memory
ceiling moves silently. The cap is a **fixed constant, not a §12 environment
variable** — small enough to fit the smallest supported deployment, large enough (~80 hashes/sec at
this cost, on a host with at least four cores) that a household's simultaneous logins are never
serialised behind each other in practice.

The dummy-cost-equalizer hash (the unknown-address path above) shares the **same** semaphore as a
real hash, not a separate one — the bound is on total concurrent Argon2 work regardless of account
validity, or bounding only the real path leaves the dummy path free to blow the same ceiling. A
request beyond the cap **queues for a permit rather than answering 429 immediately**: an immediate
429 would leak that the server is busy, which is not an account-level fact, and the login path's
whole constant-work design (previous paragraph) is built to hide exactly that kind of signal. Queueing
adds latency that is a function of load, not of the account being checked, so the enumeration-
resistance property survives it. The wait is bounded by the timeout the request already has, not a
second one invented for this step: the request context's deadline, set by chi's `Timeout`
middleware from `HTTP_WRITE_TIMEOUT`. A login still queued when the wait runs out answers `500 INTERNAL` and records **no** backoff failure, because its password was never checked.

**The deadline bounds the wait, not the login.** A login that took its permit in time finishes: the
second backoff read, the hash, the failure write and, on success, the session. That work runs on a
context detached from the request, bounded by the handler timeout again, counted from the permit. So
a login can run longer than `HTTP_WRITE_TIMEOUT` end to end, and its answer can then miss the connection's write deadline (`HTTP_WRITE_TIMEOUT` plus the 5s grace, §12),
but its failure is still recorded, and that is the part that matters.
Bounding it by the request deadline instead lets a flood switch the backoff off: behind a queue
filled faster than the cap clears, the login that gets a permit is the one about to run out of time,
its hash carries it past the deadline, and the failure write then fails. Its guess was checked and
answered `401`, but nothing was recorded, so the backoff never grows. Measured before the fix,
with the deadline scaled down to 300 ms: under a flood of unknown addresses, 4 to 9 guesses at one
account were answered `401` against a recorded `failure_count` of 1. A client that leaves after its
permit is taken no longer stops the login either; the answer is lost, but the failure is recorded.

**A login reads its backoff twice, the second time holding its permit.** The first read, before the
queue, answers a throttled caller without making it wait. It cannot be the only one: every login in a
burst on one account reads it before any of them has failed, so they all pass it, all queue, and all
have their passwords checked, at the cap's ~80 guesses a second for as long as the burst lasts, the
backoff never applying. So the permit is held from a second read, through the hash, to the failure
write. One caller's failure is written before the permit passes on, and the next caller reads it and
answers `429` without hashing. At most the cap's worth of logins hold permits at once, so a burst on one
account gets up to 4 guesses before the backoff applies, not the whole queue. The `429` is the same
answer the first read gives, keyed on the address and the IP whether or not the account exists, so the
wait before it tells a caller nothing the first read would not. The permit covers database round
trips as well as the hash: the backoff read and, on a failure, one write per key (the address's and the
IP's). So the cap's ~80 logins a second assumes a quick database. A slow one holds each permit longer
and lowers that rate for as long as it stays slow; nothing breaks, and it recovers when the database
does.

The queue moves what a login flood costs; it does not remove it. What runs ahead of the queue is not
capped: the first backoff read and the two credential lookups share the database pool with every
endpoint, so a flood fast enough to saturate the pool slows everything, as it did before the cap. A
queued login is a parked goroutine, so the queue itself slows logins and no other endpoint. But nothing bounds how many wait: each holds its goroutine and client connection, a few
tens of KiB rather than a hash's 19 MiB, for up to `HTTP_WRITE_TIMEOUT`, so a flood faster than the cap
clears (about 80 logins a second) grows memory with its rate for as long as it lasts. It holds no
database connection while it waits: pgx takes one per query. A client that leaves
ends its wait, and no hash runs for it. Whether the wait needs a bound is open: #54.

**The rate-limit key is the connection's own address, never `X-Forwarded-For`.** Self-hosting means
there may be no proxy in front, so nothing strips that header and it is attacker-controlled — using
it means an attacker picks a fresh key per request and the per-IP backoff stops existing. chi's
`RealIP` middleware stays unmounted. A deployment that does
put a trusted proxy in front changes this behind a config flag, never by default.

**Login backoff lives in Postgres — a departure, and the load-bearing one.** Per-IP and per-email
exponential backoff, capped, returning `429` with `Retry-After`. Backoff, never a hard lockout: a
lockout on a self-hosted household app is a footgun. Balances keeps the limiter in process memory. A
table keyed by ip/email with a `backoff_until` is testable, shared by every process serving the
instance, and survives a restart, which the in-memory version does not.

The curve is `LOGIN_FIRST_BACKOFF` (default `1s`) after the first failure, doubling per failure,
capped at `LOGIN_MAX_BACKOFF` (default `5m`) — §12. The doubling stops once it passes the cap, however
many failures follow: unclamped, it overflowed Postgres's interval at the 45th, and from then on the key was
never throttled again. What those defaults produce, and which keys a
failure writes, is `contract/testdata/login_backoff.json`; the suite reads it, and holds the defaults
to it (#16). The same goes for Argon2id: `contract/testdata/argon2.json` carries hashes minted by two
independent implementations, which must verify, and strings that must not. The accepted spelling is
exactly `$argon2id$v=19$m=…,t=…,p=…$salt$hash`, unpadded standard base64, `8p ≤ m ≤ 65536` KiB,
`1 ≤ t ≤ 10` (#68's ceiling, above), `1 ≤ p ≤ 255`, a hash of at least 4 bytes, and no base64 segment
of a length that cannot decode: the parser was once looser than that, and a malformed stored string
answers "does not verify", never an error or a panic.

**A database outage is not a statement about an account (#23, #27).** Only a lookup that genuinely
found nothing counts as an absent user, credential or session: `pgx.ErrNoRows`. Every other failure — a dropped connection, an exhausted pool — answers login
with `500 INTERNAL`, not `401 INVALID_CREDENTIALS`, and records **no** backoff failure. Session
resolution **leaves the cookie alone** and marks the request as one whose session it could not read,
so a route that needs a session answers `500 INTERNAL` and the same cookie works again once the
database is back. An unreadable session is a `500` with the cookie kept; an invalid one (no such
session, expired, or outliving its User: `pgx.ErrNoRows`) is a `401` with the cookie cleared; no cookie
is a `401`. A `401` tells the client to sign in again, which is the wrong instruction over an outage.
Answering an outage as though it were
about the account costs something that outlives the outage: a password reset that was never needed,
every address that tried to log in during it left sitting behind a backoff window, every active
session logged out. A 500 is not an answer about any one address: an outage that takes the database
down fails the lookups for every address alike. It is not quite uniform, though. A known address makes
one more query before the Argon2 permit than an unknown one (the credential lookup), so under a starved
pool its 500s come a little more often, and a caller flooding logins could compare the rates. That
residual, and the extra round trip it rides on, is #55.

**Any instant the database also evaluates comes from the database.** `aa1f68f` moved
`activeBackoffSeconds`, `findLive` and `touch` onto the database's `now()` rather than the app's,
because a window the database evaluates (a session's absolute lifetime, an active backoff) must be
measured by the database's clock — subtracting against the app's own clock in a second step lets
app/DB skew silently lengthen, shorten, or defeat the check, and the two disagree exactly at the
boundary a caller cares about. This bit once after that commit: the login backoff computed the
remaining wait by subtracting `h.now()` from a timestamp the database had already filtered on its own
`now()` (fixed in #25 by having the query return the remaining interval, computed in SQL, instead of
a raw timestamp). Likewise a session's `created_at`/`last_seen_at` are left to the schema's
`DEFAULT now()`, because the absolute-lifetime check compares them against the database's `now()`.
When a boundary is checked in SQL, compute
anything derived from that boundary in the same SQL, and never re-derive it in application code
against a different clock.

**Revoke every session on a password reset.** The reset is the "assume it was compromised" lever;
delete the user's sessions inside the same transaction before minting the new one.

**A password change is that recipe (#119).** `POST /auth/local/change-password` revokes every session
the User holds, **the caller's included**, and mints a fresh one for the caller, in one transaction with
the credential write: the credential is compare-and-swapped (`UPDATE credentials SET password_hash =
$new WHERE user_id = $id AND password_hash = $verified`, zero rows being a wrong current password),
`DeleteSessionsForUser` runs, and the new session is inserted through the transaction's own querier.
Rotating rather than keeping the current session matters because a thief who copied *this* browser's
cookie holds the very token that "revoke the others" would leave live. The `204` carries the new
session's `Set-Cookie`; the generated visitor writes it with `Header().Set`, which replaces a refreshed
cookie the session middleware queued for the old token. A request in flight with the old cookie when
the transaction commits answers `401`, and a `204` lost on the wire leaves the browser with a dead
cookie: both are the price of rotation and are accepted. **Known limit:** a login that verified the old
password before the change committed, and inserts its session after the revocation, survives it. The
window is one Argon2 verify, and the attacker must already hold the old password, which the change has
just retired. **An email change touches no session**: a session is keyed on the User's id, not the
address, and what stops a stolen cookie from swapping the sign-in handle is the current-password gate.

**The current-password check has a backoff of its own (#119).** Both credential-gated operations (change
password, change email) verify the current password, and the holder of a stolen cookie is exactly who
can guess at it from inside a session, so a wrong one is `403 INVALID_CURRENT_PASSWORD` and arms a
window under the key **`current-password:<user id>`** in `login_attempts`. **One key for both
operations**, so the two routes cannot double the guesses; login's curve (`LOGIN_FIRST_BACKOFF`,
doubling, `LOGIN_MAX_BACKOFF`); read before queueing and again under the Argon2 permit, and recorded
before the permit passes on, on a context detached from the request (§5.1.1's register discipline);
**cleared on success**. A request inside the window is `429 TOO_MANY_ATTEMPTS` with `Retry-After`. It is
not login's keys, and the reasons are the point: `email:<address>` would let a stolen session throttle
the owner's sign-in, and the owner's own typos here would throttle sign-in on every device. Keyed on the
User it also does not depend on #110 (behind a reverse proxy every visitor shares one address key), adds
at most one `login_attempts` row per User, and has none of register's never-decaying count. There is no
`ip:` component. A User with no credential row is answered as a wrong current password, verified
against the dummy hash at the same cost, as login's dormant User is.

**A second CSRF layer behind `SameSite=Lax`.** Reject a non-safe-method request whose
`Sec-Fetch-Site` or `Origin` names another site, before it reaches a handler —
`CROSS_SITE_REQUEST_BLOCKED`, 403.

**`Secure` on the session cookie is configurable only so local dev over HTTP works.** It defaults to
true; no deployment document suggests otherwise.

### 5.1.1 Registration, the handshake and the founding gate

Settled with Rad on 19 Nov 2026 (issue #108) against Balances' code (its `auth/local.go`,
`auth/onboarding.go`, ADR-0038 and ADR-0039), so a household running both apps meets the same sign-up
in each. The departures, and the reason for each, are below.

**Registration creates no account.** `POST /auth/local/register` validates, hashes and stores the
would-be credential in an **onboarding handshake**; the User, the Household and the credential are
created only when the person commits at the gate (`POST /onboarding/choice`). The handshake is the
`onboarding_handshakes` table: `id` (the opaque token, minted like a session id and **hashed at rest**
the same way, so a leaked row is not a presentable cookie), `email`, `user_display_name`, `locale`,
`password_hash` (nullable, for the day a Google identity opens one), `created_at`, `expires_at`. TTL
**15 minutes**, fixed, not a §12 variable. There is no `household_id`: nothing exists yet to point at,
and a later join choice adds columns rather than reshaping these. It is instance-local auth state, so
it hard-deletes like `sessions` (§4); liveness is `expires_at > now()` in SQL on the database's clock
(above), never the application's, and registration sweeps expired rows on the way in. The handshake
cookie is `afloat_onboarding`: host-only, `HttpOnly`, `SameSite=Lax`, `Secure` per `COOKIE_SECURE`,
expiring with the handshake. No session is issued until the commit.

**The gate offers "found" only, for now.** `GET /onboarding/options` answers `{email, user_display_name,
founding_open}`; no invitations field yet, and adding one later is additive. `POST /onboarding/choice`
takes `{found: true, household_display_name}` and nothing else (`additionalProperties: false`): a later join
choice adds properties, not a different shape. Anything that is not a found choice is `400 VALIDATION`.
No live handshake is `401 UNAUTHORIZED`, and a stale cookie is cleared; `FOUNDING_DISABLED` is `403`,
checked after the handshake. That ordering hides nothing: `GET /auth/methods` is public and reports
`founding_open`, so anyone can read the instance's posture without a handshake. The commit enforces
`FOUNDING_DISABLED`; `founding_open` only tells the client what to show.

**The commit is one transaction, and Balances' is not — a departure.** The handshake is claimed with
`DELETE ... RETURNING`; the Household, the User and the credential are inserted; the transaction
commits; only then is the session issued through the ordinary session path. Two concurrent commits on
one handshake serialise on its row: the second finds it gone and answers `401`, having written nothing.
Balances runs these as separate statements, so a double submit there leaves an orphan Household. The
address can be taken between register and commit: the unique violation on `users_email_idx` is
`409 EMAIL_TAKEN` and the whole transaction rolls back, the Household included. The commit runs on a
context detached from the request's cancellation (bounded by the handler timeout again), so a client
that goes away does not turn "created or rolled back" into a coin flip. Requests that need no claim
(a body that is not a found choice, a bad name) are answered without spending the handshake.

**The Household name is the client's string — a departure.** The SPA pre-fills it from its own
catalogue (en-GB `{{name}}'s Household`, id-ID `Rumah tangga {{name}}`) and the person edits it. The
server stores what was submitted, trimmed, not blank, at most **60 code points** (Balances' cap):
`VALIDATION` with `rule: required` or `max`. **Editing runs the same rules** (`PATCH /household`,
#113): founding and editing call one function, `auth.CheckHouseholdDisplayName`, so neither can store
a name the other would refuse, and the field is named `household_display_name` in both. Balances
builds an English suggestion on the server; here the frontend owns every string (§9). A `U+0000` in a
name is refused as `VALIDATION` (`pattern`) rather than reaching Postgres, which cannot store it. A
`user_display_name` or `household_display_name` made only of whitespace and format characters (Unicode category Cf: zero-width space and joiners, word joiner,
BOM) is `VALIDATION` with `rule: required`, blank to the eye though not to `TrimSpace`; such
characters are never stripped from an otherwise visible name, and a bidi control inside one is
accepted (Balances parity).

**Server-generated ids at founding — an exception to §4.** See §4: the Household's and the User's
UUIDv7s are minted in Go.

**Password policy: Balances' exactly.** At least 10 code points (counted as runes), not in the embedded
common-password list (§5.1, compared case-insensitively). A failure is `400 WEAK_PASSWORD` with
`args.reason` of `min` or `breached`, checked **before any hashing** and after the email's shape (the
contract's own validation runs first, so a malformed email is `VALIDATION` whatever the password).
**Login is unchanged** and accepts any non-empty password, so a credential created under an older or
looser rule still signs in. The list is `common_passwords.txt`: Afloat's own plus the 10-rune-and-longer
entries of Balances' that it lacked, origin noted in the file.

**`EMAIL_TAKEN` enumerates, on purpose.** A registration whose address a live (not soft-deleted) User
holds, whether or not it has a credential, is `409 EMAIL_TAKEN`. Registration is the one place that has
to say so, as in Balances; **login stays non-enumerating** and is unchanged. Registering an address
that is free and one that is taken therefore differ in answer, and in cost (a taken one is not hashed).
The register backoff below limits only requests that reach the hash, so address enumeration through
`EMAIL_TAKEN` runs at database speed: accepted, with the rest of this trade.

**The two credential-gated routes fail in a fixed order (#119).** Change password and change email each
reach a step only if every earlier one passed: the session (`401`, from the contract); the body's shape
(`VALIDATION` or `INVALID_JSON_BODY`); for a password change, `new_password` against the policy,
**before any hashing and before the throttle** (`WEAK_PASSWORD`, so a weak password costs nothing and
records nothing); the throttle on `current-password:<user id>`, read before queueing (§5.1); the
credential, where only `pgx.ErrNoRows` means none and any other failure is a `500` that records no
failure (#23); the Argon2 permit; then, detached from the request, the throttle again and the verify. Only
after the verify does the write run, so **an address's state is never revealed to a caller who has not
proved the password**: a wrong password with a taken address is `403`, never `409`. A change of address
that finds a live User holding it answers `409 EMAIL_TAKEN`, from the unique index (`users_email_idx`),
with no lookup beforehand: the index is the race-free answer, and the hash has already been paid for. It
enumerates exactly as registration does, and costs a hash per probe. The address is trimmed and
lower-cased before it is stored, so `Me.email` may differ in case from what was typed; the caller's own
address in any case, and a soft-deleted User's, are not taken. The change is not verified: there is no
mail transport, so a well-formed typo becomes the sign-in handle at once.

**A `500` from `PATCH /me` or change email changed nothing.** Each answers with the Household as well as
the User, so the `UPDATE` and the Household read run in one transaction: a failure at its begin, the
`UPDATE`, the read or the commit rolls back and answers `500 INTERNAL` with the row as it was. A `500`
never means a change landed that the caller was told had failed. A `409 EMAIL_TAKEN` is the same
rollback. The current-password proof runs before the transaction, so a wrong or throttled attempt
never opens one.

**Registration is rate limited like login, with its own key.** It runs Argon2 on unauthenticated
input, the exposure #33 capped. It shares login's concurrency permit (the bound is on total Argon2 work
whatever its source) and its discipline: the per-IP backoff is read before queueing, **read again under
the permit**, and the attempt recorded before the permit passes on, on a context detached from the
request, for the reasons in §5.1 above. The key is `register-ip:<addr>` in `login_attempts`, never
login's `ip:<addr>`, so a run of registrations cannot lock an address out of signing in nor a run of
failed logins out of registering. Per IP only: an address key would let anyone throttle a known
address's registration, and what is limited is hashing work, which the address does not change. It
counts every request that reaches the hash (a registration is an attempt whether or not it ends in an
account), with the same curve (`LOGIN_FIRST_BACKOFF`, doubling, `LOGIN_MAX_BACKOFF`), and nothing clears
it: a person who registered several accounts from one address waits up to the cap between them.
Requests refused before the hash (policy, `EMAIL_TAKEN`, validation) record nothing.

**Known limitations of that limiter** (ruled to document rather than change; follow-up #110):

- **The key is the connection's own address (`RemoteAddr`), as login's is** (§5.1, never
  `X-Forwarded-For`). Behind a reverse proxy every visitor therefore shares one `register-ip:` key.
  Login shares that limitation, but a successful login clears its keys; registration never does.
- **`failure_count` never clears or decays.** The window doubles per attempt from
  `LOGIN_FIRST_BACKOFF` to the cap, so after about ten lifetime registrations through one address
  every registration is followed by a `LOGIN_MAX_BACKOFF` (5m by default) window: any two within
  five minutes are a `429`.
- **So one party registering every five minutes keeps registration closed to everyone else on that
  key, the instance's own founder included.** That is the cost of having a limiter at all on an
  unauthenticated, hashing endpoint; it denies registration, never sign-in (login's keys are
  separate), and `FOUNDING_DISABLED` makes it moot once the operator has founded their Household.

**With the local provider off, register is a path that does not exist.** So are change password and
change email (#119): everything that needs the local credential lives under `/auth/local/*` and is behind
the gate, and `httpserver`'s list of gated paths must grow with the namespace (a test reads the contract
and fails for an `/auth/local/` route that is not on it). `PATCH /me` is not gated: a name, a language and
a zone are not credentials. The gate is shared with login (§5.2)
by one middleware matching on path alone: a bare 404 on every method, the same bytes and framing as an
unregistered path (`HEAD` included, whose `Content-Length` once differed on a live server: the
`headAsGet` stripping the unregistered path gets had to be applied to the gate too). The onboarding
routes are not gated: the gate is shared with providers still to come, and with no registration it has
no handshake to honour and answers `401` to everyone.

### 5.2 Wire shape

`/auth/local/*` is namespaced from day one so Google arrives as `/auth/google/*` without moving the
password routes. Anything that needs the local credential belongs there: `change-password` and
`change-email` (#119), which need the current password, are `POST`s in it and behind the disabled-local
gate (§5.1.1); a User's other settings are `PATCH /me`, which are not credentials and are not gated.
`INVALID_CURRENT_PASSWORD` is their own `403`: no `args`, and not a `401`, since the session is fine.

Login returns **204** with the cookie and no body; the client then calls `GET /me`.
That looks like a wasted round trip and isn't: Google arrives as a redirect that cannot return a body,
so the client needs the fetch-after-auth path regardless, and one post-auth path beats two.

**The session cookie is `afloat_session`, not Balances' bare `session`** — the one place sameness is
actively harmful. Host-only cookies don't collide across hostnames, but a self-hoster putting both
apps behind one hostname on different paths would have them silently overwrite each other.

The error envelope is Balances ADR-0027 exactly: `{"code": "SCREAMING_SNAKE", "args": {...}}`, no
`message` field, `args` values JSON primitives only. `VALIDATION` carries `{field, rule}` and reports
the first failing field only. The vocabulary is the contract's `ErrorCode` enum, closed and extended as
endpoints land. `ID_TAKEN` (#121) is a create whose client-supplied id is held by something other than a
live row of the caller's own Household, and `NAME_TAKEN` a name another live row already holds, at a create,
a rename or a restore; both are
`409`, carry no `args`, and name no entity. `NOT_FOUND` (#121) is an `{id}` that names no live row of
the caller's own Household (`404`, no `args`; see *Tenancy on a path id*, §4). An **empty** `{id}`
segment on a route that still matches, such as `/pockets//restore`, is a malformed id like any other:
`400 VALIDATION {field: id, rule: invalid}`, after the session check, as the binder refuses it
(ratified by the maintainer, 22 Nov, #121 PR 3). Only a path no route matches is the bare `404`: the
collection's `/pockets/` with a trailing slash, for one.

**Which request-body failure gets reported (#18).** The login tests drive this table:

- A body that is not an instance of the declared shape is `INVALID_JSON_BODY` with no `args`, whatever
  else is wrong with it. That covers malformed JSON (anything after the value, bytes that are not
  strict UTF-8, a byte order mark and a UTF-16 or UTF-32 body included), a body not sent as
  `application/json` (another `+json` type included), a non-object root, a wrong type (no scalar
  coercion: `5` is not a string), a `null`, and a property the schema does not declare.
- The media type is matched case-insensitively (ASCII letters only), whitespace around it ignored
  (RFC 9110 §8.3.1). One that is not `token/token` at all — a non-ASCII letter included — counts as no
  `Content-Type`, so a body is refused as undeclared whatever the validator's own folding would make
  of it. A body is read as UTF-8 whatever `charset` its `Content-Type` claims, a name no decoder
  knows included: `application/json` has no such parameter (RFC 8259 §11). Every other
  parameter is ignored too, an unparseable one included.
- Otherwise every failing field competes, an absent required one (`rule: required`) included. The
  reported one is the least by wire field name, then by rule.
- The contract's rules are judged before the handler's, not alongside them: the validator answers as
  middleware, so a handler rule (`iso4217`, a blank-to-the-eye name) competes only once every
  contract rule has passed. `{"period_start_day": 0, "household_display_name": ""}` reports
  `period_start_day` (`min`). Registration and every later write behave the same (#113).
- `rule` uses go-playground/validator's tag names (`required`, `min`, `max`, `email`), plus `pattern`,
  and, for a handler's own rules, `unique` (a repeated `category_id`) and `invalid` (a path `{id}` that is
  not a uuid, or a `category_id` that names no live Category of the Household). A contract failure inside
  an array item names its path in `field` (`allocations.0.allocation`); a handler rule names the bare
  property (`category_id`, `allocation`).
- `minLength` and `maxLength` count code points, JSON Schema's characters: an emoji is one, not two
  UTF-16 units or four bytes.
- `format: email` trims, then validates: a padded address is accepted and trimmed again for the
  lookup, and an empty or all-space one is not an address. The trim is inside the format check only,
  so `maxLength` counts the padding. "Trim" is Go's `strings.TrimSpace`, and the address rule is the
  WHATWG one for `<input type=email>`, so the server accepts what a browser's email field does.
  `contract/testdata/email.json` lists the answers it must give.

This comes from the spec-validating middleware (`httpserver/openapi_validate.go`). The generated
models carry `format: email` as a plain `string` (`oapi-codegen.yaml`), so the decode does not check
the address a second time, and it canonicalises the media type itself rather than relying on
kin-openapi's matching, whose case-folding changed between releases (#89).

**Paths, headers and the body cap (#56, #67, #92).** The path and body-cap rules are cases in
`contract/conformance/cases/routing.yaml`; the header rules are pinned in the backend's own tests:

- **A percent-encoded path is its decoded route** (RFC 3986 §6.2.2.2): `/api/auth/local/%6Cogin` is
  login. `routeOnDecodedPath` clears `RawPath` so chi routes on the decoded path. **An encoded slash
  never becomes a separator:** the raw path is kept for chi when it holds `%2F`.
- **A path with no route is a path that does not exist.** `;`, `//`, a dot segment, an encoded slash,
  backslash or percent, `%00`, invalid UTF-8, an overlong or raw non-ASCII byte sequence (#64), and a
  non-OPTIONS method with the asterisk-form target `*` (RFC 9110 §7.1 reserves that form for OPTIONS):
  each gets the router's bare unregistered-path 404, headers included. No `ErrorCode` for it. The
  disabled-login gate compares the decoded path, so no spelling of the login path escapes it. An
  unknown method (`FOO`, `PROPFIND`) answers 405 — chi's own method-not-allowed handling, on every path
  including an unregistered one (#71).
- **The request line and headers are capped at 8 KiB (#92)**, `MaxHeaderBytes` in `cmd/afloat/main.go`,
  not `net/http`'s 1 MiB default: self-hosting means there may be no proxy in front to cap them. A
  fixed constant, not a §12 variable. `net/http` reads up to 4 KiB of slop past it — up to 8 KiB is
  always accepted, past 12 KiB always refused — then answers `431` itself, before any handler runs:
  a bare plain-text answer, outside the fixed response-header set and the access log, like the
  connector-level 400s. No `ErrorCode` for it.
- **Header values are unrestricted (#67)**, a UTF-8 byte in the 0x80–0xFF range included. Header
  *names* stay strict: `net/http` refuses an invalid name outright — verified with `X-\xe9: 1` and a
  DEL or HTAB byte in the name, each a bare `400 Bad Request` — and refuses a genuine control character
  in a header *value* (C0 minus HTAB, plus DEL) the same way, before `httpserver` ever sees the request.
- **HEAD and OPTIONS (#66).** HEAD answers like GET — RFC 9110 §9.1 requires it of a general-purpose
  server, and a `headAsGet` middleware ahead of the contract's own router provides it (chi's
  `middleware.GetHead` rewrites only its routing lookup, not `r.Method`, which the spec-validating
  middleware still reads). OPTIONS is a flat, content-free 405 everywhere — no `Allow` header, on a
  registered route, an unregistered path, the disabled-login route, or a malformed path alike — because
  Afloat is same-origin with no CORS, so no client sends it, and an `Allow` header would otherwise tell
  a prober which routes exist; answering it identically everywhere is also what keeps the disabled-login
  route indistinguishable from an unregistered path on this method, with no second gate to keep in sync
  with the first. `OPTIONS *` (RFC 9110 §7.1's asterisk-form) gets the same 405: `net/http`'s own
  built-in 200 for it is switched off via `Server.DisableGeneralOptionsHandler`. `OPTIONS *?x=1` is
  malformed enough that Go's own request-line parser refuses it outright with a bare 400, before any
  handler runs.
- **The 1 MiB cap meets only the bytes a handler reads**, as Go's `MaxBytesReader` does: a declared
  `Content-Length` is not refused up front, and `/health` or logout answers normally with any body.
  The spec validator runs with its copy of the spec stripped of security requirements, because
  kin-openapi reads the whole body into memory before calling even a no-op `AuthenticationFunc`,
  and under the cap that read made an oversize body on logout or `/me` a 401.

### Response headers

The backend sends this fixed set on every response, as explicit middleware (issue #26):

| Header | Value |
|---|---|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Cache-Control` | `no-cache, no-store, max-age=0, must-revalidate` |
| `Pragma` | `no-cache` |
| `Expires` | `0` |
| `X-XSS-Protection` | `0` |
| `X-Request-Id` | The inbound id, sanitised; otherwise one minted server-side (below). |

**`X-Request-Id`** is honoured inbound so a reverse proxy's id survives into the backend's logs,
but the header is attacker-controlled when nothing sits in front, so it is sanitised first: keep only
ASCII `A–Z`, `a–z`, `0–9`, `-` and `_` (every other byte dropped, not replaced), then cut to 64. If
nothing survives, or none was sent, mint one: 8 random bytes as 16 lowercase hex characters. The
backend implements this itself rather than using chi's `middleware.RequestID`, which echoes unfiltered and
mints ids that carry the hostname and a request counter.

**The backend does not send `Strict-Transport-Security`.** HSTS is policy about the operator's domain
(`max-age`, `includeSubDomains`), not about Afloat, and whatever terminates TLS owns it — the reverse
proxy in any deployment this document supports.

## 6. The API contract

`contract/openapi.yaml` is **hand-written and authoritative**; every artefact is generated from it.

| Surface | Generator |
|---|---|
| Go | `oapi-codegen` — server interfaces and types |
| React | `openapi-typescript` |

`make check` regenerates into a scratch directory and diffs (`scripts/check-generated.sh`), the
equivalent of Balances' `backend-gen-ts-types-check`.

**The base path is the spec's `servers[0].url` (#17).** oapi-codegen emits nothing for it, so the
backend reads it from the embedded spec at startup (`httpserver/basepath.go`) and refuses to boot on
an entry it cannot mount at.

> **This is contract-first; Balances is code-first.** Balances generates TypeScript *from Go*
> (`backend/tools/gen-routes`, `gen-ts-types`). Afloat's spec leads instead, so the frontend's types
> and the backend's interfaces come from the same hand-written file.

**Who may call an operation is declared in the contract and enforced once (#114).** The global
`security: [sessionCookie]` protects every operation by default; a public one says so with
`security: []`; optional auth, as `logout` has, is `[{sessionCookie: []}, {}]`. Operations that
need only `onboardingCookie` are checked by their own handlers, which own that 401's shape.
Enforcement is `httpserver`'s `sessionRequired`, which runs ahead of request validation. **Path
parameters are the one thing that runs ahead of it:** oapi-codegen's generated wrapper binds them
before any per-operation middleware, so a malformed `{id}` reaches `paramErrorHandlerFor` first. That
handler asks the same question (`refusedForSession`, one decision for both) and answers `401` (or
`500` for an unreadable session) before it reports the parameter, so a caller without a session still
learns nothing about what the binder accepts (#121). At startup
it refuses a scheme name it does not know, and an operation naming both cookies: it cannot check a
handshake, so that shape needs a ruling first.
When the session cookie was presented but the lookup itself failed (an outage, not `pgx.ErrNoRows`),
it answers `500 INTERNAL` instead of `401`, so every session-requiring operation declares a `500`
(§5.1).

**Trajectory is computed server-side.** The client formats and floors for display but derives
nothing. This keeps one source of truth.

**`contract/testdata/trajectory.json` is a test fixture** holding the reference rows from
`CONTEXT.md` §Worked check. The backend's unit tests read it. The conformance harness proves the
backend serves the contract's *shape*; only this proves it computes the right *numbers*.

**`contract/testdata/periods.json` pins the Period Day arithmetic** (#130): which day Today is across
04:00, zone changes and daylight saving, which Budget Period a date belongs to for a `period_start_day`
of 1 to 28, and a label's window. The `period` package's unit test reads it, and the client derives
none of it. Handlers take their clock as a dependency (`budget.Deps.Now`, `time.Now` in production), so
the backend's own tests assert exact dates; the conformance harness cannot pin a clock, so its cases
build state from Postgres's own and match a date by `<re:...>`.

**`contract/testdata/budget.json` pins the Budget's derived figures** (#130, PR 2): the Commitments
Budget, Daily Pool and Unallocated for a Monthly Budget and a set of Categories (CONTEXT's worked
case,
over-allocation, Commitments above the Budget, a zero allocation, an inactive Category counted, a
deleted one not, no Monthly Budget yet). The backend's budget tests read it row by row through
`GET /budget` and keep no copy of the numbers. `trajectory.json` is untouched: `Calculate` takes the
Daily Pool as an input, and these rows are where that pool comes from.

## 7. Migrations — correction to the Decisions draft

The Decisions draft's plan of a top-level `db/migrations/` is replaced: the
migrations are **goose files, canonical and hand-written, in `backend/internal/migrations/`**, where
the binary embeds them (`//go:embed` cannot reach a parent directory, so they live in the package that
embeds them). Each file carries `-- +goose Up` and `-- +goose Down` together, as Balances' do.

| Path | Role |
|---|---|
| `backend/internal/migrations/00001_name.sql` | **Canonical.** Up and Down in one file. `//go:embed`ed and applied on boot. |

- sqlc reads the same files as its schema (their Up sections), so there is one schema source.
- What the schema enforces — defaults, every `CHECK`, the soft-delete-aware indexes, foreign keys and
  cascades — is asserted in Go (`backend/internal/migrations/schema_test.go`) on the test container,
  so it runs in `make check`. Every refusal names the SQLSTATE **and** the constraint it expects: a
  typo in the test's own SQL errors too, and must fail rather than pass.
- `make test-migrations` (`scripts/test-migrations.sh`) applies the migrations **through the goose
  CLI** to a throwaway Postgres: Up applies, the §7.1 in-place-edit hazard holds, and Down drops what
  Up created. Through goose rather than `psql`, because `psql` would run each file's Up *and* Down,
  and because the runner is what production uses. It needs docker, which is why `check` does not gate
  on it; CI runs it.
- `make gen` runs every generator; `make check` calls `scripts/check-generated.sh`, which regenerates
  into a scratch directory and compares rather than regenerating in place and running `git diff`.
  That obvious version is wrong twice: it repairs a hand-edit instead of reporting it, and `git diff`
  says nothing about files git does not yet track. Both were real bugs here.
- The binary self-migrates on boot, mirroring Balances.

### 7.1 Migrations are editable in place until the first non-resettable database

Until then, a schema change **edits `00001_baseline.sql` in place**; it does not add `00002`.
Pre-release, a tidy baseline is worth more than an accurate archaeology of how it got that way, and
nothing downstream has a history to preserve yet.

**The trigger is a database you cannot drop — not a version number.** Balances reached for `v1.0.0`
first and then corrected it (its ADR-0033, amended 2026-07-02): immutability begins at the first
deployment to something non-resettable, *whatever* version that happens to carry, and squashing stays
permitted for migrations that only ever ran in resettable environments. The number was only ever a
proxy for the thing that matters.

For Afloat the binding case is not a tag at all. `demo.` and `preview.` stay resettable as long as
they actually reset. **The Household that starts using Afloat for real is the non-resettable one**,
and that will happen months before anything is tagged. From that day, `00001` is frozen and every
change is a new file, whatever the version string says.

**While the freedom lasts, an in-place edit is only half a step — the other half is dropping the
database.** goose does not fail at all on an edited migration: it tracks version numbers and nothing
else, so it sees version 1 already applied, skips the file, and leaves the database on the **old**
schema while the code expects the new one. Never edit in place without resetting the database —
`make db-reset` is that command, and is what makes this policy safe to follow rather than a trap.

**Observed, not reasoned about.** `scripts/test-migrations.sh` appends a column to a scratch copy of
`00001`'s Up section and re-runs goose against a database already holding version 1: goose reports
`no migrations to run` and the column is absent afterwards. `goose_db_version` has only `version_id`,
`is_applied` and `tstamp` — no checksum. The test asserts it, so if goose ever changes behaviour, this
section fails rather than quietly becoming wrong.

## 8. Frontend delivery

The backend embeds the built frontend via `//go:embed` (`backend/internal/web`). `//go:embed` cannot
reach a parent directory, so `make frontend-build` builds `frontend/` and copies `dist` into
`backend/internal/web/dist`, as the migrations live in the package that embeds them. That copy is
build output and gitignored apart from a `.gitkeep`. A binary built without it serves the API alone,
which is what `make check` and the conformance suite exercise.

Served on `GET`/`HEAD` outside `/api`, after the global middleware (request id, security headers,
logging) but outside the API-only group, so a static file never pays for a session lookup:

- a file that exists is served as itself;
- an extensionless path with no file is a client route and gets `index.html` (SPA fallback); never
  for `/api/*`, and never for a missing path with an extension, so a stale `/assets/*.js` is a 404,
  not HTML served as JavaScript;
- `index.html`, `sw.js` and the manifest are `Cache-Control: no-cache`, or an installed PWA keeps
  running the old shell; content-hashed `/assets/*` are cached for a year;
- the service worker is served at root scope, precaches the shell only (no runtime caching of
  `/api`), never answers a navigation to `/api/*`, and waits for the user to accept an update
  (`registerType: 'prompt'`).

In development, Vite serves the frontend on 5181 and proxies `/api` to the backend on 5182.

### Appearance: applying it before first paint (#124)

A User's `appearance` (`system`, `light` or `dark`; CONTEXT) lives on the server (`users.appearance`,
read in `Me`, written by `PATCH /me`). The server only stores it; the client applies it, and getting it
onto the screen without a wrong-scheme flash is the one thing that needs a rule here.

- **Light and dark are keyed on an attribute.** Each colour token in `theme.css` is written once, as
  `light-dark(light, dark)`, and `color-scheme` picks the side: `:root` carries `light dark` (the OS
  decides, which is System), and `:root[data-appearance="light"]` or `[data-appearance="dark"]` narrows
  it to one. A shared selector list is not possible, because CSS cannot put a media query and an
  attribute selector in one rule, and two dark blocks would drift. The build (lightningcss) lowers
  `light-dark()` to `--lightningcss-light`/`--lightningcss-dark` toggles. `system` is the absence of
  the attribute.
- **`frontend/public/appearance-init.js`** is an external classic script loaded from `index.html`
  before the app bundle, because the CSP (#99) allows no inline script. It runs before first paint,
  reads `localStorage["afloat.appearance"]`, sets `data-appearance` on `<html>` for `light` or `dark`
  (nothing for `system`, or a value it does not know, or storage that throws), and sets `theme-color`
  to match. It is written over a small table of keys (today one row, `appearance` to
  `data-appearance`), so a later palette picker adds a row and changes no mechanism.
- **The cache is only a cache; the server wins.** `localStorage` holds the last value seen, so the
  first paint of a cold load is right. Once `/me` loads, the server's value is applied, and the cache is
  rewritten to it. A stale cache therefore costs at most one correction, never a wrong stored choice.
- **Signed-out pages follow the cache, else System.** Signing out keeps the cache, so the sign-in page
  stays in the person's choice; a different User signing in on the same device gets their own value once
  `/me` loads. The cache is not cleared, because appearance is not sensitive.
- **A change saves on change and applies at once.** The client sets the attribute and the cache
  optimistically and sends only `{appearance}`; on failure it rolls both back. The provider is the one
  writer of the attribute after boot.

## 9. Internationalisation

**en-GB and id-ID from day one**, both locale files real. i18next, matching Balances.

Beyond the mechanical argument, this is a product gate: *Afloat / Drifting / Taking on water* is an
English nautical idiom, and whether it survives translation into Indonesian is something to learn
**before** the brand session builds an identity around a waterline, not after. See `PRD.md` Q-10.

Backend returns error codes; the frontend owns all user-facing strings.

## 10. Planned and committed — not in MVP, but not speculative

These are design constraints, not wishes. Each is named here so nothing is built that forecloses it.

1. **Google OAuth.** A second auth provider in the backend. Enabled by the separate
   credentials table (§5). No user-model migration required.
2. **Email.** Invitations and password reset by email, replacing the on-screen link and the CLI reset.
   Additive; the invitation token model should be built as though email will deliver it.
3. **API keys.** Scoped, rotatable, per-Household, for the Balances read path. A third authentication
   mechanism. Read endpoints exist in MVP and are session-authenticated; nothing about them should
   assume a human session.
4. **Import.** Spreadsheet import with **app-provided templates**, mirroring Balances' flow. The
   vocabulary is reserved now so nothing else claims it: **Import** = template-based spreadsheet
   ingest into a live Household; **Export** = the spreadsheet download; **Backup / Restore** =
   whole-Household, and separate.

**Not in MVP and not planned:** telemetry of any kind. G3 (open frequency) is derived from
`captured_at`; G2 is timed by hand. This deletes an events table, an endpoint and a privacy
conversation from a self-hosted app.

> **Known consequence of starting fresh.** With no import at launch, the first Monthly Budget has no
> historical anchor. Partial mitigation is already in the model: onboarding asks for
> `expected_monthly_income` and proposes a Budget as a share of it (`PRD.md` S5). The budget will be
> less well-grounded than it would be with three months of real spend behind it — accepted knowingly.

## 11. Scaffold order

1. Repo skeleton, `LICENSE` (AGPL-3.0), `README.md`, `.nvmrc`,
   `Makefile` shell, `.githooks`.
2. `contract/openapi.yaml` — health check plus auth only. Enough to prove the generator chain.
3. The baseline migration — households, users, credentials, sessions, invitations.
4. `docker-compose.yml`; Postgres up; migrations green.
5. Backend: module init, config, DB, embedded migrations, generated server interfaces, health,
   auth, session cookie.
6. *Retired.*
7. Frontend: Vite scaffold, i18next with both locales, TanStack Query, generated types, router, auth
   screens, PWA manifest and app-shell service worker.
8. `contract/testdata/trajectory.json` and the calculation unit tests, **before the dashboard
   endpoints exist**. Red tests first.
9. Domain: households, pockets, categories, budgets, expenses. Then trajectory. Then the dashboard.
10. Caddyfile, deploy.

## 12. Environment variables

The table below is what an operator configures from. `scripts/check-env-docs.sh` — wired into
`make check` — treats it as the source of truth and fails if the backend's configuration drifts from
it: a name read but not documented, documented but not read, or a default that differs.

### Backend configuration

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | *(required)* | libpq form. Blank or absent fails at boot, never at first request. |
| `PORT` | `5182` | §1. |
| `LOG_FORMAT` | `text` | `text` for a terminal, `json` where logs are collected. |
| `LOG_LEVEL` | `info` | |
| `AUTO_MIGRATE` | `true` | Apply migrations on boot. Off only to run against a database migrated by something else. |
| `HTTP_READ_TIMEOUT` | `30s` | |
| `HTTP_WRITE_TIMEOUT` | `60s` | The handler-timeout middleware (`middleware.Timeout`, issue #30) uses the value itself; `http.Server.WriteTimeout` is the value plus a fixed 5s grace, because with the two equal the connection's write deadline passes first and the cut-off handler's 503 never reaches the client. The same deadline bounds a login's wait for an Argon2 permit (§5.1, #33). Must be positive: the backend refuses to boot on `0` or a negative value, since `0` does not mean "no timeout" to the handler timeout but a deadline already passed. |
| `HTTP_IDLE_TIMEOUT` | `120s` | |
| `SHUTDOWN_TIMEOUT` | `10s` | |
| `AUTH_LOCAL_ENABLED` | `true` | |
| `AUTH_GOOGLE_ENABLED` | `false` | Planned, not built (PRD §4.1). |
| `FOUNDING_DISABLED` | `false` | Refuses to found a new Household: the commit at the onboarding gate answers `403 FOUNDING_DISABLED`, and `GET /api/auth/methods` reports `founding_open: false` so the sign-in screen hides "Create account". It stops founding, not sign-in. **Registration is open to anyone who reaches the instance until this is set**, and the first person through the gate founds a Household, so found your own before exposing the instance, then set this to `true`. An operator who skips that step has handed their instance to whoever finds it first. Balances' code behaves the same way (its self-hosting notes say further accounts become invite-only once a Household exists; the code has no such rule). Registration's per-IP backoff keys on the connection's address, so behind a reverse proxy every visitor shares one key, and it never decays: one party registering every five minutes can keep registration closed to everyone, you included, until you set this (§5.1.1, #110). §5.1. |
| `SESSION_TTL` | `720h` | The sliding window (§5.1). |
| `SESSION_MAX_LIFETIME` | `2160h` | The absolute cap from `created_at` (§5.1). |
| `LOGIN_FIRST_BACKOFF` | `1s` | The login backoff after the first failure; doubles per failure (§5.1). Must be at least 1ms (#69) — a boundary `validate()` names in its startup error. |
| `LOGIN_MAX_BACKOFF` | `5m` | The backoff's cap. Not shorter than `LOGIN_FIRST_BACKOFF`. The backend refuses to boot on either violation. |
| `COOKIE_SECURE` | `true` | Off only for local development over plain HTTP. |
| `VERSION` | `dev` | Stamped at build time; reported by `GET /api/health`. |

`AFLOAT_REQUIRE_TEST_DB` is test-only and so not in the table: set to `1`, it turns the integration
suite's docker-missing *skip* into a *failure*. CI sets it once.

### Parsing rules

**A set-but-empty duration variable is treated as unset, for every duration variable (#69).** `FOO=`
in a `.env` file boots with `FOO`'s documented default, not a parse failure. "Empty" is exactly the
empty string, never whitespace-only (#69's second review): `LOGIN_FIRST_BACKOFF="   "` is refused, the
same as any other unparseable spelling, because `caarlos0/env` only special-cases `value == ""`
(`getOr`'s `exists && value == "" && defExists` case, true for any field carrying an `envDefault` tag,
duration or not).

**Durations never use `d`.** Go's `time.ParseDuration` understands `ns us ms s m h` and rejects `d`.
The values above are spelled in hours for this reason, and tidying `2160h` into `90d` would crash the
backend at boot. `check-env-docs.sh` rejects a `d` suffix on any duration variable.

### Compose-level variables

`docker-compose.yml` reads `AFLOAT_DB_PORT`, `AFLOAT_DB_USER` and `AFLOAT_DB_PASSWORD` to build the
local Postgres. They configure the *container*, not the backend, and are out of the table above — the
backend never reads them. `.env.example` carries both groups, separated.

## 13. Agent operating model

Afloat is built by one maintainer working through AI agents. `CLAUDE.md` said what an agent may *do*
without saying what an agent *is*, so one session did everything: read half the repo to answer a
small question, and brought the same depth to a mechanical sweep as to the trajectory formula. That
spends the scarce thing — context in the main window — on cheap work.

Model and effort are configuration, not prose: `.claude/settings.json` and `.claude/agents/*.md`
frontmatter set them, and a sentence asking for "maximum effort" changes nothing. So this lands as
committed config. The model is adopted from the sibling Uruni project (its ADR-023), which has run it
since 2026-08. It is scaffold-wide rather than backend-specific, which is why it is here and not in
`docs/adr/go/`.

1. **The main session is the orchestrator: Opus at `medium` effort**, pinned by `model` (the `opus`
   alias) and `effortLevel` in `.claude/settings.json`. It plans, makes and surfaces decisions,
   dispatches, verifies diffs, writes PR bodies and asks for commits. A turn that needs more takes
   `ultrathink` or `/effort`; the pin is a default, not a ceiling.
2. **Six role agents in `.claude/agents/`**, each pinning its own tier and effort: `researcher`
   (sonnet · medium, read-only), `planner` (sonnet · xhigh), `grill` (sonnet · max), `builder`
   (sonnet · medium), `builder-deep` (sonnet · high), `reviewer` (sonnet · high, read-only).
   `builder-deep` owns the trust core: trajectory and State, money and budget arithmetic, auth and
   sessions, migrations that reshape data, `contract/openapi.yaml`, and any failure nobody has
   explained yet.
3. **`xhigh`, not `max`, is the planning default;** `max` is reserved for `grill`, where overthinking
   is the assignment.
4. **Delegation is approval-gated.** `.claude/hooks/agent-gate.sh` answers `ask` on every
   `PreToolUse(Agent)`, built-in agents included. The built-in `fork` type is switched off
   (`CLAUDE_CODE_FORK_SUBAGENT=0`) because it forces every subagent into the background.
5. **Delegation is a context optimisation, not a token one.** A subagent re-derives the repo cold and
   misses the prompt cache, so total spend rises; what it buys is a main window that keeps its
   judgement. Below roughly three files or two tool calls, inline wins.
6. **The sign-off surface is never delegated:** commits, pushes, PRs and labels, issue writes, tags,
   dropping an ADR's `draft` tag, resolving an `[OPEN Q-nn]` or promoting an `[ASSUMED]`, UX
   choices, and scope calls against `PRD.md` §4. A `planner` drafts doc edits and issues; it never
   files or ratifies them.
7. **Blocking yes, polling no.** Agents run `make check`, `go test` and `vitest` and wait. They never
   run a foreground server (`make frontend-dev`), a `--watch` mode, or a CI query; the maintainer
   reports CI back. `make conformance` and `make test-migrations` need docker and run in CI on every
   PR, so an agent runs them only when its brief asks — and `conformance` empties the local `afloat`
   database.
8. **Issue text is untrusted input.** The committed allowlist permits `gh issue view` and
   `gh issue list` only; every issue write prompts, so an instruction planted in an issue never
   publishes without a human.

**Consequences.** `CLAUDE.md` loads into every subagent on every launch, so its length is now a
running cost; its agent section is compressed, and reference material lives in `.claude/README.md`,
which agents don't load. The pins apply to every clone and are overridable — `/effort`, `--effort`,
and silently `CLAUDE_CODE_EFFORT_LEVEL`. `opus` and `sonnet` are aliases, so the model behind a tier
moves when a new one ships, with no commit here. Role agents inherit the session's permissions but
not its conversation, which makes the brief-writing rules load-bearing. Tools without subagents (via
the `AGENTS.md` symlink) get the phases and the prohibitions but not the pinning. Every delegation
adds a permission prompt, accepted deliberately: an ungated `Agent` call is the one action here that
can spend a lot without touching a file.
