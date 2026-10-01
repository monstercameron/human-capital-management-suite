package main

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func migrateTimeUp(ctx context.Context, databaseURL, coreURL, schema string, logger bootstrap.Logger) error {
	store, err := timestore.New(ctx, timestore.Config{DSN: databaseURL, CoreDSN: coreURL, Schema: schema, MaxConns: 8, MinConns: 1})
	if err != nil {
		return fmt.Errorf("validate time database: %w", err)
	}
	store.Close()
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse time database URL: %w", err)
	}
	config.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*config)
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{schema}.Sanitize()); err != nil {
		return fmt.Errorf("provision time schema: %w", err)
	}
	tree, err := fs.Sub(timestore.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("read time migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, tree, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("construct time migration provider: %w", err)
	}
	applied, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply time migrations: %w", err)
	}
	logger.Info("hcmnext.time_schema_applied", "schema", schema, "applied_this_start", len(applied))
	return nil
}
