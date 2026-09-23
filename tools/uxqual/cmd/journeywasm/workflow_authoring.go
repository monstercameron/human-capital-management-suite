package main

import (
	"context"
	"errors"
	"strings"
	"time"

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

// workflowAuthoringCallTimeout bounds one edit. The controller runs edits one
// at a time, so a call that never returned used to hold every later edit
// behind it until the page was reloaded.
const workflowAuthoringCallTimeout = 20 * time.Second

func (c *workflowAuthoringController) callContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.ctx, workflowAuthoringCallTimeout)
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
		ctx, cancel := c.callContext()
		defer cancel()
		response, err := c.service.CreateWorkflowDraft(ctx, request)
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
	c.InsertReporting(draft, entry, func(_ []string, err error) {
		if done != nil {
			done(err)
		}
	})
}

// InsertReporting adds a library entry and reports the ids of the steps it
// created, so the editor can select what the author just added.
func (c *workflowAuthoringController) InsertReporting(draft productui.WorkflowDraftView, entry productui.WorkflowPaletteItem, done func([]string, error)) {
	if done == nil {
		done = func([]string, error) {}
	}
	if c == nil || c.service.InsertWorkflowPaletteEntry == nil || draft.DraftID == "" || draft.Revision == 0 {
		done(nil, errors.New("workflow draft insertion is unavailable"))
		return
	}
	if !c.enqueue(func() {
		ctx, cancel := c.callContext()
		defer cancel()
		response, err := c.service.InsertWorkflowPaletteEntry(ctx, &workflowv1.InsertWorkflowPaletteEntryRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, EntryId: entry.ID, EntryVersion: entry.Version,
		})
		done(append([]string(nil), response.GetInsertedNodeIds()...), err)
	}) {
		done(nil, errors.New("workflow draft queue is busy"))
	}
}

// InsertAfter adds a step and then points one result of an existing step at
// it, as two fenced edits. The second edit uses the revision the first one
// returned, so nothing else can slip between them unnoticed; if the second is
// refused the step is still there, reported as added, waiting to be connected.
func (c *workflowAuthoringController) InsertAfter(draft productui.WorkflowDraftView, entry productui.WorkflowPaletteItem, from productui.WorkflowOutcomeChange, done func([]string, error)) {
	if done == nil {
		done = func([]string, error) {}
	}
	if c == nil || c.service.InsertWorkflowPaletteEntry == nil || c.service.SetWorkflowDraftOutcome == nil || draft.DraftID == "" || draft.Revision == 0 || from.FromNodeID == "" || from.RouteKey == "" {
		done(nil, errors.New("workflow step insertion is unavailable"))
		return
	}
	if !c.enqueue(func() {
		ctx, cancel := c.callContext()
		defer cancel()
		response, err := c.service.InsertWorkflowPaletteEntry(ctx, &workflowv1.InsertWorkflowPaletteEntryRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, EntryId: entry.ID, EntryVersion: entry.Version,
		})
		inserted := append([]string(nil), response.GetInsertedNodeIds()...)
		if err != nil || len(inserted) == 0 || response.GetDraft().GetRevision() == 0 {
			done(inserted, err)
			return
		}
		_, err = c.service.SetWorkflowDraftOutcome(ctx, &workflowv1.SetWorkflowDraftOutcomeRequest{
			DraftId: draft.DraftID, ExpectedRevision: response.GetDraft().GetRevision(), FromNodeId: from.FromNodeID, RouteKey: from.RouteKey, ToNodeId: inserted[0],
		})
		done(inserted, err)
	}) {
		done(nil, errors.New("workflow draft queue is busy"))
	}
}

