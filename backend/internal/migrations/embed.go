// Package migrations embeds the goose migration files the binary applies to
// itself on boot (BOOTSTRAP.md §3).
//
// The .sql files here are GENERATED from db/migrations + db/undo by
// `make gen-goose-migrations` and must never be hand-edited; `make check`
// regenerates them into a scratch directory and diffs.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
