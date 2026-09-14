package pilotjurisdiction

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrOutsideReviewedScope is returned by [JurisdictionProfile.Authorize] when
// legal.Resolve confidently resolves a transaction to a real, registered
// jurisdiction that is simply not the one this profile reviewed. This is
// deliberately distinct from [legal.ErrLegalContextUnknown]: the platform is
// not confused about the facts, this profile just has not reviewed that
// jurisdiction, and GREEN forbids guessing outside reviewed scope.
var ErrOutsideReviewedScope = errors.New("pilotjurisdiction: OUTSIDE_REVIEWED_SCOPE")

// ScopeStatus is this profile's own verdict on a resolution attempt.
type ScopeStatus string

// Scope statuses. UnknownScope and HumanReviewRequired are exactly the two
// wire tokens GREEN names; InScope is this package's addition for a
// resolution that succeeded inside the reviewed jurisdiction.
const (
	ScopeStatusInScope             ScopeStatus = "IN_SCOPE"
	ScopeStatusUnknown             ScopeStatus = UncertaintyUnknown
	ScopeStatusHumanReviewRequired ScopeStatus = UncertaintyHumanReviewRequired
)

// Authorize resolves input through legal.Resolve - the platform's one
// jurisdiction-resolution authority - and then checks the resolved
// jurisdiction against this profile's own pinned, reviewed scope. It never
// reimplements jurisdiction resolution and it never guesses past what
// Resolve and this profile's own Jurisdiction cover:
//
//   - an ambiguous, contradictory or incomplete input fails exactly as
//     Resolve fails, wrapped in legal.ErrLegalContextUnknown; Authorize maps
//     that to the status this profile's own Uncertainty.AmbiguousInputStatus
//     declares (HUMAN_REVIEW_REQUIRED on the checked-in profile).
//   - a resolution that succeeds but lands on a jurisdiction other than the
//     one this profile pins (Resolve found real, registered coverage for
//     some OTHER jurisdiction, e.g. New York, because the caller shares one
//     registry across jurisdictions) is not this profile's to evaluate:
//     Authorize returns the status Uncertainty.OutOfScopeStatus declares
//     (UNKNOWN on the checked-in profile) wrapping [ErrOutsideReviewedScope],
//     rather than silently applying this profile's California obligations to
//     a New York transaction.
//   - a resolution that succeeds and lands inside this profile's pinned
//     jurisdiction is ScopeStatusInScope with the resolved *legal.LegalContext.
func (p JurisdictionProfile) Authorize(input legal.LegalContextInput, registry *legal.Registry, signer *legal.Signer, now values.Instant) (*legal.LegalContext, ScopeStatus, error) {
	ctx, err := legal.Resolve(input, registry, signer, now)
	if err != nil {
		status := ScopeStatus(p.Uncertainty.AmbiguousInputStatus)
		if status == "" {
			status = ScopeStatusHumanReviewRequired
		}
		return nil, status, err
	}

	pinned := p.Jurisdiction.ToLegal()
	if err := pinned.Validate(); err != nil {
		return nil, ScopeStatusHumanReviewRequired, fmt.Errorf("pilotjurisdiction: profile pins an invalid jurisdiction: %w", err)
	}
	if ctx.Jurisdiction() != pinned {
		status := ScopeStatus(p.Uncertainty.OutOfScopeStatus)
		if status == "" {
			status = ScopeStatusUnknown
		}
		return nil, status, fmt.Errorf("%w: resolved %s, this profile only reviewed %s", ErrOutsideReviewedScope, ctx.Jurisdiction(), pinned)
	}
	return ctx, ScopeStatusInScope, nil
}
