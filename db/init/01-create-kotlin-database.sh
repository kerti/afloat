#!/usr/bin/env bash
# Create the second database. POSTGRES_DB creates only one, and the Kotlin
# backend needs its own: one instance, two databases, never one shared schema
# (BOOTSTRAP.md §3).
#
# docker-entrypoint-initdb.d runs this exactly once, when the data directory is
# empty. It will NOT re-run against an existing volume — if afloat_kotlin is
# missing on an existing instance, `make db-reset` is the fix.

set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
	CREATE DATABASE afloat_kotlin OWNER $POSTGRES_USER;
EOSQL

echo "init: created database afloat_kotlin"
