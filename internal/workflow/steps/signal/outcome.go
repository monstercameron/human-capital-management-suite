package signal

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

// ToNodeOutcome maps an Accept Result onto frontier.NodeOutcome, the shape
// [frontier.Advance] consumes for a StepSignal node. StepSignal's declared
// routes are SUCCEEDED/TIMED_OUT/CANCELLED (internal/workflow/steptype.go):
// StatusAccepted and StatusDuplicateSameBytes both complete the node on the
// SUCCEEDED route (a duplicate is exactly-once — it reports the same
// completion, never a second one); StatusRefusedLate completes it on the
// declared TIMED_OUT route; every other refusal is not a declared route at
// all, so it is reported as a failed attempt (Failed=true) that takes the
// node's FailureRoute for security/operational review rather than silently
// treating a forged or misdirected signal as a normal business outcome.
func (r Result) ToNodeOutcome(nodeID string) frontier.NodeOutcome {
	switch r.Status {
	case StatusAccepted, StatusDuplicateSameBytes:
		return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.OutcomeSucceeded}
	case StatusRefusedLate:
		if r.DefaultOutputDigest != "" {
			return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.Outcome("TIMED_OUT"), OutputDigest: r.DefaultOutputDigest}
		}
		return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.Outcome("TIMED_OUT")}
	default:
		return frontier.NodeOutcome{NodeID: nodeID, Failed: true, ErrorClass: string(r.Status)}
	}
}
