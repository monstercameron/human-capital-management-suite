// Package builders is the one shared definition-builder package for the
// workflow conformance families (WF-EXT-003): the mapping sources and
// terminal-node builders every reference workflow composes the same way,
// instead of a copy of fromInput and the terminal builders in each family.
// The builders carry no family meaning — brands, schemas, governance and
// policy refs stay with the family that declares them — so a shared builder
// can never smuggle one workflow's semantics into another's.
package builders

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// FromInput binds a node input to a workflow input field.
func FromInput(path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceWorkflowInput, Path: path}
}

// FromNode binds a node input to a declared predecessor node's declared
// output field.
func FromNode(nodeID, path string) workflow.Source {
	return workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: nodeID, Path: path}
}

// Constant binds a node input to a literal constant of declared type.
func Constant(value string, t workflow.ValueType) workflow.Source {
	return workflow.Source{Kind: workflow.SourceConstant, Constant: value, Type: t}
}

// Completion records the intent's five lifecycle dimensions by name. Exactly
// five keys are legal; a sixth key is rejected by the compiler rather than
// stored.
func Completion(request, execution, business, consistency, obligation string) map[string]string {
	return map[string]string{
		"RequestState":     request,
		"ExecutionState":   execution,
		"BusinessState":    business,
		"ConsistencyState": consistency,
		"ObligationState":  obligation,
	}
}

// TerminalInputs declares the identity and terminal_code inputs every END
// node carries, plus any family-specific extra fields. The identity is
// family data — worker_id, case_id, run_id — so the family names it and the
// shape stays shared.
func TerminalInputs(idPath, idBrand string, extra ...workflow.Field) []workflow.Field {
	base := []workflow.Field{
		{Path: idPath, Type: workflow.ValueType{Kind: workflow.KindString, Brand: idBrand}},
		{Path: "terminal_code", Type: workflow.ValueType{Kind: workflow.KindString}},
	}
	return append(base, extra...)
}

// TerminalMappings binds an END node's inputs to the workflow input and its
// terminal code, plus any family-specific extra mappings.
func TerminalMappings(idPath, code string, extra ...workflow.Mapping) []workflow.Mapping {
	base := []workflow.Mapping{
		{Target: idPath, Source: FromInput(idPath)},
		{Target: "terminal_code", Source: Constant(code, workflow.ValueType{Kind: workflow.KindString})},
	}
	return append(base, extra...)
}
