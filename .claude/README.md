# `.claude/` — operator notes

For the maintainer, not the agents. Claude Code loads `settings.json`, `agents/*.md`, `hooks/*.sh`
and `skills/`; it does not load this file, which is why the reference material lives here rather than
in `CLAUDE.md` — that one is read into **every** subagent's context on **every** launch, so a line
added there is paid on each delegation.

The rules the agents follow are in [`CLAUDE.md`](../CLAUDE.md); the reasoning is
[`BOOTSTRAP.md` §13](../docs/BOOTSTRAP.md#13-agent-operating-model).

## What's here

| Path | Loaded when | Purpose |
|---|---|---|
| `settings.json` | Session start | Pins the orchestrator (`opus` · `medium`), turns off fork subagents, wires the hooks, allows the safe Bash verbs. For `gh issue` that is `view` and `list` only: issue text is public input, so every write prompts |
| `settings.local.json` | Session start | Yours, gitignored. Personal approvals + commit attribution. Seeded by `make claude-install` |
| `agents/*.md` | On delegation | One role per file; `model` and `effort` in the frontmatter are the only thing that actually binds those choices |
| `hooks/session-start.sh` | Session start | Orients; runs `make start-task` only when already on a clean `main` |
| `hooks/pre-push-gate.sh` | Before every Bash call | Denies `git push` when `make check` fails |
| `hooks/agent-gate.sh` | Before every Agent call | Turns every subagent spawn into a permission prompt (needs `jq`; without it the hook stays silent) |
| `hooks/format-file.sh` | After Edit/Write | Formats what was just written (gofmt / prettier+eslint, whichever tool exists) |
| `skills/dependabot-batch/` | When asked to batch Dependabot PRs | Routes to the runbook in `docs/agents/dependabot-batch.md` |

`settings.json` is committed, so a fresh clone asks once to trust the workspace before the hooks run.
`make claude-install` re-arms the executable bit on all four hooks; re-run it after any pull that
touches `.claude/`.

## Roles

Six agents in `agents/`, split by risk and deliverable shape rather than by backend:

- `planner` (xhigh) — docs, draft ADRs, issue breakdowns.
- `grill` (max) — adversarial review of a plan before code is written.
- `builder` (medium) — mechanical changes whose shape is already decided.
- `builder-deep` (high) — trajectory calculation, money, auth, the contract itself.
- `reviewer` (high, read-only) — checks a finished diff against the non-negotiables.
- `researcher` (medium, read-only) — answers "where does X live / how does Y work".

A brief to `builder` or `builder-deep` must say whether it changes `contract/openapi.yaml` — see
`CLAUDE.md` "One backend, one contract". Nothing in the role split enforces that; it's brief discipline.

## Overriding the pinned defaults

The session runs Opus at `medium` because `settings.json` says so.

| You want | Do this |
|---|---|
| Deeper reasoning, **one turn** | Put `ultrathink` anywhere in the prompt |
| Deeper reasoning, **this session** | `/effort max` — `max` is always session-only |
| A different level **going forward** | `/effort high` (or `low`/`medium`/`xhigh`) |
| Hand control back to the project default | `/effort auto` |
| A different model | `/model` |
| **No delegation**, do it in the main session | Answer *no* at the agent-gate prompt, or say "do this inline" |
| Set it at launch | `claude --effort xhigh` |

`/effort low|medium|high|xhigh` persists across sessions — `/effort auto` is the undo. `max` and
`ultrathink` never persist. `CLAUDE_CODE_EFFORT_LEVEL` beats everything, silently — check
`env | grep CLAUDE_CODE_EFFORT` first if effort isn't behaving as expected.

## The cost model, in one paragraph

Effort is the biggest lever: thinking tokens are billed at the output rate on every turn, trivial ones
included, which is why the orchestrator runs at `medium`. Delegation raises total tokens (a cold start,
no prompt cache) but keeps the exploration out of the main window, where it would otherwise be
re-read, cached, on every later turn. It inverts below a few files: re-reading something the
orchestrator already has cached is far cheaper than a subagent reading it cold. Keep briefs small and
self-contained; the context dump is the expensive part.
