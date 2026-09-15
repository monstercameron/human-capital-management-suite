package approval

// WF-STEP-018: the pure rules the multi-approver kernel applies before and
// after a vote. [Resolve] already counts a quorum; these functions answer the
// questions a writer has to settle around it: may this principal vote on this
// slot at all, which decisions are durable evidence for the continuation, which
// slots are still open once the node resolves, and which invalidator a material
// change fires.

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrDuplicateApprover reports a second vote by a principal who already
	// completed another slot of the same distinct-approver requirement. The
	// vote is refused rather than recorded: a recorded duplicate would consume
	// a slot a distinct approver needs, and could leave the quorum
	// unreachable.
	ErrDuplicateApprover = errors.New("workflow approval: principal already voted on this distinct-approver requirement")
	// ErrInvalidatorNotDeclared reports a change the continuation's
	// requirements do not declare as an invalidator. Nothing is withdrawn for
	// an undeclared change.
	ErrInvalidatorNotDeclared = errors.New("workflow approval: change is not a declared invalidator of this approval")
)

// requirementFor returns the continuation requirement an item decides.
func requirementFor(c Continuation, requirementID string) (Requirement, bool) {
	for _, req := range c.Requirements {
		if req.RequirementID == requirementID {
			return req, true
		}
	}
	return Requirement{}, false
}

// DuplicateApprover reports the already-completed slot that makes principalID
// a duplicate approver for self: another work item of the same requirement,
// admitted by the continuation, completed by the same principal, where the
// requirement demands distinct principals. A requirement that does not demand
// distinct principals never has a duplicate.
func DuplicateApprover(c Continuation, items []workitem.WorkItem, self workitem.WorkItem, principalID string) (workitem.WorkItem, bool) {
	req, ok := requirementFor(c, self.ApprovalRequirementRef)
	if !ok || !req.Distinct || principalID == "" {
		return workitem.WorkItem{}, false
	}
	slots := make(map[uuid.UUID]bool, len(req.WorkItemIDs))
	for _, id := range req.WorkItemIDs {
		slots[id] = true
	}
	for _, item := range items {
		if item.WorkItemID == self.WorkItemID || !slots[item.WorkItemID] {
			continue
		}
		if item.Status == workitem.StatusCompleted && item.CompletedBy == principalID {
			return item, true
		}
	}
	return workitem.WorkItem{}, false
}

// Slots returns the continuation's work items, in the order items lists them,
// refusing a continuation slot that items does not contain.
func Slots(c Continuation, items []workitem.WorkItem) ([]workitem.WorkItem, error) {
	byID := make(map[uuid.UUID]workitem.WorkItem, len(items))
	for _, item := range items {
		byID[item.WorkItemID] = item
	}
	var out []workitem.WorkItem
	for _, req := range c.Requirements {
		for _, id := range req.WorkItemIDs {
			item, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("%w: missing work item %s", ErrInvalidEvidence, id)
			}
			if item.WorkflowInstanceID != c.WorkflowInstanceID || item.NodeID != c.NodeID || item.ApprovalRequirementRef != req.RequirementID {
				return nil, fmt.Errorf("%w: work item %s", ErrBindingMismatch, id)
			}
			out = append(out, item)
		}
	}
	return out, nil
}

// OpenSlots returns the continuation slots that are neither decided nor
// closed: the work a resolved node no longer needs.
func OpenSlots(c Continuation, items []workitem.WorkItem) ([]workitem.WorkItem, error) {
	slots, err := Slots(c, items)
	if err != nil {
		return nil, err
	}
	open := make([]workitem.WorkItem, 0, len(slots))
	for _, item := range slots {
		if !item.Status.Terminal() {
			open = append(open, item)
		}
	}
	return open, nil
}

