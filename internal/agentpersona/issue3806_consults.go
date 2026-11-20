package agentpersona

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
)

// MaxConsults is the longest reviewed list of specialists one persona version
// may bring into a thread. It is deliberately tiny: a consult is a pinned,
// reviewed edge between two published personas, never free discovery.
const MaxConsults = 2

// MaxConsultDepth is how many levels of consult a run may reach. A consulted
// specialist never consults again, so recruitment cannot loop or fan out.
const MaxConsultDepth = 1

// ErrConsultDenied is returned when a run tries to consult an agent that its
// persona version does not list, or tries to consult below the depth ceiling.
var ErrConsultDenied = errors.New("agentpersona: consult denied")

// ConsultRef pins one specialist persona version a persona may consult. The
// digest is the sealed profile digest the reviewer saw at publish time.
type ConsultRef struct {
	PersonaID string `json:"persona_id"`
	Version   uint32 `json:"version"`
	Digest    string `json:"digest"`
}

// ConsultTarget is what the publish gate and the runtime need to know about a
// listed specialist: its sealed version and where it is in its lifecycle.
type ConsultTarget struct {
	Version PersonaVersion
	State   LifecycleState
}

// ConsultResolver loads the exact persona version a ConsultRef pins.
type ConsultResolver interface {
	ResolveConsult(ConsultRef) (ConsultTarget, error)
}

