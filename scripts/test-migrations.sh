#!/usr/bin/env bash
# Apply backend/internal/migrations with the goose CLI against a real
# PostgreSQL: Up applies, an applied file edited in place is silently ignored
# (BOOTSTRAP.md §7.1), and Down drops everything Up created.
#
# What the schema itself enforces - defaults, CHECKs, unique indexes, foreign
# keys, cascades - is asserted in Go, by SQLSTATE and constraint name
# (backend/internal/migrations/schema_test.go, #12), inside make check.
#
# Through goose rather than psql: psql would run each file's Up AND Down, and
# going through the runner also catches a file goose cannot parse.
#
# Spins up a throwaway container and removes it on exit. Needs docker, go and
# psql.
#
# Usage: test-migrations.sh

set -uo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

CONTAINER=afloat-migration-test
# Deliberately NOT the compose instance's 5184. This script owns a throwaway
# container of its own, and sharing the port means a failed bind here silently
# falls through to the long-lived database instead.
PORT=${AFLOAT_TEST_DB_PORT:-5185}
export PGPASSWORD=test

# Homebrew keeps libpq off the default PATH because it conflicts with the
# server package; add it if psql isn't already there.
command -v psql >/dev/null 2>&1 || PATH="/opt/homebrew/opt/libpq/bin:$PATH"

for tool in docker go psql; do
  command -v "$tool" >/dev/null 2>&1 || { echo "test-migrations: $tool not found" >&2; exit 1; }
done
docker info >/dev/null 2>&1 || { echo "test-migrations: docker is not running" >&2; exit 1; }

cleanup() { docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
# Mounted at /var/lib/postgresql, not .../data — Postgres 18 refuses the old
# layout (see docker-compose.yml).
if ! docker run -d --rm --name "$CONTAINER" \
  -e POSTGRES_PASSWORD=test -e POSTGRES_DB=afloat_test \
  -p "$PORT":5432 postgres:18-alpine >/dev/null; then
  echo "test-migrations: could not start a container on port $PORT (already in use?)" >&2
  echo "  set AFLOAT_TEST_DB_PORT to something free." >&2
  exit 1
fi

for _ in $(seq 1 60); do
  docker ps -q --filter "name=^${CONTAINER}$" | grep -q . || {
    echo "test-migrations: the container exited during startup:" >&2
    docker logs "$CONTAINER" 2>&1 | tail -10 >&2; exit 1; }
  pg_isready -h localhost -p "$PORT" -q && break
  sleep 1
done
pg_isready -h localhost -p "$PORT" -q || { echo "test-migrations: postgres never became ready" >&2; exit 1; }

# Prove we are talking to OUR container and not something else already on this
# port: afloat_test is created by this script's own POSTGRES_DB and nothing else.
if [ "$(command psql -h localhost -p "$PORT" -U postgres -d afloat_test -qtA -c 'SELECT current_database();' 2>&1)" != "afloat_test" ]; then
  echo "test-migrations: port $PORT is not this script's container — refusing to run" >&2
  exit 1
fi

psql() { command psql -h localhost -p "$PORT" -U postgres -d afloat_test -qtA "$@"; }

# The goose the binary links, not @latest: a runner newer than the one in
# production could accept a file the shipped one refuses.
goose_version=$(awk '$1 == "github.com/pressly/goose/v3" { print $2 }' backend/go.mod)
[ -n "$goose_version" ] || { echo "test-migrations: no goose version in backend/go.mod" >&2; exit 1; }
# goose DIR COMMAND...
goose() {
  local dir=$1; shift
  go run "github.com/pressly/goose/v3/cmd/goose@$goose_version" -dir "$dir" \
    postgres "host=localhost port=$PORT user=postgres password=test dbname=afloat_test sslmode=disable" "$@" 2>&1
}

pass=0
fail=0
value() { psql -c "$1" 2>&1 | head -1; }

echo "up"
up_out=$(goose backend/internal/migrations up)
if grep -q "successfully migrated database to version: 1" <<<"$up_out"; then
  echo "  PASS  00001 applies clean"; pass=$((pass + 1))
else
  echo "  FAIL  goose up did not apply:"; tail -5 <<<"$up_out"
  exit 1
fi

# ---------------------------------------------------------------------------
# The §7.1 hazard. Edit an applied migration in place and re-run goose
# against a database that already has version 1: it must silently do nothing.
# The edit goes into a scratch copy, so the tree is never written to.
# ---------------------------------------------------------------------------
echo "in-place edit (BOOTSTRAP.md §7.1)"
edited=$(mktemp -d)
cp backend/internal/migrations/*.sql "$edited"/
awk '/^-- \+goose Down$/ { print "ALTER TABLE households ADD COLUMN edit_probe text;"; print "" } { print }' \
  backend/internal/migrations/00001_baseline.sql >"$edited"/00001_baseline.sql
# Without this guard a reformatted Down marker leaves the copy unedited, and
# both assertions below would pass on a probe that never happened.
if ! grep -q '^ALTER TABLE households ADD COLUMN edit_probe' "$edited"/00001_baseline.sql; then
  echo "  FAIL  the probe edit did not land - is the '-- +goose Down' marker still there?"; fail=$((fail + 1))
fi
edit_out=$(goose "$edited" up)
if grep -q "no migrations to run" <<<"$edit_out"; then
  echo "  PASS  goose reports nothing to do"; pass=$((pass + 1))
else
  echo "  FAIL  goose did not report 'no migrations to run': $(tail -2 <<<"$edit_out")"; fail=$((fail + 1))
fi
probe=$(value "SELECT count(*) FROM information_schema.columns WHERE table_name='households' AND column_name='edit_probe';")
if [ "$probe" = "0" ]; then
  echo "  PASS  goose left the OLD schema in place, silently — §7.1 confirmed"; pass=$((pass + 1))
else
  echo "  FAIL  goose applied the edit; §7.1's claim is wrong and the doc needs correcting"; fail=$((fail + 1))
fi
rm -rf "$edited"

echo "down"
down_out=$(goose backend/internal/migrations down)
if grep -q "OK" <<<"$down_out"; then
  echo "  PASS  00001 Down applies clean"; pass=$((pass + 1))
else
  echo "  FAIL  goose down did not apply: $(tail -2 <<<"$down_out")"; fail=$((fail + 1))
fi
# goose_db_version is goose's own ledger and survives a down by design.
left=$(value "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name <> 'goose_db_version';")
if [ "$left" = "0" ]; then
  echo "  PASS  Down left no tables behind"; pass=$((pass + 1))
else
  echo "  FAIL  Down left $left table(s) behind"; fail=$((fail + 1))
fi

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
