package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

var (
	// ErrPersonaT0Policy is returned when a persona run cannot be proven to be
	// within the read-only boundary.
	ErrPersonaT0Policy = errors.New("application: persona T0 policy denied")
	// ErrPersonaT0Context is returned by the legacy policy seam because it has
	// no trusted invocation identity with which to perform a catalog check.
	ErrPersonaT0Context = errors.New("application: persona T0 invocation context is required")
)

// PersonaT0SkillCatalog is the server-owned, versioned skill catalog used by
// persona invocation. Registry implementations must resolve an exact pin,
// including its content digest, rather than accepting a caller's tier claim.
type PersonaT0SkillCatalog interface {
	ResolvePin(agentskills.SkillPin) (agentskills.SkillRecord, error)
}

// PersonaT0RevocationChecker checks the current fence for a bound invocation.
// It must consult durable current state on every call; a cached admission is
// not sufficient at the post-commit/run boundary.
type PersonaT0RevocationChecker interface {
	IsRevoked(context.Context, PersonaT0Invocation) (bool, error)
}

// PersonaT0Invocation is the server-owned identity bound to a run. All
// identity fields are required so a check cannot accidentally cross tenants,
// persona versions, installations, or invocation records.
type PersonaT0Invocation struct {
	TenantID       string
	PersonaID      string
	PersonaVersion string
	InstallationID string
	InvocationID   string
}

// PersonaT0SkillPin is the trusted catalog binding selected before the run.
// The caller supplies a pin from the server-owned persona manifest; the
// policy still resolves it and never trusts the supplied tier or digest alone.
type PersonaT0SkillPin struct {
	Invocation PersonaT0Invocation
	Pin        agentskills.SkillPin
	Scopes     []string
}

// PersonaT0SkillPolicy performs the bound catalog check for read-only skills.
// It also implements the current persona chat policy interface, but that
// interface is deliberately fail closed because it lacks binding context.
type PersonaT0SkillPolicy struct {
	catalog  PersonaT0SkillCatalog
	revoked  PersonaT0RevocationChecker
	bindings map[string]PersonaT0SkillPin
}

// NewPersonaT0SkillPolicy creates a policy over the trusted catalog and
// invocation revocation source. Bindings are copied so callers cannot mutate
// the policy after composition.
func NewPersonaT0SkillPolicy(catalog PersonaT0SkillCatalog, revoked PersonaT0RevocationChecker, bindings []PersonaT0SkillPin) (*PersonaT0SkillPolicy, error) {
	if catalog == nil || revoked == nil {
		return nil, fmt.Errorf("%w: catalog and revocation checker are required", ErrPersonaT0Policy)
	}
	indexed := make(map[string]PersonaT0SkillPin, len(bindings))
	for _, binding := range bindings {
		if err := validatePersonaT0Binding(binding); err != nil {
			return nil, err
		}
		key := personaT0BindingKey(binding.Invocation, binding.Pin)
		if _, exists := indexed[key]; exists {
			return nil, fmt.Errorf("%w: duplicate binding %s", ErrPersonaT0Policy, key)
		}
		binding.Scopes = normalizePersonaT0Scopes(binding.Scopes)
		indexed[key] = binding
	}
	return &PersonaT0SkillPolicy{catalog: catalog, revoked: revoked, bindings: indexed}, nil
}

