package libfirewall_test

import (
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depmanifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall"
)

func loadRoleManifest(t *testing.T) *depmanifest.Manifest {
	t.Helper()
	root := repopath.RootDir()
	m, err := depmanifest.Load(filepath.Join(root, "definitions", "architecture", "dependency-roles.yaml"))
	if err != nil {
		t.Fatalf("loading dependency-roles manifest: %v", err)
	}
	return m
}

// TestThirdPartySemanticFirewall is the LIB-002 primary test. It injects
// pgx/apd/OTel/goose/protobuf/grpc imports into Human Capital Management Suite's semantic-contract
// package roots (definitions, intent, capability, workflow, domain models,
// ledger, gen, and an arbitrary "owner port" package) and expects a
// violation naming the forbidden importer/module/rule for every edge whose
// importer root is not in that module's dependency-roles.yaml
// allowed_import_roots.
func TestThirdPartySemanticFirewall(t *testing.T) {
	m := loadRoleManifest(t)
	mod := m.Module

	cases := []struct {
		name       string
		importer   string
		imported   string
		wantModule string // "" means no violation expected
	}{
		{
			name:       "intent definitions importing pgx directly",
			importer:   mod + "/internal/intent/definitions",
			imported:   "github.com/jackc/pgx/v5",
			wantModule: "github.com/jackc/pgx/v5",
		},
		{
			name:       "capability importing a pgx subpackage",
			importer:   mod + "/internal/capability",
			imported:   "github.com/jackc/pgx/v5/pgxpool",
			wantModule: "github.com/jackc/pgx/v5",
		},
		{
			name:       "workflow definitions importing apd",
			importer:   mod + "/internal/workflow/definition",
			imported:   "github.com/cockroachdb/apd/v3",
			wantModule: "github.com/cockroachdb/apd/v3",
		},
		{
			name:       "domain model importing apd",
			importer:   mod + "/internal/domains/people/model",
			imported:   "github.com/cockroachdb/apd/v3",
			wantModule: "github.com/cockroachdb/apd/v3",
		},
		{
			name:       "ledger event package importing goose",
			importer:   mod + "/internal/ledger",
			imported:   "github.com/pressly/goose/v3",
			wantModule: "github.com/pressly/goose/v3",
		},
		{
			name:       "an owner port package importing OpenTelemetry",
			importer:   mod + "/internal/governance",
			imported:   "go.opentelemetry.io/otel/trace",
			wantModule: "go.opentelemetry.io/otel/trace",
		},
		{
			name:       "gen (wire tree) importing pgx",
			importer:   mod + "/gen/go/hcm/v1",
			imported:   "github.com/jackc/pgx/v5",
			wantModule: "github.com/jackc/pgx/v5",
		},

		// Allowed: the manifest's own allowed_import_roots.
		{
			name:     "internal/data importing pgx is allowed",
			importer: mod + "/internal/data/postgres",
			imported: "github.com/jackc/pgx/v5",
		},
		{
			name:     "internal/kernel importing apd is allowed",
			importer: mod + "/internal/kernel/values",
			imported: "github.com/cockroachdb/apd/v3",
		},
		{
			name:     "migrations importing goose is allowed",
			importer: mod + "/migrations",
			imported: "github.com/pressly/goose/v3",
		},
		{
			name:     "gen importing protobuf is allowed",
			importer: mod + "/gen/go/hcm/v1",
			imported: "google.golang.org/protobuf/proto",
		},
		{
			name:     "internal/transport importing grpc is allowed",
			importer: mod + "/internal/transport/grpcserver",
			imported: "google.golang.org/grpc",
		},
		{
			name:     "an unclassified third-party module is LIB-001's concern, not this firewall's",
			importer: mod + "/internal/domains/people",
			imported: "github.com/example/unclassified-thing",
		},
		{
			name:     "an import outside the module is not this package's concern",
			importer: "some/other/module/pkg",
			imported: "github.com/jackc/pgx/v5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := libfirewall.CheckImport(m, tc.importer, tc.imported)
			if tc.wantModule == "" {
				if v != nil {
					t.Fatalf("CheckImport(%q, %q) = %+v, want no violation", tc.importer, tc.imported, v)
				}
				return
			}
			if v == nil {
				t.Fatalf("CheckImport(%q, %q) = nil, want a violation on module %q", tc.importer, tc.imported, tc.wantModule)
			}
			if v.Module != tc.wantModule {
				t.Errorf("violation.Module = %q, want %q", v.Module, tc.wantModule)
			}
		})
	}
}