// ResolveConsult returns the pinned version and its lifecycle state. It
// reports a state of PUBLISHED only for the current, published pointer.
func (m *Manager) ResolveConsult(ref ConsultRef) (ConsultTarget, error) {
	if m == nil {
		return ConsultTarget{}, ErrNotFound
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	stored, state, err := m.lookupLocked(ref.PersonaID, ref.Version)
	if err != nil {
		return ConsultTarget{}, err
	}
	current := LifecycleState(StateDraft)
	if stored.published && state.current == ref.Version {
		current = state.state
	} else if stored.published {
		current = StateRetired
	}
	return ConsultTarget{Version: cloneVersion(stored.version), State: current}, nil
}

// validateConsultShape checks the declared list without resolving it.
func validateConsultShape(p PersonaProfile) error {
	if len(p.Consults) > MaxConsults {
		return fmt.Errorf("%w: a persona may consult at most %d specialists", ErrInvalidProfile, MaxConsults)
	}
	seen := map[string]struct{}{}
	for _, ref := range p.Consults {
		if strings.TrimSpace(ref.PersonaID) == "" || ref.Version == 0 || strings.TrimSpace(ref.Digest) == "" {
			return fmt.Errorf("%w: a consult must pin a persona, version and digest", ErrInvalidProfile)
		}
		if ref.PersonaID == p.PersonaID {
			return fmt.Errorf("%w: a persona may not consult itself", ErrInvalidProfile)
		}
		if _, dup := seen[ref.PersonaID]; dup {
			return fmt.Errorf("%w: duplicate consult %s", ErrInvalidProfile, ref.PersonaID)
		}
		seen[ref.PersonaID] = struct{}{}
	}
	return nil
}

// validateConsults is the publish-time gate: every listed specialist must be
// the exact published version, must serve everyone who can ask the parent,
// and must not consult anyone itself.
func (v Validator) validateConsults(p PersonaProfile) error {
	if len(p.Consults) == 0 {
		return nil
	}
	if v.Consults == nil {
		return fmt.Errorf("%w: consult resolver is required", ErrInvalidProfile)
	}
	for _, ref := range p.Consults {
		target, err := v.Consults.ResolveConsult(ref)
		if err != nil {
			return fmt.Errorf("%w: consult %s is not available: %w", ErrInvalidProfile, ref.PersonaID, err)
		}
		if target.State != StatePublished {
			return fmt.Errorf("%w: consult %s is not published", ErrInvalidProfile, ref.PersonaID)
		}
		got := target.Version
		if got.Profile.PersonaID != ref.PersonaID || got.Profile.Version != ref.Version || got.Digest != ref.Digest || got.Verify() != nil {
			return fmt.Errorf("%w: consult %s does not match its pinned version", ErrInvalidProfile, ref.PersonaID)
		}
		if len(got.Profile.Consults) > 0 {
			return fmt.Errorf("%w: consult %s consults other agents; only one level is allowed", ErrInvalidProfile, ref.PersonaID)
		}
		if !audienceWithin(p.Audience, got.Profile.Audience) {
			return errors.Join(ErrInvalidProfile, fmt.Errorf("%w: consult %s serves a narrower audience than %s", ErrAudience, ref.PersonaID, p.PersonaID))
		}
	}
	return nil
}

// AuthorizeConsult is the runtime check before a persona run calls a
// specialist. The parent must list the exact pinned version and the run must
// be at the top level (depth 0).
func AuthorizeConsult(parent PersonaVersion, depth int, target ConsultRef) (ConsultRef, error) {
	if depth < 0 || depth >= MaxConsultDepth {
		return ConsultRef{}, fmt.Errorf("%w: a consulted agent may not consult another", ErrConsultDenied)
	}
	if err := parent.Verify(); err != nil {
		return ConsultRef{}, fmt.Errorf("%w: parent profile is not sealed", ErrConsultDenied)
	}
	for _, listed := range parent.Profile.Consults {
		if listed == target {
			return listed, nil
		}
	}
	return ConsultRef{}, fmt.Errorf("%w: agent is not on the reviewed consult list", ErrConsultDenied)
}

// AuthorizeAndResolveConsult is AuthorizeConsult plus the live check a run
// needs right before it calls the specialist: the pinned version must still be
// the published, sealed, non-consulting version whose audience still covers the
// parent's. A specialist retired or suspended after the parent was published is
// refused here, not trusted from the parent's stale list.
func AuthorizeAndResolveConsult(parent PersonaVersion, depth int, target ConsultRef, resolver ConsultResolver) (PersonaVersion, error) {
	listed, err := AuthorizeConsult(parent, depth, target)
	if err != nil {
		return PersonaVersion{}, err
	}
	if resolver == nil {
		return PersonaVersion{}, fmt.Errorf("%w: consult resolver is required", ErrConsultDenied)
	}
	resolved, err := resolver.ResolveConsult(listed)
	if err != nil || resolved.State != StatePublished {
		return PersonaVersion{}, fmt.Errorf("%w: specialist is not published", ErrConsultDenied)
	}
	got := resolved.Version
	if got.Profile.PersonaID != listed.PersonaID || got.Profile.Version != listed.Version || got.Digest != listed.Digest || got.Verify() != nil {
		return PersonaVersion{}, fmt.Errorf("%w: specialist does not match its pinned version", ErrConsultDenied)
	}
	if len(got.Profile.Consults) > 0 || !audienceWithin(parent.Profile.Audience, got.Profile.Audience) {
		return PersonaVersion{}, fmt.Errorf("%w: specialist is not allowed for this parent", ErrConsultDenied)
	}
	return got, nil
}

// ConsultBudget is how a parent run's budget is divided before consulting.
type ConsultBudget struct {
	Parent   agentbudget.Limits
	Children []agentbudget.Limits
}

// SplitConsultBudget gives each of n consults an equal share of the parent's
// budget and keeps the remainder (never less than one share) for the parent,
// which still has to write the combined answer. The shares always sum to at
// most the parent's budget, so a consult can never widen what the user's run
// may spend.
func SplitConsultBudget(parent agentbudget.Limits, n int) (ConsultBudget, error) {
	if n < 1 || n > MaxConsults {
		return ConsultBudget{}, fmt.Errorf("%w: between 1 and %d consults", ErrConsultDenied, MaxConsults)
	}
	div := int64(n + 1)
	share := agentbudget.Limits{
		Steps: parent.Steps / div, Tokens: parent.Tokens / div,
		WallClock: parent.WallClock / time.Duration(div), SpendMicros: parent.SpendMicros / div,
	}
	if share.Steps <= 0 || share.Tokens <= 0 || share.WallClock <= 0 || share.SpendMicros <= 0 {
		return ConsultBudget{}, fmt.Errorf("%w: not enough budget left to consult", ErrConsultDenied)
	}
	out := ConsultBudget{Parent: parent}
	for i := 0; i < n; i++ {
		out.Children = append(out.Children, share)
		out.Parent.Steps -= share.Steps
		out.Parent.Tokens -= share.Tokens
		out.Parent.WallClock -= share.WallClock
		out.Parent.SpendMicros -= share.SpendMicros
	}
	return out, nil
}

// CombinedAudience is the audience floor of an answer that mixes a persona's
// own work with a specialist's: only people who could see both contributions.
func CombinedAudience(parent, specialist Audience) Audience {
	return Audience{
		Roles:              intersectGrant(parent.Roles, specialist.Roles),
		Populations:        intersectGrant(parent.Populations, specialist.Populations),
		OrganizationScopes: intersectGrant(parent.OrganizationScopes, specialist.OrganizationScopes),
	}
}

// AudienceAllowsAnyone reports whether an audience still names at least one
// role, population and organization scope. An empty floor means the combined
// answer must not be shown to anyone as a consulted answer.
func AudienceAllowsAnyone(a Audience) bool {
	return len(a.Roles) > 0 && len(a.Populations) > 0 && len(a.OrganizationScopes) > 0
}

// audienceWithin reports whether every value of need is allowed by have.
func audienceWithin(need, have Audience) bool {
	return containsAllGrant(have.Roles, need.Roles) && containsAllGrant(have.Populations, need.Populations) && containsAllGrant(have.OrganizationScopes, need.OrganizationScopes)
}

func intersectGrant(a, b []string) []string {
	hasStar := func(values []string) bool {
		for _, value := range values {
			if value == "*" {
				return true
			}
		}
		return false
	}
	switch {
	case hasStar(a):
		return append([]string(nil), b...)
	case hasStar(b):
		return append([]string(nil), a...)
	}
	out := []string{}
	for _, value := range a {
		for _, other := range b {
			if value == other {
				out = append(out, value)
				break
			}
		}
	}
	return out
}
