# Afloat — Bootstrap Decisions

> **Status:** Ratified 13 September 2026. This is the scaffold sheet: every cross-cutting decision
> that must be true from the first commit, because it is expensive or impossible to change later.
>
> Product scope lives in [`PRD.md`](PRD.md); domain language and arithmetic in
> [`CONTEXT.md`](CONTEXT.md); the *why* in [`VISION.md`](VISION.md). Repo/architecture reasoning is in
> `docs/scratch/Decisions Draft.md`, which this document **corrects** on migrations (§7).
>
> Backend-specific technical choices belong in `docs/adr/go/` and `docs/adr/kotlin/`, not here. This
> document is only for what both backends and the frontend must agree on.

---

## 1. Identity and naming

Settled first because it gates every identifier in the project.

| Thing | Value |
|---|---|
| Product name | **Afloat** |
| Repository | `github.com/kerti/afloat` |
| Go module | `github.com/kerti/afloat/backend` |
| Kotlin group | `dev.kerti.afloat` |
| Kotlin root project | `afloat` |
| npm package | `afloat-frontend` (private) |
| Docker images | `afloat-go`, `afloat-kotlin` |
| Databases | `afloat_go`, `afloat_kotlin` |
| Landing page | `afloat.kerti.dev` |
| Application | `app.afloat.kerti.dev` |
| Demo | `demo.afloat.kerti.dev` |
| Preview | `preview.afloat.kerti.dev` |

**Bare or suffixed is decided by the namespace, not by the language.** The table above looks
inconsistent — `backend/` is bare while its image is `afloat-go`; Kotlin takes a `-kotlin` suffix in
the tree but is bare as the Gradle root project. One rule produces all of it: **suffix where the name
would be ambiguous in its own namespace, go bare where it would not.**

- **In the repo tree**, `backend/` is the canonical implementation and the tree's default, so bare
  reads as *the* backend and `-kotlin` marks the exception.
- **In a flat global namespace** — a Docker registry, a Postgres instance — `afloat` already means
  the project, so neither backend may claim it and both are suffixed. Go is not privileged here;
  there is no tree for it to be the default of.
- **Inside `backend-kotlin/`**, Kotlin is bare again (`rootProject.name = "afloat"`): IntelliJ opens
  that directory alone, so no Go is in scope to disambiguate from.
- **In a per-file namespace** — Compose service names, for instance — `postgres`, `go` and `kotlin`
  are unambiguous and stay bare, even though the images they build are `afloat-go` / `afloat-kotlin`.

Separator follows the same logic: hyphen everywhere, underscore only for the databases, because a
hyphen in a Postgres identifier has to be double-quoted at every use site.

Stated because the values alone read as an oversight, and the next person to tidy `afloat-go` down to
`afloat` for consistency would be undoing a decision rather than finding one.

### Local ports

| Service | Port |
|---|---|
| Frontend (Vite dev server) | `5181` |
| Go backend | `5182` |
| Kotlin backend | `5183` |
| Postgres | `5184` |
| Postgres, throwaway test container | `5185` |
| Go backend, conformance `local-disabled` pair | `5186` |
| Kotlin backend, conformance `local-disabled` pair | `5187` |

**The two backends must differ.** Not a tidiness preference — Afloat's premise is two implementations
of one contract, so parity work means running both at once. A shared port makes the project's central
workflow impossible.

Chosen against three constraints, which matter more than the specific numbers:

1. **Never a service's default.** `8080`, `5173`, `5432`, `3000`, `8000` are what every project
   reaches for first, which is exactly why they collide. Spring defaults to `8080`; it gets `5183`.
2. **Never `5000` or `7000`.** macOS binds both for AirPlay Receiver, and the resulting failure
   blames your app.
3. **Between 1024 and 32767.** Above that is the ephemeral range — 49152+ on macOS, 32768+ on Linux
   — where an outbound connection can transiently hold the port, so a bind fails at random and does
   not reproduce. Afloat's Postgres was briefly on `55432`: exactly this bug, waiting.

Contiguous because one fact is easier to hold than seven, and `5181` sits above Vite's `5173` so it
still reads as the frontend. Go takes the lower backend number, matching *canonical* everywhere else.

