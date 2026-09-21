package designeredit_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
)

func TestTodo_WF_UI_005_FragmentInsertionCreatesCollapsibleGroup(t *testing.T) {
	current := workflow.Definition{WorkflowID: "customer.onboarding", Version: 1, Name: "Onboarding"}
	entry := designerpalette.Entry{
		ID: "fragment.review", Version: 3, Name: "Review pair", Kind: designerpalette.KindFragment,
		Expansion: designerpalette.Expansion{
			ApprovalRequirements: []workflow.ApprovalRequirement{{ID: "approval.manager", ResolverExpression: "ManagerOf(subject)", Scope: "customer", Quorum: 1}},
			Nodes: []workflow.Node{
				{ID: "manager", Type: workflow.StepApproval, DeclaredEffect: capability.EffectPure},
				{ID: "finance", Type: workflow.StepApproval, DeclaredEffect: capability.EffectPure, InputMappings: []workflow.Mapping{{Target: "review", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: "manager", Path: "review"}}}},
			},
			Edges: []workflow.Edge{{From: "manager", To: "finance", RouteKey: "APPROVED"}},
		},
	}
	before := entry.Expansion.Nodes[1].InputMappings[0].Source.NodeID

	result, err := designeredit.Insert(current, entry)
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if result.GroupID != "group_review_pair_1" {
		t.Fatalf("GroupID = %q", result.GroupID)
	}
	wantIDs := []string{"group_review_pair_1__manager", "group_review_pair_1__finance"}
	if !reflect.DeepEqual(result.InsertedNodeIDs, wantIDs) {
		t.Fatalf("InsertedNodeIDs = %#v, want %#v", result.InsertedNodeIDs, wantIDs)
	}
	if result.Definition.StartNodeID != wantIDs[0] || len(result.Definition.Edges) != 1 || result.Definition.Edges[0].From != wantIDs[0] || result.Definition.Edges[0].To != wantIDs[1] {
		t.Fatalf("inserted graph = start %q edges %#v", result.Definition.StartNodeID, result.Definition.Edges)
	}
	for _, node := range result.Definition.Nodes {
		if node.Metadata[designeredit.MetadataGroupID] != result.GroupID || node.Metadata[designeredit.MetadataGroupCollapsed] != "true" || node.Metadata[designeredit.MetadataGroupEntry] != entry.ID {
			t.Fatalf("node %q group metadata = %#v", node.ID, node.Metadata)
		}
	}
	if got := result.Definition.Nodes[1].InputMappings[0].Source.NodeID; got != wantIDs[0] {
		t.Fatalf("remapped source = %q, want %q", got, wantIDs[0])
	}
	if got := entry.Expansion.Nodes[1].InputMappings[0].Source.NodeID; got != before {
		t.Fatalf("Insert mutated registry entry: source = %q, want %q", got, before)
	}
	if len(result.Definition.ApprovalRequirements) != 1 || result.Definition.ApprovalRequirements[0].ID != "approval.manager" {
		t.Fatalf("approval requirements = %#v", result.Definition.ApprovalRequirements)
	}
}

func TestTodo_WF_UI_005_FragmentInsertionUsesStableNonCollidingGroupIDs(t *testing.T) {
	entry := designerpalette.Entry{
		ID: "fragment.review", Version: 1, Name: "Review", Kind: designerpalette.KindFragment,
		Expansion: designerpalette.Expansion{Nodes: []workflow.Node{{ID: "approval", Type: workflow.StepApproval}}},
	}
	first, err := designeredit.Insert(workflow.Definition{WorkflowID: "x", Version: 1}, entry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := designeredit.Insert(first.Definition, entry)
	if err != nil {
		t.Fatal(err)
	}
	if first.GroupID != "group_review_1" || second.GroupID != "group_review_2" || first.InsertedNodeIDs[0] == second.InsertedNodeIDs[0] {
		t.Fatalf("group ids = %q, %q; nodes = %q, %q", first.GroupID, second.GroupID, first.InsertedNodeIDs[0], second.InsertedNodeIDs[0])
	}
}

func TestTodo_WF_UI_005_TemplateAndBlockInsertion(t *testing.T) {
	template := workflow.Definition{WorkflowID: "template.promotion", Version: 4, Name: "Promotion", StartNodeID: "start", Nodes: []workflow.Node{{ID: "start", Type: workflow.StepTask}}}
	templateResult, err := designeredit.Insert(workflow.Definition{WorkflowID: "draft", Version: 1}, designerpalette.Entry{
		ID: "template.promotion", Version: 4, Name: "Promotion", Kind: designerpalette.KindTemplate,
		Expansion: designerpalette.Expansion{Template: &template},
	})
	if err != nil || templateResult.Definition.WorkflowID != template.WorkflowID || !reflect.DeepEqual(templateResult.InsertedNodeIDs, []string{"start"}) {
		t.Fatalf("template result = %+v, err = %v", templateResult, err)
	}
	if _, err := designeredit.Insert(templateResult.Definition, designerpalette.Entry{ID: "template.promotion", Version: 4, Name: "Promotion", Kind: designerpalette.KindTemplate, Expansion: designerpalette.Expansion{Template: &template}}); !errors.Is(err, designeredit.ErrConflict) {
		t.Fatalf("non-empty template insert error = %v, want ErrConflict", err)
	}
	blockResult, err := designeredit.Insert(workflow.Definition{WorkflowID: "draft", Version: 1}, designerpalette.Entry{ID: "kernel.task", Version: 1, Name: "Task", Kind: designerpalette.KindBlock, StepType: workflow.StepTask})
	if err != nil || len(blockResult.Definition.Nodes) != 1 || blockResult.Definition.Nodes[0].ID != "task_1" || blockResult.Definition.StartNodeID != "task_1" {
		t.Fatalf("block result = %+v, err = %v", blockResult, err)
	}
}

func TestTodo_WF_UI_005_InvalidFragmentIsRefusedAtomically(t *testing.T) {
	current := workflow.Definition{WorkflowID: "draft", Version: 1, Nodes: []workflow.Node{{ID: "existing", Type: workflow.StepTask}}}
	_, err := designeredit.Insert(current, designerpalette.Entry{
		ID: "fragment.bad", Version: 1, Name: "Bad", Kind: designerpalette.KindFragment,
		Expansion: designerpalette.Expansion{Nodes: []workflow.Node{{ID: "one", Type: workflow.StepTask}}, Edges: []workflow.Edge{{From: "one", To: "missing", RouteKey: "DONE"}}},
	})
	if !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("Insert() error = %v, want ErrInvalid", err)
	}
	if len(current.Nodes) != 1 || current.Nodes[0].ID != "existing" {
		t.Fatalf("invalid insertion mutated current definition: %+v", current)
	}
}
