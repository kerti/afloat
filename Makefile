.PHONY: help setup hooks-install claude-install doctor check gen-goose-migrations test-migrations \
        db-up db-down db-reset test-migration-runners gen-oapi gen-sqlc gen check-go check-kotlin \
        lint-install sync-kotlin-migrations sync-denylist

# `make` with no target prints help.
.DEFAULT_GOAL := help

# The three surfaces `check` gates on, by the file whose existence means the
# surface has been scaffolded (BOOTSTRAP.md §11 steps 5-7).
SURFACES := backend/go.mod backend-kotlin/build.gradle.kts frontend/package.json

# Where sync-kotlin-migrations writes. The Kotlin backend carries its
# migrations inside the jar, so the canonical db/migrations are COPIED here and
# committed. Generated output - never hand-edited (BOOTSTRAP.md §7, §12).
KOTLIN_MIGRATIONS := backend-kotlin/src/main/resources/db/migration

# Where gen-goose-migrations writes. Generated, committed, //go:embed-ed by the
# Go backend once it exists (BOOTSTRAP.md §7) - never hand-edited.
GOOSE_DIR := backend/internal/migrations

# Where gen-goose-migrations actually writes. Overridden by check, which
# generates into a scratch directory and compares, so the gate reports drift
# instead of silently repairing it.
GOOSE_OUT ?= $(GOOSE_DIR)

# One version for the lint gate, so a local pass and a CI pass mean the same
# thing. .github/workflows/ci.yml installs exactly this via `make lint-install`;
# bump it here and CI follows.
GOLANGCI_VERSION := v2.13.2

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
	@echo "  db-up                   start Postgres (one instance, two databases)"
	@echo "  db-down                 stop Postgres, keeping its data"
	@echo "  db-reset                destroy and recreate both databases from scratch"
	@echo ""
	@echo "Workflow:"
	@echo "  gen                     run every generator (contract, sqlc, goose)"
	@echo "  gen-oapi                regenerate Go server interfaces from contract/openapi.yaml"
	@echo "  gen-sqlc                regenerate typed queries from db/migrations + backend/queries"
	@echo "  gen-goose-migrations    regenerate the Go backend's embedded goose migrations"
	@echo "  sync-kotlin-migrations  copy db/migrations into the Kotlin backend's resources"
	@echo "  test-migrations         apply db/migrations to a throwaway Postgres and assert"
	@echo "  test-migration-runners  run Flyway and goose for real and compare the two schemas"
	@echo "  check                   pre-push gate: pass/fail per surface"
	@echo "  check-kotlin            build, lint and test the Kotlin backend"
	@echo "  lint-install            install the pinned golangci-lint (the version CI uses)"
	@echo "  sync-denylist           copy shared/common_passwords.txt into both backends"

# ---- first run -------------------------------------------------------------
# Idempotent - safe to re-run, and worth re-running after a pull that touches
# .claude/ or .githooks/ (both need a chmod that git alone won't reapply on some
# clones). Grows backend/frontend install steps as those surfaces land.
setup: hooks-install claude-install
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
	@echo "ok Claude Code hooks armed (session-start, pre-push gate, format-on-write)"

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
	@printf '%-16s' 'java';         /usr/libexec/java_home -v 21 >/dev/null 2>&1 || java -version 2>&1 | grep -q '"21' && echo 'ok (21)' || echo 'MISSING (Temurin 21 via SDKMAN)'
	@printf '%-16s' 'docker';       command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 && echo 'ok' || echo 'MISSING (docker compose)'
	@printf '%-16s' 'golangci-lint'; command -v golangci-lint >/dev/null 2>&1 && echo 'ok' || echo 'MISSING (brew install golangci-lint)'
	@printf '%-16s' 'jq';           command -v jq     >/dev/null 2>&1 && echo 'ok'                   || echo 'MISSING (Claude Code hooks need it)'
	@printf '%-16s' 'claude';       command -v claude >/dev/null 2>&1 && echo 'ok'                   || echo 'not on PATH (install separately; the Makefile cannot)'
	@printf '%-16s' 'git hooks';    [ "$$(git config core.hooksPath)" = ".githooks" ] && echo 'armed' || echo 'NOT armed - run make hooks-install'
	@printf '%-16s' 'pii-patterns'; [ -f .pii-patterns ] && echo 'present'                           || echo 'MISSING - run make hooks-install'
	@printf '%-16s' 'claude hooks'; [ -x .claude/hooks/session-start.sh ] && echo 'executable'       || echo 'NOT executable - run make claude-install'

