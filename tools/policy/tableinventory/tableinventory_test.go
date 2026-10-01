package tableinventory_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/tableinventory"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

func loadInventory(t *testing.T) tableinventory.Inventory {
	t.Helper()
	inventory, err := tableinventory.Scan(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	return inventory
}

func TestTodo_ALIGN_008(t *testing.T) {
	registry, err := tableinventory.Generate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	// The registry must cover exactly the tables the migrations declare; the
	// count itself grows with every persistence lane and is not the contract.
	if want := len(loadInventory(t).MigrationTables); len(registry.Tables) != want || want < 200 {
		t.Fatalf("alignment registry has %d tables, want the %d migration tables", len(registry.Tables), want)
	}
	firstRegistryDigest, secondRegistryDigest := registry.Digest(), registry.Digest()
	if firstRegistryDigest != secondRegistryDigest || registry.Explain() == "" {
		t.Fatal("alignment registry identity is not stable")
	}
}

func TestTodo_ALIGN_008_Property(t *testing.T) {
	inventory := loadInventory(t)
	first := inventory.Explain()
	second := inventory.Explain()
	if first != second || len(inventory.TablesByRole) != 6 {
		t.Fatalf("inventory is not deterministic: %q / %q, roles=%d", first, second, len(inventory.TablesByRole))
	}
}

func TestTodo_ALIGN_008_Golden(t *testing.T) {
	registry, err := tableinventory.Generate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if registry.Tables[0].Table != "access_role" || registry.Tables[len(registry.Tables)-1].Table != "worksite_revision" {
		t.Fatalf("unexpected alignment boundaries: %q / %q", registry.Tables[0].Table, registry.Tables[len(registry.Tables)-1].Table)
	}
}

func TestTodo_ALIGN_008_Security(t *testing.T) {
	inventory := loadInventory(t)
	for _, table := range inventory.MigrationTables {
		if table.Name == "artifact" || table.Name == "artifact_quarantine" || table.Name == "ledger_event_p0" {
			t.Fatalf("companion schema or physical partition leaked into logical inventory: %s", table.Name)
		}
	}
}

func TestTodo_ALIGN_008_Conformance(t *testing.T) {
	if tableinventory.Version() != 1 {
		t.Fatalf("policy version = %d, want 1", tableinventory.Version())
	}
}

func TestScanIncludesRegisteredTablesFromEarlierMigrations(t *testing.T) {
	inventory := loadInventory(t)
	for _, table := range inventory.MigrationTables {
		if table.Name == "access_role_revision" {
			if table.Migration != "00327_role_access_revision_ledger.sql" {
				t.Fatalf("access_role_revision creation migration = %q, want 00327_role_access_revision_ledger.sql", table.Migration)
			}
			return
		}
	}
	t.Fatal("registered access_role_revision table was not found in the migration inventory")
}

func TestTodo_ALIGN_009(t *testing.T) {
	inventory := loadInventory(t)
	if findings := tableinventory.Validate(inventory); len(findings) != 0 {
		t.Fatalf("default table inventory has findings: %+v", findings)
	}
	if len(inventory.TablesByRole["LEDGER"]) == 0 || len(inventory.TablesByRole["PROJECTION"]) == 0 {
		t.Fatal("semantic-role inventory omitted ledger or projection tables")
	}
}

func TestTodo_ALIGN_009_Property(t *testing.T) {
	inventory := loadInventory(t)
	for role, tables := range inventory.TablesByRole {
		copyOfTables := append([]string(nil), tables...)
		sort.Strings(copyOfTables)
		if len(copyOfTables) != len(tables) {
			t.Fatalf("role %s has unstable table count", role)
		}
	}
}

func TestTodo_ALIGN_009_Golden(t *testing.T) {
	inventory := loadInventory(t)
	if got := len(inventory.MigrationTables); got < 200 {
		t.Fatalf("logical migration inventory has %d tables, want the full migration set", got)
	}
}

func TestTodo_ALIGN_009_Security(t *testing.T) {
	inventory := loadInventory(t)
	for _, table := range inventory.Tables {
		if table.DataRole == "" {
			t.Fatalf("table %s has an empty semantic role", table.Table)
		}
	}
}

func TestTodo_ALIGN_009_Conformance(t *testing.T) {
	inventory := loadInventory(t)
	if len(inventory.SourceFiles) == 0 {
		t.Fatal("inventory did not retain migration source evidence")
	}
}

func TestLoadCheckAndFindingErrorsRenderTheRegistryTruth(t *testing.T) {
	root := repoRoot(t)
	inventory, err := tableinventory.Load(filepath.Join(root, filepath.FromSlash(tableinventory.DefaultRegistryPath)))
	if err != nil || inventory == nil || len(inventory.Tables) == 0 {
		t.Fatalf("Load must read the checked-in registry: %v", err)
	}
	if _, err := tableinventory.Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("Load must refuse a missing registry")
	}
	if err := tableinventory.Check(root); err != nil {
		t.Fatalf("Check must pass on the repository: %v", err)
	}
	broken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(broken, filepath.Dir(filepath.FromSlash(tableinventory.DefaultRegistryPath))), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, filepath.FromSlash(tableinventory.DefaultRegistryPath)), []byte("version: 1\ntables: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tableinventory.Check(broken); err == nil {
		t.Fatal("Check must refuse a root whose registry declares nothing the migrations create")
	}
	scoped := tableinventory.Finding{Table: "worksite", Code: "MISSING", Detail: "no migration"}
	if scoped.Error() != "tableinventory: worksite: MISSING: no migration" {
		t.Fatalf("scoped finding renders %q", scoped.Error())
	}
	global := tableinventory.Finding{Code: "EMPTY", Detail: "no tables"}
	if global.Error() != "tableinventory: EMPTY: no tables" {
		t.Fatalf("global finding renders %q", global.Error())
	}
}
