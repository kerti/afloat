# Local dev / lint / tests

The `Makefile` is a shell so far (`BOOTSTRAP.md` §11 step 1). `make help` is the current list; this
file grows as real targets land.

**Today:**

- `make setup` — fresh clone: `hooks-install` (pre-commit pii-guard, seeds the gitignored
  `.pii-patterns`) + `claude-install` (hook executable bits, seeds `.claude/settings.local.json`).
- `make doctor` — toolchain readout against `BOOTSTRAP.md` §3. Never fails.
- `make start-task` — before new work: refuses a dirty tree, checks GitHub access, then switches to
  `main` and fast-forwards it. The SessionStart hook runs it for you only when you are already on a
  clean `main`; from a feature branch, run it yourself when you mean to leave that branch.
- `make check` — the pre-push gate `pre-push-gate.sh` runs. `backend/` is wired (lint, build,
  `go test -race`, coverage), `frontend/` is wired (`check-frontend`), as are generated-code drift
  (Go and TypeScript), the env-var docs gate and the conformance case files. A new surface must be
  wired into `check` and into `.github/workflows/ci.yml` in the same commit that creates it (the two
  mirror step for step).
- `make conformance` — boots the backend from source and runs the black-box conformance suite against
  it (`contract/conformance/`, issue #28). Needs docker and a Go toolchain, so it is **not** in
  `make check`. `make check` compiles the module and runs `go test -short` there, which validates the
  committed case files without needing a live backend. Read `contract/conformance/README.md` before
  adding a case — in particular, a case for a decision that has not landed pins today's accident.
  An existing local volume from before the database was renamed to `afloat` needs `make db-reset`
  (destroys local data). **It empties the `afloat` database**: every case truncates every table and applies
  `contract/conformance/fixtures/seed.sql`, so anything a local run left in it is gone afterwards.
- **Frontend** (`frontend/`, React + Vite, BOOTSTRAP §3): `make frontend-install` (`npm ci`),
  `make frontend-dev` (Vite on 5181, `/api` proxied to the backend on 5182), `make check-frontend`
  (typecheck, ESLint, Prettier check, Vitest with coverage; part of `make check`), `make gen-ts`
  (regenerates `src/api/schema.gen.ts` from the contract; drift fails `make check`), and
  `make frontend-build`, which builds and copies `dist` into `backend/internal/web/dist` so the
  next `go build` embeds it (BOOTSTRAP §8). Bare copy in JSX is a lint **error**: every string lives
  in `src/locales/en-GB.json` and `id-ID.json`, and a test fails if the two key sets differ or a
  contract `ErrorCode` has no wording. The palette is one file, `src/styles/theme.css`.
- `make test-migrations` — applies `backend/internal/migrations/` with the goose CLI to a throwaway
  Postgres container: Up applies, the §7.1 in-place-edit hazard, and Down drops what Up created. Needs
  docker, so it is **not** in `make check`. The schema's own rules (CHECKs, indexes, foreign keys,
  defaults) are Go tests in `backend/internal/migrations/schema_test.go`, which `make check` runs —
  but like every integration test they **skip** when docker is down, and `make check` still reports
  green. Run it as `AFLOAT_REQUIRE_TEST_DB=1 make check` to make a missing docker fail instead.
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

- Playwright end-to-end tests against a real backend, once there is a flow worth driving: opt-in
  locally, a CI job of its own like `conformance`.
- The Caddyfile and deploy (§11 step 10).

Don't invent targets — if a task needs a dev command `make help` doesn't list, add it to the `Makefile`
(and here) rather than assuming one from Balances-v2 or Uruni carries over unchanged.
