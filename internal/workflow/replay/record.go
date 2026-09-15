package replay

import (
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// NodeRecord is one recorded node attempt: everything the historical run
// produced at that node, and nothing a replay would have to recompute.
//
// It is the record's load-bearing entry. A replay builds its
// [frontier.NodeOutcome] from these fields verbatim -- the route key the
// handler produced, the digest of the artifact it wrote, the terminal an END
// asserted -- so a capability, decision or observation whose answer has
// changed since the run cannot change the replay.
type NodeRecord struct {
	// Sequence is the recorded order of this attempt within the instance,
	// starting at 1. It is what makes a replay a re-derivation of one history
	// rather than a fresh walk of the graph: attempts are replayed in this
	// order, never in plan order.
	Sequence int `json:"sequence"`

	NodeID   string            `json:"node_id"`
	Attempt  int               `json:"attempt"`
	StepType workflow.StepType `json:"step_type"`

	// RouteKey is the outcome route the handler produced. Empty at an END and
	// whenever Await or Failed is set.
	RouteKey string `json:"route_key,omitempty"`
	// OutputDigest is the digest of the typed output artifact the attempt
	// produced. It is recorded, never interpreted.
	OutputDigest string `json:"output_digest,omitempty"`

	// Await and AwaitRef record an attempt that suspended rather than
	// completed.
	Await    frontier.AwaitKind `json:"await,omitempty"`
	AwaitRef string             `json:"await_ref,omitempty"`

	// Failed and ErrorClass record an attempt that produced no outcome.
	Failed     bool   `json:"failed,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`

	// Terminal is the terminal an END attempt asserted.
	Terminal frontier.TerminalResult `json:"terminal,omitzero"`

	// SignalID and TimerID name the recorded signal delivery or timer
	// settlement that satisfied this attempt's wait. A replay resolves them
	// against [Record.Signals] and [Record.Timers] and refuses
	// [CodeArtifactUnavailable] when the record does not hold them, rather
	// than proceeding on an outcome nothing accounts for.
	SignalID string `json:"signal_id,omitempty"`
	TimerID  string `json:"timer_id,omitempty"`

	// RecordedAt is the instant the historical run stamped on this attempt. It
	// is the only clock a replay of this attempt reads.
	RecordedAt time.Time `json:"recorded_at"`

	// RandomDraws are the random values the historical attempt drew, in draw
	// order. A replay hands them back in the same order and refuses a draw the
	// record does not hold.
	RandomDraws []string `json:"random_draws,omitempty"`
}

// FrontierEntry is one recorded membership of the instance's execution
// frontier, shaped exactly as internal/data/runtimestate.FrontierEntry records
// it so a [StoreSource] copies rather than translates.
type FrontierEntry struct {
	NodeID    string    `json:"node_id"`
	State     string    `json:"state"`
	Sequence  uint64    `json:"sequence"`
	EnteredAt time.Time `json:"entered_at"`
	LeftAt    time.Time `json:"left_at,omitzero"`
}

// Open reports whether the entry is still on the frontier -- an entry that has
// not LEFT.
func (e FrontierEntry) Open() bool {
	return e.State != "" && e.State != "LEFT"
}

// SignalReceipt is one recorded signal delivery a waiting node consumed.
type SignalReceipt struct {
	SignalID       string    `json:"signal_id"`
	NodeID         string    `json:"node_id"`
	SignalName     string    `json:"signal_name"`
	CorrelationKey string    `json:"correlation_key"`
	PayloadDigest  string    `json:"payload_digest"`
	DeliveredAt    time.Time `json:"delivered_at"`
}

// TimerSettlement is one recorded timer a waiting node was woken by.
type TimerSettlement struct {
	TimerID   string    `json:"timer_id"`
	NodeID    string    `json:"node_id"`
	Key       string    `json:"key"`
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	FiresAt   time.Time `json:"fires_at"`
	SettledAt time.Time `json:"settled_at"`
}

// Checkpoint is one recorded safe point, shaped as
// internal/data/runtimestate.Checkpoint records it.
type Checkpoint struct {
	Sequence        uint64    `json:"sequence"`
	Kind            string    `json:"kind"`
	StateDigest     string    `json:"state_digest"`
	FrontierDigest  string    `json:"frontier_digest"`
	VariableDigest  string    `json:"variable_digest"`
	InstanceVersion uint64    `json:"instance_version"`
	TakenAt         time.Time `json:"taken_at"`
}

// Record is one instance's complete durable record: what a replay reads
// instead of the world.
//
// Every field is either an identity a replay must check or material a replay
// must consume. There is no field a replay is free to ignore and none it may
// fill in: a record missing what the frontier reaches is
// [CodeArtifactUnavailable], never a silently shorter run.
type Record struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	InstanceID uuid.UUID `json:"instance_id"`

	WorkflowID      string `json:"workflow_id"`
	WorkflowVersion uint32 `json:"workflow_version"`
	// CompiledPlanDigest pins the exact plan the historical run executed. A
	// replay against any other plan is [CodePlanMismatch].
	CompiledPlanDigest string `json:"compiled_plan_digest"`

	CorrelationID string `json:"correlation_id"`
	// HistoricalIntentID is the intent the recorded run was. A replay names it
	// as its causation; see [intent.CausalSeparation].
	HistoricalIntentID string `json:"historical_intent_id"`

	ExecutionMode workflow.ExecutionMode `json:"execution_mode"`
	// FinalStatus is the instance status the record was taken at. A replay of
	// a PAUSED or PAUSE_REQUESTED instance stops at its frontier and reports
	// [StatusPausedAtFrontier] rather than pretending the run ended.
	FinalStatus runtime.InstanceStatus `json:"final_status"`
	// TerminalCode is the terminal the historical run reached, when it
	// reached one. A replay that lands on a different terminal is a
	// divergence; empty means the record asserts none, and the replay's own
	// terminal is adopted unchecked.
	TerminalCode string `json:"terminal_code,omitempty"`

	InputRef string `json:"input_ref"`

	Frontier    []FrontierEntry   `json:"frontier,omitempty"`
	Nodes       []NodeRecord      `json:"nodes"`
	Signals     []SignalReceipt   `json:"signals,omitempty"`
	Timers      []TimerSettlement `json:"timers,omitempty"`
	Checkpoints []Checkpoint      `json:"checkpoints,omitempty"`

	// TraceDigest is the digest the historical run's own trace carried. When
	// it is set, a replay that does not reproduce it exactly is
	// [CodeDivergence]. Empty means the record pins no digest, which is the
	// honest state of a run recorded before its trace was minted.
	TraceDigest string `json:"trace_digest,omitempty"`

	// JoinDeclarations is passed to [frontier.Seed] unchanged, exactly as the
	// historical start passed it.
	JoinDeclarations []frontier.JoinDeclaration `json:"join_declarations,omitempty"`

	// ExecutionContextDigest is the execution context the instance pinned at
	// start (runtime.LoadExecutionContext). When set, every pinned input
	// artifact must have been evaluated under it.
	ExecutionContextDigest string `json:"execution_context_digest,omitempty"`
	// NodeInputs are the pinned inputs pure node attempts were evaluated
	// against (runtime.LoadNodeInputs). A pure node a [Candidate] recomputes
	// reads its inputs from here and nowhere else; a node with no artifact
	// here is [CodeArtifactUnavailable], never a read of current data.
	NodeInputs []runtime.NodeInputArtifact `json:"node_inputs,omitempty"`
	// PinnedVersion is the compiled version the durable version registry
	// holds for [Record.CompiledPlanDigest], when the source read it. A
	// replay refuses a plan whose canonical bytes are not the published ones.
	PinnedVersion *PinnedVersion `json:"pinned_version,omitempty"`
}

