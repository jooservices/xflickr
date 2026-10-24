// Package migrations exposes the SQL migrations embedded into the xflickr binary.
package migrations

import "embed"

// Files contains the versioned goose migrations.
//
//go:embed *.sql
var Files embed.FS
