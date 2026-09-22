package chatstore

import "embed"

// Migrations is the independent chat database migration set.
//
//go:embed migrations/*.sql
var Migrations embed.FS