// IsBoundT0Run proves every skill in a run against the exact trusted binding
// for that invocation. Since RunRequest carries skill names and scopes (not
// caller-provided pins), an invocation with zero or multiple trusted versions
// for one name fails closed.
func (p *PersonaT0SkillPolicy) IsBoundT0Run(ctx context.Context, request agentinvoke.RunRequest) (bool, error) {
	if p == nil || request.Mode != agentinvoke.OnBehalfOf || len(request.Skills) == 0 {
		return false, ErrPersonaT0Policy
	}
	invocation := PersonaT0Invocation{TenantID: request.TenantID, PersonaID: request.PersonaID, PersonaVersion: request.PersonaVersion, InstallationID: request.InstallationID, InvocationID: request.InvocationID}
	if err := validatePersonaT0Invocation(invocation); err != nil {
		return false, err
	}
	for skill, scopes := range request.Skills {
		var pin agentskills.SkillPin
		matches := 0
		for _, binding := range p.bindings {
			if binding.Invocation == invocation && binding.Pin.ID == skill {
				pin = binding.Pin
				matches++
			}
		}
		if matches != 1 {
			return false, fmt.Errorf("%w: skill %q has no unique trusted version", ErrPersonaT0Policy, skill)
		}
		if ok, err := p.IsBoundT0Skill(ctx, invocation, pin, scopes); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// IsT0Skill is retained as a fail-closed compatibility helper. The old
// signature cannot identify a tenant, persona version, installation,
// invocation, or exact catalog pin, so it cannot authorize a run.
func (p *PersonaT0SkillPolicy) IsT0Skill(context.Context, string, []string) (bool, error) {
	return false, ErrPersonaT0Context
}

// IsBoundT0Skill proves that one exact, trusted skill binding is currently
// active and read-only for the supplied invocation and exact scope set.
func (p *PersonaT0SkillPolicy) IsBoundT0Skill(ctx context.Context, invocation PersonaT0Invocation, pin agentskills.SkillPin, scopes []string) (bool, error) {
	if p == nil || p.catalog == nil || p.revoked == nil {
		return false, ErrPersonaT0Policy
	}
	if err := validatePersonaT0Invocation(invocation); err != nil {
		return false, err
	}
	if err := validatePersonaT0Pin(pin); err != nil {
		return false, err
	}
	requested := normalizePersonaT0Scopes(scopes)
	if len(requested) == 0 {
		return false, fmt.Errorf("%w: scopes are required", ErrPersonaT0Policy)
	}
	binding, ok := p.bindings[personaT0BindingKey(invocation, pin)]
	if !ok || !slices.Equal(requested, normalizePersonaT0Scopes(binding.Scopes)) {
		return false, fmt.Errorf("%w: skill binding is not exact", ErrPersonaT0Policy)
	}
	record, err := p.catalog.ResolvePin(pin)
	if err != nil {
		return false, fmt.Errorf("%w: resolve skill pin: %v", ErrPersonaT0Policy, err)
	}
	if record.Status != agentskills.StatusActive || record.Definition.ID != pin.ID || record.Definition.Version != pin.Version || record.Digest != pin.Digest || record.Definition.SideEffectTier != agentskills.TierT0 || record.HighestCapabilityTier > agentskills.TierT0 {
		return false, fmt.Errorf("%w: skill %s is not an active exact T0 catalog entry", ErrPersonaT0Policy, pin.Key())
	}
	revoked, err := p.revoked.IsRevoked(ctx, invocation)
	if err != nil {
		return false, fmt.Errorf("%w: revocation check: %v", ErrPersonaT0Policy, err)
	}
	if revoked {
		return false, fmt.Errorf("%w: invocation is revoked", ErrPersonaT0Policy)
	}
	return true, nil
}

func validatePersonaT0Binding(binding PersonaT0SkillPin) error {
	if err := validatePersonaT0Invocation(binding.Invocation); err != nil {
		return err
	}
	if err := validatePersonaT0Pin(binding.Pin); err != nil {
		return err
	}
	if len(normalizePersonaT0Scopes(binding.Scopes)) == 0 {
		return fmt.Errorf("%w: binding scopes are required", ErrPersonaT0Policy)
	}
	return nil
}

func validatePersonaT0Invocation(invocation PersonaT0Invocation) error {
	for name, value := range map[string]string{"tenant": invocation.TenantID, "persona": invocation.PersonaID, "persona_version": invocation.PersonaVersion, "installation": invocation.InstallationID, "invocation": invocation.InvocationID} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s binding is required", ErrPersonaT0Policy, name)
		}
	}
	return nil
}

func validatePersonaT0Pin(pin agentskills.SkillPin) error {
	if strings.TrimSpace(pin.ID) == "" || pin.Version == 0 || strings.TrimSpace(pin.Digest) == "" {
		return fmt.Errorf("%w: exact skill pin is required", ErrPersonaT0Policy)
	}
	return nil
}

func normalizePersonaT0Scopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; !ok {
			seen[scope] = struct{}{}
			out = append(out, scope)
		}
	}
	slices.Sort(out)
	return out
}

func personaT0BindingKey(invocation PersonaT0Invocation, pin agentskills.SkillPin) string {
	return strings.Join([]string{invocation.TenantID, invocation.PersonaID, invocation.PersonaVersion, invocation.InstallationID, invocation.InvocationID, pin.ID, fmt.Sprint(pin.Version), pin.Digest}, "\x00")
}
