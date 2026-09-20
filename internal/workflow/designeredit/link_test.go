package designeredit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func TestTodo_WF_UI_007(t *testing.T) {
	service, _, draft := refinementService(t)
	linked, err := service.SetOutcome(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.SetOutcomeRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, FromNodeID: promotionexec.NodeRaiseThreshold,
		RouteKey: "WITHIN_THRESHOLD", ToNodeID: promotionexec.NodeApproveFinance,
	})
	if err != nil {
		t.Fatalf("SetOutcome: %v", err)
	}
	if linked.Draft.Revision != draft.Revision+1 || !hasDraftEdge(linked.Draft, promotionexec.NodeRaiseThreshold, "WITHIN_THRESHOLD", promotionexec.NodeApproveFinance) {
		t.Fatalf("linked draft = %+v", linked.Draft)
	}

	bound, err := service.BindInput(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.BindInputRequest{
		DraftID: draft.DraftID, ExpectedRevision: linked.Draft.Revision, TargetNodeID: promotionexec.NodeRaiseThreshold,
		TargetPath: "raise_ratio", SourceNodeID: promotionexec.NodeSimulateCompensation, SourcePath: "raise_ratio",
	})
	if err != nil {
		t.Fatalf("BindInput: %v", err)
	}
	if bound.Draft.Revision != linked.Draft.Revision {
		t.Fatalf("identical compiler-valid binding changed revision to %d, want %d", bound.Draft.Revision, linked.Draft.Revision)
	}
}

func TestTodo_WF_UI_007_BrowserProjection(t *testing.T) {
	_, _, draft := refinementService(t)
	node := draftNode(t, draft, promotionexec.NodeRaiseThreshold)
	if len(node.Outcomes) != 3 {
		t.Fatalf("outcome ports = %+v", node.Outcomes)
	}
	var raiseRatio designeredit.BindingView
	for _, binding := range node.Bindings {
		if binding.TargetPath == "raise_ratio" {
			raiseRatio = binding
		}
	}
	if raiseRatio.SourceNodeID != promotionexec.NodeSimulateCompensation || raiseRatio.SourcePath != "raise_ratio" || len(raiseRatio.Candidates) == 0 {
		t.Fatalf("raise ratio binding = %+v", raiseRatio)
	}
}

func TestTodo_WF_UI_007_PropertyRejectsNonDominatingOrMismatchedSources(t *testing.T) {
	service, store, draft := refinementService(t)
	before := append([]byte(nil), store.draft.Document...)
	for _, request := range []designeredit.BindInputRequest{
		{DraftID: draft.DraftID, ExpectedRevision: draft.Revision, TargetNodeID: promotionexec.NodeRaiseThreshold, TargetPath: "raise_ratio", SourceNodeID: promotionexec.NodeEvaluateBand, SourcePath: "band_position"},
		{DraftID: draft.DraftID, ExpectedRevision: draft.Revision, TargetNodeID: promotionexec.NodeRaiseThreshold, TargetPath: "raise_ratio", SourceNodeID: promotionexec.NodeEndComplete, SourcePath: "raise_ratio"},
	} {
		if _, err := service.BindInput(context.Background(), values.TenantId("tenant-a"), "author-a", request); !errors.Is(err, designeredit.ErrInvalid) {
			t.Fatalf("invalid binding %+v = %v, want ErrInvalid", request, err)
		}
		if string(store.draft.Document) != string(before) || store.draft.Revision != draft.Revision {
			t.Fatal("rejected binding mutated the draft")
		}
	}
}

func hasDraftEdge(draft designeredit.View, from, route, to string) bool {
	for _, edge := range draft.Edges {
		if edge.From == from && edge.RouteKey == route && edge.To == to {
			return true
		}
	}
	return false
}
