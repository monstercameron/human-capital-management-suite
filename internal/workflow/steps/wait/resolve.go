package wait

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

// Outcome is the domain result Resolve produces for one wake attempt against
// one TimerRequirement. This is WF-STEP-005's own vocabulary (the todo's
// GREEN line names exactly these four); ToNodeOutcome maps it onto
// frontier.NodeOutcome's narrower, step-type-conformant route vocabulary.
type Outcome string

// The four resolutions Resolve can produce.
const (
	// OutcomeFired says the timer's fire-at instant has passed and the WAIT
	// node completed.
	OutcomeFired Outcome = "FIRED"
	// OutcomeTimerReviewRequired says the requirement could not be trusted to
	// fire on its own — a DST-ambiguous/non-existent local time, or any other
	// condition the requirement itself flagged — and a human must resolve it.
	OutcomeTimerReviewRequired Outcome = "TIMER_REVIEW_REQUIRED"
	// OutcomeSuperseded says this exact requirement no longer governs the
	// wait (a dataset republish produced a new one under RECALCULATE, for
	// example); the node keeps waiting, just not on this requirement.
	OutcomeSuperseded Outcome = "SUPERSEDED"
	// OutcomeCancelled says an explicit cancellation command resolved the
	// wait before it fired.
	OutcomeCancelled Outcome = "CANCELLED"
)

// EventKind names what kind of wake attempt Resolve is being asked to
// process.
type EventKind string

// The declared wake-event kinds.
const (
	// EventWake is a caller-driven check of whether the timer has fired. It
	// is the only kind that consults "now".
	EventWake EventKind = "WAKE"
	// EventCancel is an explicit cancellation command.
	EventCancel EventKind = "CANCEL"
	// EventSupersede reports that a dataset republish (or a newer compiled
	// plan) replaced this requirement with a fresh one.
	EventSupersede EventKind = "SUPERSEDE"
)

// WakeEvent is the caller-supplied event Resolve reacts to. Resolve reads no
// ambient clock or event source: both are named here explicitly.
type WakeEvent struct {
	Kind   EventKind
	Reason string

	// Prior, when non-nil, is the durable Resolution already recorded for
	// this exact requirement. Resolve returns it unchanged without
	// re-deriving anything: that is what makes a duplicate wake for an
	// already-resolved requirement return the original resolution
	// (semantic exactly-once) rather than recompute it — including, for a
	// WAKE replay, recomputing against a "now" that may since have moved.
	Prior *Resolution
}

// Resolution is the durable, typed record of what Resolve decided for one
// TimerRequirement. It is immutable data: a runtime persists it once and
// replays it verbatim for every later duplicate wake.
type Resolution struct {
	RequirementDigest string         `json:"requirement_digest"`
	Outcome           Outcome        `json:"outcome"`
	ResolvedAt        values.Instant `json:"-"`
	Reason            string         `json:"reason,omitempty"`
	Digest            string         `json:"digest"`
}

// ResolvedAtText returns the canonical text of ResolvedAt, or "" when unset.
func (r Resolution) ResolvedAtText() string { return resolvedAtText(r.ResolvedAt) }

func resolvedAtText(i values.Instant) string {
	if !i.IsSet() {
		return ""
	}
	return i.String()
}

func newResolution(requirementDigest string, outcome Outcome, now values.Instant, reason string) Resolution {
	r := Resolution{RequirementDigest: requirementDigest, Outcome: outcome, ResolvedAt: now, Reason: reason}
	r.Digest = computeResolutionDigest(r)
	return r
}

