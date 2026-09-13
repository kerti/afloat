*Working document — synthesized from the product-design discussion*

# 1. Executive Summary

Afloat is a lightweight personal expense-tracking and spending-trajectory application. Its primary purpose is situational awareness: help a user understand whether current spending is sustainable and answer: “Given what I've spent so far, how much can I safely spend from here until the end of the month?”

Afloat is deliberately not an accounting system, reconciliation tool, or budget-enforcement application. Budgets are expectations and reference points, not hard constraints. The product should inform rather than judge, require very little effort to use, and make correction and recalculation painless.

# 2. Product Philosophy

- Inform, don't enforce.

- Situational awareness over budgetary control.

- Capture expenses quickly; organize and correct them later when necessary.

- The user decides what spending is appropriate. Afloat reports the consequences and trajectory.

- Accounting rigor may exist internally where useful, but accounting concepts should not dominate the user experience.

- Mutable data is intentional: edits and deletions are normal, and derived results should be recalculated cleanly.

- Financial feedback should be informative and emotionally non-punitive.

# 3. Core User Question

Primary question: “Given what I've spent so far, how much can I safely spend from here until the end of the month?”

MVP definition of “safe”: Remaining Daily Allowance = Remaining Budget / Remaining Days.

A future iteration may separate day-to-day discretionary spending from known recurring or periodic obligations such as subscriptions, fuel, property taxes, and other expenses that should be covered over a month or year.

# 4. Product Positioning

## What Afloat is

- A personal cash-flow radar for spending.

- An expense tracker whose primary output is spending trajectory and remaining daily freedom.

- A lightweight tool for answering “Am I going to be okay at this rate?”

## What Afloat is not

- Not a traditional ledger or double-entry accounting application.

- Not a reconciliation system.

- Not a net-worth tracker.

- Not an income tracker in the MVP.

- Not a transfer tracker in the MVP.

- Not a system that detects missing money.

- Not a system that prevents or blocks spending.

- Not a replacement for Balances.

# 5. MVP Scope

## 5.1 Setup

- A monthly overall spending budget.

- Budget/category definitions and planned amounts.

- Pockets representing practical sources/containers from which the user spends.

- For MVP, a new month can either copy the previous month's budget or use a recurring/default budget.

## 5.2 Expense Capture

Current spreadsheet prototype fields:

- Date of transaction

- Pocket

- Description

- Amount

- Budget/category

Exact eventual schema and column naming are intentionally deferred to a later technical-design session.

- Expense entry should be as fast as possible.

- Smart defaults are desirable.

- Users should be able to edit any transaction field after entry.

- Deletion is allowed; soft deletion is a possible implementation choice.

- Historical corrections should be painless.

- Incomplete metadata should not necessarily block recording an expense.

## 5.3 MVP Dashboard

- Prominent hero figure showing Remaining Daily Allowance.

- Clear spending-trajectory status.

- A statement such as “Rp420k ahead of pace” rather than a raw signed difference.

- Remaining budget and days remaining.

- Cumulative graph showing target cumulative spending versus actual cumulative spending.

- Secondary visibility into monthly and per-category safety/variance.

- Pocket and category summaries.

- A gauge chart is unnecessary; direct numbers and status communicate more clearly.

# 6. Core Metrics and Terminology

## Baseline Daily Budget

Monthly Budget / Days in Month. This answers: “If spending were distributed evenly, how much could I spend per day?”

## Remaining Daily Allowance

Remaining Budget / Remaining Days. This is the primary MVP “safe” figure and answers: “Given what I've spent, how much can I spend per remaining day?”

## Spending Trajectory Variance

The difference between target cumulative spending and actual cumulative spending. User-facing language should favor “Rp420k ahead of pace” / “Rp320k behind pace” over a raw signed number. Exact Indonesian/localized terminology remains open.

# 7. Adaptive Daily Allowance