// PinnedVersion is the part of a durable compiled-version record a replay
// checks its plan against.
type PinnedVersion struct {
	CompiledPlanDigest string `json:"compiled_plan_digest"`
	SemanticVersion    string `json:"semantic_version"`
	RecordDigest       string `json:"record_digest"`
	CanonicalPlanBytes []byte `json:"canonical_plan_bytes"`
}

// NodeInput returns the pinned input artifact of one node attempt.
func (r Record) NodeInput(nodeID string, attempt int) (runtime.NodeInputArtifact, bool) {
	for _, a := range r.NodeInputs {
		if a.NodeID == nodeID && a.Attempt == attempt {
			return a.Clone(), true
		}
	}
	return runtime.NodeInputArtifact{}, false
}

// Validate reports whether the record is replayable on its own terms.
func (r Record) Validate() error {
	switch {
	case r.TenantID == uuid.Nil:
		return refuse(CodeRecordInvalid, "", "tenant id must not be the nil UUID")
	case r.InstanceID == uuid.Nil:
		return refuse(CodeRecordInvalid, "", "instance id must not be the nil UUID")
	case r.WorkflowID == "":
		return refuse(CodeRecordInvalid, "", "workflow id is required")
	case r.CompiledPlanDigest == "":
		return refuse(CodeRecordInvalid, "",
			"compiled plan digest is required; a record must pin what it ran")
	case r.HistoricalIntentID == "":
		return refuse(CodeRecordInvalid, "",
			"historical intent id is required; a replay must name the run it re-derives")
	case !r.FinalStatus.Valid():
		return refuse(CodeRecordInvalid, "", "instance status %q is not declared", string(r.FinalStatus))
	case len(r.Nodes) == 0:
		return refuse(CodeRecordInvalid, "", "record holds no node attempts")
	}
	seen := map[int]bool{}
	for _, n := range r.Nodes {
		switch {
		case n.NodeID == "":
			return refuse(CodeRecordInvalid, "", "a node attempt names no node")
		case n.Attempt < 1:
			return refuse(CodeRecordInvalid, n.NodeID, "attempt must be at least 1")
		case n.Sequence < 1:
			return refuse(CodeRecordInvalid, n.NodeID, "recorded sequence starts at 1")
		case n.StepType == "":
			return refuse(CodeRecordInvalid, n.NodeID, "step type is required")
		case n.RecordedAt.IsZero():
			return refuse(CodeRecordInvalid, n.NodeID,
				"recorded_at is required; a replay reads no wall clock")
		case seen[n.Sequence]:
			return refuse(CodeRecordInvalid, n.NodeID,
				"two attempts share recorded sequence %d; replay order would be ambiguous", n.Sequence)
		}
		seen[n.Sequence] = true
	}
	return nil
}

