package main

import (
	"encoding/json"
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

func TestTodo_GOV_012_CommandConformance(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"planning", "schema", "definitions"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "planning", "terms.md"), []byte("Candidate and Worker are not mutually exclusive Person states."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "schema", "terms.proto"), []byte("// external observation is recorded as domain fact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "definitions", "terms.yaml"), []byte("name: Workforce Access authenticates all operators"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runTerminology(root); err == nil || !strings.Contains(err.Error(), "2 violation(s)") {
		t.Fatalf("runTerminology error = %v, want 2 violations across schemas and definitions (the planning statement is valid)", err)
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
		"  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture; CAPABILITY=RUNTIME; OWNER=TEST_OWNER`.\n" +
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
	known := map[string]bool{"internal/domains/leave": true}
	if findings := checkReachabilityTodos(todos, served, known); len(findings) != 0 {
		t.Fatalf("checkReachabilityTodos(served) = %v, want clean", findings)
	}
	findings := checkReachabilityTodos(todos, map[string]bool{}, known)
	if len(findings) != 1 {
		t.Fatalf("checkReachabilityTodos(unserved) = %v, want one finding", findings)
	}
	if got := findings[0].String(); !strings.Contains(got, "ticked runtime todo") || !strings.Contains(got, "FIXTURE-001") {
		t.Fatalf("unexpected finding wording: %q", got)
	}
}

func TestBinaryClosureIncludesShippedCommandsAndExcludesFrontendDev(t *testing.T) {
	root := repositoryRoot(t)
	reachable, err := binaryClosure(root)
	if err != nil {
		t.Fatalf("binaryClosure(%s): %v", root, err)
	}
	want := [...]shippedBinary{
		{Name: "hcmnext", Package: "./cmd/hcmnext"},
		{Name: "scheduler", Package: "./cmd/scheduler"},
		{Name: "worker", Package: "./cmd/worker"},
		{Name: "migrate", Package: "./cmd/migrate"},
		{Name: "hcmctl", Package: "./cmd/hcmctl"},
		{Name: "projector", Package: "./cmd/projector"},
	}
	got := shippedBinaryMatrix()
	if got != want {
		t.Fatalf("shipped binary matrix = %#v, want %#v", got, want)
	}
	buildScript, err := os.ReadFile(filepath.Join(root, "scripts", "build.sh"))
	if err != nil {
		t.Fatalf("read scripts/build.sh: %v", err)
	}
	defaultCommands := strings.LastIndex(string(buildScript), "cmds=(")
	if defaultCommands < 0 {
		t.Fatal("scripts/build.sh no longer declares its default shipped command set")
	}
	defaultCommands += len("cmds=(")
	endCommands := strings.Index(string(buildScript)[defaultCommands:], ")")
	if endCommands < 0 {
		t.Fatal("scripts/build.sh default command set is unterminated")
	}
	var binaryNames []string
	for _, binary := range got {
		binaryNames = append(binaryNames, binary.Name)
	}
	if gotNames := strings.Fields(string(buildScript)[defaultCommands : defaultCommands+endCommands]); strings.Join(gotNames, ",") != strings.Join(binaryNames, ",") {
		t.Fatalf("shipped binary matrix %v no longer matches scripts/build.sh defaults %v", binaryNames, gotNames)
	}
	for _, binary := range got {
		command := "cmd/" + binary.Name
		if !reachable[command] {
			t.Errorf("shipped command %s (%s) is missing from binary closure", binary.Name, binary.Package)
		}
	}
	if reachable["cmd/frontenddev"] {
		t.Fatal("development-only cmd/frontenddev must not satisfy runtime reachability")
	}
}

func TestPackageInventoryContainsActualPackagesOnly(t *testing.T) {
	root := repositoryRoot(t)
	packages, err := packageInventory(root)
	if err != nil {
		t.Fatalf("packageInventory(%s): %v", root, err)
	}
	if !packages["internal/domains/leave"] {
		t.Fatal("package inventory omitted an existing Go package")
	}
	if packages["cmd"] || packages["internal"] || packages["gen"] {
		t.Fatalf("package inventory included a repository directory that is not a Go package: cmd=%t internal=%t gen=%t", packages["cmd"], packages["internal"], packages["gen"])
	}
}

func TestWorkflowConformanceCommandAdapter(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "planning", "workflows", "people", "sample.md")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte("workflow_id: sample/v1\nstate: EXTRACTED + EXPLORED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "planning"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(context string) {
		t.Helper()
		content := "## Fixture\n\n" +
			"- [x] `CONF-TEST` **[CONFORMANCE][LUNA] exploratory proof.**\n" +
			"  - **INTENT CONTEXT:** `ROLE=CONFORMANCE; WHY=fixture" + context + "`.\n" +
			"  - **TEST:** `TestFixture`.\n" +
			"  - **TEST MATRIX:** `PRIMARY=TestFixture`.\n" +
			"  - **RED:** returns a failure for the unresolved workflow state.\n" +
			"  - **GREEN:** identifies exploratory evidence.\n" +
			"  - **REFACTOR:** none.\n" +
			"  - **Refs:** [workflow](workflows/people/sample.md).\n"
		if err := os.WriteFile(filepath.Join(root, "planning", "todos.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("")
	if err := runWorkflowConformance(root); err == nil {
		t.Fatal("untagged conformance todo citing an exploratory workflow passed")
	}
	write("; PRE_PROMOTION_EXPLORATORY=true")
	if err := runWorkflowConformance(root); err != nil {
		t.Fatalf("explicit exploratory conformance todo failed: %v", err)
	}
}

func TestWorkflowConformanceIsRequiredByPreCommitAndCI(t *testing.T) {
	root := repositoryRoot(t)
	packageBytes, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packageManifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(packageBytes, &packageManifest); err != nil {
		t.Fatalf("parse package.json: %v", err)
	}
	const scriptName = "check:workflowconformance"
	const command = "go run ./tools/planning/cmd/plancheck workflowconformance ."
	if packageManifest.Scripts[scriptName] != command {
		t.Fatalf("%s = %q, want %q", scriptName, packageManifest.Scripts[scriptName], command)
	}
	if count := strings.Count(packageManifest.Scripts["test:all"], "npm run "+scriptName); count != 1 {
		t.Fatalf("test:all invokes %s %d times, want once", scriptName, count)
	}
	hookBytes, err := os.ReadFile(filepath.Join(root, ".husky", "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hookBytes), "npm run test:all") {
		t.Fatal("pre-commit no longer runs test:all, so workflow conformance is not required there")
	}
	workflowBytes, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "tests.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(workflowBytes), "run: npm run "+scriptName); count != 1 {
		t.Fatalf("CI invokes %s %d times, want once", scriptName, count)
	}
}

// TestReachabilityCommandLiveCorpusIsGreen ensures the live command agrees
// with the repaired ticked runtime corpus while the synthetic test above
// continues to exercise the unreachable-package failure path.
func TestReachabilityCommandLiveCorpusIsGreen(t *testing.T) {
	root := repositoryRoot(t)
	if err := runReachability(root); err != nil {
		t.Fatalf("runReachability(%s): %v", root, err)
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
