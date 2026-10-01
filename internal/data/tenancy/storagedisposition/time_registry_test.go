package storagedisposition_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type timeDisposition struct {
	Sources []string `yaml:"sources"`
	Tables  []struct {
		Table               string `yaml:"table"`
		Migration           string `yaml:"migration"`
		OwnerPackage        string `yaml:"owner_package"`
		TenantScopingColumn string `yaml:"tenant_scoping_column"`
		RetentionClass      string `yaml:"retention_class"`
		AppendOnly          bool   `yaml:"append_only"`
	} `yaml:"tables"`
}

// TestTimeStorageDispositionMatchesMigrations keeps the time-keeping schema
// and its retention declaration closed over each physical base table. The
// time schema enables tenant RLS through time_enable_tenant_isolation, so
// every table must be passed to that helper in the migration creating it.
func TestTimeStorageDispositionMatchesMigrations(t *testing.T) {
	root := chatRegistryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "definitions", "storage", "time-storage-disposition.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest timeDisposition
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("time migration sources = %v, registry = %v", sources, manifest.Sources)
	}
	declared := make(map[string]string, len(manifest.Tables))
	appendOnly := make(map[string]bool, len(manifest.Tables))
	for _, row := range manifest.Tables {
		if row.Table == "" || declared[row.Table] != "" {
			t.Fatalf("duplicate or empty time table %q", row.Table)
		}
		if row.TenantScopingColumn != "tenant_id" || row.OwnerPackage != "internal/data/timestore" || !slices.Contains(sources, row.Migration) {
			t.Errorf("time table %q has invalid tenant scope, owner, or migration", row.Table)
		}
		switch row.RetentionClass {
		case "PERMANENT", "OPERATIONAL", "EVIDENCE", "REBUILDABLE":
		default:
			t.Errorf("time table %q has unknown retention class %q", row.Table, row.RetentionClass)
		}
		declared[row.Table] = row.Migration
		appendOnly[row.Table] = row.AppendOnly
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
			table := match[1]
			if prior := observed[table]; prior != "" {
				t.Errorf("time table %q declared twice: %s and %s", table, prior, source)
			}
			observed[table] = source
			isolation := regexp.MustCompile(`(?i)time_enable_tenant_isolation\(\s*'` + regexp.QuoteMeta(table) + `'\s*\)`)
			if !isolation.MatchString(text) {
				t.Errorf("time table %q lacks forced tenant RLS in %s", table, source)
			}
			if appendOnly[table] {
				trigger := regexp.MustCompile(`(?is)CREATE TRIGGER\s+\w+\s+BEFORE\s+UPDATE\s+OR\s+DELETE\s+ON\s+` + regexp.QuoteMeta(table) + `\b.*?EXECUTE FUNCTION\s+time_forbid_mutation\(\)`)
				if !trigger.MatchString(text) {
					t.Errorf("append-only time table %q lacks forbid-mutation trigger", table)
				}
			}
		}
	}
	if len(observed) != len(declared) {
		t.Errorf("time tables: migrations=%d registry=%d", len(observed), len(declared))
	}
	for table, source := range observed {
		if declared[table] != source {
			t.Errorf("time table %q source %q, registry %q", table, source, declared[table])
		}
	}
}
