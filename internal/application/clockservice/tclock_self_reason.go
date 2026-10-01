package clockservice

import "errors"

// SelfClockReason is the closed, machine-readable answer to "why is my own
// clock not offered to me". It is the one availability vocabulary: the HTTP
// boundary publishes it, the browser client maps it to a page state, and the
// page turns it into copy. A reason never carries a worker, assignment or
// profile identity, so it is safe to show the worker it describes.
type SelfClockReason string

const (
	// ReasonNoWorkerRecord means the signed-in principal does not resolve to
	// an active worker in this workspace.
	ReasonNoWorkerRecord SelfClockReason = "NO_WORKER_RECORD"
	// ReasonNoAssignment means the worker is active but holds no current
	// assignment to record time against.
	ReasonNoAssignment SelfClockReason = "NO_ASSIGNMENT"
	// ReasonNoTimeProfile means no published time profile is pinned to the
	// worker's assignment, so time cannot be recorded.
	ReasonNoTimeProfile SelfClockReason = "NO_TIME_PROFILE"
	// ReasonExempt means the worker's profile is exempt from time recording.
	ReasonExempt SelfClockReason = "EXEMPT"
	// ReasonCaptureNotPunch means the worker's profile records time by
	// another method (duration or exception only), not by clocking.
	ReasonCaptureNotPunch SelfClockReason = "CAPTURE_NOT_PUNCH"
	// ReasonNotEnabled means the workspace does not run the time clock
	// service at all. It is never produced by the service: the boundary that
	// is absent cannot say so, and the client infers it.
	ReasonNotEnabled SelfClockReason = "NOT_ENABLED"
)

// Valid reports whether r is one of the closed reasons.
func (r SelfClockReason) Valid() bool {
	switch r {
	case ReasonNoWorkerRecord, ReasonNoAssignment, ReasonNoTimeProfile, ReasonExempt, ReasonCaptureNotPunch, ReasonNotEnabled:
		return true
	}
	return false
}

// NotEligible returns an ErrWorkerNotEligible rejection that carries reason.
// Adapters use it so a missing pin, an inactive worker and a non-clocking
// profile stay distinguishable without matching on message text.
func NotEligible(reason SelfClockReason, detail string) error {
	return reject(ErrWorkerNotEligible, "self_clock", string(reason), detail)
}

// ReasonOf extracts the availability reason from an ineligibility error. It
// reports false for every other failure, so an outage is never presented as
// an eligibility decision.
func ReasonOf(err error) (SelfClockReason, bool) {
	var rejection *Rejection
	if !errors.As(err, &rejection) || !errors.Is(rejection.Err, ErrWorkerNotEligible) {
		return "", false
	}
	reason := SelfClockReason(rejection.State)
	if !reason.Valid() || reason == ReasonNotEnabled {
		return "", false
	}
	return reason, true
}
