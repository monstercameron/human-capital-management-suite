package depmanifest_test

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depmanifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func loadManifest(t *testing.T) *depmanifest.Manifest {
	t.Helper()
	root := repopath.RootDir()
	m, err := depmanifest.Load(filepath.Join(root, "definitions", "architecture", "dependency-roles.yaml"))
	if err != nil {
		t.Fatalf("loading dependency-roles manifest: %v", err)
	}
	return m
}

// TestDependencyRoleManifestRejectsUnclassifiedModule is the LIB-001
// primary test.
func TestDependencyRoleManifestRejectsUnclassifiedModule(t *testing.T) {
	m := loadManifest(t)

	t.Run("synthetic fixtures", func(t *testing.T) {
		cases := []struct {
			name         string
			modulePath   string
			wantFound    bool
			wantRole     string
			wantExactRow bool
		}{
			{"exact row: jackc/pgx", "github.com/jackc/pgx/v5", true, depmanifest.RoleInfrastructureMechanic, true},
			{"exact row: goose", "github.com/pressly/goose/v3", true, depmanifest.RoleInfrastructureMechanic, true},
			{"exact row: staticcheck is dev-only", "honnef.co/go/tools", true, depmanifest.RoleDevTestOnly, true},
			{"exact row: grpc is not PROJECT_CORE", "google.golang.org/grpc", true, depmanifest.RoleInfrastructureMechanic, true},
			{"family rule: unlisted golang.org/x module", "golang.org/x/crypto", true, depmanifest.RoleInfrastructureMechanic, false},
			{"family rule: unlisted google.golang.org module", "google.golang.org/appengine", true, depmanifest.RoleInfrastructureMechanic, false},
			{"unclassified module", "github.com/example/unclassified-thing", false, "", false},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				c := m.Classify(tc.modulePath)
				if c.Found != tc.wantFound {
					t.Fatalf("Classify(%q).Found = %v, want %v", tc.modulePath, c.Found, tc.wantFound)
				}
				if !tc.wantFound {
					return
				}
				if c.Row.Role != tc.wantRole {
					t.Errorf("Classify(%q).Row.Role = %q, want %q", tc.modulePath, c.Row.Role, tc.wantRole)
				}
				if c.Exact != tc.wantExactRow {
					t.Errorf("Classify(%q).Exact = %v, want %v", tc.modulePath, c.Exact, tc.wantExactRow)
				}
				if c.Row.Role == depmanifest.RoleProjectCore && !m.IsProjectCoreEligible(tc.modulePath) {
					t.Errorf("Classify(%q) returned PROJECT_CORE but %q is not in project_core_reserved", tc.modulePath, tc.modulePath)
				}
			})
		}
	})

	t.Run("no module row is PROJECT_CORE unless reserved", func(t *testing.T) {
		for _, row := range m.Modules {
			if row.Role == depmanifest.RoleProjectCore && !m.IsProjectCoreEligible(row.Path) {
				t.Errorf("module %s is classified PROJECT_CORE but is not one of project_core_reserved (Go, GWC, grpcbridge, SchemaFlux)", row.Path)
			}
		}
	})

	t.Run("every manifest row is complete", func(t *testing.T) {
		for _, row := range m.Modules {
			if missing := depmanifest.RowIsComplete(row); len(missing) > 0 {
				t.Errorf("module %s manifest row is missing fields: %v", row.Path, missing)
			}
		}
	})

	t.Run("every go.mod require is classified", func(t *testing.T) {
		root := repopath.RootDir()
		requires, err := depmanifest.ParseGoModRequires(root)
		if err != nil {
			t.Fatalf("parsing go.mod requires: %v", err)
		}
		if len(requires) == 0 {
			t.Fatalf("go.mod has no require block")
		}

		for _, req := range requires {
			c := m.Classify(req.Path)
			if !c.Found {
				t.Errorf("go.mod requires %s (%s) but dependency-roles.yaml classifies neither an exact row nor a matching family_rules prefix for it", req.Path, req.Version)
				continue
			}
			if c.Row.Role == depmanifest.RoleProjectCore && !m.IsProjectCoreEligible(req.Path) {
				t.Errorf("go.mod dependency %s is classified PROJECT_CORE but is not project_core_reserved", req.Path)
			}
			if c.Exact {
				if missing := depmanifest.RowIsComplete(c.Row); len(missing) > 0 {
					t.Errorf("go.mod dependency %s manifest row is missing fields: %v", req.Path, missing)
				}
			}
		}
	})
}

