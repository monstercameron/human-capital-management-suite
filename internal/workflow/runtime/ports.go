package runtime

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// WorkflowSelection is what a [WorkflowResolver] hands back: which workflow,
// which exact published version pin, and the compiled plan object that pin
// names.
//
// Plan is carried directly rather than decoded from
// [version.CompiledVersion.CanonicalPlanBytes] on every call: a
// [workflow.CompiledWorkflow]'s content digest lives in an unexported field
// [workflow.Compile] stamps, encoding/json does not round-trip unexported
// fields, and this package has no way to re-derive that digest without the
// capability registry the compiler closed over. A real resolver is expected
// to hold or cache the exact plan object its own publication pipeline
// compiled, the same way [Start] cross-checks it: Plan.Digest() must equal
// the [version.CompiledVersion.CompiledPlanDigest] [version.Resolve] returns
// for Pin, or the start is refused ([CodeVersionPlanMismatch]) rather than
// trusted.
type WorkflowSelection struct {
	WorkflowID string
	Pin        version.Pin
	Plan       *workflow.CompiledWorkflow
	// RevalidationKeys are the immutable keys declared by this registration.
	// The execution driver loads their pinned/current evidence before each
	// advancement and the runtime evaluates it using EvaluateRevalidation.
	RevalidationKeys []string
}

// WorkflowResolver looks up which workflow_id and pin a [StartRequest] binds,
// from policy or configuration data the caller owns.
//
// This is WF-RUN-023's REFACTOR clause made structural: the Promotion service
// (or any other caller) decides which compiled graph a proposal starts under,
// and this package only resolves that decision to an immutable
// [version.CompiledVersion] and checks it is fit to start. There is no
// default resolver and no fallback to "the workflow the proposal's
// definition names" — a caller that wants that behavior implements it.
type WorkflowResolver interface {
	ResolveWorkflow(ctx context.Context, req StartRequest) (WorkflowSelection, error)
}

// ContinuationRecord is one scheduling record [Advance] derived from
// [frontier.Transition.Intents] and asks a [ContinuationSink] to persist.
//
// SourceNodeID/SourceAttempt name the node execution whose completion
// produced this record; TargetNodeID names the node the record is about
// (a successor for every kind but Complete, where it is the completed node
// itself). Both are carried because a runtime working the WORK_ITEM_REQUIRED
// queue needs to know which requirement to satisfy without re-deriving it
// from the node execution history.
type ContinuationRecord struct {
	TenantID      uuid.UUID
	InstanceID    uuid.UUID
	SourceNodeID  string
	SourceAttempt int

	TargetNodeID string
	Kind         frontier.IntentKind
	RouteKey     string
	Ref          string
	TerminalCode string

	// TargetAttempt is the activation of TargetNodeID this continuation
	// belongs to, as [Advance] recorded it: 1 on first entry, higher when the
	// plan routed back to the node. It is a dispatch-time fact the sink reads
	// (a WAIT re-entered on attempt 2 needs its own timer promise), not a
	// stored column; a zero value means the first activation.
	TargetAttempt int

	RecordedAt time.Time
	// Causal is optional diagnostic/business correlation metadata. Nil keeps
	// legacy continuation rows byte-for-byte compatible.
	Causal *CausalMetadata
}

type CausalMetadata struct {
	CorrelationID      string
	CausationID        string
	LogicalOperationID string
	AttemptID          string
	TraceLink          *TraceLinkMetadata
}

type TraceLinkMetadata struct {
	TraceID    string
	SpanID     string
	TraceFlags byte
	TraceState string
	ExpiresAt  time.Time
}

// ContinuationSink persists the continuation records one [Advance] call
// derives, one method per [frontier.IntentKind]. The method name is the
// dispatch: a caller cannot decide that an APPROVAL's WORK_ITEM_REQUIRED
// intent is really ready-work, because [Advance] calls RequireWorkItem for it
// and nothing else.
//
// Every method is called inside the same transaction [Advance] is fencing:
// an implementation that fails leaves the whole advancement uncommitted
// (WF-RUN-025's fault clause), and an implementation that creates real
// governed state (a WorkItem row, a signal subscription) does so as part of
// that same atomic unit of work. This package supplies no default: the
// durable reference implementation in continuation.go persists this
// package's own audit-only ledger, [ContinuationStore], and
// [MemorySink] is the in-process double the test matrix uses; a caller that
// wants a WorkItem actually created composes its own sink around one of
// these or writes its own.
type ContinuationSink interface {
	RequireWorkItem(ctx context.Context, ex Executor, rec ContinuationRecord) error
	RequireSignalSubscription(ctx context.Context, ex Executor, rec ContinuationRecord) error
	RequireTimer(ctx context.Context, ex Executor, rec ContinuationRecord) error
	MarkReady(ctx context.Context, ex Executor, rec ContinuationRecord) error
	Complete(ctx context.Context, ex Executor, rec ContinuationRecord) error
}

