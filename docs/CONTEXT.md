# Afloat

A household spending-trajectory app. It answers one question — *how much can I safely spend from here
to the end of the period?* — from expenses the household records itself. Net-worth tracking is a
deliberate non-feature; that is [Balances](https://github.com/kerti/balances-v2).

> **Status:** Draft 1 — 13 September 2026. Companion to [`VISION.md`](VISION.md) (why) and
> [`PRD.md`](PRD.md) (what and when). This document owns **language** and **arithmetic**.
> Items marked **[OPEN Q-nn]** are unresolved and cross-referenced in `PRD.md` §11.

**Canonical casing:** the product is **"Afloat"**; the adjective and the State are lowercase
*afloat*. Domain nouns are capitalised in prose when they mean the modelled thing (a Pocket, an
Expense, the Daily Pool).

---

## Language

### Identity and ownership

These mirror Balances exactly, deliberately. Where a definition is identical, it is stated here
anyway so this document stands alone.

**Household**: The unit of access and aggregation — the people sharing economic life. Every Expense,
Budget, Category, Pocket and setting belongs to exactly one Household. Carries a `display_name`,
`reporting_currency` (default `IDR`), `period_start_day`, `day_starts_at`, and
`expected_monthly_income`. _Avoid_: Family, Team, Tenant, Account.

**User**: An individual member of a Household. Belongs to exactly one Household; all Users have full
read/write access to all of its data. Carries `display_name`, `email`, `locale` (default `en-GB`) and
`time_zone` (default `Asia/Jakarta`). _Avoid_: Account (reserved for the auth identity in prose only),
Member (acceptable informally, not a modelled term).

**Ownership** (Expense attribute): **SoleOwner** — attributed to a specific User for breakdown
purposes; **Joint** — attributed to the Household as a whole. Ownership expresses the Household's
*intent* for attribution, never who physically paid. Default **Joint**.

### Time

**Budget Period**: The window a Budget covers. Runs from `period_start_day` of one calendar month to
the day before `period_start_day` of the next, inclusive. With the default `period_start_day = 1` a
Period is exactly a calendar month; with `25` it runs the 25th to the 24th. A Period is identified by
the `year_month` of the calendar month **in which it starts**. _Avoid_: Month, Cycle. (Note the
divergence from Balances, where `year_month` is always a strict calendar month.)

**Period Day**: One day of a Budget Period, bounded by `day_starts_at` rather than midnight. Default
`04:00` local: an Expense captured at 01:00 falls on the previous Period Day. This matters because
the headline figure is a *daily* one, and midnight is not where a household's day ends.

**Today**: The Period Day containing `now` in the User's `time_zone`, offset by `day_starts_at`.

**Days Remaining**: The count of Period Days from Today to the Period's last day, **inclusive of
Today**. On the last day of a Period, Days Remaining is 1, never 0.

**Days Elapsed**: The count of Period Days strictly before Today, i.e. **settled** days. On day one
of a Period, Days Elapsed is 0.

### Money and buckets

**Monthly Budget**: The Household's total intended spend for a Budget Period. **Authoritative** — it
is a figure the Household sets, never a sum derived from Categories. _Avoid_: Limit, Cap, Target
(each implies enforcement Afloat does not perform).

**Category**: A household-defined bucket an Expense is classified into — Groceries, Transport, Rent.
Each Category carries a `bucket` of `daily` or `commitment` (below) and an optional planned
`allocation` for the Period. _Avoid_: Budget (as a noun for a category — the scratch drafts' term
"Budget/category" is retired), Tag.

> **Cross-app note:** Balances reserves **Category** for *Income* categories and **Tag** for
> user-defined grouping labels. Afloat's Category is a *spending* bucket. The two apps own separate
> vocabularies; the collision is documented rather than resolved. See §Flagged ambiguities.

**Commitment**: A Category whose spending is large, largely non-discretionary, and lumpy in time —
rent, insurance premiums, school fees, annual taxes, a planned appliance purchase. Commitment
spending is **excluded from every daily figure**: it does not reduce the Daily Pool's per-day maths,
does not move Trajectory Variance, and does not affect State. A Commitment Category **must** carry an
allocation; that is the point of it. _Avoid_: Fixed cost, Bill.

**Daily Category**: Any Category that is not a Commitment. Its allocation is optional.

**Commitments Budget**: `Σ allocation` over Commitment Categories for the Period.

**Daily Pool** (`P`): `Monthly Budget − Commitments Budget`. The money the daily figures divide. This
single subtraction is why a rent payment on day two does not report a catastrophe.

**Unallocated**: `Daily Pool − Σ allocation over Daily Categories`. May be positive (headroom the
Household has not assigned) or negative (Category allocations promise more than the Budget holds).
Both are shown; neither is an error. This exists because Monthly Budget is authoritative and Category
allocations are advisory — the two are allowed to disagree, visibly.

**Expense**: The core record. A single outflow of money, at a moment, from a Pocket, classified into
a Category. Fields: `id` (client-generated UUIDv7), `occurred_on` (the Period Day it counts against),
`captured_at` (when it was recorded), `amount`, `currency`, `pocket_id`, `category_id` (nullable),
`description` (free text, optional), `ownership`. _Avoid_: Transaction (implies a two-sided event
Afloat does not model), Entry, Spend (as a noun).

**Pocket**: A practical container the Household spends *from* — OVO, GoPay, a specific debit card,
cash in a wallet, a credit card. A Pocket exists so the Household can see where spending flows and so
that real-world behavioural barriers ("transport comes out of GoPay") are visible in the data. A
Pocket **holds no balance, has no transfers, and is never reconciled.** _Avoid_: Account, Wallet
(both imply a balance), Source.

> **A Pocket is not a Balances Asset.** The same real-world OVO account may be a bank-account Asset in
> Balances and a Pocket in Afloat. They are unrelated records with unrelated purposes and must never
> be reconciled against each other. See §Flagged ambiguities.

**Top-up**: Moving money into a Pocket (bank → OVO, ATM withdrawal → cash wallet). **A top-up is not
an Expense.** The spend is the Expense; the top-up is invisible to Afloat. Recording top-ups
double-counts and is the single most likely way for a Household to corrupt its own numbers.

### Trajectory

**Baseline Daily Budget** (`B`): `Daily Pool ÷ Total Period Days`. The flat, non-adaptive rate —
*"if spending were even, this is the daily number."* Used for the target line and for expressing
variance in days.

**Today's Allowance** (`A`): The adaptive per-day figure for Today, **computed once at the start of
Today and frozen for its duration**. Freezing matters: a rate that fell every time you bought coffee
would punish in real time, which §3 of `VISION.md` forbids.

**Left to Spend Today**: `Today's Allowance − today's daily-bucket spend`. **This is the hero figure.**
It ticks down through the day, which is correct — it is a wallet, not a grade. May go negative, and is
displayed as such rather than clamped to zero.

**Forward Daily Rate**: What each remaining day *after* Today is worth at the current position. A
secondary figure, undefined on the last day of a Period (rendered "—").

**Trajectory Variance** (`V`): `(Baseline Daily Budget × Days Elapsed) − settled daily-bucket spend`.
Positive means under pace. Computed on **settled days only** — it does not move during Today. This
keeps the live wallet figure and the trend signal from restating the same fact against each other,
and stops the variance from lurching at every purchase.

**Buffer Days**: `Trajectory Variance ÷ Baseline Daily Budget`, signed, in days. The user-facing way
to express variance — *"about two days' spending behind"* — and the input to State. It scales with
Budget size automatically, so State thresholds need no per-Household tuning.

**State**: One of `afloat` | `drifting` | `taking_on_water`. See §Calculation spec.

---

## Calculation spec

All figures below concern the **daily bucket only**. Commitment spending is excluded from every one
of them and reported separately.

### Symbols

| Symbol | Meaning |
|---|---|
| `P` | Daily Pool for the Period |
| `D` | Total Period Days |
| `e` | Days Elapsed (settled days, strictly before Today) |
| `r` | Days Remaining (inclusive of Today); `r = D − e` |
| `S_settled` | Daily-bucket spend with `occurred_on` strictly before Today |
| `S_today` | Daily-bucket spend with `occurred_on` = Today |
| `S_future` | Daily-bucket spend with `occurred_on` after Today (future-dated) |

### Core figures

```
B  (Baseline Daily Budget) = P / D
P' (Available Pool)        = P − S_future
A  (Today's Allowance)     = (P' − S_settled) / r          [adaptive mode]
A                          = B                             [fixed mode]
Left to Spend Today        = A − S_today
Forward Daily Rate         = (P' − S_settled − S_today) / (r − 1)     if r > 1
                           = undefined ("—")                          if r = 1
V  (Trajectory Variance)   = (B × e) − S_settled
Buffer Days                = V / B
```

### Why it behaves at the edges

- **Day one** (`e = 0`, `r = D`): `A = P/D = B`, `V = 0`, State is `afloat`. Nothing pretends to know
  anything yet.
- **Last day** (`r = 1`): `A` equals the entire remaining pool. Under the old "remaining daily
  allowance" framing this read as nonsense; framed as **Left to Spend Today** it is simply true and
  useful — it is the last day, and that *is* what is left. Forward Daily Rate is undefined and shown
  as "—" rather than dividing by zero.
- **Adaptive borrowing.** Under-spending early raises `A`, which is honest but invites spending the
  slack. The UI therefore always shows `B` beside `A`, so an inflated allowance is visibly borrowed
  from the Household's own future rather than presented as found money. **[OPEN Q-04]** — whether to
  additionally cap displayed `A` at some multiple of `B`.
- **Rounding.** Store `DECIMAL(20,4)`, mirroring Balances (ADR-0011); serialise decimals as strings
  on the wire. Everything is computed from exact values and rounded only on output — never feed a
  rounded intermediate into another figure, except `A` into Left to Spend Today below.
  - **R1 — allowance figures floor at 4dp on the server.** Baseline Daily Budget, Today's Allowance,
    Forward Daily Rate and Left to Spend Today are **floored** (`RoundingMode.FLOOR`, toward −∞, not
    toward zero) at the storage scale, so Afloat never tells a Household it can spend more than it
    can. `A` is rounded once, then Left to Spend Today is that floored `A` minus `S_today`. Forward
    Daily Rate is computed from exact values, then floored. The client's own floor to the display unit
    (Rp1.000 for IDR, N5a) is separate and downstream of this — the server never applies a display
    unit.
  - **R2 — Trajectory Variance and Buffer Days go on the wire half-up at 4dp.** Half-up means half
    away from zero (`RoundingMode.HALF_UP`, shopspring `Round`). State is decided from the *exact*
    (unrounded) `V` and `B` — compared as `V` against `threshold × B`, with no division — never from
    the rounded Buffer Days figure, so a boundary can round to a tidy number while State still reads
    the far side of it.

### Adaptive vs fixed

Adaptive is the default. Fixed (`A = B` always) is available as an advanced Household setting. The
setting changes only `A`; `V`, Buffer Days and State are computed from `B` in both modes.

### State thresholds

```
Buffer Days ≥ −1                      → afloat
−3 ≤ Buffer Days < −1                 → drifting
Buffer Days < −3                      → taking_on_water
```

Note that `Buffer Days < 0` is exactly equivalent to *"projected end-of-period spend, at the average
pace so far, exceeds the Daily Pool"* — the two candidate signals are the same signal, so Afloat
computes one.

**Warm-up rule.** While `e < 3`, State is pinned to `afloat`. Three settled days is too little
evidence to call a trend, and a State that panics on day two is how a Household stops opening the app
in month one. **[OPEN Q-05]** — whether three days is the right floor, and whether to surface a
distinct neutral "getting a read" presentation rather than reusing `afloat`.

**Thresholds are configuration, not constants.** They live in one place and are expected to be tuned
once real data exists. Both the warm-up floor (`e < 3` above) and the two State thresholds
(`drifting_below: -1`, `taking_on_water_below: -3`) are **arguments to the calculation**, not literals
inside it — `contract/testdata/trajectory.json`'s `parameters` section carries the defaults, and any
row may override them, so a future change to the defaults is a fixture edit, not a code change.

### Future-dated Expenses

An Expense may be dated after Today. It reduces the Available Pool immediately (`P'`), so the
allowance reflects money already spoken for. It does **not** count as a settled day, does **not**
enter `V`, and does **not** appear on the actual cumulative line before its date. On the day it
arrives it becomes an ordinary `S_today`.

### Mid-period Budget changes

Budgets are **versioned events with effective dates**; Expenses are mutable current state. Changing
the Monthly Budget mid-Period does not rewrite history.

The target cumulative line **kinks, continuously**. If the Budget changes effective day `j`
(1-indexed), let `T(j−1)` be the target cumulative through day `j−1` under the old baseline. From day
`j` onward:

```
B_new = (P_new − T(j−1)) / (D − (j − 1))
```

The line has no vertical jump at `j`, and it still terminates exactly at `P_new` on day `D`. A
retroactive redraw was rejected: it silently falsifies "you were ahead of pace yesterday".

If `P_new ≤ T(j−1)` — the Household cut its Budget to at or below what it has already targeted —
`B_new` is zero or negative. The target line is clamped flat at `T(j−1)` for the rest of the Period
and the Period is reported as already fully committed. **[OPEN Q-06]** — the right presentation for
this case.

### Recalculation

Every figure above is derived from current data on read. There are no stored aggregates and no
snapshots in the MVP. Editing or deleting a past Expense correctly changes the past — including
yesterday's State as displayed in history. That is the intended behaviour: Afloat shows what is true
now, not what it believed at the time.

### Worked check

These are the reference values the implementation must reproduce. Daily Pool `P` = Rp6.000.000 over
`D` = 30 days, so `B` = Rp200.000/day.

The table below rounds to whole rupiah for readability. The authoritative values are the 4dp ones in
`contract/testdata/trajectory.json`'s `figures` rows (BOOTSTRAP.md §6) — e.g. the day-2 row's `A` is
`203448.2758`, which the table shows as `203.448`. Both backends' unit tests read the fixture, never
this table.

| Case | `e` | `S_settled` | `S_today` | `A` | Left today | Fwd rate | Buffer | State |
|---|---|---|---|---|---|---|---|---|
| Day 1, nothing spent | 0 | 0 | 0 | 200.000 | 200.000 | 206.897 | 0.00d | afloat (warm-up) |
| Day 2, spent 100k | 1 | 100.000 | 0 | 203.448 | 203.448 | 210.714 | +0.50d | afloat (warm-up) |
| Day 16, exactly on pace | 15 | 3.000.000 | 0 | 200.000 | 200.000 | 214.286 | 0.00d | afloat |
| Day 16, 600k over | 15 | 3.600.000 | 0 | 160.000 | 160.000 | 171.429 | −3.00d | drifting |
| Day 16, 700k over | 15 | 3.700.000 | 0 | 153.333 | 153.333 | 164.286 | −3.50d | taking on water |
| Day 30, 5.5m settled, 200k today | 29 | 5.500.000 | 200.000 | 500.000 | 300.000 | — | +1.50d | afloat |
| Day 10, 500k future-dated | 9 | 1.800.000 | 0 | 176.190 | 176.190 | 185.000 | 0.00d | afloat |

The day-2 row reproduces the worked example in the original scratch draft (Rp203,4k) exactly.

**Why Commitments must be their own bucket.** Same Household, Rp9.000.000 Monthly Budget, Rp3.000.000
rent paid on day 2:

| Model | Buffer Days on day 2 | State once warm-up ends |
|---|---|---|
| Two buckets (rent is a Commitment) | **+0.50d** | afloat |
| Single pool (rent hits the daily pool) | **−9.33d** | taking on water |

The warm-up rule hides the single-pool result for three days and no longer. The bucket split, not the
warm-up rule, is what makes the first week of a Period readable.

**Target-line continuity under a mid-period change.** `P` 6.000.000 → 7.500.000 effective day 11:
`B_old` = 200.000, `B_new` = 275.000. Target runs 2.000.000 at day 10 → 2.275.000 at day 11 (a slope
change, no vertical jump) → exactly 7.500.000 at day 30.

---

## Relationships

- A **Household** has 1..N **Users**; a **User** belongs to exactly one **Household**.
- A **Household** has 0..N **Categories**, 0..N **Pockets**, and one **Budget** per **Budget Period**.
- Every **Expense** belongs to exactly one Household, references exactly one **Pocket**, references
  at most one **Category** (nullable — an uncategorised Expense is valid and counts toward the daily
  bucket), and carries an **Ownership** mode.
- A **Budget** has a Monthly Budget amount and 0..N Category allocations, and is versioned by
  effective date within its Period.
- **Pockets** have no relationships to each other. There is no transfer entity.
- Nothing in Afloat references anything in Balances.

---

## Example dialogue

> **Q:** "I topped up OVO with Rp500k from BCA, then spent Rp80k on GrabFood. What do I record?"
> **A:** One Expense: Rp80k, Pocket = OVO. The top-up is not an Expense — recording it would count
> Rp580k of spending where Rp80k happened. Afloat never sees money that merely moved.

> **Q:** "I withdrew Rp1m from an ATM. Is that spending?"
> **A:** No. It is a top-up of your Cash Pocket. The spending happens when the cash leaves your hand,
> and each of those is its own Expense against the Cash Pocket. This is the hardest discipline Afloat
> asks for, and it is asked for deliberately — a lump ATM Expense would tell you nothing about where
> the money went.

> **Q:** "Rent came out on the 2nd and the app says I'm still afloat. Is it broken?"
> **A:** No. Rent is a Commitment Category, so it never touches the daily figures. Your Daily Pool was
> already reduced by the rent allocation when the Period began. Commitments are reported on their own
> line — planned versus paid.

> **Q:** "My spouse paid for dinner from her own card. Whose Expense is it?"
> **A:** Whichever the Household intends for attribution — Ownership captures intent, not who tapped
> the card. If dinner is shared household spending, mark it Joint even though one person paid.

> **Q:** "I refunded a purchase."
> **A:** Edit the original Expense down, or delete it. Afloat has no immutable events to compensate
> for. If the original was never recorded, a negative-amount Expense is valid (Q-07 resolved
> 2026-09-13, PRD.md change log; `CHECK (amount <> 0)` forbids only zero).

---

## Flagged ambiguities

- **"Category"** collides across apps: Balances reserves it for *Income* categories, Afloat uses it
  for *spending* buckets. Resolved as a documented divergence — each app owns its own vocabulary — on
  the grounds that "Category" is the natural user-facing word for a spending bucket and no better
  candidate exists that isn't already reserved (Tag, Bucket, Budget all conflict).
- **"Pocket" vs Balances' "Asset → bank account"** describe the same real-world container for
  different purposes. Resolved: a Pocket has no balance and is never reconciled; the two records are
  independent by design and any future urge to link them should be treated as scope creep.
- **"Budget"** meant both the Monthly Budget amount and an individual spending category in the scratch
  drafts ("Budget/category"). Resolved: **Monthly Budget** for the total, **Category** for the bucket,
  **allocation** for a Category's planned amount.
- **"Month"** is ambiguous once `period_start_day ≠ 1`. Resolved: **Budget Period** is the modelled
  window; "month" is avoided in code and UI copy.
- **"Day"** is ambiguous because of `day_starts_at`. Resolved: **Period Day** in code; UI copy says
  "today" and the offset is invisible unless the user changes it.
