package workflow

import (
	"context"
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func TestTransportRemovesDraftNodeUnderRevisionFence(t *testing.T) {
	srv, _, draft := wfui006Server(t, allowWorkflowCalls)
	removed, err := srv.RemoveWorkflowDraftNode(workflowTestContext(t, RemoveWorkflowDraftNodeProcedure), &workflowv1.RemoveWorkflowDraftNodeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), NodeId: promotionexec.NodeSimulateCompensation,
	})
	if err != nil {
		t.Fatalf("RemoveWorkflowDraftNode: %v", err)
	}
	if removed.GetDraft().GetRevision() != draft.GetRevision()+1 || len(removed.GetDraft().GetNodes()) != len(draft.GetNodes())-1 {
		t.Fatalf("removed draft = revision %d with %d nodes", removed.GetDraft().GetRevision(), len(removed.GetDraft().GetNodes()))
	}
	for _, node := range removed.GetDraft().GetNodes() {
		if node.GetId() == promotionexec.NodeSimulateCompensation {
			t.Fatal("removed node is still projected")
		}
	}
	for _, edge := range removed.GetDraft().GetEdges() {
		if edge.GetFromId() == promotionexec.NodeSimulateCompensation || edge.GetToId() == promotionexec.NodeSimulateCompensation {
			t.Fatalf("edge %+v still references the removed node", edge)
		}
	}
}

func TestTransportRefusesRemovingALockedDraftNode(t *testing.T) {
	srv, store, draft := wfui006Server(t, allowWorkflowCalls)
	_, err := srv.RemoveWorkflowDraftNode(workflowTestContext(t, RemoveWorkflowDraftNodeProcedure), &workflowv1.RemoveWorkflowDraftNodeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), NodeId: promotionexec.NodeApproveManager,
	})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodeInvalidArgument || store.draft.Revision != draft.GetRevision() {
		t.Fatalf("locked removal = %v, revision %d", err, store.draft.Revision)
	}
}

func TestTransportClearsDraftOutcomeAndPreservesRevisionOnANoOp(t *testing.T) {
	srv, store, draft := wfui006Server(t, allowWorkflowCalls)
	cleared, err := srv.ClearWorkflowDraftOutcome(workflowTestContext(t, ClearWorkflowDraftOutcomeProcedure), &workflowv1.ClearWorkflowDraftOutcomeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), FromNodeId: promotionexec.NodeRaiseThreshold, RouteKey: "UNKNOWN",
	})
	if err != nil {
		t.Fatalf("ClearWorkflowDraftOutcome: %v", err)
	}
	if cleared.GetDraft().GetRevision() != draft.GetRevision()+1 || len(cleared.GetDraft().GetEdges()) != len(draft.GetEdges())-1 {
		t.Fatalf("cleared draft = revision %d with %d edges", cleared.GetDraft().GetRevision(), len(cleared.GetDraft().GetEdges()))
	}
	repeat, err := srv.ClearWorkflowDraftOutcome(workflowTestContext(t, ClearWorkflowDraftOutcomeProcedure), &workflowv1.ClearWorkflowDraftOutcomeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: cleared.GetDraft().GetRevision(), FromNodeId: promotionexec.NodeRaiseThreshold, RouteKey: "UNKNOWN",
	})
	if err != nil {
		t.Fatalf("repeat ClearWorkflowDraftOutcome: %v", err)
	}
	if repeat.GetDraft().GetRevision() != cleared.GetDraft().GetRevision() || store.draft.Revision != cleared.GetDraft().GetRevision() {
		t.Fatalf("repeat clear invented a revision: view=%d store=%d", repeat.GetDraft().GetRevision(), store.draft.Revision)
	}
	_, err = srv.ClearWorkflowDraftOutcome(workflowTestContext(t, ClearWorkflowDraftOutcomeProcedure), &workflowv1.ClearWorkflowDraftOutcomeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: cleared.GetDraft().GetRevision(), FromNodeId: promotionexec.NodeRaiseThreshold, RouteKey: "NOT_A_ROUTE",
	})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodeInvalidArgument {
		t.Fatalf("undeclared route = %v, want an invalid-argument envelope", err)
	}
}

func TestTransportRenamesDraftUnderRevisionFence(t *testing.T) {
	srv, store, draft := wfui006Server(t, allowWorkflowCalls)
	renamed, err := srv.RenameWorkflowDraft(workflowTestContext(t, RenameWorkflowDraftProcedure), &workflowv1.RenameWorkflowDraftRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), Name: "Promotion, revised",
	})
	if err != nil {
		t.Fatalf("RenameWorkflowDraft: %v", err)
	}
	if renamed.GetDraft().GetName() != "Promotion, revised" || renamed.GetDraft().GetRevision() != draft.GetRevision()+1 {
		t.Fatalf("renamed draft = %q at revision %d", renamed.GetDraft().GetName(), renamed.GetDraft().GetRevision())
	}
	_, err = srv.RenameWorkflowDraft(workflowTestContext(t, RenameWorkflowDraftProcedure), &workflowv1.RenameWorkflowDraftRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: renamed.GetDraft().GetRevision(), Name: "   ",
	})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodeInvalidArgument || store.draft.Revision != renamed.GetDraft().GetRevision() {
		t.Fatalf("blank rename = %v, revision %d", err, store.draft.Revision)
	}
}

func TestTransportAuthorizesBeforeEveryDraftDeletion(t *testing.T) {
	srv, store, draft := wfui006Server(t, allowWorkflowCalls)
	srv.deps.Authorize = func(context.Context, *trust.Principal, string) bool { return false }
	before := store.saves

	if _, err := srv.RemoveWorkflowDraftNode(workflowTestContext(t, RemoveWorkflowDraftNodeProcedure), &workflowv1.RemoveWorkflowDraftNodeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), NodeId: promotionexec.NodeSimulateCompensation,
	}); !deniedEnvelope(err) {
		t.Fatalf("denied removal = %v, want a permission-denied envelope", err)
	}
	if _, err := srv.ClearWorkflowDraftOutcome(workflowTestContext(t, ClearWorkflowDraftOutcomeProcedure), &workflowv1.ClearWorkflowDraftOutcomeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), FromNodeId: promotionexec.NodeRaiseThreshold, RouteKey: "UNKNOWN",
	}); !deniedEnvelope(err) {
		t.Fatalf("denied clear = %v, want a permission-denied envelope", err)
	}
	if _, err := srv.RenameWorkflowDraft(workflowTestContext(t, RenameWorkflowDraftProcedure), &workflowv1.RenameWorkflowDraftRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), Name: "Renamed by an unauthorized caller",
	}); !deniedEnvelope(err) {
		t.Fatalf("denied rename = %v, want a permission-denied envelope", err)
	}
	if store.saves != before {
		t.Fatalf("denied deletions saved: %d -> %d", before, store.saves)
	}
}

func deniedEnvelope(err error) bool {
	owned, ok := envelope.As(err)
	return ok && owned.Code() == envelope.CodePermissionDenied
}
