package documenthubstore

import "embed"

// Migrations is the independent document database migration set.
//
//go:embed migrations/*.sql
var Migrations embed.FS
