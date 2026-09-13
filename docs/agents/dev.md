# Local dev / lint / tests

Placeholder — there is no `Makefile` yet. `BOOTSTRAP.md` §11 "Scaffold order" step 1 calls for the
`Makefile` shell before anything else; fill this file in as real targets land, mirroring the shape of
`ci.yml` once that exists (a `make check` that mirrors CI step for step is what `pre-push-gate.sh`
gates on — see `.claude/README.md`).

Expect this file to eventually cover, per `BOOTSTRAP.md`:

- Running both backends and the frontend locally (`docker-compose.yml` profiles: `go` | `kotlin`, one
  Postgres, two databases).
- `make gen-goose-migrations`, and regenerating the OpenAPI-derived server/client artefacts.
- The restart-after-backend-edit gotcha, if one turns out to exist for either backend (Go usually
  needs a rebuild; Kotlin/Spring Boot may not, with devtools).
- Lint and test commands per surface (Go, Kotlin/Kotest, frontend/Vitest, Playwright), and which of
  them `make check` actually runs versus which are opt-in.

Don't invent targets ahead of the Makefile existing — if a task needs a dev command that isn't listed
here, check `Makefile`'s current state (`make help` once it exists) rather than assuming one from
Balances-v2 or Uruni carries over unchanged.
