package archrules_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// TestHumanInteractionBoundaries is the ARCH-GO-022 primary test: workflow,
// humanwork, forms, and messaging have separate ownership boundaries.
// Specifically:
//   - workflow does not import humanwork's core logic or messaging
//   - humanwork does not import workflow's runtime/execute (but may import
//     workflow's compiled node types)
//   - forms and messaging never import each other's internals
//   - each owner has its own package root
func TestHumanInteractionBoundaries(t *testing.T) {
	const (
		modulePrefix     = "github.com/monstercameron/human-capital-management-suite"
		workflowRoot     = "internal/workflow"
		humanworkRoot    = "internal/humanwork"
		formsRoot        = "internal/forms"
		messagingRoot    = "internal/messaging"
		workflowRuntime  = "internal/workflow/runtime"
		workflowExecute  = "internal/workflow/execute"
		workflowCompiler = "internal/workflow/compiler"
	)

	t.Run("forbidden edges structure", func(t *testing.T) {
		// Define the forbidden edges that represent the ARCH-GO-022 boundaries
		forbiddenEdges := []struct {
			from, to string
			desc     string
		}{
			// workflow must not import humanwork or messaging
			{workflowRoot, humanworkRoot, "workflow must not import humanwork"},
			{workflowRoot, messagingRoot, "workflow must not import messaging"},

			// humanwork must not import workflow's runtime or execute
			// (it may import workflow/compiler or workflow/definition types)
			{humanworkRoot, workflowRuntime, "humanwork must not import workflow/runtime"},
			{humanworkRoot, workflowExecute, "humanwork must not import workflow/execute"},

			// forms and messaging must not import each other
			{formsRoot, messagingRoot, "forms must not import messaging"},
			{messagingRoot, formsRoot, "messaging must not import forms"},
		}

		// Basic sanity: the forbidden edges should be identifiable and distinct
		if len(forbiddenEdges) == 0 {
			t.Errorf("no forbidden edges defined for ARCH-GO-022")
		}

		uniqueFromTo := make(map[string]int)
		for _, e := range forbiddenEdges {
			key := e.from + "->" + e.to
			uniqueFromTo[key]++
		}
		if len(uniqueFromTo) < len(forbiddenEdges) {
			t.Errorf("forbidden edges contain duplicates")
		}
	})

	t.Run("ownership roots are distinct", func(t *testing.T) {
		roots := []string{workflowRoot, humanworkRoot, formsRoot, messagingRoot}
		seen := make(map[string]bool)
		for _, r := range roots {
			if seen[r] {
				t.Errorf("ownership root %q declared multiple times", r)
			}
			seen[r] = true
			if r == "" {
				t.Errorf("ownership root must not be empty")
			}
		}
	})

	t.Run("edge validation", func(t *testing.T) {
		cases := []struct {
			from, to string
			under    bool
			desc     string
		}{
			// workflow importing humanwork should be caught
			{workflowRoot, humanworkRoot, true, "workflow root imports humanwork root"},
			{workflowRoot + "/a", humanworkRoot + "/b", true, "workflow subpkg imports humanwork subpkg"},
			{workflowRoot + "/runtime", humanworkRoot, true, "workflow/runtime imports humanwork"},

			// workflow importing forms should be allowed (not in forbidden list)
			{workflowRoot, formsRoot, false, "workflow importing forms is allowed"},

			// humanwork importing workflow/compiler should be allowed
			{humanworkRoot, workflowCompiler, false, "humanwork importing workflow/compiler is allowed"},

			// humanwork importing workflow/runtime should be caught
			{humanworkRoot, workflowRuntime, true, "humanwork imports workflow/runtime"},

			// forms and messaging crossing should be caught
			{formsRoot, messagingRoot, true, "forms imports messaging"},
			{messagingRoot, formsRoot, true, "messaging imports forms"},
		}

		for _, tc := range cases {
			// Check if importer is under workflow and imported is under humanwork
			fromUnder := archrules.UnderRoot(tc.from, workflowRoot)
			toUnder := archrules.UnderRoot(tc.to, humanworkRoot)
			isWorkflowToHumanwork := fromUnder && toUnder

			// Check if importer is under humanwork and imported is under workflow/runtime
			fromUnderHW := archrules.UnderRoot(tc.from, humanworkRoot)
			toUnderWR := archrules.UnderRoot(tc.to, workflowRuntime)
			isHumanworkToWorkflowRuntime := fromUnderHW && toUnderWR

			// Check if importer is under humanwork and imported is under workflow/execute
			toUnderWE := archrules.UnderRoot(tc.to, workflowExecute)
			isHumanworkToWorkflowExecute := fromUnderHW && toUnderWE

			// Check if importer is under forms and imported is under messaging
			fromUnderForms := archrules.UnderRoot(tc.from, formsRoot)
			toUnderMessaging := archrules.UnderRoot(tc.to, messagingRoot)
			isFormsToMessaging := fromUnderForms && toUnderMessaging

			// Check if importer is under messaging and imported is under forms
			fromUnderMessaging := archrules.UnderRoot(tc.from, messagingRoot)
			toUnderForms := archrules.UnderRoot(tc.to, formsRoot)
			isMessagingToForms := fromUnderMessaging && toUnderForms

			isForbidden := isWorkflowToHumanwork || isHumanworkToWorkflowRuntime ||
				isHumanworkToWorkflowExecute || isFormsToMessaging || isMessagingToForms

			if isForbidden != tc.under {
				t.Errorf("%s: %q -> %q, forbidden=%v, want %v", tc.desc, tc.from, tc.to, isForbidden, tc.under)
			}
		}
	})
}

