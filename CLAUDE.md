# CLAUDE.md — Afloat

Guidance for **any** AI agent working in this repo. Read this first, every session. The `/docs` are
the source of truth; follow them over assumptions here.

`AGENTS.md` is a symlink to this file, so tools that look for that name land here too — one source of
truth, nothing to keep in sync. Only `.claude/` is Claude Code-specific; if your tool can't run those
hooks, run the project's check command yourself before you push, because nothing else will once it
exists (see `docs/agents/dev.md`).

## What Afloat is

A household spending-trajectory app: set a Monthly Budget, capture Expenses in seconds, get one
number — how much is left to spend today — plus a State (**Afloat** / **Drifting** / **Taking on
water**) reading the trajectory. It is **not** a ledger, not a reconciliation system, not a net-worth
tracker (that's [Balances](https://github.com/kerti/balances-v2)), and it never blocks or gates
spending. See `docs/VISION.md` §2–3 before proposing anything that smells like enforcement.

**Prime directive: inform, don't enforce.** A feature that scores, gates, streaks, or moralises
spending is out, however useful it looks. Check `docs/PRD.md` §10 (explicit non-goals) before
building anything that isn't obviously capture, dashboard, or setup.

## Source-of-truth docs (`/docs`)

- `VISION.md` — why Afloat exists and what it refuses to be. Stable; don't propose changes lightly.
- `PRD.md` — scope, requirements, open questions (`[OPEN Q-nn]`) and unratified assumptions
  (`[ASSUMED]`). Read the annotation before treating either as settled.
- `CONTEXT.md` — domain vocabulary and the calculation spec. Authoritative for arithmetic; one word
  per concept, no synonyms.
- `BOOTSTRAP.md` — the ratified scaffold sheet: naming, stack, auth, schema conventions, migrations,
  contract tooling. Read it before writing backend code.
- `docs/adr/go/` — backend-specific technical decisions. Scaffold-wide decisions belong in
  `BOOTSTRAP.md`, not an ADR.
- `docs/agents/ui-conventions.md` — the frontend's house rules (editing surfaces, forms, feedback,
  copy, a11y), drawn from Uruni then Balances. A UI choice it covers is decided. One it doesn't
  cover takes the nearest Uruni, then Balances, precedent as a **proposal**. The orchestrator
  **always confirms it with the maintainer** before the PR, and the PR adds the confirmed rule to
  the file.

## One backend, one contract

Afloat ships a Go backend against one hand-written `contract/openapi.yaml`.

- **The contract leads.** Never hand-edit generated server interfaces/types; edit
  `contract/openapi.yaml` and regenerate (`BOOTSTRAP.md` §6).
- **Trajectory is computed server-side**, tested against `contract/testdata/trajectory.json`
  (`BOOTSTRAP.md` §6, PRD N5a). A change to the trajectory formula updates the fixture and the tests
  together.

## Non-negotiable engineering rules

Full detail in `BOOTSTRAP.md` §4; the ones most likely to be violated by a plausible-looking diff:

1. **Money:** `DECIMAL(20,4)`, serialised as **strings** on the wire. A JSON number or scientific
   notation is a bug, not a formatting quirk.
2. **Primary keys:** UUIDv7, **client-generated**. `POST` is idempotent on the client-supplied id
   (`INSERT ... ON CONFLICT (id) DO NOTHING` + read-back) — this is the offline-write contract, live in
   MVP even though the queue isn't. The one exception is founding: the Household and User it
   creates get server-minted UUIDv7s, because it is an online auth flow (`BOOTSTRAP.md` §4).
3. **Tenancy:** every query touching Household-scoped data filters `household_id` **in SQL**, never
   only in middleware.
4. **Soft delete everywhere.** No hard-delete endpoint, ever.
5. **`occurred_on` is a date; `captured_at` is `timestamptz`.** Period Day math is server-side only —
   if the frontend also derives it, the two disagree at 03:59.
6. **No telemetry of any kind.** Don't add an events table, a tracking pixel, or an analytics call to
   satisfy a goals metric — G2/G3 are derived from existing timestamps by design (PRD §3, Q-01).
7. **Error envelope carries codes, not messages.** The frontend owns every user-facing string,
   including i18n (`en-GB` and `id-ID` both real from day one — PRD N12a).

## Repo layout

```
afloat/
├── backend/                 # Go backend; canonical migrations in internal/migrations/
├── frontend/                # React app
├── contract/                # openapi.yaml (source of truth), test fixtures, conformance harness
├── docs/{adr/go,brand,qa}/
├── docker-compose.yml
└── Makefile
```

