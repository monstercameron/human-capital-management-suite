package replay

// DivergenceField names what differed between a replay and the record it was
// derived from. It is closed for the same reason [EffectAttempt] is: a
// divergence a reader cannot classify is one an investigation cannot triage.
type DivergenceField string

// The declared divergence fields.
const (
	// FieldFrontier reports a recorded attempt at a node the replay does not
	// place on the frontier -- the history says this node ran next, and the
	// re-derived graph walk says it could not have.
	FieldFrontier DivergenceField = "frontier"
	// FieldArtifact reports material the record does not hold: an output, a
	// signal delivery or a timer settlement the replay reached for and did
	// not find. It always accompanies [CodeArtifactUnavailable].
	FieldArtifact DivergenceField = "artifact"
	// FieldRoute reports an outcome the compiled plan cannot route -- the
	// recorded route key names an edge this plan does not declare.
	FieldRoute DivergenceField = "route"
	// FieldOpenFrontier reports a replay that stopped at a different open
	// frontier than the record holds.
	FieldOpenFrontier DivergenceField = "open_frontier"
	// FieldTerminal reports a replay that reached a different terminal than
	// the record.
	FieldTerminal DivergenceField = "terminal"
	// FieldTraceDigest reports a replay whose trace does not reproduce
	// [Record.TraceDigest].
	FieldTraceDigest DivergenceField = "trace_digest"
	// FieldOutcome reports a pure node whose outcome, recomputed by a
	// [Candidate] from its pinned inputs, is not the outcome the record holds:
	// a different route key, or a failure on one side only.
	FieldOutcome DivergenceField = "outcome"
	// FieldOutputDigest reports a pure node whose recomputed output digest is
	// not the recorded one, even though the route agrees.
	FieldOutputDigest DivergenceField = "output_digest"
)

// Divergence is the first place a replay and its record disagree.
//
// It is a value rather than a message because the whole point of WF-RUN-013's
// "comparison identifies divergence" clause is that the answer is actionable:
// which node, which field, what the history says and what the current code
// produces. A replay stops at the first one -- everything after a divergence
// is derived from a state that already differs, so reporting it would be
// reporting noise.
type Divergence struct {
	// Sequence is the recorded sequence of the attempt the divergence was
	// found at, or 0 when the divergence is about the run as a whole (a trace
	// digest, an open frontier).
	Sequence int `json:"sequence"`
	// NodeID names the first differing node. It is empty only for a
	// whole-run divergence that names no single node.
	NodeID  string          `json:"node_id,omitempty"`
	Attempt int             `json:"attempt,omitempty"`
	Field   DivergenceField `json:"field"`
	// Recorded is what the record says, Replayed what re-executing produced.
	Recorded string `json:"recorded"`
	Replayed string `json:"replayed"`
	// Detail is one sentence of context. It is for a human reading a report;
	// nothing branches on it.
	Detail string `json:"detail,omitempty"`
}

// String renders the divergence in one line.
func (d Divergence) String() string {
	loc := d.NodeID
	if loc == "" {
		loc = "instance"
	}
	out := loc + " " + string(d.Field) + ": recorded " + quote(d.Recorded) + ", replayed " + quote(d.Replayed)
	if d.Detail != "" {
		out += " (" + d.Detail + ")"
	}
	return out
}

// quote wraps a value so an empty side of a divergence renders as something a
// reader can see rather than as nothing at all.
func quote(s string) string {
	if s == "" {
		return `""`
	}
	return `"` + s + `"`
}
