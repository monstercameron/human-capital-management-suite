package approval

import (
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

// Resolve evaluates immutable WorkItem and ApprovalDecision evidence against a
// continuation. It performs no writes and reads no ambient time.
func Resolve(c Continuation, items []workitem.WorkItem, decisions []intentapproval.ApprovalDecision, now values.Instant, event Event) (Resolution, error) {
	if c.Digest == "" || computeContinuationDigest(c) != c.Digest {
		return Resolution{}, ErrInvalidContinuation
	}
	if event.Prior != nil {
		if event.Prior.ContinuationDigest != c.Digest || event.Prior.Digest == "" || computeResolutionDigest(*event.Prior) != event.Prior.Digest {
			return Resolution{}, fmt.Errorf("%w: prior resolution", ErrBindingMismatch)
		}
		return *event.Prior, nil
	}
	if !now.IsSet() {
		return Resolution{}, fmt.Errorf("%w: current instant is required", ErrInvalidEvent)
	}
	if event.Kind == "" {
		event.Kind = EventDecisionsChanged
	}
	if event.Kind != EventDecisionsChanged && event.Kind != EventInvalidated && event.Kind != EventCancelled {
		return Resolution{}, fmt.Errorf("%w: %q", ErrInvalidEvent, event.Kind)
	}
	if (event.Kind == EventInvalidated || event.Kind == EventCancelled) && event.Reason == "" {
		return Resolution{}, fmt.Errorf("%w: %s requires a reason", ErrInvalidEvent, event.Kind)
	}

	byID := make(map[uuid.UUID]workitem.WorkItem, len(items))
	for _, item := range items {
		if _, exists := byID[item.WorkItemID]; exists {
			return Resolution{}, fmt.Errorf("%w: duplicate work item %s", ErrInvalidEvidence, item.WorkItemID)
		}
		byID[item.WorkItemID] = item
	}
	decisionByDigest := make(map[string]intentapproval.ApprovalDecision, len(decisions))
	for _, decision := range decisions {
		d := decision.Digest()
		if d == "" {
			return Resolution{}, fmt.Errorf("%w: decision %q has no digest", ErrInvalidEvidence, decision.DecisionID)
		}
		if _, exists := decisionByDigest[d]; exists {
			return Resolution{}, fmt.Errorf("%w: duplicate decision digest %s", ErrInvalidEvidence, d)
		}
		decisionByDigest[d] = decision
	}

	resolved := Resolution{ContinuationDigest: c.Digest, ResolvedAt: now, Reason: event.Reason}
	if event.Kind == EventInvalidated {
		resolved.Outcome = workflow.Outcome("INVALIDATED")
		return finish(resolved), nil
	}
	if event.Kind == EventCancelled {
		resolved.Outcome = workflow.Outcome("CANCELLED")
		return finish(resolved), nil
	}

	allApproved := true
	usedDecision := make(map[string]bool, len(decisions))
	for _, req := range c.Requirements {
		if now.After(req.Expiry) {
			resolved.Outcome = workflow.Outcome("EXPIRED")
			resolved.Reason = "approval decision expired"
			applyDeadlineDefault(&resolved, c)
			return finish(resolved), nil
		}
		approvedPrincipals := map[string]bool{}
		approvedCount := uint32(0)
		for _, id := range req.WorkItemIDs {
			item, ok := byID[id]
			if !ok {
				return Resolution{}, fmt.Errorf("%w: missing work item %s", ErrInvalidEvidence, id)
			}
			if err := checkItemBinding(c, req, item); err != nil {
				return Resolution{}, err
			}
			resolved.WorkItemRefs = append(resolved.WorkItemRefs, id.String())
			switch item.Status {
			case workitem.StatusCancelled:
				resolved.Outcome = workflow.Outcome("CANCELLED")
				resolved.Reason = "approval work item cancelled"
				return finish(resolved), nil
			case workitem.StatusExpired:
				resolved.Outcome = workflow.Outcome("EXPIRED")
				resolved.Reason = "approval work item expired"
				applyDeadlineDefault(&resolved, c)
				return finish(resolved), nil
			case workitem.StatusCompleted:
				decision, ok := decisionByDigest[item.CompletedOutputDigest]
				if !ok {
					return Resolution{}, fmt.Errorf("%w: completed work item %s has no matching decision", ErrInvalidEvidence, id)
				}
				if err := checkDecisionBinding(c, req, item, decision); err != nil {
					return Resolution{}, err
				}
				usedDecision[item.CompletedOutputDigest] = true
				resolved.DecisionRefs = append(resolved.DecisionRefs, decision.DecisionID)
				if decision.Outcome == intentapproval.OutcomeRejected {
					resolved.Outcome = workflow.OutcomeRejected
					resolved.Reason = decision.Reason
					return finish(resolved), nil
				}
				if req.Distinct {
					if approvedPrincipals[decision.Approver.PrincipalID] {
						continue
					}
					approvedPrincipals[decision.Approver.PrincipalID] = true
				}
				approvedCount++
			}
		}
		if approvedCount < req.Quorum {
			allApproved = false
			if now.After(req.DecideBy) {
				resolved.Outcome = workflow.Outcome("EXPIRED")
				resolved.Reason = "approval deadline passed before quorum"
				applyDeadlineDefault(&resolved, c)
				return finish(resolved), nil
			}
		}
	}
	for d := range decisionByDigest {
		if !usedDecision[d] {
			return Resolution{}, fmt.Errorf("%w: decision %s is not recorded by a bound completed work item", ErrInvalidEvidence, d)
		}
	}
	if allApproved {
		resolved.Outcome = workflow.Outcome("APPROVED")
	}
	return finish(resolved), nil
}

func checkItemBinding(c Continuation, req Requirement, item workitem.WorkItem) error {
	if item.Kind != workitem.KindApproval || item.WorkflowInstanceID != c.WorkflowInstanceID || item.NodeID != c.NodeID ||
		item.ApprovalRequirementRef != req.RequirementID || item.ProposalRef != c.ProposalDigest.Digest {
		return fmt.Errorf("%w: work item %s", ErrBindingMismatch, item.WorkItemID)
	}
	res := item.Assignment.Resolution
	if res.RequirementID != req.RequirementID || res.RequirementRevision != req.RequirementRevision ||
		res.RequirementDigest != req.RequirementDigest || res.QuorumRequired != req.Quorum {
		return fmt.Errorf("%w: work item %s assignment", ErrBindingMismatch, item.WorkItemID)
	}
	return nil
}

func checkDecisionBinding(c Continuation, req Requirement, item workitem.WorkItem, decision intentapproval.ApprovalDecision) error {
	b := decision.Binding
	if b.RequirementID != req.RequirementID || b.RequirementRevision != req.RequirementRevision ||
		b.RequirementDigest != req.RequirementDigest || b.ProposalRevisionID != c.ProposalRevisionID ||
		!sameReference(b.ProposalDigest, c.ProposalDigest) || decision.Digest() != item.CompletedOutputDigest ||
		decision.Approver.PrincipalID != item.CompletedBy {
		return fmt.Errorf("%w: decision %q", ErrBindingMismatch, decision.DecisionID)
	}
	candidate, ok := item.Assignment.Resolution.Authorizes(decision.Approver.PrincipalID)
	if !ok || candidate.Via != decision.Approver.Via || candidate.DelegationID != decision.Approver.DelegationID {
		return fmt.Errorf("%w: decision %q approver is not the recorded candidate", ErrBindingMismatch, decision.DecisionID)
	}
	if !decision.DecidedAt.IsSet() || decision.DecidedAt.After(req.DecideBy) || !decision.Outcome.Valid() {
		return fmt.Errorf("%w: decision %q was not validly made before the deadline", ErrInvalidEvidence, decision.DecisionID)
	}
	return nil
}

// applyDeadlineDefault records the continuation's declared non-response
// payload digest on an EXPIRED resolution when one is declared.
func applyDeadlineDefault(r *Resolution, c Continuation) {
	if c.DeadlineDefault.Schema != "" {
		r.DefaultOutputDigest = c.DeadlineDefault.Digest()
	}
}

func finish(r Resolution) Resolution {
	sort.Strings(r.DecisionRefs)
	sort.Strings(r.WorkItemRefs)
	r.Digest = computeResolutionDigest(r)
	return r
}

// ToNodeOutcome maps a resolution onto the only shapes frontier.Advance accepts
// for an APPROVAL node.
func (r Resolution) ToNodeOutcome(nodeID string) frontier.NodeOutcome {
	if r.Outcome == "" {
		return frontier.NodeOutcome{NodeID: nodeID, Await: frontier.AwaitWorkItem, AwaitRef: r.ContinuationDigest}
	}
	switch r.Outcome {
	case workflow.Outcome("APPROVED"), workflow.OutcomeRejected, workflow.Outcome("INVALIDATED"), workflow.Outcome("EXPIRED"), workflow.Outcome("CANCELLED"):
		if r.Outcome == workflow.Outcome("EXPIRED") && r.DefaultOutputDigest != "" {
			return frontier.NodeOutcome{NodeID: nodeID, Outcome: r.Outcome, OutputDigest: r.DefaultOutputDigest}
		}
		return frontier.NodeOutcome{NodeID: nodeID, Outcome: r.Outcome, OutputDigest: r.Digest}
	default:
		return frontier.NodeOutcome{NodeID: nodeID, Failed: true, ErrorClass: "UNKNOWN_APPROVAL_OUTCOME"}
	}
}
