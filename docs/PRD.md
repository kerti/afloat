# Afloat — Product Requirements

> **Status:** **Draft 1** — 13 September 2026. Implementation-agnostic.
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
sync. Bank-statement import. Multi-currency **[OPEN Q-16]** (schema carries `currency`; the UI is pinned to the
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

- **S1.** A new identity resolves, once, to either founding a Household or joining one by
  invitation, at an **onboarding gate** it reaches only through a short-lived handshake (S2b). The
  binding is irreversible; a User belongs to exactly one Household. Mirrors Balances. Until
  invitations land the gate offers founding only.
- **S2.** All Users in a Household have equal, full read/write access. No roles, no permission tiers.
- **S3.** Household settings: `household_display_name`, `reporting_currency` (default IDR; an ISO 4217
  code), `period_start_day` (default 1; 1 to 28, the highest day every month has, so every Period
  starts on a day that exists), `day_starts_at` (default 04:00), `expected_monthly_income` (nullable;
  denominated in `reporting_currency`), `allowance_mode` (default `adaptive`; `adaptive` or `fixed`).
  Any member may change them (S2). Today the settings screen writes three: `household_display_name`,
  `reporting_currency` and `period_start_day`; the others follow with the slices that consume them.
- **S4.** User settings: `user_display_name`, `email`, `locale` (default `en-GB`; `en-GB` or `id-ID`),
  `time_zone` (default `Asia/Jakarta`; an IANA name) and the password. Each User changes only their own:
  S2's equal access is about the Household's data, and a name, a language and a zone are personal.
  Changing the email or the password requires the current password, and the password policy is S2b's.
  The address change is not verified, because no email exists (§4); it takes effect at once. A password
  change signs the User out everywhere else.

- **S2a.** MVP authentication is **local email + password**, server-side session, host-only session
  cookie. **Users carry no credential column** — credentials live in their own table, so an
  OAuth-only user is representable and Google OAuth later is an additive filter chain rather than a
  migration of the user model.
- **S2b.** **Registration is open, and founding is gated.** Anyone who reaches the instance can
  register while local auth is on, but registering creates no account: it parks the credential in a
  15-minute onboarding handshake, and the User, the Household and the credential are created together
  only when the person commits at the gate. An operator sets `FOUNDING_DISABLED=true` to stop any
  further Household being founded (`BOOTSTRAP.md` §12). Operators found their own Household *before*
  exposing the instance and then set it: until they do, the first person through the gate founds one.
  Passwords are at least 10 code points and not in a common-password list, at registration and at a
  later change (S4, which also requires the current password); a registered address is
  answered `EMAIL_TAKEN` (registration enumerates, login does not). Invitations are a one-time link
  generated and shown in the UI and will add a join choice to the same gate; password reset is a CLI
  command on the instance. Both are replaced by email later. Mechanics in `BOOTSTRAP.md` §5.1.1.

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
- **S10.** A Pocket has a `pocket_display_name` (at most 60 code points, trimmed, unique among the
  Household's Pockets that are not deleted, active or inactive, compared case-insensitively) and is
  active or inactive. Any member may change them (S2). A Pocket may be **deactivated** (kept in
  history, not offered at capture, reversible) or **deleted** (gone from the UI, undoable from a
  toast, and refused while any Expense references it). Neither act touches an Expense.
- **S11.** Pockets hold no balance. There is no transfer entity, and top-ups are not Expenses. The
  capture UI must make this hard to get wrong. **[OPEN Q-08]** — UI mechanism only, no schema impact.
  No speculative `kind` enum on Pocket: nothing in a Pocket's behaviour differs by type, so there is
  nothing for one to carry.

### 5.4 Period rollover

- **S12.** On the first day of a new Budget Period, the Household's Budget, Categories and
  allocations carry forward from the previous Period unchanged, unless a recurring default is set.
- **S13.** Rollover is silent, and **lazily materialised on first read of a Period** — never a
  scheduled job. That keeps a scheduler out of the backend, avoids leader election if more than one
  instance ever runs, and keeps the conformance suite purely HTTP instead of growing a hand-maintained
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
  currency. The one exception is `households.expected_monthly_income`: it has no `currency` column
  and is denominated in the Household's `reporting_currency`.
- **N4.** Soft-delete everywhere. Hard delete is not exposed.
- **N5.** No stored aggregates or derived snapshots in MVP. Every figure recomputes on read.
  **[ASSUMED]** — acceptable at single-Household data volumes; revisit if it stops being true.
- **N5a.** **Trajectory is computed server-side.** The client formats and floors for display but
  derives nothing. One source of truth. `contract/testdata/trajectory.json` carries the `CONTEXT.md`
  reference rows as a fixture the backend's unit tests read.

### 8.2 Contract

- **N6.** `contract/openapi.yaml` is the single source of truth; the backend satisfies it and the
  frontend generates its types from it.
- **N7.** A single error envelope, following the Balances ADR-0027 shape.
  It carries **codes, not messages**; the frontend owns every user-facing string.
- **N7a.** The contract is **hand-written and authoritative**; the Go and TypeScript artefacts are
  generated from it and CI checks they are in sync. This is contract-first, unlike Balances, which
  is code-first — that tooling cannot be lifted across.
- **N8.** `backend/internal/migrations/` is canonical: hand-written goose files, embedded in the binary
  and applied on boot; see `BOOTSTRAP.md` §7.
- **N8a.** One Postgres instance, one database, `afloat`.

### 8.3 Offline readiness

The offline write queue is **out of MVP**. The contract that makes it cheap to add is **in MVP**,
because retrofitting it is a contract change plus a migration, and adding it later on
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

**Base palette chosen ahead of the session (2026-11-22): Shallows**, sky blue over warm sand, from the
four-palette study, with **Figtree** as the UI typeface (self-hosted variable `woff2`, SIL OFL 1.1, no
runtime fetch). Both are tokens in `frontend/src/styles/theme.css` only. The three State colours, the
wordmark's typeface, the mark and every other deliverable below stay the session's; the study's
provisional State pills are a red/green good-bad pairing, which the first constraint below rules out.

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
| Q-16 | Multi-currency: exchange-rate source, which date's rate applies, how the trajectory sums mixed currencies. | Post-MVP, planned |

## 12. Deferred — not commitments

Recurring and annual expense planning as first-class objects. Projected end-of-Period spend as an
explicit figure. Historical budget suggestions after N Periods, always phrased as "consider" and
always editable. Seasonality (December dining, Lebaran). Cross-Category and cross-Period pattern
recognition. Bank-statement import for invisible spending such as account and processing fees. More
sophisticated allowance models separating discretionary from periodic obligations beyond the
two-bucket split. Multi-currency UI. An "Underwater" fourth State for an exhausted Budget. **Charge-date versus due-date
dating for credit-card Pockets** (the former S10 `expense_date_basis`, dropped from MVP on 2026-11-22):
the 13 September scratch draft said only that "Afloat does not impose accounting treatment", and no
document says what a due date does. Doing it properly needs a statement-closing day and a due day on the
Pocket, makes every card purchase future-dated, and would derive the date on the client, against
`BOOTSTRAP.md` §4's Period Day rule. Until then E5 and E8 already let a person date a card Expense on
its due date by hand. Adding it back later is a non-breaking contract change plus a column.

## 13. Change log

| Date | Change |
|---|---|
| 2026-11-22 | Pockets, delete and restore (issue #121, PR 3 of 3). `DELETE /pockets/{id}` is a **soft** delete (`204`: the row stays with `deleted_at` set, the Pocket leaves the list and `PATCH`, and its name is free at once) and `POST /pockets/{id}/restore` is its Undo (`200` with the Pocket, `active` as it was). Both are idempotent on the caller's own Pocket and change no column on a repeat; a delete keeps the first `deleted_at`. An unknown or another Household's id is one `404 NOT_FOUND` for both. A restore into a name another Pocket now holds is `409 NAME_TAKEN` and the Pocket stays deleted; the unique index decides it, so concurrent restores cannot both win or `500`. **`409 POCKET_IN_USE` (R1) is not built:** it arrives with the Expenses slice, inside the delete statement, because no Expense can reference a Pocket yet; the contract has no such code until then. The repo's first `DELETE` is covered by the cross-site guard. The #106 path-parameter table is now run against `DELETE` as well as `PATCH`. BOOTSTRAP §4 (*Soft delete everywhere*, *Tenancy on a path id*), §5.2 and `docs/agents/conventions.md` carry the wire. Frontend, UI rulings from the maintainer (recorded in `docs/agents/ui-conventions.md`): an edit dialog's record-level acts sit in **`DetailActions`** (Uruni #768), a two-column grid of outline buttons below a hairline with the destructive one last, and the footer holds only the dialog's decision (Save); a **destructive tone** (`Button` variant, and DetailActions' destructive text) on `--destructive`, provisionally `#b4453c` light and `#ec8b80` dark until the brand session; Delete is immediate with no confirm and raises the 8 s "Deleted OVO." Undo toast, whose Undo calls restore. |
| 2026-11-22 | UI retrofit to `docs/agents/ui-conventions.md`, all rulings from the maintainer, recorded in that file: one Shell (fixed top bar in Uruni's liquid glass, the placeholder mark linking home, one `max-w-xl` column), `BackLink`, `lib/routes.ts`; `--attention` for every error, `--scrim` with a blur, the light `--muted-foreground` darkened to `#566a78` for AA on `--muted`; settings save by control kind; a shared element catalogue that pages compose with layout classes only, and the rule that a missing element is proposed before it is built; every toast hides on a tap; id-ID domain nouns lowercase mid-sentence; meta-tests for each rule. The mark (`public/icon.svg`) stays a placeholder until the brand session (§9). |
| 2026-11-22 | Pockets, rename and deactivate (issue #121, PR 2 of 3): `PATCH /pockets/{id}` renames (the same name rule and `409 NAME_TAKEN` as at creation) and sets `active` (R1's deactivate and reactivate, an ordinary merge update, never a delete; an inactive Pocket is still listed and still holds its name). An id that is unknown, another Household's or deleted is one `404 NOT_FOUND`; validation comes before the lookup (BOOTSTRAP §4). The session is now answered ahead of a malformed path id (P10, BOOTSTRAP §6). The frontend follows `docs/agents/ui-conventions.md`: edit and create in a URL-driven dialog opened by tapping the row, Deactivate/Reactivate in its footer, one toast per write naming the Pocket. UI rulings from the maintainer, added to that file: reactivating carries the same 8 s Undo as deactivating (every reversible act's toast carries Undo); only a dialog that records money ignores an outside tap (Uruni ADR-046; the first draft of the file said "a dialog that writes", which was a misreading); plus the dialog, row, footer, validation and state rules the builder proposed from Uruni and Balances precedent. |
| 2026-11-22 | UI conventions ratified: `docs/agents/ui-conventions.md`, from a study of Uruni (primary) and Balances (secondary). Rulings from the maintainer: editing one record is a URL-driven dialog; tapping a row opens it, record-level acts sit in its footer; Submit disabled only while pending or unchanged, field errors on blur and submit; errors use an `--attention` token, never red, `--destructive` for Delete only; one Shell with fixed top bar (and fixed bottom tabs once tabs exist), one `max-w-xl` column; sentence case, domain nouns capitalised; Radix Select for short lists, native `<select>` for long ones until a combobox; money grouped live as typed, decimal-aware; toasts name the object; id-ID terms tabled in `CONTEXT.md`. Builders follow the file. For a choice the file doesn't cover, the builder applies the nearest Uruni/Balances precedent as a proposal. The maintainer always confirms it before the PR, and the PR adds it to the file. |
| 2026-11-22 | Base palette: **Shallows**, UI typeface: **Figtree**, self-hosted (§9). The light primary is a deeper step of the study's accent (`#1F74AC`, not `#2E8BC9`) so button text meets WCAG AA. State colours still open. |
| 2026-11-22 | Pockets (issue #121, PR 1 of 3; R1's deactivate and delete and R3's Undo toast are built in PRs 2 and 3). Rulings from Rad, R1 to R9: R1, a Pocket can be **deactivated** (an `active` boolean: it stays in the list, keeps labelling its Expenses and its name, is not offered at capture, reversible) or **deleted** (a soft delete, gone from the UI, gives up its name), two different acts; a Pocket any Expense row references, live or in the Recycle Bin, cannot be deleted (`409 POCKET_IN_USE`, built by the Expenses slice), and the UI offers Deactivate instead; R2, the Recycle Bin is for Expenses only, for now; R3, Undo is a timed toast of 8 seconds, paused on hover, as in Balances; R4, no Pocket search; R5, online-only, the offline queue and everything built on it deferred, while the readiness contract stays (client-generated UUIDv7 ids, a replay-safe `POST`, `captured_at` apart from `occurred_on`); R6, for the Expenses slice, an Expense stores `occurred_on` (a `date`, unchanged) and an optional time of day with no timezone, ordered by date, then time, then `captured_at`; R7, inactive Pockets are hidden unless "Show inactive Pockets" is ticked; R8, **`expense_date_basis` is dropped from MVP** (a scope cut, not a resolved question: no Q-17), see §12; R9, the toast library is `sonner`, as in Balances. The name is `pocket_display_name`, extending #113 D5 (S10), at most 60 code points and unique among live Pockets, active or inactive, case-insensitively, `409 NAME_TAKEN`; a create is idempotent on the client-supplied UUIDv7 (`200` for a replay of a live Pocket, `409 ID_TAKEN` for another Household's id or the caller's own deleted one); the table is added to `00001_baseline.sql` in place (`make db-reset`). S10 and S11 reworded. |
| 2026-11-20 | User settings (issue #119). Rulings from Rad, D1 to D8, ratified as recommended: three operations, not one `PATCH` (`PATCH /me` for name, language and zone; `POST /auth/local/change-password` and `POST /auth/local/change-email`, behind the disabled-local gate); a password change revokes every session the User holds, the caller's included, and mints one for the caller in the same transaction, an email change touches none; no email verification, the change is immediate, because there is no mail transport; a wrong current password is `403 INVALID_CURRENT_PASSWORD` with a backoff keyed `current-password:<user id>`, shared by both operations; `time_zone` is validated server-side and the binary embeds `time/tzdata`, Period Day math is not touched and the client derives none; the handlers live in `backend/internal/auth/`; the settings screen is `/settings/user`, three forms, no settings hub. S4 gains the password and each User's own-only rule, S2b the current-password clause. Handed forward: the CLI password reset S2b describes is not built (#118), and what the trajectory slice owes a `time_zone` change. |
| 2026-11-20 | Household settings (issue #113). Rulings from Rad: D1, a `period_start_day` change takes effect at once and re-derives every Period window, past and current, from the current value, with nothing stored and one line of helper text, never a confirmation; D2, `reporting_currency` is writable with no guard until money rows exist, and multi-currency stays out of MVP as a planned item, recorded as **Q-16**; D3, accepted currency codes are ISO 4217, checked server-side; D4, three fields are writable now, and each later S3 field is one additive property; D5, `flat` becomes `fixed` (CONTEXT, §4 and the trajectory already said `fixed`), and the unqualified name field, which meant two things, is retired for `household_display_name` and `user_display_name` on the wire, in the database, in Go and in i18n keys; D6, the update convention (PATCH, merge semantics, last write wins per field, `BOOTSTRAP.md` §4); D7, the handler lives in `backend/internal/household/`. S3 and S4 renamed and S3 given its ranges; N3 records `expected_monthly_income`'s missing currency column; §4 Out marks Multi-currency `[OPEN Q-16]`. |
| 2026-11-19 | Founding: self-registration and the onboarding gate (issue #108, found-only). Rulings from Rad: open registration with founding gated by `FOUNDING_DISABLED`, following Balances' code rather than its self-hosting notes; register creates no account, only a 15-minute handshake; the gate offers "found" only until invitations land; a registered address is `409 EMAIL_TAKEN` (enumeration accepted on register, login unchanged); Balances' password policy exactly; Household and User ids server-generated at founding (an exception to client-generated keys, which stand for domain POSTs); the Household name is pre-filled by the frontend from i18n and capped at 60 code points. S1 and S2b updated. |
| 2026-11-19 | The Kotlin learning track is dropped; Afloat is Go-only. The Kotlin backend, the cross-backend parity requirement and the Flyway migration format are removed: migrations are hand-written goose files in `backend/internal/migrations/`, the database is `afloat`, and the conformance harness tests the one backend against its cases. N5a, N6–N8a updated to match. |
| 2026-09-26 | Trajectory fixture and Go calculation (issue #73, BOOTSTRAP.md §11 step 8 part A). Rulings R1–R4: R1, allowance figures (Baseline Daily Budget, Today's Allowance, Left to Spend Today, Forward Daily Rate) floor at 4dp on the server, toward −∞; R2, Trajectory Variance and Buffer Days go on the wire half-up (half away from zero) at 4dp, and State is decided from exact, unrounded values with no division; R3, the warm-up floor and the two State thresholds are arguments to the calculation, carried as fixture defaults with per-row overrides — Q-05 stays open; R4, the Q-04 display cap and the Q-06 fully-committed presentation stay out of scope, while the mid-period Budget change's clamp *arithmetic* is in scope. |
| 2026-09-13 | Draft 2 — technical-gate session. Resolved Q-01, Q-02, Q-03, Q-07, Q-09, Q-11, Q-12. Added §4.1 Planned and committed; N5a server-side trajectory; N7a contract-first; N8/N8a migration scheme and two databases; N12a i18n from day one. `BOOTSTRAP.md` added as the scaffold sheet. |
| 2026-09-13 | Draft 1. Synthesised from the two scratch drafts and a grill session. Decisions taken: Household-first tenancy mirroring Balances; two-bucket Commitment split in MVP; configurable `period_start_day`; `expected_monthly_income` as the sole inbound integration; "Taking on water" replacing "Sinking"; no mascot; hero figure reframed from *remaining daily allowance* to *left to spend today*; variance settled-through-yesterday; offline queue deferred but its contract locked. |
