package main

import (
	"context"
	"errors"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

// workflowAuthoringController serializes optimistic draft mutations. A user
// may click again while the network is in flight, but mutations never race and
// each command is fenced by the revision that was visible when it was issued.
type workflowAuthoringController struct {
	ctx     context.Context
	service productclient.Service
	jobs    chan func()
}

func newWorkflowAuthoringController(ctx context.Context, service productclient.Service) *workflowAuthoringController {
	if ctx == nil {
		ctx = context.Background()
	}
	controller := &workflowAuthoringController{ctx: ctx, service: service, jobs: make(chan func(), 32)}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-controller.jobs:
				job()
			}
		}
	}()
	return controller
}

func (c *workflowAuthoringController) enqueue(job func()) bool {
	if c == nil || job == nil {
		return false
	}
	// Avoid select choosing the writable queue after cancellation. Once the
	// page lifecycle ends, no mutation may be accepted even if buffer capacity
	// remains.
	if c.ctx.Err() != nil {
		return false
	}
	select {
	case c.jobs <- job:
		return true
	case <-c.ctx.Done():
		return false
	default:
		return false
	}
}

func (c *workflowAuthoringController) Create(input productui.WorkflowDraftCreateRequest, done func(string, error)) {
	if c == nil || c.service.CreateWorkflowDraft == nil {
		if done != nil {
			done("", errors.New("workflow draft creation is unavailable"))
		}
		return
	}
	request := &workflowv1.CreateWorkflowDraftRequest{
		WorkflowId: input.WorkflowID, Name: input.Name, SemanticVersion: input.SemanticVersion, BaseVersionDigest: input.BaseVersionDigest,
	}
	if input.Template != nil {
		request.TemplateId, request.TemplateVersion = input.Template.ID, input.Template.Version
	}
	if !c.enqueue(func() {
		response, err := c.service.CreateWorkflowDraft(c.ctx, request)
		draftID := ""
		if response != nil && response.GetDraft() != nil {
			draftID = response.GetDraft().GetDraftId()
		}
		if done != nil {
			done(draftID, err)
		}
	}) && done != nil {
		done("", errors.New("workflow draft queue is busy"))
	}
}

func (c *workflowAuthoringController) Insert(draft productui.WorkflowDraftView, entry productui.WorkflowPaletteItem, done func(error)) {
	if c == nil || c.service.InsertWorkflowPaletteEntry == nil || draft.DraftID == "" || draft.Revision == 0 {
		if done != nil {
			done(errors.New("workflow draft insertion is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		_, err := c.service.InsertWorkflowPaletteEntry(c.ctx, &workflowv1.InsertWorkflowPaletteEntryRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, EntryId: entry.ID, EntryVersion: entry.Version,
		})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

func (c *workflowAuthoringController) UpdateNode(draft productui.WorkflowDraftView, change productui.WorkflowNodeParameterChange, done func(error)) {
	if c == nil || c.service.UpdateWorkflowDraftNode == nil || draft.DraftID == "" || draft.Revision == 0 || change.NodeID == "" || len(change.Values) == 0 {
		if done != nil {
			done(errors.New("workflow node refinement is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		_, err := c.service.UpdateWorkflowDraftNode(c.ctx, &workflowv1.UpdateWorkflowDraftNodeRequest{DraftId: draft.DraftID, ExpectedRevision: draft.Revision, NodeId: change.NodeID, Values: change.Values})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

func (c *workflowAuthoringController) SetOutcome(draft productui.WorkflowDraftView, change productui.WorkflowOutcomeChange, done func(error)) {
	if c == nil || c.service.SetWorkflowDraftOutcome == nil || draft.DraftID == "" || draft.Revision == 0 || change.FromNodeID == "" || change.RouteKey == "" || change.ToNodeID == "" {
		if done != nil {
			done(errors.New("workflow outcome linking is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		_, err := c.service.SetWorkflowDraftOutcome(c.ctx, &workflowv1.SetWorkflowDraftOutcomeRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, FromNodeId: change.FromNodeID, RouteKey: change.RouteKey, ToNodeId: change.ToNodeID,
		})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

func (c *workflowAuthoringController) BindInput(draft productui.WorkflowDraftView, change productui.WorkflowInputBindingChange, done func(error)) {
	if c == nil || c.service.BindWorkflowDraftInput == nil || draft.DraftID == "" || draft.Revision == 0 || change.TargetNodeID == "" || change.TargetPath == "" || change.SourceNodeID == "" || change.SourcePath == "" {
		if done != nil {
			done(errors.New("workflow input binding is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		_, err := c.service.BindWorkflowDraftInput(c.ctx, &workflowv1.BindWorkflowDraftInputRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, TargetNodeId: change.TargetNodeID, TargetPath: change.TargetPath, SourceNodeId: change.SourceNodeID, SourcePath: change.SourcePath,
		})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

func (c *workflowAuthoringController) MoveNode(draft productui.WorkflowDraftView, change productui.WorkflowNodeMove, done func(error)) {
	if c == nil || c.service.MoveWorkflowDraftNode == nil || draft.DraftID == "" || draft.Revision == 0 || change.NodeID == "" || (change.Direction != "EARLIER" && change.Direction != "LATER") {
		if done != nil {
			done(errors.New("workflow node movement is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		_, err := c.service.MoveWorkflowDraftNode(c.ctx, &workflowv1.MoveWorkflowDraftNodeRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, NodeId: change.NodeID, Direction: change.Direction,
		})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

func (c *workflowAuthoringController) NavigateHistory(draft productui.WorkflowDraftView, direction string, done func(error)) {
	if c == nil || c.service.NavigateWorkflowDraftHistory == nil || draft.DraftID == "" || draft.Revision == 0 ||
		(direction != "UNDO" && direction != "REDO") || (direction == "UNDO" && !draft.CanUndo) || (direction == "REDO" && !draft.CanRedo) {
		if done != nil {
			done(errors.New("workflow draft history navigation is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		_, err := c.service.NavigateWorkflowDraftHistory(c.ctx, &workflowv1.NavigateWorkflowDraftHistoryRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, Direction: direction,
		})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

func (c *workflowAuthoringController) ApplyOverlay(draft productui.WorkflowDraftView, change productui.WorkflowTemplateOverlayChange, done func(error)) {
	if c == nil || c.service.ApplyWorkflowTemplateOverlay == nil || draft.DraftID == "" || draft.Revision == 0 || change.Operation == "" {
		if done != nil {
			done(errors.New("workflow template overlay is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		_, err := c.service.ApplyWorkflowTemplateOverlay(c.ctx, &workflowv1.ApplyWorkflowTemplateOverlayRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, Operation: change.Operation, TargetNodeId: change.TargetNodeID,
			EntryId: change.EntryID, EntryVersion: change.EntryVersion, Reason: change.Reason,
		})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

// revalidateWorkflowDraft forces the mounted route loader to read the saved
// revision again. Navigating to the draft's existing URL is intentionally a
// no-op in the history router, so successful in-place edits must revalidate.
func revalidateWorkflowDraft(revalidate func()) bool {
	if revalidate == nil {
		return false
	}
	revalidate()
	return true
}

// shouldSettleWorkflowAuthoringBusy keeps mutation controls fenced until the
// authoritative draft reload has completed. In particular, an idempotent
// command produces an identical virtual tree, so the loader must explicitly
// clear the imperative live-region state instead of relying on a DOM diff.
func shouldSettleWorkflowAuthoringBusy(page productui.PageID, loadErr error) bool {
	return page == productui.PageWorkflowDesigner && loadErr == nil
}