// TestTodo_ARCH_GO_022_Integration runs the ownership boundary checks against
// the real tree and reports any violations found. Since this is architecture
// enforcement, any real violations found must be allowlisted with a count
// comment explaining why they are necessary.
func TestTodo_ARCH_GO_022_Integration(t *testing.T) {
	const (
		module          = "github.com/monstercameron/human-capital-management-suite"
		workflowRoot    = "internal/workflow"
		humanworkRoot   = "internal/humanwork"
		formsRoot       = "internal/forms"
		messagingRoot   = "internal/messaging"
		workflowRuntime = "internal/workflow/runtime"
		workflowExecute = "internal/workflow/execute"
	)

	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var (
		workflowToHumanwork int
		workflowToMessaging int
		humanworkToRuntime  int
		humanworkToExecute  int
		formsToMessaging    int
		messagingToForms    int
	)

	// Allowlisted workflow->humanwork edges (count: 14)
	// These represent existing coupling where workflow packages (execute, inspect, prototype,
	// simulate, and step specializations like approval and task) depend on humanwork/workitem.
	// This is under review for refactoring to reduce coupling; see ARCH-GO-022 GREEN for
	// target separation.
	allowlistedWorkflowToHumanwork := map[string]bool{
		"internal/workflow/execute->internal/humanwork/workitem":            true, // execute needs to access work items
		"internal/workflow/execute/effects->internal/humanwork/workitem":    true, // effects need to update work item state
		"internal/workflow/inspect->internal/humanwork/workitem":            true, // inspection needs work item visibility
		"internal/workflow/prototype->internal/humanwork":                   true, // prototyping accesses humanwork package
		"internal/workflow/simulate->internal/humanwork":                    true, // simulation accesses humanwork package
		"internal/workflow/steps/approval->internal/humanwork":              true, // approval step accesses humanwork
		"internal/workflow/steps/approval->internal/humanwork/workitem":     true, // approval step accesses work items
		"internal/workflow/steps/task->internal/humanwork":                  true, // task step accesses humanwork
		"internal/workflow/steps/task->internal/humanwork/workitem":         true, // task step accesses work items
		"internal/workflow/migrate/artifacts->internal/humanwork/workitem":  true, // WF-RUN-026 version-migration artifacts re-home open work items
		"internal/workflow/promotionexec->internal/humanwork":               true, // promotion execute requirement names the human-work step contract
		"internal/workflow/cancellation->internal/humanwork/workitem":       true, // WF-RUN-015 cancellation closes governed open work items
		"internal/workflow/execute->internal/humanwork":                     true, // WF-RUN-023 execution consumes the human-work service port
		"internal/workflow/promotionexec->internal/humanwork/approverclass": true, // PROMOUX-003 binds approval nodes to authority classes
	}

	// Allowlisted workflow->messaging edges (count: 1). The execute message
	// gate (MSG-002) consumes messaging.MessageIntent as a typed value only;
	// no messaging delivery or transport is reached from workflow.
	allowlistedWorkflowToMessaging := map[string]bool{
		"internal/workflow/execute->internal/messaging": true,
	}

	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(module, pkg.ImportPath)
		if !ok {
			continue
		}

		for _, imp := range pkg.Imports {
			impRel, ok := archrules.TrimModule(module, imp)
			if !ok {
				continue
			}

			// Check: workflow does not import humanwork
			if archrules.UnderRoot(rel, workflowRoot) && archrules.UnderRoot(impRel, humanworkRoot) {
				edge := rel + "->" + impRel
				if !allowlistedWorkflowToHumanwork[edge] {
					workflowToHumanwork++
					t.Errorf("ARCH-GO-022 violation: %s imports %s (workflow must not import humanwork)", rel, impRel)
				}
			}

			// Check: workflow does not import messaging
			if archrules.UnderRoot(rel, workflowRoot) && archrules.UnderRoot(impRel, messagingRoot) && !allowlistedWorkflowToMessaging[rel+"->"+impRel] {
				workflowToMessaging++
				t.Errorf("ARCH-GO-022 violation: %s imports %s (workflow must not import messaging)", rel, impRel)
			}

			// Check: humanwork does not import workflow/runtime
			if archrules.UnderRoot(rel, humanworkRoot) && archrules.UnderRoot(impRel, workflowRuntime) {
				humanworkToRuntime++
				t.Errorf("ARCH-GO-022 violation: %s imports %s (humanwork must not import workflow/runtime)", rel, impRel)
			}

			// Check: humanwork does not import workflow/execute
			if archrules.UnderRoot(rel, humanworkRoot) && archrules.UnderRoot(impRel, workflowExecute) {
				humanworkToExecute++
				t.Errorf("ARCH-GO-022 violation: %s imports %s (humanwork must not import workflow/execute)", rel, impRel)
			}

			// Check: forms does not import messaging
			if archrules.UnderRoot(rel, formsRoot) && archrules.UnderRoot(impRel, messagingRoot) {
				formsToMessaging++
				t.Errorf("ARCH-GO-022 violation: %s imports %s (forms must not import messaging)", rel, impRel)
			}

			// Check: messaging does not import forms
			if archrules.UnderRoot(rel, messagingRoot) && archrules.UnderRoot(impRel, formsRoot) {
				messagingToForms++
				t.Errorf("ARCH-GO-022 violation: %s imports %s (messaging must not import forms)", rel, impRel)
			}
		}
	}

	t.Logf("ARCH-GO-022: %d workflow->humanwork, %d workflow->messaging, %d humanwork->runtime, %d humanwork->execute, %d forms->messaging, %d messaging->forms violations (+ 14 allowlisted workflow->humanwork edges, 1 workflow->messaging)",
		workflowToHumanwork, workflowToMessaging, humanworkToRuntime, humanworkToExecute, formsToMessaging, messagingToForms)
}