// dispatchContinuation calls the one ContinuationSink method a
// [frontier.IntentKind] maps to. It is the single place that mapping is
// stated, so [Advance] cannot drift into calling the wrong method for a kind.
func dispatchContinuation(ctx context.Context, ex Executor, sink ContinuationSink, rec ContinuationRecord) error {
	switch rec.Kind {
	case frontier.IntentReady:
		return sink.MarkReady(ctx, ex, rec)
	case frontier.IntentWorkItemRequired:
		return sink.RequireWorkItem(ctx, ex, rec)
	case frontier.IntentSignalSubscriptionRequired:
		return sink.RequireSignalSubscription(ctx, ex, rec)
	case frontier.IntentTimerRequired:
		return sink.RequireTimer(ctx, ex, rec)
	case frontier.IntentComplete:
		return sink.Complete(ctx, ex, rec)
	default:
		return refuse(CodeInvalidRecord, rec.InstanceID.String(), rec.TargetNodeID,
			"continuation record carries undeclared intent kind %q", rec.Kind)
	}
}

// ProposalFacts resolves whether a bound proposal revision is the current,
// non-superseded revision of its proposal, from the caller-owned proposal
// store -- never from a boolean [StartRequest] or [ProposalBinding] asserts
// (WF-RUN-027).
//
// [Start] calls Supersession for exactly the revision [ProposalBinding.Revision]
// names; a store that has never heard of that revision at all reports the
// zero value (not superseded), the same answer a genuinely current revision
// gets -- a caller that must distinguish "unknown" from "current" composes
// its own port that does.
type ProposalFacts interface {
	Supersession(ctx context.Context, ex Executor, tenantID uuid.UUID, rev intent.ProposalRevision) (ProposalSupersessionFact, error)
}

// ProposalSupersessionFact is what [ProposalFacts] reports about one proposal
// revision.
type ProposalSupersessionFact struct {
	// Superseded is true when the proposal store already holds a later
	// revision of the same proposal.
	Superseded bool
	// SupersededByRevisionID names the revision that superseded it. Present
	// only when Superseded is true.
	SupersededByRevisionID string
	// CurrentRevision is the proposal store's own current revision for this
	// proposal, when Superseded is true. [Start] never reads this field --
	// any supersession refuses a start outright -- but [CurrencyGuard]
	// (WF-RUN-029, internal/workflow/execute) uses it with
	// internal/intent/approval.MaterialResultEqual to tell a pure
	// control-snapshot revalidation (immaterial: the run may continue) from
	// a genuine material change (the run must block).
	CurrentRevision *intent.ProposalRevision
}

// ApprovalFacts resolves the approval decisions recorded against a bound
// proposal revision, from the caller-owned approval store -- never from a
// boolean [StartRequest] or [ProposalBinding] asserts (WF-RUN-027).
type ApprovalFacts interface {
	Decisions(ctx context.Context, ex Executor, tenantID uuid.UUID, rev intent.ProposalRevision) ([]ApprovalDecisionFact, error)
}

// ApprovalOutcome names one recorded approval decision's outcome, spelled
// exactly as internal/intent/approval.Outcome's own constants so a caller
// backing [ApprovalFacts] with that package's decisions needs no translation
// table.
type ApprovalOutcome string

// Declared approval outcomes an [ApprovalDecisionFact] may carry.
const (
	ApprovalOutcomeApproved ApprovalOutcome = "APPROVED"
	ApprovalOutcomeRejected ApprovalOutcome = "REJECTED"
)

// ApprovalDecisionFact is one recorded approval decision [ApprovalFacts]
// reports.
type ApprovalDecisionFact struct {
	DecisionID string
	Outcome    ApprovalOutcome
	// ProposalDigest is the exact material digest the decision is bound to.
	// [Start] and [CurrencyGuard] both refuse [CodeApprovalBindingMismatch]
	// (or its execute-package equivalent) the instant this differs from the
	// started revision's own MaterialDigest.Digest -- a decision the store
	// hands back for this revision id must actually name this revision's own
	// digest, never merely share its id.
	ProposalDigest string
	// Invalidated reports that a later materiality assessment
	// (internal/intent/approval.Assess, INTENT-006) invalidated this
	// decision. An invalidated decision never counts toward approval.
	Invalidated bool
}
