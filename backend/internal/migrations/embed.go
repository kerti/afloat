// Package migrations embeds the goose migration files the binary applies to
// itself on boot (BOOTSTRAP.md §3).
//
// The .sql files here are the canonical, hand-written migrations, Up and Down
// in one file each (BOOTSTRAP.md §7).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
