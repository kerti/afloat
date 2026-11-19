#!/usr/bin/env bash
# Verify the committed generated code matches what the generators produce right
# now, WITHOUT touching the working tree.
#
# The obvious version of this check — regenerate in place, then `git diff` — is
# wrong twice over: it silently repairs a hand-edit instead of reporting it, and
# `git diff` says nothing at all about files git is not yet tracking. Both were
# real bugs here. So everything generates into a scratch directory and is
# compared; the tree is never written to.
#
# Usage: check-generated.sh

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

tmp=$(mktemp -d)
status=0
report() {
  if [ "$2" = ok ]; then
    printf '  ok    %s\n' "$1"
  else
    printf '  FAIL  %s - %s\n' "$1" "$2"
    status=1
  fi
}

# Both generators take their output path from their config file and ignore a
# command-line override, so each check runs against a copy of the config with
# `output`/`out` repointed at the scratch directory. The copies live in backend/
# because the paths inside them are relative to it.
# sqlc resolves `out` RELATIVE TO ITS CONFIG FILE and joins even an absolute
# path onto it, so its scratch output has to live under backend/ rather than in
# $tmp. (An absolute path here silently produces backend/var/folders/...)
sqlc_out=backend/.sqlc-check-out
cleanup() {
  rm -rf "$tmp" "$repo_root/$sqlc_out" \
    "$repo_root/backend/.oapi-check.yaml" "$repo_root/backend/.sqlc-check.yaml"
}
trap cleanup EXIT

# --- contract -> Go server interfaces and types ------------------------------
sed "s|output: internal/api/api.gen.go|output: $tmp/api.gen.go|" backend/oapi-codegen.yaml >backend/.oapi-check.yaml
(cd backend && go tool oapi-codegen -config .oapi-check.yaml ../contract/openapi.yaml)
if diff -q "$tmp/api.gen.go" backend/internal/api/api.gen.go >/dev/null 2>&1; then
  report "backend/internal/api (oapi-codegen)" ok
else
  report "backend/internal/api (oapi-codegen)" "stale or hand-edited; run make gen-oapi"
fi

# --- migrations + queries -> typed queries --------------------------------
# sqlc takes its output path from the config file, so the check runs a copy with
# `out` pointed at the scratch directory. Paths inside the config are relative
# to backend/, which is why the copy lives there.
sed "s|out: \"internal/db\"|out: \"$(basename "$sqlc_out")\"|" backend/sqlc.yaml >backend/.sqlc-check.yaml
(cd backend && go tool sqlc -f .sqlc-check.yaml generate)
if diff -rq "$sqlc_out" backend/internal/db >/dev/null 2>&1; then
  report "backend/internal/db (sqlc)" ok
else
  report "backend/internal/db (sqlc)" "stale or hand-edited; run make gen-sqlc"
fi

# --- contract -> frontend TypeScript types ------------------------------------
# Same contract, other surface (BOOTSTRAP.md §6). Skipped until the frontend's
# dependencies are installed: the generator lives in its node_modules.
if [ -d frontend/node_modules ]; then
  (cd frontend && npx --no-install openapi-typescript ../contract/openapi.yaml -o "$tmp/schema.gen.ts" >/dev/null 2>&1)
  if diff -q "$tmp/schema.gen.ts" frontend/src/api/schema.gen.ts >/dev/null 2>&1; then
    report "frontend/src/api/schema.gen.ts (openapi-typescript)" ok
  else
    report "frontend/src/api/schema.gen.ts (openapi-typescript)" "stale or hand-edited; run make gen-ts"
  fi
elif [ -e frontend/package.json ]; then
  report "frontend/src/api/schema.gen.ts (openapi-typescript)" "frontend/node_modules missing; run make frontend-install"
fi

exit $status
