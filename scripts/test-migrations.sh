#!/usr/bin/env bash
# Apply db/migrations against a real PostgreSQL and assert the schema behaves:
# defaults land where PRD.md §5 says, every CHECK rejects what it should, the
# soft-delete-aware indexes allow what they should, foreign keys cascade the way
# BOOTSTRAP.md §4's soft-delete carve-out implies, and the undo file drops
# everything it created.
#
# This is a schema test, not a migration-runner test. It runs the SQL directly
# with psql rather than through Flyway or goose, so it is meaningful before
# either backend exists (BOOTSTRAP.md §11 steps 5-6). Step 4 is what proves the
# two runners themselves agree.
#
# Spins up a throwaway container and removes it on exit. Needs docker and psql.
#
# Usage: test-migrations.sh

set -uo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

CONTAINER=afloat-migration-test
PORT=${PGPORT_TEST:-55432}
export PGPASSWORD=test

# Homebrew keeps libpq off the default PATH because it conflicts with the
# server package; add it if psql isn't already there.
command -v psql >/dev/null 2>&1 || PATH="/opt/homebrew/opt/libpq/bin:$PATH"

for tool in docker psql; do
  command -v "$tool" >/dev/null 2>&1 || { echo "test-migrations: $tool not found" >&2; exit 1; }
done
docker info >/dev/null 2>&1 || { echo "test-migrations: docker is not running" >&2; exit 1; }

