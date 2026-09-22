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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// EnvChatDatabaseURL names the independent chat database this command
// migrates. It matches the serve role's -chat-database-url/
// HCMNEXT_CHAT_DATABASE_URL spelling, so one deployment configures both.
const EnvChatDatabaseURL = "HCMNEXT_CHAT_DATABASE_URL"

const fieldChatDatabaseURL = "chat-database-url"

// chatCommandPrefix is the "migrate chat up|status" subcommand namespace. The
// chat schema is a separate migration set on a separate database, so it never
// shares the core up/down/status path or the core schema journal.
const chatCommandPrefix = "chat "

// ErrChatSharesCoreDatabase refuses to migrate the chat schema into the
// core/workflow database. The spec's first acceptance gate is that the two use
// separate databases; applying chat tables to the core database would silently
// defeat it.
var ErrChatSharesCoreDatabase = errors.New("chat database must be independent from the core database")

// chatSubcommand reports the action behind a "chat ..." command, or "" when the
// command is not a chat command.
func chatSubcommand(command string) string {
	if !strings.HasPrefix(command, chatCommandPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(command, chatCommandPrefix))
}

// validateChatCommand checks a chat invocation before any connection is opened:
// a known action, a chat DSN, and a chat DSN that is not the core database.
func validateChatCommand(action, chatURL, coreURL string) error {
	switch action {
	case "up", "status", "seed":
	case "":
		return fmt.Errorf("usage: migrate chat up|status|seed")
	default:
		return fmt.Errorf("unknown chat command %q; usage: migrate chat up|status|seed", action)
	}
	if strings.TrimSpace(chatURL) == "" {
		return fmt.Errorf("%s is not set; pass -%s or set the environment variable", EnvChatDatabaseURL, fieldChatDatabaseURL)
	}
	if sameTargetDatabase(chatURL, coreURL) {
		return ErrChatSharesCoreDatabase
	}
	return nil
}

// sameTargetDatabase compares logical database identity across both DSN
// spellings, the same rule chatstore.New applies at runtime: host, port and
// database name decide, because differing credentials are a defense in depth
// and not a separation.
func sameTargetDatabase(chatURL, coreURL string) bool {
	if strings.TrimSpace(coreURL) == "" {
		return false
	}
	chat, chatErr := pgconn.ParseConfig(chatURL)
	core, coreErr := pgconn.ParseConfig(coreURL)
	if chatErr != nil || coreErr != nil {
		return strings.TrimSpace(chatURL) == strings.TrimSpace(coreURL)
	}
	return chat.Port == core.Port && strings.EqualFold(chat.Host, core.Host) && chat.Database == core.Database
}

// openChatMigrateDB opens the chat database for Goose. It is deliberately its
// own function: the chat DSN carries chat credentials and must never be
// resolved from the core database flag.
func openChatMigrateDB(ctx context.Context, url string) (*sql.DB, error) {
	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", EnvChatDatabaseURL, err)
	}
	db := stdlib.OpenDB(*connCfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	return db, nil
}

// runChatMigrateCommand applies or reports the chat migration set against an
// already-open chat database. It is kept independent of how db was opened so a
// test can hand it a pgtest-managed connection.
func runChatMigrateCommand(ctx context.Context, action string, db *sql.DB, out io.Writer) error {
	migrationFS, err := fs.Sub(chatstore.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("read chat migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFS,
		goose.WithVerbose(false),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("build chat migration provider: %w", err)
	}
	switch action {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("apply chat migrations: %w", err)
		}
		for _, r := range results {
			fmt.Fprintf(out, "applied %s\n", r.Source.Path)
		}
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("read chat migration status: %w", err)
		}
		for _, s := range statuses {
			applied := "-"
			if !s.AppliedAt.IsZero() {
				applied = s.AppliedAt.UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(out, "%-9s %-32s %s\n", s.State, s.Source.Path, applied)
		}
	default:
		return fmt.Errorf("unknown chat command %q; usage: migrate chat up|status", action)
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read chat schema version: %w", err)
	}
	sources := provider.ListSources()
	target := int64(0)
	if len(sources) > 0 {
		target = sources[len(sources)-1].Version
	}
	fmt.Fprintf(out, "chat schema version %d of %d\n", current, target)
	return nil
}