// TestTodo_LIB_002_Property is the LIB-002 PROPERTY test: for every module row
// classified with a non-empty allowed_import_roots list, an importer
// synthesized from an arbitrary unrelated internal root is always rejected,
// and an importer synthesized as one of the row's own allowed roots is
// always accepted. This holds independent of which specific module or root
// is picked, so it is checked across every row rather than a hand-picked
// few.
func TestTodo_LIB_002_Property(t *testing.T) {
	m := loadRoleManifest(t)

	const decoyRoot = "internal/definitely-not-an-allowed-root"

	for _, row := range m.Modules {
		row := row
		if len(row.AllowedImportRoots) == 0 {
			continue // an always-forbidden-until-classified row can't assert an "allowed" half
		}
		t.Run(row.Path, func(t *testing.T) {
			decoyImporter := m.Module + "/" + decoyRoot
			if v := libfirewall.CheckImport(m, decoyImporter, row.Path); v == nil {
				t.Errorf("CheckImport(%q, %q) = nil, want a violation (importer is not in %v)", decoyImporter, row.Path, row.AllowedImportRoots)
			}

			for _, root := range row.AllowedImportRoots {
				allowedImporter := m.Module + "/" + root
				if v := libfirewall.CheckImport(m, allowedImporter, row.Path); v != nil {
					t.Errorf("CheckImport(%q, %q) = %+v, want no violation (root is in allowed_import_roots)", allowedImporter, row.Path, v)
				}
			}
		})
	}
}

// TestTodo_LIB_002_Golden pins the exact violation shape (importer/module/role/
// allowed-roots) libfirewall reports for one representative forbidden edge,
// so a change to the violation contract's fields is a visible diff rather
// than a silently different but still-"non-nil" struct.
func TestTodo_LIB_002_Golden(t *testing.T) {
	m := loadRoleManifest(t)
	importer := m.Module + "/internal/domains/people"
	imported := "github.com/jackc/pgx/v5"

	v := libfirewall.CheckImport(m, importer, imported)
	if v == nil {
		t.Fatalf("CheckImport(%q, %q) = nil, want a violation", importer, imported)
	}

	want := libfirewall.Violation{
		Importer:     "internal/domains/people",
		ImportedPath: "github.com/jackc/pgx/v5",
		Module:       "github.com/jackc/pgx/v5",
		Role:         depmanifest.RoleInfrastructureMechanic,
		AllowedRoots: []string{"cmd/hcmnext", "cmd/migrate", "internal/data", "internal/ledger", "internal/platform/bootstrap", "migrations", "tools/chatload", "tools/policy/migrationci/cmd/migrationci"},
	}
	if v.Importer != want.Importer || v.ImportedPath != want.ImportedPath || v.Module != want.Module || v.Role != want.Role {
		t.Errorf("violation = %+v, want %+v", *v, want)
	}
	sort.Strings(v.AllowedRoots)
	sort.Strings(want.AllowedRoots)
	if len(v.AllowedRoots) != len(want.AllowedRoots) {
		t.Fatalf("violation.AllowedRoots = %v, want %v", v.AllowedRoots, want.AllowedRoots)
	}
	for i := range want.AllowedRoots {
		if v.AllowedRoots[i] != want.AllowedRoots[i] {
			t.Errorf("violation.AllowedRoots = %v, want %v", v.AllowedRoots, want.AllowedRoots)
		}
	}
}

// TestTodo_LIB_002_Security rejects a root-name prefix bypass. An importer
// named internal/data-shadow is not an internal/data subpackage and therefore
// must never inherit data's permission to use the PostgreSQL driver. This
// keeps a package-name choice from becoming a path-based policy escalation.
func TestTodo_LIB_002_Security(t *testing.T) {
	m := loadRoleManifest(t)
	for _, tc := range []struct {
		name      string
		importer  string
		wantClean bool
	}{
		{"exact approved root", "internal/data", true},
		{"approved subpackage", "internal/data/pgxadapter", true},
		{"prefix lookalike is forbidden", "internal/data-shadow", false},
		{"nested prefix lookalike is forbidden", "internal/data-shadow/store", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := libfirewall.CheckImport(m, m.Module+"/"+tc.importer, "github.com/jackc/pgx/v5")
			if (v == nil) != tc.wantClean {
				t.Fatalf("CheckImport(%q, pgx) violation=%v, want clean=%v", tc.importer, v != nil, tc.wantClean)
			}
		})
	}
}

