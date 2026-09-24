package task

import (
	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/candidate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// VersionedRef identifies an immutable form or policy artifact.
type VersionedRef struct {
	Ref     string
	Version uint32
}

// CompiledTaskNode is the TASK-specific view of a compiled node. It pins the
// output contract and human-interaction policies a submission must evidence.
type CompiledTaskNode struct {
	WorkflowID      string
	WorkflowVersion uint32
	NodeID          string
	WorkType        string

	OutputSchema        workflow.SchemaRef
	FormDefinition      VersionedRef
	AccessibilityPolicy VersionedRef
	AccommodationPolicy VersionedRef
}

// Continuation is the durable condition for one parked TASK node.
type Continuation struct {
	WorkflowInstanceID uuid.UUID
	Node               CompiledTaskNode
	WorkItemID         uuid.UUID
	Digest             string
}

// SubmissionSpec is the construction input for an immutable Submission. It
// carries a payload digest, never arbitrary payload bytes or mutable answers.
type SubmissionSpec struct {
	WorkflowInstanceID uuid.UUID
	NodeID             string
	WorkItemID         uuid.UUID
	ItemVersion        int64

	CompletedBy  string
	CandidateVia candidate.Source
	DelegationID string

	ClaimID        uuid.UUID
	ClaimExpiresAt values.Instant
	SubmittedAt    values.Instant

	OutputSchema             workflow.SchemaRef
	CanonicalPayloadDigest   string
	FormDefinition           VersionedRef
	RenderContextDigest      string
	ValidationEvidenceRef    string
	AccessibilityEvidenceRef string
	AccommodationEvidenceRef string
}

// Submission is the immutable, typed completion artifact for a TASK. Construct
// it with NewSubmission; Digest verifies that none of its evidence changed.
type Submission struct {
	WorkflowInstanceID uuid.UUID
	NodeID             string
	WorkItemID         uuid.UUID
	ItemVersion        int64

	CompletedBy  string
	CandidateVia candidate.Source
	DelegationID string

	ClaimID        uuid.UUID
	ClaimExpiresAt values.Instant
	SubmittedAt    values.Instant

	OutputSchema             workflow.SchemaRef
	CanonicalPayloadDigest   string
	FormDefinition           VersionedRef
	RenderContextDigest      string
	ValidationEvidenceRef    string
	AccessibilityEvidenceRef string
	AccommodationEvidenceRef string

	digest string
}

// Digest returns the content identity minted by NewSubmission.
func (s Submission) Digest() string { return s.digest }

// ValidationRequest is the exact typed context supplied to a Validator.
type ValidationRequest struct {
	Node       CompiledTaskNode
	Submission Submission
}

// Validator owns schema/form validation. TASK coordination only requires a
// deterministic verdict and does not implement those semantics itself.
type Validator interface {
	Validate(ValidationRequest) error
}

// ValidatorFunc adapts a function to Validator.
type ValidatorFunc func(ValidationRequest) error

func (f ValidatorFunc) Validate(req ValidationRequest) error { return f(req) }

// Outcome is the TASK resolver's closed lifecycle vocabulary. RETURNED maps
// to the workflow StepTask REJECTED route because RETURNED is a Human Work
// lifecycle result, not a separate compiled route today.
type Outcome string

const (
	OutcomeSucceeded Outcome = "SUCCEEDED"
	OutcomeReturned  Outcome = "RETURNED"
	OutcomeExpired   Outcome = "EXPIRED"
	OutcomeCancelled Outcome = "CANCELLED"
)

// Event supplies a prior resolution for semantic replay.
type Event struct {
	Prior *Resolution
}

// Resolution is an immutable TASK resume verdict. Empty Outcome means the
// WorkItem is still active and the node remains parked.
type Resolution struct {
	ContinuationDigest string
	Outcome            Outcome
	WorkItemRef        string
	SubmissionDigest   string
	ResolvedAt         values.Instant
	Reason             string
	Digest             string
}
