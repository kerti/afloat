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
Budget, Category, Pocket and setting belongs to exactly one Household. Carries a
`household_display_name`, `reporting_currency` (default `IDR`), `period_start_day`, `day_starts_at`,
`allowance_mode` (`adaptive` or `fixed`) and `expected_monthly_income`. _Avoid_: Family, Team, Tenant,
Account.

**User**: An individual member of a Household. Belongs to exactly one Household; all Users have full
read/write access to all of its data. Carries `user_display_name`, `email`, `locale` (default `en-GB`),
`time_zone` (default `Asia/Jakarta`) and `appearance` (default `system`). `email` is the sign-in handle,
never the identity. `time_zone` is an input to Today and is never derived by the client. _Avoid_: Account (reserved for the auth identity in prose only),
Member (acceptable informally, not a modelled term).

**Appearance**: A User's choice of light or dark: `system` (follow the device, the default), `light` or
`dark`. Stored per User on the server, so it follows the person across devices, and applied by the client
alone. It is not the palette: Shallows is the one palette (PRD §9), and the palette is what `theme.css`
and "theme" name in the frontend. Appearance chooses which values of that palette show. _Avoid_: Theme
(means the palette), Mode, Dark mode (one of the three values, not the setting).

**Ownership** (Expense attribute): **SoleOwner** — attributed to a specific User for breakdown
purposes; **Joint** — attributed to the Household as a whole. Ownership expresses the Household's
*intent* for attribution, never who physically paid. Default **Joint**.

### Time

**Budget Period**: The window a Budget covers. Runs from `period_start_day` of one calendar month to
the day before `period_start_day` of the next, inclusive. With the default `period_start_day = 1` a
Period is exactly a calendar month; with `25` it runs the 25th to the 24th. A Period is identified by
the `year_month` of the calendar month **in which it starts**. _Avoid_: Month, Cycle. (Note the
divergence from Balances, where `year_month` is always a strict calendar month.) A Period's window is
always derived from the Household's current `period_start_day`, which is 1 to 28 so that every Period
starts on a day every month has. Changing it re-cuts the window of every Period, past and in progress,
at once; nothing records the value a Period was lived under.

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
Each Category carries a name and a `bucket` of `daily` or `commitment` (below), fixed at creation. What
is planned for it is its **allocation**, which belongs to a Period's Budget, not to the Category.
_Avoid_: Budget (as a noun for a category — the scratch drafts' term "Budget/category" is retired), Tag.

**Budget**: The Household's plan for one Budget Period: its Monthly Budget and its Category
allocations. There is one per Period, versioned by effective date within it (see Mid-period Budget
changes), and a Period's Budget comes into being on the first read of that Period, carrying forward
the latest state of the nearest earlier Period that had one. A Period's window (derived from
`period_start_day`) and the rows stored for it are separate things: changing `period_start_day`
re-cuts the window and moves which label Today falls in, and rewrites no row.

