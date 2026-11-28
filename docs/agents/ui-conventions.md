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

- `PageTitle`: the screen's one `h1`. `visuallyHidden` keeps it for screen readers only, on Home
  (§ Layout and navigation, Screen title).
- `SectionTitle`: the `h2` that opens a section.
- `SubsectionLabel`: the `h3` that labels a stretch inside a section (Settings' You section: Profile,
  Email, Password). One step under `SectionTitle`: small, semibold, muted. `Section` renders it for
  its `subsection` prop. Confirmed on the phone preview, 24 Nov 2026. (Uruni `SubsectionLabel.tsx`;
  ruled 24 Nov 2026, #140 PR 1.)
- `SectionDescription`: the sentence under a section title saying what it is for.
- `FieldHint`: the sentence under a field saying what it takes. Also the small muted note below a
  form's submit ("Already have an account? Sign in").
- `FieldError`: a failure, under its field or above a form's footer. `role="alert"`, attention
  colour. Raised in a form only; with no form open it is a `toastError` (§ Feedback).
- `BodyText`: a plain body sentence (the founding notice's "We named your Household …").
- `MutedText`: a muted body sentence: an intro, a status message (`role="status"`), a note.
- `EmptyState`: what a list says when it has nothing, naming what will appear.
- `TextLink` (`components/TextLink.tsx`): the body link, small and underlined, standalone (the Home
  links) or inside a sentence. **A title row with a trailing link**: `PageTitle` and a `TextLink`
  baseline-aligned, the link on the right, for a screen's one secondary destination (the Recycle
  Bin on `/expenses`). An arrangement of two catalogue elements, not a new one. (Confirmed by the
  maintainer, 26 Nov 2026, #140 PR 5.)

**Layout and forms**

- `Shell` (`components/Shell.tsx`): top bar and the one column; § Layout and navigation. The top
  bar wears `header-glass` (`styles/index.css`), Uruni's liquid glass: the page colour at 60% over a
  `blur(20px) saturate(1.6)` backdrop, a bright 1px rim inside the edge facing the content and a soft
  glow falling from it. Its colours are the `--glass-rim`, `--glass-glow` and `--glass-shadow`
  tokens in `theme.css`. The light values are Uruni's; the dark values are faint, since Uruni has no
  dark mode. (Uruni `index.css` `header-glass`; ruled 22 Nov.)
- `AppVersion` (`components/AppVersion.tsx`): the running build, a muted 12px `tabular-nums` line under
  the "Afloat" link in the top bar: a tag on HEAD, else "dev (abc1234)", fixed at build time by
  `vite.config.ts` (`AFLOAT_VERSION` overrides it), since the SPA ships inside the binary it names.
  (Uruni `AppVersion.tsx`, which reads the stamped binary's `/healthz` instead; ruled 27 Nov 2026.)
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
  with an optional title and description, its content at `gap-4`; `subsection` makes its title an h3
  `SubsectionLabel` for a stretch inside another section. Sections stack at the Shell's
  `gap-6`.
- `TextSetting` (`components/TextSetting.tsx`): a typed setting with its own Save and inline error.
- `CheckboxRow` (`components/CheckboxRow.tsx`): a 20px checkbox in a 44px label ("Show inactive …").
  It takes a `className` for layout only: `justify-start` puts it left under a field (Capture's "This
  is a refund", directly under the amount) where it otherwise ends the row at the right.
  (Uruni direction toggle; ruled 25 Nov 2026, #140 PR 3b.) On `/budget` "Show inactive Categories"
  sits under the two Category lists it widens, just above "Add a Category", not between the
  summary and the Commitments heading, where it read as part of the summary (27 Nov 2026).
- **The bottom bar** (`Shell`, `styles/index.css`): `bar-glass` (the page's `--card` at 75% over a
  blur, as the top bar's `header-glass`), the Capture tab on a raised rounded square in `fab-glass`
  (`--fab-shadow-near`/`--fab-shadow-far` in `theme.css`). **Always visible**: it does not hide when
  the on-screen keyboard opens. Hiding it was tried and rejected as flaky, and Uruni's does not hide
  either. (Uruni `Shell.tsx`, `index.css`; ruled 25 Nov 2026, #140 PR 3b.)
- `DateField` (`components/DateField.tsx`): the date control. A full-width trigger reading the date
  long-style ("20 November 2026"), and "Today" while it is untouched or equals the server's `today`,
  that opens a `react-day-picker` calendar in a popover. Today is `me.today`, never the device's. An
  untouched field sends no date and the server dates the Expense. On the edit route the trigger reads
  the stored date in the list's day-heading words ("Today", "Yesterday", else weekday and date, the
  year only when it differs), from the server's `today`. (Uruni `web/src/components/DateField.tsx`;
  ruled 25 Nov 2026, #140 PR 3b; edit, #140 PR 4.)
- `TimeField` (`components/TimeField.tsx`): an optional time of day. Unset it is a quiet "Add time"
  link-style trigger; set it is a small box with a clock and "19:05" and a ✕ that clears it. A popover
  holds a 24-hour grid (4 by 6), then the minutes in 5-minute steps (4 by 3), in both locales. Never
  pre-filled on create; the edit route shows the stored time. No Uruni or Balances precedent. (Ruled
  25 Nov 2026, #140 PR 3b; edit, #140 PR 4.)
- `SearchSelect` (`components/ui/search-select.tsx`): a themed picker with a search box, for a list
  too long to scan: the currency and the time zone. A trigger like the Radix `Select`'s, and a popover
  of `h-11` rows; arrow keys, Enter and Esc work from the search field; it says when nothing matches.
  No Uruni or Balances precedent. (Ruled 25 Nov 2026, #140 PR 3b.)
- `MoneyInput` (`components/MoneyInput.tsx`): the one money field (§ Forms). The currency sits inside
  the field as a prefix that is not part of the value (`Rp`, else the ISO code), `aria-hidden` and
  read as the field's description; no hint under the field repeats it. Digits are `tabular-nums`.
  (Confirmed by the maintainer, 23 Nov 2026, for #130.)
- `MoneySetting` (`components/MoneySetting.tsx`): `TextSetting` for an amount, a `MoneyInput` with its
  own Save. Save is disabled while blank when the amount cannot be cleared. A stored amount loads
  rounded half-up to the currency's decimals, as `lib/money.ts` shows it. (Confirmed by the
  maintainer, 23 Nov 2026, for #130.) A `clearable` amount saves a blank as `null` (the field is
  cleared, not zeroed); a `hint` is a `FieldHint` under it; its `save` may resolve to a sentence that
  replaces the bare "Saved." toast. Several settings may stack in one `Section`, each with its own
  Save. (Confirmed by the maintainer, 24 Nov 2026, for #130 PR 5.)

**Home's figures** (Confirmed by the maintainer on the phone preview, 27 Nov 2026, for #155 PR 5, R18.)

- `HeroDial` (`components/HeroDial.tsx`): Home's hero, one card (`rounded-2xl bg-card hero-glass`)
  holding a **dial** and the figures that support it (ruled by the maintainer at the phone preview,
  28 Nov 2026, replacing `HeroFigure`, `FigurePair` and the Days Remaining and Remaining Daily Pool
  rows). It is a `section` labelled by "Left to spend today", a labelled figure and not a live region.
  - **The ring** is the Budget Period: a tick a day (every fifth longer), a thin arc for the days
    gone by to a `--marker` triangle at Today, and a thick arc for the share of the Daily Pool spent
    in `--primary`, whose part past the whole laps the ring again in `--attention`. Two labels curve
    along the outside of the ring, "<n> days left" at the marker in `--marker` and "<percent> spent"
    at the arc's end; each starts just clear of its own point and runs away from the other, so they
    never overlap, and both are read once more, plainly, to a screen reader.
  - **The disc**'s top two thirds hold the label (medium), the figure (the display role: bold,
    `tabular-nums`, neutral whatever its sign, never coloured or clamped) and "of <Today's
    allowance>" (muted), left out when the allowance is zero or less. Sizes are in `cqw`, shares of
    the dial's width, so the words shrink with it. **The figure's size steps down by the length of
    the formatted string** (`lib/heroSize.ts`), to fit the disc's chord at its line: 9.9cqw to 10
    characters, 8.2 to 13, 7 to 16, 5.6 to 19, 4.6 to 24 (the worst case and its negative), 4
    beyond; at a 304px dial 9.9cqw is 30px.
  - **The sea** holds the disc's bottom third whatever the State; a boat (a small motor boat side on,
    the hull in `--marker`, the cabin and mast in `--foreground`) rides it at one of three fixed
    depths: the whole hull above the water when Afloat, the water nearly over the deck when Drifting,
    only the cabin and mast above it when Taking on water. The waves are a little rougher at each
    step. The sea and the boat settle as Home opens (about three seconds: the water arrives a little
    high and rough, bobs down and calms, the boat riding and rocking with it), then are still, so
    nothing moves behind the figure while it is read; under reduced motion they are drawn settled.
    The drawing is hidden from assistive technology and the State's name is read instead. No sea and
    no boat when the server sends no State.
  - **Under the dial**, the pace sentence (Muted body, centred), then a rule and the supporting
    figures in one row, each a muted label over a semibold `tabular-nums` amount: the Baseline
    (adaptive mode only, and not while fully committed) and the Remaining Daily Pool.
  (Uruni `web/src/screens/Home.tsx` hero card and its `heroSize`, stepped by length; the waterline is
  `VISION.md` §4's identity. Its colours are provisional until the brand session, PRD §9.)
- **The chart card (D5)** (R17 approved the card and `recharts`; its look, the "This Period" title,
  the error boundary and the flat target while fully committed confirmed by the maintainer on the
  phone preview, 27 Nov 2026, for #155 PR 8.) A
  `Section` titled **This Period** (a `SectionTitle` h2) holding `ChartCard`
  (`components/ChartCard.tsx`): a card surface (`rounded-lg border bg-card p-3`, the surface of
  `NoticeCard`) around the cumulative target line against the actual line for the Period, one point
  for every Period Day, from the server's `chart.points` and nothing else. It follows the State and the
  pace and comes before By Category. **Drawing** (`components/ChartCardImpl.tsx`, Recharts, the only
  file that imports it): the target is `--muted-foreground`, dashed; the actual is `--primary`, solid;
  Today's last stretch (the day that is not over, the server's `in_progress`) is dashed again and ends
  in an open dot. **Line style tells the lines apart, never colour**: no red, no green, no fill, no
  gradient. A legend names the two lines. Axis text is 12px (the Badge's size). The Y axis reads in
  the currency's symbol and the locale's compact words ("Rp2.4m", "Rp2,4 jt"; `formatMoneyCompact`),
  so a tick reads at a glance (ruled by the maintainer, 27 Nov 2026, replacing the bare digits); the
  ticks are the chart's own round numbers, never a server figure, which keep the one formatter; the X
  axis shows the Period's first and last day through `lib/dates.ts`, from the date strings. A tap or
  hover opens a tooltip with the day and the server's two figures. No "day X of Y", no device clock, no
  client arithmetic. **Accessibility rule:** the card is **one `role="img"`** named by the pace
  sentence (or, with no pace to read, "Cumulative spending against the target for this Period"); a
  **visually hidden table** (`sr-only`, a sibling of the image and not inside it, since an image's
  children are presentational) lists the same `points`, one row for each day, an empty cell for an
  actual after Today and "today, in progress" on Today's row; **Recharts' keyboard layer is off**
  (`accessibilityLayer={false}`), because a focusable SVG inside a `role="img"` is nested-interactive,
  and the chart is supplementary: the hero, the State and the pace carry the reading. **Lazy:** the
  drawing is a separate chunk behind `React.lazy` and `Suspense` (the existing `Loading` while it
  arrives), so Home's first paint never waits for it, and Recharts is imported nowhere else
  (`components/chartIsLazy.test.ts`); a chunk that fails to load leaves one muted sentence and the table.
  Colours are tokens as SVG attributes; the shadcn `ChartStyle` (a `<style>` element) is not carried
  over, so a CSP without `'unsafe-inline'` for styles stays possible. (Precedent: Balances
  `components/ui/chart.tsx` and `components/charts/*Impl.tsx`, recharts 3, behind a lazy boundary; the
  card surface is Uruni's `cardClasses.ts` card. Uruni has no chart library.)

**Lists**

- `ListCard`, `ListRow` (`components/List.tsx`): the bordered divided list, and its 44px tappable
  row that opens an edit dialog (muted title when inactive, a trailing badge slot). A row may end in
  an amount, `trailing`, as `FigureRow` shows one: `tabular-nums`, medium. `trailingMuted` sets the
  words that stand where an amount would ("No allocation") in the muted colour at regular weight. A
  Category with no allocation reads "No allocation" there and is never shown as zero; with a badge,
  the badge sits beside the title and the amount ends the row. Category rows on `/budget` are
  `ListRow`s. (Confirmed by the maintainer, 23 Nov 2026, for #130 PR 3.)
- `Badge` (`components/Badge.tsx`): the "Inactive" pill. Neutral only.
- `DayGroup`, `ExpenseRow`, `FilterRow`, `LoadMore` (`components/ExpenseList.tsx`): the Expenses list. `DayGroup` is
  a `SubsectionLabel` h3 over a bordered divided list, one per day ("Today", "Yesterday", else the
  weekday and date, the year only when it differs from today's); `ExpenseRow` is a two-line
  `li` holding one full-width link, at least 44px with a hover tint and a focus ring: a title (the
  description, else the Category, else a muted "Uncategorised") over a muted detail line (Pocket,
  Category when not already the title, time; joined " · "), the amount in `tabular-nums` medium, then
  a `CaretRight` chevron, since it opens the edit route. Its accessible name is "Edit {title},
  {amount}" ("Edit Lunch, Rp45,000"). `FilterRow` is the one row above the list that narrows it, a
  bordered link ending in a chevron: "Needs a category (N)" on `/expenses`, shown only while N is
  above zero, with no badge, no colour and no prompt (#140 R8). It sets `?uncategorised=true`; that
  view is titled "Needs a category" (27 Nov 2026), and has a "← All Expenses" `BackLink`, a `MutedText` line ("Expenses with no Category."), the same
  day-grouped list, an `EmptyState` when nothing is left, and no `FilterRow`. (Confirmed by the maintainer, 25 Nov 2026, for #140 PR 4.) `LoadMore` is a full-width
  outline button that reads "Loading…" while it fetches. Never infinite scroll. (Uruni `web/src/
screens/Home.tsx` purpose row, `web/src/components/LoadMore.tsx`; ruled 25 Nov 2026, #140 PR 3b; the link and the chevron, confirmed by the maintainer, 25 Nov 2026, for #140 PR 4.)
- `FigureRow` (`components/FigureRow.tsx`): a read-only `li` for a `ListCard`, label left, amount
  right in `tabular-nums` medium. A negative figure is neutral, never coloured. A figure that cannot
  be computed yet is hidden, not shown as zero. An optional `badge` (the "Inactive" `Badge`) sits
  beside the label, and `muted` sets the words that stand where an amount would in the muted colour at
  regular weight. (Confirmed by the maintainer, 23 Nov 2026, for #130.) More options, `detail`,
  `labelMuted`, `bar` and `small`, belong to Home's D6 and D7 rows below.
- `ShareBar` (`components/ShareBar.tsx`): how much of an allocation has gone, drawn under a
  `FigureRow` through its `bar` option, full row width under both lines. An 8px `bg-muted` track
  that runs to 125%, a thin tick at 100%, the fill in `--primary`, the share past 100% in
  `--attention`, and the fill stops at the track's end. Nothing against an allocation of zero or
  none; a negative share draws empty. `aria-hidden`: the figures beside it say it. (Balances'
  `DashboardScreen` breakdown bar; ruled by the maintainer on the phone preview, 28 Nov 2026.)
- **Home's By Category and By Pocket rows (D7)** (confirmed by the maintainer on the phone preview,
  27 Nov 2026, for #155 PR 7, R18.) Each section is a `Section` (a `SectionTitle` h2, "By Category"
  and "By Pocket") holding a `ListCard` of `FigureRow`s, after the State and the pace and the chart, and the last
  thing on Home for now. Where the Category has an allocation in force, the amount is the **share
  figure** "Rp40.000 / Rp1.500.000 (2%)", set `small` (the Hint size, 14px, still `tabular-nums`
  medium) so the longer line sits beside its name, over a `ShareBar` (ruled 28 Nov 2026, replacing
  the 27 Nov `detail` line "of Rp1.500.000"). The percent is whole and toward zero, so 100% shows
  only once the whole is spent, grouped as the locale groups, and absent against an allocation of
  zero ("Rp0 / Rp0"), where no bar is drawn. A Pocket, a Category without an allocation and the
  uncategorised row show the spent alone at the full size, with no bar, and never "No allocation" or
  zero. `labelMuted` is the muted title of an inactive row, with the "Inactive" `Badge`
  beside it as every list row has. The amount is the row's spent, `tabular-nums` medium, formatted
  half-up like every figure but the allowances; a refund can make it negative and it stays neutral. The
  row for spending with no Category is titled "Uncategorised" in the muted title (as `ExpenseRow` titles
  it), is not a link and carries no badge: Home never prompts about a missing Category ("Needs a
  category" stays on `/expenses`). Rows keep the order the server sent. A row whose spending nets to zero still shows, as "Rp0",
  and an allocation of zero reads "Rp5.000 / Rp0": both are shown as they are (confirmed by the maintainer,
  27 Nov 2026). There is no meter, difference or other colour, and no live region. A section with no row shows the sentence "Nothing recorded
  yet this Period." (the Empty rule) and keeps its heading.
- **Home's Commitments section (D6)** (confirmed by the maintainer on the phone preview, 27 Nov 2026,
  for #155 PR 9, per R16.) A `Section` ("Commitments") between the chart and By Category, holding a
  `ListCard` of `FigureRow`s: a "Total" row first, then one row per Commitment in the order the server
  sent (the Category list's order, not by name). Every row, the Total's too, reads as D7's share
  figure, paid for spent: "Rp1.500.000 / Rp2.000.000 (75%)", `small`, over a `ShareBar` (ruled
  28 Nov 2026, retiring the status words "still due", "Paid" and "more than you set aside" and the
  "paid of" line; `still_due` stays on the wire, unshown). An inactive Commitment gets
  `labelMuted` and the "Inactive" `Badge`; one with an allocation of zero and nothing paid says nothing and
  is left out, as `/budget` leaves out an inactive Category without an allocation (the Total still
  counts it; ruled by the maintainer, 27 Nov 2026). A refund shows as it is ("-Rp50.000 / Rp0"), as
  D7 shows "Rp0" (confirmed by the maintainer, 27 Nov 2026). No meter, words about the figures or
  live region. With no Commitment the
  section keeps its heading over "Your Commitments will appear here." (the Empty rule).

**Notices**

- `NoticeCard` (`components/NoticeCard.tsx`): a one-time notice in the page flow. The card surface
  (`bg-card`, `border`, `rounded-lg`, `px-4 py-3`, `text-sm`, children at `gap-2`) with a 44px ghost
  dismiss "✕" at its top right (`aria-label` "Dismiss") and the right padding clear of it. Its body
  is catalogue text (`BodyText`, `MutedText`, `TextLink`); the names a notice reports (the
  Household, the Pocket, the currency) are a plain `<strong>` inside the sentence, the only bold
  inside running text. It sits under the `PageTitle`. Dismissing it moves focus nowhere: it is not
  sent to the title (the maintainer, 24 Nov 2026). (Uruni `cardClasses.ts` card; ruled 24 Nov 2026,
  #140 R2; confirmed on a phone mock.)

**States** (`components/states.tsx`): `Loading`, `ErrorState`. `UpdatePrompt`
(`components/UpdatePrompt.tsx`): the update banner, sitting where bottom toasts do (`clearOfTabBar`,
`lib/tabBar.ts`) and above the tab bar (`z-20`), so both its buttons can be tapped (#153).

**What floats over the page** (toasts and the update prompt) wears the `--raised` surface: the card's
white in light, where the shadow carries it; `--muted`'s #14354a in dark, where the card is 1.11:1
against the page and the floating surface blended in. (Uruni's update banner sits on its tinted
`--secondary`. Confirmed by the maintainer, 27 Nov 2026, for #153.)

**Toasts**: `lib/plainToast.ts` (`toastPlain`, `toastSaved`, and `toastError` for a failure with
no form open) and `lib/undoToast.ts` (`toastUndoable`, `toastWithAction`), all tap-to-dismiss (`lib/tapToDismiss.ts`)
and all through the one `Toaster` (`components/ui/sonner.tsx`). Toasts follow the person's chosen
appearance, not the OS. (Confirmed by the maintainer, 23 Nov 2026, for #124.)

**Dialog actions** (`components/DetailActions.tsx`)

- `DetailActions`: the record-level acts of an edit dialog (Deactivate or Reactivate, Delete), in a
  two-column grid of outline buttons below a hairline (`border-t`), above the footer. The destructive
  act comes last, in the destructive text colour; an odd one out spans both columns (`fullRow`). No
  icons in the buttons. (Uruni `web/src/components/DetailActions.tsx`, #768; ruled 22 Nov; icons
  elsewhere: § Icons.)
- **The destructive tone**: `Button` `variant="destructive"` (tinted background, destructive text),
  and DetailActions' destructive act, an outline button with destructive text. For a Delete and
  nothing else. (Uruni `web/src/components/ui/button.tsx`, `DetailActions.tsx` `ActionButton`; ruled
  22 Nov.)

**Primitives** (`components/ui/`): `Button` (`default` / `outline` / `ghost` / `destructive`, sizes `default` and
`sm`, both `h-11`), `Input`, `Label` (the field label), the Radix `Select`
(`select.tsx`), `SearchSelect` (`search-select.tsx`), `Dialog` with `DialogContent`, `DialogHeader`, `DialogTitle`, `DialogFooter`
(`dialog.tsx`), `Toaster` (`sonner.tsx`).

## Typography and spacing

One font, Plus Jakarta Sans (`--font-ui`), Uruni's face; it replaced Figtree on 28 Nov 2026, whose figures read too rounded. Sizes and weights come from these roles, never per screen. (The
role table follows Uruni `docs/Design-System.md` § Typography.)

| Role                     | Size                                                      | Weight              | Colour                                | Used for                                                                                                                |
| ------------------------ | --------------------------------------------------------- | ------------------- | ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| Page title               | 20px (`text-xl`)                                          | semibold            | foreground                            | `PageTitle`; every screen, auth included                                                                                |
| Section and dialog title | 18px (`text-lg`)                                          | semibold            | foreground                            | `SectionTitle`, `DialogTitle`: one look (Uruni H2, 600; ruled 22 Nov)                                                   |
| Subsection label         | 14px (`text-sm`)                                          | semibold            | muted-foreground                      | `SubsectionLabel`: Uruni's grey semibold h3, one step under the section title (ruled 24 Nov 2026, #140 PR 1)            |
| Brand                    | 18px (`text-lg`)                                          | semibold            | foreground                            | the top bar's "Afloat" link                                                                                             |
| Hero figure              | 9.9cqw to 4.6cqw of the dial, stepped by length (4cqw past 24 characters) | bold | foreground, never coloured | `HeroDial`: Left to Spend Today (Uruni's Balance / display role, tabular). Ruled 28 Nov 2026 (the dial) |
| Body                     | 16px (base)                                               | regular             | foreground                            | `BodyText`, a list row's title, inputs, select values                                                                   |
| Muted body               | 16px                                                      | regular             | muted-foreground                      | `MutedText`, `EmptyState`, `Loading`, `BackLink`, an inactive row's title                                               |
| Label                    | 14px (`text-sm`)                                          | medium              | foreground                            | `Label`, buttons                                                                                                        |
| Destructive label        | 14px                                                      | medium              | destructive                           | the `destructive` Button, DetailActions' Delete (ruled 22 Nov)                                                          |
| Hint                     | 14px                                                      | regular             | muted-foreground                      | `FieldHint`, `SectionDescription`, `CheckboxRow`                                                                        |
| Error                    | 14px                                                      | regular             | attention                             | `FieldError`                                                                                                            |
| Load failure             | 16px                                                      | regular             | attention                             | `ErrorState`                                                                                                            |
| Link                     | 14px                                                      | regular, underlined | foreground                            | `TextLink`                                                                                                              |
| Badge                    | 12px (`text-xs`)                                          | regular             | muted-foreground on muted             | `Badge`                                                                                                                 |
| Tab label                | 11px / 16px line                                          | medium              | muted-foreground; primary when active | the bottom tab bar's labels (Uruni `Shell.tsx`)                                                                         |

- **Field borders** (`--input`) meet WCAG 3:1 non-text contrast against the surface they sit on:
  light `#658faa` (3.11:1 on the page, 3.46:1 on white), dark `#43708e` (3.05:1 on the page, 3.38:1
  on a card). Every field edge (inputs, `MoneyInput`, Select, SearchSelect, Date and Time triggers)
  uses `border-input`; `--border` stays the paler hairline of cards, lists and bars, which are not
  fields. (#140 PR 3b.)
- **Radius:** `rounded-md` for controls, the list card, the badge and the back link (`--radius` is
  0.625rem); `rounded-lg` for the dialog and the update banner; `rounded-sm` for a select item;
  `rounded-full` for the dialog's close button.
- **Spacing steps:** `gap-2` inside a field; `gap-4` inside a form or section, and a dialog's body;
  `gap-6` between a screen's sections (the Shell's column) and inside an auth card; `gap-1` between
  two lines of one block (the hero's label and figure); `gap-3` in a row. Row padding `px-3 py-2`;
  dialog padding `p-4`; the Shell's side gutters 1rem plus the safe-area inset.

## Layout and navigation

- **One Shell.** A **fixed** top bar and a **fixed** bottom tab bar (at most five tabs, derived from
  the URL). The active tab shows a top line **and** a primary-coloured icon and label, with
  `aria-current` (as Uruni). Fixed,
  not sticky: a sticky header rides iOS's overscroll bounce while a fixed footer stays put. Both bars
  pad for `env(safe-area-inset-*)`; `main` reserves their height. (Uruni `Shell.tsx`.)
  - **The tabs** (ruled 24 Nov 2026, from Uruni ADR-032): **Home** `/`, **Expenses** `/expenses`,
    **Capture** `/capture`, **Budget** `/budget`, **Settings** `/settings`, in that order, each an icon
    over a label. Home is first and matches only `/`; every other tab stays lit across its own
    sub-paths. Capture is raised and centred, the one verb tab, on a **rounded square**, not Uruni's
    circle (the maintainer, 24 Nov 2026). On Capture the line shows either side of the raised
    square and is Capture's active marker (Uruni the same). The bar is in the Shell: § Catalogue, "The bottom bar".
  - **The app opens on Home** (PRD Q-11, reversed 24 Nov 2026). **Capture is a full screen**, never
    a dialog, and returns to Home after a save. It is the only create without its noun's list; every
    other create lives on its own list. No floating button. (Uruni ADR-032, ADR-046.)
  - **Settings is one screen in sections**: Household, Pockets, You (Uruni Pengaturan, ADR-047:
    configuration in Settings, operations where the money is). Budget and its Categories are an
    operation and keep their own tab. This sets aside #119's "no settings hub": one sectioned
    screen, not a list of links. Household, Pockets and You are `h2`s; You's Profile, Email and
    Password are `h3` `SubsectionLabel`s (ruled 24 Nov 2026, #140 PR 1). The Email subsection opens
    with a `MutedText` naming the address in use ("Signed in as a@example.com"), above the change
    form (Uruni `Settings/User.tsx`; ruled 27 Nov 2026). The Pocket dialogs stay
    URL-driven on `/settings` (`?new=pocket`, `?edit=pocket:<id>`); the old `/settings/household`,
    `/settings/user` and `/pockets` redirect there, keeping the query string
    (`legacyRedirects` in `lib/routes.ts`).
  - **The Recycle Bin** is a sub-screen of Expenses (`/expenses/recycle-bin`, "← Expenses", Expenses
    lit). Neither Uruni nor Balances has one; the shape is Afloat's own. **Its entry point** is a
    `TextLink` on the right of the `/expenses` title row, with no count, so Load more never moves
    it; it is absent on the uncategorised view. The bin is a flat list in served order with no day
    headings; a row's detail line opens with its date in day-heading words. **A bin row** is a
    button (no chevron) that opens a **read-only dialog** ("Deleted Expense", `FigureRow`s, URL
    `?expense=<id>`) with one act, Restore, in `DetailActions`; an outside tap closes it. Restore
    ends in one plain toast, no Undo; a refusal follows the refusable-act rule. (Ruled by the
    maintainer, 26 Nov 2026, #140 R11, PR 5.)
  - A screen that becomes a tab loses its "← Home"; a sub-screen keeps its back link.
  - The top bar is 3.5rem plus the top safe-area inset, in `header-glass` (Catalogue, `Shell`).
    It holds the "Afloat" link home on the left with `AppVersion` under it, and nothing on the right.
    (Uruni `Shell.tsx`, without its measured `--shell-header` and banner slot.)
  - **Sign out ends Settings**: a full-width outline button after the You section, not in the top
    bar; a failed sign-out is an error toast. (Ruled by the maintainer, 27 Nov 2026.)
  - **A new screen opens at its top**: a push or replace to another path scrolls to the top; back
    and forward leave the scroll alone, and a dialog opening on the same path is not a new screen.
    **Tapping the tab already showing** scrolls to its top and refetches every query on screen,
    without navigating (Uruni). A tab's sub-path or filter (`/expenses/:id`, `?uncategorised=true`)
    is not the tab itself: that tap navigates as usual. (Ruled by the maintainer, 27 Nov 2026.)
  - The session check's Loading and ErrorState render in the auth card, before the Shell, so Sign
    out is never offered on a screen about to redirect to login.
- **One column, set once in the Shell:** `max-w-xl` (576px), centred. Screens never set their own
  width. Auth screens (login, register, onboarding) are the exception: a `max-w-sm` card. (Uruni.)
- **Screen title:** one `h1`, 20px (`text-xl font-semibold`), sentence case, on every screen: the top
  bar's "Afloat" is a link, not a heading. (Uruni.) **Home's is visually hidden** ("Home",
  "Beranda", `PageTitle visuallyHidden`): the lit tab and the hero already say what the screen is,
  so it stays for screen readers only. (Ruled by the maintainer, 27 Nov 2026.)
- **Sub-screen back:** a quiet link, top-left above the title, naming its destination ("← Home"),
  at least 44px tall: a router `Link` with a Phosphor `ArrowLeft` (`regular`), never a button. (Uruni `BackLink`.)
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
- **An Expense is edited on a route**, `/expenses/:id`: the Capture form in edit mode (one shared form
  body), "← Expenses" above the title, footer [Save] alone, Expenses lit in the tab bar. Save sends
  only the fields that differ; a cleared Category, description or time goes as `null`. It ends in one
  "Saved." toast and **goes back in history** to where the person came from (the list, the
  uncategorised view, Home via the toast's Edit), or, with no in-app history (a direct link), replaces
  to `/expenses`. The fields start from the stored Expense: a stored negative shows as positive with
  the Refund box ticked, the time is pre-filled, and the date row reads in the list's day-heading
  words. An inactive Pocket or Category the Expense already holds stays in its picker as the
  selected option, worded "{name} (inactive)", while other inactive ones stay hidden (#140 R10); it is
  not sent unless changed. **Delete** on the edit route shares Save's row: fields, hairline, then
  `DetailActions` with Save as its `primary`, [Delete | Save], half width each, Save last. Delete alone
  full width directly above Save put it where a thumb reaching for Save lands (ruled by the
  maintainer, 27 Nov 2026, replacing #140 R11's "never on Save's row"; dialogs keep Save in their
  footer). The back link reads "← Needs a category" and leads there when the row was opened from that
  view (router state, `fromUncategorised` in `lib/routes.ts`), else "← Expenses". It is immediate, with no confirm:
  it replaces the route with `/expenses` and raises "Deleted Rp25.000." with an 8 s Undo that
  restores it. A record already gone at Delete follows the gone-record rule below. (Ruled by the
  maintainer, 26 Nov 2026, #140 R11, PR 5.) A record gone at Save, or already gone when the route opens (NOT_FOUND
  on its read), follows the gone-record rule below and lands on `/expenses`; any other failed read
  keeps `ErrorState` with "Try again". (#140 R17; the "(inactive)" wording and gone on opening confirmed by the
  maintainer, 25 Nov 2026.)
- **Tapping a row opens its edit dialog** (an Expense's row opens its edit route instead, above). The row is one full-width button, at least 44px, with a
  hover tint and a focus ring; its badges (Inactive) sit inside it; its accessible name names the
  record ("Edit OVO"), and its figure when the row shows one ("Edit Rent, Rp3,000,000", "Edit Transport,
  No allocation"), then ", inactive" when it is. (Confirmed by the maintainer, 23 Nov 2026, for #130 PR 3.) No per-row pencil, trash or ⋮ menu. A row that navigates to another screen ends
  in a chevron, always `RowChevron` (`components/RowChevron.tsx`: `CaretRight`, 16px, muted); a row
  that opens a dialog does not, so the arrow keeps meaning "this goes somewhere". (Uruni
  `TransactionList`, list cards, and `components/RowChevron.tsx`; the shared element confirmed by the
  maintainer, 28 Nov 2026.)
- **The create action** is one full-width button below the list ("Add a Pocket"), the same button the
  empty state offers. No header "+" and no floating button. (Uruni Locations.)
- **An inactive Category on `/budget`** is listed only while it has a non-zero allocation in the
  current Period (#130 R9), so every figure's inputs are on screen; otherwise it is hidden, as an
  inactive Pocket is. It wears the "Inactive" `Badge`. (Confirmed by the maintainer, 23 Nov 2026,
  for #130.)
- **Record-level acts live in `DetailActions`**, not the footer: Deactivate or Reactivate, then
  Delete, in a two-column grid below a hairline and above the footer. A Deactivate or Delete that
  cannot be refused closes the dialog at once and raises the Undo toast, with no confirm (R3, P6).
  **A Delete or Deactivate the server can refuse waits for the answer with the dialog open** (a
  Category's Delete, `CATEGORY_IN_USE`; a Pocket's Delete and Deactivate, `LAST_POCKET`): on success
  it closes and raises the Undo toast; a refusal shows as a form-level `FieldError` (`role="alert"`)
  above `DetailActions`, and the dialog cannot be dismissed while the act is in flight (outside tap,
  Esc and the X are ignored only while it is pending). Reactivate is never refused and closes at
  once. A Pocket's Delete refuses the same way for `POCKET_IN_USE`.
  (Uruni #768; ruled 22 Nov. The refusable Delete: confirmed by the maintainer, 24 Nov 2026, for
  #130 PR 4; extended to Pockets and a refusable Deactivate, 24 Nov 2026, #140 R1.)
- **Footer:** holds only the dialog's decision. Create: [Cancel | Add]. Edit: [Save] alone, with no
  Cancel; the X, Esc, back and an outside tap close it. So the edit dialog reads: field, hairline,
  [Deactivate or Reactivate | Delete], footer [Save]. The Category edit dialog reads: name, Kind,
  allocation, DetailActions with [Deactivate or Reactivate | Delete], footer [Save]. (Confirmed by the maintainer, 23 Nov 2026, for #130 PR 3; Delete added, 24 Nov 2026, for #130 PR 4.) A screen with more than three buttons needs a
  reason. (Uruni `Locations.tsx`, `DetailActions.tsx`; ruled 22 Nov.)
- **A field that is fixed** once the record exists (a Category's Kind) is shown in the edit dialog as
  the create dialog's own control, disabled, never as plain text or dropped. (Confirmed by the maintainer, 23 Nov 2026, for #130 PR 3.)
- **An inactive Category row** gets a muted title, as an inactive Pocket does. The "Show inactive
  Categories (N)" count covers only the rows still hidden: an inactive Category holding a non-zero
  allocation is always listed (R9) and is not counted, and the checkbox is absent when N is 0. (Confirmed by the maintainer, 23 Nov 2026, for #130 PR 3.)
- **A record that is gone during an edit** (another member deleted it; the save answers NOT_FOUND, or
  for an allocation `VALIDATION {category_id, invalid}`) gets an error toast, a refetch of what it
  touches, and the dialog closes: with the record gone there is no form left to hold the message.
  (Confirmed by the maintainer, 23 Nov 2026, for #130 PR 3.)
- **Outside tap:** only a dialog that **records money** (an Expense, a payment: a fact that happened)
  ignores a tap outside it (`posts` on `DialogContent`); it closes by its X, Esc or its buttons. A
  dialog that sets a plan, such as a Category's allocation, is not a record of money and closes on an
  outside tap, like every other dialog, a Pocket's create and edit included. (Confirmed by the
  maintainer, 23 Nov 2026, for #130.) Uruni's reason: a money record dismissible by a stray tap is
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
  leaves it enabled: submitting shows why. Field errors appear on blur once the field has been
  edited and, all at once, on submit. A blur alone, such as the outside tap that closes a dialog,
  shows nothing. (Hybrid, ruled: Uruni's no-op rule, Balances' validation timing; "edited" confirmed
  by the maintainer, 23 Nov 2026, for #130, after the blur flashed an error as a dialog closed.)
- **Unchanged** compares trimmed text to the stored value, so a trailing space doesn't enable Save.
  The value is sent as typed; the server trims. (Uruni's no-op rule.)
- **Client checks mirror the server's**, with the server's code winning: a required field shows its
  own message ("Enter a Pocket name.") on blur once edited and on submit; typing clears a server
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
- **Pickers:** every dropdown is themed; there is no native `<select>` anywhere, including the
  calendar header. The calendar header is a month-year label between ‹ › month arrows with « »
  year arrows outside them, all ghost `Button`s with 44px targets (ruled 25 Nov 2026, #140 PR 3b). A short list is the Radix
  `Select`, full width, `h-11`: the period start day (1 to 28), language, appearance, Pocket,
  Category. A long list, where scanning is slow, is `SearchSelect` with its search box: the currency
  and the time zone. The date is `DateField`, the time `TimeField`. This supersedes "long lists stay
  native". (Uruni, adapted; ruled 25 Nov 2026, #140 PR 3b.) A picker starts blank unless there is only
  one choice. The Appearance select lists System, Light, Dark, the default first. (Confirmed by the
  maintainer, 23 Nov 2026, for #124.)
- **Capture's Pocket is the one exception to "starts blank"**: it is preselected to the Pocket of the
  Household's last Expense (`default_pocket_id` on `GET /pockets`, #140 R3), else the only active
  Pocket, else blank. Capture is the screen opened most, so a remembered choice saves a tap. Uruni's
  counter-precedent, `web/src/lib/accountDefault.ts`, preselects only when there is exactly one
  account and otherwise leaves it blank; Afloat departs from it on purpose. A chosen Pocket that has
  gone inactive reads as blank, never as another Pocket the person did not pick. (Ruled 25 Nov 2026,
  #140 PR 3b.)
- **Capture's Category starts at "No Category"**, not blank: the Category is optional, and the edit
  route shows "No Category" for an Expense without one, so create and edit read alike. (Ruled by the
  maintainer, 28 Nov 2026.)
- **A popover uses the opaque card surface** (`bg-card`, `border`, `shadow-lg`): the date and time
  calendars, the `SearchSelect` list, the Radix `Select` list and the chart's touch tooltip alike. Never the glass of the bars,
  never transparent over the page. (Ruled 25 Nov 2026, #140 PR 3b.)
- **A Category's bucket field** is labelled "Kind" in en-GB ("Jenis" in id-ID), never "Type", and
  its options read Daily, then Commitment. (Confirmed by the maintainer, 23 Nov 2026, for #130.)
- **A name's length counts code points**, as the server does, not UTF-16 units: a name field has no
  `maxLength` attribute, its input is clamped with `lib/codePoints.ts` (`clampName`, 60), so an emoji
  name is not cut at 30. (Confirmed by the maintainer, 23 Nov 2026, for #130 PR 3.)
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
  - **An optional setting** says so in its label ("Your name (optional)", "Expected monthly income
    (optional)"); its hint does not repeat it. (Confirmed by the maintainer, 24 Nov 2026, for #130
    PR 5.)
- **A save that makes something newly true says so in its own toast**, plainly, and proceeds:
  "Saved. Your Monthly Budget is now more than your expected monthly income." Only on the save that
  crosses; a save while it is already true gets "Saved.". No persistent line, nothing stored (PRD S6,
  #130 R17; confirmed by the maintainer, 24 Nov 2026).

## Feedback

- **Every write the user starts ends in exactly one toast.** Create: "Added OVO.". Edit: "Saved."
  **Every reversible act's toast names the object and carries Undo, both directions alike**:
  "Deactivated OVO." and "Reactivated OVO." each get the 8 s Undo, even though the reverse is one tap
  away in the dialog. Consistency wins over "it's cheap to redo". Failure with no form open: an error
  toast with the code's copy. (Balances ADR-0058 §4; Uruni "the one way to say saved"; ruled 22 Nov.)
- **Capture's toast** is one 8 s toast naming the object by its amount: "Added Rp25.000." A refund
  reads with its sign ("Added -Rp450.000."). It carries an **Edit** action that opens the Expense
  just saved on its edit route, and **no Undo**: the Undo of a create would be a delete into the
  Recycle Bin, so a slip is mended from the Expense itself. It is the action-toast kind (8 s, pauses
  on hover, countdown bar), raised by `toastWithAction` in `lib/undoToast.ts`, and its Edit works
  after Capture has unmounted. Capture then returns to Home. (#140 R16; ruled 25 Nov 2026, #140
  PR 3b; Edit added, confirmed by the maintainer, 25 Nov 2026, for #140 PR 4.)
- **Bottom toasts clear the tab bar**: the `Toaster` offsets them above the bar, its raised Capture
  square and `env(safe-area-inset-bottom)`, so a toast never covers a tab. (#140 PR 3b.)
- **A one-time notice is a `NoticeCard` in the page flow**, carried by router state and never
  stored: it is gone once dismissed or once the person leaves the page, and a refresh does not bring
  it back. (Ruled 24 Nov 2026, #140 R2.)
- **An error toast** is the card surface with attention-coloured text and border, never a solid
  red fill.
- **A tap on a toast hides it**, as Uruni's do (`web/src/components/toast/`: "the whole toast is the
  dismiss button"). sonner has no tap-to-dismiss, and a tap there only focuses the toast, which
  pauses it. So every toast carries an id (`lib/tapToDismiss.ts`), and the Toaster dismisses the
  one tapped. A tap on a button inside it (Undo) is the button's. **Toasts are raised only through
  the helpers** `toastPlain`, `toastSaved` and `toastError` (`lib/plainToast.ts`) and
  `toastUndoable` and `toastWithAction` (`lib/undoToast.ts`), never by importing sonner; a test enforces it
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
  `components/states.tsx` holds `Loading` and `ErrorState`. The spinner is Phosphor `CircleNotch`
  (`regular`, an indicator) and the alert glyph `WarningCircle` (default duotone), both `aria-hidden`.
- **Empty:** a plain sentence naming what will appear here, plus the create action. A separate string
  when a filter hides everything ("No active Pockets. 3 inactive hidden…"). (Uruni, Balances.)
- **Load failure:** an alert in the attention colour with an icon, `role="alert"`, and a full-width
  Retry button in the primary (default) variant. (Uruni `ErrorState`.)
  On Home, a failed **refresh** keeps the figures already shown, with no alert: `ErrorState` is
  only for a Home with nothing to show yet, and the next focus or write refetches. (Confirmed by
  the maintainer, 27 Nov 2026, for #155 PR 5. Other screens still swap to `ErrorState`.)

## Icons

- **Phosphor** (`@phosphor-icons/react`), **duotone app-wide**, set once by an `IconContext.Provider`
  in `main.tsx` (weight `duotone`, size 24), as Uruni does. A page or component never sets a weight
  of its own except in the two cases below. Icons are `aria-hidden` unless they are the only label.
- **Icons only, no authored SVG.** Every icon is a Phosphor icon; no inline `<svg>` is written for
  an icon, and none is left in `frontend/src` (the 25 Nov 2026 rule, #140 PR 3b, which moved the last
  hand-drawn ones). The rule is about icons: **a data graphic is not an icon**, and the chart's SVG is
  drawn by Recharts, with no `<svg` authored for it (§ Home's figures, the chart card). (Recorded as
  icons-only, 27 Nov 2026, #155 PR 8, R17.)
- **A glyph on a filled background** is `bold`, so it holds against the fill: the Capture plus on its
  primary square.
- **A glyph that is itself a control** is `regular`, never duotone: the close ✕ (dialog, notice, the
  time clear) and the select chevrons (`X`, `CaretDown`).
  (Uruni `web/src/main.tsx`; ruled 25 Nov 2026, #140 PR 3b.)

## Colour and type

- **Errors and warnings use `--attention`, never red.** `--destructive` is reserved for a Delete
  (the `destructive` Button tone and DetailActions' last act); provisional, like `--attention`:
  light `#b4453c`, dark `#ec8b80`, until the brand session. Nothing is coloured good/bad red/green (PRD §9). The attention colour itself is the brand
  session's; until then `--attention` (`styles/theme.css`) holds a provisional terracotta, AA for text
  on the background, card and muted surfaces in both light and dark appearance. (Uruni.)
- **A negative Unallocated** (allocations above the Daily Pool) is information, not a fault: the
  figure stays neutral and a `MutedText` line under the figures says so ("Your Category allocations
  add up to more than your Daily Pool. That's fine; it's just information."). No attention colour.
  (#130 C15; confirmed by the maintainer, 23 Nov 2026.)
- **Raw Tailwind palette classes** (`text-red-500`, `bg-green-…`, `bg-white`, `bg-[#…]`) are banned in
  components; use the theme tokens. A test enforces it (`components/noRawColours.test.ts`). (Balances.)
- **Light and dark** both real (PRD §9), via the tokens in `theme.css`; the appearance picker is #124.
  The light page is `#f0f3f5` and its borders and input edges `#d0dde5`, the Shallows blue tint kept
  so a card still reads against the page. (Ruled 25 Nov 2026, #140 PR 3b.)
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
- **Amounts are compared on their wire strings, exactly** (`compareMoney` in `lib/money.ts`, BigInt
  over the digits), never as floats and never after display rounding: a Monthly Budget a fraction of
  a rupiah above the income counts as above it. (Confirmed by the maintainer, 24 Nov 2026, for #130
  PR 5.)
- Digit separators follow the **app locale**, not the currency (en-GB `1,234.50`, id-ID `1.234,50`).
  IDR is `Rp` with no space and no decimals (`Rp9.000.000`); any other currency is its ISO code, a
  no-break space and its own decimals (2, or 3 for BHD/KWD and the like, 0 for JPY and the like).
  Rounding is half-up, except the allowance figures below; a negative reads `-Rp500`. (Confirmed by
  the maintainer, 23 Nov 2026, for #130.)
- **The allowance figures round toward minus infinity** (confirmed by the maintainer on the phone
  preview, 27 Nov 2026, for #155 PR 5, R11): the hero, Today's allowance and the Baseline go through
  `formatAllowance` (`lib/money.ts`), which cuts at the currency's own minor unit (whole rupiah for IDR) on the digits,
  never a float: `203448.2758` reads `Rp203.448`, `-0.5` reads `-Rp1`, `0.9999` reads `Rp0`, and a
  zero never prints a sign (`-Rp0`). A negative grows away from zero, so no allowance ever reads as
  more than the Household can spend. **Every other figure stays half-up** (`formatMoney`), the
  Remaining Daily Pool included. There is no larger display unit: the Rp1.000 floor is dropped. The
  server floors at 4dp already (#73 R1) and applies no display unit; the client derives nothing
  (PRD N5a).
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
- **A data graphic is one `role="img"` with a name and a visually hidden table of the same data**
  (the table beside the image, not inside it); a library's keyboard layer is off beneath it. (The
  chart card, #155 PR 8, confirmed with it.)
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
