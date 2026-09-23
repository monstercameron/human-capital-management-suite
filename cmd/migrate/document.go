package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/pressly/goose/v3"
)

const (
	EnvDocumentDatabaseURL   = "HCMNEXT_DOCUMENT_DATABASE_URL"
	fieldDocumentDatabaseURL = "document-database-url"
	documentCommandPrefix    = "document "
)

var ErrDocumentSharesDatabase = errors.New("document database must be independent from core and chat databases")

func documentSubcommand(command string) string {
	if !strings.HasPrefix(command, documentCommandPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(command, documentCommandPrefix))
}

func validateDocumentCommand(action, documentURL, coreURL, chatURL string) error {
	switch action {
	case "up", "status":
	case "":
		return errors.New("usage: migrate document up|status")
	default:
		return fmt.Errorf("unknown document command %q; usage: migrate document up|status", action)
	}
	if strings.TrimSpace(documentURL) == "" {
		return fmt.Errorf("%s is not set; pass -%s or set the environment variable", EnvDocumentDatabaseURL, fieldDocumentDatabaseURL)
	}
	if strings.TrimSpace(coreURL) == "" {
		return fmt.Errorf("%s is not set; document isolation requires -database-url or the environment variable", EnvDatabaseURL)
	}
	if sameTargetDatabase(documentURL, coreURL) || sameTargetDatabase(documentURL, chatURL) {
		return ErrDocumentSharesDatabase
	}
	return nil
}

func openDocumentMigrateDB(ctx context.Context, url string) (*sql.DB, error) {
	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", EnvDocumentDatabaseURL, err)
	}
	db := stdlib.OpenDB(*connCfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect document database: %w", err)
	}
	return db, nil
}

func runDocumentMigrateCommand(ctx context.Context, action string, db *sql.DB, out io.Writer) error {
	migrationFS, err := fs.Sub(documenthubstore.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("read document migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS,
		goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("build document migration provider: %w", err)
	}
	switch action {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("apply document migrations: %w", err)
		}
		for _, r := range results {
			fmt.Fprintf(out, "applied %s\n", r.Source.Path)
		}
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("read document migration status: %w", err)
		}
		for _, status := range statuses {
			applied := "-"
			if !status.AppliedAt.IsZero() {
				applied = status.AppliedAt.UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(out, "%-9s %-32s %s\n", status.State, status.Source.Path, applied)
		}
	default:
		return fmt.Errorf("unknown document command %q; usage: migrate document up|status", action)
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read document schema version: %w", err)
	}
	sources := provider.ListSources()
	target := int64(0)
	if len(sources) > 0 {
		target = sources[len(sources)-1].Version
	}
	fmt.Fprintf(out, "document schema version %d of %d\n", current, target)
	return nil
}
