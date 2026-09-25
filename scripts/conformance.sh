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
# The local-disabled profile's pair (BOOTSTRAP.md §1): same builds, booted with
# AUTH_LOCAL_ENABLED=false, for the cases that declare `profile: local-disabled`.
GO_LOCAL_DISABLED_PORT=${AFLOAT_GO_LOCAL_DISABLED_PORT:-5186}
KOTLIN_LOCAL_DISABLED_PORT=${AFLOAT_KOTLIN_LOCAL_DISABLED_PORT:-5187}

# One database per backend, both migrated from db/migrations. A shared database
# was the other candidate and is worse: it makes case ordering significant and a
# failure then tells you two things at once (#28). A backend's second-profile
# process shares its twin's database rather than getting a third and fourth:
# compose creates only these two, and the local-disabled cases write nothing.
GO_DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/afloat_go"
KOTLIN_DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/afloat_kotlin"

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

# The environment both backends read, identical by name and value except PORT
# and the database (BOOTSTRAP.md §12). Spelled out rather than inherited from
# the caller's .env, so a run means the same thing on every machine.
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

# What differs per profile. The default pair migrates its database; the
# local-disabled pair starts after it and must not migrate the same one again
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

pid_of() {
  local entry
  for entry in "${STARTED[@]}"; do
    if [ "${entry% *}" = "$1" ]; then echo "${entry##* }"; return; fi
  done
}

echo '==> starting the default pair'
start go "$GO_PORT" "$GO_DATABASE_URL" default_env "$LOG_DIR/afloat-go"
start kotlin "$KOTLIN_PORT" "$KOTLIN_DATABASE_URL" default_env java -jar "$KOTLIN_JAR"
wait_ready go "$GO_PORT" "$(pid_of go)"
wait_ready kotlin "$KOTLIN_PORT" "$(pid_of kotlin)"

# Only now: the default pair has migrated both databases, so this pair can
# start on them with AUTO_MIGRATE=false.
echo '==> starting the local-disabled pair'
start go-local-disabled "$GO_LOCAL_DISABLED_PORT" "$GO_DATABASE_URL" local_disabled_env "$LOG_DIR/afloat-go"
start kotlin-local-disabled "$KOTLIN_LOCAL_DISABLED_PORT" "$KOTLIN_DATABASE_URL" local_disabled_env java -jar "$KOTLIN_JAR"
wait_ready go-local-disabled "$GO_LOCAL_DISABLED_PORT" "$(pid_of go-local-disabled)"
wait_ready kotlin-local-disabled "$KOTLIN_LOCAL_DISABLED_PORT" "$(pid_of kotlin-local-disabled)"

echo '==> conformance'
cd contract/conformance
AFLOAT_REQUIRE_CONFORMANCE=1 \
AFLOAT_GO_BASE_URL="http://localhost:${GO_PORT}/api" \
AFLOAT_KOTLIN_BASE_URL="http://localhost:${KOTLIN_PORT}/api" \
AFLOAT_GO_LOCAL_DISABLED_BASE_URL="http://localhost:${GO_LOCAL_DISABLED_PORT}/api" \
AFLOAT_KOTLIN_LOCAL_DISABLED_BASE_URL="http://localhost:${KOTLIN_LOCAL_DISABLED_PORT}/api" \
AFLOAT_GO_DATABASE_URL="$GO_DATABASE_URL" \
AFLOAT_KOTLIN_DATABASE_URL="$KOTLIN_DATABASE_URL" \
  go test -count=1 -v ./...
