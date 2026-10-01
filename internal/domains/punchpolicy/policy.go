package punchpolicy

import (
	"fmt"
	"strings"
)

// RoundingDirection is the closed vocabulary of rounding directions. Every
// declared direction rounds both the start and the end of an interval by the
// same absolute rule (nearest, always up, or always down to the increment
// boundary), so the expected loss at one edge is matched by the expected
// gain at the other over a uniform distribution of punch minutes: this is
// what 29 CFR 785.48(b) requires and what TestTodo_TCLOCK_009_Property
// proves. A rounding rule that instead always shrinks the paid interval (for
// example rounding the start forward and the end backward) is not a
// declared direction: it fails Rounding.Valid and can never reach an
// evaluation.
type RoundingDirection string

const (
	RoundNearest RoundingDirection = "NEAREST"
	RoundUp      RoundingDirection = "UP"
	RoundDown    RoundingDirection = "DOWN"
)

// Valid reports whether d is a declared, neutral rounding direction.
func (d RoundingDirection) Valid() bool {
	switch d {
	case RoundNearest, RoundUp, RoundDown:
		return true
	}
	return false
}

// Rounding pins the increment and direction applied to a punch's evaluated
// time. IncrementMinutes zero means punches are evaluated at clock precision
// with no rounding at all.
type Rounding struct {
	IncrementMinutes int
	Mode             RoundingDirection
}

func (r Rounding) validate() error {
	if r.IncrementMinutes < 0 {
		return fmt.Errorf("%w: rounding increment must not be negative", ErrInvalidPolicy)
	}
	if r.IncrementMinutes == 0 {
		return nil
	}
	if !r.Mode.Valid() {
		return fmt.Errorf("%w: rounding mode %q is not a declared neutral direction", ErrInvalidPolicy, r.Mode)
	}
	return nil
}

// Grace is the early/late window around a shift boundary within which a
// punch is not itself an exception. It says nothing about lockout: a punch
// inside the early grace window can still be locked out if it also falls
// inside Lockout.EarlyMinutes.
type Grace struct {
	EarlyMinutes int
	LateMinutes  int
}

func (g Grace) validate() error {
	if g.EarlyMinutes < 0 || g.LateMinutes < 0 {
		return fmt.Errorf("%w: grace minutes must not be negative", ErrInvalidPolicy)
	}
	return nil
}

// Lockout is the clock-in lockout relative to the published shift.
// RequiresPublishedShift makes an unpublished shift itself a lockout
// condition rather than an unlimited window: CheckClockIn always routes an
// unpublished shift to the override path, never to a silent block.
// OverrideRoles is the closed list of supervisor roles that may clear a
// lockout; an empty list means no override role is configured for this
// policy, so a locked-out punch can only be recorded once a shift is
// published or the policy is republished with a role.
type Lockout struct {
	EarlyMinutes           int
	RequiresPublishedShift bool
	OverrideRoles          []string
}

func (l Lockout) validate() error {
	if l.EarlyMinutes < 0 {
		return fmt.Errorf("%w: lockout early minutes must not be negative", ErrInvalidPolicy)
	}
	for i, role := range l.OverrideRoles {
		if strings.TrimSpace(role) == "" {
			return fmt.Errorf("%w: override role[%d] is blank", ErrInvalidPolicy, i)
		}
	}
	return nil
}

func (l Lockout) hasRole(role string) bool {
	for _, r := range l.OverrideRoles {
		if r == role {
			return true
		}
	}
	return false
}

// AutoDeduct is an automatic meal/break deduction applied after a threshold
// of worked minutes. WaivedByAttestation makes the deduction yield to the
// worker's own attestation that the break was not taken: the deduction never
// applies over an attestation the policy says to trust.
type AutoDeduct struct {
	AfterMinutes        int
	DeductMinutes       int
	WaivedByAttestation bool
}

