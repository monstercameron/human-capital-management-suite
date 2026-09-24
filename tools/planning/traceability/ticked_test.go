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
// todos stay silent; the live-corpus subtest checks that every currently
// ticked todo has resolvable tests and command evidence.
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

	t.Run("ticked todo with no TEST or matrix names is flagged", func(t *testing.T) {
		todo := todoregistry.Todo{
			ID:       "T-100A",
			Done:     true,
			Evidence: "`go test -count=1 ./pkg/x/` PASS",
		}
		findings := CheckTickedTodos([]todoregistry.Todo{todo}, map[string]bool{})
		if len(findings) != 1 || findings[0].Kind != TickedMissingTest {
			t.Fatalf("expected one MISSING_TEST for absent TEST name, got %v", findings)
		}
		if got := findings[0].Detail; got != "completed todo has no TEST name" {
			t.Errorf("unexpected missing TEST detail: %q", got)
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

	t.Run("unticked and retired todos are ignored", func(t *testing.T) {
		todos := []todoregistry.Todo{
			{ID: "T-105", Done: false, Test: "TestMissing", TestMatrix: map[string]string{"PRIMARY": "TestMissing"}},
			{ID: "T-106", Done: true, Retired: true, Test: "TestMissing", TestMatrix: map[string]string{"PRIMARY": "TestMissing"}},
		}
		if findings := CheckTickedTodos(todos, map[string]bool{}); len(findings) != 0 {
			t.Errorf("expected unticked and retired todos to stay silent, got %v", findings)
		}
	})

	t.Run("historical test crosswalk does not waive ticked todo requirements", func(t *testing.T) {
		todo := todoregistry.Todo{ID: "UXSCAN-001", Done: true, Test: "TestMissing"}
		findings := CheckTickedTodos([]todoregistry.Todo{todo}, map[string]bool{})
		if len(findings) != 2 || findings[0].Kind != TickedMissingEvidence || findings[1].Kind != TickedMissingTest {
			t.Fatalf("expected evidence and test gaps for allow-listed historical todo, got %v", findings)
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
		existing, err := ScanRepositoryTestNames(filepath.Join("..", "..", ".."))
		if err != nil {
			t.Fatalf("ScanTestNames: %v", err)
		}
		findings := CheckTickedTodos(todos, existing)

		if len(findings) != 0 {
			t.Errorf("found %d invalid claims among ticked todos:", len(findings))
			for _, finding := range findings {
				t.Errorf("  %s", finding)
			}
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

// TestTodo_REV_103_02_Fault verifies that prose which resembles a command
// cannot satisfy the evidence requirement unless it names a supported,
// backtick-quoted test runner. It also pins supported command families.
func TestTodo_REV_103_02_Fault(t *testing.T) {
	for _, tc := range []struct {
		name     string
		evidence string
		wantKind string
	}{
		{name: "unquoted command", evidence: "go test ./pkg/x", wantKind: TickedMissingCommand},
		{name: "unsupported command", evidence: "`go generate ./pkg/x`", wantKind: TickedMissingCommand},
		{name: "vet does not run tests", evidence: "`go vet ./pkg/x/`", wantKind: TickedMissingCommand},
		{name: "run does not run tests", evidence: "`go run ./cmd/check`", wantKind: TickedMissingCommand},
		{name: "build does not run tests", evidence: "`go build ./cmd/check`", wantKind: TickedMissingCommand},
		{name: "npm build does not run tests", evidence: "`npm run build`", wantKind: TickedMissingCommand},
		{name: "unknown npm test script is unsupported", evidence: "`npm run test:unknown`", wantKind: TickedMissingCommand},
		{name: "test command after another command is unsupported", evidence: "`echo go test ./pkg/x/`", wantKind: TickedMissingCommand},
		{name: "test command", evidence: "`go test -count=1 ./pkg/x/`"},
		{name: "npm test suite", evidence: "`npm run test:all`"},
		{name: "vitest suite", evidence: "`npx vitest run`"},
		{name: "playwright suite", evidence: "`npx playwright test -c tools/uxqual/browser/playwright.config.mjs`"},
		{name: "node test suite", evidence: "`node --test scripts/check-code-style.test.mjs`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			todo := todoregistry.Todo{
				ID:         "T-REV10302-FAULT",
				Done:       true,
				Test:       "TestExisting",
				TestMatrix: map[string]string{"PRIMARY": "TestExisting"},
				Evidence:   tc.evidence,
			}
			findings := CheckTickedTodos([]todoregistry.Todo{todo}, map[string]bool{"TestExisting": true})
			if tc.wantKind == "" {
				if len(findings) != 0 {
					t.Fatalf("expected accepted evidence command, got %v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Kind != tc.wantKind {
				t.Fatalf("expected one %s finding, got %v", tc.wantKind, findings)
			}
		})
	}
}
