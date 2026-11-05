package simulate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// receiptDigestProfile is the canonicalization profile of a simulation
// receipt. A digest is always prefixed with the profile that produced it, so
// bytes canonicalized under one profile can never be mistaken for another
// profile's digest.
const receiptDigestProfile = "hcmnext.workflow.simulate.Receipt/v1"

// InterpreterVersion identifies the interpreter that produced a receipt. It is
// part of the receipt's material identity: the same plan walked by a different
// interpreter is a different artifact.
const InterpreterVersion = "hcmnext.workflow.simulate/v1"

// Evidence is one evidence artifact a node execution recorded: the reference
// name the compiled plan declared (for example "capability_execution_id") and
// the identity minted for it in this run.
//
// The identity is derived from the plan digest, the node id and the reference
// name rather than from a counter or a clock, so two runs of the same plan
// mint the same evidence ids and a receipt digest stays stable.
type Evidence struct {
	Ref string `json:"ref"`
	ID  string `json:"id"`
}

// NodeTrace is one executed node: what ran, in what order, at what virtual
// instant, which outcome it produced, which route that took, the digests of
// what went in and what came out, and the evidence it recorded.
type NodeTrace struct {
	Order       int                    `json:"order"`
	NodeID      string                 `json:"node_id"`
	Type        workflow.StepType      `json:"type"`
	Depth       uint32                 `json:"depth"`
	SafePoint   bool                   `json:"safe_point"`
	EffectClass capability.EffectClass `json:"effect_class"`

	EnteredAt time.Time     `json:"entered_at"`
	ExitedAt  time.Time     `json:"exited_at"`
	Elapsed   time.Duration `json:"elapsed_ns"`

	Outcome workflow.Outcome `json:"outcome"`
	// RouteKey is the edge the outcome selected, empty at a terminal.
	RouteKey string `json:"route_key,omitempty"`
	// NextNodeID is where the route led, empty at a terminal.
	NextNodeID string `json:"next_node_id,omitempty"`

	InputDigest  string `json:"input_digest"`
	OutputDigest string `json:"output_digest"`
	// Detail is a short, deterministic statement of what the node decided. It
	// is evidence for a reader, never parsed.
	Detail string `json:"detail,omitempty"`

	Evidence []Evidence `json:"evidence"`
}

// WorkItemState is what a simulated human work item is, which in SIMULATE mode
// is always the same thing: something that would have been awaited.
type WorkItemState string

// WouldAwait is the only state a simulated work item carries. A simulation
// that blocked on a human would be an execution, and one that recorded an
// approval nobody gave would be a forgery, so the third possibility - stating
// exactly what would have been awaited - is the only honest one.
const WouldAwait WorkItemState = "WOULD_AWAIT"

// WorkItem is one approval or task the plan would have awaited, with the
// requirement it discharges and the candidate approvers resolved at the
// simulation instant.
//
// A candidate is not an approver: internal/humanwork resolves who is
// authorized to decide, and decision-time authority is re-evaluated by
// internal/intent/approval when a real decision lands. Listing candidates here
// grants nobody anything.
type WorkItem struct {
	// NodeID is the node that raised it. For a terminal that declares an
	// outstanding approval requirement it is that terminal's id.
	NodeID        string        `json:"node_id"`
	Kind          string        `json:"kind"`
	State         WorkItemState `json:"state"`
	RequirementID string        `json:"requirement_id"`
	Stage         uint32        `json:"stage"`
	QuorumMin     uint32        `json:"quorum_min"`
	// Outcome is the humanwork resolution outcome: RESOLVED, or
	// NO_AUTHORIZED_APPROVER with the exclusions that emptied the set.
	Outcome string `json:"outcome"`
	// Candidates are the principals authorized to decide, sorted.
	Candidates []string `json:"candidates,omitempty"`
	// Excluded names each principal the resolver removed and the rule that
	// removed them, sorted, so an empty candidate set can explain itself.
	Excluded []string `json:"excluded,omitempty"`
	// FallbackUsed reports whether escalation fallback contributed.
	FallbackUsed bool `json:"fallback_used"`

	ExpressionDigest  string `json:"expression_digest"`
	RequirementDigest string `json:"requirement_digest"`
	// Tier is the approval tier the rules engine produced.
	Tier string `json:"tier"`
	// DecideBy and Expiry are the deadlines the requirement declares.
	DecideBy time.Time `json:"decide_by"`
	Expiry   time.Time `json:"expiry"`
}

