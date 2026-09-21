package workflow

import (
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_WF_UI_009_TransportMovesNodeUnderRevisionFence(t *testing.T) {
	srv, _, draft := wfui006Server(t, nil)
	if len(draft.GetNodes()) < 2 {
		t.Fatal("promotion draft has fewer than two nodes")
	}
	first, second := draft.GetNodes()[0].GetId(), draft.GetNodes()[1].GetId()
	moved, err := srv.MoveWorkflowDraftNode(workflowTestContext(t, MoveWorkflowDraftNodeProcedure), &workflowv1.MoveWorkflowDraftNodeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), NodeId: second, Direction: "EARLIER",
	})
	if err != nil {
		t.Fatalf("MoveWorkflowDraftNode: %v", err)
	}
	if moved.GetDraft().GetRevision() != draft.GetRevision()+1 || moved.GetDraft().GetNodes()[0].GetId() != second || moved.GetDraft().GetNodes()[1].GetId() != first {
		t.Fatalf("moved draft = %+v", moved.GetDraft())
	}
	if len(moved.GetDraft().GetEdges()) != len(draft.GetEdges()) {
		t.Fatal("presentation move changed execution routes")
	}
}

func TestTodo_WF_UI_009_TransportAuthorizesBeforeMove(t *testing.T) {
	srv, store, draft := wfui006Server(t, nil)
	srv.deps.Authorize = func(*trust.Principal, string) bool { return false }
	before := store.saves
	_, err := srv.MoveWorkflowDraftNode(workflowTestContext(t, MoveWorkflowDraftNodeProcedure), &workflowv1.MoveWorkflowDraftNodeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), NodeId: draft.GetNodes()[1].GetId(), Direction: "EARLIER",
	})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodePermissionDenied || store.saves != before {
		t.Fatalf("denied move = %v, saves %d -> %d", err, before, store.saves)
	}
}