**This is Afloat's own allocation and claims nothing about any other project.** No registry, no
reserved block, no assumption that a neighbouring repository has heard of this table. Afloat and
Balances are standalone (`VISION.md` §5); a genuinely shared convention would have to be written down
on both sides to be one at all, and that is not a coupling either app is asking for. Every port here
is overridable — `AFLOAT_DB_PORT`, `AFLOAT_TEST_DB_PORT` — so a collision with something else on the
machine is a variable, not a patch.

**Go does not mirror the Kotlin group.** Go's convention is the repository path; the JVM's is
reverse-DNS of a domain you own. A vanity import path (`kerti.dev/afloat`) would couple `go get` to
the landing page staying up, for no benefit.

**The app gets its own host.** The landing page owns the apex; if both lived there, the SPA fallback
would fight the marketing page for `/`.

**There is an "Afloat Budgeting" on the App Store.** Accepted. It is harmless for a self-hosted
household tool and a portfolio repository, and the name fits the product.

## 2. Repo layout

Per `docs/scratch/Decisions Draft.md` §3, with `db/` corrected per §7 below:

```
afloat/
├── backend/                        # Go — canonical implementation
│   └── internal/migrations/        # GENERATED goose files, committed, //go:embed
├── backend-kotlin/                 # Kotlin — learning track, opened separately in IntelliJ
├── frontend/                       # shared, backend-agnostic React app
├── contract/
│   ├── openapi.yaml                # single source of truth for the API
│   └── testdata/trajectory.json    # shared calculation fixture — see §6
├── db/
│   ├── migrations/V0001__*.sql     # CANONICAL, Flyway-native
│   ├── undo/U0001__*.sql           # goose Down source only — NOT a Flyway location
│   └── init/                       # container first-boot only — creates the second database
├── shared/                         # cross-backend runtime data, owned by neither
│   └── common_passwords.txt        # CANONICAL denylist — copied into both, see §5.1
├── docs/{adr/{go,kotlin},brand,qa}/
├── scripts/                        # repo tooling the Makefile shells out to
├── docker-compose.yml              # profiles: go | kotlin, one Postgres, two databases
└── Makefile
```

## 3. Stack

Inherited wholesale from Balances-v2 where it exists there, which is most of it.

**Frontend** — React 19, Vite, TypeScript, Tailwind 4, shadcn/Radix, TanStack Query, react-router,
Recharts, i18next, Vitest, Playwright, MSW. Node 22 (`.nvmrc`). Mobile-first; installable PWA with an
app-shell service worker (no push in MVP — see §10).

**Go backend** — Go 1.27.x. Migrations embedded via `//go:embed` and applied by the binary, mirroring
Balances.

**Kotlin backend** — Temurin 21 via SDKMAN, Gradle Kotlin DSL, Spring Boot 4.1.1, Kotest on the JUnit5
platform. Initializr dependencies: Spring Web, Spring Data JPA, PostgreSQL Driver, Validation,
Actuator, **Spring Security**, **Flyway**.

> `docs/scratch/Decisions Draft.md` omitted Spring Security and Flyway from the dependency list.
> Both must be present from the first build — Spring Security in particular is the single most
> invasive thing to retrofit into an existing Spring application.

**Database** — PostgreSQL, one instance, two databases (`afloat_go`, `afloat_kotlin`). Never one
shared schema: two migration ledgers over one schema is a corruption path, and parity is proven by
contract conformance rather than by sharing rows.

## 4. Cross-cutting data conventions

Both backends must satisfy all of these identically.

- **Primary keys:** UUIDv7, **client-generated**. Column type `uuid`.
- **Idempotent create:** `POST` accepts the client-supplied id; a replay returns **200** with the
  existing resource, a first write returns **201**. Implemented as `INSERT ... ON CONFLICT (id) DO
  NOTHING` plus a read-back.
  - *Spring Data JPA footgun:* a non-null assigned `@Id` makes `isNew()` false, so `save()` calls
    `merge()` — a SELECT before every INSERT. Implement `Persistable<UUID>` with a transient `isNew`
    flag. Both backends stay correct, so contract conformance will never catch this.
- **Money:** `DECIMAL(20,4)`, serialised as **strings** on the wire, at exactly the storage scale —
  4 decimal places, always, never trimmed and never exponent notation. `"25000000"` and `"2.5e7"` are
  both wrong for a `25000000.0000` row; `"25000000.0000"` is the only correct rendering. In Go, use
  `decimal.Decimal.StringFixed(4)`, not `.String()`, which strips trailing zeros. Jackson must be
  configured explicitly or Spring emits JSON numbers (or scientific notation) while Go emits strings.
  Worth an early conformance test.