// DeclaredInvalidator returns the invalidator the continuation's requirements
// declare for kind. Every continuation requirement must be present in set
// with the exact digest the continuation pinned, so a caller cannot widen the
// invalidators by presenting a different requirement; the first matching
// requirement's declaration (requirements are ordered by id) is returned.
func DeclaredInvalidator(c Continuation, set humanwork.RequirementSet, kind humanwork.InvalidatorKind) (humanwork.Invalidator, error) {
	if !kind.Valid() {
		return humanwork.Invalidator{}, fmt.Errorf("%w: %q is not an invalidator kind", ErrInvalidatorNotDeclared, kind)
	}
	if len(c.Requirements) == 0 {
		return humanwork.Invalidator{}, ErrInvalidContinuation
	}
	var found *humanwork.Invalidator
	for _, pinned := range c.Requirements {
		compiled, ok := set.Find(pinned.RequirementID)
		if !ok || compiled.Digest() != pinned.RequirementDigest {
			return humanwork.Invalidator{}, fmt.Errorf("%w: requirement %q does not match the continuation", ErrBindingMismatch, pinned.RequirementID)
		}
		for _, inv := range compiled.Invalidators {
			if inv.Kind == kind && found == nil {
				declared := inv
				found = &declared
			}
		}
	}
	if found == nil {
		return humanwork.Invalidator{}, fmt.Errorf("%w: %s", ErrInvalidatorNotDeclared, kind)
	}
	return *found, nil
}

// DecisionFromRecord decodes one work_item_decision row [Complete] recorded
// back into the immutable decision, refusing a body whose digest does not
// reproduce the row's recorded digest.
func DecisionFromRecord(rec workitem.DecisionRecord) (intentapproval.ApprovalDecision, error) {
	if rec.Kind != workitem.DecisionKindApproval {
		return intentapproval.ApprovalDecision{}, fmt.Errorf("%w: work item %s records a %s decision", ErrInvalidEvidence, rec.WorkItemID, rec.Kind)
	}
	var wire decisionWire
	if err := json.Unmarshal(rec.Body, &wire); err != nil {
		return intentapproval.ApprovalDecision{}, fmt.Errorf("%w: decode approval decision: %v", ErrInvalidEvidence, err)
	}
	at, err := time.Parse(time.RFC3339Nano, wire.DecidedAt)
	if err != nil {
		return intentapproval.ApprovalDecision{}, fmt.Errorf("%w: decode approval decision instant: %v", ErrInvalidEvidence, err)
	}
	decision := intentapproval.ApprovalDecision{
		DecisionID: wire.DecisionID,
		Binding: intentapproval.DecisionBinding{
			RequirementID: wire.Binding.RequirementID, RequirementRevision: wire.Binding.RequirementRevision,
			IntentID: wire.Binding.IntentID, ProposalRevisionID: wire.Binding.ProposalRevisionID,
			ProposalDigest: wire.Binding.ProposalDigest, TaskVersion: wire.Binding.TaskVersion,
			RenderedProjectionDigest: wire.Binding.RenderedProjectionDigest,
			ControlSnapshots:         wire.Binding.ControlSnapshots, RequirementDigest: wire.Binding.RequirementDigest,
			ResolutionExpressionDigest: wire.Binding.ResolutionExpressionDigest,
		},
		Outcome: intentapproval.Outcome(wire.Outcome),
		Approver: intentapproval.ApproverReference{
			PrincipalID: wire.Approver.PrincipalID, IdentityAssuranceRef: wire.Approver.IdentityAssuranceRef,
			SessionRef: wire.Approver.SessionRef, Via: humanwork.CandidateSource(wire.Approver.Via),
			DelegationID: wire.Approver.DelegationID,
		},
		AuthorityDecisionRef: wire.AuthorityDecisionRef, Reason: wire.Reason,
		DecidedAt: values.NewInstant(at), VoteDigest: wire.VoteDigest,
	}
	if got := decision.Digest(); got == "" || got != rec.BodyDigest {
		return intentapproval.ApprovalDecision{}, fmt.Errorf("%w: work item %s decision body does not reproduce its digest", ErrInvalidEvidence, rec.WorkItemID)
	}
	return decision, nil
}
