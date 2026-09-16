.PHONY: help setup hooks-install claude-install doctor check gen-goose-migrations test-migrations \
        db-up db-down db-reset test-migration-runners

# `make` with no target prints help.
.DEFAULT_GOAL := help

# The three surfaces `check` gates on, by the file whose existence means the
# surface has been scaffolded (BOOTSTRAP.md §11 steps 5-7).
SURFACES := backend/go.mod backend-kotlin/build.gradle.kts frontend/package.json

# Where gen-goose-migrations writes. Generated, committed, //go:embed-ed by the
# Go backend once it exists (BOOTSTRAP.md §7) - never hand-edited.
GOOSE_DIR := backend/internal/migrations

# Where gen-goose-migrations actually writes. Overridden by check, which
# generates into a scratch directory and compares, so the gate reports drift
# instead of silently repairing it.
GOOSE_OUT ?= $(GOOSE_DIR)

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
	@echo "  gen-goose-migrations    regenerate the Go backend's embedded goose migrations"
	@echo "  test-migrations         apply db/migrations to a throwaway Postgres and assert"
	@echo "  test-migration-runners  run Flyway and goose for real and compare the two schemas"
	@echo "  check                   pre-push gate: pass/fail per surface"

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
# `git push`. Once .github/workflows/ci.yml exists it must mirror it step for
# step, so green locally ~ green in CI.
#
# Nothing is scaffolded yet, so every surface skips. A surface that exists but
# has no steps here FAILS rather than skipping: otherwise the step that
# scaffolds a backend would ship an ungated one and this would stay green.
# Replace a surface's FAIL branch with its real lint/test steps as it lands.
check:
	@fail=0; \
	printf '%-32s' 'goose migrations'; \
	if ! ls db/migrations/V*.sql >/dev/null 2>&1; then echo '- skipped (no migrations yet)'; \
	else \
	  tmp=$$(mktemp -d); \
	  if ./scripts/gen-goose-migrations.sh "$$tmp" >/dev/null 2>&1 \
	     && diff -rq "$$tmp" $(GOOSE_DIR) >/dev/null 2>&1; then echo 'in sync'; \
	  else echo 'FAIL - stale or hand-edited; run make gen-goose-migrations'; fail=1; fi; \
	  rm -rf "$$tmp"; \
	fi; \
	for s in $(SURFACES); do \
	  printf '%-32s' "$$s"; \
	  if [ -e "$$s" ]; then echo 'FAIL - exists, but check has no steps for it'; fail=1; \
	  else echo '- skipped (not scaffolded yet)'; fi; \
	done; \
	if [ $$fail -eq 0 ]; then echo 'all green'; else echo 'FAILED - wire the surface(s) above into check'; exit 1; fi
