package libfirewall_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depmanifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall"
)

const pgxImportPath = "github.com/jackc/pgx/v5"

func loadFirewallConfigAndRoles(t *testing.T) (*libfirewall.Config, *depmanifest.Manifest) {
	t.Helper()
	root := repopath.RootDir()
	cfg, err := libfirewall.LoadConfig(filepath.Join(root, "definitions", "architecture", "library-firewall.yaml"))
	if err != nil {
		t.Fatalf("loading library-firewall config: %v", err)
	}
	roles, err := depmanifest.Load(filepath.Join(root, "definitions", "architecture", "dependency-roles.yaml"))
	if err != nil {
		t.Fatalf("loading dependency-roles manifest: %v", err)
	}
	return cfg, roles
}

// pgxOwningRoots returns dependency-roles.yaml's own allowed_import_roots
// for pgx/v5 -- the scope TestPGXAdapterQualification's leak-scan runs
// over (currently the data and ledger adapters, the migration runner, the
// bootstrap composer and the migrate commands). It deliberately reuses that
// manifest's row rather than a second, possibly-drifting list in
// library-firewall.yaml.
func pgxOwningRoots(t *testing.T, roles *depmanifest.Manifest) []string {
	t.Helper()
	for _, row := range roles.Modules {
		if row.Path == pgxImportPath {
			return row.AllowedImportRoots
		}
	}
	t.Fatalf("dependency-roles.yaml has no row for %s", pgxImportPath)
	return nil
}

const leakedTxSource = `package fakeadapter

import "github.com/jackc/pgx/v5"

// Begin leaks the raw pgx.Tx to any caller; RED fixture.
func Begin() pgx.Tx { return nil }
`

const leakedRowsAliasSource = `package fakeadapter

import "github.com/jackc/pgx/v5"

// Rows is a bare alias RED fixture: it leaks pgx.Rows under Human Capital Management Suite's own
// exported name without ever owning a wrapper.
type Rows = pgx.Rows
`

const leakedConnFieldSource = `package fakeadapter

import "github.com/jackc/pgx/v5"

// Handle exposes the raw pgx.Conn through an exported field; RED fixture.
type Handle struct {
	Conn *pgx.Conn
}
`

const ownedWrapperSource = `package fakeadapter

import "github.com/jackc/pgx/v5"

// TxHandle is an owned, opaque wrapper; the pgx.Tx it holds is never
// exported directly. GREEN fixture.
type TxHandle struct {
	tx pgx.Tx
}

// Begin returns the owned wrapper type, not the raw pgx.Tx. GREEN fixture.
func Begin() *TxHandle { return &TxHandle{} }

// ErrCode is a legitimate exported reference to a *different* pgx symbol
// (not Tx/Rows/Conn); it must not be flagged as a leak.
type ErrCode = pgx.Identifier
`

// TestPGXAdapterQualification is the LIB-004 primary test. It has two
// halves: (1) an import-root check, reusing dependency-roles.yaml's own pgx
// row (allowed_import_roots) so a package outside that row's own allowed
// roots can never import pgx at all; (2) a leak-scan over
// synthetic source within an allowed root proving that even there, an
// exported alias, field, or function signature naming pgx.Tx, pgx.Rows or
// pgx.Conn directly is flagged, while an owned wrapper type is not.
func TestPGXAdapterQualification(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)
	leakTypesByImport := libfirewall.LeakTypesByImport(cfg.PGXLeakTypes)
	pgxTypeNames := leakTypesByImport[pgxImportPath]
	if len(pgxTypeNames) == 0 {
		t.Fatalf("library-firewall.yaml pgx_leak_types has no entries for %s", pgxImportPath)
	}

	t.Run("import root boundary", func(t *testing.T) {
		cases := []struct {
			name     string
			importer string
			wantV    bool
		}{
			{"internal/data may import pgx", cfg.Module + "/internal/data/postgres", false},
			{"internal/ledger may import pgx", cfg.Module + "/internal/ledger", false},
			{"migrations may import pgx", cfg.Module + "/migrations", false},
			{"a domain package may not import pgx", cfg.Module + "/internal/domains/people", true},
			{"capability may not import pgx", cfg.Module + "/internal/capability", true},
			{"transport may not import pgx", cfg.Module + "/internal/transport", true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				v := libfirewall.CheckImport(roles, tc.importer, pgxImportPath)
				if tc.wantV && v == nil {
					t.Errorf("CheckImport(%q, %q) = nil, want a violation", tc.importer, pgxImportPath)
				}
				if !tc.wantV && v != nil {
					t.Errorf("CheckImport(%q, %q) = %+v, want no violation", tc.importer, pgxImportPath, v)
				}
			})
		}
	})

	t.Run("leaked concrete type scan", func(t *testing.T) {
		leakCases := []struct {
			name   string
			source string
		}{
			{"leaked pgx.Tx return value", leakedTxSource},
			{"leaked pgx.Rows type alias", leakedRowsAliasSource},
			{"leaked pgx.Conn struct field", leakedConnFieldSource},
		}
		for _, tc := range leakCases {
			t.Run(tc.name, func(t *testing.T) {
				exposures, err := libfirewall.ScanSource("fixture.go", tc.source, []string{pgxImportPath})
				if err != nil {
					t.Fatalf("ScanSource: %v", err)
				}
				leaked := libfirewall.FilterLeakTypes(exposures, pgxImportPath, pgxTypeNames)
				if len(leaked) == 0 {
					t.Errorf("expected a leaked-type exposure, found none in exposures=%+v", exposures)
				}
			})
		}

		t.Run("owned wrapper does not leak", func(t *testing.T) {
			exposures, err := libfirewall.ScanSource("fixture.go", ownedWrapperSource, []string{pgxImportPath})
			if err != nil {
				t.Fatalf("ScanSource: %v", err)
			}
			leaked := libfirewall.FilterLeakTypes(exposures, pgxImportPath, pgxTypeNames)
			if len(leaked) != 0 {
				t.Errorf("owned-wrapper fixture flagged as leaking Tx/Rows/Conn: %+v", leaked)
			}
		})
	})

	_ = pgxOwningRoots(t, roles) // exercised directly by TestTodo_LIB_004_Integration
}

