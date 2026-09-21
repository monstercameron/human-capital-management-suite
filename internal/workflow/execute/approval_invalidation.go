package execute

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

// WF-STEP-018: requirement invalidators.
//
// [Driver.InvalidateApproval] fires one declared invalidator against a waiting
// APPROVAL node. In one transaction it confirms the change is an invalidator
// the continuation's own compiled requirements declare, withdraws every
// pending decision durably (a workflow_approval_withdrawal row per completed
// slot, since decisions themselves are immutable), cancels every still-open
// slot, resolves the node INVALIDATED through [stepapproval.Resolve] and
// advances it down the plan's INVALIDATED edge. An undeclared change, or a
// node that has already resolved, is refused with nothing written.

// ApprovalInvalidationRequest names the material change and the approval it
// invalidates.
type ApprovalInvalidationRequest struct {
	// Start is the context the instance runs under; its proposal is the one
	// the continuation's votes were cast against.
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	Continuation            stepapproval.Continuation
	// Requirements are the compiled requirements the continuation pinned. Each
	// must reproduce the digest the continuation recorded.
	Requirements humanwork.RequirementSet
	Change       humanwork.InvalidatorKind
	// Reason and EvidenceRef describe the change: why it is material and where
	// its evidence lives (a superseding proposal revision, a revoked grant).
	Reason      string
	EvidenceRef string
	RecordedAt  time.Time
	// Meta is recorded on every slot cancellation; Meta.At defaults to
	// RecordedAt and its actor is the withdrawal's withdrawer.
	Meta workitem.TransitionMeta
}

// ApprovalInvalidationResult is the INVALIDATED advancement and what it
// withdrew and closed.
type ApprovalInvalidationResult struct {
	Result
	Resolution  stepapproval.Resolution
	Invalidator humanwork.Invalidator
	Withdrawn   []stepapproval.Withdrawal
	Cancelled   []workitem.WorkItem
}

// InvalidateApproval withdraws the pending decisions of a waiting APPROVAL
// node and routes it INVALIDATED, atomically.
func (d *Driver) InvalidateApproval(ctx context.Context, req ApprovalInvalidationRequest) (ret0 ApprovalInvalidationResult, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.invalidate_approval", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.Start.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.ExpectedInstanceVersion < 1 {
		return ApprovalInvalidationResult{}, invalid("approval invalidation requires tenant, instance and a positive instance version")
	}
	if req.RecordedAt.IsZero() || req.Reason == "" || req.EvidenceRef == "" {
		return ApprovalInvalidationResult{}, invalid("approval invalidation requires RecordedAt, a reason and change evidence")
	}
	at := req.RecordedAt.UTC()
	meta := req.Meta
	if meta.At.IsZero() {
		meta.At = at
	}
	if err := meta.Validate(); err != nil {
		return ApprovalInvalidationResult{}, err
	}
	selection, err := resolvePinnedPlan(ctx, req.Start, "approval invalidation")
	if err != nil {
		return ApprovalInvalidationResult{}, err
	}
	if err := validateApprovalContinuation(req.Start, req.InstanceID, req.Continuation, selection); err != nil {
		return ApprovalInvalidationResult{}, err
	}
	invalidator, err := stepapproval.DeclaredInvalidator(req.Continuation, req.Requirements, req.Change)
	if err != nil {
		return ApprovalInvalidationResult{}, err
	}
	ctx, release, err := d.acquireInstanceLease(ctx, req.Start.TenantID, req.InstanceID)
	if err != nil {
		return ApprovalInvalidationResult{}, err
	}
	defer func() { retErr = releasing(retErr, release) }()

	run := runContext{
		start: req.Start, selection: selection, instanceID: req.InstanceID,
		traceID: d.opts.Instrumentation.TraceID(ctx),
	}
	out := ApprovalInvalidationResult{Invalidator: invalidator}
	advanced, created, evidenceIDs, timers, err := d.advanceOnce(ctx, run, req.ExpectedInstanceVersion, at, 1,
		func(ctx context.Context, ex runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error) {
			outcome, refs, invErr := invalidateApprovalSlots(ctx, ex, req, invalidator, meta, at, &out)
			return outcome, refs, nil, invErr
		}, nil)
	if err != nil {
		settled := d.settlePause(ctx, run, at, err)
		if paused, ok := pausedResult(settled, Result{}); ok {
			return ApprovalInvalidationResult{Result: paused}, nil
		}
		return ApprovalInvalidationResult{}, settled
	}
	base := Result{
		Advances: []runtime.AdvanceReceipt{advanced}, WorkItems: created, Timers: timers,
		InstanceVersion: advanced.NewInstanceVersion, Frontier: append([]string(nil), advanced.Frontier...),
		EvidenceIDs: evidenceIDs,
	}
	result, err := d.finishApprovalDrain(ctx, run, base, advanced)
	if err != nil {
		return ApprovalInvalidationResult{}, err
	}
	out.Result = result
	return out, nil
}

