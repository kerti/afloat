.PHONY: help setup hooks-install claude-install doctor start-task check test-migrations \
        db-up db-down db-reset gen-oapi gen-sqlc gen-ts gen check-go conformance lint-install \
        frontend-install frontend-dev frontend-build check-frontend

# `make` with no target prints help.
.DEFAULT_GOAL := help

# One version for the lint gate, so a local pass and a CI pass mean the same
# thing. .github/workflows/ci.yml installs exactly this via `make lint-install`;
# bump it here and CI follows.
GOLANGCI_VERSION := v2.14.0

# Where check-go writes its coverage profile, relative to backend/. Produced on
# every run rather than behind a flag: CI uploads the profile from the same job
# that runs `make check`, so a separate coverage invocation would be a second
# run of the suite and the mirror would stop being one. Gitignored.
GO_COVERAGE := coverage.out

help:
	@echo "afloat - make targets (run 'make <target>')"
	@echo ""
	@echo "First run:"
	@echo "  setup                   fresh-clone entry point: git hooks + Claude Code hooks"
	@echo "  hooks-install           enable the pre-commit pii-guard (git hooks)"
	@echo "  claude-install          arm the Claude Code hooks + seed personal settings"
	@echo "  doctor                  report what's installed and what's missing"
	@echo ""
	@echo "Database:"
	@echo "  db-up                   start Postgres"
	@echo "  db-down                 stop Postgres, keeping its data"
	@echo "  db-reset                destroy and recreate the database from scratch"
	@echo ""
	@echo "Workflow:"
	@echo "  start-task              before new work: clean tree + GitHub access, then main fast-forwarded"
	@echo "  gen                   run every generator (contract, sqlc, TypeScript types)"
	@echo "  gen-oapi                regenerate Go server interfaces from contract/openapi.yaml"
	@echo "  gen-sqlc                regenerate typed queries from the migrations + backend/queries"
	@echo "  gen-ts                  regenerate frontend/src/api/schema.gen.ts from contract/openapi.yaml"
	@echo "  test-migrations         goose up, in-place edit and down against a throwaway Postgres"
	@echo "  check                   pre-push gate: pass/fail per surface"
	@echo "  check-go                lint and test the Go backend"
	@echo "  check-frontend          typecheck, lint, format-check, test and build the frontend"
	@echo "  conformance             boot the backend and run the conformance suite against it"
	@echo "  lint-install            install the pinned golangci-lint (the version CI uses)"
	@echo ""
	@echo "Frontend:"
	@echo "  frontend-install        npm ci in frontend/"
	@echo "  frontend-dev            Vite dev server on 5181, /api proxied to the backend on 5182"
	@echo "  frontend-build          build frontend/ and copy it into the Go binary's embed dir"

# ---- first run -------------------------------------------------------------
# Idempotent - safe to re-run, and worth re-running after a pull that touches
# .claude/ or .githooks/ (both need a chmod that git alone won't reapply on some
# clones). Grows backend/frontend install steps as those surfaces land.
setup: hooks-install claude-install frontend-install
	@echo "ok setup complete - run 'make doctor' to see what's still missing"

# Point git at the repo's own hooks directory and seed the local, gitignored
# .pii-patterns denylist from the template + your git identity, so the
# pre-commit pii-guard protects commits out of the box.
hooks-install:
	@git config core.hooksPath .githooks
	@chmod +x .githooks/pre-commit
	@if [ ! -f .pii-patterns ]; then \
	  cp .pii-patterns.example .pii-patterns; \
	  { git config user.name; git config user.email; } \
	    | sed 's/[][\\.^$$*+?(){}|]/\\&/g' >> .pii-patterns; \
	  echo "hooks-install: seeded .pii-patterns (gitignored) from template + git identity"; \
	fi
	@echo "ok git hooks installed (core.hooksPath=.githooks); pre-commit pii-guard active"

