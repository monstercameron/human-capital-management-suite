package simulate

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestMappingSourceAdapterVariableRefuses proves a SIMULATE run carries no
// variable history: the adapter serves nothing, so a VARIABLE mapping
// resolves through the shared resolver to UNRESOLVED_VARIABLE rather than
// to a value no durable history stands behind.
func TestMappingSourceAdapterVariableRefuses(t *testing.T) {
	adapter := mappingSourceAdapter{}
	if raw, ok := adapter.Variable("proposal", "grade"); ok || raw != "" {
		t.Fatalf("simulate resolves no variables, got %q, %v", raw, ok)
	}
	node := workflow.CompiledNode{ID: "n1", Mappings: []workflow.CompiledMapping{
		{
			Target: "grade", TargetType: workflow.ValueType{Kind: workflow.KindString},
			SourceKind: workflow.SourceVariable, SourceVariable: "proposal", SourcePath: "grade",
			SourceType: workflow.ValueType{Kind: workflow.KindString},
		},
	}}
	_, err := workflow.ResolveMappings(node, adapter)
	mappingErr, ok := err.(*workflow.MappingError)
	if !ok || mappingErr.Code != workflow.CodeUnresolvedVariable {
		t.Fatalf("error = %v, want a mapping refusal with code UNRESOLVED_VARIABLE", err)
	}
}
