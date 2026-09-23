package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// TestTDDContractCommandAdapterSyntheticGreen proves plancheck wires GOV-017
// to the checker rather than only compiling the library. A complete fixture
// must return a zero exit/error result from the command adapter.
func TestTDDContractCommandAdapterSyntheticGreen(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "planning"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const markdown = "## Fixtures\n\n" +
		"- [ ] `FIXTURE-001` **[P0][LUNA] complete fixture.**\n" +
		"  - **TEST:** `TestFixture`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestFixture; GOLDEN=TestFixtureGolden`.\n" +
		"  - **RED:** returns a typed error for the seeded defect.\n" +
		"  - **GREEN:** returns the exact accepted state.\n" +
		"  - **REFACTOR:** preserves the oracle and rerun scope.\n"
	if err := os.WriteFile(filepath.Join(root, "planning", "todos.md"), []byte(markdown), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := runTDDContract(root); err != nil {
		t.Fatalf("runTDDContract(synthetic green): %v", err)
	}
}

// TestTDDContractCommandAdapterLiveCorpusRemainsNonGreen ensures the live
// command exposes existing findings instead of silently treating incomplete
// planning evidence as success.
func TestTDDContractCommandAdapterLiveCorpusRemainsNonGreen(t *testing.T) {
	root := repositoryRoot(t)
	if err := runTDDContract(root); err == nil {
		t.Fatalf("runTDDContract(%s) unexpectedly accepted the live corpus", root)
	}
}

// TestTraceabilityCommandAdapterTickedGaps proves plancheck wires REV-103-02
// to the build-facing command rather than only compiling the library: a
// ticked todo whose TEST has no function fails runTraceability, while a
// corpus with no ticked todos returns cleanly.
func TestTraceabilityCommandAdapterTickedGaps(t *testing.T) {
	const body = "  - **Depends:** none.\n" +
		"  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture`.\n" +
		"  - **TEST:** `TestFixture`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestFixture`.\n" +
		"  - **RED:** returns a typed error for the seeded defect.\n" +
		"  - **GREEN:** returns the exact accepted state.\n" +
		"  - **REFACTOR:** preserves the oracle and rerun scope.\n" +
		"  - **Refs:** [fixture](fixture.md).\n"

	writeTodos := func(t *testing.T, markdown string) string {
		t.Helper()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "planning"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "planning", "todos.md"), []byte(markdown), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		return root
	}

	// No _test.go files under the synthetic root, so TestFixture cannot
	// resolve: the ticked todo must fail the command.
	ticked := "- [x] `FIXTURE-001` **[P0][LUNA] ticked fixture.**\n" + body +
		"  - **Evidence (2026-09-21):** `TestFixture` in `pkg/x`; `go test -count=1 ./pkg/x/` PASS.\n"
	if err := runTraceability(writeTodos(t, ticked)); err == nil {
		t.Fatalf("runTraceability accepted a ticked todo whose TEST has no function")
	} else if got := err.Error(); !strings.Contains(got, "ticked todo gap") {
		t.Fatalf("expected a ticked-todo-gap verdict, got %q", got)
	}

	// An unticked todo is out of scope for the ticked check and carries no
	// evidence burden: the command resolves cleanly.
	unticked := "- [ ] `FIXTURE-002` **[P0][LUNA] unticked fixture.**\n" + body
	if err := runTraceability(writeTodos(t, unticked)); err != nil {
		t.Fatalf("runTraceability(synthetic clean): %v", err)
	}
}

