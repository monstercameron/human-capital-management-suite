package traceability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// TestTodo_REV_103_02 is the REV-103-02 primary: the traceability check
// fails a ticked todo whose TEST or TEST MATRIX names have no function,
// which carries no Evidence line, or whose evidence names no command.
// Fixtures prove each kind fires and clean/unticked/retired/allow-listed
// todos stay silent; the live-corpus subtest proves the RED gaps
// (SVC-003, DB-005, LEDGER-001 missing tests, command-less evidence) are
// still detected rather than silently accepted, while allow-listed UXSCAN
// variance stays silent until its entries are retired with real tests.
func TestTodo_REV_103_02(t *testing.T) {
	t.Run("clean ticked todo is silent", func(t *testing.T) {
		todos := []todoregistry.Todo{{
			ID:   "T-100",
			Done: true,
			Test: "TestReal",
			TestMatrix: map[string]string{
				"PRIMARY": "TestReal",
				"GOLDEN":  "TestReal_Golden",
			},
			Evidence: "`TestReal` in `pkg/x`; `go test -count=1 ./pkg/x/` PASS",
		}}
		existing := map[string]bool{"TestReal": true, "TestReal_Golden": true}
		if findings := CheckTickedTodos(todos, existing); len(findings) != 0 {
			t.Errorf("expected zero findings for a fully evidenced ticked todo, got %v", findings)
		}
	})

	t.Run("missing TEST is flagged once even when PRIMARY duplicates it", func(t *testing.T) {
		todos := []todoregistry.Todo{{
			ID:         "T-101",
			Done:       true,
			Test:       "TestMissing",
			TestMatrix: map[string]string{"PRIMARY": "TestMissing"},
			Evidence:   "`go test -count=1 ./pkg/x/` PASS",
		}}
		findings := CheckTickedTodos(todos, map[string]bool{})
		if len(findings) != 1 || findings[0].Kind != TickedMissingTest || findings[0].ID != "T-101" {
			t.Fatalf("expected one MISSING_TEST for T-101, got %v", findings)
		}
		const want = "T-101: MISSING_TEST: TEST TestMissing names no Test/Fuzz/Benchmark function in the repository"
		if got := findings[0].String(); got != want {
			t.Errorf("finding message changed:\n got:  %s\n want: %s", got, want)
		}
	})

	t.Run("missing matrix entries follow sorted class order", func(t *testing.T) {
		todos := []todoregistry.Todo{{
			ID:   "T-102",
			Done: true,
			Test: "TestReal",
			TestMatrix: map[string]string{
				"PRIMARY": "TestReal",
				"RACE":    "TestReal_Race",
				"GOLDEN":  "TestReal_Golden",
			},
			Evidence: "`go test -count=1 ./pkg/x/` PASS",
		}}
		findings := CheckTickedTodos(todos, map[string]bool{"TestReal": true})
		if len(findings) != 2 {
			t.Fatalf("expected two MISSING_MATRIX_TEST findings, got %v", findings)
		}
		if findings[0].Kind != TickedMissingMatrixTest || !strings.Contains(findings[0].Detail, "GOLDEN=TestReal_Golden") {
			t.Errorf("expected GOLDEN first in sorted class order, got %v", findings)
		}
		if findings[1].Kind != TickedMissingMatrixTest || !strings.Contains(findings[1].Detail, "RACE=TestReal_Race") {
			t.Errorf("expected RACE second in sorted class order, got %v", findings)
		}
	})

	t.Run("ticked todo with no evidence is flagged", func(t *testing.T) {
		todos := []todoregistry.Todo{{
			ID:         "T-103",
			Done:       true,
			Test:       "TestReal",
			TestMatrix: map[string]string{"PRIMARY": "TestReal"},
		}}
		findings := CheckTickedTodos(todos, map[string]bool{"TestReal": true})
		if len(findings) != 1 || findings[0].Kind != TickedMissingEvidence {
			t.Fatalf("expected one MISSING_EVIDENCE, got %v", findings)
		}
	})

	t.Run("evidence naming no command is flagged", func(t *testing.T) {
		todos := []todoregistry.Todo{{
			ID:         "T-104",
			Done:       true,
			Test:       "TestReal",
			TestMatrix: map[string]string{"PRIMARY": "TestReal"},
			Evidence:   "`TestReal` reviewed by hand, looks fine",
		}}
		findings := CheckTickedTodos(todos, map[string]bool{"TestReal": true})
		if len(findings) != 1 || findings[0].Kind != TickedMissingCommand {
			t.Fatalf("expected one MISSING_COMMAND, got %v", findings)
		}
	})

	t.Run("unticked retired and allowlisted todos are ignored", func(t *testing.T) {
		todos := []todoregistry.Todo{
			{ID: "T-105", Done: false, Test: "TestMissing", TestMatrix: map[string]string{"PRIMARY": "TestMissing"}},
			{ID: "T-106", Done: true, Retired: true, Test: "TestMissing", TestMatrix: map[string]string{"PRIMARY": "TestMissing"}},
			{ID: "UXSCAN-001", Done: true, Test: "TestMissing", TestMatrix: map[string]string{"PRIMARY": "TestMissing"}},
		}
		if findings := CheckTickedTodos(todos, map[string]bool{}); len(findings) != 0 {
			t.Errorf("expected unticked, retired and allow-listed todos to stay silent, got %v", findings)
		}
	})

	t.Run("matrix applicability annotations are not test names", func(t *testing.T) {
		todos := []todoregistry.Todo{{
			ID:   "T-107",
			Done: true,
			Test: "TestReal",
			TestMatrix: map[string]string{
				"PRIMARY":           "TestReal",
				"GOLDEN":            "TestReal_Golden",
				"UNIT_ONLY(reason":  "one oracle covers everything",
				"DUPLICATE_OF_TEST": "TestReal",
				"EMPTY":             "",
			},
			Evidence: "`go test -count=1 ./pkg/x/` PASS",
		}}
		findings := CheckTickedTodos(todos, map[string]bool{"TestReal": true})
		if len(findings) != 1 || findings[0].Kind != TickedMissingMatrixTest || !strings.Contains(findings[0].Detail, "GOLDEN=TestReal_Golden") {
			t.Fatalf("expected only the GOLDEN matrix gap, got %v", findings)
		}
	})

	t.Run("output is deterministic across map iterations", func(t *testing.T) {
		todos := []todoregistry.Todo{{
			ID:   "T-108",
			Done: true,
			Test: "TestMissing",
			TestMatrix: map[string]string{
				"PRIMARY": "TestMissing",
				"ZEBRA":   "TestMissing_Zebra",
				"ALPHA":   "TestMissing_Alpha",
				"MIDDLE":  "TestMissing_Middle",
			},
			Evidence: "no command here",
		}}
		first := RenderTickedFindings(CheckTickedTodos(todos, map[string]bool{}))
		for i := 0; i < 25; i++ {
			if got := RenderTickedFindings(CheckTickedTodos(todos, map[string]bool{})); got != first {
				t.Fatalf("nondeterministic output:\nfirst:\n%s\n got:\n%s", first, got)
			}
		}
	})

	t.Run("live corpus still carries the RED gaps", func(t *testing.T) {
		content, err := os.ReadFile(filepath.Join("..", "..", "..", "planning", "todos.md"))
		if err != nil {
			t.Fatalf("read todos.md: %v", err)
		}
		todos, parseErrs := todoregistry.ParseTodos(string(content))
		if len(parseErrs) > 0 {
			t.Fatalf("ParseTodos returned errors: %v", parseErrs)
		}
		existing, err := ScanTestNames(filepath.Join("..", "..", ".."))
		if err != nil {
			t.Fatalf("ScanTestNames: %v", err)
		}
		findings := CheckTickedTodos(todos, existing)

		// SVC-003 names neither its primary nor any matrix test.
		if !hasTickedFinding(findings, "SVC-003", TickedMissingTest, "TestTodo_SVC_003") {
			t.Errorf("expected SVC-003 MISSING_TEST to be detected")
		}
		if !hasTickedFinding(findings, "SVC-003", TickedMissingMatrixTest, "GOLDEN=TestTodo_SVC_003_Golden") {
			t.Errorf("expected SVC-003 MISSING_MATRIX_TEST to be detected")
		}
		// DB-005 proves its primary but not its fuzz/race/integration/
		// fault/mutation matrix names; the check must flag exactly those.
		if !hasTickedFinding(findings, "DB-005", TickedMissingMatrixTest, "FuzzTodo_DB_005") {
			t.Errorf("expected DB-005 MISSING_MATRIX_TEST for FuzzTodo_DB_005 to be detected")
		}
		if hasTickedFinding(findings, "DB-005", TickedMissingTest, "") {
			t.Errorf("DB-005 primary exists and must not be flagged")
		}
		// LEDGER-001 proves its primary but not its race/mutation names.
		if !hasTickedFinding(findings, "LEDGER-001", TickedMissingMatrixTest, "RACE=TestTodo_LEDGER_001_Race") {
			t.Errorf("expected LEDGER-001 MISSING_MATRIX_TEST for race to be detected")
		}
		// UXSCAN-001 is ticked with no evidence line, but it is recorded
		// reviewed variance in tsProvenTodos, so the check must not
		// re-flag it: genuine no-evidence gaps are proven by fixture
		// above, and retiring this entry means writing the Go test,
		// citing it from evidence, and deleting the entry.
		if hasTickedFinding(findings, "UXSCAN-001", TickedMissingEvidence, "") {
			t.Errorf("UXSCAN-001 is allow-listed reviewed variance and must stay silent")
		}
		// At least one ticked evidence line still names no command.
		seenCommandGap := false
		for _, f := range findings {
			if f.Kind == TickedMissingCommand {
				seenCommandGap = true
				break
			}
		}
		if !seenCommandGap {
			t.Errorf("expected at least one MISSING_COMMAND finding in the live corpus")
		}
	})
}

// TestTodo_REV_103_02_Golden pins the exact rendered bytes for a fixed
// two-todo fixture so a future refactor cannot silently change diagnostic
// wording or ordering.
func TestTodo_REV_103_02_Golden(t *testing.T) {
	todos := []todoregistry.Todo{
		{
			ID:   "TICKED-001",
			Done: true,
			Test: "TestMissing",
			TestMatrix: map[string]string{
				"PRIMARY": "TestMissing",
				"GOLDEN":  "TestMissing_Golden",
			},
			Evidence: "reviewed by hand, looks fine",
		},
		{
			ID:         "TICKED-002",
			Done:       true,
			Test:       "TestReal",
			TestMatrix: map[string]string{"PRIMARY": "TestReal"},
		},
	}
	got := RenderTickedFindings(CheckTickedTodos(todos, map[string]bool{"TestReal": true}))
	want, err := os.ReadFile(filepath.Join("testdata", "rev10302.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch:\n got:  %q\n want: %q", got, string(want))
	}
}

func hasTickedFinding(findings []TickedFinding, id, kind, substr string) bool {
	for _, f := range findings {
		if f.ID == id && f.Kind == kind && strings.Contains(f.Detail, substr) {
			return true
		}
	}
	return false
}
