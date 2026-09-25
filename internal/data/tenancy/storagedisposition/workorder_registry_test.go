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

type workOrderDisposition struct {
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

// TestWorkOrderStorageDispositionMatchesMigrations keeps the work-order-owned
// schema and its retention declaration closed over each physical base table.
func TestWorkOrderStorageDispositionMatchesMigrations(t *testing.T) {
	root := chatRegistryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "definitions", "storage", "workorder-storage-disposition.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest workOrderDisposition
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "data", "workorderstore", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			sources = append(sources, "internal/data/workorderstore/migrations/"+entry.Name())
		}
	}
	sort.Strings(sources)
	if !slices.Equal(sources, manifest.Sources) {
		t.Fatalf("work order migration sources = %v, registry = %v", sources, manifest.Sources)
	}
	declared := make(map[string]string, len(manifest.Tables))
	appendOnly := make(map[string]bool, len(manifest.Tables))
	for _, row := range manifest.Tables {
		if row.Table == "" || declared[row.Table] != "" {
			t.Fatalf("duplicate or empty work order table %q", row.Table)
		}
		if row.TenantScopingColumn != "tenant_id" || row.OwnerPackage != "internal/data/workorderstore" || !slices.Contains(sources, row.Migration) {
			t.Errorf("work order table %q has invalid tenant scope, owner, or migration", row.Table)
		}
		if row.RetentionClass != "PERMANENT" && row.RetentionClass != "OPERATIONAL" {
			t.Errorf("work order table %q has unknown retention class %q", row.Table, row.RetentionClass)
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
		if !strings.Contains(text, "FORCE ROW LEVEL SECURITY") || !strings.Contains(text, "CREATE POLICY tenant_isolation") {
			t.Errorf("work order source %s lacks forced tenant RLS", source)
		}
		for _, match := range tablePattern.FindAllStringSubmatch(text, -1) {
			if prior := observed[match[1]]; prior != "" {
				t.Errorf("work order table %q declared twice: %s and %s", match[1], prior, source)
			}
			observed[match[1]] = source
			if appendOnly[match[1]] {
				trigger := regexp.MustCompile(`(?is)CREATE TRIGGER\s+\w+\s+BEFORE\s+UPDATE\s+OR\s+DELETE\s+ON\s+` + regexp.QuoteMeta(match[1]) + `\b.*?EXECUTE FUNCTION\s+workorder_forbid_mutation\(\)`)
				if !trigger.MatchString(text) {
					t.Errorf("append-only work order table %q lacks forbid-mutation trigger", match[1])
				}
			}
		}
	}
	if len(observed) != len(declared) {
		t.Errorf("work order tables: migrations=%d registry=%d", len(observed), len(declared))
	}
	for table, source := range observed {
		if declared[table] != source {
			t.Errorf("work order table %q source %q, registry %q", table, source, declared[table])
		}
	}
}