// Ordered returns the recorded attempts sorted by their recorded sequence. It
// is a copy: a replay never sorts the caller's slice in place.
func (r Record) Ordered() []NodeRecord {
	out := append([]NodeRecord(nil), r.Nodes...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out
}

// Signal returns the recorded delivery with this id.
func (r Record) Signal(id string) (SignalReceipt, bool) {
	for _, s := range r.Signals {
		if s.SignalID == id {
			return s, true
		}
	}
	return SignalReceipt{}, false
}

// Timer returns the recorded settlement with this id.
func (r Record) Timer(id string) (TimerSettlement, bool) {
	for _, t := range r.Timers {
		if t.TimerID == id {
			return t, true
		}
	}
	return TimerSettlement{}, false
}

// OpenFrontier returns, sorted, every node the record still places on the
// frontier. It is empty for a record whose instance completed, and it is what
// a paused replay is checked against.
func (r Record) OpenFrontier() []string {
	var out []string
	for _, e := range r.Frontier {
		if e.Open() {
			out = append(out, e.NodeID)
		}
	}
	sort.Strings(out)
	return out
}

// Paused reports whether the record was taken while the instance was paused or
// had a standing pause request.
func (r Record) Paused() bool {
	return r.FinalStatus == runtime.InstancePaused || r.FinalStatus == runtime.InstancePauseRequested
}

// Clone returns a deep copy, so a caller holding a record and a replay reading
// one never share a backing array.
func (r Record) Clone() Record {
	nodes := make([]NodeRecord, len(r.Nodes))
	for i, n := range r.Nodes {
		n.RandomDraws = append([]string(nil), n.RandomDraws...)
		nodes[i] = n
	}
	r.Nodes = nodes
	r.Frontier = append([]FrontierEntry(nil), r.Frontier...)
	r.Signals = append([]SignalReceipt(nil), r.Signals...)
	r.Timers = append([]TimerSettlement(nil), r.Timers...)
	r.Checkpoints = append([]Checkpoint(nil), r.Checkpoints...)
	r.JoinDeclarations = append([]frontier.JoinDeclaration(nil), r.JoinDeclarations...)
	if r.NodeInputs != nil {
		inputs := make([]runtime.NodeInputArtifact, len(r.NodeInputs))
		for i, a := range r.NodeInputs {
			inputs[i] = a.Clone()
		}
		r.NodeInputs = inputs
	}
	if r.PinnedVersion != nil {
		pv := *r.PinnedVersion
		pv.CanonicalPlanBytes = append([]byte(nil), pv.CanonicalPlanBytes...)
		r.PinnedVersion = &pv
	}
	return r
}
