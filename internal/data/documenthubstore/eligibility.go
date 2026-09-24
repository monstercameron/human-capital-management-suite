// Live audience eligibility for team and channel document paths (HUB-013).
// A placement path (a document deployed under scope_kind='placement') must
// recheck live team or channel membership on every resolution: a departed
// member loses the official view immediately, never on a cache's schedule.
// Eligibility authority itself belongs to chat (membership, channel
// policy); this package only declares the port it consumes, so
// documenthubstore never imports a chat package. A direct document grant
// (an owner's personal share) never substitutes for live eligibility, so it
// can never widen who sees an official placement.
package documenthubstore

import (
	"context"
	"errors"
)

// ErrIneligibleAudience is returned when the live team or channel
// authority does not currently admit the subject to the placement scope.
// It is returned whether or not a placement exists there, so a probe can
// never distinguish "not eligible" from "nothing placed."
var ErrIneligibleAudience = errors.New("document placement: audience is not currently eligible")

// AudienceEligibility is the port a live team or channel authority
// implements so documenthubstore can recheck placement audience without
// depending on chat's membership or policy packages.
type AudienceEligibility interface {
	// Eligible reports whether subjectID currently belongs to the audience
	// of scopeID under scopeKind (e.g. a channel's current membership).
	Eligible(ctx context.Context, tenantID, scopeKind, scopeID, subjectID string) (bool, error)
}

// ResolvePlacementDeployment resolves the active deployment for one team or
// channel placement scope, but only after the live audience authority
// admits the subject. A stale or revoked membership refuses before the
// deployment (and so the Markdown or attachments it names) is ever
// resolved.
func (s *Store) ResolvePlacementDeployment(ctx context.Context, tenantID, docID, scopeKind, scopeID, subjectID string, aud AudienceEligibility) (Deployment, error) {
	if aud == nil {
		return Deployment{}, errors.New("document placement: audience authority is required")
	}
	if scopeKind != "placement" || scopeID == "" || subjectID == "" {
		return Deployment{}, ErrRouteInvalid
	}
	eligible, err := aud.Eligible(ctx, tenantID, scopeKind, scopeID, subjectID)
	if err != nil {
		return Deployment{}, err
	}
	if !eligible {
		return Deployment{}, ErrIneligibleAudience
	}
	return s.ResolveDeployment(ctx, tenantID, docID, scopeKind, scopeID)
}