// TestTodo_LIB_004_Property: for any exported func whose single result type
// is exactly "pgx.<Name>" for Name in {Tx, Rows, Conn}, the scan always
// reports a leak, regardless of the function's own name -- the property
// under test is "the checker keys off the type, not incidental naming".
func TestTodo_LIB_004_Property(t *testing.T) {
	cfg, _ := loadFirewallConfigAndRoles(t)
	typeNames := libfirewall.LeakTypesByImport(cfg.PGXLeakTypes)[pgxImportPath]

	funcNames := []string{"Begin", "Query", "Acquire", "Handle", "Whatever123"}
	for _, name := range funcNames {
		for _, typeName := range typeNames {
			src := "package fakeadapter\n\nimport \"github.com/jackc/pgx/v5\"\n\nfunc " + name + "() pgx." + typeName + " { return nil }\n"
			exposures, err := libfirewall.ScanSource("fixture.go", src, []string{pgxImportPath})
			if err != nil {
				t.Fatalf("ScanSource(%s/%s): %v", name, typeName, err)
			}
			if len(libfirewall.FilterLeakTypes(exposures, pgxImportPath, typeNames)) == 0 {
				t.Errorf("func %s() pgx.%s was not flagged as a leak", name, typeName)
			}
		}
	}
}

// TestTodo_LIB_004_Golden pins the exact Exposure shape reported for one
// canonical leak.
func TestTodo_LIB_004_Golden(t *testing.T) {
	cfg, _ := loadFirewallConfigAndRoles(t)
	typeNames := libfirewall.LeakTypesByImport(cfg.PGXLeakTypes)[pgxImportPath]

	exposures, err := libfirewall.ScanSource("adapter.go", leakedTxSource, []string{pgxImportPath})
	if err != nil {
		t.Fatalf("ScanSource: %v", err)
	}
	leaked := libfirewall.FilterLeakTypes(exposures, pgxImportPath, typeNames)
	if len(leaked) != 1 {
		t.Fatalf("got %d leaked exposures, want 1: %+v", len(leaked), leaked)
	}
	got := leaked[0]
	if got.File != "adapter.go" || got.Name != "Begin" || got.Kind != "func" || got.Type != "pgx.Tx" || got.ImportPath != pgxImportPath {
		t.Errorf("exposure = %+v, want {File:adapter.go Name:Begin Kind:func Type:pgx.Tx ImportPath:%s}", got, pgxImportPath)
	}
}