cleanup() { docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker run -d --rm --name "$CONTAINER" \
  -e POSTGRES_PASSWORD=test -e POSTGRES_DB=afloat_test \
  -p "$PORT":5432 postgres:18-alpine >/dev/null

for _ in $(seq 1 60); do
  pg_isready -h localhost -p "$PORT" -q && break
  sleep 1
done
pg_isready -h localhost -p "$PORT" -q || { echo "test-migrations: postgres never became ready" >&2; exit 1; }

psql() { command psql -h localhost -p "$PORT" -U postgres -d afloat_test -qtA "$@"; }

pass=0
fail=0
# Asserts a statement is REJECTED — a constraint doing its job.
rejects() {
  if [[ "$(psql -c "$2" 2>&1)" == *ERROR* ]]; then echo "  PASS  $1"; ((pass++))
  else echo "  FAIL  $1 -> accepted, but should not be"; ((fail++)); fi
}
# Asserts a statement is ACCEPTED.
accepts() {
  out=$(psql -c "$2" 2>&1)
  if [[ "$out" == *ERROR* ]]; then echo "  FAIL  $1 -> ${out:0:140}"; ((fail++))
  else echo "  PASS  $1"; ((pass++)); fi
}
value() { psql -c "$1" 2>&1 | head -1; }

H=019212e0-0000-7000-8000-000000000001
U=019212e0-0000-7000-8000-000000000002
U2=019212e0-0000-7000-8000-000000000003

echo "up"
if psql -v ON_ERROR_STOP=1 -f db/migrations/V0001__baseline.sql >/dev/null 2>&1; then
  echo "  PASS  V0001 applies clean"; ((pass++))
else
  echo "  FAIL  V0001 did not apply:"; psql -v ON_ERROR_STOP=1 -f db/migrations/V0001__baseline.sql 2>&1 | tail -5
  exit 1
fi

echo "households"
accepts "insert with defaults"          "INSERT INTO households (id,display_name) VALUES ('$H','Test Household');"
defaults=$(value "SELECT reporting_currency||' / '||period_start_day||' / '||day_starts_at||' / '||allowance_mode FROM households WHERE id='$H';")
if [ "$defaults" = "IDR / 1 / 04:00:00 / adaptive" ]; then
  echo "  PASS  defaults are PRD S3 ($defaults)"; ((pass++))
else
  echo "  FAIL  defaults are '$defaults', expected 'IDR / 1 / 04:00:00 / adaptive'"; ((fail++))
fi
rejects "period_start_day = 29"          "INSERT INTO households (id,display_name,period_start_day) VALUES (gen_random_uuid(),'x',29);"
accepts "period_start_day = 28"          "INSERT INTO households (id,display_name,period_start_day) VALUES (gen_random_uuid(),'x',28);"
rejects "reporting_currency 'idr'"       "INSERT INTO households (id,display_name,reporting_currency) VALUES (gen_random_uuid(),'x','idr');"
rejects "negative expected income"       "INSERT INTO households (id,display_name,expected_monthly_income) VALUES (gen_random_uuid(),'x',-1);"
accepts "zero expected income"           "INSERT INTO households (id,display_name,expected_monthly_income) VALUES (gen_random_uuid(),'x',0);"
rejects "blank display_name"             "INSERT INTO households (id,display_name) VALUES (gen_random_uuid(),'   ');"
rejects "unknown allowance_mode"         "INSERT INTO households (id,display_name,allowance_mode) VALUES (gen_random_uuid(),'x','strict');"

echo "users"
accepts "insert with defaults"           "INSERT INTO users (id,household_id,email,display_name) VALUES ('$U','$H','a@example.com','A');"
udefaults=$(value "SELECT locale||' / '||time_zone FROM users WHERE id='$U';")
if [ "$udefaults" = "en-GB / Asia/Jakarta" ]; then
  echo "  PASS  defaults are PRD S4 ($udefaults)"; ((pass++))
else
  echo "  FAIL  defaults are '$udefaults', expected 'en-GB / Asia/Jakarta'"; ((fail++))
fi
rejects "unknown locale"                 "INSERT INTO users (id,household_id,email,display_name,locale) VALUES (gen_random_uuid(),'$H','q@e.c','x','fr-FR');"
rejects "duplicate email, other case"    "INSERT INTO users (id,household_id,email,display_name) VALUES (gen_random_uuid(),'$H','A@EXAMPLE.COM','dup');"
rejects "household must exist"           "INSERT INTO users (id,household_id,email,display_name) VALUES (gen_random_uuid(),gen_random_uuid(),'z@e.c','x');"
accepts "second user"                    "INSERT INTO users (id,household_id,email,display_name) VALUES ('$U2','$H','b@example.com','B');"
accepts "soft-delete both"               "UPDATE users SET deleted_at=now() WHERE id IN ('$U','$U2');"
accepts "two soft-deleted share email"   "UPDATE users SET email='same@example.com' WHERE id IN ('$U','$U2');"
accepts "live user reuses that email"    "INSERT INTO users (id,household_id,email,display_name) VALUES (gen_random_uuid(),'$H','same@example.com','live');"
accepts "undelete with a free address"   "UPDATE users SET deleted_at=NULL, email='a@example.com' WHERE id='$U';"

echo "auth state"
accepts "credential"                     "INSERT INTO credentials (user_id,password_hash) VALUES ('$U','\$argon2id\$v=19\$m=19456,t=2,p=1\$c2FsdA\$aGFzaA');"
rejects "two credentials for one user"   "INSERT INTO credentials (user_id,password_hash) VALUES ('$U','x');"
accepts "session"                        "INSERT INTO sessions (id,user_id,expires_at) VALUES (repeat('a',64),'$U',now()+interval '30 days');"
accepts "invitation"                     "INSERT INTO invitations (id,household_id,invited_email,token_hash,created_by,expires_at) VALUES (gen_random_uuid(),'$H','s@e.c',repeat('b',64),'$U',now()+interval '72 hours');"
rejects "duplicate token_hash"           "INSERT INTO invitations (id,household_id,invited_email,token_hash,created_by,expires_at) VALUES (gen_random_uuid(),'$H','o@e.c',repeat('b',64),'$U',now());"
accepts "login_attempts row"             "INSERT INTO login_attempts (key,failure_count,backoff_until) VALUES ('ip:203.0.113.7',3,now()+interval '4 seconds');"
rejects "negative failure_count"         "INSERT INTO login_attempts (key,failure_count) VALUES ('email:x@y.z',-1);"

echo "delete behaviour"
# Users are soft-deleted in normal operation, so a hard DELETE is always
# deliberate (test teardown, or an erasure path if Q-14 is ever answered).
# Instance-local auth state must never be what blocks one.
accepts "hard-delete a user"             "DELETE FROM users WHERE id='$U';"
remaining=$(value "SELECT (SELECT count(*) FROM credentials)||'/'||(SELECT count(*) FROM sessions)||'/'||(SELECT count(*) FROM invitations);")
if [ "$remaining" = "0/0/0" ]; then
  echo "  PASS  credentials, sessions, invitations all cascaded"; ((pass++))
else
  echo "  FAIL  cascade left credentials/sessions/invitations = $remaining, expected 0/0/0"; ((fail++))
fi
rejects "household delete, users live"   "DELETE FROM households WHERE id='$H';"

echo "down"
if psql -v ON_ERROR_STOP=1 -f db/undo/U0001__baseline.sql >/dev/null 2>&1; then
  echo "  PASS  U0001 applies clean"; ((pass++))
else
  echo "  FAIL  U0001 did not apply"; ((fail++))
fi
left=$(value "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")
if [ "$left" = "0" ]; then
  echo "  PASS  undo left no tables behind"; ((pass++))
else
  echo "  FAIL  undo left $left table(s) behind"; ((fail++))
fi

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
