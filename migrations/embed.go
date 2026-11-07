// Package migrations embeds the goose SQL migrations.
package migrations

import "embed"

// FS holds every *.sql migration. Never edit an applied migration; add a new one.
//
//go:embed *.sql
var FS embed.FS