- **Currency:** every monetary value carries its `currency` column even though the MVP UI is pinned to
  the Household's reporting currency.
- **Dates:** `occurred_on` is a `date` (the Period Day). `captured_at` is `timestamptz`. Everything
  else that is an instant is `timestamptz`.
- **Period Day is computed server-side only.** `day_starts_at` plus per-User `time_zone` is real
  arithmetic; if the frontend also derives it, the two disagree at 03:59.
- **Soft delete everywhere:** nullable `deleted_at`. Hard delete is not exposed.
  - *Scope: Household-scoped domain data.* Instance-local auth state — `sessions`, `credentials`,
    `invitations` — is exempt and hard-deletes. Session revocation **is** the row delete; a
    soft-deleted session is a live session that merely looks dead, and every read of the table would
    have to remember the filter. Taking this literally while writing `V0001__baseline.sql` produces a
    logout that does not log anyone out.
- **Tenancy:** every query touching Household-scoped data filters `household_id` **in SQL**, not only
  in middleware.
- **Constraints:** `CHECK (amount <> 0)` on expenses — negatives are valid (refunds with no recorded
  original), zero is not.
- **Error envelope:** one shape across both backends, following Balances ADR-0027. It carries
  **codes, not messages**; the frontend localises.

## 5. Authentication

**MVP: local email + password only.** Server-side session, session cookie.

Cookie attributes — **host-only** (no `Domain` attribute), `HttpOnly`, `Secure`, `SameSite=Lax`. This
is not a default worth drifting from: `demo.` and `preview.` sit under a shared parent, and a cookie
scoped to `.afloat.kerti.dev` would leak a demo session into preview.

**Session cookie rather than a bearer token, deliberately.** With two backends, a cookie keeps the
entire mechanism server-side and the client sees one opaque value. A JWT scheme means two independent
issuers that must agree on claims, signing and expiry, and nothing in the OpenAPI contract checks any
of that.

**Users carry no credential column.** Credentials live in their own table keyed by user. This is the
one design cost paid up front so that Google OAuth later is an additive filter chain rather than a
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
Balances' exact values. Kotlin uses Spring Security's `Argon2PasswordEncoder` configured to match —
**a hash written by either backend must verify in the other**, and that is worth a conformance test,
for the same reason the money encoding is (§4): both backends can be internally consistent and still
disagree, and the contract cannot see it.

**Password policy is a floor only:** minimum length plus a common-password denylist. No composition
rules. Cap the maximum length too, and put a body-size limit on JSON routes in both backends —
Balances caps only its file-upload handlers. Both lengths are counted in **Unicode code points**, not
bytes and not UTF-16 units: an Indonesian or any non-ASCII passphrase must not be penalised for
encoding wider. Go counts runes; Kotlin must use `codePointCount`, because `String.length` measures
an astral character twice and would let a nine-character passphrase through.

**The denylist is one file, `shared/common_passwords.txt`.** Both backends reject the same passwords
or they are not the same app, and a denylist that drifts is a divergence no contract test can see. It
is owned by neither backend and copied into both by `scripts/sync-denylist.sh`, gated by `make check`
— neither backend can read it where it lives, because Go's `//go:embed` cannot reference a parent
directory and the Kotlin backend has to carry it inside the jar. The copies are generated output;
edit the canonical file.

**Session tokens are hashed at rest.** The cookie carries a 256-bit random, URL-safe value; the
`sessions` primary key is its SHA-256. A database leak yields nothing usable. Hash before every read
and write; the cookie keeps the plaintext, or the next lookup never matches.

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

**The rate-limit key is the connection's own address, never `X-Forwarded-For`.** Self-hosting means
there may be no proxy in front, so nothing strips that header and it is attacker-controlled — using
it means an attacker picks a fresh key per request and the per-IP backoff stops existing. Whatever
each backend's equivalent of chi's `RealIP` middleware is, it stays unmounted. A deployment that does
put a trusted proxy in front changes this behind a config flag, never by default.