Most of this doesn't exist yet — `BOOTSTRAP.md` §11 has the scaffold order. Don't invent structure
ahead of it; if a task needs a directory not listed here or in `BOOTSTRAP.md`, stop and ask rather than
guessing the layout.

## How agents work here (`BOOTSTRAP.md` §13)

The main session is the **orchestrator**: Opus at `medium`, pinned in `.claude/settings.json`. It holds
the plan, makes and surfaces the decisions, reads the diffs, writes the PR body, and asks for the
commit. Bulk reading, writing and reasoning go to the role agents in `.claude/agents/`, each pinned to a
model tier and effort in its frontmatter. **The frontmatter is the mechanism; this table is the map.**

| Agent | Tier · effort | Give it |
|---|---|---|
| `researcher` | sonnet · medium | Where something lives, how it already works, what exists before we add another. Read-only. |
| `planner` | sonnet · xhigh | Doc edits, `draft` ADRs, issue breakdowns (`to-issues`, `triage`). Drafts issues; never files them. |
| `grill` | sonnet · max | Stress-testing a plan or draft ADR before code exists (`grill-with-docs`). |
| `builder` | sonnet · medium | Code whose shape is decided: wiring, handlers, migrations + regeneration, fixtures, i18n, UI assembly. |
| `builder-deep` | sonnet · high | Trajectory and State, money and budget arithmetic, auth and sessions, data-reshaping migrations, the contract, unexplained failures. |
| `reviewer` | sonnet · high | A finished slice, before the PR. Read-only. |

- **Delegation needs a yes, every time.** `.claude/hooks/agent-gate.sh` makes every `Agent` call a
  permission prompt. Propose the batch in one message (agent, task, why it beats inline); one yes
  covers that batch only.
- **Delegate for context, not tokens.** A subagent starts cold and misses the prompt cache, so spend
  goes *up*; what it buys is a clean main window. Under ~3 files or 2 tool calls, do it inline.
  Anything that would dump listings, logs or search results into the main window, delegate.
- **Every brief carries its own context:** issue, branch, exact paths, the docs and ADRs in play,
  whether it changes `contract/openapi.yaml`, definition of done, "do not commit, push, or open a PR".
  Dispatch in the foreground.
- **Reports come back bounded** (paths changed, decisions made, what's open, what contradicted the
  docs) — and **are never trusted**: confirm a builder with `git diff` and `make check` yourself.
- **Never delegated:** commits, pushes, PRs and labels, issue writes, tags, dropping an ADR's `draft`
  tag, resolving an `[OPEN Q-nn]` or promoting an `[ASSUMED]`, UX choices, scope calls. That is the
  sign-off surface.
- **One writer per path.** Parallel code work uses `isolation: worktree`; migrations and generated
  files are single-writer.
- **Agent context is public-repo context.** No agent reads `.env`, server logs or the dev database (a
  real household's spending) without a reason in the brief.
- **Blocking yes, polling no.** Agents run `make check` / `go test` / `vitest` and wait. Never a
  foreground server, a `--watch` mode, or a CI query. `make conformance` and `make test-migrations`
  only when the brief asks (`conformance` empties the local database).
- **Runbooks:** batching Dependabot PRs is `docs/agents/dependabot-batch.md` (skill:
  `dependabot-batch`); starting work from a fresh `main` is `make start-task`.
- **Tools without subagents** (anything reading `AGENTS.md`): same phases, one agent. Ask the human to
  raise effort before planning or grilling and drop it for mechanical work. Every rule above still binds.

## Things explicitly NOT to do

- **Don't add a feature, screen, setting, or dependency `PRD.md` §4 doesn't ask for.** Three similar
  lines beats a premature abstraction, and a small self-hosted app beats a flexible one.
- **Don't resolve an `[OPEN Q-nn]` or promote an `[ASSUMED]` by writing code that assumes an answer.**
  Flag it and ask; PRD §11 is the list.
- **Don't touch Balances.** Afloat doesn't know Balances exists (`VISION.md` §5) — no outbound calls,
  no Balances-shaped concepts, no configuration naming it, beyond the one nullable
  `expected_monthly_income` field it exposes for something else to write.
- **Don't create planning/analysis documents** unless asked. Decisions go in ADRs or `docs/PRD.md`'s
  change log; nothing else.
- **Don't bypass `--no-verify` or `--no-gpg-sign`** on git commits.
- **Don't add comments that just restate the code.** Only when the WHY is non-obvious.
