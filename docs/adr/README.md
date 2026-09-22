# Afloat — Architecture Decision Records

One file per decision, split by backend track: [`go/`](go/) and [`kotlin/`](kotlin/). A decision that
both backends must agree on is **not** an ADR here — it belongs in [`../BOOTSTRAP.md`](../BOOTSTRAP.md),
which is the cross-cutting scaffold sheet. Only put an ADR in `go/` or `kotlin/` when it's a choice
specific to that backend's implementation (a library, a framework idiom, an internal package shape).

The `go/` track opens at step 5; the `kotlin/` track opens at step 6.

## How an ADR changes

Adopted from the sibling Uruni project's ADR convention (`docs/ADR/README.md` there):

**Numbers are permanent within their track** — never reused, never renumbered. `go/0001-...` and
`kotlin/0001-...` are independent sequences; a number in one track says nothing about the other.

**Text depends on the tag:**

- **`draft`** — the decision is still editable in place. Grilling a draft, changing your mind,
  tightening the wording: all fine, no ceremony. Use the `grill` agent / `grill-with-docs` skill before
  a draft firms up, especially for anything touching money or trajectory.
- **no tag (implemented)** — code has shipped behind this decision. Change it only by adding a
  superseding ADR and marking this one superseded.

**An implemented ADR may be *amended* only to correct a statement of fact that has since become
false** — never to change the decision, trade-off, or accepted cost itself; that's a superseding ADR.
The test: *would this ADR have been written differently if we had known?* No — amendment. Yes —
supersede. An amendment (like an edit to a `draft`) ships in the same PR as the code that makes it
true, and is recorded in an `## Amendments` section at the foot of the ADR with date and cause.

**The tag comes off when the decision is fully implemented** — the last slice of the milestone that
implements it, not the first. Once any code exists behind a `draft` ADR, an edit to it ships in the
same PR as the code that makes it true — prose drifting ahead of the implementation is the failure
mode this guards against.

**A number is claimed when its ADR is written**, first come. No issue or plan reserves one in advance.

Standing cross-cutting decisions that no ADR owns go in `BOOTSTRAP.md`; product decisions in `PRD.md`'s
change log.

## The decisions

### `go/`

| # | Decision | Stage |
|---|---|---|
| [0001](go/0001-chi-router-and-oapi-codegen-strict-handlers.md) | chi as the HTTP router, with oapi-codegen strict handlers | implemented |
| [0002](go/0002-pgx-and-sqlc-for-typed-postgres-access.md) | pgx and sqlc for typed Postgres access | implemented |
| [0003](go/0003-config-logging-and-validation.md) | Config, logging and validation | implemented |
| [0004](go/0004-testing-strategy.md) | Testing strategy | implemented |

### `kotlin/`

| # | Decision | Stage |
|---|---|---|
| [0001](kotlin/0001-spring-data-jpa-conventions.md) | Spring Data JPA conventions | draft |
