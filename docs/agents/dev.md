# Local dev / lint / tests

The `Makefile` is a shell so far (`BOOTSTRAP.md` §11 step 1). `make help` is the current list; this
file grows as real targets land.

**Today:**

- `make setup` — fresh clone: `hooks-install` (pre-commit pii-guard, seeds the gitignored
  `.pii-patterns`) + `claude-install` (hook executable bits, seeds `.claude/settings.local.json`).
- `make doctor` — toolchain readout against `BOOTSTRAP.md` §3. Never fails.
- `make check` — the pre-push gate `pre-push-gate.sh` runs. `backend/` is wired (lint, build,
  `go test -race`, coverage), as are generated-code drift, the env-var docs gate and the conformance
  case files; `frontend/` still skips. A surface that exists but has no steps in `check` **fails** —
  so the scaffold step that creates the frontend must also wire its lint/test steps into `check`, and
  into `.github/workflows/ci.yml` in the same commit (the two mirror step for step).
- `make conformance` — boots the backend from source and runs the black-box conformance suite against
  it (`contract/conformance/`, issue #28). Needs docker and a Go toolchain, so it is **not** in
  `make check`. `make check` compiles the module and runs `go test -short` there, which validates the
  committed case files without needing a live backend. Read `contract/conformance/README.md` before
  adding a case — in particular, a case for a decision that has not landed pins today's accident.
  **It empties the `afloat` database**: every case truncates every table and applies
  `contract/conformance/fixtures/seed.sql`, so anything a local run left in it is gone afterwards.
- `make test-migrations` — applies `backend/internal/migrations/` with goose to a throwaway Postgres
  container and asserts the schema behaves, the §7.1 in-place-edit hazard, and that Down drops what Up
  created. Needs docker, so it is **not** in `make check`.
- **Environment variables are documented in `BOOTSTRAP.md` §12**, and `scripts/check-env-docs.sh`
  (in `make check`) treats that table as the source of truth: it fails when the backend reads a name
  the table lacks, lacks one it lists, or defaults differently — including a duration spelled with a
  `d` suffix, which Go's `time.ParseDuration` rejects.
- `make lint-install` — installs the `GOLANGCI_VERSION` pinned in the `Makefile`. Optional locally if
  your own `golangci-lint` already matches; CI runs exactly this, so bump the pin in one place.
- **CI** (`.github/workflows/ci.yml`, on `pull_request` and pushes to `main`) has three jobs: `check`
  runs `make check`, `migrations` runs `make test-migrations`, and `conformance` runs
  `make conformance` — the last two need docker and minutes and so stay out of the local gate. It sets
  `AFLOAT_REQUIRE_TEST_DB=1`, which turns the integration tests' docker-missing skip into a failure —
  a runner that has docker must never report green on a suite that never ran.
- **Coverage** is produced by the gate, not beside it. `make check-go` always writes
  `backend/coverage.out`; the `check` job uploads it to Codecov under the flag `backend-go`. A
  separate coverage job would be a second run of the suite and the CI-mirrors-`make check` promise
  would stop being true. Policy
  lives in `codecov.yml`: a fixed 80% floor (not a delta gate, which would punish deleting dead
  code), **every status informational** until the numbers clear it, and an ignore list for generated
  code. The trap worth knowing: `go tool cover` counts statements, but Codecov normalises to
  **lines**, and only Codecov's number is what the gate reads — at `f56fbfc` that is 75.75%, against
  78.76% from the local tool. Compare like for like before concluding coverage moved. Re-validate that file after editing it — a malformed one is accepted silently
  and then ignored:
  `curl -X POST --data-binary @codecov.yml https://codecov.io/validate`.
- **The other workflows** run beside CI rather than inside it, because `check` mirrors `make check`
  and nothing else belongs in that job: `codeql.yml` (Go, manual build mode — autobuild looks at the
  repository root and finds no backend), `gitleaks.yml` (full history,
  and not the same thing as the local pre-commit pii-guard), and `govulncheck.yml` (Go only;
  reachability-based, so it reports a CVE only in a path the binary can reach).
- **Every action is pinned to a commit SHA** with a trailing `# vX.Y.Z` comment. The comment is not
  decoration — Dependabot reads it and rewrites both. `.github/dependabot.yml` groups minor and patch
  bumps per ecosystem; ungrouped, a week's bumps open a dozen PRs and each one burns a full CI run.

**Expected later, per `BOOTSTRAP.md`:**

- Running the backend and the frontend locally against the one Postgres.
- Regenerating the OpenAPI-derived client artefacts for the frontend.
- Lint and test commands for the frontend (Vitest, Playwright), and which of them `make check`
  actually runs versus which are opt-in.

Don't invent targets — if a task needs a dev command `make help` doesn't list, add it to the `Makefile`
(and here) rather than assuming one from Balances-v2 or Uruni carries over unchanged.
