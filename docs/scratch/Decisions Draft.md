# Afloat — Architecture & Setup Decisions

Summary of planning session, September 11, 2026. Covers environment setup and the
repo/architecture decisions for the Afloat project. Intended as a handoff reference
for Claude Code sessions and future-you.

---

## 1. Context and goals

- **Primary goal:** learn Kotlin backend development to a level usable for corporate
  EM/backend job applications, by hand-rolling a real backend rather than following
  toy tutorials.
- **Secondary goal:** ship a genuinely useful personal-finance-adjacent app (Afloat)
  that replaces a Google Sheets workflow currently in use — Balances already covers
  net-worth tracking, so Afloat serves a different need.
- **Constraint accepted explicitly:** the Kotlin track is allowed to progress slower
  than the Go track, or even stall Go progress entirely, without that being treated
  as a problem. Learning velocity on Kotlin takes priority over shipping speed on Go.
- Frontend (React) will rely heavily on Claude Code; backend (both Kotlin and Go)
  will be substantially hand-rolled, since backend implementation is the actual
  learning objective.

---

## 2. Workstation setup (MacBook Air M4, 16GB)

| Tool | Choice | Why |
|---|---|---|
| JDK | Temurin 21 (LTS) via SDKMAN | SDKMAN is the standard way to manage JVM versions on macOS and allows per-project version switching later. JDK 21 is the current LTS with the widest tooling/library support — newer 25/26 options in Spring Initializr were intentionally not chosen. |
| Build tool | Gradle (Kotlin DSL) | Modern default over Maven or Groovy Gradle; gives real Kotlin autocomplete in the build file itself. |
| IDE | IntelliJ IDEA Community Edition | Same vendor as Kotlin; Community is sufficient to start. Ultimate would add Spring/Ktor-specific tooling but isn't required. |
| Containers | OrbStack | Already installed; will run Postgres and (later) both backend images. |
| Shell | zsh (default, unchanged) + Homebrew bash 5 installed separately | macOS ships bash 3.2 for licensing reasons (GPL). SDKMAN requires bash ≥4. Homebrew's bash was installed alongside zsh rather than replacing it — zsh remains the default login shell; bash 5 is only invoked explicitly (e.g. by SDKMAN's installer script) or via shebang. |

### Framework choice: Spring Boot (not Ktor)

- Ktor is JetBrains-native, lightweight, and closer in feel to how a Go service is
  built (explicit, unopinionated).
- Spring Boot is heavier and more opinionated, but is what the large majority of
  Kotlin backend job postings assume — chosen specifically because of the
  job-market relevance goal, not because it's technically superior for this project.

### Initial Spring Initializr configuration

- Gradle - Kotlin, Kotlin language, Spring Boot 4.1.1 (stable — deliberately not a
  SNAPSHOT/M1 build), Java 21, Jar packaging (not War — War is only relevant for
  deploying into an external servlet container, which is legacy), Properties
  config format initially (acceptable to migrate to YAML later as config grows).
- Dependencies: Spring Web, Spring Data JPA, PostgreSQL Driver.
- Recommended additions for later: Spring Boot Actuator (health/metrics — cheap to
  add early rather than bolted on later) and Validation (for `@Valid` DTOs).

### Testing

- **Kotest** chosen for tests, added as Gradle dependencies (not a system-level
  install): `kotest-runner-junit5`, `kotest-assertions-core`, `kotest-property`,
  and `kotest-extensions-spring` for Spring integration. Runs on the JUnit5
  platform, which Spring Boot already defaults to (`useJUnitPlatform()`).
- IntelliJ Kotest plugin optional — nicer gutter icons and test templates, not
  required for tests to run.

---

## 3. The core architectural pivot: one repo, two backends, one frontend

### Original plan (superseded)

Two separate repos — `afloat` (Go + React, the "real"/cheap-to-run product) and
`afloat-kotlin` (Kotlin + React, the learning project) — developed independently,
with product-definition docs drafted once in `afloat-kotlin` and then copied over
to `afloat` when ready.

### Why this was reconsidered

The only meaningful difference between the two versions is the backend
implementation. If the API contract is identical, the React frontend can be
**exactly the same code** serving either backend. Maintaining two full repos with
duplicated frontend code, duplicated docs, and duplicated CI would be pure
overhead once this was recognized.

### Decision: single monorepo, two interchangeable backends

```
afloat/
├── backend/            # Go — canonical implementation
├── backend-kotlin/     # Kotlin — learning track, opened separately in IntelliJ
├── frontend/           # shared, backend-agnostic React app
├── contract/
│   └── openapi.yaml    # single source of truth for the API contract
├── db/
│   └── migrations/     # canonical SQL migrations, consumed by both backends
├── docs/
│   ├── adr/
│   │   ├── go/         # technical decisions specific to the Go implementation
│   │   └── kotlin/     # technical decisions specific to the Kotlin implementation
│   ├── PRD.md           # product-only, implementation-agnostic
│   ├── CONTEXT.md       # domain glossary, implementation-agnostic
│   └── parity-matrix.md # generated — see §5
├── .claude/
├── .github/
├── .githooks/
├── docker-compose.yml       # profiles: `go` | `kotlin`, one shared Postgres
├── docker-compose.dev.yml
├── Dockerfile.go
├── Dockerfile.kotlin
├── Caddyfile
├── Makefile
├── .env.example
├── .nvmrc
├── .gitignore
├── CLAUDE.md
├── CONTRIBUTING.md
├── README.md            # must explain the two-backend arrangement immediately
└── LICENSE              # AGPL-3.0
```

