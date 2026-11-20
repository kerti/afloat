# Batching Dependabot PRs

Fold every open Dependabot PR into one PR. Fixing each on its own branch doesn't scale: a Dependabot
PR someone pushed to stops rebasing itself, and every PR costs its own update-branch + CI + merge
cycle. One consolidated PR costs one. `.github/dependabot.yml` already groups minor and patch bumps
per ecosystem, so a batch is usually three PRs plus any majors.

`CLAUDE.md`'s sign-off rules bind every step: ask before committing (a "PR it" is the grant for
commit + push + open), never merge, never watch CI, never close, merge or comment on a Dependabot PR.

## 1. Inventory

```sh
gh pr list --author app/dependabot --state open \
  --json number,title,headRefName,labels,files,statusCheckRollup
```

For each PR note the ecosystem (gomod, npm, github-actions), every package with its from → to
version (a grouped PR lists several in its body), and whether it is a **security** update. A major
arrives ungrouped, on its own, by design: give it its own read.

Read the failing checks before blaming the bump. When every PR fails the same check (`govulncheck`
most likely), the cause is usually upstream of all of them — a new Go stdlib advisory fixed by a Go
release, not by any of the PRs. Pull it out with
`gh run view <id> --log-failed | grep -A8 '^Vulnerability #'`.

Show the maintainer the list before touching anything. A green PR can simply be merged by the
maintainer — say so and leave it out unless asked. A lone security update can go through this as a
batch of one.

## 2. Branch

```sh
make start-task && git switch -c chore/deps-batch-<yyyy-mm-dd>
```

## 3. Apply, one ecosystem at a time

Run each ecosystem's checks before moving to the next, so a failure names one ecosystem. Use the
exact versions Dependabot proposed — never "latest".

- **gomod (`backend/`):** `go get <module>@<version>` per module, then `go mod tidy`. Check:
  `go build ./... && go test ./...`. Read the changelog before including anything on these paths:
  - `shopspring/decimal` — the money path. Look for rounding or serialisation changes; money is a
    string on the wire (`CLAUDE.md` rule 1).
  - `getkin/kin-openapi` — request validation. Its media-type matching has changed between releases
    before (`BOOTSTRAP.md` §5.2, #89, #96).
  - `oapi-codegen`, `sqlc` — these are `tool` directives in `go.mod`, so a bump changes generated
    code. Run `make gen` and commit what it writes; `make check` fails on drift.

  Leave the `go` line alone: it is the module's minimum language version, CI's `setup-go` reads it
  through `go-version-file`, and it moves only by deliberate review.
- **npm (`frontend/`):** `npm install <pkg>@<version>` (`-D` for a devDependency), keeping the range
  style `package.json` already uses for that package. Give npm the bare version, not `^x.y.z` —
  npm adds the `^` itself, and a caret spec resolves to the newest in range instead of Dependabot's
  version. A PR that touches only `package-lock.json` bumps a **transitive** dependency: run
  `npm update <pkg>` (an `npm install` would wrongly make it a direct dependency), and do these
  last, since a direct bump can already have moved them. `npm update` takes the newest version the
  parents' ranges allow, so it can land a patch past Dependabot's — confirm with
  `npm ls <pkg> --all` that it is at or above the proposed version and report any drift.
  **npm regenerates `package-lock.json`; never hand-merge or cherry-pick a lockfile.** An
  `openapi-typescript` bump changes generated code: run `make gen-ts`. Check: `make check-frontend`.
- **github-actions:** edit the `uses:` lines to the new full commit SHA with its `# vX.Y.Z` comment,
  exactly as `gh pr diff <n>` has them. Keep every action SHA-pinned. Run `actionlint` if installed.

A PR whose diff touches something else (a workflow input, a config file) — apply it from
`gh pr diff <n>` verbatim and say what it was.

If an ecosystem's checks fail and the cause isn't obvious within that ecosystem, stop and report:
which update, the failing output, and the options (drop that update from the batch, or fix it).
Don't widen the batch into a refactor.

## 4. Gate

```sh
make check
```

It covers generated-code drift, the backend and the frontend. Remember it reports green with the
integration tests **skipped** when docker is down; run `AFLOAT_REQUIRE_TEST_DB=1 make check` if
there is any doubt (`docs/agents/dev.md`).

## 5. Commit and PR

Ask before committing unless the maintainer said "PR it". Commit message:

```
chore(deps): batch Dependabot updates (<yyyy-mm-dd>)
```

Body: one line per update — `package from → to (#n)` — then a line for anything regenerated
(lockfile, generated code). End with the session's attribution lines.

Open one PR labelled `dependencies`, with an `Area | Change` table of the updates and a
"Verified locally" list, and **no `Closes #n` lines**. Dependabot sees that `main` already has each
version and closes its own PRs; a `Closes` line would close them as "completed" under our name and
hide a version Dependabot still thinks is pending. Report the PR link and stop — no CI watching.

## 6. After the maintainer merges

One call, not a loop:

```sh
gh pr list --author app/dependabot --state open --json number,title
```

Dependabot usually closes the superseded PRs within minutes of the merge, but only when it next
re-checks. Report any still open with their numbers; the maintainer decides (wait,
`@dependabot recreate`, or close). Never close them yourself.
