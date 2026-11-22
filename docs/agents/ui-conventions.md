# UI conventions

The frontend's house rules. Ratified by the maintainer on 22 November 2026
from a study of **Uruni** (primary) and **Balances** (secondary); each rule names its source so the
reasoning can be read there. `PRD.md` §9 (brand) and `CLAUDE.md` outrank this file; where they
disagree, they win and this file is out of date.

**How to use it.**

- **A choice this file covers** is already decided. Follow it.
- **A choice it doesn't cover** is a proposal until the maintainer confirms it:
  1. The builder takes the nearest [Uruni](https://github.com/kerti/uruni) precedent (`web/src`,
     `docs/Design-System.md`, `docs/ADR/`), else the nearest
     [Balances](https://github.com/kerti/balances-v2) one (`frontend/src`, `docs/`), and builds it.
  2. The builder lists it under **"UI precedents applied"** in the report, with the source.
  3. The orchestrator puts every such precedent to the maintainer before the PR opens. A builder
     never treats its own precedent as settled.
  4. Each confirmed decision is added to this file **in the same PR**, as a new rule with its source.
     A rejected one is reworked to the maintainer's answer and documented the same way.
- **The reviewer** fails a frontend diff that rests on an unconfirmed precedent, or that lacks the rule
  this file should have gained.

Uruni's design-system doc lags its code in places (its money-input line still says "on blur"). When
they disagree, the code is the precedent.

Already ruled for Afloat and not reopened here: the Undo toast (8 s, sonner, #121 R3/R9), inactive
items hidden behind "Show inactive … (N)" (R7), deactivate is immediate with Undo, no confirm (R3).

## Layout and navigation

- **One Shell.** A **fixed** top bar and, once the app has tabs, a **fixed** bottom tab bar (at most
  five tabs, the active one marked with a top line and `aria-current`, derived from the URL). Fixed,
  not sticky: a sticky header rides iOS's overscroll bounce while a fixed footer stays put. Both bars
  pad for `env(safe-area-inset-*)`; `main` reserves their height. (Uruni `Shell.tsx`.) The tab list is
  decided when the dashboard and capture slices land; until then, the Shell has no bottom bar.
- **One column, set once in the Shell:** `max-w-xl` (576px), centred. Screens never set their own
  width. Auth screens (login, register, onboarding) are the exception: a `max-w-sm` card. (Uruni.)
- **Screen title:** one `h1`, 20px (`text-xl font-semibold`), sentence case.
- **Sub-screen back:** a quiet link, top-left above the title, naming its destination ("← Pockets"),
  at least 44px tall. (Uruni `BackLink`.)
- **Routes come from one builder module**, `lib/routes.ts`; no path literals in components.
  (Balances.)
- **Tabs, filters and an open dialog live in the URL** (search params), so a link and Android's back
  button both work. (Uruni ADR-032/046.)

## Editing surfaces

- **Editing one record is a dialog**, never an inline form. The dialog is URL-driven
  (`?edit=pocket:<id>`) so back closes it. Create uses the same form body (`mode: "create" | "edit"`).
  (Uruni ADR-032/046, Balances conventions.)
- **A form that builds a big record or types an amount** (Expense capture) is a full route, not a
  dialog. No nested dialogs, no mode-switching dialogs, no unbounded list inside a dialog. (Uruni.)
- **Tapping a row opens its edit dialog.** The whole card is the target; no per-row pencil, trash or
  ⋮ menu. A row that navigates to another screen ends in a chevron; a row that opens a dialog does
  not. (Uruni.)
- **Record-level acts live in the dialog's footer**: Deactivate/Reactivate, later Delete. Deactivate
  still closes the dialog at once and raises the Undo toast.
- **Footer:** two buttons side by side, half a row each, primary on the right. A screen with more
  than three buttons needs a reason. (Uruni.)
- **A dialog that writes** closes only by its X, Esc or its buttons, never by a backdrop tap. A dialog
  that writes nothing may close on an outside tap. (Uruni ADR-046.)
- Edit-form state is initialised from the record (`useState(() => toForm(record))` plus
  `key={record.id}`), never reset in a `useEffect`. (Balances.)

## Forms

- Every form is `noValidate`; Afloat renders its own errors.
- **Submit is disabled only while the write is in flight, or when nothing has changed.** Invalid input
  leaves it enabled: submitting shows why. Field errors appear on blur and, all at once, on submit.
  (Hybrid, ruled: Uruni's no-op rule, Balances' validation timing.)
- **Pending:** the label changes ("Saving…", "Adding…") and the inputs are disabled.
- **Errors go where the user is looking.** A server code that belongs to a field shows under that
  field (one code→field map). Any other failure while a form is open shows inside the form, above the
  footer. With no form open (a failed Undo, deactivate from a toast) it is a toast. The wire's text is
  never shown; the copy comes from the code. Never a generic "Something went wrong" when a code came
  back. (Balances ADR-0058 §5, Uruni.)
- **Pickers:** Radix Select for short lists, full width, `h-11`. Long lists (currency, time zone) stay
  native `<select>` until a searchable combobox is built. A picker starts blank unless there is only
  one choice. The capture date field is decided in that slice. (Uruni, adapted.)
- **Money input** groups digits live, in the app locale, as the user types, and keeps the caret after
  the same digit when separators come and go (Uruni `AmountInput`, #749). A typed `.` or `,` is the
  decimal point; paste goes through a strict locale parser; the value on the wire is a canonical
  decimal string, never a float (Balances `NumericInput`, ADR-0056). `inputmode="decimal"`, or
  `numeric` for a zero-decimal currency.
- **Settings save by control kind.** A choice (select, switch, theme) saves on change, raises a
  "Saved" toast and rolls back on failure. A typed value keeps an explicit Save, enabled when changed;
  Enter saves, Esc reverts; a failure is inline on that field. (Balances ADR-0058 §7.)

## Feedback

- **Every write the user starts ends in exactly one toast.** Create: "Added …". Edit: "Saved." A
  reversible act names the object and carries Undo ("Deactivated OVO."). Failure with no form open:
  an error toast with the code's copy. (Balances ADR-0058 §4; Uruni "the one way to say saved".)
- **Durations:** 4 s plain, 8 s with an action; an action toast pauses on hover and shows the
  countdown bar. One `<Toaster>`, bottom-centre (a phone's thumb zone), mounted at the root so a
  toast survives navigation.
- Raise toasts from `mutateAsync().then(...)` or the hook's own `onSuccess`, **never** from
  `mutate`'s per-call `onSuccess`: TanStack drops per-call callbacks once the component unmounts.
- **Offline / update banners** share one slot. Offline says so honestly and is never shown as "no
  results". An update banner offers Reload and never reloads by itself. (Uruni, Balances.)

## States

- **Loading:** a small inline spinner plus text, `role="status"`. No skeletons. (Uruni.)
- **Empty:** a plain sentence naming what will appear here, plus the create action. A separate string
  when a filter hides everything ("No active Pockets. 3 inactive hidden…"). (Uruni, Balances.)
- **Load failure:** an alert in the attention colour with an icon, `role="alert"`, and a full-width
  Retry button. (Uruni `ErrorState`.)

## Colour and type

- **Errors and warnings use `--attention`, never red.** `--destructive` is reserved for a Delete
  button. Nothing is coloured good/bad red/green (PRD §9). The attention colour itself is the brand
  session's; until then the token exists with a provisional value. (Uruni.)
- **Raw Tailwind palette classes** (`text-red-500`, `bg-green-…`) are banned in components; use the
  theme tokens. (Balances.)
- **Light and dark** both real (PRD §9), via the tokens in `theme.css`; the picker is #124.
- **Weight marks the figure, not the name:** a row's title is regular weight; its amount is
  `tabular-nums`, medium. Money is always `tabular-nums`. (Uruni.)
- **Motion** 150–250ms ease-out, and none under `prefers-reduced-motion`. (Uruni.)
- Body text at least 16px.

## Copy

- **Sentence case everywhere**: titles, buttons, labels, nav. Domain nouns from `CONTEXT.md` keep
  their capital when they mean the modelled thing (Pocket, Household, Expense). (Uruni.)
- Plain, warm, specific; written for a household, not an engineer. A failure is described as what
  happened and what to do, never as an "error". (Uruni, Balances.)
- **Name the object** in confirms and toasts ("Deactivated OVO."). Per-row accessible labels name it
  too ("Edit OVO").
- Punctuation: "…" (one character) for pending; sentences end with a full stop, buttons and titles
  don't; **no em dash** in copy (a test enforces it). (Uruni, Balances.)
- **Plurals** use i18next `_one` / `_other` keys; `id-ID` needs only `_other`.
- Every string exists in `en-GB` and `id-ID` with the same keys; error copy is keyed by the backend
  code. The brand name "Afloat" is never translated.
- **Indonesian terms** come from the id-ID column of `CONTEXT.md`'s glossary. A term not there yet:
  add it in the same PR and list it in the report; never decide a translation only inside the JSON.

## Money and dates

- Money is formatted at the display edge only, by one formatter (`lib/money.ts`), hand-built rather
  than `Intl` currency style (browsers disagree). A bad input renders as given, never `NaN`. (Uruni,
  Balances.)
- Dates are formatted by one module (`lib/dates.ts`), long style, the month never abbreviated
  ("3 September 2026").
- **Never `new Date("YYYY-MM-DD")`** (it parses as UTC and shifts the day), and **never the device
  clock for a business date**: Today and the Period Day come from the server (`CLAUDE.md` rule 5). A
  test enforces it. (Uruni.)

## Accessibility and touch

- **44px minimum tap target**, built into the primitives (`Button`, `Input`, icon sizes), not
  remembered per screen. (Uruni, Balances.)
- Colour is never the only signal; decorative icons are `aria-hidden`.
- Radix owns focus trap and restore; a dialog's close button has a label; the dialog sits above the
  on-screen keyboard.
- Live regions are mounted before they are needed.
- Focus order follows visual order.

## Testing

- Vitest + Testing Library + user-event + MSW, colocated `*.test.tsx`. Query by role and accessible
  name, using the locale strings, not literals.
- Toasts: `test/sonnerMock.ts`, asserting one toast per write, its duration and its action; fake
  timers where real sonner runs.
- **Meta-tests** guard the rules above: no em dash in copy; every `ErrorCode` in
  `contract/openapi.yaml` has copy in both locales; no device clock and no `new Date("YYYY-MM-DD")`
  in business-date code.
- Later, with Playwright: one journey per screen and an axe scan (WCAG 2.1 A/AA, failing on serious
  and critical).