# ---- codegen ---------------------------------------------------------------
# db/migrations/V####__name.sql is canonical and Flyway-native; db/undo/U####__
# name.sql is the Down source. Neither is consumable by goose as-is, so the
# script concatenates each pair into the file the Go binary //go:embeds
# (BOOTSTRAP.md §7). GOOSE_OUT lets check generate somewhere else and compare.
gen-goose-migrations:
	@./scripts/gen-goose-migrations.sh $(GOOSE_OUT)

# db/migrations is canonical and already Flyway-native, so unlike the goose
# files this is a copy rather than a generator. The Kotlin backend needs its
# migrations INSIDE the jar: a filesystem: location pointing at ../db/migrations
# works in tests and breaks the moment it runs from a jar or a container.
# check regenerates into a scratch directory and diffs, so drift is reported
# rather than silently repaired.
sync-kotlin-migrations:
	@./scripts/sync-kotlin-migrations.sh $(KOTLIN_MIGRATIONS)

# shared/common_passwords.txt is owned by neither backend, and neither can read
# it where it lives: Go's //go:embed cannot reach a parent directory, and the
# Kotlin backend needs the file inside the jar for the same reason as the
# migrations above. Both copies are generated and diffed by check.
sync-denylist:
	@./scripts/sync-denylist.sh

# The contract leads (BOOTSTRAP.md §6): these read contract/openapi.yaml and
# db/migrations, never the other way round. Output is committed and never
# hand-edited; check regenerates and diffs.
gen-oapi:
	@cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../contract/openapi.yaml
	@echo 'ok backend/internal/api/api.gen.go'

gen-sqlc:
	@cd backend && go tool sqlc generate
	@echo 'ok backend/internal/db'

gen: gen-oapi gen-sqlc gen-goose-migrations

# Schema behaviour against a real Postgres: defaults, CHECKs, the soft-delete
# aware indexes, cascade behaviour, and that the undo drops what the migration
# created. Needs docker; not part of check for that reason.
test-migrations:
	@./scripts/test-migrations.sh

# Runs the RUNNERS, not the SQL: proves Flyway and goose agree on the schema,
# which test-migrations cannot catch (a generator that drops a statement, or a
# runner that disagrees about what is applied). Needs db-up first.
test-migration-runners:
	@./scripts/test-migration-runners.sh

# ---- database --------------------------------------------------------------
# One Postgres instance, two databases (BOOTSTRAP.md §3). The backends live
# behind the `go` and `kotlin` compose profiles and do not exist yet, so a plain
# `up` starts Postgres alone.
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
# V0001 in place leaves Flyway refusing to start and goose silently skipping, so
# the edit is only safe paired with throwing both databases away. This is that
# command, and the reason the policy is safe to follow.
db-reset:
	@docker compose down -v
	@$(MAKE) --no-print-directory db-up
	@echo 'ok both databases recreated empty - re-run your migrations'

# ---- workflow --------------------------------------------------------------
# Pre-push gate: .claude/hooks/pre-push-gate.sh runs this before every
# `git push`. .github/workflows/ci.yml mirrors it step for step, so green
# locally ~ green in CI - a surface wired into one is wired into both.
#
# Nothing is scaffolded yet, so every surface skips. A surface that exists but
# has no steps here FAILS rather than skipping: otherwise the step that
# scaffolds a backend would ship an ungated one and this would stay green.
# Replace a surface's FAIL branch with its real lint/test steps as it lands.
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
	@cd backend && go build ./... && go test -race ./...

# Build, lint and test the Kotlin backend. Gradle's own `check` lifecycle task
# is deliberately the entry point rather than `test`: it already depends on
# compilation and on every verification task registered in the build, so adding
# ktlint or detekt later wires itself into this gate with no Makefile change.
#
# The wrapper is required, not optional. A build that runs on whatever Gradle
# happens to be on PATH is not the build CI runs.
check-kotlin:
	@if [ ! -x backend-kotlin/gradlew ]; then \
	  echo 'FAIL backend-kotlin/gradlew is missing or not executable - commit the Gradle wrapper'; \
	  exit 1; \
	fi
	@cd backend-kotlin && ./gradlew --quiet --console=plain check

