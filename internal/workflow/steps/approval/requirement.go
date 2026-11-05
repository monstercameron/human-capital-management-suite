package approval

import (
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ResolveRequirementDeadlines returns the set with every requirement
// deadline expression resolved against the evaluated anchor and pinned
// calendar dataset. Requirements without expressions pass through with their
// declared absolute deadlines; set order is preserved. NewContinuation stays
// absolute-only: resolve first, then bind.
func ResolveRequirementDeadlines(set humanwork.RequirementSet, anchor values.Instant, cals values.CalendarDataset, dataset values.DatasetVersions) (humanwork.RequirementSet, error) {
	out := set
	out.Requirements = append([]humanwork.ApprovalRequirement(nil), set.Requirements...)
	for i, req := range out.Requirements {
		if req.DeadlineExpr == nil {
			continue
		}
		dl, err := req.ResolveDeadline(anchor, cals, dataset)
		if err != nil {
			return humanwork.RequirementSet{}, fmt.Errorf("approval: requirement %q: %w", req.RequirementID, err)
		}
		out.Requirements[i].Deadline = dl
	}
	return out, nil
}

// NewContinuation binds the compiled approval requirements to the exact
// proposal and WorkItems that may satisfy the parked workflow node.
func NewContinuation(
	instanceID uuid.UUID,
	nodeID string,
	proposal intent.ProposalRevision,
	set humanwork.RequirementSet,
	items []workitem.WorkItem,
) (Continuation, error) {
	if instanceID == uuid.Nil || nodeID == "" || proposal.ProposalRevisionID == "" || proposal.MaterialDigest.Digest == "" {
		return Continuation{}, ErrInvalidContinuation
	}
	if len(set.Requirements) == 0 {
		return Continuation{}, fmt.Errorf("%w: no approval requirements", ErrInvalidContinuation)
	}

	byRequirement := make(map[string][]uuid.UUID, len(set.Requirements))
	seenItems := make(map[uuid.UUID]bool, len(items))
	for _, item := range items {
		if seenItems[item.WorkItemID] {
			return Continuation{}, fmt.Errorf("%w: duplicate work item %s", ErrInvalidContinuation, item.WorkItemID)
		}
		seenItems[item.WorkItemID] = true
		if item.Kind != workitem.KindApproval || item.WorkflowInstanceID != instanceID || item.NodeID != nodeID {
			return Continuation{}, fmt.Errorf("%w: work item %s belongs to another approval node", ErrBindingMismatch, item.WorkItemID)
		}
		if item.ProposalRef != proposal.MaterialDigest.Digest {
			return Continuation{}, fmt.Errorf("%w: work item %s names another proposal", ErrBindingMismatch, item.WorkItemID)
		}
		if _, ok := set.Find(item.ApprovalRequirementRef); !ok {
			return Continuation{}, fmt.Errorf("%w: work item %s names unknown requirement %q", ErrBindingMismatch, item.WorkItemID, item.ApprovalRequirementRef)
		}
		byRequirement[item.ApprovalRequirementRef] = append(byRequirement[item.ApprovalRequirementRef], item.WorkItemID)
	}

	requirements := append([]humanwork.ApprovalRequirement(nil), set.Requirements...)
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].RequirementID < requirements[j].RequirementID })
	out := Continuation{
		WorkflowInstanceID: instanceID,
		NodeID:             nodeID,
		ProposalRevisionID: proposal.ProposalRevisionID,
		ProposalDigest:     proposal.MaterialDigest,
		Requirements:       make([]Requirement, 0, len(requirements)),
	}
	for _, req := range requirements {
		if req.RequirementID == "" || req.Revision == 0 || req.Digest() == "" || req.Quorum.MinApprovals == 0 ||
			!req.Deadline.DecideBy.IsSet() || !req.Deadline.Expiry.IsSet() || req.Deadline.Expiry.Before(req.Deadline.DecideBy) {
			return Continuation{}, fmt.Errorf("%w: malformed requirement %q", ErrInvalidContinuation, req.RequirementID)
		}
		ids := append([]uuid.UUID(nil), byRequirement[req.RequirementID]...)
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
		if uint32(len(ids)) < req.Quorum.MinApprovals {
			return Continuation{}, fmt.Errorf("%w: requirement %q has quorum %d but only %d work items", ErrInvalidContinuation, req.RequirementID, req.Quorum.MinApprovals, len(ids))
		}
		out.Requirements = append(out.Requirements, Requirement{
			RequirementID:       req.RequirementID,
			RequirementRevision: req.Revision,
			RequirementDigest:   req.Digest(),
			Quorum:              req.Quorum.MinApprovals,
			Distinct:            req.Quorum.RequireDistinctPrincipals,
			DecideBy:            req.Deadline.DecideBy,
			Expiry:              req.Deadline.Expiry,
			WorkItemIDs:         ids,
		})
	}
	out.Digest = computeContinuationDigest(out)
	if out.Digest == "" {
		return Continuation{}, ErrInvalidContinuation
	}
	return out, nil
}
