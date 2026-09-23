package main

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

func TestTodo_WF_UI_005_AuthoringControllerCreatesExactTemplate(t *testing.T) {
	var got *workflowv1.CreateWorkflowDraftRequest
	controller := newWorkflowAuthoringController(context.Background(), productclient.Service{
		CreateWorkflowDraft: func(_ context.Context, request *workflowv1.CreateWorkflowDraftRequest) (*workflowv1.CreateWorkflowDraftResponse, error) {
			got = request
			return &workflowv1.CreateWorkflowDraftResponse{Draft: &workflowv1.WorkflowDraftView{DraftId: "draft-42"}}, nil
		},
	})
	result := make(chan string, 1)
	controller.Create(productui.WorkflowDraftCreateRequest{Template: &productui.WorkflowPaletteItem{ID: "template.promotion", Version: 7}}, func(id string, err error) {
		if err != nil {
			t.Errorf("Create callback error = %v", err)
		}
		result <- id
	})
	if id := <-result; id != "draft-42" {
		t.Fatalf("created draft = %q", id)
	}
	want := &workflowv1.CreateWorkflowDraftRequest{TemplateId: "template.promotion", TemplateVersion: 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("create request = %+v, want %+v", got, want)
	}
}

func TestAuthoringControllerCreatesPinnedSemanticSuccessor(t *testing.T) {
	var got *workflowv1.CreateWorkflowDraftRequest
	controller := newWorkflowAuthoringController(context.Background(), productclient.Service{
		CreateWorkflowDraft: func(_ context.Context, request *workflowv1.CreateWorkflowDraftRequest) (*workflowv1.CreateWorkflowDraftResponse, error) {
			got = request
			return &workflowv1.CreateWorkflowDraftResponse{Draft: &workflowv1.WorkflowDraftView{DraftId: "draft-successor"}}, nil
		},
	})
	done := make(chan error, 1)
	controller.Create(productui.WorkflowDraftCreateRequest{
		WorkflowID: "workflow.promotion", Name: "Promotion", SemanticVersion: "2.1.0", BaseVersionDigest: "sha256:base",
	}, func(_ string, err error) { done <- err })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got.GetWorkflowId() != "workflow.promotion" || got.GetName() != "Promotion" || got.GetSemanticVersion() != "2.1.0" || got.GetBaseVersionDigest() != "sha256:base" {
		t.Fatalf("successor request = %+v", got)
	}
}

func TestTodo_WF_UI_005_AuthoringControllerSerializesOptimisticEdits(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	done := make(chan error, 2)
	var calls, inFlight, maxInFlight atomic.Int32
	var requests [2]*workflowv1.InsertWorkflowPaletteEntryRequest
	controller := newWorkflowAuthoringController(context.Background(), productclient.Service{
		InsertWorkflowPaletteEntry: func(_ context.Context, request *workflowv1.InsertWorkflowPaletteEntryRequest) (*workflowv1.InsertWorkflowPaletteEntryResponse, error) {
			index := int(calls.Add(1)) - 1
			requests[index] = request
			current := inFlight.Add(1)
			for observed := maxInFlight.Load(); current > observed && !maxInFlight.CompareAndSwap(observed, current); observed = maxInFlight.Load() {
			}
			if index == 0 {
				close(firstStarted)
				<-releaseFirst
			}
			inFlight.Add(-1)
			return &workflowv1.InsertWorkflowPaletteEntryResponse{}, nil
		},
	})
	draft := productui.WorkflowDraftView{DraftID: "draft-42", Revision: 4}
	controller.Insert(draft, productui.WorkflowPaletteItem{ID: "kernel.task", Version: 1}, func(err error) { done <- err })
	<-firstStarted
	controller.Insert(draft, productui.WorkflowPaletteItem{ID: "fragment.review", Version: 2}, func(err error) { done <- err })
	if calls.Load() != 1 {
		t.Fatalf("second edit ran before the first completed: calls = %d", calls.Load())
	}
	close(releaseFirst)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if maxInFlight.Load() != 1 {
		t.Fatalf("maximum concurrent mutations = %d, want 1", maxInFlight.Load())
	}
	if requests[0].GetExpectedRevision() != 4 || requests[0].GetEntryId() != "kernel.task" || requests[1].GetExpectedRevision() != 4 || requests[1].GetEntryId() != "fragment.review" {
		t.Fatalf("serialized requests = %+v", requests)
	}
}

