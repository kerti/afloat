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
│   └── undo/U0001__*.sql           # goose Down source only — NOT a Flyway location
├── docs/{adr/{go,kotlin},brand,qa}/
├── docker-compose.yml              # profiles: go | kotlin, one Postgres, two databases
└── Makefile
```

## 3. Stack

Inherited wholesale from Balances-v2 where it exists there, which is most of it.

**Frontend** — React 19, Vite, TypeScript, Tailwind 4, shadcn/Radix, TanStack Query, react-router,
Recharts, i18next, Vitest, Playwright, MSW. Node 22 (`.nvmrc`). Mobile-first; installable PWA with an
app-shell service worker (no push in MVP — see §10).

**Go backend** — Go 1.26. Migrations embedded via `//go:embed` and applied by the binary, mirroring
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
- **Money:** `DECIMAL(20,4)`, serialised as **strings** on the wire. Jackson must be configured
  explicitly or Spring emits JSON numbers (or scientific notation) while Go emits strings. Worth an
  early conformance test.
- **Currency:** every monetary value carries its `currency` column even though the MVP UI is pinned to
  the Household's reporting currency.
- **Dates:** `occurred_on` is a `date` (the Period Day). `captured_at` is `timestamptz`. Everything
  else that is an instant is `timestamptz`.
- **Period Day is computed server-side only.** `day_starts_at` plus per-User `time_zone` is real
  arithmetic; if the frontend also derives it, the two disagree at 03:59.
- **Soft delete everywhere:** nullable `deleted_at`. Hard delete is not exposed.
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

- `make gen-goose-migrations` concatenates each `V`/`U` pair into the goose file with the markers
  inserted. CI runs it and `git diff --exit-code`.
- Kotlin: Gradle `processResources` copies `db/migrations/` into
  `src/main/resources/db/migration`, so Flyway's default classpath location applies — the same
  mechanism already planned for `frontend/dist`.
- **`db/undo/` is deliberately not a Flyway location.** Flyway's `U__` undo migrations are a paid
  feature; keeping the files outside the scanned path avoids a Teams-required error and sets correct
  expectations: `flyway undo` is not available, and the undo files exist solely to feed goose.
- Both binaries self-migrate on boot, mirroring Balances.

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
