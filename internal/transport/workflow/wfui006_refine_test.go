package workflow

import (
	"context"
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	workflowcore "github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func wfui006Server(t *testing.T, authorize func(context.Context, *trust.Principal, string) bool) (*server, *wfui005AuthoringStore, *workflowv1.WorkflowDraftView) {
	t.Helper()
	definition := promotionexec.Definition()
	store := &wfui005AuthoringStore{}
	catalog := wfui005AuthoringCatalog{
		{ID: "template.promotion", Version: 1, Name: "Promotion", Kind: designerpalette.KindTemplate, Expansion: designerpalette.Expansion{Template: &definition}},
		{ID: "kernel.task", Version: 1, Name: "Task", Kind: designerpalette.KindBlock, StepType: workflowcore.StepTask},
	}
	authoring := &designeredit.Service{Store: store, Catalog: catalog, NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000206", nil }}
	srv := &server{deps: Dependencies{DraftAuthoring: authoring, Palette: catalog, Authorize: authorize}}
	created, err := srv.CreateWorkflowDraft(workflowTestContext(t, CreateWorkflowDraftProcedure), &workflowv1.CreateWorkflowDraftRequest{TemplateId: "template.promotion", TemplateVersion: 1, SemanticVersion: "1.1.1"})
	if err != nil {
		t.Fatalf("CreateWorkflowDraft: %v", err)
	}
	return srv, store, created.GetDraft()
}

func TestTodo_WF_UI_006_TransportTypedRefinementAndOverlay(t *testing.T) {
	srv, store, draft := wfui006Server(t, allowWorkflowCalls)
	updated, err := srv.UpdateWorkflowDraftNode(workflowTestContext(t, UpdateWorkflowDraftNodeProcedure), &workflowv1.UpdateWorkflowDraftNodeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), NodeId: promotionexec.NodeAwaitPayrollConfirmation,
		Values: map[string]string{"display_name": "Payroll acknowledgement", "signal_timeout_seconds": "7200"},
	})
	if err != nil {
		t.Fatalf("UpdateWorkflowDraftNode: %v", err)
	}
	if updated.GetDraft().GetRevision() != 2 || len(updated.GetDraft().GetNodes()) != len(draft.GetNodes()) {
		t.Fatalf("updated draft = %+v", updated.GetDraft())
	}
	var signal *workflowv1.WorkflowDraftNode
	for _, node := range updated.GetDraft().GetNodes() {
		if node.GetId() == promotionexec.NodeAwaitPayrollConfirmation {
			signal = node
		}
	}
	if signal == nil || signal.GetLabel() != "Payroll acknowledgement" || len(signal.GetParameters()) < 3 {
		t.Fatalf("projected signal inspector = %+v", signal)
	}

	_, err = srv.ApplyWorkflowTemplateOverlay(workflowTestContext(t, ApplyWorkflowTemplateOverlayProcedure), &workflowv1.ApplyWorkflowTemplateOverlayRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: 2, Operation: "OMIT", TargetNodeId: promotionexec.NodeApproveManager, Reason: "try to bypass approval",
	})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodeInvalidArgument || store.draft.Revision != 2 {
		t.Fatalf("locked overlay = %v, revision %d", err, store.draft.Revision)
	}

	added, err := srv.ApplyWorkflowTemplateOverlay(workflowTestContext(t, ApplyWorkflowTemplateOverlayProcedure), &workflowv1.ApplyWorkflowTemplateOverlayRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: 2, Operation: "ADD", EntryId: "kernel.task", EntryVersion: 1,
	})
	if err != nil || added.GetDraft().GetRevision() != 3 || len(added.GetDraft().GetOverlays()) != 1 || added.GetDraft().GetOverlays()[0].GetOperation() != "ADD" {
		t.Fatalf("recorded add overlay = %+v, %v", added, err)
	}
}

func TestTodo_WF_UI_006_SecurityAuthorizesBeforeMutation(t *testing.T) {
	srv, store, draft := wfui006Server(t, allowWorkflowCalls)
	srv.deps.Authorize = func(context.Context, *trust.Principal, string) bool { return false }
	before := store.saves
	_, err := srv.UpdateWorkflowDraftNode(workflowTestContext(t, UpdateWorkflowDraftNodeProcedure), &workflowv1.UpdateWorkflowDraftNodeRequest{DraftId: draft.GetDraftId(), ExpectedRevision: 1, NodeId: promotionexec.NodeSnapshotWorker, Values: map[string]string{"display_name": "Denied"}})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodePermissionDenied || store.saves != before {
		t.Fatalf("denied typed update = %v, saves %d -> %d", err, before, store.saves)
	}
	_, err = srv.ApplyWorkflowTemplateOverlay(workflowTestContext(t, ApplyWorkflowTemplateOverlayProcedure), &workflowv1.ApplyWorkflowTemplateOverlayRequest{DraftId: draft.GetDraftId(), ExpectedRevision: 1, Operation: "ADD", EntryId: "kernel.task", EntryVersion: 1})
	if err == nil || store.saves != before {
		t.Fatalf("denied overlay = %v, saves %d -> %d", err, before, store.saves)
	}
}
