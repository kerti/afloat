#!/usr/bin/env bash
# Boot the backend from source and run the black-box conformance suite against
# it (issue #28).
#
# Deliberately NOT part of `make check`. That gate is the pre-push one and stays
# fast; this needs a Postgres, a build and live servers.
#
# Built and then run as a plain process rather than via `go run`, which wraps
# the server in a parent that outlives a kill of itself, leaving a port bound
# and the next run failing for a reason that has nothing to do with the code.
set -euo pipefail

cd "$(dirname "$0")/.."

DB_HOST=${AFLOAT_DB_HOST:-localhost}
DB_PORT=${AFLOAT_DB_PORT:-5184}
DB_USER=${AFLOAT_DB_USER:-afloat}
DB_PASSWORD=${AFLOAT_DB_PASSWORD:-afloat}

SERVER_PORT=${AFLOAT_PORT:-5182}
# The local-disabled profile's server (BOOTSTRAP.md §1): the same build, booted
# with AUTH_LOCAL_ENABLED=false, for the cases that declare
# `profile: local-disabled`.
LOCAL_DISABLED_PORT=${AFLOAT_LOCAL_DISABLED_PORT:-5186}

# Both servers share the one database: the local-disabled cases write nothing.
DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/afloat"

LOG_DIR=$(mktemp -d)
# One entry per process, as "name pid", so cleanup stops and reports whatever
# actually started, however far the script got.
STARTED=()

cleanup() {
  local status=$?
  set +e
  local entry name pid
  for entry in "${STARTED[@]+"${STARTED[@]}"}"; do
    pid=${entry##* }
    kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null
  done
  if [ $status -ne 0 ]; then
    for entry in "${STARTED[@]+"${STARTED[@]}"}"; do
      name=${entry% *}
      echo
      echo "--- $name log (last 40 lines) ---"
      tail -40 "$LOG_DIR/$name.log" 2>/dev/null || echo "(no log)"
    done
  fi
  rm -rf "$LOG_DIR"
  exit $status
}
trap cleanup EXIT

# The environment the backend reads (BOOTSTRAP.md §12), except PORT and the
# database. Spelled out rather than inherited from the caller's .env, so a run
# means the same thing on every machine.
common_env() {
  echo "LOG_FORMAT=text"
  echo "LOG_LEVEL=info"
  echo "HTTP_READ_TIMEOUT=30s"
  echo "HTTP_WRITE_TIMEOUT=60s"
  echo "HTTP_IDLE_TIMEOUT=120s"
  echo "SHUTDOWN_TIMEOUT=10s"
  echo "SESSION_TTL=720h"
  echo "SESSION_MAX_LIFETIME=2160h"
  # What cases/login.yaml's windows assume: 1s, then 2s.
  echo "LOGIN_FIRST_BACKOFF=1s"
  echo "LOGIN_MAX_BACKOFF=5m"
  # Off because the harness speaks plain HTTP to localhost. A Secure cookie is
  # never sent back over http://, so leaving it on would make every session
  # case fail for a reason that is not a divergence.
  echo "COOKIE_SECURE=false"
  # The documented default, and what cases/health.yaml asserts.
  echo "VERSION=dev"
}

# What differs per profile. The default server migrates the database; the
# local-disabled server starts after it and must not migrate it again
# concurrently. Google is on there because a backend refuses to boot with no
# identity provider at all.
default_env() {
  echo "AUTO_MIGRATE=true"
  echo "AUTH_LOCAL_ENABLED=true"
  echo "AUTH_GOOGLE_ENABLED=false"
}

local_disabled_env() {
  echo "AUTO_MIGRATE=false"
  echo "AUTH_LOCAL_ENABLED=false"
  echo "AUTH_GOOGLE_ENABLED=true"
}

# start NAME PORT DATABASE_URL PROFILE_ENV_FN CMD...
start() {
  local name=$1 port=$2 db=$3 profile=$4
  shift 4
  env $(common_env) $($profile) DATABASE_URL="$db" PORT="$port" \
    "$@" >"$LOG_DIR/$name.log" 2>&1 &
  STARTED+=("$name $!")
}

wait_ready() {
  local name=$1 port=$2 pid=$3 deadline=$((SECONDS + 120))
  printf 'waiting for %s on %s' "$name" "$port"
  while [ $SECONDS -lt $deadline ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      echo ' DIED'
      return 1
    fi
    if curl -fsS -o /dev/null "http://localhost:${port}/api/health" 2>/dev/null; then
      echo ' ok'
      return 0
    fi
    printf '.'
    sleep 1
  done
  echo ' TIMED OUT'
  return 1
}

echo '==> postgres'
make --no-print-directory db-up

echo '==> building the backend'
(cd backend && go build -o "$LOG_DIR/afloat" ./cmd/afloat)

pid_of() {
  local entry
  for entry in "${STARTED[@]}"; do
    if [ "${entry% *}" = "$1" ]; then echo "${entry##* }"; return; fi
  done
}

echo '==> starting the default server'
start default "$SERVER_PORT" "$DATABASE_URL" default_env "$LOG_DIR/afloat"
wait_ready default "$SERVER_PORT" "$(pid_of default)"

# Only now: the default server has migrated the database, so this one can
# start on it with AUTO_MIGRATE=false.
echo '==> starting the local-disabled server'
start local-disabled "$LOCAL_DISABLED_PORT" "$DATABASE_URL" local_disabled_env "$LOG_DIR/afloat"
wait_ready local-disabled "$LOCAL_DISABLED_PORT" "$(pid_of local-disabled)"

echo '==> conformance'
cd contract/conformance
AFLOAT_REQUIRE_CONFORMANCE=1 \
AFLOAT_BASE_URL="http://localhost:${SERVER_PORT}/api" \
AFLOAT_LOCAL_DISABLED_BASE_URL="http://localhost:${LOCAL_DISABLED_PORT}/api" \
AFLOAT_DATABASE_URL="$DATABASE_URL" \
  go test -count=1 -v ./...