// TestReachabilityCommandAdapterClosesRuntimeTicks proves plancheck wires
// REV-103-01 to the build-facing command rather than only compiling the
// library: a ticked todo naming an unreachable package fails the seam,
// while the same todo with that package in the closure returns cleanly.
func TestReachabilityCommandAdapterClosesRuntimeTicks(t *testing.T) {
	const body = "  - **Depends:** none.\n" +
		"  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture`.\n" +
		"  - **TEST:** `TestFixture`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=TestFixture`.\n" +
		"  - **RED:** returns a typed error for the seeded defect.\n" +
		"  - **GREEN:** returns the exact accepted state.\n" +
		"  - **REFACTOR:** preserves the oracle and rerun scope.\n" +
		"  - **Refs:** [fixture](fixture.md).\n" +
		"  - **Evidence (2026-09-21):** `TestFixture` in `internal/domains/leave`; `go test -count=1 ./internal/domains/leave/` PASS.\n"
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "planning"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	markdown := "- [x] `FIXTURE-001` **[GATE_B][LUNA] ticked fixture.**\n" + body
	if err := os.WriteFile(filepath.Join(root, "planning", "todos.md"), []byte(markdown), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var todos []todoregistry.Todo
	todos, err := readTodos(root)
	if err != nil {
		t.Fatalf("readTodos: %v", err)
	}
	if len(todos) != 1 || !todos[0].Done {
		t.Fatalf("expected one ticked todo, got %v", todos)
	}

	served := map[string]bool{"internal/domains/leave": true}
	if findings := checkReachabilityTodos(todos, served); len(findings) != 0 {
		t.Fatalf("checkReachabilityTodos(served) = %v, want clean", findings)
	}
	findings := checkReachabilityTodos(todos, map[string]bool{})
	if len(findings) != 1 {
		t.Fatalf("checkReachabilityTodos(unserved) = %v, want one finding", findings)
	}
	if got := findings[0].String(); !strings.Contains(got, "ticked runtime todo") || !strings.Contains(got, "FIXTURE-001") {
		t.Fatalf("unexpected finding wording: %q", got)
	}
}

// TestReachabilityCommandLiveCorpusRemainsNonGreen ensures the live
// command exposes existing unreachable ticks instead of silently treating
// library-only code as served.
func TestReachabilityCommandLiveCorpusRemainsNonGreen(t *testing.T) {
	root := repositoryRoot(t)
	if err := runReachability(root); err == nil {
		t.Fatalf("runReachability(%s) unexpectedly accepted the live corpus", root)
	} else if got := err.Error(); !strings.Contains(got, "ticked runtime todo") {
		t.Fatalf("expected a ticked-runtime-todo verdict, got %q", got)
	}
}

// TestLegalResearchCheckersRunAgainstRepository verifies the LEGAL-017 and
// LEGAL-018 checkers are wired into plancheck's repository-facing path, not
// merely unit-tested as isolated parsing libraries.
func TestLegalResearchCheckersRunAgainstRepository(t *testing.T) {
	root := repositoryRoot(t)
	if err := runFederalBaseline(root); err != nil {
		t.Fatalf("runFederalBaseline(%s): %v", root, err)
	}
	if err := runResearchQuestions(root); err != nil {
		t.Fatalf("runResearchQuestions(%s): %v", root, err)
	}
}

// TestBacklogAndArchitectureCheckersRunAgainstRepository proves the three
// repository-facing adapters are kept registered and execute their live scans,
// rather than leaving GOV-015, GOV-016, or ARCH-GO-017 as library-only checks.
func TestBacklogAndArchitectureCheckersRunAgainstRepository(t *testing.T) {
	root := repositoryRoot(t)
	if err := runProgress(root); err != nil {
		t.Fatalf("runProgress(%s): %v", root, err)
	}
	// The live corpus deliberately contains unresolved Gate A phase
	// inversions while the plan is being reconciled. A non-nil result is the
	// truthful command outcome; accepting it here would turn this adapter
	// into a false-green path. The dependencygraph package owns the small
	// clean/invalid graph fixtures that exercise both result classes.
	if err := runDependencyGraph(root); err == nil {
		t.Fatalf("runDependencyGraph(%s) unexpectedly accepted the known live graph violations", root)
	}
	if err := runGarbageDrawer(root); err != nil {
		t.Fatalf("runGarbageDrawer(%s): %v", root, err)
	}
}

// TestGateAndBoundaryCheckersRunAgainstRepository verifies the NEXT-002/
// NEXT-003 evidence-closure, GOV-010 authority-gate, GOV-011 coverage-matrix
// and GOV-013 boundary-test adapters are wired into plancheck's
// repository-facing path (not merely unit-tested as isolated libraries) and
// that each one, run against the live corpus, resolves cleanly once known,
// allow-listed gaps are taken into account: runAuthorityGate and
// runCoverageMatrix filter their real-tree findings against
// known-defects.yaml's known_authority_gate_gaps/known_coverage_gaps
// allow-lists exactly as their packages' own real-corpus tests do, so an
// item that is only allow-listed must not fail the command adapter.
func TestGateAndBoundaryCheckersRunAgainstRepository(t *testing.T) {
	root := repositoryRoot(t)
	if err := runP1AEvidence(root, false); err != nil {
		t.Errorf("runP1AEvidence(%s, live=false): %v", root, err)
	}
	if err := runAuthorityGate(root); err != nil {
		t.Errorf("runAuthorityGate(%s): %v", root, err)
	}
	if err := runCoverageMatrix(root); err != nil {
		t.Errorf("runCoverageMatrix(%s): %v", root, err)
	}
	if err := runBoundaryTests(root); err != nil {
		t.Errorf("runBoundaryTests(%s): %v", root, err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "planning", "todos.md")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root above %s", dir)
		}
		dir = parent
	}
}
