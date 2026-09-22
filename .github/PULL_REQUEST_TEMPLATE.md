Closes #

## What changed

## Why

## Backends touched

<!-- Name them. A brief that says "implement X" without naming Go, Kotlin or
     both is the failure mode CLAUDE.md calls out: it gets done in one and
     silently skipped in the other. Delete the rows that don't apply. -->

- [ ] Go (`backend/`)
- [ ] Kotlin (`backend-kotlin/`)
- [ ] Frontend (`frontend/`)
- [ ] Contract (`contract/openapi.yaml`) — regenerated, not hand-edited
- [ ] Neither backend (docs, tooling, CI)

## Checklist

- [ ] `make check` is green (CI mirrors it step for step)
- [ ] Nothing here scores, gates, streaks or moralises spending (`VISION.md` §2–3)
- [ ] No feature, screen, setting or dependency `PRD.md` §4 doesn't ask for
- [ ] No `[OPEN Q-nn]` resolved or `[ASSUMED]` promoted by code that assumes an answer
- [ ] Money is `DECIMAL(20,4)` and serialised as a **string** on the wire
- [ ] Every Household-scoped query filters `household_id` **in SQL**
- [ ] Soft delete only — no hard-delete path
- [ ] Errors carry codes, not messages; user-facing strings live in the frontend (`en-GB` + `id-ID`)
- [ ] No telemetry of any kind
- [ ] A change to one backend's behaviour has its counterpart in the other, or a filed issue saying why not
- [ ] No real names, real figures, or absolute local paths in the diff
