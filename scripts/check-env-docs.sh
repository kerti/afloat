#!/usr/bin/env bash
# Verify the backend reads exactly the environment variables BOOTSTRAP.md §12
# documents, with the documented defaults.
#
# §12 is what an operator configures from. Nothing about that promise is
# visible to the contract or to the backend's own tests - a renamed variable or
# a changed default is internally consistent - so it needs its own gate.
#
# The table in BOOTSTRAP.md §12 is the source of truth. This script reads it and
# compares it against backend/internal/config/config.go's `env:"NAME"` /
# `envDefault:"…"` tags.
#
# Usage: check-env-docs.sh

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

bootstrap=docs/BOOTSTRAP.md
go_config=backend/internal/config/config.go

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

# DATABASE_URL has no default; it is required. It still has to appear under its
# documented NAME.
default_exempt=" DATABASE_URL "

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

# --- the `d` suffix ----------------------------------------------------------
# Go's time.ParseDuration does not accept `30d`. A documented value spelled that
# way crashes the backend at boot, and it is the kind of thing a later tidy-up
# introduces while "simplifying" 2160h.
bad_durations=""
for file in "$bootstrap" .env.example "$go_config"; do
  [ -e "$file" ] || continue
  hits=$(grep -nE '(TTL|TIMEOUT|LIFETIME)[^A-Za-z0-9_]{0,4}[^|]*[^A-Za-z0-9]([0-9]+d)\b' "$file" || true)
  [ -n "$hits" ] && bad_durations+="$file: $hits"$'\n'
done
if [ -n "$bad_durations" ]; then
  report "duration suffixes" "a \`d\` suffix fails Go's time.ParseDuration:"$'\n'"$bad_durations"
else
  report "duration suffixes" ok
fi

exit $status