**Repo name:** `afloat`, not `afloat-kotlin`. Once the repo contains both backends,
naming it after only one of them would be actively misleading to anyone landing on
it (including recruiters).

### Why this layout, specifically

- **`backend/` and `frontend/` as top-level siblings** follows the same pattern
  already used in Balances-v2. This was initially framed as being "forced" by
  Gradle and npm being unable to share a root directory — that reasoning was
  **wrong and later corrected** (see below) — but the sibling-folder layout is
  still the right choice here for a different, valid reason: IDE ergonomics.
- **Correction on the "build systems fight" reasoning:** Go's toolchain and npm
  don't actually conflict when sharing a directory tree — each only looks at its
  own territory (Go via `go.mod` + explicit `//go:embed` references, npm via
  `package.json` and its own subtree) and neither scans the other's files unless
  explicitly configured to. This is *why Uruni's flat layout* (`go.mod` at root,
  frontend in `web/`) works fine. So putting Gradle files at the true repo root
  (mirroring Uruni) was technically just as valid as nesting them in `backend/`.
- **The actual reason to still nest Kotlin under `backend-kotlin/`:** IntelliJ
  ergonomics. If Gradle files sit at repo root, opening the repo in IntelliJ scopes
  the whole monorepo as "the Gradle project," requiring manual exclusion of
  `frontend/node_modules` (easily 200k+ files) from indexing/search/VCS diffs.
  Nesting Kotlin in its own subfolder means opening *just* `backend-kotlin/` in
  IntelliJ, with a clean project tree and no exclusion rules needed.

---

## 4. Making "identical contract" real, not aspirational

Two artifacts were identified as necessary to make contract parity an enforceable
fact rather than a hope:

1. **`contract/openapi.yaml`** — the single source of truth for the API shape.
   Both backends must satisfy it; the frontend generates types from it. Without
   this, error envelope shape, timestamp formatting, and pagination conventions
   will silently diverge between a Spring app and a Go app the first time nobody's
   watching closely.
2. **`db/migrations/`** — canonical, versioned SQL migrations shared by both
   backends (consumed by Flyway on the Kotlin side, goose/sqlc-style tooling on the
   Go side), rather than two independently-evolving schema histories.

### Technical ADRs are explicitly *not* shared

Product-level docs (`PRD.md`, `CONTEXT.md`) are implementation-agnostic and meant
to transfer/apply to both backends unmodified. Technical ADRs are **not** expected
to transfer — a decision like "use Spring Data JPA" or "embed frontend via
classpath `static/` resources" has no Go equivalent, and the reverse is equally
true. Hence `docs/adr/go/` and `docs/adr/kotlin/` are kept as separate
subdirectories from the start, rather than letting one implementation's technical
choices implicitly bleed into the other's.

---

## 5. Tracking parity between backends

### Question: is OpenAPI alone sufficient?

No. The spec defines the *intended* contract; it says nothing about which parts of
that contract each backend has *actually* implemented, what milestone each is at,
or non-HTTP parity (background jobs, admin tooling, migration completeness,
behavioral edge cases within an identical schema).

### Decision: a generated parity matrix, not a hand-written one

A manually maintained status table rots — it's accurate the day it's written and
silently wrong a few weeks later. Instead:

```
make parity-matrix
```

- Runs both backends against the shared Postgres instance.
- Runs a contract-conformance tool (e.g. Schemathesis or Dredd) against
  `contract/openapi.yaml` for each backend.
- Emits `docs/parity-matrix.md` as a **generated** table (endpoint × backend ×
  pass/fail), regenerated fresh every run and never hand-edited.
- Explicitly **not** a CI gate — doesn't block merges, doesn't fail builds. It
  exists purely as a reference artifact for humans and agents to consult, per your
  stated intent.

A small **hand-maintained** section for non-API parity (background jobs, CLI
tooling, migration state) sits in the same file but is visually separated from the
generated table, so it's clear which part is trustworthy-by-construction and which
relies on manual upkeep.

---

## 6. Release and milestone strategy

### Decision: independent tags per backend

```
backend-go/v0.4.0
backend-kotlin/v0.4.0
```

Originally this was recommended as a safeguard against Kotlin's slower pace either
stalling Go or forcing rushed Kotlin work to hit a joint milestone. You clarified
that Go stalling in favor of Kotlin learning time is explicitly fine — so the
safeguard reasoning no longer applies, but the mechanism (independent tags) is kept
because it's still the more honest signal of where each implementation actually
stands.

A bare `v0.4.0` tag (no backend suffix) is reserved for the point where both
backends have genuinely reached the same milestone — which may lag well behind
Go's own tags, and that's an accurate reflection of reality rather than a problem
to solve.

---

## 7. Open / deferred items

- Whether `backend-kotlin/` gets a fully self-contained `README.md` so it reads
  standalone for recruiters browsing GitHub directly (recommended, not yet
  actioned).
- Concrete `docker-compose.yml` profile wiring (`--profile go` / `--profile
  kotlin`) against one shared Postgres — described conceptually, not yet written.
- The actual product brief / domain "grill session" content for Afloat itself
  (`docs/PRD.md`, `docs/CONTEXT.md`) — not started; this is explicitly the next
  step, to happen in Claude Code.
- Gradle wiring to copy `frontend/dist` into `backend-kotlin/src/main/resources/static`
  as part of `processResources`, and the Go-side equivalent (`//go:embed`) for
  `backend/` — sketched as an example, not yet implemented in a real build file.
