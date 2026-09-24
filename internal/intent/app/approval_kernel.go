package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

// WF-STEP-018: the served approval decision runs on the generic approval
// kernel (internal/workflow/execute.Driver.CompleteApproval and
// InvalidateApproval), not on a promotion-specific claim/complete/commit
// followed by a separate Resume.
//
// The journey keeps what only it knows -- who the verified caller is, the
// routed candidate that admits them, the WF-STEP-003 authority recheck
// against current durable facts, and the intent_decision row -- and hands it
// to the kernel as hooks that run inside the kernel's one vote transaction.
// The kernel owns everything generic: the sibling lock, the distinct-approver
// and separation-of-duties refusals, the WorkItem completion and decision
// record, quorum resolution over durable votes, and the advancement.

// ApprovalVoteRequest is one approver's vote on one routed approval WorkItem.
type ApprovalVoteRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	WorkItemID              uuid.UUID
	ExpectedWorkItemVersion int64
	Continuation            stepsapproval.Continuation
	Decision                intentapproval.ApprovalDecision
	RecordedAt              time.Time
	Meta                    workitem.TransitionMeta
	// Recheck re-evaluates the decider's current authority against the
	// WorkItem as the vote transaction loaded it. An error refuses the vote
	// with nothing written. A nil Recheck admits the vote on the decision's
	// own authority reference.
	Recheck func(context.Context, workitem.Executor, workitem.WorkItem) error
	// Prepare and Record are the kernel's same-transaction hooks: move the
	// open item to IN_PROGRESS for the decider, and append the caller's own
	// evidence after completion.
	Prepare func(context.Context, workitem.Executor, workitem.WorkItem) (workitem.WorkItem, error)
	Record  func(context.Context, workitem.Executor, workitem.WorkItem) error
}

// ApprovalVoteResult is the committed vote and, when it resolved the node,
// the advancement it produced.
type ApprovalVoteResult struct {
	Execution ExecutionResult
	Item      workitem.WorkItem
	// Pending reports a durable vote short of quorum: no advancement ran.
	Pending bool
	// Replay reports that the item already recorded this vote.
	Replay bool
}

// ApprovalInvalidationRequest fires one declared invalidator against a
// waiting approval node.
type ApprovalInvalidationRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	Continuation            stepsapproval.Continuation
	Requirements            humanwork.RequirementSet
	Change                  humanwork.InvalidatorKind
	Reason                  string
	EvidenceRef             string
	RecordedAt              time.Time
	Meta                    workitem.TransitionMeta
}

// ApprovalKernel is the [ProposalExecutor] extension that decides approvals
// atomically. A cell whose executor does not provide it cannot decide an
// approval: there is no non-atomic fallback.
type ApprovalKernel interface {
	CompleteApproval(ctx context.Context, req ApprovalVoteRequest) (ApprovalVoteResult, error)
	InvalidateApproval(ctx context.Context, req ApprovalInvalidationRequest) (ExecutionResult, error)
}

// approvalKernel returns the composed executor's approval kernel.
func (e *journeyEngine) approvalKernel() (ApprovalKernel, error) {
	kernel, ok := e.svc.executor.(ApprovalKernel)
	if !ok {
		return nil, fmt.Errorf("%w: this cell's workflow driver provides no approval kernel", ErrProposalDecisionUnavailable)
	}
	return kernel, nil
}

// journeyContinuation rebuilds, from the durable item alone, the compiled
// requirement it was routed under and the continuation its node parked on.
func journeyContinuation(item workitem.WorkItem, revision intent.ProposalRevision) (stepsapproval.Continuation, humanwork.RequirementSet, error) {
	requirement, err := routedApprovalRequirement(item)
	if err != nil {
		return stepsapproval.Continuation{}, humanwork.RequirementSet{}, fmt.Errorf("app: journey: rebuild the approval requirement: %w", err)
	}
	set := humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{requirement}}
	continuation, err := stepsapproval.NewContinuation(item.WorkflowInstanceID, item.NodeID, revision, set, []workitem.WorkItem{item})
	if err != nil {
		return stepsapproval.Continuation{}, humanwork.RequirementSet{}, fmt.Errorf("app: journey: rebuild the approval continuation: %w", err)
	}
	return continuation, set, nil
}

// journeyVote is one decision the journey hands the kernel.
type journeyVote struct {
	principal *trust.Principal
	inst      intent.Instance
	start     runtime.StartRequest
	instance  runtime.Instance
	item      workitem.WorkItem
	candidate humanwork.Candidate
	decision  intentapproval.ApprovalDecision
	at        time.Time
	// replay is a vote the item already records; the kernel resolves it again
	// without the hooks, which only a fresh vote needs.
	replay bool
}

