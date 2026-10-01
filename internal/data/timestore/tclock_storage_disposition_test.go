package timestore

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

type timeStorageManifest struct {
	Sources []string `yaml:"sources"`
	Tables  []struct {
		Table               string  `yaml:"table"`
		Migration           string  `yaml:"migration"`
		OwnerPackage        string  `yaml:"owner_package"`
		Plane               string  `yaml:"plane"`
		DataRole            string  `yaml:"data_role"`
		TenantScopingColumn string  `yaml:"tenant_scoping_column"`
		AppendOnly          bool    `yaml:"append_only"`
		RetentionClass      string  `yaml:"retention_class"`
		RebuildSource       *string `yaml:"rebuild_source"`
	} `yaml:"tables"`
}

func timeStorageRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("cannot locate repository root")
		}
		root = parent
	}
}

func loadTimeManifest(t *testing.T) timeStorageManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(timeStorageRoot(t), "definitions", "storage", "time-storage-disposition.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest timeStorageManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestTodo_TCLOCK_002_StorageDispositionMatchesMigrations(t *testing.T) {
	manifest := loadTimeManifest(t)
	root := timeStorageRoot(t)
	dir := filepath.Join(root, "internal", "data", "timestore", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			sources = append(sources, "internal/data/timestore/migrations/"+entry.Name())
		}
	}
	sort.Strings(sources)
	if !slices.Equal(sources, manifest.Sources) {
		t.Fatalf("migration sources = %v, registry = %v", sources, manifest.Sources)
	}

	declared := make(map[string]struct {
		migration, role, retention string
		appendOnly                 bool
	}, len(manifest.Tables))
	for _, row := range manifest.Tables {
		if row.Table == "" || row.Migration == "" || row.OwnerPackage != "internal/data/timestore" || row.Plane != "TIME_DATA" || row.TenantScopingColumn != "tenant_id" {
			t.Fatalf("invalid time disposition row: %+v", row)
		}
		if _, duplicate := declared[row.Table]; duplicate {
			t.Fatalf("duplicate time table %q", row.Table)
		}
		if row.RetentionClass != "PERMANENT" && row.RetentionClass != "OPERATIONAL" && row.RetentionClass != "REBUILDABLE" {
			t.Fatalf("unknown retention class for %s: %q", row.Table, row.RetentionClass)
		}
		declared[row.Table] = struct {
			migration, role, retention string
			appendOnly                 bool
		}{row.Migration, row.DataRole, row.RetentionClass, row.AppendOnly}
	}

	tablePattern := regexp.MustCompile(`(?i)CREATE TABLE(?: IF NOT EXISTS)?\s+([a-z_][a-z0-9_]*)`)
	observed := make(map[string]string)
	for _, source := range sources {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, match := range tablePattern.FindAllStringSubmatch(text, -1) {
			name := match[1]
			if prior := observed[name]; prior != "" {
				t.Fatalf("table %q declared by both %s and %s", name, prior, source)
			}
			observed[name] = source
			row, ok := declared[name]
			if !ok {
				t.Errorf("migration %s creates unregistered table %q", source, name)
				continue
			}
			if row.migration != source {
				t.Errorf("table %q source is %s, registry says %s", name, source, row.migration)
			}
			if !strings.Contains(text, "time_enable_tenant_isolation('"+name+"')") {
				t.Errorf("table %q lacks tenant RLS helper call in %s", name, source)
			}
			if row.appendOnly && !strings.Contains(text, "ON "+name) || row.appendOnly && !strings.Contains(text, "time_forbid_mutation()") {
				t.Errorf("append-only table %q lacks time_forbid_mutation trigger in %s", name, source)
			}
		}
	}
	if len(observed) != len(declared) {
		t.Fatalf("migration tables=%d, registry tables=%d", len(observed), len(declared))
	}
	for name, source := range observed {
		if declared[name].migration != source {
			t.Errorf("table %q source mismatch", name)
		}
	}
}

func TestTodo_TCLOCK_002_StorageDispositionMatchesLivePostgres(t *testing.T) {
	manifest := loadTimeManifest(t)
	rows := make(map[string]struct {
		appendOnly bool
	}, len(manifest.Tables))
	for _, row := range manifest.Tables {
		rows[row.Table] = struct{ appendOnly bool }{row.AppendOnly}
	}
	db := pgtest.NewEmpty(t)
	migrationFS, err := fsSubMigrations()
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	for name, row := range rows {
		name, row := name, row
		t.Run(name, func(t *testing.T) {
			var enabled, forced bool
			if err := db.QueryRow(context.Background(), `SELECT c.relrowsecurity, c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = current_schema() AND c.relname = $1`, name).Scan(&enabled, &forced); err != nil {
				t.Fatal(err)
			}
			if !enabled || !forced {
				t.Fatalf("%s RLS enabled=%v forced=%v, want both true", name, enabled, forced)
			}
			var policyCount int
			if err := db.QueryRow(context.Background(), `SELECT count(*) FROM pg_policies WHERE schemaname = current_schema() AND tablename = $1 AND policyname = 'tenant_isolation'`, name).Scan(&policyCount); err != nil {
				t.Fatal(err)
			}
			if policyCount != 1 {
				t.Fatalf("%s tenant_isolation policy count=%d, want 1", name, policyCount)
			}
			var triggerCount int
			if err := db.QueryRow(context.Background(), `SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace JOIN pg_proc p ON p.oid = t.tgfoid WHERE n.nspname = current_schema() AND c.relname = $1 AND NOT t.tgisinternal AND p.proname = 'time_forbid_mutation'`, name).Scan(&triggerCount); err != nil {
				t.Fatal(err)
			}
			if row.appendOnly && triggerCount == 0 {
				t.Fatalf("%s is append-only but has no time_forbid_mutation trigger", name)
			}
			if !row.appendOnly && triggerCount != 0 {
				t.Fatalf("%s is mutable but has %d time_forbid_mutation triggers", name, triggerCount)
			}
		})
	}
}

func fsSubMigrations() (fs.FS, error) {
	return fs.Sub(Migrations, "migrations")
}
