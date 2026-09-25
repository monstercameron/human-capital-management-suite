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

type projectDisposition struct {
	Sources []string `yaml:"sources"`
	Tables  []struct {
		Table               string `yaml:"table"`
		Migration           string `yaml:"migration"`
		TenantScopingColumn string `yaml:"tenant_scoping_column"`
		RLSPolicy           string `yaml:"rls_policy"`
		AppendOnly          bool   `yaml:"append_only"`
	} `yaml:"tables"`
}

// TestTodo_PM_003_StorageDispositionMatchesEmbeddedSchemas keeps the project
// registry closed over every embedded project migration and physical table.
func TestTodo_PM_003_StorageDispositionMatchesEmbeddedSchemas(t *testing.T) {
	root := chatRegistryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "definitions", "storage", "project-storage-disposition.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest projectDisposition
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "data", "projectstore", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			sources = append(sources, "internal/data/projectstore/migrations/"+entry.Name())
		}
	}
	sort.Strings(sources)
	if !slices.Equal(sources, manifest.Sources) {
		t.Fatalf("project migration sources = %v, registry = %v", sources, manifest.Sources)
	}
	declared := make(map[string]string, len(manifest.Tables))
	for _, row := range manifest.Tables {
		if row.Table == "" || declared[row.Table] != "" {
			t.Fatalf("duplicate or empty project table: %q", row.Table)
		}
		if row.TenantScopingColumn != "tenant_id" || row.RLSPolicy != "tenant_isolation" || !slices.Contains(sources, row.Migration) {
			t.Errorf("project table %q has invalid tenant policy or migration", row.Table)
		}
		declared[row.Table] = row.Migration
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
			if !strings.Contains(text, "tenant_isolation") || !strings.Contains(text, "FORCE ROW LEVEL SECURITY") {
				t.Errorf("project source %s lacks forced tenant RLS", source)
			}
			if prior := observed[match[1]]; prior != "" {
				t.Errorf("project table %q declared twice: %s and %s", match[1], prior, source)
			}
			observed[match[1]] = source
		}
	}
	if len(observed) != len(declared) {
		t.Errorf("project tables: migrations=%d registry=%d", len(observed), len(declared))
	}
	for table, source := range observed {
		if declared[table] != source {
			t.Errorf("project table %q source %q, registry %q", table, source, declared[table])
		}
	}
}