// TestTodo_ARCH_GO_022_Conformance validates that the forbidden edges are
// consistent and correctly defined. For architecture checks, this verifies
// that the rules themselves are structurally sound.
func TestTodo_ARCH_GO_022_Conformance(t *testing.T) {
	const (
		workflowRoot    = "internal/workflow"
		humanworkRoot   = "internal/humanwork"
		formsRoot       = "internal/forms"
		messagingRoot   = "internal/messaging"
		workflowRuntime = "internal/workflow/runtime"
		workflowExecute = "internal/workflow/execute"
	)

	// Define forbidden edges with test cases
	forbiddenEdges := []struct {
		from, to string
		name     string
	}{
		{workflowRoot, humanworkRoot, "workflow->humanwork"},
		{workflowRoot, messagingRoot, "workflow->messaging"},
		{humanworkRoot, workflowRuntime, "humanwork->workflow/runtime"},
		{humanworkRoot, workflowExecute, "humanwork->workflow/execute"},
		{formsRoot, messagingRoot, "forms->messaging"},
		{messagingRoot, formsRoot, "messaging->forms"},
	}

	for _, edge := range forbiddenEdges {
		t.Run(edge.name, func(t *testing.T) {
			// Test with exact root paths
			if !archrules.UnderRoot(edge.from, edge.from) {
				t.Errorf("UnderRoot(%q, %q) should be true", edge.from, edge.from)
			}
			if !archrules.UnderRoot(edge.to, edge.to) {
				t.Errorf("UnderRoot(%q, %q) should be true", edge.to, edge.to)
			}

			// Test with subpaths
			fromSub := edge.from + "/subpkg"
			toSub := edge.to + "/subpkg"
			if !archrules.UnderRoot(fromSub, edge.from) {
				t.Errorf("UnderRoot(%q, %q) should be true", fromSub, edge.from)
			}
			if !archrules.UnderRoot(toSub, edge.to) {
				t.Errorf("UnderRoot(%q, %q) should be true", toSub, edge.to)
			}

			// Test that roots are distinct
			if strings.HasPrefix(edge.to, edge.from+"/") && edge.from != workflowRoot+"/runtime" && edge.from != workflowRoot+"/execute" {
				// This is OK only for workflow/runtime and workflow/execute which are subpaths of workflow
				if edge.from == workflowRoot {
					// It's OK for workflow/* to have subpaths
					return
				}
				t.Logf("Note: %q is under %q", edge.to, edge.from)
			}
		})
	}

	// Verify that at least some forbidden edges are defined
	if len(forbiddenEdges) == 0 {
		t.Errorf("ARCH-GO-022 conformance check: no forbidden edges defined")
	}
}

