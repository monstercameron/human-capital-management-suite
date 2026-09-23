package app

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

// PROMOUX-015: who may decide a promotion approval.
//
// Before this file JourneyEngine.Decide checked the page grant and the
// execution role, then claimed and completed the open WorkItem as the routed
// approver identity on the caller's behalf. Any promotion operator could
// therefore decide every approval of every proposal, including their own. The
// rule below is the one the WorkItem itself states: the caller must be a
// member the item admits, reusing internal/humanwork/workitem's own read rules
// (the same ones UXAUDIT-017's journeyWorkItemSummary discloses), and
// separation of duties is evaluated against the durable record before any
// write.

// authorizeDecision is the cell-level half of a decision's admission: the
// composed execution authority must admit this intent type at all. It is
// deliberately not [IntentService.authorizeExecution]: the execution role gates
// EXECUTE, and a decision's authority is the routed WorkItem membership
// [journeyDecider] checks.
func (s *IntentService) authorizeDecision(def intent.Definition) *envelope.Error {
	if !s.executionAuthority.admitsType(def.Ref.TypeID) {
		return p1aRefusal("DecideIntent", "deciding a promotion approval")
	}
	return nil
}

// journeyDecider admits caller as the decider of the open item, or refuses
// before any write:
//
//   - the journey's initiator may not decide any of its approvals
//     ([ErrProposalDecisionSeparation]);
//   - the caller must be a member the item admits and for whom the item
//     offers a claim -- its ASSIGNEE while ASSIGNED, or an eligible CANDIDATE
//     while AVAILABLE -- exactly workitem.Visible and workitem.PermittedActions
//     ([ErrProposalDecisionRoute]);
//   - a principal who already completed another approval of the same
//     proposal may not decide this one ([ErrProposalDecisionSeparation]).
//
// It returns the recorded candidate that admits the caller, so the decision
// names the same route (direct or delegated) the assignment recorded.
func journeyDecider(
	items []workitem.WorkItem, open workitem.WorkItem, inst intent.Instance, caller string, now time.Time,
) (humanwork.Candidate, error) {
	if caller == "" {
		return humanwork.Candidate{}, ErrProposalDecisionRoute
	}
	if inst.Initiator.PrincipalID == caller {
		return humanwork.Candidate{}, fmt.Errorf("%w: the initiator of a promotion may not decide its approvals", ErrProposalDecisionSeparation)
	}
	membership := workitem.MembershipOf(open, caller, now)
	if !workitem.Visible(open, membership, false, false) ||
		!slices.Contains(workitem.PermittedActions(open, membership), workitem.ActionClaim) {
		return humanwork.Candidate{}, fmt.Errorf("%w: the caller is not a member this approval admits", ErrProposalDecisionRoute)
	}
	for _, sibling := range items {
		if sibling.WorkItemID == open.WorkItemID || sibling.Kind != workitem.KindApproval ||
			sibling.Status != workitem.StatusCompleted || sibling.ProposalRef != open.ProposalRef {
			continue
		}
		if sibling.CompletedBy == caller {
			return humanwork.Candidate{}, fmt.Errorf("%w: %q already decided %q for this proposal",
				ErrProposalDecisionSeparation, caller, sibling.ApprovalRequirementRef)
		}
	}
	candidate, ok := open.Assignment.Resolution.Authorizes(caller)
	if !ok {
		// An assignee is always a recorded candidate on a routed item; one
		// that is not is a routing defect, and refusing is the safe answer.
		return humanwork.Candidate{}, fmt.Errorf("%w: the caller is not a recorded candidate", ErrProposalDecisionRoute)
	}
	return candidate, nil
}

// errRoutedRequirement reports an item whose recorded requirement digest no
// routed candidate reproduces.
var errRoutedRequirement = errors.New("no routed candidate reproduces the recorded requirement digest")

// routedApprovalRequirement recompiles the requirement an approval item was
// routed under. internal/platform/execution compiles the finance, manager and
// prototype requirements over the routed principal, so the candidate whose
// compilation reproduces the assignment's recorded requirement digest is the
// one the routing used; no configured identity is assumed.
func routedApprovalRequirement(item workitem.WorkItem) (humanwork.ApprovalRequirement, error) {
	compile := prototype.CompileApprovalRequirement
	switch item.NodeID {
	case promotionexec.NodeApproveFinance:
		compile = promotionexec.CompileFinanceApprovalRequirement
	case promotionexec.NodeApproveManager:
		compile = promotionexec.CompileManagerApprovalRequirement
	}
	resolution := item.Assignment.Resolution
	for _, candidate := range resolution.Candidates {
		// A delegated candidate decides under the requirement compiled for
		// the delegator it borrows authority from.
		for _, named := range []string{candidate.PrincipalID, candidate.DelegatedFrom} {
			if named == "" {
				continue
			}
			requirement, err := compile(named, item.DeadlineAt)
			if err != nil {
				continue
			}
			if requirement.Digest() == resolution.RequirementDigest {
				return requirement, nil
			}
		}
	}
	return humanwork.ApprovalRequirement{}, fmt.Errorf("%w: work item %s", errRoutedRequirement, item.WorkItemID)
}

// journeyDecisionError projects a decider refusal onto the journey port: a
// caller the approval does not admit, or whose decision would violate
// separation of duties, is denied; every other error travels unchanged.
func journeyDecisionError(err error) error {
	if errors.Is(err, ErrProposalDecisionRoute) || errors.Is(err, ErrProposalDecisionSeparation) || errors.Is(err, ErrPromotionAuthorityStale) {
		return fmt.Errorf("%w: %w", workspace.ErrDenied, err)
	}
	if errors.Is(err, ErrProposalDecisionUnavailable) {
		return fmt.Errorf("%w: %w", workspace.ErrJourneyUnavailable, err)
	}
	return err
}
