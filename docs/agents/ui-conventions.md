# UI conventions

The frontend's house rules. Ratified by the maintainer on 22 November 2026
from a study of **Uruni** (primary) and **Balances** (secondary); each rule names its source so the
reasoning can be read there. `PRD.md` §9 (brand) and `CLAUDE.md` outrank this file; where they
disagree, they win and this file is out of date.

**How to use it.**

- **A choice this file covers** is already decided. Follow it.
- **Never invent an element.** Every visual element (a title, a section label, a card, a badge, a
  row, a link style, a spacing) comes from the catalogue below, as its shared component or class
  module. Only those carry looks; a page composes them and adds layout only, and never restyles one
  with its own type, colour, radius or padding classes. Changing an element is then one edit, and it
  changes everywhere. An element the catalogue lacks is **proposed to the maintainer first**, with
  its nearest Uruni (else Balances) precedent, before anyone builds it. That holds even when the
  precedent is obvious. This is stricter than the rule for other uncovered choices below, which may
  be built first and confirmed before the PR. (Uruni `cardClasses.ts`, `SectionLabel.tsx`,
  `Design-System.md` § Typography; ruled 22 Nov.)
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

## Catalogue

Every element Afloat renders is one entry here: one shared component, or one class module where an
element can be several tags. **Pages carry layout classes only** (flex, grid, gap, margin, width,
order, min-height): never their own type size, weight, colour, radius, padding or border for a
catalogued element. A test fails a page that does (`components/pagesCarryLayoutOnly.test.ts`).
Components take a `className` for layout. Uruni's mechanism: one component per repeated element
(`web/src/components/SectionLabel.tsx`, `SubsectionLabel.tsx`, `Badge.tsx`), class constants where
the tag varies (`web/src/components/cardClasses.ts`, `segmented.ts`). Afloat's elements are always one
tag each, so it has components only. A missing element is proposed first (see How to use it).

**Text** (`components/Text.tsx`; roles in the table below)

- `PageTitle`: the screen's one `h1`.
- `SectionTitle`: the `h2` that opens a section.
- `SectionDescription`: the sentence under a section title saying what it is for.
- `FieldHint`: the sentence under a field saying what it takes. Also the small muted note below a
  form's submit ("Already have an account? Sign in").
- `FieldError`: a failure, under its field or above a form's footer. `role="alert"`, attention
  colour. Raised in a form only; with no form open it is a `toastError` (§ Feedback).
- `BodyText`: a plain body sentence ("Signed in as …").
- `MutedText`: a muted body sentence: an intro, a status message (`role="status"`), a note.
- `EmptyState`: what a list says when it has nothing, naming what will appear.
- `TextLink` (`components/TextLink.tsx`): the body link, small and underlined, standalone (the Home
  links) or inside a sentence.

**Layout and forms**

- `Shell` (`components/Shell.tsx`): top bar and the one column; § Layout and navigation. The top
  bar wears `header-glass` (`styles/index.css`), Uruni's liquid glass: the page colour at 60% over a
  `blur(20px) saturate(1.6)` backdrop, a bright 1px rim inside the edge facing the content and a soft
  glow falling from it. Its colours are the `--glass-rim`, `--glass-glow` and `--glass-shadow`
  tokens in `theme.css`. The light values are Uruni's; the dark values are faint, since Uruni has no
  dark mode. (Uruni `index.css` `header-glass`; ruled 22 Nov.)