**Login backoff lives in Postgres — a departure, and the load-bearing one.** Per-IP and per-email
exponential backoff, capped, returning `429` with `Retry-After`. Backoff, never a hard lockout: a
lockout on a self-hosted household app is a footgun. Balances keeps the limiter in process memory,
which is right for one backend and wrong for two — duplicated stateful logic is exactly the class of
divergence contract conformance cannot catch (§4's Jackson footgun again). Two backends could
disagree on the backoff curve, on key normalisation, or on eviction, and every contract test would
still pass. A table keyed by ip/email with a `backoff_until` is identical by construction, testable,
and survives a restart, which the in-memory version does not.

**Any instant the database also evaluates comes from the database.** `aa1f68f` moved
`activeBackoffSeconds`, `findLive` and `touch` onto the database's `now()` rather than the app's,
because a window the database evaluates (a session's absolute lifetime, an active backoff) must be
measured by the database's clock — subtracting against the app's own clock in a second step lets
app/DB skew silently lengthen, shorten, or defeat the check, and the two disagree exactly at the
boundary a caller cares about. This bit both sides once each after that commit: Go's login backoff
computed the remaining wait by subtracting `h.now()` from a timestamp the database had already
filtered on its own `now()` (fixed in #25 by having the query return the remaining interval, computed
in SQL, instead of a raw timestamp); Kotlin's session `INSERT` supplied `created_at`/`last_seen_at`
from the app clock while the absolute-lifetime check compares them against the database's `now()`
(#25). Neither error was visible on the wire — the second was a persisted-state divergence only, with
nothing in a response to show which clock wrote the row. When a boundary is checked in SQL, compute
anything derived from that boundary in the same SQL, and never re-derive it in application code
against a different clock.

**Revoke every session on a password reset.** The reset is the "assume it was compromised" lever;
delete the user's sessions inside the same transaction before minting the new one.

**A second CSRF layer behind `SameSite=Lax`.** Reject a non-safe-method request whose
`Sec-Fetch-Site` or `Origin` names another site, before it reaches a handler —
`CROSS_SITE_REQUEST_BLOCKED`, 403.

**`Secure` on the session cookie is configurable only so local dev over HTTP works.** It defaults to
true; no deployment document suggests otherwise.

### 5.2 Wire shape

`/auth/local/*` is namespaced from day one so Google arrives as `/auth/google/*` without moving the
password routes. Login returns **204** with the cookie and no body; the client then calls `GET /me`.
That looks like a wasted round trip and isn't: Google arrives as a redirect that cannot return a body,
so the client needs the fetch-after-auth path regardless, and one post-auth path beats two.

**The session cookie is `afloat_session`, not Balances' bare `session`** — the one place sameness is
actively harmful. Host-only cookies don't collide across hostnames, but a self-hoster putting both
apps behind one hostname on different paths would have them silently overwrite each other.

The error envelope is Balances ADR-0027 exactly: `{"code": "SCREAMING_SNAKE", "args": {...}}`, no
`message` field, `args` values JSON primitives only. `VALIDATION` carries `{field, rule}` and reports
the first failing field only.

### Response headers

Both backends send this fixed set on every response, as explicit middleware on Go and Spring
Security's defaults on Kotlin (issue #26):

| Header | Value |
|---|---|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Cache-Control` | `no-cache, no-store, max-age=0, must-revalidate` |
| `Pragma` | `no-cache` |
| `Expires` | `0` |
| `X-XSS-Protection` | `0` |
| `X-Request-Id` | The inbound id, sanitised; otherwise one minted server-side (below). |

**`X-Request-Id`** is honoured inbound so a reverse proxy's id survives into both backends' logs,
but the header is attacker-controlled when nothing sits in front, so it is sanitised first: keep only
ASCII `A–Z`, `a–z`, `0–9`, `-` and `_` (every other byte dropped, not replaced), then cut to 64. If
nothing survives, or none was sent, mint one: 8 random bytes as 16 lowercase hex characters. Go
implements this itself rather than using chi's `middleware.RequestID`, which echoes unfiltered and
mints ids that carry the hostname and a request counter.

**Neither backend sends `Strict-Transport-Security`.** HSTS is policy about the operator's domain
(`max-age`, `includeSubDomains`), not about Afloat, and whatever terminates TLS owns it — the reverse
proxy in any deployment this document supports. Kotlin disables Spring Security's HSTS writer
explicitly: it only fires on a secure request, so it is silent today, but it would start sending
Spring's default the day someone enabled TLS on Tomcat or trusted forwarded headers.

## 6. The API contract

`contract/openapi.yaml` is **hand-written and authoritative**; every artefact is generated from it.

| Surface | Generator |
|---|---|
| Go | `oapi-codegen` — server interfaces and types |
| Kotlin | `org.openapi.generator` Gradle plugin, `kotlin-spring`, `interfaceOnly=true` |
| React | `openapi-typescript` |

CI regenerates and runs `git diff --exit-code`, mirroring Balances' `backend-gen-ts-types-check`.

> **This is contract-first; Balances is code-first.** Balances generates TypeScript *from Go*
> (`backend/tools/gen-routes`, `gen-ts-types`). That tooling cannot be lifted across — with two
> backends the spec has to lead.

**Trajectory is computed server-side, in both backends.** The client formats and floors for display
but derives nothing. This keeps one source of truth, and it puts real domain logic in the Kotlin
track rather than leaving it a CRUD shell.

**`contract/testdata/trajectory.json` is a shared test fixture** holding the reference rows from
`CONTEXT.md` §Worked check. Both backends' unit tests read it. Contract conformance proves the two
backends have the same *shape*; only this proves they compute the same *numbers* — the "behavioural
edge cases within an identical schema" gap the Decisions draft names and leaves unaddressed.

## 7. Migrations — correction to the Decisions draft

The plan of one `db/migrations/` consumed by both Flyway and goose **does not work**. Evidence from
Balances: every goose file carries `-- +goose Up` *and* `-- +goose Down` in the same file. Flyway has
no concept of those markers and would execute the whole file, undo section included. Naming differs
(`00001_x.sql` vs `V1__x.sql`), and `goose_db_version` / `flyway_schema_history` are independent
ledgers.

**The scheme:**

| Path | Role |
|---|---|
| `db/migrations/V0001__name.sql` | **Canonical.** Up only. Flyway-native. |
| `db/undo/U0001__name.sql` | Down only. Source for the goose generator. |
| `backend/internal/migrations/00001_name.sql` | **Generated**, committed, `//go:embed`ed. |

- `make gen-goose-migrations` (`scripts/gen-goose-migrations.sh`) concatenates each `V`/`U` pair into
  the goose file with the markers inserted. `make check` regenerates into a scratch directory and
  diffs, so a stale generated file and a hand-edited one both fail the same way; CI mirrors it.
- `make test-migrations` (`scripts/test-migrations.sh`) applies the canonical SQL to a throwaway
  Postgres and asserts the schema behaves — defaults, every `CHECK`, the soft-delete-aware indexes,
  cascade behaviour, and that the undo drops what the migration created. It runs the SQL through
  `psql` rather than through either runner, so it is meaningful before either backend exists. It
  needs docker, which is why `check` does not gate on it.
- `make gen` runs every generator; `make check` calls `scripts/check-generated.sh`, which regenerates
  into a scratch directory and compares rather than regenerating in place and running `git diff`.
  That obvious version is wrong twice: it repairs a hand-edit instead of reporting it, and `git diff`
  says nothing about files git does not yet track. Both were real bugs here.
- `make test-migration-runners` (`scripts/test-migration-runners.sh`) runs the **runners** instead:
  Flyway against `db/migrations/` into `afloat_kotlin`, goose against the generated files into
  `afloat_go`, then compares a fingerprint of every table, constraint and index in both (excluding
  each runner's own ledger) and requires them identical. This is the gate `test-migrations` cannot
  be: it is what catches a generator that drops a statement, or the two runners disagreeing about
  what has already been applied. Flyway runs from its official image and goose via `go run`, so
  neither backend needs to exist.
- Kotlin: Gradle `processResources` copies `db/migrations/` into
  `src/main/resources/db/migration`, so Flyway's default classpath location applies — the same
  mechanism already planned for `frontend/dist`.
- **`db/undo/` is deliberately not a Flyway location.** Flyway's `U__` undo migrations are a paid
  feature; keeping the files outside the scanned path avoids a Teams-required error and sets correct
  expectations: `flyway undo` is not available, and the undo files exist solely to feed goose.
- Both binaries self-migrate on boot, mirroring Balances.

### 7.1 Migrations are editable in place until the first non-resettable database

Until then, a schema change **edits `V0001__baseline.sql` and its `U0001` in place** and regenerates;
it does not add `V0002`. Pre-release, a tidy baseline is worth more than an accurate archaeology of
how it got that way, and nothing downstream has a history to preserve yet.

**The trigger is a database you cannot drop — not a version number.** Balances reached for `v1.0.0`
first and then corrected it (its ADR-0033, amended 2026-07-02): immutability begins at the first
deployment to something non-resettable, *whatever* version that happens to carry, and squashing stays
permitted for migrations that only ever ran in resettable environments. The number was only ever a
proxy for the thing that matters.

For Afloat the binding case is not a tag at all. `demo.` and `preview.` stay resettable as long as
they actually reset. **The Household that starts using Afloat for real is the non-resettable one**,
and that will happen months before anything is tagged. From that day, `V0001` is frozen and every
change is a new `V####`, whatever the version string says.

**While the freedom lasts, an in-place edit is only half a step — the other half is dropping both
databases.** The two ledgers do not fail the same way, and one does not fail at all:

- **Flyway (Kotlin) fails loudly.** It recomputes each applied migration's checksum against
  `flyway_schema_history`, finds the mismatch, and refuses to start. Impossible to miss.
- **goose (Go) does not fail at all.** It tracks version numbers and nothing else, so it sees
  version 1 already applied, skips the file, and leaves the database on the **old** schema while the
  code expects the new one.

So an edit that Kotlin refuses to start against is one Go will happily run on the wrong schema. Never
edit in place without resetting both databases — `make db-reset` is that command, and is what makes
this policy safe to follow rather than a trap.

**Both halves are observed, not reasoned about.** `scripts/test-migration-runners.sh` appends a
column to `V0001`, regenerates, and re-runs both runners against databases already holding version 1:
Flyway reports `Migration checksum mismatch for migration version 0001` and refuses; goose reports
`no migrations to run` and the column is absent from `afloat_go` afterwards. The schemas confirm why —
`flyway_schema_history` carries a `checksum` column and `goose_db_version` has only `version_id`,
`is_applied` and `tstamp`. The test asserts all of it, so if either runner ever changes behaviour,
this section fails rather than quietly becoming wrong.

## 8. Frontend delivery

Both backends embed `frontend/dist` — Gradle `processResources` into `src/main/resources/static`, Go
via `//go:embed`. Both must implement SPA fallback **identically**: unknown path → `index.html`, but
never for `/api/*` and never for the service worker file, which must be served at root scope with
correct cache headers or the PWA silently stops updating. This is non-HTTP parity and belongs in the
hand-maintained section of the parity matrix.

## 9. Internationalisation

**en-GB and id-ID from day one**, both locale files real. i18next, matching Balances.

Beyond the mechanical argument, this is a product gate: *Afloat / Drifting / Taking on water* is an
English nautical idiom, and whether it survives translation into Indonesian is something to learn
**before** the brand session builds an identity around a waterline, not after. See `PRD.md` Q-10.

Backend returns error codes; the frontend owns all user-facing strings.

## 10. Planned and committed — not in MVP, but not speculative

These are design constraints, not wishes. Each is named here so nothing is built that forecloses it.

1. **Google OAuth.** A second Spring Security filter chain / Go middleware. Enabled by the separate
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

1. Repo skeleton, `LICENSE` (AGPL-3.0), `README.md` explaining the two-backend arrangement, `.nvmrc`,
   `Makefile` shell, `.githooks`.
2. `contract/openapi.yaml` — health check plus auth only. Enough to prove the generator chain.
3. `db/migrations/V0001__baseline.sql` — households, users, credentials, sessions, invitations.
   `make gen-goose-migrations`.
4. `docker-compose.yml` with profiles and both databases; Postgres up; both migration paths green.
5. Go backend: module init, config, DB, embedded migrations, generated server interfaces, health,
   auth, session cookie.
6. Kotlin backend: Initializr with the §3 dependency list, Flyway resource copy, generated interfaces,
   the same endpoints. IntelliJ opens `backend-kotlin/` alone.
7. Frontend: Vite scaffold, i18next with both locales, TanStack Query, generated types, router, auth
   screens, PWA manifest and app-shell service worker.
8. `contract/testdata/trajectory.json` and the calculation unit tests **in both backends, before the
   dashboard endpoints exist**. Red tests first — this is the parity gate.
9. Domain: households, pockets, categories, budgets, expenses. Then trajectory. Then the dashboard.
10. `make parity-matrix` wiring, Caddyfile, deploy.

## 12. Environment variables

**Both backends read the same variable names, with the same defaults and the same meanings.** An
operator who has configured one has configured the other; switching a deployment from Go to Kotlin
is a change of image, not a rewrite of the environment.

This is the operator-facing half of the two-backend premise, and it is a *naming* contract, not a
shared runtime artefact. There is deliberately no config file both backends read: that would be a
format, two parsers, a deployment artefact and a new failure mode in which one backend refuses to
boot on a file the other accepts. Numeric agreement is proven by the shared fixtures in §6, not by
shared state.

`scripts/check-env-parity.sh` — wired into `make check` — treats the table below as the source of
truth and fails if either backend's configuration drifts from it.

### Backend configuration

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | *(required)* | libpq form. Blank or absent fails at boot, never at first request. |
| `PORT` | `5182` Go / `5183` Kotlin | The one deliberate difference — §1 requires the two to differ. |
| `LOG_FORMAT` | `text` | `text` for a terminal, `json` where logs are collected. |
| `LOG_LEVEL` | `info` | |
| `AUTO_MIGRATE` | `true` | Apply migrations on boot. Off only to run against a database migrated by something else. |
| `HTTP_READ_TIMEOUT` | `30s` | |
| `HTTP_WRITE_TIMEOUT` | `60s` | Go: enforced twice with one value — `http.Server.WriteTimeout` and the handler-timeout middleware (`middleware.Timeout`, issue #30). Kotlin: the transaction manager's default timeout (`JpaTransactionManager.defaultTimeout`, issue #30) — a deadline on the whole transaction, not a per-statement cap: each statement gets the time left as its JDBC `queryTimeout`, and Postgres cancels one that overruns it. It covers every repository call, because each repository interface carries `@Transactional(readOnly = true)` (declared query methods get no transaction otherwise, and so no deadline), and every `@Transactional` service method. It deliberately does not cover the health probe's raw `SELECT 1`. Rounded up to whole seconds, minimum 1s, since JPA timeouts count in seconds; Go's is exact. A documented deliberate difference, not a gap to close the same way Go's was. |
| `HTTP_IDLE_TIMEOUT` | `120s` | |
| `SHUTDOWN_TIMEOUT` | `10s` | |
| `AUTH_LOCAL_ENABLED` | `true` | |
| `AUTH_GOOGLE_ENABLED` | `false` | Planned, not built (PRD §4.1). |
| `SESSION_TTL` | `720h` | The sliding window (§5.1). |
| `SESSION_MAX_LIFETIME` | `2160h` | The absolute cap from `created_at` (§5.1). |
| `COOKIE_SECURE` | `true` | Off only for local development over plain HTTP. |
| `VERSION` | `dev` | Stamped at build time; reported by `GET /api/health`. |

`AFLOAT_REQUIRE_TEST_DB` is test-only and so not in the table: set to `1`, it turns the integration
suites' docker-missing *skip* into a *failure*, in both backends. CI sets it once.

### The two that do not share cleanly

**`DATABASE_URL` is given in libpq form and the Kotlin backend translates it.** Go's `pgx` takes
`postgres://user:pass@host:5184/afloat_kotlin`; JDBC needs `jdbc:postgresql://…` and Spring will not
accept the libpq form. Translating at boot is required rather than optional: `DATABASE_URL` is the
most operator-visible setting there is, and a deployment where it alone differs by backend makes the
rest of this section a half-truth.

**Open: the libpq Unix-socket form is unresolved.** `pgx` accepts the hostless socket spelling,
`postgres:///afloat?host=/var/run/postgresql`; Kotlin's `PostgresUrlTranslator` requires a host and
rejects it. That may be the right answer — a deployment without a proxying host in front is already
an unusual shape — but it is currently an accident of `urlPattern`, not a decision anyone made. Needs
a ruling either way before this parity contract can claim it (#32 item 7).

**Durations use the suffixes both runtimes accept, and never `d`.** Go's `time.ParseDuration`
understands `ns us ms s m h` and rejects `d`; Spring's simple duration style accepts `d` as well. So
`720h` works everywhere and `30d` boots Kotlin and crashes Go. The values above are spelled in hours
for this reason, and tidying `2160h` into `90d` would be undoing a decision rather than finding one.
`check-env-parity.sh` rejects a `d` suffix on any duration variable.

### Compose-level variables

`docker-compose.yml` reads `AFLOAT_DB_PORT`, `AFLOAT_DB_USER` and `AFLOAT_DB_PASSWORD` to build the
local Postgres. They configure the *container*, not either backend, and are out of scope for the
parity contract above — a backend never reads them. `.env.example` carries both groups, separated.
