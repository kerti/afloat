# Afloat

A household spending-trajectory app. Set a Monthly Budget, capture Expenses in seconds, and get one
number on the first screen — **how much is left to spend today** — with a State reading the
trajectory: **Afloat**, **Drifting** or **Taking on water**.

It informs; it never enforces. Not a ledger, not a reconciliation system, not a net-worth tracker.
See [`docs/VISION.md`](docs/VISION.md).

> **Status:** pre-scaffold. The build follows [`docs/BOOTSTRAP.md`](docs/BOOTSTRAP.md) §11; most of the
> layout below does not exist yet.

## Two backends, one contract

This repository holds **two complete, interchangeable backends** behind one frontend.

| Path | What it is |
|---|---|
| `backend/` | **Go** — the canonical implementation. |
| `backend-kotlin/` | **Kotlin / Spring Boot** — a learning track, held to the same contract. Open this directory on its own in IntelliJ, not the repo root. |
| `frontend/` | React PWA. Backend-agnostic: it cannot tell which backend it is talking to. |
| `contract/openapi.yaml` | The hand-written API contract. Both backends' server interfaces and the frontend's types are generated from it. |
| `contract/testdata/trajectory.json` | Shared calculation fixture. Both backends' unit tests read it. |
| `db/migrations/` | Canonical SQL migrations. Flyway reads them directly; the Go backend's goose files are generated from them. |

How the two are kept honest:

- **The contract leads.** Nobody hand-edits generated code; a change starts in `openapi.yaml` and is
  regenerated into all three surfaces.
- **Shape and numbers are checked separately.** Contract conformance proves both backends serve the
  same API; the shared trajectory fixture proves they compute the same answers.
- **Separate databases.** One Postgres instance, two databases (`afloat_go`, `afloat_kotlin`), never a
  shared schema. `docker-compose.yml` picks a backend by profile (`go` | `kotlin`).
- **Trajectory is computed server-side** in both backends. The frontend formats; it derives nothing.

## Getting started

```sh
make setup    # git hooks (pii-guard) + Claude Code hooks
make doctor   # what's installed, what's missing
make db-up    # Postgres: one instance, two databases
make help     # every target
```

Until a release exists, a schema change **edits `db/migrations/V0001__baseline.sql` in place** rather
than adding a `V0002` — and always alongside `make db-reset`, because Flyway then refuses to start
while goose silently leaves the Go database on the old schema. See `BOOTSTRAP.md` §7.1.

Toolchain, per `BOOTSTRAP.md` §3: Go 1.27.x, Temurin 21, Node 22 (`.nvmrc`), Docker.

## Docs

- [`docs/VISION.md`](docs/VISION.md) — why Afloat exists and what it refuses to be.
- [`docs/PRD.md`](docs/PRD.md) — scope, requirements, open questions.
- [`docs/CONTEXT.md`](docs/CONTEXT.md) — domain vocabulary and the calculation spec.
- [`docs/BOOTSTRAP.md`](docs/BOOTSTRAP.md) — stack, conventions, scaffold order.
- [`docs/adr/`](docs/adr/) — backend-specific decisions.
- [`CLAUDE.md`](CLAUDE.md) — rules for AI agents working in this repo.

## License

[AGPL-3.0](LICENSE).