// Terminal is the END the walk reached, restated so the receipt does not
// require the plan to be re-read to interpret it.
type Terminal struct {
	NodeID                    string                 `json:"node_id"`
	TerminalCode              string                 `json:"terminal_code"`
	RuntimeStatus             workflow.RuntimeStatus `json:"runtime_status"`
	OutstandingObligationRefs []string               `json:"outstanding_obligation_refs,omitempty"`
}

// LifecycleState is the five-dimension intent lifecycle state the terminal
// declared, spelled as canonical state ids. There is no sixth dimension and no
// collapsed status: the product renders all five.
type LifecycleState struct {
	RequestState     string `json:"request_state"`
	ExecutionState   string `json:"execution_state"`
	BusinessState    string `json:"business_state"`
	ConsistencyState string `json:"consistency_state"`
	ObligationState  string `json:"obligation_state"`
}

func lifecycleStateOf(d lifecycle.Dimensions) LifecycleState {
	return LifecycleState{
		RequestState:     string(d.State(lifecycle.DimensionRequest)),
		ExecutionState:   string(d.State(lifecycle.DimensionExecution)),
		BusinessState:    string(d.State(lifecycle.DimensionBusiness)),
		ConsistencyState: string(d.State(lifecycle.DimensionConsistency)),
		ObligationState:  string(d.State(lifecycle.DimensionObligation)),
	}
}

// Counters mirrors evidence.EffectCounters in the receipt's own JSON shape. It
// is a separate struct only so the receipt's canonical bytes do not depend on
// another package's field tags; the values are copied straight across and
// [Receipt.EffectCounters] hands the domain type back.
type Counters struct {
	DomainWrites     int `json:"domain_writes"`
	Reservations     int `json:"reservations"`
	WorkItems        int `json:"work_items"`
	Timers           int `json:"timers"`
	Messages         int `json:"messages"`
	OutboxEntries    int `json:"outbox_entries"`
	ProviderCalls    int `json:"provider_calls"`
	ApprovalBindings int `json:"approval_bindings"`
}

func countersOf(c evidence.EffectCounters) Counters {
	return Counters{
		DomainWrites:     c.DomainWrites,
		Reservations:     c.Reservations,
		WorkItems:        c.WorkItems,
		Timers:           c.Timers,
		Messages:         c.Messages,
		OutboxEntries:    c.OutboxEntries,
		ProviderCalls:    c.ProviderCalls,
		ApprovalBindings: c.ApprovalBindings,
	}
}