Preferred behavior: adaptive. If the user spends less than the current allowance, remaining budget is spread across fewer remaining days, increasing subsequent daily allowance. This is a recalculation of trajectory, not a literal bank of unused daily credits.

Example: Rp6m over 30 days starts at Rp200k/day. If Rp100k is spent on day one, Rp5.9m remains for 29 days, giving approximately Rp203.4k/day.

The user may configure adaptive versus fixed allowance behavior. Adaptive is the preferred default; configuration should likely be an advanced setting.

# 8. Budget Philosophy

- Budgets are flexible expectations, not hard limits.

- Money can effectively move between categories.

- A category exceeding its allocation is information, not a violation.

- Afloat should show category-level variance while still communicating overall trajectory.

- Repeated category variance can eventually produce recommendations for future budgets.

- Seasonality should eventually be considered; e.g. December may historically require more dining/entertainment budget.

# 9. Pockets

A Pocket is not an accounting account. It represents where the user practically spends from and can function as a real-world behavioral spending barrier.

Examples: OVO for GrabFood, GoPay for transport, or another account for general spending. Afloat records expenses against pockets but does not need to model transfers between them. Pocket limits are therefore an external behavioral mechanism, not an enforced Afloat rule.

# 10. Transactions and Scope Boundaries

- Afloat's core record is an expense, not a complete financial transaction ledger.

- Transfers are outside MVP scope.

- Income is outside MVP scope.

- Credit-card spending can be recorded as an expense against a credit-card pocket.

- The user can choose charge date or due date for credit-card spending; Afloat does not impose accounting treatment.

- Refunds can be handled by editing the relevant expense rather than requiring immutable accounting events.

- All records are intentionally mutable and derived figures should be recalculable from current data.

# 11. Emotional Design: Afloat / Drifting / Sinking

Afloat should communicate trajectory through a playful metaphor inspired by products such as Duolingo: the mascot/ship reacts to the user's situation without shaming them.

- Afloat — comfortably on course.

- Drifting — spending is moving faster than the intended trajectory.

- Sinking — the current trajectory is unsustainable or would exhaust the budget before month-end.

The metaphor should communicate “help us stay afloat,” not “you are bad because you overspent.” The illustration should never replace factual numbers. “Underwater” may eventually describe an exhausted budget, but is not required for MVP.

# 12. Forecasting

Forecasting is part of the long-term value, but MVP can remain simple. The target-vs-actual trajectory graph is central. A future enhancement can add projected end-of-month spending based on current pace.

Potential future message: “At your current pace, you'll exceed your budget by approximately RpX.” Tone should remain informational rather than punitive.

# 13. Historical Learning and Suggestions

Afloat should not attempt intelligent budget recommendations immediately. For MVP, users either copy a previous budget or use a recurring/default budget. After at least X months of historical data (exact threshold TBD), users may explicitly request suggestions based on history.

- Category-level historical spending patterns.

- Consistent underspending/overspending.

- Seasonality, especially month/category effects.

- Suggestions for upcoming monthly allocations.

Suggestions must remain optional and editable; Afloat should say “consider” rather than prescribe.

# 14. User Experience Principles

- Expense capture should take only a few seconds.

- Smart defaults should minimize typing and selection.

- Capture first, organize later is a valid interaction pattern.

- Editing should be seamless and stress-free.

- The app should tolerate incomplete information when possible.

- Changes to budgets should be easy at any time, including mid-month.

- Changes should trigger clean recalculation rather than complicated workflows.

- The first screen should immediately answer the core question without requiring navigation.

# 15. Month and Budget Mutability

Budget changes are informational changes, not violations of a financial contract. If the user's situation changes mid-month, changing the monthly budget should be easy. The system must preserve enough history for recalculation and chart interpretation to remain coherent. Exact semantics for mid-month changes are deferred.

# 16. Relationship with Balances

## Conceptual separation

Balances: “How much money do I have, and how is my overall financial position changing?”

Afloat: “What have I spent, and am I still on track?”

## Integration