func TestTodo_WF_UI_005_AuthoringControllerFailsClosedWithoutRPCs(t *testing.T) {
	controller := newWorkflowAuthoringController(context.Background(), productclient.Service{})
	createDone := make(chan error, 1)
	controller.Create(productui.WorkflowDraftCreateRequest{}, func(_ string, err error) { createDone <- err })
	if err := <-createDone; err == nil {
		t.Fatal("Create succeeded without an RPC")
	}
	insertDone := make(chan error, 1)
	controller.Insert(productui.WorkflowDraftView{DraftID: "draft-42", Revision: 1}, productui.WorkflowPaletteItem{ID: "kernel.task", Version: 1}, func(err error) { insertDone <- err })
	if err := <-insertDone; err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("Insert error = %v, want explicit unavailable error", err)
	}
}

func TestTodo_WF_UI_005_AuthoringControllerRejectsEditsAfterPageCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	controller := newWorkflowAuthoringController(ctx, productclient.Service{
		InsertWorkflowPaletteEntry: func(context.Context, *workflowv1.InsertWorkflowPaletteEntryRequest) (*workflowv1.InsertWorkflowPaletteEntryResponse, error) {
			calls.Add(1)
			return &workflowv1.InsertWorkflowPaletteEntryResponse{}, nil
		},
	})
	done := make(chan error, 1)
	controller.Insert(
		productui.WorkflowDraftView{DraftID: "draft-42", Revision: 4},
		productui.WorkflowPaletteItem{ID: "kernel.task", Version: 1},
		func(err error) { done <- err },
	)
	if err := <-done; err == nil {
		t.Fatal("Insert succeeded after the page lifecycle was cancelled")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("RPC calls after cancellation = %d, want 0", got)
	}
}

func TestTodo_WF_UI_005_SavedEditRevalidatesTheCurrentDraftRoute(t *testing.T) {
	called := 0
	if !revalidateWorkflowDraft(func() { called++ }) {
		t.Fatal("revalidateWorkflowDraft refused a mounted route")
	}
	if called != 1 {
		t.Fatalf("route revalidations = %d, want 1", called)
	}
	if revalidateWorkflowDraft(nil) {
		t.Fatal("revalidateWorkflowDraft accepted a missing route")
	}
}

func TestTodo_WF_UI_009_AuthoringBusyStateSettlesOnlyAfterSuccessfulDraftReload(t *testing.T) {
	if !shouldSettleWorkflowAuthoringBusy(productui.PageWorkflowDesigner, nil) {
		t.Fatal("successful workflow reload did not settle authoring busy state")
	}
	if shouldSettleWorkflowAuthoringBusy(productui.PageWorkflowDesigner, errors.New("load failed")) {
		t.Fatal("failed workflow reload incorrectly settled authoring busy state")
	}
	if shouldSettleWorkflowAuthoringBusy(productui.PagePeople, nil) {
		t.Fatal("unrelated page load settled workflow authoring busy state")
	}
}

func TestTodo_WF_UI_006_AuthoringControllerSerializesTypedRefinementsAndOverlays(t *testing.T) {
	var updates []*workflowv1.UpdateWorkflowDraftNodeRequest
	var overlays []*workflowv1.ApplyWorkflowTemplateOverlayRequest
	service := productclient.Service{
		UpdateWorkflowDraftNode: func(_ context.Context, request *workflowv1.UpdateWorkflowDraftNodeRequest) (*workflowv1.UpdateWorkflowDraftNodeResponse, error) {
			updates = append(updates, request)
			return &workflowv1.UpdateWorkflowDraftNodeResponse{}, nil
		},
		ApplyWorkflowTemplateOverlay: func(_ context.Context, request *workflowv1.ApplyWorkflowTemplateOverlayRequest) (*workflowv1.ApplyWorkflowTemplateOverlayResponse, error) {
			overlays = append(overlays, request)
			return &workflowv1.ApplyWorkflowTemplateOverlayResponse{}, nil
		},
	}
	controller := newWorkflowAuthoringController(context.Background(), service)
	draft := productui.WorkflowDraftView{DraftID: "draft-6", Revision: 9}
	done := make(chan error, 2)
	controller.UpdateNode(draft, productui.WorkflowNodeParameterChange{NodeID: "signal", Values: map[string]string{"signal_timeout_seconds": "7200"}}, func(err error) { done <- err })
	controller.ApplyOverlay(draft, productui.WorkflowTemplateOverlayChange{Operation: "REPLACE", TargetNodeID: "simulate", EntryID: "kernel.task", EntryVersion: 1, Reason: "customer review"}, func(err error) { done <- err })
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if len(updates) != 1 || updates[0].GetExpectedRevision() != 9 || updates[0].GetValues()["signal_timeout_seconds"] != "7200" {
		t.Fatalf("typed update requests = %+v", updates)
	}
	if len(overlays) != 1 || overlays[0].GetOperation() != "REPLACE" || overlays[0].GetReason() != "customer review" {
		t.Fatalf("overlay requests = %+v", overlays)
	}
}

