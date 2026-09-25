package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/monstercameron/human-capital-management-suite/internal/application/documentembed"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatroutestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
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
	case "up", "status", "seed", "embed", "prune":
	case "":
		return errors.New("usage: migrate document up|status|seed|embed|prune")
	default:
		return fmt.Errorf("unknown document command %q; usage: migrate document up|status|seed|embed|prune", action)
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
		return fmt.Errorf("unknown document command %q; usage: migrate document up|status|seed|embed", action)
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

// runDocumentSeedAction opens the document store and the core persona
// read, then runs the document demo seed. The tenant defaults to the demo
// tenant the dev cell serves.
func runDocumentSeedAction(ctx context.Context, documentURL, coreURL, chatURL, tenant string, out io.Writer) error {
	if strings.TrimSpace(tenant) == "" {
		tenant = defaultDocumentSeedTenant
	}
	store, err := documenthubstore.New(ctx, documenthubstore.Config{DSN: documentURL, CoreDSN: coreURL, ChatDSN: chatURL})
	if err != nil {
		return fmt.Errorf("open document store: %w", err)
	}
	defer store.Close()
	conn, err := openSeedDB(ctx, coreURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	people, err := loadSeedPeople(ctx, conn, tenant)
	if err != nil {
		return err
	}
	opts := documentSeedOptions{Tenant: tenant, MediaRoot: os.Getenv(EnvDocumentMediaRoot)}
	if strings.TrimSpace(chatURL) != "" {
		// The showcase documents link seeded chat rooms and post messages
		// that link back; without a chat database they name rooms plainly.
		chat, err := chatstore.New(ctx, chatstore.Config{DSN: chatURL})
		if err != nil {
			return fmt.Errorf("open chat store: %w", err)
		}
		defer chat.Close()
		opts.Chat = chat

		// Those showcase posts land in rooms the chat seeder already
		// registered in the core route directory (see leaseSeedRoute in
		// chat_seed.go), so they need the same route write lease that
		// seeder's own writes use, or chatstore's routeFence refuses them
		// with ErrNoRouteLease. conn is already open on the core database
		// for loadSeedPeople above; chatroutestore.New reuses it the same
		// way "migrate chat seed" opens its own route connection in
		// main.go, and Migrate is idempotent so this is safe even when
		// "chat seed" has already run it.
		routes, err := chatroutestore.New(conn)
		if err != nil {
			return fmt.Errorf("open chat route directory: %w", err)
		}
		if err := routes.Migrate(ctx); err != nil {
			return fmt.Errorf("migrate chat route directory: %w", err)
		}
		opts.Routes = routes
	}
	return runDocumentSeedCommand(ctx, store, people, opts, out)
}

// runDocumentEmbedAction enqueues an index job for every searchable
// version in the tenant that has no vectors for the configured model
// (HCMNEXT_EMBEDDING_DIR, or HCMNEXT_EMBEDDING_URL and
// HCMNEXT_EMBEDDING_MODEL), requeueing skipped and failed ones. With drain
// it then runs the indexer loop in process until the queue is empty,
// tuned by the same HCMNEXT_EMBEDDING_* variables as the cell; without it
// the cell's background indexer picks the jobs up.
func runDocumentEmbedAction(ctx context.Context, documentURL, coreURL, chatURL, tenant string, drain bool, getenv func(string) string, out io.Writer) error {
	if strings.TrimSpace(tenant) == "" {
		tenant = defaultDocumentSeedTenant
	}
	embedder, err := documentembed.FromEnv(getenv)
	if err != nil {
		return fmt.Errorf("document embed: %w", err)
	}
	cfg, err := documentembed.IndexerConfigFromEnv(getenv)
	if err != nil {
		return err
	}
	store, err := documenthubstore.New(ctx, documenthubstore.Config{DSN: documentURL, CoreDSN: coreURL, ChatDSN: chatURL})
	if err != nil {
		return fmt.Errorf("open document store: %w", err)
	}
	defer store.Close()
	return runDocumentEmbedCommand(ctx, store, embedder, cfg, tenant, drain, out)
}

func runDocumentEmbedCommand(ctx context.Context, store *documenthubstore.Store, embedder documentembed.Embedder, cfg documentembed.IndexerConfig, tenant string, drain bool, out io.Writer) error {
	queued, err := store.EnqueueMissingIndexJobs(ctx, tenant, embedder.Model())
	if err != nil {
		return fmt.Errorf("enqueue index jobs: %w", err)
	}
	stats, err := store.IndexQueueStats(ctx, tenant, embedder.Model())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "document embed %s with %s: enqueued %d; queue: %d queued, %d running, %d done, %d failed, %d skipped\n",
		tenant, embedder.Model(), queued, stats.Queued, stats.Running, stats.Done, stats.Failed, stats.Skipped)
	if !drain {
		return nil
	}
	indexer := documentembed.NewIndexer(store, embedder, cfg, tenant)
	lastPrint := time.Time{}
	report := func(p documentembed.DrainProgress, final bool) {
		if !final && time.Since(lastPrint) < time.Second {
			return
		}
		lastPrint = time.Now()
		rate := 0.0
		if p.Elapsed > 0 {
			rate = float64(p.Processed) / p.Elapsed.Seconds()
		}
		fmt.Fprintf(out, "drain: %d done, %d remaining, %d failed, %d skipped; %d jobs in %s (%.1f docs/sec)\n",
			p.Done, p.Queued+p.Running, p.Failed, p.Skipped, p.Processed, p.Elapsed.Round(time.Millisecond), rate)
	}
	final, err := indexer.Drain(ctx, tenant, func(p documentembed.DrainProgress) { report(p, false) })
	if err != nil {
		return fmt.Errorf("drain index queue: %w", err)
	}
	report(final, true)
	return nil
}
