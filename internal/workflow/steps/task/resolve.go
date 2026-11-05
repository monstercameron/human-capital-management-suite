package task

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

// NewContinuation binds one compiled TASK node to its exact durable WorkItem.
func NewContinuation(instanceID uuid.UUID, node CompiledTaskNode, item workitem.WorkItem) (Continuation, error) {
	if err := validateNode(node); err != nil {
		return Continuation{}, err
	}
	if instanceID == uuid.Nil || item.WorkItemID == uuid.Nil || item.Kind != workitem.KindTask ||
		item.WorkflowInstanceID != instanceID || item.NodeID != node.NodeID || item.WorkType != node.WorkType {
		return Continuation{}, ErrBindingMismatch
	}
	c := Continuation{WorkflowInstanceID: instanceID, Node: node, WorkItemID: item.WorkItemID}
	c.Digest = computeContinuationDigest(c)
	if c.Digest == "" {
		return Continuation{}, ErrInvalidContinuation
	}
	return c, nil
}

func validateNode(n CompiledTaskNode) error {
	if n.WorkflowID == "" || n.WorkflowVersion == 0 || n.NodeID == "" || n.WorkType == "" || !n.OutputSchema.Valid() ||
		n.FormDefinition.Ref == "" || n.FormDefinition.Version == 0 ||
		n.AccessibilityPolicy.Ref == "" || n.AccessibilityPolicy.Version == 0 ||
		n.AccommodationPolicy.Ref == "" || n.AccommodationPolicy.Version == 0 {
		return ErrInvalidNode
	}
	if n.Deadline.AnchorPath != "" {
		if err := n.Deadline.Validate(); err != nil {
			return fmt.Errorf("%w: deadline: %v", ErrInvalidNode, err)
		}
	}
	if n.DeadlineDefault.Schema != "" {
		if err := n.DeadlineDefault.Validate(); err != nil {
			return fmt.Errorf("%w: deadline default: %v", ErrInvalidNode, err)
		}
	}
	return nil
}

// ResolveClaimExpiry resolves the claim-expiry instant for a TASK node
// carrying a deadline expression: anchor is the evaluated AnchorPath instant
// for this instance and cals is the pinned calendar dataset. A node with no
// deadline expression is refused; claim expiry without a declared deadline
// is a runtime concern, not a guess made here.
func ResolveClaimExpiry(node CompiledTaskNode, anchor values.Instant, cals values.CalendarDataset, dataset values.DatasetVersions) (values.Instant, error) {
	if node.Deadline.AnchorPath == "" {
		return values.Instant{}, ErrNoDeadline
	}
	res, err := values.ResolveTimeExpr(node.Deadline, anchor, cals, dataset)
	if err != nil {
		return values.Instant{}, err
	}
	return res.FireAt, nil
}

// Resolve converts the current WorkItem state into a deterministic TASK
// resolution. Completed items require an exact immutable Submission and an
// injected validator; other terminal states accept no submission.
func Resolve(c Continuation, item workitem.WorkItem, submission *Submission, validator Validator, now values.Instant, event Event) (Resolution, error) {
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
		return Resolution{}, fmt.Errorf("%w: current instant is required", ErrInvalidContinuation)
	}
	if item.WorkItemID != c.WorkItemID || item.WorkflowInstanceID != c.WorkflowInstanceID || item.NodeID != c.Node.NodeID ||
		item.Kind != workitem.KindTask || item.WorkType != c.Node.WorkType {
		return Resolution{}, ErrBindingMismatch
	}

	r := Resolution{ContinuationDigest: c.Digest, WorkItemRef: item.WorkItemID.String(), ResolvedAt: now}
	switch item.Status {
	case workitem.StatusCompleted:
		if submission == nil || validator == nil {
			return Resolution{}, fmt.Errorf("%w: completed item requires submission and validator", ErrInvalidEvidence)
		}
		if err := checkCompletion(c, item, *submission); err != nil {
			return Resolution{}, err
		}
		if err := validator.Validate(ValidationRequest{Node: c.Node, Submission: *submission}); err != nil {
			return Resolution{}, fmt.Errorf("%w: %v", ErrValidationFailed, err)
		}
		r.Outcome = OutcomeSucceeded
		r.SubmissionDigest = submission.Digest()
	case workitem.StatusReturned:
		if submission != nil {
			return Resolution{}, fmt.Errorf("%w: returned item cannot carry a completion submission", ErrInvalidSubmission)
		}
		r.Outcome, r.Reason = OutcomeReturned, "work item returned"
	case workitem.StatusExpired:
		if submission != nil {
			return Resolution{}, fmt.Errorf("%w: expired item cannot carry a completion submission", ErrInvalidSubmission)
		}
		r.Outcome, r.Reason = OutcomeExpired, "work item expired"
		if c.Node.DeadlineDefault.Schema != "" {
			r.DefaultOutputDigest = c.Node.DeadlineDefault.Digest()
		}
	case workitem.StatusCancelled:
		if submission != nil {
			return Resolution{}, fmt.Errorf("%w: cancelled item cannot carry a completion submission", ErrInvalidSubmission)
		}
		r.Outcome, r.Reason = OutcomeCancelled, "work item cancelled"
	default:
		if submission != nil {
			return Resolution{}, fmt.Errorf("%w: active item cannot carry a completed submission", ErrInvalidSubmission)
		}
	}
	r.Digest = computeResolutionDigest(r)
	return r, nil
}

