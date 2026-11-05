package approval

import (
	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Requirement is the immutable subset of an ApprovalRequirement the runtime
// needs while a node is parked. WorkItemIDs are the exact human-work records
// allowed to satisfy it.
type Requirement struct {
	RequirementID       string
	RequirementRevision uint64
	RequirementDigest   string
	Quorum              uint32
	Distinct            bool
	DecideBy            values.Instant
	Expiry              values.Instant
	WorkItemIDs         []uuid.UUID
}

// Continuation is the durable, proposal-bound condition an APPROVAL node is
// waiting to satisfy. Digest is minted by NewContinuation over every field.
type Continuation struct {
	WorkflowInstanceID uuid.UUID
	NodeID             string
	ProposalRevisionID string
	ProposalDigest     digest.Reference
	Requirements       []Requirement
	// DeadlineDefault, when its schema is set, is the declared non-response
	// payload EXPIRED resolutions complete with. Use WithDeadlineDefault so
	// the digest stays minted over it.
	DeadlineDefault values.DeadlineDefault
	Digest          string
}

// WithDeadlineDefault returns a copy carrying the declared non-response
// payload with a freshly minted digest.
func (c Continuation) WithDeadlineDefault(d values.DeadlineDefault) Continuation {
	c.DeadlineDefault = d
	c.Digest = computeContinuationDigest(c)
	return c
}

// EventKind names an explicit caller-supplied reason to resolve a continuation.
// DecisionsChanged is the normal event after a WorkItem completion.
type EventKind string

const (
	EventDecisionsChanged EventKind = "DECISIONS_CHANGED"
	EventInvalidated      EventKind = "INVALIDATED"
	EventCancelled        EventKind = "CANCELLED"
)

// Event is the external fact Resolve is evaluating. Prior makes replay
// semantic: a duplicate event returns the original resolution verbatim.
type Event struct {
	Kind   EventKind
	Reason string
	Prior  *Resolution
}

// Resolution is the immutable outcome of evaluating a Continuation. An empty
// Outcome means the node still awaits human work.
type Resolution struct {
	ContinuationDigest string
	Outcome            workflow.Outcome
	DecisionRefs       []string
	WorkItemRefs       []string
	ResolvedAt         values.Instant
	Reason             string
	// DefaultOutputDigest, when set, is the declared non-response payload
	// digest an EXPIRED resolution completes with. ToNodeOutcome prefers it
	// over Digest on the EXPIRED route.
	DefaultOutputDigest string
	Digest              string
}
