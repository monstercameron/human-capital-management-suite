package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/user"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/schema"
	fixtureseed "github.com/monstercameron/human-capital-management-suite/internal/data/seed"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

const migrationLockName = "hcmnext:migration"

var errMigrationDirty = errors.New("migration: dirty journal requires operator repair")

// releaseOwner is the team accountable for this schema artifact.
const releaseOwner = "data-plane"

// openMigrateDB parses url and opens a *sql.DB against it, pinging once so
// a bad connection string or unreachable server fails before any Goose
// action runs.
func openMigrateDB(ctx context.Context, url string) (*sql.DB, error) {
	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", EnvDatabaseURL, err)
	}
	db := stdlib.OpenDB(*connCfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	return db, nil
}

// openSeedDB opens the transaction-capable dbport adapter required by the
// fixture seeder. It intentionally lives beside openMigrateDB: migrate uses
// database/sql for Goose, while the seed package owns its transaction through
// the driver-free dbport seam.
func openSeedDB(ctx context.Context, url string) (*pgxadapter.Conn, error) {
	conn, err := pgxadapter.Connect(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close(ctx)
		return nil, fmt.Errorf("connect: %w", err)
	}
	return conn, nil
}

type seedReceipt struct {
	Tenant   uuid.UUID `json:"tenant"`
	Digest   string    `json:"digest"`
	Inserted int       `json:"inserted"`
	Skipped  int       `json:"skipped"`
}