// TestTodo_LIB_001_Property checks classification invariants across every
// declared requirement and every explicit manifest row.
func TestTodo_LIB_001_Property(t *testing.T) {
	m := loadManifest(t)
	root := repopath.RootDir()
	requires, err := depmanifest.ParseGoModRequires(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range requires {
		c := m.Classify(req.Path)
		if !c.Found {
			t.Errorf("%s is unclassified", req.Path)
			continue
		}
		if c.Row.Role == depmanifest.RoleProjectCore && !m.IsProjectCoreEligible(req.Path) {
			t.Errorf("unreserved module %s classified PROJECT_CORE", req.Path)
		}
	}
	for _, row := range m.Modules {
		if missing := depmanifest.RowIsComplete(row); len(missing) != 0 {
			t.Errorf("%s missing %v", row.Path, missing)
		}
	}
}

// TestTodo_LIB_001_Golden pins the named infrastructure candidates and their
// role so a manifest rewrite cannot silently turn mechanics into semantics.
func TestTodo_LIB_001_Golden(t *testing.T) {
	m := loadManifest(t)
	for _, name := range []string{"google.golang.org/protobuf", "google.golang.org/grpc", "github.com/jackc/pgx/v5", "github.com/cockroachdb/apd/v3", "go.opentelemetry.io/otel", "github.com/pressly/goose/v3"} {
		c := m.Classify(name)
		if !c.Found || c.Row.Role != depmanifest.RoleInfrastructureMechanic {
			t.Errorf("%s classification = %+v, want infrastructure mechanic", name, c)
		}
	}
}

// TestTodo_LIB_001_Integration cross-checks the checked-in go.mod against the
// checked-in YAML manifest using the actual parsers.
func TestTodo_LIB_001_Integration(t *testing.T) {
	m := loadManifest(t)
	requires, err := depmanifest.ParseGoModRequires(repopath.RootDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(requires) < 10 {
		t.Fatalf("parsed only %d requirements", len(requires))
	}
	for _, req := range requires {
		if c := m.Classify(req.Path); !c.Found {
			t.Errorf("go.mod module %s@%s has no manifest classification", req.Path, req.Version)
		}
	}
}

// TestTodo_LIB_001_Security ensures no role can bypass the closed role set
// and every project-core classification is reserved by name.
func TestTodo_LIB_001_Security(t *testing.T) {
	m := loadManifest(t)
	for _, row := range m.Modules {
		if !depmanifest.ValidRole(row.Role) {
			t.Errorf("%s has invalid role %q", row.Path, row.Role)
		}
		if row.Role == depmanifest.RoleProjectCore && !m.IsProjectCoreEligible(row.Path) {
			t.Errorf("unreserved core module %s", row.Path)
		}
	}
	if depmanifest.ValidRole("PROJECT_CORE; INFRASTRUCTURE_MECHANIC") {
		t.Fatal("compound role unexpectedly accepted")
	}
}

// TestTodo_LIB_001_Conformance verifies family defaults supply the same
// ownership and replacement fields required of exact rows.
func TestTodo_LIB_001_Conformance(t *testing.T) {
	m := loadManifest(t)
	for _, name := range []string{"golang.org/x/example", "google.golang.org/example", "go.opentelemetry.io/example"} {
		c := m.Classify(name)
		if !c.Found {
			t.Errorf("family %s not classified", name)
			continue
		}
		if c.Row.Role != depmanifest.RoleInfrastructureMechanic || c.Row.SemanticOwner == "" || c.Row.SecurityOwner == "" || c.Row.LicenseOwner == "" || c.Row.UpgradeSLA == "" || c.Row.Exposure == "" || c.Row.ReplacementStrategy == "" {
			t.Errorf("family %s lacks complete mechanic ownership policy: %+v", name, c.Row)
		}
		if !c.Exact && len(c.Row.AllowedImportRoots) == 0 && name == "go.opentelemetry.io/example" {
			t.Errorf("OTel family has no allowed adapter roots")
		}
	}
}
