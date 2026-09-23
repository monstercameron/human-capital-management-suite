package workflow

import (
	"context"
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func TestTodo_WF_UI_007_TransportLinksOutcomeAndBindsCompilerCandidate(t *testing.T) {
	srv, _, draft := wfui006Server(t, allowWorkflowCalls)
	linked, err := srv.SetWorkflowDraftOutcome(workflowTestContext(t, SetWorkflowDraftOutcomeProcedure), &workflowv1.SetWorkflowDraftOutcomeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), FromNodeId: promotionexec.NodeRaiseThreshold,
		RouteKey: "WITHIN_THRESHOLD", ToNodeId: promotionexec.NodeApproveFinance,
	})
	if err != nil {
		t.Fatalf("SetWorkflowDraftOutcome: %v", err)
	}
	if linked.GetDraft().GetRevision() != draft.GetRevision()+1 {
		t.Fatalf("linked revision = %d, want %d", linked.GetDraft().GetRevision(), draft.GetRevision()+1)
	}
	node := projectedDraftNode(t, linked.GetDraft(), promotionexec.NodeRaiseThreshold)
	if !projectedOutcomeTargets(node, "WITHIN_THRESHOLD", promotionexec.NodeApproveFinance) {
		t.Fatalf("linked node outcomes = %+v", node.GetOutcomes())
	}

	bound, err := srv.BindWorkflowDraftInput(workflowTestContext(t, BindWorkflowDraftInputProcedure), &workflowv1.BindWorkflowDraftInputRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: linked.GetDraft().GetRevision(), TargetNodeId: promotionexec.NodeRaiseThreshold,
		TargetPath: "raise_ratio", SourceNodeId: promotionexec.NodeSimulateCompensation, SourcePath: "raise_ratio",
	})
	if err != nil {
		t.Fatalf("BindWorkflowDraftInput: %v", err)
	}
	bindingNode := projectedDraftNode(t, bound.GetDraft(), promotionexec.NodeRaiseThreshold)
	found := false
	for _, binding := range bindingNode.GetBindings() {
		if binding.GetTargetPath() != "raise_ratio" {
			continue
		}
		found = binding.GetSourceNodeId() == promotionexec.NodeSimulateCompensation && binding.GetSourcePath() == "raise_ratio" && len(binding.GetCandidates()) > 0
	}
	if !found {
		t.Fatalf("projected compiler binding = %+v", bindingNode.GetBindings())
	}
}

func TestTodo_WF_UI_007_SecurityAuthorizesBeforeLinkMutation(t *testing.T) {
	srv, store, draft := wfui006Server(t, allowWorkflowCalls)
	srv.deps.Authorize = func(context.Context, *trust.Principal, string) bool { return false }
	before := store.saves
	_, err := srv.SetWorkflowDraftOutcome(workflowTestContext(t, SetWorkflowDraftOutcomeProcedure), &workflowv1.SetWorkflowDraftOutcomeRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), FromNodeId: promotionexec.NodeRaiseThreshold,
		RouteKey: "WITHIN_THRESHOLD", ToNodeId: promotionexec.NodeApproveFinance,
	})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodePermissionDenied || store.saves != before {
		t.Fatalf("denied link = %v, saves %d -> %d", err, before, store.saves)
	}
}

func TestTodo_WF_UI_007_TransportRejectsNonDominatingBindingWithoutMutation(t *testing.T) {
	srv, store, draft := wfui006Server(t, allowWorkflowCalls)
	before := store.saves
	_, err := srv.BindWorkflowDraftInput(workflowTestContext(t, BindWorkflowDraftInputProcedure), &workflowv1.BindWorkflowDraftInputRequest{
		DraftId: draft.GetDraftId(), ExpectedRevision: draft.GetRevision(), TargetNodeId: promotionexec.NodeRaiseThreshold,
		TargetPath: "raise_ratio", SourceNodeId: promotionexec.NodeEndComplete, SourcePath: "raise_ratio",
	})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodeInvalidArgument || store.saves != before {
		t.Fatalf("invalid binding = %v, saves %d -> %d", err, before, store.saves)
	}
}

func projectedDraftNode(t *testing.T, draft *workflowv1.WorkflowDraftView, nodeID string) *workflowv1.WorkflowDraftNode {
	t.Helper()
	for _, node := range draft.GetNodes() {
		if node.GetId() == nodeID {
			return node
		}
	}
	t.Fatalf("node %q not found", nodeID)
	return nil
}

func projectedOutcomeTargets(node *workflowv1.WorkflowDraftNode, route, target string) bool {
	for _, outcome := range node.GetOutcomes() {
		if outcome.GetRouteKey() != route {
			continue
		}
		for _, candidate := range outcome.GetTargetNodeIds() {
			if candidate == target {
				return true
			}
		}
	}
	return false
}