Afloat is independently usable. Integration with Balances is optional and should not be a prerequisite.

The important integration concept is attribution, not reconciliation. Balances can infer monthly expenses from net-worth change, tracked income, asset revaluations, and investment returns. Afloat provides explicitly recorded/attributed expenses.

Conceptually: Inferred Expenses − Afloat Attributed Expenses = Unattributed Expenses.

Afloat does not need to know or explain the difference. Determining why inferred expenses were not attributed is a Balances concern.

## API Direction

- Prefer loose coupling.

- Afloat should expose meaningful expense data that Balances can consume.

- Afloat should not need to know that Balances exists.

- Dedicated user-configurable API keys are a likely integration mechanism.

- Keys should be rotatable and scope-limited, e.g. read/write permissions.

- Exact endpoints, authentication, storage, rotation, and security design are deferred.

# 17. Existing Google Sheets Prototype

- Ledger — daily expense records: Date, Pocket, Description, Amount, Budget/category.

- Totals — current-month totals, Pocket totals, Budget/category totals, days in month, days remaining, Baseline Daily Budget, Remaining Daily Allowance, trajectory variance, and daily totals.

- Dashboard — daily spending graph, target cumulative spending line, actual cumulative spending line, and originally a gauge for daily variance; gauge is now considered unnecessary.

- Lookups — pockets, budget/category values, and planned category spending.

# 18. Product Opportunities Uncovered

- The differentiator may be spending trajectory rather than conventional budget tracking.

- The Afloat/Drifting/Sinking metaphor can make financial status intuitive and emotionally lighter.

- Adaptive Remaining Daily Allowance can turn underspending into immediately understandable future flexibility.

- Historical category variance can eventually produce budget suggestions.

- Month-aware and seasonal patterns can make suggestions more realistic than flat averages.

- Afloat can serve as an attribution source for Balances without becoming a reconciliation product.

- Fast capture plus later correction can reduce the friction that makes expense trackers unpleasant.

- Incomplete expense metadata can be tolerated rather than blocking entry.

- A future projected end-of-month figure can make trajectory more actionable.

# 19. Open Questions for the Next Product-Design Sessions

- What exactly happens on day one of a new month?

- How should mid-month budget changes affect historical target lines and trajectory calculations?

- How should zero-spending days affect the interpretation of spending pace?

- Should future-dated expenses be supported, and how should they affect current safety?

- What should the user see and do in the first 30 seconds after opening Afloat?

- Exact thresholds/logic for Afloat vs Drifting vs Sinking.

- Whether “Underwater” is useful as a special state.

- Exact localization/Indonesian terminology for trajectory variance and related concepts.

- How many months of history are required before suggestions become available.

- Whether recurring/periodic obligations belong in a later budgeting model and how they interact with daily allowance.

- Detailed transaction/domain model, database schema, and API design.

# 20. Explicitly Deferred Technical Topics

- Database schema and exact column names.

- Kotlin/backend architecture.

- API endpoint design and authentication implementation.

- API-key storage, rotation, and scope implementation.

- Soft-delete implementation details.

- Recalculation architecture and caching.

- Bank-statement import architecture.

- Potential integrations with bank/payment-provider data.

# 21. Potential Future Features — Not MVP Commitments

- Recurring and annual expense planning.

- Bank-statement import for less obvious expenses such as account fees and processing fees.

- Projected end-of-month spending.

- Historical budget suggestions.

- Seasonality-aware suggestions.

- Pattern recognition across categories and months.

- More sophisticated day-to-day allowance models.

# 22. Product North Star

Afloat should make opening a personal-finance app feel less like checking whether you failed a test and more like checking the instruments on a ship.

A user should be able to glance at Afloat and understand:

- Where spending currently stands.

- How much can comfortably be spent per day from here.

- Whether the spending trajectory is healthy.

- Which categories or pockets are driving the result.

Then the user decides what to do. Afloat's responsibility is to make that decision better informed, not to make it for them.

*End of working product-definition foundation*
