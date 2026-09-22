#!/usr/bin/env bash
# Copy the canonical common-password denylist into both backends.
#
# shared/common_passwords.txt is canonical and owned by neither backend: a
# household that switches backends must have the same passwords rejected, and a
# denylist that drifts is a divergence no contract test can see.
#
# It is copied rather than read in place because neither backend can reach it
# where it lives. Go's //go:embed cannot reference a parent directory, so the
# bytes have to sit inside package auth; the Kotlin backend has to carry the
# file inside the jar for the same reason the migrations are copied — a
# filesystem path that works from a test working directory breaks the moment
# the application runs from a jar or a container.
#
# Both copies are generated output. Never hand-edit them; edit
# shared/common_passwords.txt and re-run this. `make check` regenerates into a
# scratch tree and diffs, so a hand-edit is reported rather than silently
# repaired.
#
# Usage: sync-denylist.sh [destination root]
#   destination root defaults to the repo, i.e. the committed copies.

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

src=shared/common_passwords.txt
dest_root=${1:-.}

# Relative to the destination root, so `make check` can point the whole thing
# at a scratch directory and diff the result.
go_dest="$dest_root/backend/internal/auth/common_passwords.txt"
kotlin_dest="$dest_root/backend-kotlin/src/main/resources/common_passwords.txt"

if [ ! -f "$src" ]; then
  echo "sync-denylist: $src does not exist" >&2
  exit 1
fi

copies=0
for dest in "$go_dest" "$kotlin_dest"; do
  mkdir -p "$(dirname "$dest")"
  # Not cp -p: copying the mtime would make the diff in `make check` depend on
  # checkout order. Content is what has to match.
  cp "$src" "$dest"
  copies=$((copies + 1))
done

echo "ok $copies cop(ies) of $src"
