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

	"gopkg.in/yaml.v3"
)

type chatDisposition struct {
	Sources []string `yaml:"sources"`
	Tables  []struct {
		Table               string  `yaml:"table"`
		Migration           string  `yaml:"migration"`
		TenantScopingColumn *string `yaml:"tenant_scoping_column"`
	} `yaml:"tables"`
}

// chatMigrationSources lists the embedded chat migration tree from disk so a new
// migration cannot be added without appearing in the registry.
func chatMigrationSources(root string) ([]string, error) {
	dir := filepath.Join(root, "internal", "data", "chatstore", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		out = append(out, "internal/data/chatstore/migrations/"+e.Name())
	}
	sort.Strings(out)
	return out, nil
}

func manifestSources(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	var manifest chatDisposition
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]bool, len(manifest.Sources))
	for _, s := range manifest.Sources {
		out[s] = true
	}
	return out
}

func chatRegistryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate chat registry test")
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

// TestTodo_CHAT_004_StorageDispositionMatchesEmbeddedSchemas keeps the
// independent chat registry closed over every embedded or dynamic CREATE TABLE
// source. It also refuses a tenant-owned table that has no declared scope;
// source migrations must provide the matching tenant_isolation policy before
// the registry can claim the table is compliant.
func TestTodo_CHAT_004_StorageDispositionMatchesEmbeddedSchemas(t *testing.T) {
	root := chatRegistryRoot(t)
	manifestPath := filepath.Join(root, "definitions", "storage", "chat-storage-disposition.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest chatDisposition
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Tables) == 0 {
		t.Fatal("chat storage registry has no tables")
	}

	tablePattern := regexp.MustCompile(`(?i)CREATE TABLE(?: IF NOT EXISTS)?\s+([a-z_][a-z0-9_]*)`)
	declared := make(map[string]bool, len(manifest.Tables))
	for _, row := range manifest.Tables {
		if row.Table == "" || declared[row.Table] {
			t.Fatalf("duplicate or empty chat table registration: %q", row.Table)
		}
		declared[row.Table] = true
		if row.Migration == "" {
			t.Fatalf("chat table %q has no source", row.Table)
		}
		if row.TenantScopingColumn == nil || strings.TrimSpace(*row.TenantScopingColumn) == "" {
			t.Errorf("chat table %q has no tenant scoping column", row.Table)
		}
	}

	// The source list is derived from the migrations directory rather than
	// restated here. A hardcoded list silently hid 00006 and 00007, so a new
	// migration could add a table with no disposition row and no test failure.
	sources, err := chatMigrationSources(root)
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, "internal/data/chatroutestore/store.go")
	declaredSources := manifestSources(t, data)
	for _, source := range sources {
		if !declaredSources[source] {
			t.Errorf("chat registry sources omit %s", source)
		}
	}
	for source := range declaredSources {
		if !slices.Contains(sources, source) {
			t.Errorf("chat registry lists source %s that no longer exists", source)
		}
	}

	var observed []string
	for _, source := range sources {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		matches := tablePattern.FindAllStringSubmatch(text, -1)
		// Only a source that creates a table has to carry a policy; a migration
		// that only alters columns, indexes or triggers has nothing to isolate.
		if len(matches) > 0 && !strings.Contains(text, "tenant_isolation") && !strings.Contains(text, "_tenant ON") {
			t.Errorf("chat source %s declares no tenant_isolation policy", source)
		}
		for _, match := range matches {
			observed = append(observed, match[1])
			if !declared[match[1]] {
				t.Errorf("chat source %s creates unregistered table %q", source, match[1])
			}
		}
	}
	sort.Strings(observed)
	if len(observed) != len(declared) {
		t.Fatalf("chat registry/source table count mismatch: source=%d registry=%d (%v)", len(observed), len(declared), observed)
	}
}
