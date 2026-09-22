#!/usr/bin/env bash
# Verify both backends read the same environment variable names, with the same
# defaults, as documented in BOOTSTRAP.md §12.
#
# §12 is a NAMING CONTRACT, not a shared artefact: an operator who has
# configured one backend has configured the other. Nothing about that promise is
# visible to contract conformance — both backends can be internally consistent
# and still read different names — so it needs its own gate, for the same reason
# BOOTSTRAP.md §6 gives the trajectory fixture one.
#
# The table in BOOTSTRAP.md §12 is the source of truth. This script reads it and
# compares it against:
#
#   backend/internal/config/config.go           `env:"NAME"` / `envDefault:"…"`
#   backend-kotlin/src/main/resources/          `${NAME:default}` placeholders
#     application.y{a,}ml
#
# A surface that does not exist yet is skipped, so this is safe to wire into
# `make check` before the Kotlin backend is scaffolded.
#
# Usage: check-env-parity.sh

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

bootstrap=docs/BOOTSTRAP.md
go_config=backend/internal/config/config.go
kotlin_config=""
for candidate in backend-kotlin/src/main/resources/application.yml \
                 backend-kotlin/src/main/resources/application.yaml; do
  [ -e "$candidate" ] && kotlin_config=$candidate && break
done

status=0
report() {
  if [ "$2" = ok ]; then
    printf '  ok    %s\n' "$1"
  elif [ "$2" = skip ]; then
    printf '  -     %s (not scaffolded yet)\n' "$1"
  else
    printf '  FAIL  %s - %s\n' "$1" "$2"
    status=1
  fi
}

# PORT differs by backend on purpose (BOOTSTRAP.md §1: the two must differ, or
# parity work is impossible). DATABASE_URL has no default; it is required, and
# the Kotlin side translates libpq to JDBC rather than taking another name.
# Both still have to appear under the same NAME in both backends.
default_exempt=" PORT DATABASE_URL "

# --- the documented contract -------------------------------------------------
# Rows of the §12 "Backend configuration" table: | `NAME` | `default` | notes |
documented=$(awk '
  /^### Backend configuration$/     { in_table = 1; next }
  in_table && /^### /               { in_table = 0 }
  in_table && /^\| `/               { print }
' "$bootstrap" | sed 's/^| `\([A-Z0-9_]*\)`.*/\1/')

if [ -z "$documented" ]; then
  report "$bootstrap §12 table" "no variables found - has the table moved or been renamed?"
  exit 1
fi

documented_default_for() {
  awk -v want="$1" '
    /^### Backend configuration$/ { in_table = 1; next }
    in_table && /^### /           { in_table = 0 }
    in_table && $0 ~ "^\\| `" want "` \\|" {
      # second cell, backticks stripped; empty when it is prose like *(required)*
      split($0, cells, "|")
      gsub(/[` ]/, "", cells[3])
      print cells[3]
    }
  ' "$bootstrap"
}

# --- Go ----------------------------------------------------------------------
if [ ! -e "$go_config" ]; then
  report "go env names" skip
else
  go_names=$(grep -o 'env:"[A-Z0-9_]*' "$go_config" | cut -d'"' -f2 | sort -u)

  missing=$(comm -23 <(printf '%s\n' "$documented" | sort -u) <(printf '%s\n' "$go_names"))
  extra=$(comm -13 <(printf '%s\n' "$documented" | sort -u) <(printf '%s\n' "$go_names"))

  if [ -n "$missing" ]; then
    report "go env names" "documented in §12 but not read by Go: $(echo "$missing" | tr '\n' ' ')"
  elif [ -n "$extra" ]; then
    report "go env names" "read by Go but undocumented in §12: $(echo "$extra" | tr '\n' ' ')"
  else
    report "go env names" ok
  fi

  for name in $go_names; do
    case "$default_exempt" in *" $name "*) continue ;; esac
    want=$(documented_default_for "$name")
    # An empty cell in §12 means the row documents no default, so there is
    # nothing to hold the backend against. A NON-empty cell must be matched.
    [ -z "$want" ] && continue
    got=$(grep -o "env:\"$name[^\"]*\"[^\`]*envDefault:\"[^\"]*\"" "$go_config" \
          | grep -o 'envDefault:"[^"]*"' | cut -d'"' -f2 || true)
    if [ -z "$got" ]; then
      # Previously skipped, which made a dropped envDefault invisible: §12 would
      # promise a default the backend does not have, and the gate stayed green.
      report "go default $name" "§12 documents $want, Go declares no envDefault"
    elif [ "$got" != "$want" ]; then
      report "go default $name" "§12 documents $want, Go defaults to $got"
    fi
  done