func TestTodo_WF_UI_007_AuthoringControllerSendsRevisionFencedLinks(t *testing.T) {
	var outcome *workflowv1.SetWorkflowDraftOutcomeRequest
	var binding *workflowv1.BindWorkflowDraftInputRequest
	service := productclient.Service{
		SetWorkflowDraftOutcome: func(_ context.Context, request *workflowv1.SetWorkflowDraftOutcomeRequest) (*workflowv1.SetWorkflowDraftOutcomeResponse, error) {
			outcome = request
			return &workflowv1.SetWorkflowDraftOutcomeResponse{}, nil
		},
		BindWorkflowDraftInput: func(_ context.Context, request *workflowv1.BindWorkflowDraftInputRequest) (*workflowv1.BindWorkflowDraftInputResponse, error) {
			binding = request
			return &workflowv1.BindWorkflowDraftInputResponse{}, nil
		},
	}
	controller := newWorkflowAuthoringController(context.Background(), service)
	draft := productui.WorkflowDraftView{DraftID: "draft-7", Revision: 12}
	done := make(chan error, 2)
	controller.SetOutcome(draft, productui.WorkflowOutcomeChange{FromNodeID: "decision", RouteKey: "APPROVED", ToNodeID: "commit"}, func(err error) { done <- err })
	controller.BindInput(draft, productui.WorkflowInputBindingChange{TargetNodeID: "commit", TargetPath: "worker_id", SourceNodeID: "snapshot", SourcePath: "worker_id"}, func(err error) { done <- err })
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if outcome.GetDraftId() != "draft-7" || outcome.GetExpectedRevision() != 12 || outcome.GetRouteKey() != "APPROVED" || outcome.GetToNodeId() != "commit" {
		t.Fatalf("outcome request = %+v", outcome)
	}
	if binding.GetDraftId() != "draft-7" || binding.GetExpectedRevision() != 12 || binding.GetTargetPath() != "worker_id" || binding.GetSourceNodeId() != "snapshot" {
		t.Fatalf("binding request = %+v", binding)
	}
}

func TestTodo_WF_UI_009_AuthoringControllerSendsRevisionFencedMove(t *testing.T) {
	var moved *workflowv1.MoveWorkflowDraftNodeRequest
	service := productclient.Service{
		MoveWorkflowDraftNode: func(_ context.Context, request *workflowv1.MoveWorkflowDraftNodeRequest) (*workflowv1.MoveWorkflowDraftNodeResponse, error) {
			moved = request
			return &workflowv1.MoveWorkflowDraftNodeResponse{}, nil
		},
	}
	controller := newWorkflowAuthoringController(context.Background(), service)
	done := make(chan error, 1)
	controller.MoveNode(productui.WorkflowDraftView{DraftID: "draft-9", Revision: 17}, productui.WorkflowNodeMove{NodeID: "review", Direction: "LATER"}, func(err error) { done <- err })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if moved.GetDraftId() != "draft-9" || moved.GetExpectedRevision() != 17 || moved.GetNodeId() != "review" || moved.GetDirection() != "LATER" {
		t.Fatalf("move request = %+v", moved)
	}
}

func TestAuthoringControllerSendsRevisionFencedDeletions(t *testing.T) {
	var removed *workflowv1.RemoveWorkflowDraftNodeRequest
	var cleared *workflowv1.ClearWorkflowDraftOutcomeRequest
	var renamed *workflowv1.RenameWorkflowDraftRequest
	service := productclient.Service{
		RemoveWorkflowDraftNode: func(_ context.Context, request *workflowv1.RemoveWorkflowDraftNodeRequest) (*workflowv1.RemoveWorkflowDraftNodeResponse, error) {
			removed = request
			return &workflowv1.RemoveWorkflowDraftNodeResponse{}, nil
		},
		ClearWorkflowDraftOutcome: func(_ context.Context, request *workflowv1.ClearWorkflowDraftOutcomeRequest) (*workflowv1.ClearWorkflowDraftOutcomeResponse, error) {
			cleared = request
			return &workflowv1.ClearWorkflowDraftOutcomeResponse{}, nil
		},
		RenameWorkflowDraft: func(_ context.Context, request *workflowv1.RenameWorkflowDraftRequest) (*workflowv1.RenameWorkflowDraftResponse, error) {
			renamed = request
			return &workflowv1.RenameWorkflowDraftResponse{}, nil
		},
	}
	controller := newWorkflowAuthoringController(context.Background(), service)
	draft := productui.WorkflowDraftView{DraftID: "draft-11", Revision: 31}
	done := make(chan error, 3)
	controller.RemoveNode(draft, "review", func(err error) { done <- err })
	controller.ClearOutcome(draft, productui.WorkflowOutcomeChange{FromNodeID: "decision", RouteKey: "APPROVED"}, func(err error) { done <- err })
	controller.Rename(draft, "Promotion, revised", func(err error) { done <- err })
	for range 3 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if removed.GetDraftId() != "draft-11" || removed.GetExpectedRevision() != 31 || removed.GetNodeId() != "review" {
		t.Fatalf("remove request = %+v", removed)
	}
	// An empty to_node_id is the documented "clear every target" form, so the
	// controller must send it rather than refuse the command.
	if cleared.GetExpectedRevision() != 31 || cleared.GetFromNodeId() != "decision" || cleared.GetRouteKey() != "APPROVED" || cleared.GetToNodeId() != "" {
		t.Fatalf("clear request = %+v", cleared)
	}
	if renamed.GetExpectedRevision() != 31 || renamed.GetName() != "Promotion, revised" {
		t.Fatalf("rename request = %+v", renamed)
	}
}

