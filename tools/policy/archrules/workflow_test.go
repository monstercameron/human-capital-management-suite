package archrules_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// TestWorkflowLayersRejectRuntimeCompilerAndDomainPersistenceCycles is the
// ARCH-GO-007 primary test: workflow/definition must not import
// workflow/compiler or workflow/runtime (the model must not know how it is
// compiled or run), workflow/compiler must not import a concrete store, and
// workflow/runtime must not import a domain's own persistence subpackage
// directly (it reaches domain state only through capability results).
func TestWorkflowLayersRejectRuntimeCompilerAndDomainPersistenceCycles(t *testing.T) {
	cfg := loadArchConfig(t)
	wl := cfg.WorkflowLayers

	t.Run("definition must not import compiler or runtime", func(t *testing.T) {
		cases := []struct {
			importer, imported string
			wantV              bool
		}{
			{wl.DefinitionRoot, wl.CompilerRoot, true},
			{wl.DefinitionRoot, wl.RuntimeRoot, true},
			{wl.DefinitionRoot, wl.StepRoot, false},
			{wl.CompilerRoot, wl.DefinitionRoot, false}, // downward, allowed
			{wl.RuntimeRoot, wl.CompilerRoot, false},    // not a forbidden pair here
		}
		for _, tc := range cases {
			v := archrules.CheckForbiddenEdges("workflow-definition-must-not-import-compiler-or-runtime", wl.ForbiddenEdges, tc.importer, tc.imported)
			if (v != nil) != tc.wantV {
				t.Errorf("CheckForbiddenEdges(%q -> %q) violation=%v, want %v", tc.importer, tc.imported, v != nil, tc.wantV)
			}
		}
	})

	t.Run("compiler must not contain a concrete store", func(t *testing.T) {
		cases := []struct {
			path  string
			wantV bool
		}{
			{"internal/workflow/compiler/pg/postgres", true},
			{"internal/workflow/compiler/x/store", true},
			{"internal/workflow/compiler", false},
			{"internal/workflow/compiler/typecheck", false},
		}
		for _, tc := range cases {
			got := archrules.MatchesAnyGlob(wl.CompilerForbiddenContent, tc.path)
			if got != tc.wantV {
				t.Errorf("MatchesAnyGlob(%q) = %v, want %v", tc.path, got, tc.wantV)
			}
		}
	})

	t.Run("runtime must not import domain persistence", func(t *testing.T) {
		cases := []struct {
			path  string
			wantV bool
		}{
			{"internal/domains/people/store", true},
			{"internal/domains/compensation/persistence", true},
			{"internal/domains/promotion/repository", true},
			{"internal/domains/people", false},
			{"internal/domains/people/capability", false},
		}
		for _, tc := range cases {
			got := archrules.MatchesAnyGlob(wl.DomainPersistenceMarkers, tc.path)
			if got != tc.wantV {
				t.Errorf("MatchesAnyGlob(%q) = %v, want %v", tc.path, got, tc.wantV)
			}
		}
	})
}

// TestTodo_ARCH_GO_007_Integration runs every half of the check against the
// real tree.
func TestTodo_ARCH_GO_007_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	wl := cfg.WorkflowLayers
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var edgeViolations, contentViolations, persistViolations int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok {
			continue
		}
		if archrules.UnderRoot(rel, wl.CompilerRoot) && archrules.MatchesAnyGlob(wl.CompilerForbiddenContent, rel) {
			contentViolations++
			t.Errorf("ARCH-GO-007 violation: compiler package %s matches a forbidden concrete-store content marker", rel)
		}
		for _, imp := range pkg.Imports {
			impRel, ok := archrules.TrimModule(cfg.Module, imp)
			if !ok {
				continue
			}
			if v := archrules.CheckForbiddenEdges("workflow-layers", wl.ForbiddenEdges, rel, impRel); v != nil {
				edgeViolations++
				t.Errorf("ARCH-GO-007 violation: %s imports %s (forbidden workflow-layer edge)", rel, impRel)
			}
			if archrules.UnderRoot(rel, wl.RuntimeRoot) && archrules.MatchesAnyGlob(wl.DomainPersistenceMarkers, impRel) {
				persistViolations++
				t.Errorf("ARCH-GO-007 violation: workflow runtime package %s imports domain persistence %s directly", rel, impRel)
			}
		}
	}
	t.Logf("ARCH-GO-007: %d forbidden-edge, %d forbidden-content, %d domain-persistence violations", edgeViolations, contentViolations, persistViolations)
}

// TestTodo_ARCH_GO_007_Conformance checks each declared forbidden_edges
// entry independently, not just the primary test's hand-picked pair.
func TestTodo_ARCH_GO_007_Conformance(t *testing.T) {
	cfg := loadArchConfig(t)
	wl := cfg.WorkflowLayers
	if len(wl.ForbiddenEdges) == 0 {
		t.Fatalf("workflow_layers.forbidden_edges is empty")
	}
	for _, e := range wl.ForbiddenEdges {
		if v := archrules.CheckForbiddenEdges("workflow-layers", wl.ForbiddenEdges, e.From, e.To); v == nil {
			t.Errorf("declared forbidden edge %s -> %s was not caught by its own rule", e.From, e.To)
		}
	}
}

// TestTodo_ARCH_GO_007_Property: for any subpackage nested arbitrarily
// deep under definition_root, importing any subpackage nested under
// compiler_root or runtime_root is always forbidden -- the rule is a root
// prefix match, not tied to the exact top-level path used in the primary
// test.
func TestTodo_ARCH_GO_007_Property(t *testing.T) {
	cfg := loadArchConfig(t)
	wl := cfg.WorkflowLayers

	deepImporters := []string{wl.DefinitionRoot + "/a/b/c", wl.DefinitionRoot + "/x"}
	deepImported := []string{wl.CompilerRoot + "/y/z", wl.RuntimeRoot + "/leases"}
	for _, importer := range deepImporters {
		for _, imported := range deepImported {
			if v := archrules.CheckForbiddenEdges("workflow-layers", wl.ForbiddenEdges, importer, imported); v == nil {
				t.Errorf("deep edge %s -> %s was not caught", importer, imported)
			}
		}
	}
}

func TestTodo_ARCH_GO_007_Golden(t *testing.T) {
	wl := loadArchConfig(t).WorkflowLayers
	if wl.DefinitionRoot != "internal/workflow/definition" || wl.CompilerRoot != "internal/workflow/compiler" || wl.RuntimeRoot != "internal/workflow/runtime" || wl.StepRoot != "internal/workflow/step" {
		t.Fatalf("workflow roots drifted: definition=%q compiler=%q runtime=%q step=%q", wl.DefinitionRoot, wl.CompilerRoot, wl.RuntimeRoot, wl.StepRoot)
	}
	if len(wl.ForbiddenEdges) != 2 || wl.ForbiddenEdges[0] != (archrules.Edge{From: wl.DefinitionRoot, To: wl.CompilerRoot}) || wl.ForbiddenEdges[1] != (archrules.Edge{From: wl.DefinitionRoot, To: wl.RuntimeRoot}) {
		t.Fatalf("workflow forbidden edges = %+v", wl.ForbiddenEdges)
	}
	if len(wl.CompilerForbiddenContent) == 0 || len(wl.DomainPersistenceMarkers) == 0 {
		t.Fatalf("workflow forbidden content/persistence markers must be declared")
	}
}