# The behaviour itself is committed in .claude/settings.json (portable - it
# addresses scripts via $$CLAUDE_PROJECT_DIR), so all that's left per clone is
# the executable bit and your personal settings file.
claude-install:
	@chmod +x .claude/hooks/*.sh
	@if [ ! -f .claude/settings.local.json ]; then \
	  cp .claude/settings.local.json.example .claude/settings.local.json; \
	  echo "claude-install: seeded .claude/settings.local.json (gitignored)"; \
	fi
	@if ! command -v jq >/dev/null 2>&1; then \
	  echo "! jq not found - the Claude Code hooks parse tool JSON with it and will" >&2; \
	  echo "  silently no-op until it's installed (brew install jq)." >&2; \
	fi
	@echo "ok Claude Code hooks armed (session-start, pre-push gate, agent gate, format-on-write)"

# Pre-task pre-flight: refuse on a dirty tree, verify GitHub access, then switch
# to main and fast-forward it, so new work never branches off a stale main. The
# SessionStart hook runs this only when you are already on a clean main.
start-task:
	@test -z "$$(git status --porcelain)" || { echo "FAIL working tree dirty - commit or stash first, then re-run"; exit 1; }
	@git ls-remote origin HEAD >/dev/null 2>&1 || { echo "FAIL no GitHub access - unlock the SSH key (ssh-add) or run 'gh auth login'"; exit 1; }
	@git checkout main >/dev/null 2>&1 || { echo "FAIL could not switch to main"; exit 1; }
	@git pull --ff-only >/dev/null 2>&1 || { echo "FAIL pull failed (diverged or no upstream) - resolve manually"; exit 1; }
	@echo "ok on main, up to date @ $$(git rev-parse --short HEAD)"

# Report on the parts of the environment the Makefile can't install for you.
# Versions are the ones BOOTSTRAP.md §3 pins. Never fails - a readout, not a gate.
doctor:
	@printf '%-16s' 'go';           if command -v go >/dev/null 2>&1; then \
	  v=$$(go env GOVERSION); \
	  case "$$v" in go1.27|go1.27.*) echo "$$v";; *) echo "$$v - BOOTSTRAP.md pins Go 1.27.x";; esac; \
	else echo 'MISSING (Go 1.27.x)'; fi
	@printf '%-16s' 'node';         if command -v node >/dev/null 2>&1; then \
	  v=$$(node --version); want=$$(cat .nvmrc); \
	  case "$$v" in v$$want.*) echo "$$v";; *) echo "$$v - .nvmrc pins $$want (nvm use)";; esac; \
	else echo "MISSING (Node $$(cat .nvmrc))"; fi
	@printf '%-16s' 'docker';       command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 && echo 'ok' || echo 'MISSING (docker compose)'
	@printf '%-16s' 'golangci-lint'; command -v golangci-lint >/dev/null 2>&1 && echo 'ok' || echo 'MISSING (brew install golangci-lint)'
	@printf '%-16s' 'jq';           command -v jq     >/dev/null 2>&1 && echo 'ok'                   || echo 'MISSING (Claude Code hooks need it)'
	@printf '%-16s' 'claude';       command -v claude >/dev/null 2>&1 && echo 'ok'                   || echo 'not on PATH (install separately; the Makefile cannot)'
	@printf '%-16s' 'git hooks';    [ "$$(git config core.hooksPath)" = ".githooks" ] && echo 'armed' || echo 'NOT armed - run make hooks-install'
	@printf '%-16s' 'pii-patterns'; [ -f .pii-patterns ] && echo 'present'                           || echo 'MISSING - run make hooks-install'
	@printf '%-16s' 'claude hooks'; [ -x .claude/hooks/session-start.sh ] && echo 'executable'       || echo 'NOT executable - run make claude-install'

# ---- codegen ---------------------------------------------------------------
# The contract leads (BOOTSTRAP.md §6): these read contract/openapi.yaml and
# the migrations, never the other way round. Output is committed and never
# hand-edited; check regenerates and diffs.
gen-oapi:
	@cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../contract/openapi.yaml
	@echo 'ok backend/internal/api/api.gen.go'

gen-sqlc:
	@cd backend && go tool sqlc generate
	@echo 'ok backend/internal/db'

# The frontend's types come from the same contract (BOOTSTRAP.md §6).
gen-ts:
	@cd frontend && npm run -s gen:api
	@echo 'ok frontend/src/api/schema.gen.ts'

gen: gen-oapi gen-sqlc gen-ts

# Schema behaviour against a real Postgres, applied through goose: defaults,
# CHECKs, the soft-delete aware indexes, cascade behaviour, the §7.1 in-place
# edit hazard, and that Down drops what Up created. Needs docker; not part of
# check for that reason.
test-migrations:
	@./scripts/test-migrations.sh

# ---- database --------------------------------------------------------------
# One Postgres instance, one database (BOOTSTRAP.md §3).
db-up:
	@docker compose up -d
	@printf 'waiting for postgres'
	@for i in $$(seq 1 60); do \
	  if [ "$$(docker inspect -f '{{.State.Health.Status}}' afloat-postgres-1 2>/dev/null)" = healthy ]; then \
	    echo ' ok'; exit 0; fi; \
	  printf '.'; sleep 1; \
	done; echo ' TIMED OUT'; docker compose logs --tail=20 postgres; exit 1

db-down:
	@docker compose down
	@echo 'ok postgres stopped (data kept - use db-reset to destroy it)'

# The other half of the in-place migration policy (BOOTSTRAP.md §7.1). Editing
# an applied migration in place leaves goose silently skipping it, so the edit
# is only safe paired with throwing the database away. This is that command,
# and the reason the policy is safe to follow.
db-reset:
	@docker compose down -v
	@$(MAKE) --no-print-directory db-up
	@echo 'ok database recreated empty - re-run your migrations'

# ---- workflow --------------------------------------------------------------
# Pre-push gate: .claude/hooks/pre-push-gate.sh runs this before every
# `git push`. .github/workflows/ci.yml mirrors it step for step, so green
# locally ~ green in CI - a surface wired into one is wired into both.
#
# A surface that does not exist yet skips; one that exists runs its real
# steps. A surface added without steps here would ship ungated, so wire it
# into check (and ci.yml) in the same commit that creates it.

# Install the pinned linter into $(go env GOPATH)/bin. Optional locally (brew's
# copy is fine if it matches); CI calls it so the gate is reproducible.
lint-install:
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	@echo "ok golangci-lint $(GOLANGCI_VERSION) -> $$(go env GOPATH)/bin"

# Lint and test for the Go backend. Kept as its own target so it can be run
# directly while working; check calls it.
# golangci-lint covers gofmt, goimports, go vet and more (docs/adr/go/0004), so
# it replaces the hand-rolled format check rather than sitting beside it. Tests
# run with -race: this is a server with a pool and shared middleware.
check-go:
	@cd backend && \
	  if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; \
	  else echo 'golangci-lint not installed - see make doctor'; \
	       unformatted=$$(gofmt -l . | grep -v '\.gen\.go$$' || true); \
	       if [ -n "$$unformatted" ]; then echo 'FAIL gofmt:'; echo "$$unformatted"; exit 1; fi; \
	       go vet ./...; fi
	@cd backend && go build ./... && \
	  go test -race -covermode=atomic -coverprofile=$(GO_COVERAGE) ./...

# ---- frontend --------------------------------------------------------------
# npm ci, not install: the lockfile is the dependency set, as go.sum is.
frontend-install:
	@cd frontend && npm ci --no-audit --no-fund

frontend-dev:
	@cd frontend && npm run dev

# The Go binary embeds the built frontend (BOOTSTRAP.md §8), and //go:embed
# cannot reach a parent directory, so the build is copied into the package
# that embeds it. Run before `go build` for a binary that serves the app;
# without it the binary serves the API alone. The copy is build output and
# gitignored, apart from the .gitkeep that keeps the embed pattern valid.
WEB_DIST := backend/internal/web/dist
frontend-build:
	@cd frontend && npm run -s build
	@find $(WEB_DIST) -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
	@cp -R frontend/dist/. $(WEB_DIST)/
	@echo 'ok frontend built and copied into $(WEB_DIST)'

# Typecheck, lint, formatting and the Vitest suite, with coverage (CI uploads
# frontend/coverage/lcov.info from the same run, as it does Go's profile).
check-frontend:
	@if [ ! -d frontend/node_modules ]; then \
	  echo 'FAIL frontend/node_modules is missing - run make frontend-install'; exit 1; fi
	@cd frontend && npm run -s typecheck && npm run -s lint && npm run -s format:check && npm run -s test:coverage
	@# The production build too: tests never run vite or the PWA plugin, so a
	@# bump that breaks them would otherwise stay green until someone ships.
	@cd frontend && npm run -s build -- --logLevel warn

# Boot the backend from source and run the black-box conformance suite against
# it (issue #28). Needs docker and live servers, and is deliberately NOT part of
# check: that gate is the pre-push one and stays fast. check compiles this
# module and validates the case files; only this target runs the suite against
# live servers.
conformance:
	@./scripts/conformance.sh

check:
	@fail=0; \
	printf '%-32s' 'generated code'; \
	if [ ! -e backend/go.mod ]; then echo '- skipped (not scaffolded yet)'; \
	elif ./scripts/check-generated.sh >/dev/null 2>&1; then echo 'in sync'; \
	else echo 'FAIL - see: ./scripts/check-generated.sh'; fail=1; fi; \
	printf '%-32s' 'backend/go.mod'; \
	if [ ! -e backend/go.mod ]; then echo '- skipped (not scaffolded yet)'; \
	elif $(MAKE) --no-print-directory check-go >/dev/null 2>&1; then echo 'ok'; \
	else echo 'FAIL - see: make check-go'; fail=1; fi; \
	printf '%-32s' 'env var docs'; \
	if ./scripts/check-env-docs.sh >/dev/null 2>&1; then echo 'in sync'; \
	else echo 'FAIL - see: ./scripts/check-env-docs.sh'; fail=1; fi; \
	printf '%-32s' 'conformance harness'; \
	if [ ! -e contract/conformance/go.mod ]; then echo '- skipped (not scaffolded yet)'; \
	elif (cd contract/conformance && go build ./... && go vet ./... && go test -short ./...) >/dev/null 2>&1; then echo 'ok'; \
	else echo 'FAIL - see: cd contract/conformance && go vet ./... && go test -short ./...'; fail=1; fi; \
	printf '%-32s' 'frontend/package.json'; \
	if [ ! -e frontend/package.json ]; then echo '- skipped (not scaffolded yet)'; \
	elif $(MAKE) --no-print-directory check-frontend >/dev/null 2>&1; then echo 'ok'; \
	else echo 'FAIL - see: make check-frontend'; fail=1; fi; \
	if [ $$fail -eq 0 ]; then echo 'all green'; else echo 'FAILED - wire the surface(s) above into check'; exit 1; fi
