// Package projectstore owns project persistence in its own PostgreSQL schema.
package projectstore

import "embed"

// Migrations is the project-owned migration tree.
//
//go:embed migrations/*.sql
var Migrations embed.FS
