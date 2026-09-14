package approval

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// SeparationAuthority implements [workitem.AuthorityRecheckPort]. It is
// PROMOUX-003's answer to the boundary EP-WORK-003 (WORK-006) left open:
// [workitem.Store.CompleteWithAuthorityRecheck] and
// [workitem.Store.DecideApproval] recheck current authority through a
// caller-supplied [workitem.AuthorityRecheckPort] before their CAS, but no
// concrete implementation of that port existed anywhere in this module --
// the recheck was wired and tested against a fake that simply returned
// whatever a test told it to. SeparationAuthority is that missing driver
// for the one question this package owns: does completing this WorkItem, as
// the decision's own completer, still respect separation of duties against
// every sibling approval requirement on the same proposal.
//
// It answers that question with exactly the guarantee [Complete] itself
// enforces -- [workitem.Store.LockApprovalSiblings] plus
// [workitem.ConflictingCompletion] -- so a caller that completes through
// CompleteWithAuthorityRecheck/DecideApproval and one that completes
// through this package's own [Complete] are held to the identical rule,
// never two independently-maintained separation policies.
//
// SeparationAuthority answers only separation of duties. It is not a
// stand-in for identity, session or delegation validity: a caller composes
// it alongside whatever else its own authority driver needs to check
// (directory membership, revocation, step-up), the same way
// [workitem.CompleteWithAuthorityRecheckInput] already composes a session
// port separately from the authority port.
type SeparationAuthority struct{}

var _ workitem.AuthorityRecheckPort = SeparationAuthority{}

// Recheck implements [workitem.AuthorityRecheckPort].
func (SeparationAuthority) Recheck(
	ctx context.Context, ex workitem.Executor, req workitem.AuthorityRecheckRequest,
) (ret0 workitem.AuthorityRecheckDecision, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.steps.approval.authority_recheck", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	item := req.Item
	if item.Kind != workitem.KindApproval || item.ApprovalRequirementRef == "" {
		return workitem.AuthorityRecheckDecision{}, fmt.Errorf(
			"%w: work item %s is not an approval task", ErrBindingMismatch, item.WorkItemID)
	}
	if item.ProposalRef == "" {
		return workitem.AuthorityRecheckDecision{}, fmt.Errorf(
			"%w: work item %s names no proposal to evaluate separation of duties against", ErrBindingMismatch, item.WorkItemID)
	}
	approver := req.Completion.CompletedBy
	if approver == "" {
		// Fail closed: an unnamed completer never matches a sibling's
		// CompletedBy by construction, so this is refused explicitly rather
		// than silently reported as "no conflict found".
		return workitem.AuthorityRecheckDecision{Allowed: false, Reason: "no completing principal named"}, nil
	}

	siblings, err := (workitem.Store{}).LockApprovalSiblings(ctx, ex, req.TenantID, item.ProposalRef)
	if err != nil {
		return workitem.AuthorityRecheckDecision{}, err
	}
	if conflict, found := workitem.ConflictingCompletion(siblings, item.WorkItemID, item.ApprovalRequirementRef, approver); found {
		return workitem.AuthorityRecheckDecision{
			Allowed: false,
			Reason: fmt.Sprintf("%q already completed requirement %q for this proposal and may not also decide %q",
				approver, conflict.ApprovalRequirementRef, item.ApprovalRequirementRef),
		}, nil
	}
	return workitem.AuthorityRecheckDecision{
		Allowed:     true,
		DecisionRef: "authz:separation:" + item.WorkItemID.String(),
		Reason:      "no separation-of-duties conflict on this proposal",
	}, nil
}
