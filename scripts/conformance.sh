#!/usr/bin/env bash
# Boot both backends from source, against their own databases, and run the
# cross-backend conformance suite against the pair (issue #28).
#
# Deliberately NOT part of `make check`. That gate is the pre-push one and stays
# fast; this needs a Postgres, two builds and two JVM/Go processes, and its
# whole value is that it runs both backends at once.
#
# Both are built and then run as plain processes rather than via `go run` and
# `./gradlew bootRun`. Those wrap the server in a parent that outlives a kill of
# itself, which leaves a port bound and the next run failing for a reason that
# has nothing to do with the code.
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT=$(pwd)

DB_HOST=${AFLOAT_DB_HOST:-localhost}
DB_PORT=${AFLOAT_DB_PORT:-5184}
DB_USER=${AFLOAT_DB_USER:-afloat}
DB_PASSWORD=${AFLOAT_DB_PASSWORD:-afloat}

GO_PORT=${AFLOAT_GO_PORT:-5182}
KOTLIN_PORT=${AFLOAT_KOTLIN_PORT:-5183}

# One database per backend, both migrated from db/migrations. A shared database
# was the other candidate and is worse: it makes case ordering significant and a
# failure then tells you two things at once (#28).
GO_DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/afloat_go"
KOTLIN_DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/afloat_kotlin"

LOG_DIR=$(mktemp -d)
GO_LOG="$LOG_DIR/go.log"
KOTLIN_LOG="$LOG_DIR/kotlin.log"
GO_PID=""
KOTLIN_PID=""

cleanup() {
  local status=$?
  set +e
  if [ -n "$GO_PID" ]; then kill "$GO_PID" 2>/dev/null; wait "$GO_PID" 2>/dev/null; fi
  if [ -n "$KOTLIN_PID" ]; then kill "$KOTLIN_PID" 2>/dev/null; wait "$KOTLIN_PID" 2>/dev/null; fi
  if [ $status -ne 0 ]; then
    echo
    echo "--- go backend log (last 40 lines) ---"
    tail -40 "$GO_LOG" 2>/dev/null || echo "(no log)"
    echo "--- kotlin backend log (last 40 lines) ---"
    tail -40 "$KOTLIN_LOG" 2>/dev/null || echo "(no log)"
  fi
  rm -rf "$LOG_DIR"
  exit $status
}
trap cleanup EXIT

# The environment both backends read, identical by name and value except PORT
# and the database (BOOTSTRAP.md §12). Spelled out rather than inherited from
# the caller's .env, so a run means the same thing on every machine.
common_env() {
  echo "LOG_FORMAT=text"
  echo "LOG_LEVEL=info"
  echo "AUTO_MIGRATE=true"
  echo "HTTP_READ_TIMEOUT=30s"
  echo "HTTP_WRITE_TIMEOUT=60s"
  echo "HTTP_IDLE_TIMEOUT=120s"
  echo "SHUTDOWN_TIMEOUT=10s"
  echo "AUTH_LOCAL_ENABLED=true"
  echo "AUTH_GOOGLE_ENABLED=false"
  echo "SESSION_TTL=720h"
  echo "SESSION_MAX_LIFETIME=2160h"
  # Off because the harness speaks plain HTTP to localhost. A Secure cookie is
  # never sent back over http://, so leaving it on would make every session
  # case fail for a reason that is not a divergence.
  echo "COOKIE_SECURE=false"
  # The documented default, and what cases/health.yaml asserts.
  echo "VERSION=dev"
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

echo '==> building the go backend'
(cd backend && go build -o "$LOG_DIR/afloat-go" ./cmd/afloat)

echo '==> building the kotlin backend'
(cd backend-kotlin && ./gradlew --quiet --console=plain bootJar)
# bootJar leaves two artefacts in build/libs: the executable one and
# `-plain.jar`, which is the library jar and has no Main-Class. Picking by
# `head -1` gets the wrong one, and the error it produces - "no main manifest
# attribute" - says nothing about which jar it read.
KOTLIN_JAR=$(ls "$ROOT"/backend-kotlin/build/libs/*.jar | grep -v -- '-plain\.jar$' | head -1)
if [ -z "$KOTLIN_JAR" ]; then
  echo "FAIL no executable jar in backend-kotlin/build/libs" >&2
  exit 1
fi

echo '==> starting both backends'
env $(common_env) DATABASE_URL="$GO_DATABASE_URL" PORT="$GO_PORT" \
  "$LOG_DIR/afloat-go" >"$GO_LOG" 2>&1 &
GO_PID=$!

env $(common_env) DATABASE_URL="$KOTLIN_DATABASE_URL" PORT="$KOTLIN_PORT" \
  java -jar "$KOTLIN_JAR" >"$KOTLIN_LOG" 2>&1 &
KOTLIN_PID=$!

wait_ready go "$GO_PORT" "$GO_PID"
wait_ready kotlin "$KOTLIN_PORT" "$KOTLIN_PID"

echo '==> conformance'
cd contract/conformance
AFLOAT_REQUIRE_CONFORMANCE=1 \
AFLOAT_GO_BASE_URL="http://localhost:${GO_PORT}/api" \
AFLOAT_KOTLIN_BASE_URL="http://localhost:${KOTLIN_PORT}/api" \
  go test -v ./...
