package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

// WF-STEP-003: an approval that cannot be approved is closed, not left open.
//
// A decision that arrives after the routed deadline, or from an approver whose
// authority the recheck found no longer current, may not complete the item.
// Before this file the first was refused with nothing written (the item stayed
// open with no route taken) and the second could not happen at all. Both now
// close the WorkItem durably -- EXPIRED, or CANCELLED with the stale-authority
// reason on the transition -- and resolve the step through
// internal/workflow/steps/approval.Resolve to its EXPIRED or INVALIDATED route
// in the same transaction, so the caller resumes the driver down the compiled
// plan's edge exactly as it does for a completed decision.

// approvalClosure names why an open approval is closed without a decision.
type approvalClosure string

const (
	approvalClosureExpired     approvalClosure = "EXPIRED"
	approvalClosureInvalidated approvalClosure = "INVALIDATED"
	approvalClosureCancelled   approvalClosure = "CANCELLED"
)

// Transition reasons the closures record. They are read back by
// [journeyEngine.recoverClosedApproval] to finish a closure whose resume was
// interrupted.
const (
	journeyReasonExpired     = "journey.approval.expired"
	journeyReasonInvalidated = "journey.approval.invalidated"
	journeyReasonCancelled   = "journey.approval.cancelled"
)

// closureReason is the transition reason a closure records.
func closureReason(kind approvalClosure) string {
	switch kind {
	case approvalClosureExpired:
		return journeyReasonExpired
	case approvalClosureCancelled:
		return journeyReasonCancelled
	default:
		return journeyReasonInvalidated
	}
}

// closureKindOf maps a closing transition back to its closure, or reports
// that the item was closed by something other than a decision closure.
func closureKindOf(reason string, status workitem.Status) (approvalClosure, bool) {
	switch {
	case reason == journeyReasonExpired && status == workitem.StatusExpired:
		return approvalClosureExpired, true
	case reason == journeyReasonInvalidated && status == workitem.StatusCancelled:
		return approvalClosureInvalidated, true
	case reason == journeyReasonCancelled && status == workitem.StatusCancelled:
		return approvalClosureCancelled, true
	default:
		return "", false
	}
}

// closureRefusal is the error the caller returns once the closure's route has
// been taken: the decision the caller asked for was not made.
func closureRefusal(kind approvalClosure, detail string) error {
	switch kind {
	case approvalClosureExpired:
		return fmt.Errorf("%w: %s", ErrProposalDecisionExpired, detail)
	case approvalClosureCancelled:
		return fmt.Errorf("%w: %s", ErrProposalDecisionStage, detail)
	default:
		return fmt.Errorf("%w: %s", ErrProposalDecisionInvalidated, detail)
	}
}

// closureEvent is the step event a closure resolves under. An expired item
// resolves from its own EXPIRED status; an invalidated or cancelled one needs
// its event and reason, because a CANCELLED item alone always routes the
// plan's CANCELLED edge.
func closureEvent(kind approvalClosure, detail string) stepsapproval.Event {
	switch kind {
	case approvalClosureExpired:
		return stepsapproval.Event{Kind: stepsapproval.EventDecisionsChanged}
	case approvalClosureCancelled:
		return stepsapproval.Event{Kind: stepsapproval.EventCancelled, Reason: detail}
	default:
		return stepsapproval.Event{Kind: stepsapproval.EventInvalidated, Reason: detail}
	}
}

