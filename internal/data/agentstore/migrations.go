package agentstore

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	iconmigration "github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore/migrations"
	"github.com/pressly/goose/v3"
)

// Migrations is the isolated agent database migration set. It is not included
// in the core migration tree.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Migrate applies all embedded agent database migrations to db. db must be
// opened with the agent migration identity and the intended schema on its
// search_path; request-serving code uses the restricted agent pool instead.
func Migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("agentstore: migration database is required")
	}
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("agentstore: read migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS,
		goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("agentstore: create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("agentstore: apply migrations: %w", err)
	}
	return iconmigration.Migrate(ctx, db)
}
