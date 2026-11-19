# Afloat

A household spending-trajectory app. Set a Monthly Budget, capture Expenses in seconds, and get one
number on the first screen — **how much is left to spend today** — with a State reading the
trajectory: **Afloat**, **Drifting** or **Taking on water**.

It informs; it never enforces. Not a ledger, not a reconciliation system, not a net-worth tracker.
See [`docs/VISION.md`](docs/VISION.md).

> **Status:** pre-scaffold. The build follows [`docs/BOOTSTRAP.md`](docs/BOOTSTRAP.md) §11; most of the
> layout below does not exist yet.

## Layout

| Path | What it is |
|---|---|
| `backend/` | **Go** backend. |
| `backend/internal/migrations/` | Canonical SQL migrations, hand-written goose files embedded in the binary. |
| `frontend/` | React PWA. |
| `contract/openapi.yaml` | The hand-written API contract. The backend's server interfaces and the frontend's types are generated from it. |
| `contract/testdata/` | Calculation and auth fixtures the backend's unit tests read, e.g. `trajectory.json`. |
| `contract/conformance/` | Black-box HTTP cases run against the real backend. |

- **The contract leads.** Nobody hand-edits generated code; a change starts in `openapi.yaml` and is
  regenerated.
- **Shape and numbers are checked separately.** The conformance suite proves the backend serves the
  contract's answers; the trajectory fixture proves it computes the right numbers.
- **Trajectory is computed server-side.** The frontend formats; it derives nothing.

## Getting started

```sh
make setup    # git hooks (pii-guard) + Claude Code hooks
make doctor   # what's installed, what's missing
make db-up    # Postgres, database `afloat`
make help     # every target
```

Until a release exists, a schema change **edits `backend/internal/migrations/00001_baseline.sql` in
place** rather than adding a `00002` — and always alongside `make db-reset`, because goose silently
leaves an existing database on the old schema. See `BOOTSTRAP.md` §7.1.

Toolchain, per `BOOTSTRAP.md` §3: Go 1.27.x, Node 22 (`.nvmrc`), Docker.

Local ports — frontend `5181`, backend `5182`, Postgres `5184`. Deliberately none of the usual
defaults (`BOOTSTRAP.md` §1).

## Docs

- [`docs/VISION.md`](docs/VISION.md) — why Afloat exists and what it refuses to be.
- [`docs/PRD.md`](docs/PRD.md) — scope, requirements, open questions.
- [`docs/CONTEXT.md`](docs/CONTEXT.md) — domain vocabulary and the calculation spec.
- [`docs/BOOTSTRAP.md`](docs/BOOTSTRAP.md) — stack, conventions, scaffold order.
- [`docs/adr/`](docs/adr/) — backend-specific decisions.
- [`CLAUDE.md`](CLAUDE.md) — rules for AI agents working in this repo.

## License

[AGPL-3.0](LICENSE).
