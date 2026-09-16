#!/usr/bin/env bash
# Run BOTH migration runners against their own database and prove they produce
# the same schema (BOOTSTRAP.md §11 step 4: "both migration paths green").
#
#   Flyway -> afloat_kotlin, reading db/migrations/ directly
#   goose  -> afloat_go,     reading the generated backend/internal/migrations/
#
# Runs the runners themselves rather than psql, which is the point:
# scripts/test-migrations.sh already proves the SQL is correct, and cannot catch
# a generator that drops a statement or a runner that disagrees about what has
# been applied. Neither backend has to exist — Flyway runs from its official
# image and goose via `go run`, so this is meaningful before §11 steps 5-6.
#
# It also asserts the in-place-edit asymmetry §7.1 warns about, because that
# warning is load-bearing and was written from goose's schema rather than from
# observed behaviour.
#
# Usage: test-migration-runners.sh   (expects `make db-up` to have run)

set -uo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

PORT=${AFLOAT_DB_PORT:-55432}
USER=${AFLOAT_DB_USER:-afloat}
PASSWORD=${AFLOAT_DB_PASSWORD:-afloat}
export PGPASSWORD=$PASSWORD

command -v psql >/dev/null 2>&1 || PATH="/opt/homebrew/opt/libpq/bin:$PATH"
for tool in docker go psql; do
  command -v "$tool" >/dev/null 2>&1 || { echo "test-migration-runners: $tool not found" >&2; exit 1; }
done
pg_isready -h localhost -p "$PORT" -q || { echo "test-migration-runners: nothing on port $PORT — run 'make db-up'" >&2; exit 1; }

pass=0
fail=0
ok()   { echo "  PASS  $1"; ((pass++)); }
bad()  { echo "  FAIL  $1"; ((fail++)); }
q()    { psql -h localhost -p "$PORT" -U "$USER" -d "$1" -qtA -c "$2" 2>&1; }

flyway() {
  docker run --rm --network host -v "$repo_root/db/migrations:/flyway/sql:ro" flyway/flyway:latest \
    -url="jdbc:postgresql://localhost:$PORT/afloat_kotlin" -user="$USER" -password="$PASSWORD" \
    -connectRetries=5 "$@" 2>&1
}
goose() {
  go run github.com/pressly/goose/v3/cmd/goose@latest -dir backend/internal/migrations \
    postgres "host=localhost port=$PORT user=$USER password=$PASSWORD dbname=afloat_go sslmode=disable" "$@" 2>&1
}

# Schema fingerprint: every column of every table that is not a runner's own
# ledger, plus constraints and indexes. This is what "the same schema" means;
# the ledgers are expected to differ and are excluded by name.
fingerprint() {
  q "$1" "
    SELECT string_agg(line, E'\n' ORDER BY line) FROM (
      SELECT 'col '||table_name||'.'||column_name||' '||data_type||' '||is_nullable||' '||
             coalesce(column_default,'-') AS line
      FROM information_schema.columns
      WHERE table_schema='public' AND table_name NOT IN ('goose_db_version','flyway_schema_history')
      UNION ALL
      SELECT 'con '||conrelid::regclass||' '||contype::text||' '||pg_get_constraintdef(oid)
      FROM pg_constraint WHERE connamespace='public'::regnamespace
        AND conrelid::regclass::text NOT IN ('goose_db_version','flyway_schema_history')
      UNION ALL
      SELECT 'idx '||indexdef FROM pg_indexes
      WHERE schemaname='public' AND tablename NOT IN ('goose_db_version','flyway_schema_history')
    ) s;"
}

echo "reset"
q afloat_go     "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" >/dev/null
q afloat_kotlin "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" >/dev/null
echo "  both databases emptied"

echo "flyway -> afloat_kotlin"
flyway_out=$(flyway migrate)
if grep -q "Successfully applied 1 migration" <<<"$flyway_out"; then ok "applied db/migrations"; else bad "flyway migrate did not apply:"; tail -5 <<<"$flyway_out"; fi

echo "goose -> afloat_go"
goose_out=$(goose up)
if grep -q "successfully migrated database to version: 1" <<<"$goose_out"; then ok "applied backend/internal/migrations"; else bad "goose up did not apply:"; tail -5 <<<"$goose_out"; fi

echo "parity"
go_fp=$(fingerprint afloat_go)
kt_fp=$(fingerprint afloat_kotlin)
go_n=$(grep -c . <<<"$go_fp")

# Guard before comparing. psql writes errors to the same stream, so a query that
# fails returns identical text from both databases and would otherwise compare
# equal — a broken fingerprint must never read as parity. The floor is a sanity
# bound, not an exact count: the baseline is ~109 objects.
if grep -q '^ERROR:' <<<"$go_fp$kt_fp" || [ "$go_n" -lt 50 ]; then
  bad "fingerprint query did not return a schema ($go_n lines):"
  head -3 <<<"$go_fp"
elif [ "$go_fp" = "$kt_fp" ]; then
  ok "both databases have an identical schema ($go_n objects)"
else
  bad "schemas differ:"
  diff <(echo "$go_fp") <(echo "$kt_fp") | head -20
fi

# ---------------------------------------------------------------------------
# The §7.1 asymmetry. Edit the canonical migration in place, regenerate, and
# re-run both runners against databases that already have version 1 applied.
# Flyway must refuse; goose must silently do nothing. Restored afterwards.
# ---------------------------------------------------------------------------
echo "in-place edit (BOOTSTRAP.md §7.1)"
backup=$(mktemp)
cp db/migrations/V0001__baseline.sql "$backup"
restore() { cp "$backup" db/migrations/V0001__baseline.sql; rm -f "$backup"; ./scripts/gen-goose-migrations.sh >/dev/null; }
trap restore EXIT

printf '\nALTER TABLE households ADD COLUMN parity_probe text;\n' >>db/migrations/V0001__baseline.sql
./scripts/gen-goose-migrations.sh >/dev/null

# Every runner invocation below is captured before matching, never piped:
# Flyway exits non-zero here by design, and under pipefail a pipe would report
# that failure instead of whether the text matched.
edited_out=$(flyway migrate)
if grep -qi "checksum mismatch" <<<"$edited_out"; then ok "flyway refuses the edited migration (checksum mismatch)"; else bad "flyway did NOT detect the in-place edit:"; tail -5 <<<"$edited_out"; fi

goose_out=$(goose up)
if grep -q "no migrations to run" <<<"$goose_out"; then ok "goose reports nothing to do"; else bad "goose did not report 'no migrations to run': $(tail -2 <<<"$goose_out")"; fi

probe=$(q afloat_go "SELECT count(*) FROM information_schema.columns WHERE table_name='households' AND column_name='parity_probe';")
if [ "$probe" = "0" ]; then
  ok "goose left afloat_go on the OLD schema, silently — §7.1 confirmed"
else
  bad "goose applied the edit; §7.1's claim is wrong and the doc needs correcting"
fi

restore
trap - EXIT

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
