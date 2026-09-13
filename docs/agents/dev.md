# Local dev / lint / tests

The `Makefile` is a shell so far (`BOOTSTRAP.md` §11 step 1). `make help` is the current list; this
file grows as real targets land.

**Today:**

- `make setup` — fresh clone: `hooks-install` (pre-commit pii-guard, seeds the gitignored
  `.pii-patterns`) + `claude-install` (hook executable bits, seeds `.claude/settings.local.json`).
- `make doctor` — toolchain readout against `BOOTSTRAP.md` §3. Never fails.
- `make check` — the pre-push gate `pre-push-gate.sh` runs. Every surface (`backend/`,
  `backend-kotlin/`, `frontend/`) currently skips. A surface that exists but has no steps in `check`
  **fails** — so the scaffold step that creates a backend or the frontend must also wire its
  lint/test steps into `check`, and into `ci.yml` once that exists (the two must mirror step for step).

**Expected later, per `BOOTSTRAP.md`:**

- Running both backends and the frontend locally (`docker-compose.yml` profiles: `go` | `kotlin`, one
  Postgres, two databases).
- `make gen-goose-migrations`, and regenerating the OpenAPI-derived server/client artefacts.
- The restart-after-backend-edit gotcha, if one turns out to exist for either backend (Go usually
  needs a rebuild; Kotlin/Spring Boot may not, with devtools).
- Lint and test commands per surface (Go, Kotlin/Kotest, frontend/Vitest, Playwright), and which of
  them `make check` actually runs versus which are opt-in.

Don't invent targets — if a task needs a dev command `make help` doesn't list, add it to the `Makefile`
(and here) rather than assuming one from Balances-v2 or Uruni carries over unchanged.
