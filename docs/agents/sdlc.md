# SDLC

Afloat is pre-scaffold: `BOOTSTRAP.md` §11 "Scaffold order" is the current spine, and it supersedes
anything below until the repo skeleton, both backends, and the frontend exist. Follow it in order;
don't skip ahead to a domain feature before its prerequisite step is green.

Once the scaffold is in place, the shape per change is:

**idea → grill (if it touches trajectory/money/the contract) → draft ADR or PRD edit → issue → build →
review → merge**

- **idea** — a feature or fix. Check it against `PRD.md` §4 (in/out) and §10 (non-goals) before
  spending time on it. If it's already out of scope, say so and stop.
- **grill** — for anything reshaping the trajectory calculation, money handling, auth, or
  `contract/openapi.yaml`: stress-test the plan with the `grill` agent / `grill-with-docs` skill before
  writing code. Skip this step for mechanical changes with an already-known shape.
- **draft ADR or PRD edit** — cross-backend calls go in `BOOTSTRAP.md`; backend-specific ones in
  `docs/adr/{go,kotlin}/` (see that directory's `README.md` for the `draft` convention); scope changes
  go in `PRD.md`, dated in its change log.
- **issue** — once there's an issue tracker (not yet decided — see `docs/agents/issue-tracker.md` once
  it exists), break the plan into vertical slices with `to-issues`.
- **build** — `builder` for mechanical work, `builder-deep` for the trust core (trajectory, money,
  auth, the contract itself). State which backend(s) in the brief — see `CLAUDE.md` "Two backends, one
  contract".
- **review** — `reviewer` checks the diff against the non-negotiables and backend parity before it
  goes to the maintainer.

How far up this chain a change needs to start scales with how much it commits the project to a shape:
a copy fix doesn't need a grill session; a change to how the State model reads pace does.
