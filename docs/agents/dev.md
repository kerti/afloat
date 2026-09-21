# Local dev / lint / tests

The `Makefile` is a shell so far (`BOOTSTRAP.md` §11 step 1). `make help` is the current list; this
file grows as real targets land.

**Today:**

- `make setup` — fresh clone: `hooks-install` (pre-commit pii-guard, seeds the gitignored
  `.pii-patterns`) + `claude-install` (hook executable bits, seeds `.claude/settings.local.json`).
- `make doctor` — toolchain readout against `BOOTSTRAP.md` §3. Never fails.
- `make check` — the pre-push gate `pre-push-gate.sh` runs. `backend/` is wired (lint, build,
  `go test -race`); `backend-kotlin/` is wired but skips until it is scaffolded; `frontend/` still
  skips. A surface that exists but has no steps in `check` **fails** — so the scaffold step that
  creates a backend or the frontend must also wire its lint/test steps into `check`, and into
  `.github/workflows/ci.yml` in the same commit (the two mirror step for step).
- `make check-kotlin` — `./gradlew check` in `backend-kotlin/`. Gradle's `check` lifecycle task, not
  `test`: it already depends on compilation and on every verification task in the build, so a linter
  added later joins this gate without a `Makefile` change. Requires the committed Gradle wrapper.
- `make sync-kotlin-migrations` — copies `db/migrations/V*.sql` into
  `backend-kotlin/src/main/resources/db/migration`. The Kotlin backend carries its migrations inside
  the jar, so it needs its own copy; the copy is **generated output and never hand-edited**, and
  `make check` diffs it against a scratch regeneration rather than repairing it silently. Edit
  `db/migrations` and re-run this.
- `make sync-denylist` — copies `shared/common_passwords.txt` into `backend/internal/auth/` and
  `backend-kotlin/src/main/resources/`. The denylist is owned by neither backend and **neither can
  read it where it lives**: Go's `//go:embed` cannot reference a parent directory, and the Kotlin
  backend needs the file inside the jar for the same reason as the migrations above. Both copies are
  **generated output and never hand-edited**; `make check` diffs them against a scratch regeneration.
  Edit `shared/common_passwords.txt` and re-run this.
- **Environment variables are a cross-backend contract** (`BOOTSTRAP.md` §12): both backends read the
  same names with the same defaults, so an operator configures once and can switch backends.
  `scripts/check-env-parity.sh` (in `make check`) treats the §12 table as the source of truth and
  fails on drift in either backend — including a duration spelled with a `d` suffix, which Spring
  accepts and Go rejects.
- `make lint-install` — installs the `GOLANGCI_VERSION` pinned in the `Makefile`. Optional locally if
  your own `golangci-lint` already matches; CI runs exactly this, so bump the pin in one place.
- **CI** (`.github/workflows/ci.yml`, on `pull_request` and pushes to `main`) has two jobs: `check`
  runs `make check`, and `migrations` runs `make test-migration-runners` + `make test-migrations`,
  which need docker and minutes and so stay out of the local gate. It sets
  `AFLOAT_REQUIRE_TEST_DB=1`, which turns the integration tests' docker-missing skip into a failure —
  a runner that has docker must never report green on a suite that never ran.

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
