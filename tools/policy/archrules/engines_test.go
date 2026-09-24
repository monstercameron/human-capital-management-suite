package archrules_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// TestEnginePackagesRejectDomainAndAdapterImports is the ARCH-GO-008
// primary test. package-dependency-policy.yaml's depedge package already
// enforces "engine must not import a domain implementation"
// (engine-must-not-import-domain-implementation); this test covers the
// complementary invariant ARCH-GO-008 also names: one engine may depend on
// a sibling engine's own root contract package, but never reach into a
// subpackage beneath it (that subpackage is that engine's private
// implementation detail, not a public cross-engine contract).
func TestEnginePackagesRejectDomainAndAdapterImports(t *testing.T) {
	cfg := loadArchConfig(t)
	enginesRoot := cfg.Engines.Root

	cases := []struct {
		name     string
		importer string
		imported string
		wantV    bool
	}{
		{"rules engine importing payband engine's subpackage", enginesRoot + "/rules", enginesRoot + "/payband/tables", true},
		{"rules engine importing payband engine's root contract", enginesRoot + "/rules", enginesRoot + "/payband", false},
		{"an engine importing its own subpackage is fine", enginesRoot + "/payband/tables", enginesRoot + "/payband", false},
		{"an engine importing a domain implementation", enginesRoot + "/rules", "internal/domains/people/aggregate", false}, // out of this rule's scope (depedge's job)
		{"a non-engine package is not this check's concern", "internal/domains/people", enginesRoot + "/rules/internal", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := archrules.CheckEngineCrossImport(enginesRoot, tc.importer, tc.imported)
			if tc.wantV && v == nil {
				t.Errorf("CheckEngineCrossImport(%q, %q) = nil, want a violation", tc.importer, tc.imported)
			}
			if !tc.wantV && v != nil {
				t.Errorf("CheckEngineCrossImport(%q, %q) = %+v, want no violation", tc.importer, tc.imported, v)
			}
		})
	}
}

// TestTodo_ARCH_GO_008_Integration runs the cross-engine check against the
// real tree.
func TestTodo_ARCH_GO_008_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	enginesRoot := cfg.Engines.Root
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var total int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok || !archrules.UnderRoot(rel, enginesRoot) {
			continue
		}
		for _, imp := range pkg.Imports {
			impRel, ok := archrules.TrimModule(cfg.Module, imp)
			if !ok {
				continue
			}
			if v := archrules.CheckEngineCrossImport(enginesRoot, rel, impRel); v != nil {
				total++
				t.Errorf("ARCH-GO-008 violation: %s imports sibling-engine subpackage %s", v.Importer, v.Imported)
			}
		}
	}
	t.Logf("ARCH-GO-008: %d cross-engine subpackage violations", total)
}

// TestTodo_ARCH_GO_008_Property: for any two distinct engine names and any
// non-empty subpackage suffix, importing that subpackage across engines is
// always flagged, while importing the bare sibling root never is.
func TestTodo_ARCH_GO_008_Property(t *testing.T) {
	cfg := loadArchConfig(t)
	enginesRoot := cfg.Engines.Root

	engineNames := []string{"alpha", "beta", "gamma"}
	suffixes := []string{"internal", "tables", "compiler/x"}
	for i, a := range engineNames {
		for j, b := range engineNames {
			if i == j {
				continue
			}
			for _, suffix := range suffixes {
				importer := enginesRoot + "/" + a
				imported := enginesRoot + "/" + b + "/" + suffix
				if v := archrules.CheckEngineCrossImport(enginesRoot, importer, imported); v == nil {
					t.Errorf("CheckEngineCrossImport(%q, %q) = nil, want a violation", importer, imported)
				}
			}
			importer := enginesRoot + "/" + a
			imported := enginesRoot + "/" + b
			if v := archrules.CheckEngineCrossImport(enginesRoot, importer, imported); v != nil {
				t.Errorf("CheckEngineCrossImport(%q, %q) = %+v, want no violation (bare sibling root)", importer, imported, v)
			}
		}
	}
}

func TestTodo_ARCH_GO_008_Golden(t *testing.T) {
	root := loadArchConfig(t).Engines.Root
	if root != "internal/engines" {
		t.Fatalf("engines.root = %q, want internal/engines", root)
	}
	// Public sibling contracts are allowed, implementation subpackages are not.
	if archrules.CheckEngineCrossImport(root, root+"/rules", root+"/payband") != nil {
		t.Fatal("sibling root contract was rejected")
	}
	if archrules.CheckEngineCrossImport(root, root+"/rules", root+"/payband/tables") == nil {
		t.Fatal("sibling implementation subpackage was accepted")
	}
}

func TestTodo_ARCH_GO_008_Conformance(t *testing.T) {
	root := loadArchConfig(t).Engines.Root
	for _, a := range []string{"alpha", "nested/beta", "gamma"} {
		for _, b := range []string{"delta", "epsilon"} {
			if v := archrules.CheckEngineCrossImport(root, root+"/"+a, root+"/"+b+"/private/impl"); v == nil {
				t.Errorf("cross-engine private import %s -> %s was accepted", a, b)
			}
		}
	}
}