// voteThroughKernel records v and, when it resolves the node, advances the
// workflow, in the kernel's one transaction.
func (e *journeyEngine) voteThroughKernel(ctx context.Context, v journeyVote) (decidedApproval, error) {
	kernel, err := e.approvalKernel()
	if err != nil {
		return decidedApproval{}, err
	}
	revision := v.start.Proposal.Revision
	continuation, _, err := journeyContinuation(v.item, revision)
	if err != nil {
		return decidedApproval{}, err
	}
	actor := v.decision.Approver.PrincipalID
	req := ApprovalVoteRequest{
		Start: v.start, InstanceID: v.instance.InstanceID, ExpectedInstanceVersion: v.instance.InstanceVersion,
		WorkItemID: v.item.WorkItemID, ExpectedWorkItemVersion: v.item.ItemVersion,
		Continuation: continuation, Decision: v.decision, RecordedAt: v.at,
		Meta: workitem.TransitionMeta{ActorPrincipalID: actor, Reason: journeyReasonDecided, At: v.at},
	}
	if !v.replay {
		req.Recheck = func(ctx context.Context, ex workitem.Executor, current workitem.WorkItem) error {
			candidate, ok := current.Assignment.Resolution.Authorizes(actor)
			if !ok {
				return ErrProposalDecisionRoute
			}
			if err := e.validateRoutedJourneyApprover(ctx, ex, v.principal, v.inst, current, candidate, revision, v.at); err != nil {
				return err
			}
			stale, err := e.recheckApprovalAuthority(ctx, ex, v.principal, v.inst, current, candidate, revision, v.at)
			if err != nil {
				return err
			}
			if stale != nil {
				// Authority changed between the read and the vote: refuse; the
				// next attempt takes the INVALIDATED route from its own read.
				return fmt.Errorf("%w: %v", ErrProposalDecisionInvalidated, stale)
			}
			return nil
		}
		req.Prepare = func(ctx context.Context, ex workitem.Executor, current workitem.WorkItem) (workitem.WorkItem, error) {
			store := workitem.Store{}
			claimed, err := store.ClaimCurrent(ctx, ex, workitem.ClaimCurrentInput{
				TenantID: current.TenantID, WorkItemID: current.WorkItemID, ExpectedVersion: current.ItemVersion,
				ClaimantPrincipalID: actor, ClaimExpiresAt: v.at.Add(journeyClaimWindow), Now: v.at,
				Meta: workitem.TransitionMeta{ActorPrincipalID: actor, Reason: journeyReasonClaimed, At: v.at},
			})
			if err != nil {
				return workitem.WorkItem{}, proposalWorkItemError(err)
			}
			started, err := store.Start(ctx, ex, current.TenantID, current.WorkItemID, claimed.ItemVersion, v.at,
				workitem.TransitionMeta{ActorPrincipalID: actor, Reason: journeyReasonStarted, At: v.at})
			if err != nil {
				return workitem.WorkItem{}, proposalWorkItemError(err)
			}
			return started, nil
		}
		req.Record = func(ctx context.Context, ex workitem.Executor, _ workitem.WorkItem) error {
			tx, ok := ex.(dbport.Tx)
			if !ok {
				return fmt.Errorf("app: approval decision requires the caller's database transaction")
			}
			return e.recordApprovalDecision(ctx, tx, v.principal, v.inst, revision, v.decision, v.at)
		}
	}
	voted, err := kernel.CompleteApproval(ctx, req)
	if err != nil {
		return decidedApproval{}, approvalKernelError(err)
	}
	return decidedApproval{
		item: voted.Item, instance: v.instance, decision: v.decision, replayed: v.replay || voted.Replay,
		settled: true, executed: !voted.Pending, execution: voted.Execution,
	}, nil
}

// invalidateThroughKernel closes the routed approval whose authority is no
// longer current: the kernel cancels the item, withdraws any pending vote and
// routes the node INVALIDATED in one transaction. The caller reports the
// refusal after consuming the execution result.
func (e *journeyEngine) invalidateThroughKernel(
	ctx context.Context, start runtime.StartRequest, instance runtime.Instance, item workitem.WorkItem,
	actor string, at time.Time, detail string,
) (decidedApproval, error) {
	kernel, err := e.approvalKernel()
	if err != nil {
		return decidedApproval{}, err
	}
	continuation, set, err := journeyContinuation(item, start.Proposal.Revision)
	if err != nil {
		return decidedApproval{}, err
	}
	evidence := "approval-closure:" + string(approvalClosureInvalidated) + ":" + item.WorkItemID.String()
	result, err := kernel.InvalidateApproval(ctx, ApprovalInvalidationRequest{
		Start: start, InstanceID: instance.InstanceID, ExpectedInstanceVersion: instance.InstanceVersion,
		Continuation: continuation, Requirements: set, Change: humanwork.InvalidatorAuthorityRevoked,
		Reason: detail, EvidenceRef: evidence, RecordedAt: at,
		Meta: workitem.TransitionMeta{
			ActorPrincipalID: actor, Reason: closureReason(approvalClosureInvalidated), Detail: detail,
			EvidenceRef: evidence, At: at,
		},
	})
	if err != nil {
		return decidedApproval{}, approvalKernelError(err)
	}
	return decidedApproval{
		item: item, instance: instance, settled: true, executed: true, execution: result,
		refusal: closureRefusal(approvalClosureInvalidated, detail),
	}, nil
}

// approvalKernelError keeps the journey's own refusals and projects the
// kernel's onto the decision vocabulary.
func approvalKernelError(err error) error {
	for _, owned := range []error{
		ErrProposalDecisionInvalidated, ErrProposalDecisionRoute, ErrProposalDecisionSeparation,
		ErrProposalDecisionConflict, ErrProposalDecisionStale, ErrProposalDecisionExpired,
		ErrProposalDecisionStage, ErrProposalDecisionUnavailable, ErrPromotionAuthorityStale, workspace.ErrJourneyStage,
	} {
		if errors.Is(err, owned) {
			return err
		}
	}
	switch {
	case errors.Is(err, stepsapproval.ErrSeparationConflict), errors.Is(err, stepsapproval.ErrDuplicateApprover):
		return fmt.Errorf("%w: %w", ErrProposalDecisionSeparation, err)
	case workitem.CodeOf(err) != "":
		return proposalWorkItemError(err)
	default:
		return executionError(err)
	}
}
