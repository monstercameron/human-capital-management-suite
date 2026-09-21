package scopefidelity

import (
	"os"
	"path/filepath"
	"testing"
)

const rev001BareWorkflow = `name: tests
on: [push]
jobs:
  unit:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - name: unit tests
        run: go test ./tools/planning/scopeexchange/
`

const rev001GatedWorkflow = `name: tests
on: [push]
jobs:
  governance:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - name: backlog governance gate
        run: go run ./tools/planning/cmd/plancheck --allowlist definitions/planning/known-defects.yaml planning/todos.md
`

// TestTodo_REV_001_03 is the REV-001-03 primary: a workflow with no
// live-backlog governance gate must report every missing wiring element; a
// workflow invoking plancheck against planning/todos.md with the
// known-defects.yaml allowlist stays clean.
func TestTodo_REV_001_03(t *testing.T) {
	exp := GateExpectation{
		Binaries:  []string{"tools/planning/cmd/plancheck", "todogovernance"},
		Allowlist: "definitions/planning/known-defects.yaml",
		Target:    "planning/todos.md",
	}

	bare := CheckWorkflowGate("fixture-workflow.yml", rev001BareWorkflow, exp)
	codes := map[string]bool{}
	for _, f := range bare {
		codes[f.Code] = true
	}
	for _, want := range []string{CodeMissingGateStep, CodeMissingAllowlistRef, CodeMissingLiveTarget} {
		if !codes[want] {
			t.Errorf("bare workflow missing code %q, got %v", want, bare)
		}
	}

	if got := CheckWorkflowGate("fixture-workflow.yml", rev001GatedWorkflow, exp); len(got) != 0 {
		t.Errorf("gated workflow unexpectedly flagged: %v", got)
	}
}

// TestTodo_REV_001_03_Golden pins the exact rendered findings bytes for the
// canonical ungated-workflow fixture.
func TestTodo_REV_001_03_Golden(t *testing.T) {
	exp := GateExpectation{
		Binaries:  []string{"tools/planning/cmd/plancheck", "todogovernance"},
		Allowlist: "definitions/planning/known-defects.yaml",
		Target:    "planning/todos.md",
	}
	findings := CheckWorkflowGate("fixture-workflow.yml", rev001BareWorkflow, exp)
	want, err := os.ReadFile(filepath.Join("testdata", "rev00103.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderFindings(findings); got != string(want) {
		t.Errorf("golden mismatch:\n got: %q\nwant: %q", got, string(want))
	}
}