// TestTodo_LIB_002_Integration is the LIB-002 INTEGRATION test: it runs the
// firewall over the real `go list -json ./...` import graph of the current
// tree and reports every real forbidden edge found in HEAD. Per the
// orchestrator's instructions this test does not weaken the rule to pass;
// any real violation found here is surfaced via t.Errorf and must be fixed
// in the offending package, not in this checker.
func TestTodo_LIB_002_Integration(t *testing.T) {
	m := loadRoleManifest(t)
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("go list returned no packages")
	}

	var total int
	for _, pkg := range pkgs {
		for _, v := range libfirewall.CheckPackage(m, pkg.ImportPath, pkg.Imports) {
			total++
			t.Errorf("third-party semantic firewall violation: %s imports %s (role %s), allowed roots: %v",
				v.Importer, v.ImportedPath, v.Role, v.AllowedRoots)
		}
	}
	t.Logf("scanned %d packages, %d firewall violations", len(pkgs), total)
}

// TestTodo_LIB_002_Conformance runs a small canonical vector set (one
// representative edge per manifest role family Human Capital Management Suite currently
// classifies) end to end, so the firewall's role-handling stays correct as
// dependency-roles.yaml grows new rows.
func TestTodo_LIB_002_Conformance(t *testing.T) {
	m := loadRoleManifest(t)

	vectors := []struct {
		importer      string
		imported      string
		wantViolation bool
	}{
		{m.Module + "/internal/data", "github.com/jackc/pgx/v5", false},                   // INFRASTRUCTURE_MECHANIC, allowed root
		{m.Module + "/internal/domains/people", "github.com/jackc/pgx/v5", true},          // INFRASTRUCTURE_MECHANIC, wrong root
		{m.Module + "/tools/policy", "gopkg.in/yaml.v3", false},                           // DEV_TEST_ONLY, allowed root
		{m.Module + "/internal/domains/people", "gopkg.in/yaml.v3", true},                 // DEV_TEST_ONLY, wrong root
		{m.Module + "/internal/domains/people", "github.com/example/unclassified", false}, // unclassified: not this firewall's job
	}

	for _, v := range vectors {
		got := libfirewall.CheckImport(m, v.importer, v.imported)
		if (got != nil) != v.wantViolation {
			t.Errorf("CheckImport(%q, %q) violation=%v, want %v", v.importer, v.imported, got != nil, v.wantViolation)
		}
	}
}

// TestTodo_LIB_002_Mutation starts from a known-clean edge (internal/data
// importing pgx, which is allowed) and mutates exactly one field at a time
// -- the importer root, the imported module -- confirming each mutation is
// independently caught. A checker that only "got lucky" on the original
// combination would pass one of these mutants without this test.
func TestTodo_LIB_002_Mutation(t *testing.T) {
	m := loadRoleManifest(t)

	if v := libfirewall.CheckImport(m, m.Module+"/internal/data", "github.com/jackc/pgx/v5"); v != nil {
		t.Fatalf("baseline edge unexpectedly flagged: %+v", v)
	}

	mutants := []struct {
		name     string
		importer string
		imported string
	}{
		{"mutate importer root", m.Module + "/internal/domains/people", "github.com/jackc/pgx/v5"},
		{"mutate imported module", m.Module + "/internal/data", "go.opentelemetry.io/otel"},
	}
	for _, mu := range mutants {
		t.Run(mu.name, func(t *testing.T) {
			if v := libfirewall.CheckImport(m, mu.importer, mu.imported); v == nil {
				t.Errorf("mutant (%s, %s) was not caught", mu.importer, mu.imported)
			}
		})
	}
}

// TestTodo_LIB_002_Race runs CheckImport/CheckPackage concurrently across many
// goroutines sharing one loaded *depmanifest.Manifest, so `go test -race`
// can prove the read-only checker introduces no data race even though the
// manifest is loaded once and fanned out.
func TestTodo_LIB_002_Race(t *testing.T) {
	m := loadRoleManifest(t)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			importer := m.Module + "/internal/domains/people"
			imported := "github.com/jackc/pgx/v5"
			if i%2 == 0 {
				importer = m.Module + "/internal/data"
			}
			_ = libfirewall.CheckImport(m, importer, imported)
			_ = libfirewall.CheckPackage(m, importer, []string{imported, "fmt", "os"})
		}(i)
	}
	wg.Wait()
}
