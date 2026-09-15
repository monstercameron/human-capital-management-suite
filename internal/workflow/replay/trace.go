package replay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Canonicalization profile identity. A digest is always prefixed with the
// profile that produced it, so bytes canonicalized as a replay trace can never
// be mistaken for a frontier transition's digest or a plan's.
//
// It follows the canonicalization the rest of the workflow layer already uses
// for its own artifacts (internal/workflow/frontier.canonicalDigest,
// internal/workflow/simulate.computeReceiptDigest): a profile-prefixed
// SHA-256 over the encoding/json rendering, with struct fields in declaration
// order, every slice sorted by the code that built it, and no map and no
// floating point anywhere in the value.
const traceDigestProfile = "hcmnext.workflow.replay.Trace/v1"

// InputSource names where one replayed node took its inputs from. It is
// carried in the trace because "this node was re-derived from a recorded
// output" and "this node was re-derived from a recorded signal" are materially
// different facts about a replay's fidelity.
type InputSource string

// The declared input sources.
const (
	// SourceRecordedOutput is the ordinary case: the node's own recorded
	// attempt.
	SourceRecordedOutput InputSource = "RECORDED_OUTPUT"
	// SourceRecordedSignal is a waiting node satisfied by a recorded signal
	// delivery.
	SourceRecordedSignal InputSource = "RECORDED_SIGNAL"
	// SourceRecordedTimer is a waiting node woken by a recorded timer
	// settlement.
	SourceRecordedTimer InputSource = "RECORDED_TIMER"
	// SourceRecomputed is a pure node whose outcome a [Candidate] recomputed
	// from the node's pinned historical inputs, and which matched the record.
	SourceRecomputed InputSource = "RECOMPUTED_FROM_PINNED_INPUTS"
)

// TraceEntry is one replayed node attempt.
//
// It restates what the replay consumed and what advancing on it produced, so a
// reader comparing two traces can see not only that they differ but where.
type TraceEntry struct {
	Sequence int               `json:"sequence"`
	NodeID   string            `json:"node_id"`
	Attempt  int               `json:"attempt"`
	StepType workflow.StepType `json:"step_type"`
	Source   InputSource       `json:"source"`

	RouteKey     string `json:"route_key,omitempty"`
	OutputDigest string `json:"output_digest,omitempty"`
	// InputDigest is the digest of the pinned input artifact a recomputed node
	// was evaluated against. Empty for a node that took its recorded outcome.
	InputDigest string `json:"input_digest,omitempty"`
	// CompletedState is the state the node holds after the advancement,
	// spelled as [frontier.NodeState].
	CompletedState string `json:"completed_state"`
	// Frontier is the resulting frontier, sorted.
	Frontier []string `json:"frontier"`
	// TransitionDigest is the digest [frontier.Advance] minted for this
	// advancement. It is the per-step fingerprint the trace digest is built
	// over, so a divergence at one node changes the trace at that node rather
	// than only at the end.
	TransitionDigest string `json:"transition_digest"`
	// ObservedAt is the recorded instant of the attempt. It comes from the
	// record; this package reads no wall clock.
	ObservedAt time.Time `json:"observed_at"`
}

// Trace is the complete result of one replay: every node it re-derived, in
// recorded order, and where the instance stood when it stopped.
//
// It has a content digest, and reproducing that digest is the contract:
// [Record.TraceDigest], when set, must equal [Trace.Digest] or the replay is a
// divergence.
type Trace struct {
	TenantID   string `json:"tenant_id"`
	InstanceID string `json:"instance_id"`
	WorkflowID string `json:"workflow_id"`
	Version    uint32 `json:"version"`
	PlanDigest string `json:"plan_digest"`
	// HistoricalIntentID is the run this trace re-derives. It is part of the
	// digest so a trace of one instance can never be read as a trace of
	// another with the same shape.
	HistoricalIntentID string `json:"historical_intent_id"`

	Entries []TraceEntry `json:"entries"`

	Status Status `json:"status"`
	// TerminalCode is set when the replay reached a terminal.
	TerminalCode string `json:"terminal_code,omitempty"`
	// Frontier is where the replay stopped, sorted. It is empty for a
	// completed instance and is the paused instance's open frontier for a
	// [StatusPausedAtFrontier] replay.
	Frontier []string `json:"frontier"`

	digest string
}

// Digest is the trace's content identity.
func (t Trace) Digest() string { return t.digest }

// Verify recomputes the digest over the trace's current content and reports
// whether it still matches the digest the replay minted. It is what makes a
// trace handed across a boundary checkable rather than merely plausible.
func (t Trace) Verify() error {
	if got := computeTraceDigest(t); got != t.digest {
		return refuse(CodeDivergence, "", "trace content no longer matches its digest")
	}
	return nil
}

// JSON renders the trace as indented, deterministic JSON with its digest
// attached. Two replays of the same record render identical bytes, which is
// what makes a checked-in golden a contract rather than a snapshot.
func (t Trace) JSON() ([]byte, error) {
	type rendered struct {
		Trace
		Digest string `json:"digest"`
	}
	b, err := json.MarshalIndent(rendered{Trace: t, Digest: t.digest}, "", "  ")
	if err != nil {
		return nil, wrap(CodeDivergence, "", err, "render trace")
	}
	return append(b, '\n'), nil
}

// computeTraceDigest hashes a trace's canonical bytes. The unexported digest
// field is excluded by construction, so recomputing over a trace reproduces
// the digest it was minted with.
func computeTraceDigest(t Trace) string {
	b, err := json.Marshal(t)
	if err != nil {
		// A trace is plain data, so this is unreachable. If it ever happens,
		// produce bytes that cannot collide with a real digest rather than
		// silently returning an empty one.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(traceDigestProfile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