- `LogoMark` (`components/LogoMark.tsx`): the mark, `size-7`, left of the "Afloat" link in the top
  bar. It is a second way home for a thumb, but hidden from keyboards and screen readers, since the
  name link beside it already is that. (Uruni `Shell.tsx`, #356.)
- **The mark itself** is `public/icon.svg`, a **placeholder** until the brand session (PRD §9): a
  neutral disc above a waterline. One file serves the favicon, the PWA manifest and `LogoMark`, so a
  new mark changes all three.
- `AuthCard` (`components/AuthCard.tsx`): the `max-w-sm` frame outside the Shell.
- `BackLink` (`components/BackLink.tsx`): the sub-screen's way back.
- `Form`, `Field`, `Section` (`components/Form.tsx`): a `noValidate` form with its fields stacked
  (`gap-4`); one field with its label, control, hint and error stacked (`gap-2`); a screen section
  with an optional title and description, its content at `gap-4`. Sections stack at the Shell's
  `gap-6`.
- `TextSetting` (`components/TextSetting.tsx`): a typed setting with its own Save and inline error.
- `CheckboxRow` (`components/CheckboxRow.tsx`): a 20px checkbox in a 44px label ("Show inactive …").
- `MoneyInput` (`components/MoneyInput.tsx`): the one money field (§ Forms). The currency sits inside
  the field as a prefix that is not part of the value (`Rp`, else the ISO code), `aria-hidden` and
  read as the field's description; no hint under the field repeats it. Digits are `tabular-nums`.
  (Confirmed by the maintainer, 23 Nov 2026, for #130.)
- `MoneySetting` (`components/MoneySetting.tsx`): `TextSetting` for an amount, a `MoneyInput` with its
  own Save. Save is disabled while blank when the amount cannot be cleared. A stored amount loads
  rounded half-up to the currency's decimals, as `lib/money.ts` shows it. (Confirmed by the
  maintainer, 23 Nov 2026, for #130.)

**Lists**

- `ListCard`, `ListRow` (`components/List.tsx`): the bordered divided list, and its 44px tappable
  row that opens an edit dialog (muted title when inactive, a trailing badge slot).
- `Badge` (`components/Badge.tsx`): the "Inactive" pill. Neutral only.
- `FigureRow` (`components/FigureRow.tsx`): a read-only `li` for a `ListCard`, label left, amount
  right in `tabular-nums` medium. A negative figure is neutral, never coloured. A figure that cannot
  be computed yet is hidden, not shown as zero. (Confirmed by the maintainer, 23 Nov 2026, for #130.)

**States** (`components/states.tsx`): `Loading`, `ErrorState`. `UpdatePrompt`
(`components/UpdatePrompt.tsx`): the update banner.

**Toasts**: `lib/plainToast.ts` (`toastPlain`, `toastSaved`, and `toastError` for a failure with
no form open) and `lib/undoToast.ts` (`toastUndoable`), all tap-to-dismiss (`lib/tapToDismiss.ts`)
and all through the one `Toaster` (`components/ui/sonner.tsx`). Toasts follow the person's chosen
appearance, not the OS. (Confirmed by the maintainer, 23 Nov 2026, for #124.)

**Dialog actions** (`components/DetailActions.tsx`)

- `DetailActions`: the record-level acts of an edit dialog (Deactivate or Reactivate, Delete), in a
  two-column grid of outline buttons below a hairline (`border-t`), above the footer. The destructive
  act comes last, in the destructive text colour; an odd one out spans both columns (`fullRow`). No
  icons (Afloat has no icon library). (Uruni `web/src/components/DetailActions.tsx`, #768; ruled 22 Nov.)
- **The destructive tone**: `Button` `variant="destructive"` (tinted background, destructive text),
  and DetailActions' destructive act, an outline button with destructive text. For a Delete and
  nothing else. (Uruni `web/src/components/ui/button.tsx`, `DetailActions.tsx` `ActionButton`; ruled
  22 Nov.)

**Primitives** (`components/ui/`): `Button` (`default` / `outline` / `ghost` / `destructive`, sizes `default` and
`sm`, both `h-11`), `Input`, `Label` (the field label), `NativeSelect` and the Radix `Select`
(`select.tsx`), `Dialog` with `DialogContent`, `DialogHeader`, `DialogTitle`, `DialogFooter`
(`dialog.tsx`), `Toaster` (`sonner.tsx`).

## Typography and spacing

One font, Figtree (`--font-ui`). Sizes and weights come from these roles, never per screen. (The
role table follows Uruni `docs/Design-System.md` § Typography.)

| Role                     | Size             | Weight              | Colour                    | Used for                                                                  |
| ------------------------ | ---------------- | ------------------- | ------------------------- | ------------------------------------------------------------------------- |
| Page title               | 20px (`text-xl`) | semibold            | foreground                | `PageTitle`; every screen, auth included                                  |
| Section and dialog title | 18px (`text-lg`) | semibold            | foreground                | `SectionTitle`, `DialogTitle`: one look (Uruni H2, 600; ruled 22 Nov)     |
| Brand                    | 18px (`text-lg`) | semibold            | foreground                | the top bar's "Afloat" link                                               |
| Body                     | 16px (base)      | regular             | foreground                | `BodyText`, a list row's title, inputs, select values                     |
| Muted body               | 16px             | regular             | muted-foreground          | `MutedText`, `EmptyState`, `Loading`, `BackLink`, an inactive row's title |
| Label                    | 14px (`text-sm`) | medium              | foreground                | `Label`, buttons                                                          |
| Destructive label        | 14px             | medium              | destructive               | the `destructive` Button, DetailActions' Delete (ruled 22 Nov)            |
| Hint                     | 14px             | regular             | muted-foreground          | `FieldHint`, `SectionDescription`, `CheckboxRow`                          |
| Error                    | 14px             | regular             | attention                 | `FieldError`                                                              |
| Load failure             | 16px             | regular             | attention                 | `ErrorState`                                                              |
| Link                     | 14px             | regular, underlined | foreground                | `TextLink`                                                                |
| Badge                    | 12px (`text-xs`) | regular             | muted-foreground on muted | `Badge`                                                                   |

- **Radius:** `rounded-md` for controls, the list card, the badge and the back link (`--radius` is
  0.625rem); `rounded-lg` for the dialog and the update banner; `rounded-sm` for a select item;
  `rounded-full` for the dialog's close button.
- **Spacing steps:** `gap-2` inside a field; `gap-4` inside a form or section, and a dialog's body;
  `gap-6` between a screen's sections (the Shell's column) and inside an auth card; `gap-1` between
  two lines of one block (Home's signed-in lines); `gap-3` in a row. Row padding `px-3 py-2`;
  dialog padding `p-4`; the Shell's side gutters 1rem plus the safe-area inset.

## Layout and navigation

- **One Shell.** A **fixed** top bar and, once the app has tabs, a **fixed** bottom tab bar (at most
  five tabs, the active one marked with a top line and `aria-current`, derived from the URL). Fixed,
  not sticky: a sticky header rides iOS's overscroll bounce while a fixed footer stays put. Both bars
  pad for `env(safe-area-inset-*)`; `main` reserves their height. (Uruni `Shell.tsx`.) The tab list is
  decided when the dashboard and capture slices land; until then, the Shell has no bottom bar.
  - The top bar is 3.5rem plus the top safe-area inset, in `header-glass` (Catalogue, `Shell`).
    It holds the "Afloat" link home on the left and Sign out on the right; a failed sign-out is an
    error toast. (Uruni `Shell.tsx`, without its measured `--shell-header` and banner slot.)
  - The session check's Loading and ErrorState render in the auth card, before the Shell, so Sign
    out is never offered on a screen about to redirect to login.
- **One column, set once in the Shell:** `max-w-xl` (576px), centred. Screens never set their own
  width. Auth screens (login, register, onboarding) are the exception: a `max-w-sm` card. (Uruni.)
- **Screen title:** one `h1`, 20px (`text-xl font-semibold`), sentence case, on every screen, Home
  included ("Home", "Beranda"): the top bar's "Afloat" is a link, not a heading. (Uruni.)
- **Sub-screen back:** a quiet link, top-left above the title, naming its destination ("← Home"),
  at least 44px tall: a router `Link` with an inline-SVG arrow, never a button. (Uruni `BackLink`.)
- **Routes come from one builder module**, `lib/routes.ts`; no path literals in components (an ESLint
  rule fails a `to="/…"` or `navigate("/…")` literal). (Balances.)
- **Tabs, filters and an open dialog live in the URL** (search params), so a link and Android's back
  button both work. (Uruni ADR-032/046.)

## Editing surfaces

- **Editing one record is a dialog**, never an inline form. The dialog is URL-driven
  (`?edit=pocket:<id>`) so back closes it. Create uses the same form body (`mode: "create" | "edit"`)
  under its own parameter (`?new=pocket`). (Uruni ADR-032/046, Balances conventions.)
- **A dialog in the URL** (`lib/useDialogParam.ts`): opening pushes a history entry tagged as the
  dialog's. Closing goes back when that entry is the dialog's, and otherwise strips the parameter in
  place, so closing never stacks history. A link to a record that doesn't exist clears the parameter.
  (Uruni.)
- **A form that builds a big record or types an amount** (Expense capture) is a full route, not a
  dialog. No nested dialogs, no mode-switching dialogs, no unbounded list inside a dialog. (Uruni.)
- **Tapping a row opens its edit dialog.** The row is one full-width button, at least 44px, with a
  hover tint and a focus ring; its badges (Inactive) sit inside it; its accessible name names the
  record ("Edit OVO"). No per-row pencil, trash or ⋮ menu. A row that navigates to another screen ends
  in a chevron; a row that opens a dialog does not. (Uruni `TransactionList`, list cards.)
- **The create action** is one full-width button below the list ("Add a Pocket"), the same button the
  empty state offers. No header "+" and no floating button. (Uruni Locations.)
- **Record-level acts live in `DetailActions`**, not the footer: Deactivate or Reactivate, then
  Delete, in a two-column grid below a hairline and above the footer. Deactivate and Delete both close
  the dialog at once and raise the Undo toast; Delete has no confirm (R3, P6). (Uruni #768; ruled 22 Nov.)
- **Footer:** holds only the dialog's decision. Create: [Cancel | Add]. Edit: [Save] alone, with no
  Cancel; the X, Esc, back and an outside tap close it. So the edit dialog reads: field, hairline,
  [Deactivate or Reactivate | Delete], footer [Save]. A screen with more than three buttons needs a
  reason. (Uruni `Locations.tsx`, `DetailActions.tsx`; ruled 22 Nov.)
- **Outside tap:** only a dialog that **records money** ignores a tap outside it (`posts` on
  `DialogContent`); it closes by its X, Esc or its buttons. Every other dialog, a Pocket's create and
  edit included, closes on an outside tap. Uruni's reason: a money record dismissible by a stray tap is
  the wrong shape for the one thing the app must not get wrong. (Uruni ADR-046, `dialog.tsx` `posts`.)
- **Dialog chrome:** the card is inset 1rem from the screen edge, with a plain header, a muted footer
  strip and a 44px close button whose label the caller translates. On open, focus lands in the first
  field with the caret at the end and nothing selected. The dialog centres in the space the on-screen
  keyboard leaves (`lib/useKeyboardInset.ts`). Entrance: opacity and scale, 150ms ease-out; no exit
  animation (the dialog unmounts with its parameter); none under reduced motion. (Uruni `dialog.tsx`.)
- Edit-form state is initialised from the record (`useState(() => toForm(record))` plus
  `key={record.id}`), never reset in a `useEffect`. (Balances.)

## Forms

- Every form is the `Form` component, which is `noValidate`; Afloat renders its own errors.
- **Submit is disabled only while the write is in flight, or when nothing has changed.** Invalid input
  leaves it enabled: submitting shows why. Field errors appear on blur and, all at once, on submit.
  (Hybrid, ruled: Uruni's no-op rule, Balances' validation timing.)
- **Unchanged** compares trimmed text to the stored value, so a trailing space doesn't enable Save.
  The value is sent as typed; the server trims. (Uruni's no-op rule.)
- **Client checks mirror the server's**, with the server's code winning: a required field shows its
  own message ("Enter a Pocket name.") on blur once touched and on submit; typing clears a server
  error. (Balances validation timing.)
- **Pending:** the label changes ("Saving…", "Adding…") and the inputs are disabled.
- **A create keeps one client id per attempt**, in page state: closing and reopening the dialog after
  a failure resends the same UUIDv7, so a retry meets the `200` replay. Replaced only after a success
  or an `ID_TAKEN`. (#121 R5.)
- **Errors go where the user is looking.** A server code that belongs to a field shows under that
  field (one code→field map). Any other failure while a form is open shows inside the form, above the
  footer. With no form open (a failed Undo, deactivate from a toast) it is a toast. The wire's text is
  never shown; the copy comes from the code. Never a generic "Something went wrong" when a code came
  back. (Balances ADR-0058 §5, Uruni.)
- **Pickers:** Radix Select for short lists, full width, `h-11`. Long lists stay native `<select>`,
  also `h-11`, until a searchable combobox is built: currency, time zone, and the period start day
  (1 to 28). Language is a Radix Select. A picker starts blank unless there is only one choice. The
  capture date field is decided in that slice. (Uruni, adapted.) The Appearance select lists System,
  Light, Dark, the default first. (Confirmed by the maintainer, 23 Nov 2026, for #124.)
- **Money input** groups digits live, in the app locale, as the user types, and keeps the caret after
  the same digit when separators come and go (Uruni `AmountInput`, #749). A typed `.` or `,` is the
  decimal point; paste goes through a strict locale parser; the value on the wire is a canonical
  decimal string, never a float (Balances `NumericInput`, ADR-0056). `inputmode="decimal"`, or
  `numeric` for a zero-decimal currency.
- **Settings save by control kind.** (Balances ADR-0058 §7.)
  - **A choice** (select, switch, appearance) saves on change. The new value shows at once
    (optimistic). Success is a "Saved." toast. A failure rolls the value back and raises an error
    toast, since no form is open. A language change applies before its toast, so the toast reads
    in the language just chosen.
  - **A typed value** is its own small form: the input with its Save beside it, enabled when
    changed. Enter saves and Esc reverts. Inputs are disabled while pending. A failure is inline on
    that field, and typing clears it. The settings name fields have no client-side blank check: the
    server's `VALIDATION` already shows inline.
  - **Email and password changes** are typed forms with an explicit submit. Each success is a toast
    in its own words ("Saved. Sign in with {{email}} from now on."), not a bare "Saved.".

## Feedback

- **Every write the user starts ends in exactly one toast.** Create: "Added OVO.". Edit: "Saved."
  **Every reversible act's toast names the object and carries Undo, both directions alike**:
  "Deactivated OVO." and "Reactivated OVO." each get the 8 s Undo, even though the reverse is one tap
  away in the dialog. Consistency wins over "it's cheap to redo". Failure with no form open: an error
  toast with the code's copy. (Balances ADR-0058 §4; Uruni "the one way to say saved"; ruled 22 Nov.)
- **An error toast** is the card surface with attention-coloured text and border, never a solid
  red fill.
- **A tap on a toast hides it**, as Uruni's do (`web/src/components/toast/`: "the whole toast is the
  dismiss button"). sonner has no tap-to-dismiss, and a tap there only focuses the toast, which
  pauses it. So every toast carries an id (`lib/tapToDismiss.ts`), and the Toaster dismisses the
  one tapped. A tap on a button inside it (Undo) is the button's. **Toasts are raised only through
  the helpers** `toastPlain`, `toastSaved` and `toastError` (`lib/plainToast.ts`) and
  `toastUndoable` (`lib/undoToast.ts`), never by importing sonner; a test enforces it
  (`lib/toastsThroughHelpers.test.ts`). Afloat keeps sonner (#121 R9) where Uruni hand-builds its
  provider. (Ruled 22 Nov.)
- **Durations:** 4 s plain, 8 s with an action; an action toast pauses on hover and shows the
  countdown bar. One `<Toaster>`, bottom-centre (a phone's thumb zone), mounted at the root so a
  toast survives navigation.
- Raise toasts from `mutateAsync().then(...)` or the hook's own `onSuccess`, **never** from
  `mutate`'s per-call `onSuccess`: TanStack drops per-call callbacks once the component unmounts.
- **Offline / update banners** share one slot. Offline says so honestly and is never shown as "no
  results". An update banner offers Reload and never reloads by itself. (Uruni, Balances.)

## States

- **Loading:** a small inline spinner plus text, `role="status"`. No skeletons. (Uruni `Loading.tsx`.)
  `components/states.tsx` holds `Loading` and `ErrorState`. Icons are inline SVG, `aria-hidden`;
  there is no icon library.
- **Empty:** a plain sentence naming what will appear here, plus the create action. A separate string
  when a filter hides everything ("No active Pockets. 3 inactive hidden…"). (Uruni, Balances.)
- **Load failure:** an alert in the attention colour with an icon, `role="alert"`, and a full-width
  Retry button in the primary (default) variant. (Uruni `ErrorState`.)

## Colour and type

- **Errors and warnings use `--attention`, never red.** `--destructive` is reserved for a Delete
  (the `destructive` Button tone and DetailActions' last act); provisional, like `--attention`:
  light `#b4453c`, dark `#ec8b80`, until the brand session. Nothing is coloured good/bad red/green (PRD §9). The attention colour itself is the brand
  session's; until then `--attention` (`styles/theme.css`) holds a provisional terracotta, AA for text
  on the background, card and muted surfaces in both light and dark appearance. (Uruni.)
- **Raw Tailwind palette classes** (`text-red-500`, `bg-green-…`, `bg-white`, `bg-[#…]`) are banned in
  components; use the theme tokens. A test enforces it (`components/noRawColours.test.ts`). (Balances.)
- **Light and dark** both real (PRD §9), via the tokens in `theme.css`; the appearance picker is #124.
- **The dialog overlay** is `--scrim` (10% black) plus `backdrop-blur-xs` where supported, the same
  in light and dark. (Uruni `dialog.tsx`, Balances `dialog.tsx`: identical.)
- **Weight marks the figure, not the name:** a row's title is regular weight; its amount is
  `tabular-nums`, medium. Money is always `tabular-nums`. (Uruni.)
- **Motion** 150–250ms ease-out, and none under `prefers-reduced-motion`. (Uruni.)
- Body text is 16px. Labels, hints, errors and links are 14px, and the badge 12px (§ Typography and
  spacing); no running text is smaller than 14px.

## Copy

- **Sentence case everywhere**: titles, buttons, labels, nav. In **en-GB**, domain nouns from
  `CONTEXT.md` keep their capital when they mean the modelled thing (Pocket, Household, Expense). In
  **id-ID**, domain nouns are lowercase mid-sentence ("kantong", "periode anggaran", "rumah
  tangga") and capitalised only where the sentence or label starts. (Uruni copy: "amplop",
  "kategori"; ruled 22 Nov.)
- Plain, warm, specific; written for a household, not an engineer. A failure is described as what
  happened and what to do, never as an "error". (Uruni, Balances.)
- **Name the object** in confirms and in create and reversible-act toasts ("Added OVO.",
  "Deactivated OVO."). An edit's toast is just "Saved." (§ Feedback). Per-row accessible labels name
  it too, with any badge the row shows ("Edit OVO", "Edit OVO, inactive").
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
- Digit separators follow the **app locale**, not the currency (en-GB `1,234.50`, id-ID `1.234,50`).
  IDR is `Rp` with no space and no decimals (`Rp9.000.000`); any other currency is its ISO code, a
  no-break space and its own decimals (2, or 3 for BHD/KWD and the like, 0 for JPY and the like).
  Rounding is half-up; a negative reads `-Rp500`. (Confirmed by the maintainer, 23 Nov 2026, for #130.)
- A date range says a shared year once: "1 November to 30 November 2026", id-ID "sampai"; across a
  year end both years show. (Confirmed by the maintainer, 23 Nov 2026, for #130.)
- Dates are formatted by one module (`lib/dates.ts`), long style, the month never abbreviated
  ("3 September 2026").
- **Never `new Date("YYYY-MM-DD")`** (it parses as UTC and shifts the day), and **never the device
  clock for a business date**: Today and the Period Day come from the server (`CLAUDE.md` rule 5). A
  test enforces it. (Uruni.)

## Accessibility and touch

- **44px minimum tap target**, built into the primitives (`Button`, `Input`, icon sizes), not
  remembered per screen: `Button` (default and `sm`) and `Input` are `h-11` app-wide. A control drawn
  smaller than 44px (`TextLink`) gets its target from the `touch-target` utility (`styles/index.css`),
  an invisible 44px box centred on it, so its look and the layout don't change. (Uruni `index.css`
  `touch-target`, Balances; ruled 22 Nov.)
- **Text contrast is AA (4.5:1) on every surface it sits on**, the muted badge included: the light
  `--muted-foreground` is `#566a78` (4.89:1 on `--muted`), a step darker than the Shallows study's
  `#5e7280`. (Ruled 22 Nov.)
- Colour is never the only signal; decorative icons are `aria-hidden`.
- Radix owns focus trap and restore; a dialog's close button has a label; the dialog sits above the
  on-screen keyboard.
- Live regions are mounted before they are needed.
- A checkbox is 20px inside a label at least 44px tall; "Show inactive …" is a checkbox, never a
  switch. (Balances `ShowInactiveToggle`.)
- Focus order follows visual order.

## Testing

- Vitest + Testing Library + user-event + MSW, colocated `*.test.tsx`. Query by role and accessible
  name, using the locale strings, not literals.
- Toasts: `test/sonnerMock.ts`, asserting one toast per write, its duration and its action; fake
  timers where real sonner runs.
- **Meta-tests** guard the rules above: no em dash and no `...` in copy, and no catalogue key that
  nothing reads (`locales/locales.test.ts`); every `ErrorCode` in `contract/openapi.yaml` has copy in
  both locales (same file); no device clock and no `new Date("YYYY-MM-DD")` in src, with `lib/uuid.ts`
  allowed (`lib/noDeviceClock.test.ts`); no raw palette colour (`components/noRawColours.test.ts`); no type, colour, radius, padding or
  border utility in a page (`components/pagesCarryLayoutOnly.test.ts`).
- Later, with Playwright: one journey per screen and an axe scan (WCAG 2.1 A/AA, failing on serious
  and critical).
