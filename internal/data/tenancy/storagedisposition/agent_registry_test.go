package storagedisposition_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"gopkg.in/yaml.v3"
)

type agentDisposition struct {
	Sources []string `yaml:"sources"`
	Tables  []struct {
		Table               string `yaml:"table"`
		Migration           string `yaml:"migration"`
		OwnerPackage        string `yaml:"owner_package"`
		TenantScopingColumn string `yaml:"tenant_scoping_column"`
		RLSPolicy           string `yaml:"rls_policy"`
		AppendOnly          bool   `yaml:"append_only"`
	} `yaml:"tables"`
}

func agentRegistryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate agent registry test")
	}
	root := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("cannot find repository root")
		}
		root = parent
	}
}

// TestTodo_AGENTP_004_AgentStorageDispositionMatchesEmbeddedMigrations keeps
// the separate agent database registry closed over its migration sources,
// tenant policies and append-only triggers.
func TestTodo_AGENTP_004_AgentStorageDispositionMatchesEmbeddedMigrations(t *testing.T) {
	root := agentRegistryRoot(t)
	manifestPath := filepath.Join(root, "definitions", "storage", "agent-storage-disposition.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest agentDisposition
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	entries, err := agentstore.Migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			sources = append(sources, "internal/data/agentstore/migrations/"+entry.Name())
		}
	}
	sort.Strings(sources)
	if !slices.Equal(sources, manifest.Sources) {
		t.Fatalf("agent migration sources = %v, registry = %v", sources, manifest.Sources)
	}

	tablePattern := regexp.MustCompile(`(?i)CREATE TABLE(?: IF NOT EXISTS)?\s+([a-z_][a-z0-9_]*)`)
	declared := make(map[string]agentDispositionTable, len(manifest.Tables))
	for _, row := range manifest.Tables {
		if row.Table == "" || declared[row.Table].migration != "" {
			t.Fatalf("duplicate or empty agent table registration: %q", row.Table)
		}
		if row.TenantScopingColumn != "tenant_id" || row.RLSPolicy != "tenant_isolation" || row.Migration == "" || row.OwnerPackage == "" {
			t.Errorf("agent table %q has incomplete ownership or tenant isolation metadata", row.Table)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(row.OwnerPackage))); err != nil {
			t.Errorf("agent table %q owner package %q is missing: %v", row.Table, row.OwnerPackage, err)
		}
		declared[row.Table] = agentDispositionTable{migration: row.Migration, appendOnly: row.AppendOnly}
	}

	observed := make(map[string]string)
	for _, source := range sources {
		raw, err := agentstore.Migrations.ReadFile("migrations/" + filepath.Base(source))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, match := range tablePattern.FindAllStringSubmatch(text, -1) {
			table := match[1]
			if prior := observed[table]; prior != "" {
				t.Errorf("agent table %q declared in both %s and %s", table, prior, source)
			}
			observed[table] = source
			row, ok := declared[table]
			if !ok {
				t.Errorf("agent migration %s creates unregistered table %q", source, table)
				continue
			}
			if row.migration != source {
				t.Errorf("agent table %q migration=%q, want %q", table, row.migration, source)
			}
			if !strings.Contains(text, "ALTER TABLE "+table+" ENABLE ROW LEVEL SECURITY") ||
				!strings.Contains(text, "ALTER TABLE "+table+" FORCE ROW LEVEL SECURITY") ||
				!strings.Contains(text, "CREATE POLICY tenant_isolation ON "+table) {
				t.Errorf("agent table %q lacks forced tenant_isolation RLS in %s", table, source)
			}
			trigger := regexp.MustCompile(`(?im)CREATE\s+TRIGGER\s+\w+\s+BEFORE\s+UPDATE\s+OR\s+DELETE\s+ON\s+` + regexp.QuoteMeta(table) + `\s+FOR\s+EACH\s+ROW\s+EXECUTE\s+FUNCTION\s+forbid_mutation\s*\(`)
			if got := trigger.MatchString(text); got != row.appendOnly {
				t.Errorf("agent table %q append_only=%t but forbid_mutation trigger present=%t", table, row.appendOnly, got)
			}
		}
	}
	if len(observed) != len(declared) {
		t.Errorf("agent tables: migrations=%d registry=%d", len(observed), len(declared))
	}
	for table := range declared {
		if observed[table] == "" {
			t.Errorf("agent registry lists table %q absent from embedded migrations", table)
		}
	}
}

type agentDispositionTable struct {
	migration  string
	appendOnly bool
}