// SetOutcomes connects several results as a chain of fenced edits, each using
// the revision the one before it returned. It stops at the first refusal and
// reports it; what was already connected stays connected.
func (c *workflowAuthoringController) SetOutcomes(draft productui.WorkflowDraftView, changes []productui.WorkflowOutcomeChange, done func(error)) {
	if done == nil {
		done = func(error) {}
	}
	if c == nil || c.service.SetWorkflowDraftOutcome == nil || draft.DraftID == "" || draft.Revision == 0 || len(changes) == 0 {
		done(errors.New("workflow outcome linking is unavailable"))
		return
	}
	for _, change := range changes {
		if change.FromNodeID == "" || change.RouteKey == "" || change.ToNodeID == "" {
			done(errors.New("workflow outcome linking is unavailable"))
			return
		}
	}
	queued := append([]productui.WorkflowOutcomeChange(nil), changes...)
	if !c.enqueue(func() {
		revision := draft.Revision
		for _, change := range queued {
			ctx, cancel := c.callContext()
			response, err := c.service.SetWorkflowDraftOutcome(ctx, &workflowv1.SetWorkflowDraftOutcomeRequest{
				DraftId: draft.DraftID, ExpectedRevision: revision, FromNodeId: change.FromNodeID, RouteKey: change.RouteKey, ToNodeId: change.ToNodeID,
			})
			cancel()
			if err != nil {
				done(err)
				return
			}
			if next := response.GetDraft().GetRevision(); next != 0 {
				revision = next
			}
		}
		done(nil)
	}) {
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
		ctx, cancel := c.callContext()
		defer cancel()
		_, err := c.service.UpdateWorkflowDraftNode(ctx, &workflowv1.UpdateWorkflowDraftNodeRequest{DraftId: draft.DraftID, ExpectedRevision: draft.Revision, NodeId: change.NodeID, Values: change.Values})
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
		ctx, cancel := c.callContext()
		defer cancel()
		_, err := c.service.SetWorkflowDraftOutcome(ctx, &workflowv1.SetWorkflowDraftOutcomeRequest{
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
		ctx, cancel := c.callContext()
		defer cancel()
		_, err := c.service.BindWorkflowDraftInput(ctx, &workflowv1.BindWorkflowDraftInputRequest{
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
		ctx, cancel := c.callContext()
		defer cancel()
		_, err := c.service.MoveWorkflowDraftNode(ctx, &workflowv1.MoveWorkflowDraftNodeRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, NodeId: change.NodeID, Direction: change.Direction,
		})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

func (c *workflowAuthoringController) RemoveNode(draft productui.WorkflowDraftView, nodeID string, done func(error)) {
	if c == nil || c.service.RemoveWorkflowDraftNode == nil || draft.DraftID == "" || draft.Revision == 0 || nodeID == "" {
		if done != nil {
			done(errors.New("workflow node removal is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		ctx, cancel := c.callContext()
		defer cancel()
		_, err := c.service.RemoveWorkflowDraftNode(ctx, &workflowv1.RemoveWorkflowDraftNodeRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, NodeId: nodeID,
		})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

// ClearOutcome disconnects one outcome port. An empty ToNodeID is meaningful
// here -- it clears every target on the route -- so, unlike SetOutcome, this
// command does not require one.
func (c *workflowAuthoringController) ClearOutcome(draft productui.WorkflowDraftView, change productui.WorkflowOutcomeChange, done func(error)) {
	if c == nil || c.service.ClearWorkflowDraftOutcome == nil || draft.DraftID == "" || draft.Revision == 0 || change.FromNodeID == "" || change.RouteKey == "" {
		if done != nil {
			done(errors.New("workflow outcome clearing is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		ctx, cancel := c.callContext()
		defer cancel()
		_, err := c.service.ClearWorkflowDraftOutcome(ctx, &workflowv1.ClearWorkflowDraftOutcomeRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, FromNodeId: change.FromNodeID, RouteKey: change.RouteKey, ToNodeId: change.ToNodeID,
		})
		if done != nil {
			done(err)
		}
	}) && done != nil {
		done(errors.New("workflow draft queue is busy"))
	}
}

func (c *workflowAuthoringController) Rename(draft productui.WorkflowDraftView, name string, done func(error)) {
	if c == nil || c.service.RenameWorkflowDraft == nil || draft.DraftID == "" || draft.Revision == 0 || strings.TrimSpace(name) == "" {
		if done != nil {
			done(errors.New("workflow draft renaming is unavailable"))
		}
		return
	}
	if !c.enqueue(func() {
		ctx, cancel := c.callContext()
		defer cancel()
		_, err := c.service.RenameWorkflowDraft(ctx, &workflowv1.RenameWorkflowDraftRequest{
			DraftId: draft.DraftID, ExpectedRevision: draft.Revision, Name: name,
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
		ctx, cancel := c.callContext()
		defer cancel()
		_, err := c.service.NavigateWorkflowDraftHistory(ctx, &workflowv1.NavigateWorkflowDraftHistoryRequest{
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
		ctx, cancel := c.callContext()
		defer cancel()
		_, err := c.service.ApplyWorkflowTemplateOverlay(ctx, &workflowv1.ApplyWorkflowTemplateOverlayRequest{
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
