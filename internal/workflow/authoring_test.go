package workflow_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func TestTodo_WF_UI_007_Property(t *testing.T) {
	definition := promotionexec.Definition()
	targetID := promotionexec.NodeRaiseThreshold
	for _, input := range nodeByID(t, definition, targetID).Inputs {
		candidates := workflow.OutputBindingCandidates(definition, targetID, input.Path)
		for _, candidate := range candidates {
			mutant := promotionexec.Definition()
			target := nodeByIDPointer(t, &mutant, targetID)
			for index := range target.InputMappings {
				if target.InputMappings[index].Target == input.Path {
					target.InputMappings[index].Source = workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: candidate.NodeID, Path: candidate.Path}
				}
			}
			if _, err := promotionexec.Compile(mutant); err != nil {
				t.Fatalf("picker offered compiler-invalid %s.%s -> %s.%s: %v", candidate.NodeID, candidate.Path, targetID, input.Path, err)
			}
		}
	}
}

func TestTodo_WF_UI_007_DominanceAndAssignabilityFilter(t *testing.T) {
	stringType := workflow.ValueType{Kind: workflow.KindString}
	integerType := workflow.ValueType{Kind: workflow.KindInteger}
	definition := workflow.Definition{
		WorkflowID: "authoring.candidates", Version: 1, StartNodeID: "start",
		Nodes: []workflow.Node{
			{ID: "start", Type: workflow.StepTransform, Outputs: []workflow.Field{{Path: "worker", Type: stringType}, {Path: "count", Type: integerType}}},
			{ID: "left", Type: workflow.StepTransform, Outputs: []workflow.Field{{Path: "worker", Type: stringType}}},
			{ID: "right", Type: workflow.StepTransform, Outputs: []workflow.Field{{Path: "worker", Type: stringType}}},
			{ID: "target", Type: workflow.StepEnd, Inputs: []workflow.Field{{Path: "worker", Type: stringType}}},
		},
		Edges: []workflow.Edge{{From: "start", To: "left", RouteKey: "SUCCEEDED"}, {From: "start", To: "right", RouteKey: "FAILED"}, {From: "left", To: "target", RouteKey: "SUCCEEDED"}, {From: "right", To: "target", RouteKey: "SUCCEEDED"}},
	}
	candidates := workflow.OutputBindingCandidates(definition, "target", "worker")
	if len(candidates) != 1 || candidates[0].NodeID != "start" || candidates[0].Path != "worker" {
		t.Fatalf("binding candidates = %+v, want only dominating assignable start.worker", candidates)
	}
	if got := workflow.OutputBindingCandidates(definition, "target", "missing"); len(got) != 0 {
		t.Fatalf("missing target path candidates = %+v", got)
	}
}

func nodeByID(t *testing.T, definition workflow.Definition, id string) workflow.Node {
	t.Helper()
	for _, node := range definition.Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("definition has no node %q", id)
	return workflow.Node{}
}

func nodeByIDPointer(t *testing.T, definition *workflow.Definition, id string) *workflow.Node {
	t.Helper()
	for index := range definition.Nodes {
		if definition.Nodes[index].ID == id {
			return &definition.Nodes[index]
		}
	}
	t.Fatalf("definition has no node %q", id)
	return nil
}