func TestAuthoringControllerFailsClosedOnIncompleteDeletions(t *testing.T) {
	var calls atomic.Int32
	service := productclient.Service{
		RemoveWorkflowDraftNode: func(context.Context, *workflowv1.RemoveWorkflowDraftNodeRequest) (*workflowv1.RemoveWorkflowDraftNodeResponse, error) {
			calls.Add(1)
			return &workflowv1.RemoveWorkflowDraftNodeResponse{}, nil
		},
		ClearWorkflowDraftOutcome: func(context.Context, *workflowv1.ClearWorkflowDraftOutcomeRequest) (*workflowv1.ClearWorkflowDraftOutcomeResponse, error) {
			calls.Add(1)
			return &workflowv1.ClearWorkflowDraftOutcomeResponse{}, nil
		},
		RenameWorkflowDraft: func(context.Context, *workflowv1.RenameWorkflowDraftRequest) (*workflowv1.RenameWorkflowDraftResponse, error) {
			calls.Add(1)
			return &workflowv1.RenameWorkflowDraftResponse{}, nil
		},
	}
	controller := newWorkflowAuthoringController(context.Background(), service)
	fenced := productui.WorkflowDraftView{DraftID: "draft-11", Revision: 31}
	unfenced := productui.WorkflowDraftView{DraftID: "draft-11"}
	done := make(chan error, 5)
	controller.RemoveNode(fenced, "", func(err error) { done <- err })
	controller.RemoveNode(unfenced, "review", func(err error) { done <- err })
	controller.ClearOutcome(fenced, productui.WorkflowOutcomeChange{FromNodeID: "decision"}, func(err error) { done <- err })
	controller.Rename(fenced, "   ", func(err error) { done <- err })
	controller.Rename(unfenced, "Promotion", func(err error) { done <- err })
	for range 5 {
		if err := <-done; err == nil {
			t.Fatal("an incomplete deletion was accepted")
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("RPC calls for incomplete deletions = %d, want 0", got)
	}

	without := newWorkflowAuthoringController(context.Background(), productclient.Service{})
	missing := make(chan error, 3)
	without.RemoveNode(fenced, "review", func(err error) { missing <- err })
	without.ClearOutcome(fenced, productui.WorkflowOutcomeChange{FromNodeID: "decision", RouteKey: "APPROVED"}, func(err error) { missing <- err })
	without.Rename(fenced, "Promotion", func(err error) { missing <- err })
	for range 3 {
		if err := <-missing; err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("missing RPC error = %v, want an explicit unavailable error", err)
		}
	}
}

func TestTodo_WF_UI_010_AuthoringControllerSendsRevisionFencedHistoryNavigation(t *testing.T) {
	var navigated *workflowv1.NavigateWorkflowDraftHistoryRequest
	service := productclient.Service{
		NavigateWorkflowDraftHistory: func(_ context.Context, request *workflowv1.NavigateWorkflowDraftHistoryRequest) (*workflowv1.NavigateWorkflowDraftHistoryResponse, error) {
			navigated = request
			return &workflowv1.NavigateWorkflowDraftHistoryResponse{}, nil
		},
	}
	controller := newWorkflowAuthoringController(context.Background(), service)
	done := make(chan error, 1)
	controller.NavigateHistory(productui.WorkflowDraftView{DraftID: "draft-10", Revision: 23, CanUndo: true}, "UNDO", func(err error) { done <- err })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if navigated.GetDraftId() != "draft-10" || navigated.GetExpectedRevision() != 23 || navigated.GetDirection() != "UNDO" {
		t.Fatalf("history request = %+v", navigated)
	}
	refused := make(chan error, 1)
	controller.NavigateHistory(productui.WorkflowDraftView{DraftID: "draft-10", Revision: 23}, "UNDO", func(err error) { refused <- err })
	if err := <-refused; err == nil {
		t.Fatal("unavailable undo was accepted")
	}
}
