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
		// CREATE TABLE is always written out; row security, the policy and the
		// trigger may instead be applied to a list of tables in a loop.
		applied := text + agentMigrationLoopStatements(text)
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
			if !strings.Contains(applied, "ALTER TABLE "+table+" ENABLE ROW LEVEL SECURITY") ||
				!strings.Contains(applied, "ALTER TABLE "+table+" FORCE ROW LEVEL SECURITY") ||
				!strings.Contains(applied, "CREATE POLICY tenant_isolation ON "+table) {
				t.Errorf("agent table %q lacks forced tenant_isolation RLS in %s", table, source)
			}
			trigger := regexp.MustCompile(`(?im)CREATE\s+TRIGGER\s+\w+\s+BEFORE\s+UPDATE\s+OR\s+DELETE\s+ON\s+` + regexp.QuoteMeta(table) + `\s+FOR\s+EACH\s+ROW\s+EXECUTE\s+FUNCTION\s+forbid_mutation\s*\(`)
			if got := trigger.MatchString(applied); got != row.appendOnly {
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

var (
	agentMigrationLoop      = regexp.MustCompile(`(?is)FOREACH\s+\w+\s+IN\s+ARRAY\s+ARRAY\[(.*?)\]\s+LOOP(.*?)END\s+LOOP`)
	agentMigrationLoopTable = regexp.MustCompile(`'([a-z_][a-z0-9_]*)'`)
	agentMigrationLoopExec  = regexp.MustCompile(`(?is)EXECUTE\s+format\('((?:[^']|'')*)'\s*,\s*\w+\s*\)`)
)

// agentMigrationLoopStatements writes out the statements a migration applies
// to a list of tables in a loop (FOREACH name IN ARRAY ARRAY['a','b'] LOOP
// EXECUTE format('... %I ...', name); END LOOP), one copy per listed table,
// so the checks above read them as they read statements written in full. A
// table missing from the list gets no statement and still fails.
func agentMigrationLoopStatements(text string) string {
	var out strings.Builder
	for _, loop := range agentMigrationLoop.FindAllStringSubmatch(text, -1) {
		for _, table := range agentMigrationLoopTable.FindAllStringSubmatch(loop[1], -1) {
			for _, statement := range agentMigrationLoopExec.FindAllStringSubmatch(loop[2], -1) {
				out.WriteString("\n" + strings.ReplaceAll(strings.ReplaceAll(statement[1], "''", "'"), "%I", table[1]) + ";")
			}
		}
	}
	return out.String()
}

// TestTodo_AGENTP_004_AgentMigrationLoopStatements proves the loop reader
// credits a statement only to the tables its loop lists.
func TestTodo_AGENTP_004_AgentMigrationLoopStatements(t *testing.T) {
	migration := `CREATE TABLE a (tenant_id uuid); CREATE TABLE b (tenant_id uuid); CREATE TABLE c (tenant_id uuid);
DO $$ DECLARE name text; BEGIN
 FOREACH name IN ARRAY ARRAY['a','b'] LOOP
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)', name);
 END LOOP;
 FOREACH name IN ARRAY ARRAY['b'] LOOP
  EXECUTE format('CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION forbid_mutation()', name);
 END LOOP;
END $$;`
	got := agentMigrationLoopStatements(migration)
	for _, want := range []string{
		"ALTER TABLE a FORCE ROW LEVEL SECURITY;",
		"ALTER TABLE b FORCE ROW LEVEL SECURITY;",
		"CREATE POLICY tenant_isolation ON b USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);",
		"CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON b FOR EACH ROW EXECUTE FUNCTION forbid_mutation();",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("loop statements lack %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"ALTER TABLE c ", "ON c ", "DELETE ON a "} {
		if strings.Contains(got, forbidden) {
			t.Errorf("loop statements credit a table its loop does not list (%q):\n%s", forbidden, got)
		}
	}
}
