# Afloat — Product Requirements

> **Status:** **Draft 1** — 13 September 2026. Implementation-agnostic; applies to both backends.
>
> **Companions:** [`BOOTSTRAP.md`](BOOTSTRAP.md) — the ratified scaffold sheet: naming, stack, auth,
> schema conventions, migrations, contract tooling. Read it before writing code.
> [`VISION.md`](VISION.md) — why Afloat exists, and what it refuses to be (stable).
> [`CONTEXT.md`](CONTEXT.md) — domain language and the full calculation spec (authoritative for
> arithmetic; this document does not restate formulas).
>
> **Annotations.** `**[OPEN Q-nn]**` marks something deliberately unresolved; all of them are
> collected in §11. `**[ASSUMED]**` marks something written as a decision that has not actually been
> ratified — challenge it or promote it.
>
> Supersedes `docs/scratch/Product Vision and PRD Foundation Draft.md`. Architecture and repo
> decisions live in `docs/scratch/Decisions Draft.md` pending promotion to ADRs.

---

## 1. Summary

Afloat is a household spending-trajectory app. A Household sets a Monthly Budget, records Expenses in
seconds, and gets one number on the first screen: **how much is left to spend today**, with a State
telling it whether the trajectory is healthy.

It is deliberately not an accounting system. Budgets are expectations, not contracts; records are
mutable; the app informs and never enforces.

## 2. Users and context

### Primary user

The author's Household, replacing an in-use Google Sheets workflow (Ledger / Totals / Dashboard /
Lookups). Indonesian, IDR, `Asia/Jakarta`, spending across e-wallets (OVO, GoPay), bank cards and
cash. This is a real user with a real dataset, not a persona.

### Secondary audience

Recruiters and engineers reading the public repository. This is a stated goal of the project and it
sets a floor on polish: the UI, the README and the docs are part of the deliverable, not decoration
around it.

### Household composition

Afloat is **Household-first**, mirroring Balances. Multiple Users share one set of data, and Expenses
carry an Ownership mode (SoleOwner / Joint). A spending tracker that cannot see a spouse's spending
measures a fraction of a household's outflow and reports it as the whole.

### What the user does today

| Sheet | Contents | Afloat equivalent |
|---|---|---|
| Ledger | Date, Pocket, Description, Amount, Budget/category | Expense capture |
| Totals | Period totals, per-Pocket, per-Category, days in month, days remaining, baseline, remaining allowance, variance, daily totals | Derived on read; no stored aggregates |
| Dashboard | Daily spend graph, target vs actual cumulative lines, variance gauge | Dashboard, minus the gauge |
| Lookups | Pockets, Categories, planned amounts | Setup |

The gauge is dropped. It encodes one scalar in a shape that reads slower than the scalar.

## 3. Goals and success metrics

Success is measured against the spreadsheet, because the spreadsheet is the incumbent and it works.

| # | Metric | Target | How measured |
|---|---|---|---|
| G1 | The Sheet is abandoned | No new rows in the Ledger sheet for 30 consecutive days | Manual check |
| G2 | Capture is fast enough to actually happen | Median capture, app-open to saved, **under 10 seconds** | Timed by hand, periodically. No instrumentation. |
| G3 | The app is opened | ≥ 4 distinct days per week, per active User | Derived from the `captured_at` distribution |
| G4 | The headline number is trusted | End-of-Period actual within **±10%** of the Budget, sustained for 3 Periods | Derived |
| G5 | Capture is complete | Unattributed share of the Balances residual **< 20%** | Cross-checked manually against Balances |

> **Q-01 resolved (2026-09-13):** **no telemetry of any kind.** G3 falls out of the `captured_at`
> distribution for free and G2 is timed by hand, which deletes an events table, an endpoint, a
> frontend pipeline and a privacy conversation from a self-hosted app. G4 and G5 stand; the ±10% and
> <20% figures are still guesses and should be revisited once three Periods of real data exist.

**Explicit non-goals as metrics:** number of Expenses recorded, streaks, "budget adherence" scores.
Each would push the product toward the enforcement posture §3 of `VISION.md` rejects.

## 4. MVP scope

### In

