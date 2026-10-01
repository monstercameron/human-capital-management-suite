package timeprofile

import (
	"errors"
	"fmt"
)

// Sentinel errors. All are matchable with errors.Is; a concrete rejection
// type wraps the sentinel that applies to it so a caller can branch on the
// sentinel without parsing prose.
var (
	// ErrInvalidProfile is returned by TimeProfile.Validate for any
	// structural or cross-field violation. See ProfileRejection.Field for
	// which check failed.
	ErrInvalidProfile = errors.New("TIMEPROFILE_INVALID")

	// ErrNoProfile is returned by Resolve when no eligibility rule matches
	// the assignment. An assignment with no resolvable profile cannot
	// record time (WTIME-001 GREEN).
	ErrNoProfile = errors.New("TIMEPROFILE_NO_MATCH")

	// ErrAmbiguousProfile is returned by Resolve when two or more rules at
	// the same top priority match the same assignment.
	ErrAmbiguousProfile = errors.New("TIMEPROFILE_AMBIGUOUS")

	// ErrNoTimeTemplate is returned by TemplateFor when the profile's
	// capture mode records no time and carries no destination that ignores
	// capture mode (contractor, agency).
	ErrNoTimeTemplate = errors.New("TIMEPROFILE_NO_TEMPLATE")

	// ErrPlanConstraintViolated is returned by CheckPlan when a compiled
	// plan node carries a capability class the profile's category forbids,
	// or the expanded plan is missing a capability class the category
	// requires.
	ErrPlanConstraintViolated = errors.New("TIMEPROFILE_PLAN_CONSTRAINT_VIOLATED")
)

// ProfileRejection is the stable failure shape for TimeProfile.Validate. Field
// names the exact check that failed so a caller never has to parse prose to
// branch on it.
type ProfileRejection struct {
	Field  string
	Reason string
}

func (r *ProfileRejection) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", ErrInvalidProfile, r.Field, r.Reason)
}

// Unwrap exposes the TIMEPROFILE_INVALID sentinel to errors.Is.
func (r *ProfileRejection) Unwrap() error { return ErrInvalidProfile }

func invalidProfile(field, reason string) error {
	return &ProfileRejection{Field: field, Reason: reason}
}

// PlanConstraintViolation is the stable failure shape for CheckPlan. NodeID is
// empty when the violation is a missing required class rather than a
// forbidden one present on a specific node.
type PlanConstraintViolation struct {
	NodeID string
	Class  string
	Reason string
}

func (v *PlanConstraintViolation) Error() string {
	if v.NodeID == "" {
		return fmt.Sprintf("%s: class=%s: %s", ErrPlanConstraintViolated, v.Class, v.Reason)
	}
	return fmt.Sprintf("%s: node=%s class=%s: %s", ErrPlanConstraintViolated, v.NodeID, v.Class, v.Reason)
}

// Unwrap exposes the TIMEPROFILE_PLAN_CONSTRAINT_VIOLATED sentinel to errors.Is.
func (v *PlanConstraintViolation) Unwrap() error { return ErrPlanConstraintViolated }

func forbiddenClassViolation(nodeID, class string) error {
	return &PlanConstraintViolation{NodeID: nodeID, Class: class, Reason: "capability class is forbidden for this profile's category"}
}

func missingRequiredClassViolation(class string) error {
	return &PlanConstraintViolation{Class: class, Reason: "expanded plan is missing a capability class this profile's category requires"}
}