fi

# --- Kotlin ------------------------------------------------------------------
if [ -z "$kotlin_config" ]; then
  report "kotlin env names" skip
else
  kotlin_names=$(grep -o '\${[A-Z0-9_]*' "$kotlin_config" | cut -d'{' -f2 | sort -u)

  missing=$(comm -23 <(printf '%s\n' "$documented" | sort -u) <(printf '%s\n' "$kotlin_names"))
  extra=$(comm -13 <(printf '%s\n' "$documented" | sort -u) <(printf '%s\n' "$kotlin_names"))

  if [ -n "$missing" ]; then
    report "kotlin env names" "documented in §12 but not read by Kotlin: $(echo "$missing" | tr '\n' ' ')"
  elif [ -n "$extra" ]; then
    report "kotlin env names" "read by Kotlin but undocumented in §12: $(echo "$extra" | tr '\n' ' ')"
  else
    report "kotlin env names" ok
  fi

  for name in $kotlin_names; do
    case "$default_exempt" in *" $name "*) continue ;; esac
    want=$(documented_default_for "$name")
    [ -z "$want" ] && continue

    # EVERY occurrence, not the first. Several names are relayed into more than
    # one key — LOG_LEVEL, SHUTDOWN_TIMEOUT and the two HTTP timeouts each
    # appear twice — and the placeholder Boot actually binds for the server
    # knobs is the second one. Comparing only `head -1` let a second, divergent
    # default sit there unread.
    # `|| true` on the pipeline, not decoration: a placeholder with no default
    # matches nothing, and under `set -eo pipefail` grep's exit 1 would kill the
    # script mid-report — an exit code with no FAIL line, which is worse than
    # the hole it replaced.
    got=$(grep -o "\${$name:[^}]*}" "$kotlin_config" \
          | sed "s/^\${$name://; s/}$//" | sort -u || true)
    count=$(printf '%s\n' "$got" | grep -c . || true)

    if [ "$count" -eq 0 ]; then
      # The name is bound (it is in kotlin_names) but with no `:default`, so an
      # operator who sets nothing gets an unresolved placeholder, not the
      # documented value.
      report "kotlin default $name" "§12 documents $want, the placeholder carries no default"
    elif [ "$count" -gt 1 ]; then
      report "kotlin default $name" \
        "placeholders disagree: $(echo "$got" | tr '\n' ' ')- §12 documents $want"
    elif [ "$got" != "$want" ]; then
      report "kotlin default $name" "§12 documents $want, application.yml defaults to $got"
    fi
  done
fi

# --- the `d` suffix ----------------------------------------------------------
# Spring's simple duration style accepts `30d`; Go's time.ParseDuration does
# not. A value spelled that way boots one backend and crashes the other, which
# is precisely the failure §12 exists to prevent — and it is the kind of thing
# a later tidy-up introduces while "simplifying" 2160h.
bad_durations=""
for file in "$bootstrap" .env.example "$go_config" ${kotlin_config:-}; do
  [ -e "$file" ] || continue
  hits=$(grep -nE '(TTL|TIMEOUT|LIFETIME)[^A-Za-z0-9_]{0,4}[^|]*[^A-Za-z0-9]([0-9]+d)\b' "$file" || true)
  [ -n "$hits" ] && bad_durations+="$file: $hits"$'\n'
done
if [ -n "$bad_durations" ]; then
  report "duration suffixes" "a \`d\` suffix parses in Spring and fails in Go:"$'\n'"$bad_durations"
else
  report "duration suffixes" ok
fi

exit $status
