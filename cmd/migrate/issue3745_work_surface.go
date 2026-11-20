package main

// Issue 3745: the local work surface. A plain local run has no document or
// project database, so Documents and Projects say "Not set up for this
// workspace" and every document or ticket link in Chat is unavailable.
//
//	migrate work-surface-demo -profile local-dev \
//	    -database-url <core> -chat-database-url <chat> \
//	    -document-database-url <documents> -project-database-url <projects>
//
// brings both databases to the current schema, then prepares a small
// deterministic set of demo work in the demo workspace (tenant harborcare-demo
// by default): an open project and document, and a restricted project and
// document only the administrator and one named reviewer can open. It prints
// the addresses to paste into Chat, who sees what, and the server flags that
// serve it. Every database must be on a loopback host; a rerun changes
// nothing.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const workSurfaceDemoCommand = "work-surface-demo"

func workSurfaceDemoSpec(rest []string) bootstrap.Spec {
	return bootstrap.Spec{
		Role: bootstrap.RoleMigrate,
		Args: rest,
		ConfigFields: []bootstrap.Field{
			{Name: fieldAgentDemoProfile, Usage: "required local development profile"},
			{Name: fieldTenant, Usage: "tenant slug (harborcare-demo, the default, or ironridge-demo)", Default: application.LocalAgentDemoDefaultTenant()},
			{Name: "database-url", Env: EnvDatabaseURL, Usage: "core PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldChatDatabaseURL, Env: EnvChatDatabaseURL, Usage: "optional chat PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldDocumentDatabaseURL, Env: EnvDocumentDatabaseURL, Usage: "document PostgreSQL URL", Kind: bootstrap.KindString, Secret: true},
			{Name: fieldProjectDatabaseURL, Env: application.EnvProjectDatabaseURL, Usage: "project PostgreSQL URL (a restricted role, not the core role)", Kind: bootstrap.KindString, Secret: true},
		},
		Validate: func(values *bootstrap.Values) error {
			return application.ValidateLocalWorkSurfaceConfig(workSurfaceConfig(values, nil))
		},
		Build: func(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			config := workSurfaceConfig(deps.Values, nil)
			work := bootstrap.Workload{Name: "migrate-work-surface-demo", Run: func(ctx context.Context) error {
				ctx, cancel := context.WithTimeout(ctx, migrationTimeout)
				defer cancel()
				return runWorkSurfaceDemoCommand(ctx, config, os.Stdout)
			}}
			return bootstrap.Runtime{Workloads: []bootstrap.Workload{work}}, nil
		},
	}
}

func workSurfaceConfig(values *bootstrap.Values, people []application.WorkSurfacePerson) application.LocalWorkSurfaceConfig {
	return application.LocalWorkSurfaceConfig{
		Profile: values.String(fieldAgentDemoProfile), Tenant: values.String(fieldTenant),
		DatabaseURL: values.String("database-url"), ChatDatabaseURL: values.String(fieldChatDatabaseURL),
		DocumentDatabaseURL: values.String(fieldDocumentDatabaseURL), ProjectDatabaseURL: values.String(fieldProjectDatabaseURL),
		People: people,
	}
}

func runWorkSurfaceDemoCommand(ctx context.Context, config application.LocalWorkSurfaceConfig, out io.Writer) error {
	if err := application.ValidateLocalWorkSurfaceConfig(config); err != nil {
		return err
	}
	// Both schemas must exist before anything is written into them.
	documentDB, err := openDocumentMigrateDB(ctx, config.DocumentDatabaseURL)
	if err != nil {
		return err
	}
	err = runDocumentMigrateCommand(ctx, "up", documentDB, io.Discard)
	_ = documentDB.Close()
	if err != nil {
		return err
	}
	if err := migrateWorkSurfaceProject(ctx, config); err != nil {
		return err
	}
	conn, err := openSeedDB(ctx, config.DatabaseURL)
	if err != nil {
		return err
	}
	workers, err := loadSeedPeople(ctx, conn, config.Tenant)
	_ = conn.Close(ctx)
	if err != nil {
		return err
	}
	for _, w := range workers {
		config.People = append(config.People, application.WorkSurfacePerson{Key: w.Key, Name: w.Name})
	}
	receipt, err := application.PrepareLocalWorkSurface(ctx, config)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(out, application.LocalWorkSurfaceGuide(receipt))
	return err
}

// migrateWorkSurfaceProject brings the project schema to the current version
// the way the server's -migrate does: the store first validates the restricted
// role, then the project-owned migrations run in the project schema.
func migrateWorkSurfaceProject(ctx context.Context, config application.LocalWorkSurfaceConfig) error {
	store, err := projectstore.New(ctx, projectstore.Config{DSN: config.ProjectDatabaseURL, CoreDSN: config.DatabaseURL, Schema: application.ProjectSchemaName, MaxConns: 4, MinConns: 1})
	if err != nil {
		return fmt.Errorf("validate project database: %w", err)
	}
	store.Close()
	connCfg, err := pgx.ParseConfig(config.ProjectDatabaseURL)
	if err != nil {
		return fmt.Errorf("parse project database URL: %w", err)
	}
	connCfg.RuntimeParams["search_path"] = application.ProjectSchemaName
	var db *sql.DB = stdlib.OpenDB(*connCfg)
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect project database: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+pgx.Identifier{application.ProjectSchemaName}.Sanitize()); err != nil {
		return fmt.Errorf("provision project schema: %w", err)
	}
	migrationFS, err := fs.Sub(projectstore.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("read project migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("build project migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply project migrations: %w", err)
	}
	return nil
}
