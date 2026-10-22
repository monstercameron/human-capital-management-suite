package agentstore

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
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

// MigrateDSN opens a short-lived database/sql connection for the isolated
// agent migration set. Request-serving code uses the restricted agent pool.
func MigrateDSN(ctx context.Context, dsn string) error {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("agentstore: parse migration DSN: %w", err)
	}
	db := stdlib.OpenDB(*config)
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("agentstore: migration connection: %w", err)
	}
	if err := Migrate(ctx, db); err != nil {
		return fmt.Errorf("agentstore: migrate: %w", err)
	}
	return nil
}

// EnsureDatabase creates the named database if it does not exist. Callers
// must authorize this operation before invoking it; PostgreSQL database
// creation runs outside a transaction and therefore requires a maintenance
// connection with elevated privileges.
func EnsureDatabase(ctx context.Context, dsn string) error {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("agentstore: parse local database DSN: %w", err)
	}
	databaseName := config.Database
	if strings.TrimSpace(databaseName) == "" {
		return fmt.Errorf("agentstore: local database DSN has no database name")
	}
	config.Database = "postgres"
	db := stdlib.OpenDB(*config)
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("agentstore: local maintenance connection: %w", err)
	}
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)", databaseName).Scan(&exists); err != nil {
		return fmt.Errorf("agentstore: check local database: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+(pgx.Identifier{databaseName}).Sanitize()); err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42P04" {
			return fmt.Errorf("agentstore: create local database: %w", err)
		}
	}
	return nil
}