**Allocation**: What a Category is planned at **in one Budget Period**: a value of that Period's Budget,
not a property of the Category, so a Period's allocation never moves when a later Period's does.
Optional for a Daily Category and required for a Commitment; zero is an allocation, and a Commitment's
is never cleared (a Daily Category's can be: "none" is a value too). Changing one changes the
**current** Period's Budget only; a following Period carries the latest state forward when it is first
read. A Commitment's allocation is part of the Commitments Budget and so moves the Daily Pool; a Daily
Category's moves only Unallocated.

**Deleting a Category** is soft and is the Undo-able act: it takes the Category out of every figure of the
current Period at once, touches no allocation row, and a restore returns the figures exactly. It is
refused (`CATEGORY_IN_USE`) while any **other** Period holds an allocation for the Category, because that
Period's Budget is a record; the current Period's own allocation never blocks. A Category that cannot be
deleted is **deactivated**, which changes no figure. **Restoring** is "as if it was never deleted": within the Period it was
deleted in the figures come back exactly; after a following Period was first read (its carry-forward
skipped the Category) the restore gives the Category what that carry-forward would have, its latest
state from the nearest earlier Period that holds an allocation for it, dated the current Period's first
day. A Daily Category whose latest state is "none", or that never had one, comes back with none.

> **Cross-app note:** Balances reserves **Category** for *Income* categories and **Tag** for
> user-defined grouping labels. Afloat's Category is a *spending* bucket. The two apps own separate
> vocabularies; the collision is documented rather than resolved. See §Flagged ambiguities.

**Commitment**: A Category whose spending is large, largely non-discretionary, and lumpy in time —
rent, insurance premiums, school fees, annual taxes, a planned appliance purchase. Commitment
spending is **excluded from every daily figure**: it does not reduce the Daily Pool's per-day maths,
does not move Trajectory Variance, and does not affect State. A Commitment Category **must** have an
allocation in every Period; that is the point of it. _Avoid_: Fixed cost, Bill.

**Daily Category**: Any Category that is not a Commitment. Its allocation is optional.

**Commitments Budget**: `Σ allocation` over Commitment Categories for the Period.

**Daily Pool** (`P`): `Monthly Budget − Commitments Budget`. The money the daily figures divide. This
single subtraction is why a rent payment on day two does not report a catastrophe.

**Unallocated**: `Daily Pool − Σ allocation over Daily Categories`. May be positive (headroom the
Household has not assigned) or negative (Category allocations promise more than the Budget holds).
Both are shown; neither is an error. This exists because Monthly Budget is authoritative and Category
allocations are advisory — the two are allowed to disagree, visibly.

**Expense**: The core record. A single outflow of money, at a moment, from a Pocket, classified into
a Category. Fields: `id` (client-generated UUIDv7), `occurred_on` (the Period Day it counts against;
Today, as the server reckons it, unless the person picks another), `occurred_time` (the time of day
it happened, optional, no timezone; orders a day's Expenses, falling back to `captured_at`),
`captured_at` (when it was recorded, stamped by the server), `amount`, `currency`, `pocket_id`, `category_id` (nullable),
`description` (free text, optional), `ownership` and, for a SoleOwner Expense, `owner_user_id`. _Avoid_: Transaction (implies a two-sided event
Afloat does not model), Entry, Spend (as a noun).

**Pocket**: A practical container the Household spends *from* — OVO, GoPay, a specific debit card,
cash in a wallet, a credit card. A Pocket exists so the Household can see where spending flows and so
that real-world behavioural barriers ("transport comes out of GoPay") are visible in the data. A
Pocket **holds no balance, has no transfers, and is never reconciled.** Fields: `id`
(client-generated UUIDv7), `pocket_display_name` (at most 60 code points, unique among the
Household's Pockets that are not deleted, case-insensitively), `active`. **Every Expense has exactly
one Pocket, and a Household always has at least one active Pocket**: founding creates the first, and
a delete or deactivate that would leave none is refused (`LAST_POCKET`), so capture is never blocked
for want of one. _Avoid_: Account, Wallet (both imply a balance), Source.

**Deactivate / Inactive**: Retiring a Pocket or Category that has history. An **Inactive** one stays
in the list, keeps its name, keeps labelling its Expenses and keeps its place in breakdowns; it is
only not offered for new capture: no new Expense names it, and no edit moves an Expense into it. An
Expense already in it stays, and deactivating is reversible. It is not a delete: a deleted Pocket is a
soft-deleted row, gone from the UI, that gives up its name and is refused while any Expense
references it; it can be restored (the Undo of a delete) unless another Pocket has taken the name. Neither act edits, moves or removes an Expense, or changes a figure. _Avoid_: Archive,
Close, Disable (each leaves it unclear whether history survives).

**Recycle Bin**: Where a deleted Expense goes. Deleting an Expense is a soft delete: it leaves every
list and figure, and can be restored from the Recycle Bin at any time. The Recycle Bin never empties;
nothing in it is ever erased. It holds Expenses only. A deleted Expense still counts as using its
Pocket and Category, so neither can be deleted (only deactivated) once it has labelled one. Restoring
an Expense never changes what it was: it comes back in the Pocket and Category it had, even one
deactivated since. _Avoid_:
Trash, Archive.

> **A Pocket is not a Balances Asset.** The same real-world OVO account may be a bank-account Asset in
> Balances and a Pocket in Afloat. They are unrelated records with unrelated purposes and must never
> be reconciled against each other. See §Flagged ambiguities.

**Top-up**: Moving money into a Pocket (bank → OVO, ATM withdrawal → cash wallet). **A top-up is not
an Expense.** The spend is the Expense; the top-up is invisible to Afloat. Recording top-ups
double-counts and is the single most likely way for a Household to corrupt its own numbers.

### Trajectory

**Baseline Daily Budget** (`B`, or `B_now` where it matters which day is meant): The slope of the target
line on the day it is read: *"if spending were even from here, this is the daily number."* It is
`Daily Pool ÷ Total Period Days` while the Daily Pool has not changed in the Period; after a change it
is the slope that carries the line from where it stood to the new pool by the Period's last day, and
never below zero (see Mid-period Budget changes). It is zero when the Household is **fully committed**.
Used for the target line and for reading the State.

**Today's Allowance** (`A`): The adaptive per-day figure for Today. It is **frozen against today's
spending**: it is made from settled days only, so buying a coffee does not move it. A rate that fell
every time you bought coffee would punish in real time, which §3 of `VISION.md` forbids. It is not
frozen against recomputation: nothing is stored (Recalculation, below), so it still moves inside the
day when something else it is made from changes: a future-dated Expense, an edit of a past Expense, or a
change of the Budget.

**Left to Spend Today**: `Today's Allowance − today's daily-bucket spend`. **This is the hero figure.**
It ticks down through the day, which is correct — it is a wallet, not a grade. May go negative, and is
displayed as such rather than clamped to zero.

**Forward Daily Rate**: What each remaining day *after* Today is worth at the current position. A
secondary figure, undefined on the last day of a Period (rendered "—").

**Remaining Daily Pool**: `P' − S_settled`: what the Available Pool still holds after the spending that
has settled before Today. In adaptive mode it is the number Today's Allowance is made of (`A × r` before the
floor), so it reconciles with Days Remaining and the allowance beside it: a pool of 6.000.000 with 3.000.000
settled and 15 days left reads 3.000.000, and the allowance is 200.000. In fixed mode the allowance is the
baseline and the two no longer reconcile; the figure is the same. It is frozen through Today (Today's spending is
no part of it), signed, and like every daily figure leaves out Commitments; zero or below is the Available
Pool used up. D8's money figure, shown with Days Remaining. _Avoid_: Remaining Budget (a Budget is the whole
plan), Remaining Daily Allowance.

**Trajectory Variance** (`V`): `T(e) − settled daily-bucket spend`, where `T(e)` is the target line's
cumulative target through the settled days (`B × e` while the Daily Pool has not changed in the Period).
Positive means under pace. Computed on **settled days only** — it does not move during Today. This
keeps the live wallet figure and the trend signal from restating the same fact against each other,
and stops the variance from lurching at every purchase. It is a money figure, and what the Household
reads as the pace. Absent while the Household is fully committed.

**Buffer Days**: `Trajectory Variance ÷ the Baseline Daily Budget in force`, signed, in days: how many
days' worth of baseline the Household is ahead of or behind. It is the input to State, and it scales with
Budget size automatically, so State thresholds need no per-Household tuning. It is not what the Household
is shown: the pace is stated in money (Trajectory Variance), and Buffer Days is not on the wire (#155 R7).
Undefined while fully committed.

**Fully committed**: The Household's baseline in force is zero: its Daily Pool is at or below zero, or it
cut its Budget to at or below what the target line had already reached, so the line is flat from there
(see Mid-period Budget changes). It is a statement about **the target line, not about money**: a cut to
2.500.000 on day 16 with 1.000.000 settled is fully committed and has Rp1.500.000 unspent. With no pace to
read there is no State, no Baseline Daily Budget and no Trajectory Variance; Today's Allowance and Left to
Spend Today are still answered, and a fixed-mode allowance is 0. Nothing says the plan is used up.
_Avoid_: used up, exhausted, spent (each says something about money).

**Available Pool used up**: `P' − S_settled ≤ 0`: what the Household has settled is at or past what the
pool held. It is a statement **about money, and independent of fully committed**: it can hold with a
healthy baseline. Adaptive Today's Allowance is zero or negative, and is shown as it is, negative as
negative; the State is computed as usual, and usually reads *taking on water*, though not always (a future-dated
Expense can use the pool up while the settled pace is afloat, and warm-up reads afloat).

**State**: One of `afloat` | `drifting` | `taking_on_water`. See §Calculation spec. There is none while
fully committed.

### Indonesian (id-ID) terms

The one id-ID word for each term. A slice that first shows a term in the UI adds its row here in the
same PR (`docs/agents/ui-conventions.md` § Copy); a translation is never decided only in the locale
JSON. Blank means not yet in the UI.

| Term | id-ID | Since |
|---|---|---|
| Household | rumah tangga | founding (#109), Household settings (#117) |
| User | | |
| Pocket | kantong | approved 22 Nov 2026 (#121) |
| Deactivate / Inactive | nonaktifkan / nonaktif | Pockets PR 2 (#125) |
| Delete | hapus | Pockets PR 3 (#121) |
| Home (the screen) | Beranda | UI retrofit |
| Period | periode anggaran | Household settings (#117) |
| Expense | | |
| Category | kategori | Categories (#130 PR 2), provisional (R16) |
| Daily (bucket) | harian | Categories (#130 PR 2), provisional (R16) |
| Commitment (bucket) | komitmen | Categories (#130 PR 2), provisional (R16); no settled word |
| `bucket` (the field, as a user reads it: **Kind**) | jenis | Categories (#130 PR 2), provisional (R16); en-GB word is "Kind" |
| Allocation | alokasi | Categories (#130 PR 2), provisional (R16) |
| Budget | anggaran | Budget (#130 PR 1), provisional (R16) |
| Monthly Budget | anggaran bulanan | Budget (#130 PR 1), provisional (R16) |
| Commitments Budget | anggaran komitmen | Budget (#130 PR 1), provisional (R16) |
| Daily Pool | dana harian | Budget (#130 PR 1), provisional (R16) |
| Unallocated | belum dialokasikan | Budget (#130 PR 1), provisional (R16) |
| `expected_monthly_income` (as a user reads it: **Expected monthly income**) | penghasilan bulanan yang diharapkan | Budget (#130 PR 5), provisional (R16) |
| Recycle Bin | tempat sampah | Recycle Bin (#140 PR 5), provisional |
| Restore | pulihkan | Recycle Bin (#140 PR 5), provisional |
| State names, *ahead of pace*, Buffer Days | | **[OPEN Q-10]** |

---

## Calculation spec

All figures below concern the **daily bucket only**. Commitment spending is excluded from every one
of them and reported separately.

### Symbols

| Symbol | Meaning |
|---|---|
| `P` | Daily Pool in force Today (it may have changed in the Period) |
| `D` | Total Period Days |
| `e` | Days Elapsed (settled days, strictly before Today) |
| `r` | Days Remaining (inclusive of Today); `r = D − e` |
| `T(j)` | The cumulative target through day `j` of the Period; `T(0) = 0` (§Mid-period Budget changes) |
| `B_now` | The slope of the target line on Today; `P / D` while the pool has not changed in the Period; never negative |
| `S_settled` | Daily-bucket spend with `occurred_on` in the current Period and strictly before Today |
| `S_today` | Daily-bucket spend with `occurred_on` = Today |
| `S_future` | Daily-bucket spend with `occurred_on` after Today and on or before the current Period's last day (future-dated) |

Every spend figure is bounded by the current Budget Period: an Expense dated in any other Period, earlier
or later, counts toward that Period's figures and none of this one's (Q-25, ruled 24 Nov 2026).

### Core figures

```
B  (Baseline Daily Budget) = B_now = max(P / D, 0)         if the Daily Pool has not changed in the Period
                           = the slope of the target line on Today    otherwise (§Mid-period Budget changes)
P' (Available Pool)        = P − max(S_future, 0)
A  (Today's Allowance)     = (P' − S_settled) / r          [adaptive mode]
A                          = B                             [fixed mode]
Left to Spend Today        = A − S_today
Forward Daily Rate         = (P' − S_settled − S_today) / (r − 1)     if r > 1
                           = undefined ("—")                          if r = 1
V  (Trajectory Variance)   = T(e) − S_settled              (T(e) = B × e if the pool has not changed)
Buffer Days                = V / B                         if B > 0
                           = undefined, and so is State    if B = 0  (fully committed)
```

`P'` takes `max(S_future, 0)`, not `S_future`: it is the net of future-dated spend, and a refund dated in
the future never raises Today's Allowance (#155 R3). A refund dated before Today lowers
`S_settled` and so raises it; one dated Today raises Left to Spend Today.

### Why it behaves at the edges

- **Day one** (`e = 0`, `r = D`): for a positive pool, `A = P/D = B`, `V = 0`, State is `afloat`
  (at or below zero the Household is fully committed: no State). Nothing pretends to know
  anything yet.
- **Last day** (`r = 1`): `A` equals the entire remaining pool. Under the old "remaining daily
  allowance" framing this read as nonsense; framed as **Left to Spend Today** it is simply true and
  useful — it is the last day, and that *is* what is left. Forward Daily Rate is undefined and shown
  as "—" rather than dividing by zero.
- **Adaptive borrowing.** Under-spending early raises `A`, which is honest but invites spending the
  slack. The UI therefore shows `B` beside `A` (except while fully committed, when there is no `B`), so an inflated allowance is visibly borrowed
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
    away from zero (shopspring `Round`). State is decided from the *exact*
    (unrounded) `V` and `B` — compared as `V` against `threshold × B`, with no division — never from
    the rounded Buffer Days figure, so a boundary can round to a tidy number while State still reads
    the far side of it.

### Adaptive vs fixed

Adaptive is the default. Fixed (`A = B` always) is available as an advanced Household setting. The
setting changes only `A`; `V`, Buffer Days and State are read against the same target line in both
modes. After a mid-Period change, a fixed allowance is `B_now`, the slope in force Today; while fully
committed it is 0, never a negative raw `P / D`.

### State thresholds

```
Buffer Days ≥ −1                      → afloat
−3 ≤ Buffer Days < −1                 → drifting
Buffer Days < −3                      → taking_on_water
```

Buffer Days here is `V ÷ B_now`, and State is decided from `V` against `threshold × B_now`, so a Household
that changed its Budget mid-Period is read against the pace it now has. While fully committed there is no
State.

For a Period whose Daily Pool never changed, `Buffer Days < 0` is exactly equivalent to *"projected
end-of-period spend, at the average pace so far, exceeds the Daily Pool"* — the two candidate signals are
the same signal, so Afloat computes one. After a change the line has a kink and the equivalence no longer
holds, which is why `V` is defined against the line and not against a projection.

**Warm-up rule.** While `e < 3`, State is pinned to `afloat`. Three settled days is too little
evidence to call a trend, and a State that panics on day two is how a Household stops opening the app
in month one. **[OPEN Q-05]** — whether three days is the right floor, and whether to surface a
distinct neutral "getting a read" presentation rather than reusing `afloat`.

**Thresholds are configuration, not constants.** They live in one place and are expected to be tuned
once real data exists. Both the warm-up floor (`e < 3` above) and the two State thresholds
(`drifting_below: -1`, `taking_on_water_below: -3`) are **arguments to the calculation**, not literals
inside it — `contract/testdata/trajectory.json`'s `parameters` section carries the defaults, and any
row may override them, so a future change to the defaults is a fixture edit plus the matching edit to
`DefaultParameters()`, and a test pins the second to the first so they cannot drift apart.

### Future-dated Expenses

An Expense may be dated after Today. It reduces the Available Pool immediately (`P'`), so the
allowance reflects money already spoken for. A future-dated **refund** does not raise it: `P'` takes
`max(S_future, 0)`. It does **not** count as a settled day, does **not**
enter `V`, and does **not** appear on the actual cumulative line before its date. On the day it
arrives it becomes an ordinary `S_today`.

### Mid-period Budget changes

Budgets are **versioned events with effective dates**; Expenses are mutable current state. Changing
the Daily Pool mid-Period does not rewrite history. **Any change of the Daily Pool is one**: the Monthly
Budget, or a Commitment's allocation (which moves the Commitments Budget and so the pool). A Daily
Category's allocation moves only Unallocated, and is none.

The target cumulative line `T` **kinks, continuously**. If the pool changes effective day `j`
(1-indexed), let `T(j−1)` be the target cumulative through day `j−1`, **kept as an exact value** across
every earlier change. From day `j` onward:

```
B_new = (P_new − T(j−1)) / (D − (j − 1))
slope = max(B_new, 0)
```

The line has no vertical jump at `j`, and it still terminates exactly at `P_new` on day `D` unless the
clamp engages. A retroactive redraw was rejected: it silently falsifies "you were ahead of pace
yesterday". A pool in force from the Period's first day (a Budget set on day 14 is read as its Budget from
day 1) is no change and no kink.

**Every slope is clamped at zero, the first segment's included.** A Daily Pool at or below zero from day 1
is a flat target at zero, and a later raise starts from `T(j−1) = 0`: a Monthly Budget of 6.000.000 with
Commitments of 7.000.000 (`P` = −1.000.000), raised to `P` = 2.000.000 on day 11, has slope 100.000 from
day 11 and ends at 2.000.000; unclamped it would have run at 116.667 from `T(10)` = −333.333. A cut to at or
below `T(j−1)` holds the line flat at `T(j−1)` for as long as that pool stays, and a later raise starts from
the flat value: 6.000.000, cut to 2.500.000 on day 16, raised to 5.000.000 on day 21, has slopes 200.000, 0
and 200.000, and `T(30)` = 5.000.000.

**What the figures read.** `V = T(e) − S_settled`; Buffer Days and the State thresholds use `B_now`, the
slope in force Today; a fixed-mode allowance is `B_now`. With no change `T(e) = B × e`, so a Period whose
pool never moved and was positive, with no future-dated refund, is read exactly as before. Where no spend is future-dated and the last segment is
unclamped, the line ends on `P`, so `V = r × (A_adaptive − B_now)`: the pace and the allowance are one
statement.

**Fully committed** and **Available Pool used up** (the glossary) are the two situations a cut and an
empty pool put a Household in, and they are independent: the first is about the line (the baseline in force
is zero), the second about money (`P' − S_settled ≤ 0`). In the first there is no State, baseline or
variance and nothing says the plan is used up; in the second the allowance reads zero or negative and the
State is computed as usual.

**Consequences, ruled and accepted** (#155 R2):

- A cut can turn *afloat* into *drifting* with no new spend: 6.000.000 cut to 4.000.000 on day 16 with
  3.150.000 settled has `V` = −150.000 against `B_now` of about 66.667, Buffer Days −2.25, *drifting*; before
  the cut the same spend read −0.75.
- A raise does not cure an overspend already incurred: raised to 7.500.000 on day 11 with 2.600.000 settled,
  `V` = −600.000, Buffer Days −2.18, *drifting*, where a flat reading would have said *afloat*.
- Just above the clamp the State is a hair trigger. A cut to 3.000.100 after a target of 3.000.000 on day 16
  gives a `B_now` of about 6.67 rupiah a day, so an overspend of Rp7 reads *drifting* and one of Rp25 *taking on
  water*. At zero there is no State.
- Deleting or restoring a Category carries its allocation from the Period's first day ("as if it was never
  deleted"), so it moves the line retroactively for that Category. That is the Category rulings' choice, not
  changed here.

### Recalculation

Every figure above is derived from current data on read. There are no stored aggregates and no
snapshots in the MVP. Editing or deleting a past Expense correctly changes the past — including
yesterday's State as displayed in history. That is the intended behaviour: Afloat shows what is true
now, not what it believed at the time. Today's Allowance is frozen against *today's spending*, not against
this: a future-dated Expense, an edit of a past one or a change of the Budget moves it inside the day.

### Worked check

These are the reference values the implementation must reproduce. Daily Pool `P` = Rp6.000.000 over
`D` = 30 days, so `B` = Rp200.000/day.

The table below rounds to whole rupiah for readability. The authoritative values are the 4dp ones in
`contract/testdata/trajectory.json`'s `figures` rows (BOOTSTRAP.md §6) — e.g. the day-2 row's `A` is
`203448.2758`, which the table shows as `203.448`. The backend's unit tests read the fixture, never
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

**The same Household after a change of the Daily Pool.** `D` = 30; every row's values are in
`contract/testdata/trajectory.json`'s `figures` rows (the table rounds to whole rupiah), each computed
independently in exact rationals. `P` is the pool in force Today.

| Case | `e` | `P` | `S_settled` | `A` | `B_now` | `V` | Buffer | State |
|---|---|---|---|---|---|---|---|---|
| 6.000.000 → 7.500.000 on day 11, exactly on the line | 10 | 7.500.000 | 2.000.000 | 275.000 | 275.000 | 0 | 0.00d | afloat |
| The same, 600k over the line's 2.000.000 | 10 | 7.500.000 | 2.600.000 | 245.000 | 275.000 | −600.000 | −2.18d | drifting |
| 6.000.000 → 4.000.000 on day 16, on the line | 15 | 4.000.000 | 3.000.000 | 66.667 | 66.667 | 0 | 0.00d | afloat |
| The same, 150k over, with no new spend after the cut | 15 | 4.000.000 | 3.150.000 | 56.667 | 66.667 | −150.000 | −2.25d | drifting |
| 6.000.000 → 7.500.000 on day 11 → 6.500.000 on day 21 | 20 | 6.500.000 | 4.400.000 | 210.000 | 175.000 | +350.000 | +2.00d | afloat |
| Cut to 2.500.000 on day 16, 1.000.000 settled (fully committed, money left) | 15 | 2.500.000 | 1.000.000 | 100.000 | 0 | no State | no State | no State |
| Cut to 2.800.000 on day 16, 3.000.000 settled (fully committed, pool used up) | 15 | 2.800.000 | 3.000.000 | −13.333 | 0 | no State | no State | no State |
| Fixed mode, cut to 2.500.000 on day 16, 1.000.000 settled | 15 | 2.500.000 | 1.000.000 | 0 | 0 | no State | no State | no State |
| Pool used up with a healthy baseline, 6.200.000 settled | 20 | 6.000.000 | 6.200.000 | −20.000 | 200.000 | −2.200.000 | −11.00d | taking on water |

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
