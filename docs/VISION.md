# Afloat — Product Vision

> **Status:** Draft 1 — 13 September 2026. This document is meant to be **stable**. It states what
> Afloat is for and what it refuses to become. Scope, requirements and open questions live in
> [`PRD.md`](PRD.md); domain language and formulas live in [`CONTEXT.md`](CONTEXT.md).
>
> Supersedes `docs/scratch/Product Vision and PRD Foundation Draft.md`.

**Canonical casing:** the product name is **"Afloat"** (capital A). The adjective **"afloat"** stays
lowercase, and is also the name of one of the three trajectory States — which is deliberate:
*"Afloat tells you whether you're afloat."*

---

## 1. The question

A household knows roughly what it earns and roughly what it has. What it does not know, on any given
Tuesday, is this:

> **"Given what I've spent so far, how much can I safely spend from here to the end of the period?"**

Every existing answer to that question is either an accounting system that demands rigour the
household will not sustain, or a budgeting app that answers a different question — *did you obey your
categories?* — and answers it with a scolding.

Afloat answers the actual question, in one number, on the first screen, without being asked twice.

## 2. Positioning

### What Afloat is

- A **cash-flow radar** for household spending.
- An expense tracker whose primary output is **trajectory**, not a ledger.
- The tool for *"are we going to be okay at this rate?"*

### What Afloat is not

- Not a ledger or double-entry accounting application.
- Not a reconciliation system, and not a system that detects missing money.
- Not a net-worth tracker — that is [Balances](#5-relationship-with-balances).
- Not an income tracker. It stores one expected-income figure as a sanity anchor and nothing more.
- Not a transfer tracker. Pockets hold no balances and money does not move between them.
- Not a system that prevents, blocks, or gates spending.
- Not a replacement for Balances, and not a module of it (see §5).

## 3. Philosophy

**Inform, don't enforce.** The household decides what spending is appropriate. Afloat reports the
consequences and the trajectory. It never blocks, never gates, never withholds.

**Situational awareness over budgetary control.** A Budget is an expectation and a reference point,
not a contract. A Category over its allocation is *information*, not a violation. Money moves between
Categories freely because that is what actually happens.

**Capture first, organise later.** Recording a spend must take seconds and must never be blocked by
missing metadata. Correction is a normal, expected, frictionless act — not an exception path.

**Mutable by design.** Edits and deletions are ordinary. Every derived figure recomputes cleanly from
current data. The one exception is the Budget itself, which is versioned, because a target line that
silently rewrites its own history is a lie (see `CONTEXT.md` §Mid-period budget changes).

**Emotionally non-punitive.** Financial feedback should read like an instrument, not a report card.
Afloat never uses shame as a motivator, never implies moral failure, and never presents a recoverable
situation as a terminal one.

**Accounting rigour may exist internally where it earns its place. Accounting *concepts* must never
surface in the interface.**

## 4. The three States

Afloat reports trajectory as one of three States. The metaphor is nautical, and the choice of words
is load-bearing rather than decorative.

| State | Meaning |
|---|---|
| **Afloat** | Spending is at or near the intended pace. Nothing to do. |
| **Drifting** | Spending is running ahead of pace. Recoverable with mild attention. |
| **Taking on water** | The current pace would exhaust the period's budget before the period ends. |

**Why not "Sinking".** Sinking is terminal and removes agency — it tells someone their ship is going
down at exactly the moment they most need to believe they can act. *Taking on water* carries the same
severity signal and the same urgency, but implies bailing is possible, which is the behaviour the
product actually wants. This is a direct application of §3's non-punitive principle, not a softening
of the signal.

**The State never replaces the numbers.** It is a reading of them, always shown beside them.

**No mascot.** The identity is the **waterline** — a horizon whose level and calmness respond to
State. A character would cost illustration states, expressions and animation, would have to survive
being seen daily for years, and risks the guilt-driven register that makes people resent finance
apps. A waterline scales from a full-bleed dashboard down to a single line of colour in a list row.

> Brand identity, palette and logo are scoped as a follow-on session — see `PRD.md` §9. They follow
> Balances' precedent: generated from a script, documented in `docs/brand/`, adopted by ADR.

## 5. Relationship with Balances

### The boundary is structural, not a matter of taste

Balances tracks household net worth from month-end snapshots, and **itemised cash-flow tracking is a
declared non-feature** of it (Balances ADR-0001). Its comprehensive income identity is:

```
ΔNet Worth = Earned Income + Investment Return + Asset Value Change
             + Write-Offs + Tracking Changes − Living Expenses
```

Balances tracks every term on the right **except Living Expenses**, which it derives as the residual
and labels a cash-spending proxy. That residual is a single unexplained number.

**Afloat is the itemisation of that residual.** This is not an analogy — it is the same quantity,
approached from the other side:

```
Inferred Expenses (Balances residual) − Attributed Expenses (Afloat) = Unattributed Expenses
```

Afloat's job ends at attribution. *Why* some spending went unattributed is a Balances concern, and
Afloat neither knows nor cares.

### Integration posture

- Afloat is **independently useful**. Integration is optional and never a prerequisite.
- Afloat **does not know Balances exists**. No Balances-shaped concepts, no outbound calls, no
  configuration naming it.
- Afloat **exposes** attributed expense data over its own API; a consumer pulls it.
- The single inbound concession is `expected_monthly_income`, a Household setting writable over the
  API by anything at all — Balances, a script, or the user typing it. It anchors budget sanity checks
  and stays one nullable amount, never an income ledger.

### Shared shape, separate instances

Afloat mirrors Balances' identity model — **Household** as the unit of access and aggregation,
**Users** belonging to exactly one Household, **Ownership** (SoleOwner / Joint) on records — with its
own authentication and its own database. Same vocabulary, same tenancy discipline, no coupling.

## 6. North star

> Opening a personal-finance app should feel less like checking whether you failed a test, and more
> like checking the instruments on a ship.

A household member should be able to glance at Afloat and know:

1. What is left to spend today.
2. Whether the trajectory is healthy.
3. What is driving the answer.

Then they decide. Afloat's responsibility is to make that decision better informed — never to make it
for them.