- Household, Users, Ownership on Expenses. Local email+password auth with a server-side session.
  Invitations by one-time link shown in the UI.
- Budget Period with configurable `period_start_day`.
- Monthly Budget; Categories with `daily` / `commitment` buckets and optional allocations; Pockets.
- Expense capture, edit, soft-delete, restore.
- Dashboard: hero figure, State, target-vs-actual cumulative chart, Category and Pocket breakdowns,
  Commitments panel.
- Adaptive and fixed allowance modes.
- New-Period rollover: copy previous Period's Budget, or use a recurring default.
- PWA shell, installable, mobile-first capture. **Online-only writes.**
- Read API for attributed expense data; write endpoint for `expected_monthly_income`.
  Session-authenticated — API keys are planned, not built (§4.1).

### Out

Income tracking as events. Transfers and top-ups. Pocket balances. Reconciliation. Bank or e-wallet
sync. Bank-statement import. Multi-currency (schema carries `currency`; the UI is pinned to the
reporting currency, mirroring Balances' `multi_currency_enabled` default-off). Forecasting beyond the
State model. Historical budget suggestions. Seasonality. Backup / restore / erasure. Offline write
queue (see §8.3). Notifications. Email. Telemetry.

### 4.1 Planned and committed

Not in MVP, but **not speculative** — each is a design constraint, and `BOOTSTRAP.md` §10 records what
must not be foreclosed.

- **Google OAuth.** A second filter chain, enabled by keeping credentials in their own table from day
  one rather than a column on `users`.
- **Email.** Invitations and password reset by email, replacing the on-screen link and the CLI reset.
  The invitation token model is built as though email will deliver it.
- **Scoped, rotatable API keys**, per Household, for the Balances read path. The MVP read endpoints
  must therefore assume nothing about a human session beyond authentication.
- **Import.** Spreadsheet import with **app-provided templates**, mirroring Balances' flow. The
  vocabulary is reserved now: *Import* = template-based ingest into a live Household, *Export* = the
  spreadsheet download, *Backup / Restore* = whole-Household and separate.

## 5. Setup requirements

### 5.1 Identity and onboarding

- **S1.** A new verified identity resolves, once, to either founding a Household or joining one by
  invitation. The binding is irreversible; a User belongs to exactly one Household. Mirrors Balances.
- **S2.** All Users in a Household have equal, full read/write access. No roles, no permission tiers.
- **S3.** Household settings: `display_name`, `reporting_currency` (default IDR), `period_start_day`
  (default 1), `day_starts_at` (default 04:00), `expected_monthly_income` (nullable),
  `allowance_mode` (default adaptive).
- **S4.** User settings: `display_name`, `email`, `locale` (default `en-GB`), `time_zone` (default
  `Asia/Jakarta`).

- **S2a.** MVP authentication is **local email + password**, server-side session, host-only session
  cookie. **Users carry no credential column** — credentials live in their own table, so an
  OAuth-only user is representable and Google OAuth later is an additive filter chain rather than a
  migration of the user model.
- **S2b.** Invitations are a one-time link generated and shown in the UI; password reset is a CLI
  command on the instance. Both are replaced by email later.

> **Q-02 resolved (2026-09-13).** Rationale and cookie attributes in `BOOTSTRAP.md` §5.

### 5.2 The first Budget

The Monthly Budget is the single input every other figure derives from, and a Household typing a
number from thin air produces confident nonsense downstream. Onboarding must therefore ground it:

- **S5.** Onboarding asks for `expected_monthly_income` and proposes a Monthly Budget as a share of
  it, editable. If income is left blank, the Household types a Budget directly.
- **S6.** Where the Monthly Budget exceeds `expected_monthly_income`, Afloat says so plainly, once,
  and proceeds. It does not block. (Consistent with *inform, don't enforce*.)
- **S7.** `expected_monthly_income` is writable over the API by any authorised client. Afloat has no
  knowledge of what writes it.

> **Q-03 resolved (2026-09-13): start fresh.** Import is planned as a real, template-driven feature
> (§4.1), not built for MVP and not faked with a one-off script. The consequence is accepted
> knowingly: the first Monthly Budget has no historical anchor, and S5's share-of-income proposal is
> the only grounding it gets.

### 5.3 Categories and Pockets

- **S8.** A Category has a name, a `bucket` (`daily` | `commitment`), and an optional `allocation`.
  Commitment Categories **require** an allocation.
- **S9.** Monthly Budget is authoritative. Category allocations are advisory and need not sum to it.
  The difference is surfaced as **Unallocated**, positive or negative, without being treated as an
  error.
- **S10.** A Pocket has a name and a `expense_date_basis` of `charge_date` (default) or `due_date`.
  The basis is a **per-Pocket setting**, never a question asked during capture — a per-Expense choice
  would put an accounting decision in the middle of the ten-second path.
- **S11.** Pockets hold no balance. There is no transfer entity, and top-ups are not Expenses. The
  capture UI must make this hard to get wrong. **[OPEN Q-08]** — UI mechanism only, no schema impact.
  No speculative `kind` enum on Pocket: `expense_date_basis` already covers the one behavioural
  difference between Pocket types.

### 5.4 Period rollover

- **S12.** On the first day of a new Budget Period, the Household's Budget, Categories and
  allocations carry forward from the previous Period unchanged, unless a recurring default is set.
- **S13.** Rollover is silent, and **lazily materialised on first read of a Period** — never a
  scheduled job. That keeps a scheduler out of both backends, avoids leader election if more than one
  instance ever runs, and keeps the parity matrix purely HTTP instead of growing a hand-maintained
  background-jobs section. **Q-09 resolved (2026-09-13).**

## 6. Expense capture

The ten-second target (G2) is the constraint every decision here answers to.

- **E1.** Capture is reachable in one tap from a cold app open, and is the app's default action on
  mobile.
- **E2.** Required to save: **amount only**. Pocket, Category, date and description all have defaults
  or may be left empty.
- **E3.** Defaults: `occurred_on` = Today; Pocket = the Household's most-used Pocket, or last used;
  Category = empty. Ownership = Joint.
- **E4.** An uncategorised Expense is valid, counts toward the daily bucket, and is surfaced in a
  "needs a category" list for later. Capture first, organise later.
- **E5.** Every field is editable after the fact, with no exception path and no confirmation friction.
- **E6.** Deletion is soft, mirroring Balances (ADR-0007). A Recycle Bin makes deletion recoverable;
  hard delete is not a UI feature.
- **E7.** Amounts may be negative, representing a refund with no recorded original. Zero is invalid:
  `CHECK (amount <> 0)`. **Q-07 resolved (2026-09-13).**
- **E8.** `occurred_on` may be in the future. See `CONTEXT.md` for the effect on figures.
- **E9.** The Expense `id` is **client-generated (UUIDv7)** and create is **idempotent** on it. See
  §8.3 — this is the offline-readiness contract, and it is required in MVP even though the queue is
  not.
- **E10.** `captured_at` is recorded separately from `occurred_on`.

## 7. Dashboard

The first screen answers the core question with no navigation.

- **D1. Hero: Left to Spend Today.** One number, dominant. Floored to Rp1.000. Negative is shown as
  negative, not clamped.
- **D2. State.** One of Afloat / Drifting / Taking on water, with the waterline treatment. Always
  beside the numbers, never instead of them.
- **D3. Pace, in plain language.** "Rp420k ahead of pace", not a signed delta. Localised Indonesian
  wording is unresolved — **[OPEN Q-10]**.
- **D4. Baseline beside the adaptive figure.** So an inflated allowance reads as borrowed from the
  Household's own future rather than as found money.
- **D5. Cumulative chart.** Target line versus actual line for the Period. The target line kinks at
  Budget changes and never redraws history (`CONTEXT.md`). Today is drawn as in-progress.
- **D6. Commitments panel.** Planned versus paid, and what is still due this Period. Separate from
  every daily figure.
- **D7. Category and Pocket breakdowns.** Spend, allocation where set, and variance. Over-allocation
  is presented as information, never as a violation.
- **D8. Remaining Budget and Days Remaining**, as supporting figures.
- **D9. No gauge chart.**

> **Q-11 resolved (2026-09-13): capture is the launch surface.** Capture, not the dashboard, is the
> reason to open Afloat, so it is the default route on mobile (E1). Notifications stay out of scope;
> the service worker does app-shell caching only, and web push slots in later without redesign.

## 8. Non-functional requirements

### 8.1 Tenancy and data

- **N1.** Belt-and-suspenders tenancy: every query touching Household-scoped data filters
  `household_id` **in SQL**, not only in middleware. Mirrors Balances.
- **N2.** Monetary values `DECIMAL(20,4)`; decimals serialised as **strings** on the wire. Mirrors
  Balances (ADR-0011).
- **N3.** Every monetary value carries its `currency` even while the UI is pinned to the reporting
  currency.
- **N4.** Soft-delete everywhere. Hard delete is not exposed.
- **N5.** No stored aggregates or derived snapshots in MVP. Every figure recomputes on read.
  **[ASSUMED]** — acceptable at single-Household data volumes; revisit if it stops being true.
- **N5a.** **Trajectory is computed server-side, in both backends.** The client formats and floors for
  display but derives nothing. One source of truth, real domain logic in the Kotlin track rather than
  a CRUD shell, and a parity gate that verifies something. `contract/testdata/trajectory.json` carries
  the `CONTEXT.md` reference rows as a fixture both backends' unit tests read.

### 8.2 Contract

- **N6.** `contract/openapi.yaml` is the single source of truth; both backends satisfy it and the
  frontend generates its types from it.
- **N7.** A single shared error envelope across both backends, following the Balances ADR-0027 shape.
  It carries **codes, not messages**; the frontend owns every user-facing string.
- **N7a.** The contract is **hand-written and authoritative**; the Go, Kotlin and TypeScript artefacts
  are generated from it and CI checks they are in sync. This is contract-first, unlike Balances, which
  is code-first — that tooling cannot be lifted across.
- **N8.** `db/migrations/` is canonical and Flyway-native, and the source from which the Go side's
  goose files are **generated and committed**. The Decisions draft's plan of one directory consumed
  directly by both tools does not work; see `BOOTSTRAP.md` §7.
- **N8a.** One Postgres instance, **two databases**. Never one shared schema — two migration ledgers
  over one schema is a corruption path.

### 8.3 Offline readiness

The offline write queue is **out of MVP**. The contract that makes it cheap to add is **in MVP**,
because retrofitting it is a cross-backend contract change plus a migration, and adding it later on
top of these four rules is a frontend-only change:

- **N9.** Client-generated UUIDv7 Expense ids (E9).
- **N10.** `POST /expenses` is idempotent on the client-supplied id and safe to replay.
- **N11.** The create payload is fully self-describing — no server round-trip is required to compute
  defaults before a write. Capture must never depend on the server telling the client the current
  allowance or a default Category.
- **N12.** `captured_at` distinct from `occurred_on` (E10), so a queued Expense keeps its true capture
  time when it lands hours later.

### 8.3a Internationalisation

- **N12a.** **en-GB and id-ID from day one**, both locale files real, i18next, matching Balances.
  Beyond avoiding a week of string extraction later, this is a product gate: whether the nautical
  state vocabulary survives translation into Indonesian must be known **before** the brand session
  builds an identity around it. See Q-10.

### 8.4 Client

- **N13.** Mobile-first. Capture is designed for a phone at the point of sale; the dashboard is the
  richer large-screen view.
- **N14.** Installable PWA with an app-shell service worker. Reads may be served stale from cache;
  writes require connectivity in MVP and must fail visibly rather than silently dropping.

## 9. Brand and visual identity

Not started, and correctly sequenced **after** the State model — palette and mark are downstream of
what the three States mean. Scoped as a dedicated follow-on session producing an ADR plus
`docs/brand/`, following Balances' precedent: assets **generated from a script**, never hand-drawn in
a design tool, so the set is reproducible.

Constraints inherited from the domain, to be honoured by whatever is designed:

- **No red/green good-bad pairing.** Overspending is not a moral failure and Afloat does not colour it
  as one. Colour-blind safe throughout.
- **No currency symbol in the mark** — the schema is multi-currency-capable even where the UI is not.
- **Culturally neutral**, Indonesian retail context.
- **The waterline is the identity** (`VISION.md` §4). No mascot.
- Must survive at 16px, and must degrade to a single line of colour in a dense list row.
- Light and dark, matching Balances' theme discipline.

Deliverables for that session: concept, palette (including the three State colours), wordmark, glyph,
favicon, app icon, social card, `docs/brand/logo.md`, `gen.py`, `make brand`.

> **[OPEN Q-12]** There is an **Afloat Budgeting** app on the App Store and a **Keep Afloat**. Not a
> blocker for a self-hosted household tool, but it affects domain availability and how the repo reads
> as a portfolio piece. Confirm the name before any brand work begins — a rename after the identity
> is generated is the expensive ordering.

## 10. Explicit non-goals

Restated here because each was raised and rejected on purpose, and each will be proposed again:

- Blocking, gating or warning-before-spend. Afloat informs.
- Streaks, scores, badges, or any adherence metric.
- Reconciliation of Pockets against real balances.
- Detecting missing or unrecorded money. That is the Balances residual's job.
- A "close the period" ritual or any approval workflow.
- Double-entry, immutable events, or compensating entries. Refunds are edits.
- Any Afloat-side knowledge of Balances.

## 11. Open questions

Struck as resolved on 2026-09-13, each recorded in place above and in `BOOTSTRAP.md`: **Q-01** no
telemetry · **Q-02** local auth, credentials in their own table · **Q-03** start fresh, Import planned
· **Q-07** negatives allowed, `CHECK (amount <> 0)` · **Q-09** lazy rollover, no scheduler · **Q-11**
capture is the launch surface · **Q-12** name kept.

| # | Question | Blocks |
|---|---|---|
| Q-04 | Cap the displayed adaptive allowance at some multiple of baseline, or rely on showing baseline beside it? | Dashboard |
| Q-05 | Is a 3-settled-day warm-up the right floor, and should warm-up have its own neutral presentation rather than reusing `afloat`? | State model |
| Q-06 | Presentation when a mid-period Budget cut lands below the already-targeted cumulative | Chart |
| Q-08 | What UI mechanism actually prevents top-ups and ATM withdrawals being recorded as Expenses? Copy alone will not do it. | Capture |
| Q-10 | Indonesian terminology for the trajectory vocabulary — *ahead of pace*, *Drifting*, *Taking on water*. The State names may not survive translation intact. | Copy, i18n, brand |
| Q-13 | Cash discipline. Every cash spend must be recorded individually or the Cash Pocket silently under-reports. Is a periodic "cash reconciliation" adjustment Expense worth the conceptual cost? | Post-MVP, probably |
| Q-14 | Backup / restore / erasure. Balances has all three, including a GDPR erasure path. Afloat holds spending data, which is arguably more sensitive. Defer, or parity from the start? | Post-MVP scope |
| Q-15 | Does a Household ever need more than one Budget Period shape — e.g. a weekly view for the daily bucket? | Post-MVP |

## 12. Deferred — not commitments

Recurring and annual expense planning as first-class objects. Projected end-of-Period spend as an
explicit figure. Historical budget suggestions after N Periods, always phrased as "consider" and
always editable. Seasonality (December dining, Lebaran). Cross-Category and cross-Period pattern
recognition. Bank-statement import for invisible spending such as account and processing fees. More
sophisticated allowance models separating discretionary from periodic obligations beyond the
two-bucket split. Multi-currency UI. An "Underwater" fourth State for an exhausted Budget.

## 13. Change log

| Date | Change |
|---|---|
| 2026-09-13 | Draft 2 — technical-gate session. Resolved Q-01, Q-02, Q-03, Q-07, Q-09, Q-11, Q-12. Added §4.1 Planned and committed; N5a server-side trajectory; N7a contract-first; N8/N8a migration scheme and two databases; N12a i18n from day one. `BOOTSTRAP.md` added as the scaffold sheet. |
| 2026-09-13 | Draft 1. Synthesised from the two scratch drafts and a grill session. Decisions taken: Household-first tenancy mirroring Balances; two-bucket Commitment split in MVP; configurable `period_start_day`; `expected_monthly_income` as the sole inbound integration; "Taking on water" replacing "Sinking"; no mascot; hero figure reframed from *remaining daily allowance* to *left to spend today*; variance settled-through-yesterday; offline queue deferred but its contract locked. |