// invalidateApprovalSlots is the body of the invalidation transaction.
func invalidateApprovalSlots(
	ctx context.Context, ex runtime.Executor, req ApprovalInvalidationRequest, invalidator humanwork.Invalidator,
	meta workitem.TransitionMeta, at time.Time, out *ApprovalInvalidationResult,
) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	tenantID := req.Start.TenantID
	c := req.Continuation
	store := workitem.Store{}
	if _, err := store.LockApprovalSiblings(ctx, ex, tenantID, req.Start.Proposal.Revision.MaterialDigest.Digest); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	node, err := openApprovalNode(ctx, ex, tenantID, req.InstanceID, c.NodeID)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	if node.Status != runtime.NodeWaiting {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("%w: approval node %s is %s, not WAITING", ErrApprovalCompletionConflict, c.NodeID, node.Status)
	}
	items, err := store.ListForInstance(ctx, ex, tenantID, req.InstanceID)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	slots, err := stepapproval.Slots(c, items)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	reason := string(invalidator.Kind) + ": " + req.Reason
	decisions, err := stepapproval.LoadDecisions(ctx, ex, c, items)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	byDigest := make(map[string]int, len(decisions))
	for i, decision := range decisions {
		byDigest[decision.Digest()] = i
	}
	for _, slot := range slots {
		switch {
		case slot.Status == workitem.StatusCompleted:
			decision := decisions[byDigest[slot.CompletedOutputDigest]]
			withdrawn, err := stepapproval.RecordWithdrawal(ctx, ex, stepapproval.NewWithdrawal(
				c, slot, decision, invalidator, reason, req.EvidenceRef, meta.ActorPrincipalID, at))
			if err != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
			}
			out.Withdrawn = append(out.Withdrawn, withdrawn)
		case !slot.Status.Terminal():
			cancelMeta := meta
			if cancelMeta.EvidenceRef == "" {
				cancelMeta.EvidenceRef = req.EvidenceRef
			}
			cancelled, err := store.Cancel(ctx, ex, tenantID, slot.WorkItemID, slot.ItemVersion, cancelMeta)
			if err != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
			}
			out.Cancelled = append(out.Cancelled, cancelled)
		}
	}
	resolution, err := stepapproval.Resolve(c, items, nil, values.NewInstant(at),
		stepapproval.Event{Kind: stepapproval.EventInvalidated, Reason: reason})
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	out.Resolution = resolution
	refs := runtime.GovernanceRefs{
		DecisionID:  "invalidation:" + string(invalidator.Kind) + ":" + invalidator.RuleID,
		ProposalRef: req.Start.Proposal.Revision.MaterialDigest.Digest,
	}
	if len(slots) > 0 {
		refs.HumanTaskID = slots[0].WorkItemID.String()
	}
	return resolution.ToNodeOutcome(c.NodeID), refs, nil
}
