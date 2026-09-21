# Spring Data JPA conventions

`draft` — no code has shipped behind this yet. It lands with `BOOTSTRAP.md` §11 step 6
(issues #13, #14) and the tag comes off when step 9's domain tables are reading through it.

The Kotlin backend uses **Spring Data JPA**, per `BOOTSTRAP.md` §3, with the conventions below. The
Go backend uses `sqlc` over hand-written SQL (`go/0002`). That asymmetry is deliberate and this ADR
exists to keep it from becoming a correctness gap.

## Why not mirror sqlc

The obvious move — hand-write every query in Kotlin too, so both backends read the same way — is
wrong twice.

It mirrors an **implementation idiom** rather than a behaviour. What the project requires is that
the two backends produce identical responses and identical numbers; `BOOTSTRAP.md` §6 proves the
first with contract conformance and the second with shared fixtures. Neither cares how a row was
fetched. `CLAUDE.md` names the opposite failure directly: code "copied without adapting to the
target backend's idioms".

And it discards the point of the track. `BOOTSTRAP.md` §2 makes the Kotlin backend a learning
track; a JPA dependency used as a connection pool teaches nothing that `sqlc` has not already
taught on the Go side.

## What JPA costs, and the rules that pay for it

`go/0002` is right that two of Afloat's non-negotiables are SQL-shaped and go silently wrong through
an ORM. JPA is kept, so those two need rules rather than an abstraction to hide behind.

### 1. `ddl-auto` is `validate`. Never `update`, never `create`

Flyway owns the schema (`BOOTSTRAP.md` §7). A JPA-generated column is schema drift with no ledger
entry, on a project whose migration policy assumes the ledger is complete.

`validate` is not merely the safe setting, it is a **gain over `sqlc`**: an entity that disagrees
with the migration fails the application at boot. `sqlc` catches the same class of error at
generation time; nothing else in the Kotlin build would catch it at all.

### 2. `household_id` appears in explicit query text, always

`BOOTSTRAP.md` §4: every query touching Household-scoped data filters `household_id` **in SQL**, not
only in middleware. In Kotlin that means:

- A tenant-scoped read is a repository method carrying an explicit `@Query` — JPQL or native — in
  which the `household_id` predicate is visible **at the method**, next to its name.
- A derived query method must never be the thing that scopes a tenant. `findByHouseholdIdAnd…` is
  technically compliant and still banned: the filter then lives in a method name, where a rename or
  a refactor drops it silently and nothing fails.
- Tenancy must never be ambient. No Hibernate filter enabled per session, no `@TenantId`, no
  interceptor that appends a predicate. An ambient tenant filter is precisely the "middleware only"
  failure §4 forbids, wearing a Hibernate hat: every query looks correct in isolation, and the one
  code path that runs outside the filter's scope is invisible.
- **Reading the tenant root by its own primary key is the one exception.** `findById(householdId)`
  on `HouseholdRepository` needs no `@Query`, because there the tenant key *is* the identifier: a
  rename cannot drop the predicate the way it can from a `findByHouseholdIdAnd…` method name, and
  `@SQLRestriction` supplies the `deleted_at IS NULL` half. `GET /api/me` is the first and currently
  only case (`AuthService.me`), and Go spells the same read out longhand as
  `WHERE id = $1 AND deleted_at IS NULL` (`GetHouseholdByID`). Any read of a table that merely
  *carries* a `household_id` column stays under the rule above.

The distinction is that a reviewer must be able to confirm tenancy by reading the repository, the
same property `go/0002` values in written SQL.

### 3. Soft delete may be ambient; tenancy may not

`@SQLRestriction("deleted_at IS NULL")` on a Household-scoped entity is **allowed**. Soft delete is a
static, global rule with one correct answer everywhere (`BOOTSTRAP.md` §4), and expressing it once on
the entity is better than repeating it in every predicate, where it can be forgotten.

Tenancy is per-request and per-user and has no such single answer, which is why it does not get the
same treatment.

Where an entity carries `@SQLRestriction`, any native query against that table must restate
`deleted_at IS NULL` itself — the restriction applies to Hibernate-managed loads, not to native SQL.
That asymmetry is the cost of the convenience, and it is the reason this rule stops at soft delete.

### 4. Derived query methods are fine where neither applies

`credentials`, `sessions` and `login_attempts` are instance-local auth state: not Household-scoped,
exempt from soft delete (`BOOTSTRAP.md` §4), hard-deleting. Derived methods over them carry no risk
this ADR is about, and writing `@Query` for `findByEmail` is ceremony.

### 5. Idempotent create implements `Persistable<UUID>`

`BOOTSTRAP.md` §4 already records this and it is repeated here because it is the rule most likely to
be met by a plausible-looking `save()`. Primary keys are client-generated UUIDv7, so a non-null
assigned `@Id` makes Spring Data's `isNew()` false and `save()` issues `merge()` — a `SELECT` before
every `INSERT`. The contract requires `INSERT ... ON CONFLICT (id) DO NOTHING` plus a read-back, with
**201** on a first write and **200** on a replay.

Both backends stay internally consistent while disagreeing about this, and contract conformance
cannot see it, because the status codes can be made to match while the write semantics differ under
concurrency. No domain `POST` exists before step 9; the rule is recorded now because step 9 is where
it gets decided by whoever writes the first repository.

### 6. Money is `BigDecimal`, mapped to `DECIMAL(20,4)`, serialised as a string

`BOOTSTRAP.md` §4 and `CLAUDE.md` non-negotiable 1. The serialisation half is global Jackson
configuration rather than per-field annotations (issue #13): per-field means every future money
field is another chance to forget. The mapping half is the column definition and the entity's scale
agreeing — which `ddl-auto: validate` checks, as long as rule 1 holds.

Two things are true at once here and it is worth being explicit, because they look like a
contradiction. Every money field in `contract/openapi.yaml` is declared `type: string`, so the
generated models carry `String` and a handler converts with `toPlainString()` — that is the path
`GET /api/me` actually takes, and it is the same thing Go does with `Decimal.String()`. The global
`BigDecimal` serialiser (`config/MoneySerializationConfiguration`) therefore sits under all of that
rather than on the live path: it is the floor for the first hand-written DTO, the first projection,
or a generator change that yields a `BigDecimal` property. It is configured and asserted now because
the alternative is discovering in review that one of those three happened.

## Consequences

- A tenant-scoped repository is more verbose than idiomatic Spring Data. That is the trade: the
  verbosity is where the review happens.
- `@SQLRestriction` and native queries interact badly, and this ADR permits the combination. The
  mitigation is rule 3's restatement requirement, which is a convention, not a mechanism. If that
  proves insufficient at step 9, the answer is to drop `@SQLRestriction` rather than to add a
  mechanism.
- Two backends now have genuinely different data-access shapes, so a bug fixed in one does not
  suggest where to look in the other. This is accepted: `BOOTSTRAP.md` §6 already places the parity
  guarantee in the contract and in shared fixtures, not in shared structure.

## Alternatives rejected

**Hand-written SQL via `JdbcTemplate` throughout.** Maximum parity with `sqlc`, zero learning value,
and it leaves the JPA dependency in `BOOTSTRAP.md` §3 unused. Rejected above.

**JPA with no rules, relying on review.** This is what the two SQL-shaped non-negotiables make
unsafe: both failures compile, pass their own tests, and reach production looking correct.

**A tenant-aware base repository that appends `household_id` automatically.** The most attractive
option and the most dangerous: it makes tenancy invisible and correct in the common case, which is
exactly the state in which the uncommon case is never noticed. Rejected as rule 2.
