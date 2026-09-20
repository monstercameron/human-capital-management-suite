package productclient

import (
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
)

func TestTodo_WF_UI_010_ProductProjectionValidatesHistoryAndDiff(t *testing.T) {
	value := &workflowv1.WorkflowDraftView{
		DraftId: "draft-10", WorkflowId: "workflow.people.change", Name: "People change", SemanticVersion: "1.2.0", Revision: 8,
		HistoryPosition: 2, HistoryLength: 3, CanUndo: true, CanRedo: true, HistoryLabel: "Connect review", LayoutMode: "AUTO",
		SemanticChanges: []*workflowv1.WorkflowDraftSemanticChange{{Kind: "EDGE", Operation: "ADDED", SubjectId: "review", Field: "APPROVED", After: "end"}},
	}
	projected, err := projectWorkflowDraft(value)
	if err != nil {
		t.Fatalf("projectWorkflowDraft: %v", err)
	}
	if !projected.CanUndo || !projected.CanRedo || projected.HistoryPosition != 2 || projected.HistoryLength != 3 || projected.LayoutMode != "AUTO" || len(projected.Changes) != 1 {
		t.Fatalf("projected history = %+v", projected)
	}
	value.CanUndo = false
	if _, err = projectWorkflowDraft(value); err == nil {
		t.Fatal("inconsistent can_undo was accepted")
	}
	value.CanUndo = true
	value.SemanticChanges[0].Kind = "RAW_DOCUMENT"
	if _, err = projectWorkflowDraft(value); err == nil {
		t.Fatal("unknown semantic change kind was accepted")
	}
}
