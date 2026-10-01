// Package crewshift owns draft and published crew shifts: who is assigned,
// where and under which project or work order, the local timezone the shift
// is planned in, the break plan, and the revision history publication
// produces. It is pure (ARCH-GO-009-style contract): no database, no clock,
// no network, and no package-level mutable state. Every instant a caller
// cares about is passed in; the package never reads time.Now.
//
// Two todos live here. FTIME-006 creates and publishes revisioned shifts:
// publication checks eligibility, qualifications, project access, overlap
// across projects, rest and a configured notice policy, and it records who
// approved the change. A daylight-saving transition never silently changes
// worked hours because every duration is computed from absolute instants,
// never from naive wall-clock subtraction. An optimizer's proposed shift
// reaches the exact same Publish path as a manually drafted one: there is no
// second, looser authority path for a machine-proposed shift to slip through.
//
// FTIME-007 reconciles schedule changes and attendance. Publish, cancel and
// reassign all go through ApplyLifecycle, which requires the caller's
// expected revision to match the stored one and a live (non-revoked) grant
// for the action; every prior version is retained in the returned History,
// and affected workers get computed Notifications. Reconcile compares actual
// punches (supplied as intervals, already captured by internal/domains/clock
// or internal/domains/attendance) against a published shift and reports late,
// early, missed, unscheduled and break exceptions -- it is read-only over the
// punches and never rewrites one.
//
// Minors' and EU rest-period rules are not implemented here: they plug into
// Publish through the PublishCheck slice so another lane can add them without
// editing this package.
package crewshift

import (
	"errors"
	"fmt"
)

const contractVersion = 1

// Version reports the crewshift contract version. A change to the meaning or
// shape of a Shift, a lifecycle transition or a reconciliation result must
// advance this value.
func Version() int { return contractVersion }

// Sentinel errors. Callers distinguish them with errors.Is; the accompanying
// rejection structs (PublishRejection, LifecycleRejection) carry the field,
// state and reason a caller needs to explain the refusal.
var (
	// ErrInvalidShift reports a shift whose own shape is not well formed,
	// independent of any publication or lifecycle rule.
	ErrInvalidShift = errors.New("crewshift: invalid shift")
	// ErrPublishRejected is the FTIME-006 sentinel: a draft failed one of
	// the publication checks.
	ErrPublishRejected = errors.New("CREWSHIFT_PUBLISH_REJECTED")
	// ErrLifecycleRejected is the FTIME-007 sentinel: a publish, cancel or
	// reassign attempt failed its revision, grant or transition rule.
	ErrLifecycleRejected = errors.New("CREWSHIFT_LIFECYCLE_REJECTED")
	// ErrReconcileRejected reports a reconciliation request that cannot be
	// evaluated, such as a shift that was never published.
	ErrReconcileRejected = errors.New("CREWSHIFT_RECONCILE_REJECTED")
)

// Explanation is the ARCH-GO-009 human-readable trace of one Publish or
// ApplyLifecycle outcome. It restates the decision; it never carries
// authority of its own.
type Explanation struct {
	ShiftID  string
	Revision int64
	Outcome  string
	Reasons  []string
}

// Explain renders a stable, human-readable trace of a publish attempt. It is
// evidence for an operator or an audit log, not a second decision path.
func (r PublishOutcome) Explain() Explanation {
	exp := Explanation{ShiftID: r.Shift.ID, Revision: r.Shift.Revision}
	if r.Rejection != nil {
		exp.Outcome = "REJECTED"
		exp.Reasons = []string{r.Rejection.Error()}
		return exp
	}
	exp.Outcome = "PUBLISHED"
	exp.Reasons = []string{fmt.Sprintf("approved by %s", r.Shift.ApprovedBy.String())}
	return exp
}

// Explain is the function form of PublishOutcome.Explain.
func Explain(r PublishOutcome) Explanation { return r.Explain() }
