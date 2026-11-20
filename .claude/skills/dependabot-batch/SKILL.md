---
name: dependabot-batch
description: Consolidate open Dependabot PRs into one PR for Afloat - apply every update at its exact version on one branch, regenerate the lockfile and any generated code a tool bump changes, run make check, and open one PR without Closes lines so Dependabot retires its own PRs. Use when the maintainer asks to batch, consolidate or "do the Dependabot PRs", or when Dependabot PRs are red on the same check.
---

# Dependabot batch

The procedure lives in `docs/agents/dependabot-batch.md` — the single source of truth. This skill
exists only to route batching intent to that runbook; it does not restate it, so the two cannot
drift.

Steps:

1. Read `docs/agents/dependabot-batch.md` in full.
2. Execute its sections in order: inventory → branch → apply per ecosystem → gate → commit and PR →
   post-merge check.
3. Show the inventory before touching anything, ask before committing, never merge, never watch CI,
   never close a Dependabot PR.
