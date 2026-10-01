// WTIME-001/002 (application-service half): resolve the one TimeProfile
// that applies to an assignment as of an instant, and pin that resolution
// for the lifetime of one session or period run. The pure resolution rule
// lives in internal/domains/timeprofile (Resolve); this file adds
// authorization, the current published rule set, and idempotent pinning
// through ProfileStore so an open run's pin is never silently repinned.
package timecardservice

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ResolveTimeProfileRequest carries the assignment facts to resolve against
// the tenant's currently published eligibility rules.
type ResolveTimeProfileRequest struct {
	WorkerRef string
	Facts     timeprofile.AssignmentFacts
	At        time.Time
}

// ResolveTimeProfile reads the tenant's published eligibility rules and
// resolves the one profile that applies. It never pins: a caller that needs
// a stable pin for an open run calls AssignTimeProfile.
func (s Service) ResolveTimeProfile(ctx context.Context, p *trust.Principal, req ResolveTimeProfileRequest) (timeprofile.TimeProfile, error) {
	if err := validPrincipal(p); err != nil {
		return timeprofile.TimeProfile{}, err
	}
	if s.Profiles == nil {
		return timeprofile.TimeProfile{}, ErrUnavailable
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapAssignProfile); err != nil {
		return timeprofile.TimeProfile{}, err
	}
	rules, err := s.Profiles.Rules(ctx, tenant)
	if err != nil {
		return timeprofile.TimeProfile{}, err
	}
	return timeprofile.Resolve(rules, req.Facts, values.NewInstant(req.At))
}

// AssignTimeProfileRequest pins the resolved profile to one assignment.
// ExpectedRevision is 0 for a first pin and the previously observed pin
// revision for a repin; a mismatch is ErrRevisionConflict, which is what
// stops a repin from silently replacing a pin an open run still relies on --
// a profile change only ever applies from the next session or period.
type AssignTimeProfileRequest struct {
	WorkerRef        string
	AssignmentRef    string
	Facts            timeprofile.AssignmentFacts
	At               time.Time
	ExpectedRevision int64
}

func (s Service) AssignTimeProfile(ctx context.Context, p *trust.Principal, req AssignTimeProfileRequest) (ProfilePin, error) {
	if err := validPrincipal(p); err != nil {
		return ProfilePin{}, err
	}
	if s.Profiles == nil {
		return ProfilePin{}, ErrUnavailable
	}
	if strings.TrimSpace(req.AssignmentRef) == "" {
		return ProfilePin{}, ErrInvalidRequest
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapAssignProfile); err != nil {
		return ProfilePin{}, err
	}
	existing, found, err := s.Profiles.PinFor(ctx, tenant, req.AssignmentRef)
	if err != nil {
		return ProfilePin{}, err
	}
	if found && existing.Revision != req.ExpectedRevision {
		return ProfilePin{}, ErrRevisionConflict
	}
	if !found && req.ExpectedRevision != 0 {
		return ProfilePin{}, ErrRevisionConflict
	}
	rules, err := s.Profiles.Rules(ctx, tenant)
	if err != nil {
		return ProfilePin{}, err
	}
	profile, err := timeprofile.Resolve(rules, req.Facts, values.NewInstant(req.At))
	if err != nil {
		return ProfilePin{}, err
	}
	return s.Profiles.Pin(ctx, tenant, req.AssignmentRef, profile, req.ExpectedRevision, s.now())
}