// TestTodo_ARCH_GO_022_Golden would verify generated artifacts or canonical
// outputs. For architecture ownership checks, this is typically N/A as there
// are no generated artifacts; instead, INTEGRATION validates the real graph.
func TestTodo_ARCH_GO_022_Golden(t *testing.T) {
	t.Skip("ARCH-GO-022 Golden test: N/A for architecture ownership checks; INTEGRATION validates the real graph")
}

// TestTodo_ARCH_GO_022_Race validates that the architecture check itself is
// free of race conditions. For a static analysis check that reads the package
// graph without mutation, race detection is typically N/A.
func TestTodo_ARCH_GO_022_Race(t *testing.T) {
	t.Skip("ARCH-GO-022 Race test: N/A for static architecture analysis with no mutable shared state")
}

// TestTodo_ARCH_GO_022_Browser validates browser/UI aspects of human
// interaction workflows. For an architecture ownership test, this is N/A.
func TestTodo_ARCH_GO_022_Browser(t *testing.T) {
	t.Skip("ARCH-GO-022 Browser test: N/A for architecture ownership checks; UI validation belongs to HCI layers")
}

// TestTodo_ARCH_GO_022_Mutation validates that semantic mutants in the
// ownership rules are caught. For an architecture check, this is typically
// covered by code review rather than mutation testing on the rule definitions.
func TestTodo_ARCH_GO_022_Mutation(t *testing.T) {
	t.Skip("ARCH-GO-022 Mutation test: N/A for architecture policy checks; ownership enforcement validated by INTEGRATION")
}