func (a AutoDeduct) validate() error {
	if a.AfterMinutes < 0 || a.DeductMinutes < 0 {
		return fmt.Errorf("%w: auto-deduct minutes must not be negative", ErrInvalidPolicy)
	}
	if a.DeductMinutes > 0 && a.AfterMinutes == 0 {
		return fmt.Errorf("%w: an auto-deduct with no worked-minutes threshold would apply unconditionally", ErrInvalidPolicy)
	}
	return nil
}

// Policy is a versioned, data-only punch policy for one site and
// jurisdiction. It is never mutated in place: a change to any field is a new
// Version, and an EvaluatedInterval pins the exact ID and Version it applied.
type Policy struct {
	ID           string
	Version      int
	Jurisdiction string
	Rounding     Rounding
	Grace        Grace
	Lockout      Lockout
	AutoDeduct   AutoDeduct
}

// Validate reports whether the policy is a well-formed, self-consistent
// value. It does not know whether a jurisdiction accepts it: that is
// ValidateFor.
func (p Policy) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("%w: policy id is required", ErrInvalidPolicy)
	}
	if p.Version < 1 {
		return fmt.Errorf("%w: policy version must be at least 1", ErrInvalidPolicy)
	}
	if strings.TrimSpace(p.Jurisdiction) == "" {
		return fmt.Errorf("%w: jurisdiction is required", ErrInvalidPolicy)
	}
	if err := p.Rounding.validate(); err != nil {
		return err
	}
	if err := p.Grace.validate(); err != nil {
		return err
	}
	if err := p.Lockout.validate(); err != nil {
		return err
	}
	return p.AutoDeduct.validate()
}

// JurisdictionRules is the versioned rule pack a jurisdiction publishes for
// punch policies. RoundingForbidden and MaxLockoutEarlyMinutes are zero-value
// friendly: RoundingForbidden false permits rounding, and
// MaxLockoutEarlyMinutes zero means the jurisdiction imposes no cap.
type JurisdictionRules struct {
	Code                   string
	RoundingForbidden      bool
	MaxLockoutEarlyMinutes int
	// AllowedOverrideRoles, when non-empty, is the closed set of roles the
	// jurisdiction permits as a lockout override; an empty set imposes no
	// restriction beyond the policy's own OverrideRoles.
	AllowedOverrideRoles []string
}

func (rules JurisdictionRules) allows(role string) bool {
	if len(rules.AllowedOverrideRoles) == 0 {
		return true
	}
	for _, r := range rules.AllowedOverrideRoles {
		if r == role {
			return true
		}
	}
	return false
}

// ValidateFor reports whether the jurisdiction's rule pack accepts an
// otherwise well-formed policy. A jurisdiction that forbids rounding rejects
// any policy with a non-zero rounding increment; a jurisdiction that caps
// the lockout window rejects a policy configured past the cap; a policy
// naming an override role the jurisdiction does not recognise is rejected
// rather than silently narrowed.
func ValidateFor(policy Policy, rules JurisdictionRules) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if rules.Code != policy.Jurisdiction {
		return fmt.Errorf("%w: jurisdiction rules are for %q, policy is pinned to %q", ErrPolicyRejected, rules.Code, policy.Jurisdiction)
	}
	if rules.RoundingForbidden && policy.Rounding.IncrementMinutes > 0 {
		return fmt.Errorf("%w: %s forbids punch rounding", ErrPolicyRejected, rules.Code)
	}
	if rules.MaxLockoutEarlyMinutes > 0 && policy.Lockout.EarlyMinutes > rules.MaxLockoutEarlyMinutes {
		return fmt.Errorf("%w: lockout of %d minutes exceeds the %d minute cap in %s", ErrPolicyRejected, policy.Lockout.EarlyMinutes, rules.MaxLockoutEarlyMinutes, rules.Code)
	}
	for _, role := range policy.Lockout.OverrideRoles {
		if !rules.allows(role) {
			return fmt.Errorf("%w: override role %q is not recognised by %s", ErrPolicyRejected, role, rules.Code)
		}
	}
	return nil
}
