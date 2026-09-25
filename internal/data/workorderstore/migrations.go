// Package workorderstore owns durable work order persistence.
package workorderstore

import "embed"

// Migrations is the work-order-owned migration tree.
//
//go:embed migrations/*.sql
var Migrations embed.FS
