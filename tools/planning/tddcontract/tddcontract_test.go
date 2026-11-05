package tddcontract

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

func todoFixture(id string, fields string) string {
	return "- [ ] `" + id + "` **[P0][LUNA] fixture.**\n" + fields + "\n"
}

func completeFields(testName string) string {
	return "  - **TEST:** `" + testName + "`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=" + testName + "; GOLDEN=" + testName + "Golden`.\n" +
		"  - **RED:** returns a typed error for the seeded defect.\n" +
		"  - **GREEN:** returns the exact accepted state.\n" +
		"  - **REFACTOR:** preserves the oracle and rerun scope.\n"
}

// TestTodoTDDContractCompleteness exercises every GOV-017 red-first rule with
// source fixtures. The assertions intentionally check stable ID/file/line/code
// diagnostics, not just a boolean failure.
func TestTodoTDDContractCompleteness(t *testing.T) {
	const file = "fixtures/tdd.md"
	markdown := "## Synthetic TDD fixtures\n\n" +
		todoFixture("MISSING-TEST", "  - **TEST MATRIX:** `PRIMARY=TestMissingTest`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("DUP-TEST", "  - **TEST:** `TestDuplicate`.\n  - **TEST:** `TestDuplicateAgain`.\n"+"  - **TEST MATRIX:** `PRIMARY=TestDuplicate`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("MISSING-MATRIX", "  - **TEST:** `TestMissingMatrix`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("DUP-MATRIX", "  - **TEST:** `TestDuplicateMatrix`.\n  - **TEST MATRIX:** `PRIMARY=TestDuplicateMatrix`.\n  - **TEST MATRIX:** `GOLDEN=TestDuplicateMatrixGolden`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("MISSING-RED", "  - **TEST:** `TestMissingRed`.\n  - **TEST MATRIX:** `PRIMARY=TestMissingRed`.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("DUP-RED", "  - **TEST:** `TestDuplicateRed`.\n  - **TEST MATRIX:** `PRIMARY=TestDuplicateRed`.\n  - **RED:** returns an error.\n  - **RED:** returns another error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("MISSING-GREEN", "  - **TEST:** `TestMissingGreen`.\n  - **TEST MATRIX:** `PRIMARY=TestMissingGreen`.\n  - **RED:** returns an error.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("DUP-GREEN", "  - **TEST:** `TestDuplicateGreen`.\n  - **TEST MATRIX:** `PRIMARY=TestDuplicateGreen`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **GREEN:** returns another state.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("MISSING-REFACTOR", "  - **TEST:** `TestMissingRefactor`.\n  - **TEST MATRIX:** `PRIMARY=TestMissingRefactor`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n") +
		todoFixture("DUP-REFACTOR", "  - **TEST:** `TestDuplicateRefactor`.\n  - **TEST MATRIX:** `PRIMARY=TestDuplicateRefactor`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n  - **REFACTOR:** keeps the test small.\n") +
		"- [ ] `COMBINED` **[P0][LUNA] fixture.**\n  - **TEST:** `TestCombined`.\n  - **TEST MATRIX:** `PRIMARY=TestCombined`.\n  - **RED/GREEN:** returns a result.\n  - **REFACTOR:** keeps the test.\n" +
		todoFixture("INVALID-NAME", "  - **TEST:** `not_a_test`.\n  - **TEST MATRIX:** `PRIMARY=TestInvalidMatrix`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("DUP-NAME-A", completeFields("TestSameName")) +
		todoFixture("DUP-NAME-B", completeFields("TestSameName")) +
		todoFixture("NO-RED-EVIDENCE", "  - **TEST:** `TestNoRedEvidence`.\n  - **TEST MATRIX:** `PRIMARY=TestNoRedEvidence`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n  - **Evidence (2026-09-03):** green passed on branch fixture.\n") +
		todoFixture("VAGUE", "  - **TEST:** `TestVague`.\n  - **TEST MATRIX:** `PRIMARY=TestVague`.\n  - **RED:** something.\n  - **GREEN:** works correctly.\n  - **REFACTOR:** keeps the test.\n") +
		todoFixture("EVIDENCE-ONLY", "  - **TEST:** `TestEvidenceOnly`.\n  - **TEST MATRIX:** `PRIMARY=TestEvidenceOnly`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n  - **Disposition:** EVIDENCE_ONLY_LATER.\n  - **Disposition:** EVIDENCE_ONLY.\n")

	findings, err := CheckMarkdown(markdown, file)
	if err != nil {
		t.Fatalf("CheckMarkdown: %v", err)
	}
	wantCodes := []string{
		MissingTest, DuplicateTest, MissingTestMatrix, DuplicateTestMatrix,
		MissingRed, DuplicateRed, MissingGreen, DuplicateGreen,
		MissingRefactor, DuplicateRefactor, CombinedRedGreen,
		InvalidGoTestName, NonUniqueGoTestName, GreenEvidenceWithoutRed,
		VagueOracle, InvalidEvidenceOnlyStatus, NonUniqueEvidenceOnly,
		EvidenceOnlyMissingOwner, EvidenceOnlyMissingRationale,
		EvidenceOnlyMissingOracle, EvidenceOnlyMissingExpiry,
	}
	for _, code := range wantCodes {
		found := false
		for _, finding := range findings {
			if finding.Code == code {
				found = true
				if finding.File != file || finding.Line <= 0 || finding.TodoID == "" {
					t.Errorf("%s lacks stable source identity: %+v", code, finding)
				}
				break
			}
		}
		if !found {
			t.Errorf("missing diagnostic code %s in %v", code, findings)
		}
	}
	for i := 1; i < len(findings); i++ {
		previous, current := findings[i-1], findings[i]
		if previous.File > current.File ||
			(previous.File == current.File && previous.Line > current.Line) ||
			(previous.File == current.File && previous.Line == current.Line && previous.TodoID > current.TodoID) ||
			(previous.File == current.File && previous.Line == current.Line && previous.TodoID == current.TodoID && previous.Code > current.Code) {
			t.Fatalf("diagnostics are not deterministic: %q before %q", previous, current)
		}
	}

	valid := "## Valid fixture\n\n" + todoFixture("VALID", completeFields("TestValid"))
	validFindings, err := CheckMarkdown(valid, file)
	if err != nil {
		t.Fatalf("valid CheckMarkdown: %v", err)
	}
	if len(validFindings) != 0 {
		t.Fatalf("valid fixture produced findings: %v", validFindings)
	}

	root := repoRoot(t)
	realContent, err := os.ReadFile(filepath.Join(root, "planning", "archive", "todos-2026-10-22.md"))
	if err != nil {
		t.Fatalf("read real corpus: %v", err)
	}
	realFindings, err := CheckMarkdown(string(realContent), "planning/archive/todos-2026-10-22.md")
	if err != nil {
		t.Fatalf("real corpus check: %v", err)
	}
	if len(realFindings) == 0 {
		t.Log("GOV-017 can close: real corpus has no TDD contract findings")
	} else {
		t.Logf("GOV-017 cannot close: real corpus has %d live findings; first findings: %s", len(realFindings), summarize(realFindings, 12))
	}
}

func summarize(findings []Diagnostic, limit int) string {
	if len(findings) < limit {
		limit = len(findings)
	}
	parts := make([]string, 0, limit)
	for _, finding := range findings[:limit] {
		parts = append(parts, finding.String())
	}
	return strings.Join(parts, " | ")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}

// TestTodo_GOV_017_Golden pins source location, todo identity, diagnostic
// code, and message for a representative malformed todo.
func TestTodo_GOV_017_Golden(t *testing.T) {
	markdown := "## Golden\n\n" + todoFixture("GOLDEN-001", "  - **TEST:** `bad`.\n  - **TEST MATRIX:** `PRIMARY=TestGolden`.\n  - **RED:** returns an error.\n  - **GREEN:** returns a state.\n  - **REFACTOR:** keeps the test.\n  - **Refs:** [plan](plan.md).\n")
	findings, err := CheckMarkdown(markdown, "golden.md")
	if err != nil {
		t.Fatalf("CheckMarkdown: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want one: %v", len(findings), findings)
	}
	const want = "golden.md:4: GOLDEN-001: INVALID_GO_TEST_NAME: TEST name \"bad\" is not a valid Go test, fuzz, or benchmark name"
	if got := findings[0].String(); got != want {
		t.Fatalf("diagnostic changed:\n got: %s\nwant: %s", got, want)
	}

	// Reuse the existing schema parser on a valid fixture to ensure GOV-017's
	// source shape remains compatible with the canonical registry parser.
	if todos, parseErrs := todoregistry.ParseTodos(markdown); len(todos) != 1 || len(parseErrs) != 0 {
		t.Fatalf("canonical parser compatibility changed: todos=%d errors=%v", len(todos), parseErrs)
	}
}