// ControlVersion is one pinned control artifact the run read: the compiled
// plan, the compiler, the interpreter, a rule table, a catalog, a directory
// projection. A replay that cannot cite the same controls is not a replay.
type ControlVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Receipt is the whole zero-effect artifact one [Run] produces.
//
// Every field is derived from the compiled plan, the declared inputs and the
// fake clock. Nothing in it is read from the host: [Receipt.Digest] is
// byte-stable across runs, processes and machines for the same plan and
// inputs, which is the only thing that makes "running this changed nothing" a
// checkable claim rather than a sentence in a document.
type Receipt struct {
	InterpreterVersion string `json:"interpreter_version"`
	CompilerVersion    string `json:"compiler_version"`

	WorkflowID      string                   `json:"workflow_id"`
	WorkflowVersion uint32                   `json:"workflow_version"`
	PlanDigest      string                   `json:"plan_digest"`
	Mode            workflow.ExecutionMode   `json:"mode"`
	TerminalProfile workflow.TerminalProfile `json:"terminal_profile"`

	InputsDigest string `json:"inputs_digest"`

	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
	Elapsed   time.Duration `json:"elapsed_virtual_ns"`

	Trace     []NodeTrace `json:"trace"`
	WorkItems []WorkItem  `json:"work_items"`

	Terminal  Terminal       `json:"terminal"`
	Lifecycle LifecycleState `json:"lifecycle"`

	Counters Counters         `json:"counters"`
	Controls []ControlVersion `json:"controls"`
	// RecordedEffects holds the would-be effects a WF-TEST-001 recording run
	// captured instead of performing, in walk order. It is omitted when
	// empty so receipts from plain simulations keep their exact bytes.
	RecordedEffects []RecordedEffect `json:"recorded_effects,omitempty"`
	// Cancel holds the WF-TEST-008 settlement of a run canceled at a node:
	// where it stopped, what it had committed, the unwind the compiled
	// plan demands, and the human decisions left open. It is nil for runs
	// to completion, so their receipts keep their exact bytes.
	Cancel *CancelRecord `json:"cancel,omitempty"`
	// ZeroEffect is the domain's own receipt, minted by
	// evidence.NewZeroEffectReceipt, which refuses to exist over a non-zero
	// effect count. Its ExecutionState is always NOT_PLANNED: P1A never plans
	// an execution, and the terminal's own ExecutionState lives in Lifecycle.
	ZeroEffect evidence.ZeroEffectReceipt `json:"-"`
	// ZeroEffectDigest is the hex digest of the domain receipt's canonical
	// bytes, so the simulation receipt cites it without embedding another
	// package's encoding in its own canonical form.
	ZeroEffectDigest string `json:"zero_effect_digest"`

	digest string
}

// Digest is the receipt's content identity.
func (r Receipt) Digest() string { return r.digest }

// EffectCounters returns the domain effect counters the run recorded.
func (r Receipt) EffectCounters() evidence.EffectCounters {
	return evidence.EffectCounters{
		DomainWrites:     r.Counters.DomainWrites,
		Reservations:     r.Counters.Reservations,
		WorkItems:        r.Counters.WorkItems,
		Timers:           r.Counters.Timers,
		Messages:         r.Counters.Messages,
		OutboxEntries:    r.Counters.OutboxEntries,
		ProviderCalls:    r.Counters.ProviderCalls,
		ApprovalBindings: r.Counters.ApprovalBindings,
	}
}

// NodeIDs returns the executed node ids in trace order.
func (r Receipt) NodeIDs() []string {
	out := make([]string, 0, len(r.Trace))
	for _, n := range r.Trace {
		out = append(out, n.NodeID)
	}
	return out
}

// Node returns the trace entry for a node id, and whether it ran.
func (r Receipt) Node(id string) (NodeTrace, bool) {
	for _, n := range r.Trace {
		if n.NodeID == id {
			return n, true
		}
	}
	return NodeTrace{}, false
}

// Verify recomputes the digest over the receipt's current content and reports
// whether it still matches the digest minted at the end of the run.
func (r Receipt) Verify() error {
	if got := computeReceiptDigest(r); got != r.digest {
		return refuse(CodeReceiptInvalid, "", "receipt content no longer matches its digest")
	}
	return nil
}

// JSON renders the receipt as indented, deterministic JSON. Struct field order
// is declaration order and every slice is emitted in the order the run
// produced it, so two identical runs render identical bytes.
func (r Receipt) JSON() ([]byte, error) {
	type rendered struct {
		Receipt
		Digest string `json:"digest"`
	}
	b, err := json.MarshalIndent(rendered{Receipt: r, Digest: r.digest}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// computeReceiptDigest hashes the receipt's canonical bytes under the receipt
// profile. The unexported digest field is excluded by construction, so
// recomputing over a receipt reproduces the digest it was minted with.
func computeReceiptDigest(r Receipt) string {
	return canonicalDigest(receiptDigestProfile, r)
}

// canonicalDigest hashes a value under a profile. The canonical form is the
// encoding/json rendering: struct fields in declaration order, map keys
// sorted, no floating point anywhere in a receipt.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// A receipt is plain data, so this is unreachable. If it ever happens,
		// produce bytes that cannot collide with a real digest rather than
		// silently returning an empty one.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
