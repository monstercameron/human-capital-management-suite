package archrules_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// TestGovernanceCompositionRejectsPolicyImplementationImports is the
// ARCH-GO-006 primary test: governance may consume authz/rules results
// through their own package (import downward into a composed subsystem is
// fine), but no composed subsystem (internal/trust/authz,
// internal/engines/rules) may import internal/governance back -- that
// direction would let a subsystem authorize its own output through the
// very composer meant to check it, which is the cycle this rule forbids.
func TestGovernanceCompositionRejectsPolicyImplementationImports(t *testing.T) {
	cfg := loadArchConfig(t)
	gc := cfg.GovernanceComposition

	cases := []struct {
		name     string
		importer string
		imported string
		wantV    bool
	}{
		{"governance importing authz is allowed (composition)", gc.GovernanceRoot, "internal/trust/authz", false},
		{"governance importing the rules engine is allowed (composition)", gc.GovernanceRoot, "internal/engines/rules", false},
		{"authz importing governance is forbidden", "internal/trust/authz", gc.GovernanceRoot, true},
		{"the rules engine importing governance is forbidden", "internal/engines/rules", gc.GovernanceRoot, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isComposedImportingGovernance := false
			for _, root := range gc.ComposedRoots {
				if archrules.UnderRoot(tc.importer, root) && archrules.UnderRoot(tc.imported, gc.GovernanceRoot) {
					isComposedImportingGovernance = true
				}
			}
			if isComposedImportingGovernance != tc.wantV {
				t.Errorf("forbidden(%q -> %q) = %v, want %v", tc.importer, tc.imported, isComposedImportingGovernance, tc.wantV)
			}
		})
	}
}

// TestTodo_ARCH_GO_006_Integration runs the reverse-import check against
// the real tree: no composed subsystem package may import governance.
func TestTodo_ARCH_GO_006_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	gc := cfg.GovernanceComposition
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var total int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok || !archrules.UnderAnyRoot(rel, gc.ComposedRoots) {
			continue
		}
		for _, imp := range pkg.Imports {
			impRel, ok := archrules.TrimModule(cfg.Module, imp)
			if !ok {
				continue
			}
			if archrules.UnderRoot(impRel, gc.GovernanceRoot) {
				total++
				t.Errorf("ARCH-GO-006 violation: composed subsystem package %s imports governance package %s", rel, impRel)
			}
		}
	}
	t.Logf("ARCH-GO-006: %d composed-subsystem-imports-governance violations", total)
}

// TestTodo_ARCH_GO_006_Security checks that governance's own composed set
// has no accidental duplicate or self-referential entry, since a
// misconfigured composed_roots list (e.g. naming internal/governance
// itself) would make every governance-internal edge a false positive
// cycle finding.
func TestTodo_ARCH_GO_006_Security(t *testing.T) {
	cfg := loadArchConfig(t)
	gc := cfg.GovernanceComposition

	seen := map[string]bool{}
	for _, root := range gc.ComposedRoots {
		if archrules.UnderRoot(root, gc.GovernanceRoot) || archrules.UnderRoot(gc.GovernanceRoot, root) {
			t.Errorf("composed root %q overlaps governance_root %q", root, gc.GovernanceRoot)
		}
		if seen[root] {
			t.Errorf("composed root %q listed more than once", root)
		}
		seen[root] = true
	}
}

// TestTodo_ARCH_GO_006_Conformance checks every composed root independently
// against the same forbidden-reverse-import vector.
func TestTodo_ARCH_GO_006_Conformance(t *testing.T) {
	cfg := loadArchConfig(t)
	gc := cfg.GovernanceComposition

	for _, root := range gc.ComposedRoots {
		t.Run(root, func(t *testing.T) {
			if !archrules.UnderRoot(root, root) {
				t.Fatalf("UnderRoot(%q, %q) = false, want true", root, root)
			}
			if !archrules.UnderRoot(gc.GovernanceRoot, gc.GovernanceRoot) {
				t.Fatalf("governance_root does not match itself")
			}
		})
	}
}

func TestTodo_ARCH_GO_006_Golden(t *testing.T) {
	cfg := loadArchConfig(t)
	gc := cfg.GovernanceComposition
	if gc.GovernanceRoot != "internal/governance" {
		t.Fatalf("governance_root = %q", gc.GovernanceRoot)
	}
	want := []string{"internal/trust/authz", "internal/engines/rules"}
	if len(gc.ComposedRoots) != len(want) {
		t.Fatalf("composed_roots = %v, want %v", gc.ComposedRoots, want)
	}
	for i := range want {
		if gc.ComposedRoots[i] != want[i] {
			t.Errorf("composed_roots[%d] = %q, want %q", i, gc.ComposedRoots[i], want[i])
		}
	}
}

func TestTodo_ARCH_GO_006_Mutation(t *testing.T) {
	cfg := loadArchConfig(t)
	gc := cfg.GovernanceComposition
	violates := func(importer, imported string) bool {
		for _, root := range gc.ComposedRoots {
			if archrules.UnderRoot(importer, root) && archrules.UnderRoot(imported, gc.GovernanceRoot) {
				return true
			}
		}
		return false
	}
	for _, root := range gc.ComposedRoots {
		if !violates(root+"/subsystem", gc.GovernanceRoot+"/decision") {
			t.Errorf("reverse-import mutation %q -> governance was not detected", root)
		}
		if violates(gc.GovernanceRoot+"/compose", root+"/contract") {
			t.Errorf("allowed composition import governance -> %q was rejected", root)
		}
	}
}
