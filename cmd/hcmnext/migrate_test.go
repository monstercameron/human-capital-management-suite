package main

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// recordingMigrateLogger captures the one event migrateUp reports.
type recordingMigrateLogger struct {
	mu     sync.Mutex
	events []string
	attrs  []any
}

func (l *recordingMigrateLogger) Info(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, msg)
	l.attrs = append(l.attrs, args...)
}

func (l *recordingMigrateLogger) Error(string, ...any) {}

func (l *recordingMigrateLogger) saw(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, event := range l.events {
		if event == msg {
			return true
		}
	}
	return false
}

// schemaPinnedURL pins every connection the migration runner opens to this
// test's own schema, the way the serve process's pool is pinned in a
// deployment.
func schemaPinnedURL(t *testing.T, rawURL, schema string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse pgtest URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// TestMigrateUpBringsAnEmptySchemaToHead is the adapter half of
// internal/application's Migrator port: this command owns the Goose runner
// (definitions/architecture/library-firewall.yaml confines goose to the
// migrations and cmd roots), so this is where it is proved to work.
func TestMigrateUpBringsAnEmptySchemaToHead(t *testing.T) {
	db := pgtest.NewEmpty(t)
	logger := &recordingMigrateLogger{}
	dsn := schemaPinnedURL(t, db.URL, db.Schema)

	if err := migrateUp(context.Background(), dsn, logger); err != nil {
		t.Fatalf("migrateUp on an empty schema: %v", err)
	}
	if !logger.saw("hcmnext.schema_applied") {
		t.Error("migrateUp applied the schema without reporting it")
	}

	target, err := migrations.TargetVersion()
	if err != nil {
		t.Fatalf("target version: %v", err)
	}
	var applied int64
	if err := db.SQL.QueryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&applied); err != nil {
		t.Fatalf("read the applied version: %v", err)
	}
	if applied != target {
		t.Errorf("schema is at version %d, want the embedded tree's %d", applied, target)
	}

	// Applying again is the case a developer actually hits: a second start
	// against a database that is already at head must be a no-op, not a
	// failure.
	if err := migrateUp(context.Background(), dsn, logger); err != nil {
		t.Fatalf("migrateUp on a schema already at head: %v", err)
	}
}

// TestMigrateUpRejectsAnUnusableDatabaseURL proves the failure a
// -migrate=true start reports rather than swallowing.
func TestMigrateUpRejectsAnUnusableDatabaseURL(t *testing.T) {
	err := migrateUp(context.Background(), "not-a-postgres-url", &recordingMigrateLogger{})
	if err == nil {
		t.Fatal("migrateUp accepted a URL it cannot parse")
	}
	if !strings.Contains(err.Error(), "parse the database URL") {
		t.Errorf("error = %q, want it to name parsing the database URL", err)
	}
}

func TestMigrateWorkOrderUpCreatesItsIsolatedSchema(t *testing.T) {
	db := pgtest.NewEmpty(t)
	logger := &recordingMigrateLogger{}
	const schema = "hcmnext_workorder_migration_test"
	if err := migrateWorkOrderUp(context.Background(), db.URL, "", schema, logger); err != nil {
		t.Fatalf("migrateWorkOrderUp on an empty schema: %v", err)
	}
	if !logger.saw("hcmnext.work_order_schema_applied") {
		t.Fatal("work-order schema migration did not report success")
	}
	var exists bool
	if err := db.SQL.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = 'work_order'
	)`, schema).Scan(&exists); err != nil {
		t.Fatalf("check isolated work-order table: %v", err)
	}
	if !exists {
		t.Fatalf("work_order table was not created in schema %q", schema)
	}
	// A second startup must see an already-applied version and stay idempotent.
	if err := migrateWorkOrderUp(context.Background(), db.URL, "", schema, logger); err != nil {
		t.Fatalf("migrateWorkOrderUp on a schema already at head: %v", err)
	}
}

// TestMigrateUpSatisfiesTheApplicationMigratorPort is what makes this
// function the adapter the composition root asks for: the command hands it to
// application.WithMigrator, and a signature change here has to be a
// compilation failure rather than a runtime surprise.
func TestMigrateUpSatisfiesTheApplicationMigratorPort(t *testing.T) {
	var port application.Migrator = migrateUp
	if port == nil {
		t.Fatal("migrateUp does not satisfy application.Migrator")
	}
	spec, err := application.SpecFor(application.RoleServe, nil, application.WithMigrator(port))
	if err != nil {
		t.Fatalf("SpecFor with this command's migrator: %v", err)
	}
	if spec.Build == nil {
		t.Error("the composed spec has no build step")
	}
}

func TestMigrateWorkOrderUpSatisfiesTheApplicationMigratorPort(t *testing.T) {
	var port application.WorkOrderMigrator = migrateWorkOrderUp
	if port == nil {
		t.Fatal("migrateWorkOrderUp does not satisfy application.WorkOrderMigrator")
	}
	spec, err := application.SpecFor(application.RoleServe, nil, application.WithWorkOrderMigrator(port))
	if err != nil {
		t.Fatalf("SpecFor with this command's work-order migrator: %v", err)
	}
	if spec.Build == nil {
		t.Error("the composed spec has no build step")
	}
}