// ErrInvalidEvidence aliases the submission refusal at the WorkItem boundary.
var ErrInvalidEvidence = ErrInvalidSubmission

func checkCompletion(c Continuation, item workitem.WorkItem, s Submission) error {
	if err := s.Verify(); err != nil {
		return err
	}
	if s.WorkflowInstanceID != c.WorkflowInstanceID || s.NodeID != c.Node.NodeID || s.WorkItemID != c.WorkItemID ||
		s.ItemVersion != item.ItemVersion || s.CompletedBy != item.CompletedBy || s.Digest() != item.CompletedOutputDigest ||
		s.OutputSchema != c.Node.OutputSchema || s.FormDefinition != c.Node.FormDefinition {
		return ErrBindingMismatch
	}
	if item.CompletedAt == nil || !item.CompletedAt.UTC().Equal(s.SubmittedAt.Time()) {
		return fmt.Errorf("%w: completion time does not equal submission time", ErrBindingMismatch)
	}
	if item.ClaimID != nil || item.ClaimedBy != "" || item.ClaimedAt != nil || item.ClaimExpiresAt != nil {
		return fmt.Errorf("%w: completed work item retained a live claim", ErrBindingMismatch)
	}
	if !item.DeadlineAt.IsZero() && s.SubmittedAt.Time().After(item.DeadlineAt.UTC()) {
		return fmt.Errorf("%w: submission passed the work-item deadline", ErrClaimExpired)
	}
	candidate, ok := item.Assignment.Resolution.Authorizes(s.CompletedBy)
	if !ok || candidate.Via != s.CandidateVia || candidate.DelegationID != s.DelegationID {
		return fmt.Errorf("%w: completer is not the recorded candidate", ErrBindingMismatch)
	}
	return nil
}

// ToNodeOutcome maps the task resolution to the current StepTask route table.
func (r Resolution) ToNodeOutcome(nodeID string) frontier.NodeOutcome {
	switch r.Outcome {
	case "":
		return frontier.NodeOutcome{NodeID: nodeID, Await: frontier.AwaitWorkItem, AwaitRef: r.ContinuationDigest}
	case OutcomeSucceeded:
		return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.OutcomeSucceeded, OutputDigest: r.Digest}
	case OutcomeReturned:
		return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.OutcomeRejected, OutputDigest: r.Digest}
	case OutcomeExpired:
		if r.DefaultOutputDigest != "" {
			return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.Outcome("EXPIRED"), OutputDigest: r.DefaultOutputDigest}
		}
		return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.Outcome("EXPIRED"), OutputDigest: r.Digest}
	case OutcomeCancelled:
		return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.Outcome("CANCELLED"), OutputDigest: r.Digest}
	default:
		return frontier.NodeOutcome{NodeID: nodeID, Failed: true, ErrorClass: "UNKNOWN_TASK_OUTCOME"}
	}
}