// Resolve is WF-STEP-005's pure decision function. Given a TimerRequirement
// [ComputeTimerRequirement] minted, the caller-supplied instant "now", and one
// caller-supplied WakeEvent, it returns exactly one of FIRED,
// TIMER_REVIEW_REQUIRED, SUPERSEDED or CANCELLED.
//
//   - A requirement with a missing or mismatched content digest is refused
//     outright before either resolving or replaying it.
//   - event.Prior, when set, is replayed verbatim (exactly-once dedup) — a
//     duplicate wake for an already-resolved requirement never recomputes.
//   - A requirement with ReviewRequired set always resolves to
//     TIMER_REVIEW_REQUIRED, regardless of event kind: a timer that could not
//     be trusted to compute its own fire-at instant cannot be trusted to fire
//     on a schedule either.
//   - EventCancel and EventSupersede resolve unconditionally to CANCELLED and
//     SUPERSEDED; time never overrides an explicit command.
//   - EventWake requires a valid "now" and refuses an early wake (now before
//     fire_at) rather than firing ahead of schedule.
func Resolve(req TimerRequirement, now values.Instant, event WakeEvent) (Resolution, error) {
	if req.Digest == "" || computeRequirementDigest(req) != req.Digest {
		return Resolution{}, ErrInvalidRequirement
	}
	if event.Prior != nil {
		if event.Prior.RequirementDigest != req.Digest {
			return Resolution{}, fmt.Errorf("%w: requirement=%s prior=%s", ErrDigestMismatch, req.Digest, event.Prior.RequirementDigest)
		}
		if event.Prior.Digest == "" || computeResolutionDigest(*event.Prior) != event.Prior.Digest {
			return Resolution{}, fmt.Errorf("%w: prior resolution content", ErrDigestMismatch)
		}
		return *event.Prior, nil
	}
	if req.ReviewRequired {
		return newResolution(req.Digest, OutcomeTimerReviewRequired, now, req.ReviewReason), nil
	}
	switch event.Kind {
	case EventCancel:
		return newResolution(req.Digest, OutcomeCancelled, now, event.Reason), nil
	case EventSupersede:
		return newResolution(req.Digest, OutcomeSuperseded, now, event.Reason), nil
	case EventWake, "":
		if !now.IsSet() {
			return Resolution{}, ErrNowRequired
		}
		if now.Before(req.FireAt) {
			return Resolution{}, fmt.Errorf("%w: now=%s fire_at=%s", ErrEarlyWake, now, req.FireAt)
		}
		return newResolution(req.Digest, OutcomeFired, now, ""), nil
	default:
		return Resolution{}, fmt.Errorf("%w: %q", ErrUnknownEventKind, event.Kind)
	}
}

// ToNodeOutcome maps a Resolution onto frontier.NodeOutcome, the shape
// [frontier.Advance] consumes. WF-STEP-005's own four-outcome vocabulary does
// not line up one-to-one with StepWait's declared route outcomes
// (SUCCEEDED/LATE/CANCELLED): FIRED completes the node (SUCCEEDED); CANCELLED
// completes it on the declared CANCELLED route; TIMER_REVIEW_REQUIRED is not
// a declared route at all — it is treated as a failed attempt (Failed=true)
// so the runtime takes the node's FailureRoute for human review rather than
// silently succeeding or guessing a route; SUPERSEDED is not terminal at all
// — the node is still awaiting a timer, just not this one, so it is reported
// as AwaitTimer with the (stale) requirement digest as the correlation ref.
func (r Resolution) ToNodeOutcome(nodeID string) frontier.NodeOutcome {
	switch r.Outcome {
	case OutcomeFired:
		return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.OutcomeSucceeded}
	case OutcomeCancelled:
		return frontier.NodeOutcome{NodeID: nodeID, Outcome: workflow.Outcome("CANCELLED")}
	case OutcomeSuperseded:
		return frontier.NodeOutcome{NodeID: nodeID, Await: frontier.AwaitTimer, AwaitRef: r.RequirementDigest}
	case OutcomeTimerReviewRequired:
		return frontier.NodeOutcome{NodeID: nodeID, Failed: true, ErrorClass: string(OutcomeTimerReviewRequired)}
	default:
		return frontier.NodeOutcome{NodeID: nodeID, Failed: true, ErrorClass: "UNKNOWN_TIMER_OUTCOME"}
	}
}
