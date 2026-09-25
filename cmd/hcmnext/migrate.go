// migrate.go supplies this command's adapter for internal/application's
// Migrator port.
//
// It lives in cmd rather than in the application composition root on purpose:
// definitions/architecture/library-firewall.yaml (LIB-008) confines
// github.com/pressly/goose/v3 to the migrations and cmd roots, so the
// application root states the dependency ("bring the schema to the target
// version before the listeners start") and this command supplies the one
// implementation of it.

package main

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workorderstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// migrateUp applies every pending migration with Goose over the embedded tree.
//
// It is deliberately the plain apply. cmd/migrate owns the journaled one
// (DB-006: artifact digest, tool version, per-migration checksum verification,
// owner, start and finish), and duplicating that here would create a second
// migration authority - exactly the "competing migration roots" NEXT-004 names
// as a failure.
func migrateUp(ctx context.Context, url string, logger bootstrap.Logger) error {
	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse the database URL: %w", err)
	}
	db := stdlib.OpenDB(*connCfg)
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(
		goose.DialectPostgres, db, migrations.FS,
		goose.WithVerbose(false),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("build the migration provider: %w", err)
	}
	applied, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	target, err := migrations.TargetVersion()
	if err != nil {
		return err
	}
	logger.Info("hcmnext.schema_applied", "version", target, "applied_this_start", len(applied))
	return nil
}

// migrateProjectUp applies the project-owned migration tree using its
// restricted database role and isolated schema. The project store performs
// the same-credential and pool validation used by serving before Goose runs.
func migrateProjectUp(ctx context.Context, url, coreURL, schema string, logger bootstrap.Logger) error {
	store, err := projectstore.New(ctx, projectstore.Config{DSN: url, CoreDSN: coreURL, Schema: schema, MaxConns: 8, MinConns: 1})
	if err != nil {
		return fmt.Errorf("validate project database: %w", err)
	}
	store.Close()

	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse project database URL: %w", err)
	}
	connCfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*connCfg)
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect project database: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS hcmnext_project`); err != nil {
		return fmt.Errorf("provision project schema: %w", err)
	}
	migrationFS, err := fs.Sub(projectstore.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("read project migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS,
		goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("build project migration provider: %w", err)
	}
	applied, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply project migrations: %w", err)
	}
	logger.Info("hcmnext.project_schema_applied", "schema", schema, "applied_this_start", len(applied))
	return nil
}

// migrateWorkOrderUp applies the work-order-owned migration tree in its
// isolated schema. The work-order capability owns a separate schema and
// migration stream so its operational records do not become part of the HCM
// employee-record schema.
func migrateWorkOrderUp(ctx context.Context, url, coreURL, schema string, logger bootstrap.Logger) error {
	store, err := workorderstore.New(ctx, workorderstore.Config{DSN: url, CoreDSN: coreURL, Schema: schema})
	if err != nil {
		return fmt.Errorf("validate work order database: %w", err)
	}
	store.Close()

	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse work order database URL: %w", err)
	}
	connCfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*connCfg)
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect work order database: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+pgx.Identifier{schema}.Sanitize()); err != nil {
		return fmt.Errorf("provision work order schema: %w", err)
	}
	migrationFS, err := fs.Sub(workorderstore.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("read work order migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS,
		goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("build work order migration provider: %w", err)
	}
	applied, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply work order migrations: %w", err)
	}
	logger.Info("hcmnext.work_order_schema_applied", "schema", schema, "applied_this_start", len(applied))
	return nil
}
