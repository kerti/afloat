#!/usr/bin/env bash
# Copy the canonical Flyway migrations into the Kotlin backend's resources.
#
# db/migrations/V####__name.sql is canonical and Flyway-native, so unlike the
# goose files (scripts/gen-goose-migrations.sh) nothing has to be transformed —
# this is a copy, not a generator. It exists because the Kotlin backend has to
# carry its migrations INSIDE the jar: pointing Flyway at ../db/migrations with
# a filesystem: location works in tests and breaks the moment the application
# runs from a jar or a container, which is the worst time to find out.
#
# The copy is generated output. Never hand-edit it; edit db/migrations and
# re-run this. `make check` regenerates into a scratch directory and diffs, so
# a hand-edit is reported rather than silently repaired.
#
# Usage: sync-kotlin-migrations.sh [destination]
#   destination defaults to the committed copy in the Kotlin resources tree.

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

src=db/migrations
dest=${1:-backend-kotlin/src/main/resources/db/migration}

if [ ! -d "$src" ]; then
  echo "sync-kotlin-migrations: $src does not exist" >&2
  exit 1
fi

shopt -s nullglob
migrations=("$src"/V*.sql)
if [ ${#migrations[@]} -eq 0 ]; then
  echo "sync-kotlin-migrations: no V*.sql in $src" >&2
  exit 1
fi

mkdir -p "$dest"

# Remove any V*.sql the source no longer has, so a deleted or renamed migration
# does not linger in the copy and get applied by a backend nobody expects it in.
for stale in "$dest"/V*.sql; do
  if [ ! -e "$src/$(basename "$stale")" ]; then
    rm -f "$stale"
  fi
done

for migration in "${migrations[@]}"; do
  # cp -p would copy the mtime and make the diff in `make check` depend on
  # checkout order. Content is what has to match.
  cp "$migration" "$dest/$(basename "$migration")"
done

# The undo files in db/undo/ are deliberately NOT copied. They are a goose
# source (BOOTSTRAP.md §7) for the Go backend's embedded Down migrations;
# Flyway's undo is a paid feature and the Kotlin backend never runs one.

echo "ok $dest (${#migrations[@]} migration(s) from $src)"
