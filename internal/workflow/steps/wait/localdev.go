package wait

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Profile identifies the runtime profile that is asking to advance a WAIT.
// Production has no manual effective-date advance capability. LocalDev is a
// loopback-only development facility and is deliberately the sole profile
// accepted by Advance.
type Profile string

const (
	ProfileProduction Profile = "production"
	ProfileLocalDev   Profile = "local-dev"

	// LocalDevMaxAdvanceSeconds bounds one development wake request. The
	// caller must still supply both instants; this package never reads a clock.
	LocalDevMaxAdvanceSeconds int64 = 24 * 60 * 60
)

// ProfileCapabilities is the typed capability projection a UI or runtime can
// read. It is descriptive only; Advance repeats the profile fence so callers
// cannot turn a stale capability response into production authority.
type ProfileCapabilities struct {
	Profile                 Profile `json:"profile"`
	CanAdvanceEffectiveDate bool    `json:"can_advance_effective_date"`
	MaxAdvanceSeconds       int64   `json:"max_advance_seconds"`
}

// CapabilitiesFor returns the closed capability set for a runtime profile.
// Unknown profiles fail closed rather than inheriting local-development
// behavior.
func CapabilitiesFor(profile Profile) (ProfileCapabilities, error) {
	switch profile {
	case ProfileProduction:
		return ProfileCapabilities{Profile: profile}, nil
	case ProfileLocalDev:
		return ProfileCapabilities{Profile: profile, CanAdvanceEffectiveDate: true, MaxAdvanceSeconds: LocalDevMaxAdvanceSeconds}, nil
	default:
		return ProfileCapabilities{}, fmt.Errorf("wait: unknown runtime profile %q", profile)
	}
}

// AdvanceRequest names a single, bounded local-development wake attempt.
// Now and Target are both caller-supplied trusted values. Target may not move
// backwards and may not be more than LocalDevMaxAdvanceSeconds after Now.
type AdvanceRequest struct {
	Profile     Profile
	Requirement TimerRequirement
	Now         values.Instant
	Target      values.Instant
}

// Advance evaluates one explicit local-development wake. It is not a timer,
// sleeper, poller or completion shortcut: the typed WAIT resolver remains the
// only authority for FIRED, review, cancellation and early-wake outcomes.
// Production and unknown profiles are refused before resolution.
func Advance(request AdvanceRequest) (Resolution, error) {
	capabilities, err := CapabilitiesFor(request.Profile)
	if err != nil {
		return Resolution{}, err
	}
	if !capabilities.CanAdvanceEffectiveDate {
		return Resolution{}, fmt.Errorf("wait: profile %q cannot advance effective dates", request.Profile)
	}
	if !request.Now.IsSet() || !request.Target.IsSet() {
		return Resolution{}, ErrAdvanceInstantsRequired
	}
	if request.Target.Before(request.Now) {
		return Resolution{}, ErrAdvanceBackwards
	}
	nowSec, _ := request.Now.Unix()
	targetSec, _ := request.Target.Unix()
	if targetSec-nowSec > capabilities.MaxAdvanceSeconds {
		return Resolution{}, fmt.Errorf("wait: local-dev advance exceeds %d seconds", capabilities.MaxAdvanceSeconds)
	}
	return Resolve(request.Requirement, request.Target, WakeEvent{Kind: EventWake})
}

// EffectiveDateWait explains the business-facing state of a durable wait.
// These fields are intentionally separate from workflow identifiers and
// digests so a renderer can explain the wait without leaking machinery.
type EffectiveDateWait struct {
	EffectiveInstant       values.Instant `json:"effective_instant"`
	Timezone               string         `json:"timezone"`
	Owner                  string         `json:"owner"`
	ScheduledAction        string         `json:"scheduled_action"`
	RemainingChecks        string         `json:"remaining_checks"`
	NotificationBehavior   string         `json:"notification_behavior"`
	AuthorizedIntervention string         `json:"authorized_intervention"`
	ReviewRequired         bool           `json:"review_required"`
	ReviewReason           string         `json:"review_reason,omitempty"`
}

// ExplainEffectiveDateWait projects the typed requirement into the facts an
// effective-date wait needs to communicate. It never derives a countdown or
// claims that the wait has completed.
func ExplainEffectiveDateWait(requirement TimerRequirement, owner, scheduledAction, remainingChecks, notificationBehavior, authorizedIntervention string) (EffectiveDateWait, error) {
	if requirement.Digest == "" {
		return EffectiveDateWait{}, ErrInvalidRequirement
	}
	return EffectiveDateWait{
		EffectiveInstant: requirement.FireAt,
		Timezone:         requirement.Reference.Zone.String(),
		Owner:            owner, ScheduledAction: scheduledAction,
		RemainingChecks: remainingChecks, NotificationBehavior: notificationBehavior,
		AuthorizedIntervention: authorizedIntervention,
		ReviewRequired:         requirement.ReviewRequired, ReviewReason: requirement.ReviewReason,
	}, nil
}
