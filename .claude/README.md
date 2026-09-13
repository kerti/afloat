# `.claude/` — operator notes

For the maintainer, not the agents. Claude Code loads `settings.json`, `agents/*.md` and `hooks/*.sh`;
it does not load this file, which is why the reference material lives here rather than in `CLAUDE.md`
— that one is read into **every** subagent's context on **every** launch, so a line added there is
paid on each delegation.

The rules the agents follow are in [`CLAUDE.md`](../CLAUDE.md).

## What's here

| Path | Loaded when | Purpose |
|---|---|---|
| `settings.json` | Session start | Pins the orchestrator (`opus` · `medium`), wires the hooks |
| `settings.local.json` | Session start | Yours, gitignored. Personal approvals + commit attribution. Copy from `settings.local.json.example` |
| `agents/*.md` | On delegation | One role per file; `model` and `effort` in the frontmatter are the only thing that actually binds those choices |
| `hooks/session-start.sh` | Session start | Orients; fast-forwards `main` only when already on `main` and clean |
| `hooks/pre-push-gate.sh` | Before every Bash call | Denies `git push` when `make check` fails |
| `hooks/format-file.sh` | After Edit/Write | Formats what was just written (gofmt / prettier+eslint / ktlint, whichever tool exists) |

No `agent-gate.sh` yet — hold off wiring a permission prompt on every subagent spawn until the
role split above is actually in daily use; add it (mirroring uruni's) if delegation volume makes that
worth the friction.

## Roles

Six agents in `agents/`, split by risk and deliverable shape rather than by backend:

- `planner` (xhigh) — docs, draft ADRs, issue breakdowns.
- `grill` (max) — adversarial review of a plan before code is written.
- `builder` (medium) — mechanical changes whose shape is already decided.
- `builder-deep` (high) — trajectory calculation, money, auth, the contract itself.
- `reviewer` (high, read-only) — checks a finished diff against the non-negotiables.
- `researcher` (medium, read-only) — answers "where does X live / how does Y work".

A brief to `builder` or `builder-deep` must say which backend(s) it touches — see `CLAUDE.md`
"Two backends, one contract". Nothing in the role split enforces that; it's brief discipline.

## Overriding the pinned defaults

The session runs Opus at `medium` because `settings.json` says so.

| You want | Do this |
|---|---|
| Deeper reasoning, **one turn** | Put `ultrathink` anywhere in the prompt |
| Deeper reasoning, **this session** | `/effort max` — `max` is always session-only |
| A different level **going forward** | `/effort high` (or `low`/`medium`/`xhigh`) |
| Hand control back to the project default | `/effort auto` |
| A different model | `/model` |
| **No delegation**, do it in the main session | Say "do this inline" |

`/effort low|medium|high|xhigh` persists across sessions — `/effort auto` is the undo. `max` and
`ultrathink` never persist. `CLAUDE_CODE_EFFORT_LEVEL` beats everything, silently — check
`env | grep CLAUDE_CODE_EFFORT` first if effort isn't behaving as expected.
