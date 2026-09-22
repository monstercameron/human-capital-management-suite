package frontier

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// AwaitKind names what a step handler is waiting for when it returns without
// an outcome. A handler says what it is awaiting; it never creates the work
// item, subscription or timer itself, and it never schedules its own successor.
type AwaitKind string

// The declared awaiting-work markers.
const (
	// AwaitNone is a handler that completed. It is the zero value, so a
	// handler that says nothing about waiting is read as having finished.
	AwaitNone AwaitKind = ""
	// AwaitWorkItem is an APPROVAL or TASK awaiting governed human work.
	AwaitWorkItem AwaitKind = "WORK_ITEM"
	// AwaitSignal is a SIGNAL awaiting a correlated external event.
	AwaitSignal AwaitKind = "SIGNAL"
	// AwaitTimer is a WAIT awaiting a time or calendar condition.
	AwaitTimer AwaitKind = "TIMER"
)

// TerminalResult is the terminal an END handler reports. It is checked
// against the terminal the plan compiled for that node rather than believed:
// lifecycle dimensions are a published property of the plan, and a runtime
// that could restate them at execution time could quietly promote a degraded
// terminal into a clean one.
//
// The zero value means "the handler asserts nothing", and the compiled
// terminal is adopted unchanged.
type TerminalResult struct {
	TerminalCode  string                 `json:"terminal_code,omitempty"`
	RuntimeStatus workflow.RuntimeStatus `json:"runtime_status,omitempty"`
	// Dimensions is the five-dimension tuple the handler observed. The zero
	// tuple means the handler asserts none.
	Dimensions lifecycle.Dimensions `json:"dimensions,omitzero"`
	// Asserted must be set for Dimensions to be checked at all, because the
	// zero tuple is itself a legal set of states.
	Asserted bool `json:"asserted"`
}

// NodeOutcome is the typed result one step handler returns for one node. It
// carries what happened, never what should happen next: deriving successors
// from it is [Advance]'s job, which is what keeps a handler from enqueuing its
// own continuation.
type NodeOutcome struct {
	// NodeID is the node the outcome belongs to. It must be on the frontier.
	NodeID string `json:"node_id"`
	// Outcome is the route key the handler produced — a fixed step-type
	// outcome, or for a DECISION the route key its evaluator selected. It is
	// empty at an END and whenever Await or Failed is set.
	Outcome workflow.Outcome `json:"outcome,omitempty"`
	// OutputDigest is the digest of the node's typed output artifact. It is
	// recorded, never interpreted: this package does not read node outputs.
	OutputDigest string `json:"output_digest,omitempty"`

	// Await, when set, reports that the node has not completed and names what
	// it is suspended on.
	Await AwaitKind `json:"await,omitempty"`
	// AwaitRef is the correlation the runtime needs to satisfy the wait: a
	// requirement ref, an event type or a wake condition. It is opaque here.
	AwaitRef string `json:"await_ref,omitempty"`

	// Failed reports an attempt that produced no outcome at all. It takes the
	// node's declared retry budget and then its declared failure_route; it
	// never takes an outcome route, because a step that did not run produced
	// no outcome to route on.
	Failed bool `json:"failed,omitempty"`
	// ErrorClass classifies the failure for the runtime's own records.
	ErrorClass string `json:"error_class,omitempty"`
	// RetryTerminal reports that the immediate caller's retry policy
	// (WF-RUN-006) already settled this failure terminal: nonretryable,
	// DO_NOT_RETRY, deadline or budget exhaustion. The node's declared retry
	// budget is not applied again, so the failure takes its failure_route at
	// once. It is meaningful only with Failed.
	RetryTerminal bool `json:"retry_terminal,omitempty"`

	// Terminal is the END handler's reported terminal. It is ignored for every
	// other step type.
	Terminal TerminalResult `json:"terminal,omitzero"`

	// Outputs is the node's typed output (WF-EXT-004), or nil when the step
	// runner reports none -- every step runner before WF-EXT-004. It is a
	// pointer, not a bare slice, so NodeOutcome stays comparable with ==:
	// existing callers compare outcomes directly (internal/workflow/execute's
	// signals and retry tests among them), and two nil pointers compare equal
	// exactly the way two absent-Outputs outcomes always have.
	//
	// It is tagged json:"-" and must stay that way: a typed value may carry
	// personal data, and this type's own OutputDigest field is what a
	// receipt, a continuation record or a log is allowed to carry.
	// internal/workflow/execute records Outputs as a durable
	// runtime.NodeOutputArtifact inside the advancement transaction and never
	// lets it reach this package's own digesting or persistence -- this
	// package still reads and writes only OutputDigest, exactly as before.
	Outputs *workflow.OutputDocument `json:"-"`
}

// awaitAdmitted reports whether a step type may raise this awaiting-work
// marker. The mapping is fixed by the step type, so a runtime cannot decide
// that a TASK is really waiting on a timer.
func awaitAdmitted(t workflow.StepType, kind AwaitKind) bool {
	switch kind {
	case AwaitWorkItem:
		return t == workflow.StepApproval || t == workflow.StepTask
	case AwaitSignal:
		return t == workflow.StepSignal
	case AwaitTimer:
		return t == workflow.StepWait
	default:
		return false
	}
}

// intentForAwait maps an awaiting-work marker to the scheduling intent that
// satisfies it.
func intentForAwait(kind AwaitKind) IntentKind {
	switch kind {
	case AwaitWorkItem:
		return IntentWorkItemRequired
	case AwaitSignal:
		return IntentSignalSubscriptionRequired
	case AwaitTimer:
		return IntentTimerRequired
	default:
		return ""
	}
}

// activationFor maps a successor's compiled step type to the state it enters
// and the scheduling intent a runtime must persist for it.
//
// It is one table rather than a chain of conditions at the call sites, because
// "which step types need a work item" is exactly the kind of question that
// drifts when two callers answer it separately.
func activationFor(t workflow.StepType) (NodeState, IntentKind) {
	switch t {
	case workflow.StepApproval, workflow.StepTask:
		return NodeWaiting, IntentWorkItemRequired
	case workflow.StepSignal:
		return NodeWaiting, IntentSignalSubscriptionRequired
	case workflow.StepWait:
		return NodeWaiting, IntentTimerRequired
	default:
		// CAPABILITY, DECISION, TRANSFORM, OBSERVE, COMPENSATE, PARALLEL,
		// SUBWORKFLOW and END are ready work: a worker claims them and runs
		// them. An END is ready work too — it mints the terminal artifact, and
		// only its completion produces IntentComplete.
		return NodeReady, IntentReady
	}
}