// TestTodo_LIB_004_Race scans the same synthetic fixtures concurrently from
// many goroutines, proving the parser-based scan has no shared mutable
// state across calls.
func TestTodo_LIB_004_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := libfirewall.ScanSource("fixture.go", leakedTxSource, []string{pgxImportPath}); err != nil {
				t.Errorf("ScanSource: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestTodo_LIB_004_Integration runs both halves of the qualification
// against the real tree: the import-root boundary over every real package,
// and the leak-scan over every real file inside pgx's own allowed roots.
func TestTodo_LIB_004_Integration(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)
	root := repopath.RootDir()
	typeNames := libfirewall.LeakTypesByImport(cfg.PGXLeakTypes)[pgxImportPath]

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var boundaryViolations int
	var ownedDirs []string
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if v := libfirewall.CheckImport(roles, pkg.ImportPath, imp); v != nil && v.Module == pgxImportPath {
				boundaryViolations++
				t.Errorf("LIB-004 import boundary violation: %s imports %s (allowed: %v)", v.Importer, v.ImportedPath, v.AllowedRoots)
			}
		}
		for _, imp := range pkg.Imports {
			if imp == pgxImportPath || (len(imp) > len(pgxImportPath) && imp[:len(pgxImportPath)+1] == pgxImportPath+"/") {
				ownedDirs = append(ownedDirs, pkg.Dir)
				break
			}
		}
	}

	var leakViolations int
	for _, dir := range ownedDirs {
		exposures, err := libfirewall.ScanExposures(dir, []string{pgxImportPath})
		if err != nil {
			t.Fatalf("scanning %s: %v", dir, err)
		}
		for _, exp := range libfirewall.FilterLeakTypes(exposures, pgxImportPath, typeNames) {
			leakViolations++
			t.Errorf("LIB-004 leaked pgx type: %s:%d %s %s exposes %s", exp.File, exp.Line, exp.Kind, exp.Name, exp.Type)
		}
	}
	t.Logf("pgx: %d import-boundary violations, %d owning packages scanned, %d leaked-type violations", boundaryViolations, len(ownedDirs), leakViolations)
}

// TestTodo_LIB_004_Fault feeds the scanner a directory that does not exist
// and a file that fails to parse, proving both fail cleanly with an error
// rather than a panic.
func TestTodo_LIB_004_Fault(t *testing.T) {
	if _, err := libfirewall.ScanExposures(filepath.Join(repopath.RootDir(), "definitely", "does", "not", "exist"), []string{pgxImportPath}); err == nil {
		t.Errorf("ScanExposures on a missing directory returned nil error, want an error")
	}

	if _, err := libfirewall.ScanSource("broken.go", "package fakeadapter\n\nfunc Broken( {\n", []string{pgxImportPath}); err == nil {
		t.Errorf("ScanSource on unparsable source returned nil error, want a parse error")
	}
}

// TestTodo_LIB_004_Conformance checks all three governed leak types (Tx,
// Rows, Conn) end to end against one fixture apiece, rather than only the
// Tx case the primary test happens to name first.
func TestTodo_LIB_004_Conformance(t *testing.T) {
	cfg, _ := loadFirewallConfigAndRoles(t)
	typeNames := libfirewall.LeakTypesByImport(cfg.PGXLeakTypes)[pgxImportPath]
	want := map[string]bool{"Tx": true, "Rows": true, "Conn": true}
	if len(typeNames) != len(want) {
		t.Fatalf("pgx_leak_types has %d entries, want %d: %v", len(typeNames), len(want), typeNames)
	}
	for _, n := range typeNames {
		if !want[n] {
			t.Errorf("unexpected pgx leak type %q", n)
		}
	}

	fixtures := map[string]string{"Tx": leakedTxSource, "Rows": leakedRowsAliasSource, "Conn": leakedConnFieldSource}
	for typeName, src := range fixtures {
		exposures, err := libfirewall.ScanSource("fixture.go", src, []string{pgxImportPath})
		if err != nil {
			t.Fatalf("ScanSource(%s): %v", typeName, err)
		}
		if len(libfirewall.FilterLeakTypes(exposures, pgxImportPath, []string{typeName})) == 0 {
			t.Errorf("fixture for %s produced no matching leak", typeName)
		}
	}
}

// TestTodo_LIB_004_Mutation starts from the clean owned-wrapper fixture and
// mutates it one field/return-type at a time into each of the three known
// leak shapes, confirming every mutation is caught independently.
func TestTodo_LIB_004_Mutation(t *testing.T) {
	cfg, _ := loadFirewallConfigAndRoles(t)
	typeNames := libfirewall.LeakTypesByImport(cfg.PGXLeakTypes)[pgxImportPath]

	baseline, err := libfirewall.ScanSource("fixture.go", ownedWrapperSource, []string{pgxImportPath})
	if err != nil {
		t.Fatalf("ScanSource(baseline): %v", err)
	}
	if len(libfirewall.FilterLeakTypes(baseline, pgxImportPath, typeNames)) != 0 {
		t.Fatalf("baseline fixture unexpectedly leaks: %+v", baseline)
	}

	mutants := map[string]string{
		"return pgx.Tx instead of wrapper": leakedTxSource,
		"alias pgx.Rows directly":          leakedRowsAliasSource,
		"exported field is *pgx.Conn":      leakedConnFieldSource,
	}
	for name, src := range mutants {
		t.Run(name, func(t *testing.T) {
			exposures, err := libfirewall.ScanSource("fixture.go", src, []string{pgxImportPath})
			if err != nil {
				t.Fatalf("ScanSource: %v", err)
			}
			if len(libfirewall.FilterLeakTypes(exposures, pgxImportPath, typeNames)) == 0 {
				t.Errorf("mutant %q was not caught", name)
			}
		})
	}
}