check:
	@fail=0; \
	printf '%-32s' 'goose migrations'; \
	if ! ls db/migrations/V*.sql >/dev/null 2>&1; then echo '- skipped (no migrations yet)'; \
	else \
	  tmp=$$(mktemp -d); \
	  if ./scripts/gen-goose-migrations.sh "$$tmp" >/dev/null 2>&1 \
	     && diff -rq -x '*.go' "$$tmp" $(GOOSE_DIR) >/dev/null 2>&1; then echo 'in sync'; \
	  else echo 'FAIL - stale or hand-edited; run make gen-goose-migrations'; fail=1; fi; \
	  rm -rf "$$tmp"; \
	fi; \
	printf '%-32s' 'generated code'; \
	if [ ! -e backend/go.mod ]; then echo '- skipped (not scaffolded yet)'; \
	elif ./scripts/check-generated.sh >/dev/null 2>&1; then echo 'in sync'; \
	else echo 'FAIL - see: ./scripts/check-generated.sh'; fail=1; fi; \
	printf '%-32s' 'backend/go.mod'; \
	if [ ! -e backend/go.mod ]; then echo '- skipped (not scaffolded yet)'; \
	elif $(MAKE) --no-print-directory check-go >/dev/null 2>&1; then echo 'ok'; \
	else echo 'FAIL - see: make check-go'; fail=1; fi; \
	printf '%-32s' 'kotlin migration copy'; \
	if [ ! -e backend-kotlin/build.gradle.kts ]; then echo '- skipped (not scaffolded yet)'; \
	else \
	  tmp=$$(mktemp -d); \
	  if ./scripts/sync-kotlin-migrations.sh "$$tmp" >/dev/null 2>&1 \
	     && diff -rq "$$tmp" $(KOTLIN_MIGRATIONS) >/dev/null 2>&1; then echo 'in sync'; \
	  else echo 'FAIL - stale or hand-edited; run make sync-kotlin-migrations'; fail=1; fi; \
	  rm -rf "$$tmp"; \
	fi; \
	printf '%-32s' 'denylist copies'; \
	if [ ! -e shared/common_passwords.txt ]; then echo '- skipped (no denylist)'; \
	else \
	  tmp=$$(mktemp -d); \
	  if ./scripts/sync-denylist.sh "$$tmp" >/dev/null 2>&1 \
	     && diff -q "$$tmp/backend/internal/auth/common_passwords.txt" backend/internal/auth/common_passwords.txt >/dev/null 2>&1 \
	     && { [ ! -e backend-kotlin/build.gradle.kts ] \
	          || diff -q "$$tmp/backend-kotlin/src/main/resources/common_passwords.txt" backend-kotlin/src/main/resources/common_passwords.txt >/dev/null 2>&1; }; \
	  then echo 'in sync'; \
	  else echo 'FAIL - stale or hand-edited; run make sync-denylist'; fail=1; fi; \
	  rm -rf "$$tmp"; \
	fi; \
	printf '%-32s' 'env var parity'; \
	if ./scripts/check-env-parity.sh >/dev/null 2>&1; then echo 'in sync'; \
	else echo 'FAIL - see: ./scripts/check-env-parity.sh'; fail=1; fi; \
	printf '%-32s' 'backend-kotlin/build.gradle.kts'; \
	if [ ! -e backend-kotlin/build.gradle.kts ]; then echo '- skipped (not scaffolded yet)'; \
	elif $(MAKE) --no-print-directory check-kotlin >/dev/null 2>&1; then echo 'ok'; \
	else echo 'FAIL - see: make check-kotlin'; fail=1; fi; \
	printf '%-32s' 'frontend/package.json'; \
	if [ -e frontend/package.json ]; then echo 'FAIL - exists, but check has no steps for it'; fail=1; \
	else echo '- skipped (not scaffolded yet)'; fi; \
	if [ $$fail -eq 0 ]; then echo 'all green'; else echo 'FAILED - wire the surface(s) above into check'; exit 1; fi