// closeApproval closes the open item on tx, resolves the step's route from the
// closed row and commits. The returned decidedApproval carries no decision and
// a refusal the caller reports after resuming the driver.
func (e *journeyEngine) closeApproval(
	ctx context.Context, tx dbport.Tx, instance runtime.Instance, revision intent.ProposalRevision,
	item workitem.WorkItem, actor string, at time.Time, kind approvalClosure, detail string,
) (decidedApproval, error) {
	store := workitem.Store{}
	meta := workitem.TransitionMeta{
		ActorPrincipalID: actor, Reason: closureReason(kind), Detail: detail, At: at,
		EvidenceRef: "approval-closure:" + string(kind) + ":" + item.WorkItemID.String(),
	}
	var closed workitem.WorkItem
	var err error
	if kind == approvalClosureExpired {
		closed, err = store.Expire(ctx, tx, item.TenantID, item.WorkItemID, item.ItemVersion, meta)
	} else {
		closed, err = store.Cancel(ctx, tx, item.TenantID, item.WorkItemID, item.ItemVersion, meta)
	}
	if err != nil {
		return decidedApproval{}, proposalWorkItemError(err)
	}
	outcome, err := resolveJourneyApproval(closed, revision, nil, at, closureEvent(kind, detail))
	if err != nil {
		return decidedApproval{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return decidedApproval{}, fmt.Errorf("app: journey: commit the approval closure: %w", err)
	}
	return decidedApproval{item: closed, instance: instance, outcome: outcome, refusal: closureRefusal(kind, detail)}, nil
}

// routeAbandonedApproval takes the CANCELLED route for an approval still open
// on a journey whose proposal has already reached a terminal RequestState
// (cancelled, withdrawn, superseded, rejected or closed): the approval can
// never be decided again, and without this the instance would wait on it
// forever. It acts only for a caller the open item admits as its decider, and
// only while the instance is parked on that item's node; anything else is left
// untouched. The caller still reports its own stage refusal.
func (e *journeyEngine) routeAbandonedApproval(
	ctx context.Context, principal *trust.Principal, intentID string, inst intent.Instance, def intent.Definition, rec IntentRecord,
) error {
	if e.svc.executor == nil {
		return nil
	}
	relationships, err := e.approvalDecisionRelationships(ctx, principal, intentID, inst)
	if err != nil {
		return err
	}
	simulated, err := e.resimulateDetailedWithRelationships(ctx, intentID, relationships)
	if err != nil {
		return err
	}
	if simulated.Revision == nil || simulated.Artifact.GetProposalRevisionId() == "" {
		return nil
	}
	start, startErr := e.svc.executionStart(inst, simulated.Artifact, journeyApprovalRef(intentID), *simulated.Revision)
	if startErr != nil {
		return journeyError(startErr)
	}
	revision := start.Proposal.Revision
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	instance, found, err := e.instanceIn(ctx, tx, principal, revision.MaterialDigest.Digest)
	if err != nil || !found || instance.RuntimeStatus.Terminal() {
		return err
	}
	items, err := (workitem.Store{}).ListForInstance(ctx, tx, e.svc.tenantUUID(principal.Tenant()), instance.InstanceID)
	if err != nil {
		return fmt.Errorf("app: journey: list the work items: %w", err)
	}
	item, open := openJourneyWorkItem(items)
	now := e.now().UTC()
	actor := principal.Subject()
	if !open || item.Kind != workitem.KindApproval || !slices.Contains(instance.CurrentNodeIDs, item.NodeID) {
		return nil
	}
	if _, deciderErr := journeyDecider(items, item, inst, actor, now); deciderErr != nil {
		return nil
	}
	done, err := e.closeApproval(ctx, tx, instance, revision, item, actor, now, approvalClosureCancelled,
		"the proposal was "+strings.ToLower(string(inst.Lifecycle.Request))+" before this approval was decided")
	if err != nil {
		return err
	}
	result, err := e.svc.executor.Resume(ctx, ExecutionResumeRequest{
		Start: start, InstanceID: done.instance.InstanceID, ExpectedInstanceVersion: done.instance.InstanceVersion,
		WorkItem: done.item, Outcome: done.outcome,
	})
	if err != nil {
		return journeyError(executionError(err))
	}
	return e.svc.consumeExecutionResult(ctx, inst, def, rec, result)
}

// recoverClosedApproval finds an approval this actor closed whose route the
// driver has not yet taken -- the closure committed, the resume did not -- and
// rebuilds the same route from the durable closing transition. It writes
// nothing. found is false when there is no such item, including when the
// instance has already moved past the closed node.
func (e *journeyEngine) recoverClosedApproval(
	ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, instance runtime.Instance,
	items []workitem.WorkItem, revision intent.ProposalRevision, actor string,
) (decidedApproval, bool, error) {
	if instance.RuntimeStatus.Terminal() {
		return decidedApproval{}, false, nil
	}
	for _, item := range items {
		if item.Kind != workitem.KindApproval || item.ProposalRef != revision.MaterialDigest.Digest ||
			(item.Status != workitem.StatusCancelled && item.Status != workitem.StatusExpired) ||
			!slices.Contains(instance.CurrentNodeIDs, item.NodeID) {
			continue
		}
		transitions, err := (workitem.Store{}).LoadTransitions(ctx, tx, tenantID, item.WorkItemID)
		if err != nil {
			return decidedApproval{}, false, fmt.Errorf("app: journey: load the approval closure: %w", err)
		}
		for i := len(transitions) - 1; i >= 0; i-- {
			closing := transitions[i]
			if closing.ToStatus != item.Status {
				continue
			}
			kind, ours := closureKindOf(closing.Reason, item.Status)
			if !ours || closing.ActorPrincipalID != actor {
				// Closed by something other than this actor's decision
				// closure; its route is not this call's to take.
				return decidedApproval{}, false, nil
			}
			outcome, err := resolveJourneyApproval(item, revision, nil, closing.At, closureEvent(kind, closing.Detail))
			if err != nil {
				return decidedApproval{}, false, err
			}
			return decidedApproval{
				item: item, instance: instance, outcome: outcome, replayed: true,
				refusal: closureRefusal(kind, closing.Detail),
			}, true, nil
		}
	}
	return decidedApproval{}, false, nil
}
