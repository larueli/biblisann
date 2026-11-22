// Package migrations embeds the goose SQL migrations.
//
// Migrations are forward-only and must be safe for a rolling upgrade: expand in one release,
// contract in a later one. There are no Down sections.
package migrations

import "embed"

// FS holds the *.sql files at its root.
//
//go:embed *.sql
var FS embed.FS
