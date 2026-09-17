// ATTEND-004: route and resolve attendance exceptions.
//
// Routing turns each evaluated finding into one scoped human work item that
// carries the evaluation evidence digest, an injected deadline and the
// routing requester identity. Resolution reevaluates the affected interval
// from corrected pinned evidence under separation of duties: the resolver
// must differ from the requester, the correction must carry its own evidence
// ref, arrive before the deadline and actually change the pinned input. The
// prior result is preserved on the resolution, never mutated. A correction
// that still shows an exception stays open work. The package is kernel-pure:
// clocks arrive as parameters, never from the wall.
package attendance

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidExceptionWork reports work that cannot be trusted: routing
	// with no exception, or a tampered work item.
	ErrInvalidExceptionWork = errors.New("attendance: invalid exception work")
	// ErrSeparationOfDuties reports a resolver that equals the requester.
	ErrSeparationOfDuties = errors.New("attendance: requester and resolver must differ")
	// ErrResolutionRejected reports a correction that fails the resolution
	// gate: forged evidence, a missed deadline, an unchanged input or a
	// prior the work is not bound to.
	ErrResolutionRejected = errors.New("attendance: resolution rejected")
)

// WorkState is the lifecycle of one exception work item.
type WorkState string

// Work states.
const (
	WorkOpen     WorkState = "OPEN"
	WorkResolved WorkState = "RESOLVED"
)

// ExceptionWorkItem is one scoped human work item for one evaluated finding.
type ExceptionWorkItem struct {
	WorkID         string
	WorkerID       string
	Finding        Finding
	EvidenceDigest string
	PriorOutcome   Outcome
	Requester      string
	Deadline       time.Time
	State          WorkState
}

// RouteExceptionWork creates one scoped work item per evaluated finding. now
// and ttl are injected: the deadline is exactly now plus ttl.
func RouteExceptionWork(result Result, workerID, requester string, now time.Time, ttl time.Duration) ([]ExceptionWorkItem, error) {
	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("%w: evaluation: %v", ErrInvalidExceptionWork, err)
	}
	if result.Outcome != Exception || len(result.Exceptions) == 0 {
		return nil, fmt.Errorf("%w: no exception to route", ErrInvalidExceptionWork)
	}
	if result.InputDigest == "" || workerID == "" || requester == "" {
		return nil, fmt.Errorf("%w: worker, requester and evidence digest are required", ErrInvalidExceptionWork)
	}
	if now.IsZero() || ttl <= 0 {
		return nil, fmt.Errorf("%w: injected clock and positive ttl are required", ErrInvalidExceptionWork)
	}
	items := make([]ExceptionWorkItem, 0, len(result.Exceptions))
	for i, finding := range result.Exceptions {
		items = append(items, ExceptionWorkItem{
			WorkID:         fmt.Sprintf("exception-work-%s-%s-%d", finding.ShiftID, finding.Kind, i),
			WorkerID:       workerID,
			Finding:        finding,
			EvidenceDigest: result.InputDigest,
			PriorOutcome:   result.Outcome,
			Requester:      requester,
			Deadline:       now.Add(ttl),
			State:          WorkOpen,
		})
	}
	return items, nil
}

// ExceptionCorrection carries the corrected pinned evidence for the affected
// interval plus the resolution authority.
type ExceptionCorrection struct {
	Corrected   Request
	EvidenceRef VersionedRef
	Resolver    string
	ResolvedAt  time.Time
}

// ExceptionResolution records the fresh reevaluation with the prior result
// preserved. State is RESOLVED only when the interval reevaluates clean.
type ExceptionResolution struct {
	WorkID       string
	Resolver     string
	PriorDigest  string
	PriorOutcome Outcome
	Result       Result
	State        WorkState
}

// ResolveExceptionWork reevaluates the affected interval from the corrected
// request and preserves the prior result on the resolution.
func ResolveExceptionWork(item ExceptionWorkItem, prior Result, correction ExceptionCorrection) (ExceptionResolution, error) {
	if item.WorkID == "" || item.State != WorkOpen || item.EvidenceDigest == "" || item.Requester == "" {
		return ExceptionResolution{}, fmt.Errorf("%w: work item is tampered or not open", ErrInvalidExceptionWork)
	}
	if item.EvidenceDigest != prior.InputDigest {
		return ExceptionResolution{}, fmt.Errorf("%w: work is not bound to this evaluation", ErrResolutionRejected)
	}
	if correction.Resolver == "" || correction.ResolvedAt.IsZero() {
		return ExceptionResolution{}, fmt.Errorf("%w: resolver and resolution time are required", ErrResolutionRejected)
	}
	if correction.Resolver == item.Requester {
		return ExceptionResolution{}, fmt.Errorf("%w: resolver %q routed the work", ErrSeparationOfDuties, correction.Resolver)
	}
	if err := correction.EvidenceRef.Validate("correction"); err != nil {
		return ExceptionResolution{}, fmt.Errorf("%w: correction evidence: %v", ErrResolutionRejected, err)
	}
	if correction.ResolvedAt.After(item.Deadline) {
		return ExceptionResolution{}, fmt.Errorf("%w: resolution is past the deadline", ErrResolutionRejected)
	}
	fresh, err := Evaluate(correction.Corrected)
	if err != nil {
		return ExceptionResolution{}, fmt.Errorf("%w: correction reevaluation: %v", ErrResolutionRejected, err)
	}
	if fresh.InputDigest == "" || fresh.InputDigest == prior.InputDigest {
		return ExceptionResolution{}, fmt.Errorf("%w: correction changes no pinned evidence", ErrResolutionRejected)
	}
	resolution := ExceptionResolution{
		WorkID:       item.WorkID,
		Resolver:     correction.Resolver,
		PriorDigest:  prior.InputDigest,
		PriorOutcome: prior.Outcome,
		Result:       fresh,
		State:        WorkOpen,
	}
	if fresh.Outcome != Exception {
		resolution.State = WorkResolved
	}
	return resolution, nil
}
