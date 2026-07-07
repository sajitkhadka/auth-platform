// Package migrations embeds the broker's SQL migration files so they can be
// applied at startup without shipping them alongside the binary.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
