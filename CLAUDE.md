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
  contract tooling. Read it before writing code that touches either backend.
- `docs/adr/go/`, `docs/adr/kotlin/` — backend-specific technical decisions once they exist.
  Cross-backend decisions belong in `BOOTSTRAP.md`, not an ADR.

## Two backends, one contract

Afloat ships a Go backend (canonical) and a Kotlin backend (learning track) against one hand-written
`contract/openapi.yaml`. This is the thing most likely to go wrong by omission:

- **State which backend(s) a task touches.** A brief that says "implement expense capture" without
  naming Go, Kotlin, or both will get silently duplicated into one and skipped in the other, or copied
  without adapting to the target backend's idioms.
- **The contract leads.** Never hand-edit generated server interfaces/types; edit
  `contract/openapi.yaml` and regenerate (`BOOTSTRAP.md` §6).
- **Trajectory is computed server-side, in both backends**, from the same
  `contract/testdata/trajectory.json` fixture (`BOOTSTRAP.md` §6, PRD N5a). A change to the trajectory
  formula that updates one backend's tests and not the other's is incomplete.

## Non-negotiable engineering rules

Full detail in `BOOTSTRAP.md` §4; the ones most likely to be violated by a plausible-looking diff:

1. **Money:** `DECIMAL(20,4)`, serialised as **strings** on the wire in both backends. A JSON number or
   scientific notation from Spring is a bug, not a formatting quirk.
2. **Primary keys:** UUIDv7, **client-generated**. `POST` is idempotent on the client-supplied id
   (`INSERT ... ON CONFLICT (id) DO NOTHING` + read-back) — this is the offline-write contract, live in
   MVP even though the queue isn't.
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
├── backend/                 # Go — canonical implementation
├── backend-kotlin/          # Kotlin — learning track, opened separately in IntelliJ
├── frontend/                # shared, backend-agnostic React app
├── contract/                # openapi.yaml (source of truth) + shared test fixtures
├── db/                      # canonical Flyway migrations + goose-source undo files
├── docs/{adr/{go,kotlin},brand,qa}/
├── docker-compose.yml
└── Makefile
```

Most of this doesn't exist yet — `BOOTSTRAP.md` §11 has the scaffold order. Don't invent structure
ahead of it; if a task needs a directory not listed here or in `BOOTSTRAP.md`, stop and ask rather than
guessing the layout.

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