// runSeedCommand loads the deterministic fixture corpus in one transaction.
// It writes the receipt only after Commit succeeds, so a successful-looking
// receipt can never describe rolled-back seed work.
func runSeedCommand(ctx context.Context, db dbport.Beginner, tenant string, out io.Writer) error {
	if tenant == "" {
		return fmt.Errorf("seed tenant must not be empty")
	}
	tenantID := pgstore.TenantID(tenant)
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := ensureSeedTenant(ctx, tx, tenantID, tenant); err != nil {
		return err
	}
	summary, err := fixtureseed.Seed(ctx, tx, tenantID)
	if err != nil {
		return fmt.Errorf("seed tenant %q: %w", tenant, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed transaction: %w", err)
	}

	receipt := seedReceipt{
		Tenant:   summary.TenantID,
		Digest:   summary.Digest,
		Inserted: summary.Inserted,
		Skipped:  summary.Skipped,
	}
	if err := json.NewEncoder(out).Encode(receipt); err != nil {
		return fmt.Errorf("write seed receipt: %w", err)
	}
	return nil
}

// ensureSeedTenant makes the bootstrap seed step runnable immediately after a
// zero-to-current migration. The tenant identity supplied by the operator is
// the durable scope; the remaining row values are deterministic local-cell
// defaults and are left untouched when a tenant was already provisioned.
func ensureSeedTenant(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, tenant string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')
		ON CONFLICT (tenant_id) DO NOTHING`,
		tenantID, tenant, tenant)
	if err != nil {
		return fmt.Errorf("register seed tenant %q: %w", tenant, err)
	}
	return nil
}

// runMigrateCommand runs command (up|down|status) against db, an
// already-open connection to the target server, writing its report to out.
// It is the composition root's single entry point into Goose/the schema
// journal, kept independent of how db was opened so a test can hand it a
// pgtest-managed connection directly.
func runMigrateCommand(ctx context.Context, command string, db *sql.DB, out io.Writer) error {
	return withMigrationLock(ctx, db, func() error {
		return runMigrateCommandLocked(ctx, command, db, out)
	})
}

func withMigrationLock(ctx context.Context, db *sql.DB, fn func() error) error {
	// Goose receives *sql.DB, so a session advisory lock is used instead of a
	// transaction lock. Limiting this command's pool to one connection keeps
	// the lock and every Goose/journal statement on the same PostgreSQL
	// session, while a second migrate process waits instead of interleaving.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, migrationLockName); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, migrationLockName)
	}()
	return fn()
}

func runMigrateCommandLocked(ctx context.Context, command string, db *sql.DB, out io.Writer) error {
	info := buildinfo.Current()
	tool := toolVersion(info)

	provider, err := goose.NewProvider(
		goose.DialectPostgres, db, migrations.FS,
		goose.WithVerbose(false),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("build migration provider: %w", err)
	}

	files, err := migrations.Files()
	if err != nil {
		return err
	}
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		return err
	}

	switch command {
	case "status":
		return status(ctx, provider, db, out, digest, files, tool)
	case "up":
		return up(ctx, provider, db, out, digest, files, tool)
	default:
		return down(ctx, provider, db, out, digest, files, tool)
	}
}

func toolVersion(info buildinfo.Info) string {
	revision := info.Revision
	if revision == "" {
		revision = "unknown"
	}
	if info.Modified {
		revision += "+modified"
	}
	return fmt.Sprintf("hcmnext-migrate/%s (%s)", revision, info.GoVersion)
}

// releaseVersion binds the release identity to the artifact it was built from.
func releaseVersion(files []migrations.File, digest string) string {
	return fmt.Sprintf("p1a-%05d-%s", files[len(files)-1].Version, digest[:12])
}

func newJournal(db *sql.DB, toolVersion string) (*schema.Journal, error) {
	who := "unknown"
	if current, err := user.Current(); err == nil && current.Username != "" {
		who = current.Username
	}
	return schema.NewJournal(db, toolVersion, who, schema.WithTrustedTimeSource("HOST_CLOCK")), nil
}

// prepare records the release and refuses to proceed when an already applied
// migration no longer matches the bytes in this build.
func prepare(ctx context.Context, db *sql.DB, digest string, files []migrations.File, tool string) (*schema.Journal, string, error) {
	journal, err := newJournal(db, tool)
	if err != nil {
		return nil, "", err
	}
	version := releaseVersion(files, digest)
	release := schema.Release{
		Version:            version,
		ArtifactDigest:     digest,
		SourceDigest:       digest,
		ToolVersion:        tool,
		CompatibilityClass: schema.CompatibilityBackward,
		Owner:              releaseOwner,
		Reversible:         true,
	}
	if err := journal.EnsureRelease(ctx, release); err != nil {
		return nil, "", err
	}
	releaseID, err := journal.ReleaseID(ctx, version)
	if err != nil {
		return nil, "", err
	}
	if err := journal.VerifyChecksums(ctx, releaseID, files); err != nil {
		return nil, "", fmt.Errorf("startup blocked: %w", err)
	}
	return journal, version, nil
}

func up(ctx context.Context, provider *goose.Provider, db *sql.DB, out io.Writer, digest string, files []migrations.File, tool string) error {
	// The journal tables live in the first migration, so that one is applied
	// before the journal can record anything. The loop below then journals it
	// like any other, through the already-applied path.
	if _, err := provider.ApplyVersion(ctx, files[0].Version, true); err != nil &&
		!errors.Is(err, goose.ErrAlreadyApplied) {
		return fmt.Errorf("apply %s: %w", files[0].Name, err)
	}
	if err := verifyMigrationJournalClean(ctx, db); err != nil {
		return err
	}

	journal, version, err := prepare(ctx, db, digest, files, tool)
	if err != nil {
		return err
	}
	releaseID, err := journal.ReleaseID(ctx, version)
	if err != nil {
		return err
	}

	for _, file := range files {
		applied, err := journal.Applied(ctx, releaseID, file)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		entry, err := journal.Begin(ctx, releaseID, file, schema.DirectionUp)
		if err != nil {
			return err
		}
		_, applyErr := provider.ApplyVersion(ctx, file.Version, true)
		switch {
		case errors.Is(applyErr, goose.ErrAlreadyApplied):
			// Applied by the bootstrap step above, or by a build that predates
			// the journal. Record it rather than replaying it.
			if err := journal.Succeed(ctx, entry); err != nil {
				return err
			}
			fmt.Fprintf(out, "recorded %s (already applied)\n", file.Name)
		case applyErr != nil:
			if journalErr := journal.Fail(ctx, entry, applyErr); journalErr != nil {
				return errors.Join(applyErr, journalErr)
			}
			return fmt.Errorf("apply %s: %w", file.Name, applyErr)
		default:
			if err := journal.Succeed(ctx, entry); err != nil {
				return err
			}
			fmt.Fprintf(out, "applied %s\n", file.Name)
		}
	}

	return report(ctx, provider, out, digest, version)
}

func down(ctx context.Context, provider *goose.Provider, db *sql.DB, out io.Writer, digest string, files []migrations.File, tool string) error {
	if err := verifyMigrationJournalClean(ctx, db); err != nil {
		return err
	}
	journal, version, err := prepare(ctx, db, digest, files, tool)
	if err != nil {
		return err
	}
	releaseID, err := journal.ReleaseID(ctx, version)
	if err != nil {
		return err
	}

	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	var target migrations.File
	for _, file := range files {
		if file.Version == current {
			target = file
		}
	}
	if target.Version == 0 {
		return fmt.Errorf("no embedded migration matches the applied version %d", current)
	}

	entry, err := journal.Begin(ctx, releaseID, target, schema.DirectionDown)
	if err != nil {
		return err
	}
	result, err := provider.Down(ctx)
	if err != nil {
		if journalErr := journal.Fail(ctx, entry, err); journalErr != nil {
			return errors.Join(err, journalErr)
		}
		return fmt.Errorf("roll back %s: %w", target.Name, err)
	}
	if err := journal.Succeed(ctx, entry); err != nil {
		return err
	}
	// The up-entry is no longer in effect; the down-entry is the evidence.
	if err := journal.MarkRolledBack(ctx, releaseID, target.Version); err != nil {
		return err
	}
	fmt.Fprintf(out, "rolled back %s\n", result.Source.Path)

	return report(ctx, provider, out, digest, version)
}

func status(ctx context.Context, provider *goose.Provider, db *sql.DB, out io.Writer, digest string, files []migrations.File, tool string) error {
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > 0 {
		if err := verifyMigrationJournalClean(ctx, db); err != nil {
			return err
		}
		if _, _, err := prepare(ctx, db, digest, files, tool); err != nil {
			return err
		}
	}
	statuses, err := provider.Status(ctx)
	if err != nil {
		return fmt.Errorf("read migration status: %w", err)
	}
	for _, s := range statuses {
		applied := "-"
		if !s.AppliedAt.IsZero() {
			applied = s.AppliedAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(out, "%-9s %-32s %s\n", s.State, s.Source.Path, applied)
	}
	return report(ctx, provider, out, digest, releaseVersion(files, digest))
}

func verifyMigrationJournalClean(ctx context.Context, db *sql.DB) error {
	var version int64
	var name, status, direction string
	err := db.QueryRowContext(ctx, `
		SELECT migration_version, migration_name, status, direction
		FROM migration_journal
		WHERE status IN ('PLANNED', 'RUNNING', 'FAILED')
		  AND (direction = 'DOWN' OR rolled_back_at IS NULL)
		ORDER BY started_at, journal_id
		LIMIT 1`).Scan(&version, &name, &status, &direction)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read migration journal state: %w", err)
	}
	return fmt.Errorf("%w: migration %d (%s) is %s/%s", errMigrationDirty, version, name, direction, status)
}

func report(ctx context.Context, provider *goose.Provider, out io.Writer, digest, version string) error {
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	target, err := migrations.TargetVersion()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "schema version %d of %d, release %s, migration digest %s:%s\n",
		current, target, version, migrations.DigestAlgorithm, digest)
	return nil
}
