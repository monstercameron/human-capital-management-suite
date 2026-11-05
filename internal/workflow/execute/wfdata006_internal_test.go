package execute

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func wfData006Write(value string) runtime.VariableWrite {
	return runtime.VariableWrite{
		VariableName: "proposal",
		SchemaRef:    "schema.test.wfdata006/v1",
		Value:        json.RawMessage(value),
		WriterNodeID: "build_proposal",
		WriterRef:    "transform-execution:1",
		Reason:       "proposal rebuilt after band evaluation",
		CausationID:  "cause:band-evaluated",
	}
}

// TestDataPlaneSourceVariableReadsPinnedRevision proves the durable source
// serves VARIABLE mappings from the revision current when the attempt's
// inputs resolved, and that a source with no variable history resolves
// nothing instead of inventing a value.
func TestDataPlaneSourceVariableReadsPinnedRevision(t *testing.T) {
	at := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	log, err := runtime.NewVariableRevisionLog(uuid.New(), uuid.New(), nil)
	if err != nil {
		t.Fatalf("empty log: %v", err)
	}
	if _, err := log.Append(wfData006Write(`{"grade":"L5"}`), at); err != nil {
		t.Fatalf("append L5: %v", err)
	}
	pinned := log.PinAttempt()
	if _, err := log.Append(wfData006Write(`{"grade":"L6"}`), at.Add(time.Minute)); err != nil {
		t.Fatalf("append L6: %v", err)
	}
	src := dataPlaneSource{hasInputs: true, variables: pinned}
	raw, ok := src.Variable("proposal", "grade")
	if !ok || raw != `"L5"` {
		t.Fatalf("proposal.grade = %q, want the L5 revision current at the pin", raw)
	}
	if _, ok := src.Variable("proposal", "missing"); ok {
		t.Fatal("a missing field must not resolve")
	}
	if _, ok := (dataPlaneSource{}).Variable("proposal", "grade"); ok {
		t.Fatal("a source with no variable history must not resolve a variable")
	}
}

// TestNodeReadsVariablesScansMappings proves the driver loads the revision
// history exactly for nodes whose compiled mappings read a variable.
func TestNodeReadsVariablesScansMappings(t *testing.T) {
	withVariable := workflow.CompiledNode{ID: "n1", Mappings: []workflow.CompiledMapping{
		{Target: "grade", SourceKind: workflow.SourceVariable, SourceVariable: "proposal", SourcePath: "grade"},
	}}
	without := workflow.CompiledNode{ID: "n2", Mappings: []workflow.CompiledMapping{
		{Target: "worker_id", SourceKind: workflow.SourceWorkflowInput, SourcePath: "worker_id"},
		{Target: "kind", SourceKind: workflow.SourceConstant, Constant: "PROMOTION"},
	}}
	if !nodeReadsVariables(withVariable) {
		t.Fatal("a node with a VARIABLE mapping must load the revision history")
	}
	if nodeReadsVariables(without) {
		t.Fatal("a node with no VARIABLE mapping must not load the revision history")
	}
	if nodeReadsVariables(workflow.CompiledNode{ID: "n3"}) {
		t.Fatal("a node with no mappings must not load the revision history")
	}
}
