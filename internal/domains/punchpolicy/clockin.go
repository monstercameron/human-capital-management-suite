package punchpolicy

import (
	"fmt"
	"strings"
	"time"
)

// DecisionStatus is the closed vocabulary CheckClockIn returns. There is
// deliberately no "denied" or "blocked" status: an early clock-in with no
// override never silently stops the punch from being recorded, it only
// tells the caller whether a supervisor override applied. Recording a
// held-for-review punch is CLOCK-004's concern, not this one's.
type DecisionStatus string

const (
	// Allowed means the punch is inside every configured window: no
	// lockout condition applies.
	Allowed DecisionStatus = "ALLOWED"
	// RequiresOverride means a lockout condition applies (an unpublished
	// shift when the policy requires one, or a punch earlier than the
	// early-lockout window) and no override cleared it.
	RequiresOverride DecisionStatus = "REQUIRES_OVERRIDE"
	// OverriddenAllowed means a lockout condition applied and a supervisor
	// holding a role the policy names cleared it.
	OverriddenAllowed DecisionStatus = "OVERRIDDEN_ALLOWED"
)

// Override is the supervisor credential offered to clear a lockout. An
// Override with a Role the policy does not name never upgrades the
// decision: it is recorded as an attempted, rejected override, still
// routing through RequiresOverride.
type Override struct {
	Role       string
	ApprovedBy string
}

func (o Override) empty() bool {
	return strings.TrimSpace(o.Role) == "" && strings.TrimSpace(o.ApprovedBy) == ""
}

// Decision is the CheckClockIn answer, pinned to the exact policy identity
// that produced it.
type Decision struct {
	Status        DecisionStatus
	Reason        string
	PolicyID      string
	PolicyVersion int
}

// CheckClockIn reports whether punchAt is inside every window Policy.Lockout
// configures for shift. It never blocks the punch from being recorded: an
// unpublished shift under RequiresPublishedShift, or a punch earlier than
// EarlyMinutes before the shift start, always routes to RequiresOverride
// (or OverriddenAllowed, if override names a role the policy recognises)
// rather than a refusal.
func CheckClockIn(policy Policy, punchAt time.Time, shift Shift, override Override) (Decision, error) {
	if err := policy.Validate(); err != nil {
		return Decision{}, err
	}
	if punchAt.IsZero() {
		return Decision{}, fmt.Errorf("%w: punch time is required", ErrInvalidInterval)
	}
	decision := Decision{Status: Allowed, PolicyID: policy.ID, PolicyVersion: policy.Version}

	locked := false
	if policy.Lockout.RequiresPublishedShift && !shift.Published {
		locked = true
		decision.Reason = "shift is not published"
	} else if policy.Lockout.EarlyMinutes > 0 {
		if err := shift.Interval.validate(); err != nil {
			return Decision{}, err
		}
		earlyBoundary := shift.Interval.Start.Add(-time.Duration(policy.Lockout.EarlyMinutes) * time.Minute)
		if punchAt.Before(earlyBoundary) {
			locked = true
			decision.Reason = fmt.Sprintf("punch is earlier than %d minutes before the published shift start", policy.Lockout.EarlyMinutes)
		}
	}
	if !locked {
		return decision, nil
	}

	decision.Status = RequiresOverride
	if override.empty() {
		return decision, nil
	}
	if !policy.Lockout.hasRole(override.Role) {
		decision.Reason = decision.Reason + fmt.Sprintf("; override role %q is not recognised by this policy", override.Role)
		return decision, nil
	}
	if strings.TrimSpace(override.ApprovedBy) == "" {
		decision.Reason += "; supervisor approver is required"
		return decision, nil
	}
	decision.Status = OverriddenAllowed
	decision.Reason = decision.Reason + fmt.Sprintf("; cleared by override role %q", override.Role)
	return decision, nil
}
