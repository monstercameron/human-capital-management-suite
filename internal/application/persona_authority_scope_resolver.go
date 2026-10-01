package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaAuthorityScopeResolver = errors.New("application: persona authority scope resolution denied")

// PersonaAuthorityScopeResolver derives exact authority scopes from immutable
// persona pins and the current skill catalog. It never treats a data class,
// tier, or missing scope reference as an authorization grant.
type PersonaAuthorityScopeResolver struct {
	Catalog agentgate.SkillCatalog
}

// NewPersonaAuthorityScopeResolver constructs a fail-closed scope resolver.
func NewPersonaAuthorityScopeResolver(catalog agentgate.SkillCatalog) (*PersonaAuthorityScopeResolver, error) {
	if catalog == nil {
		return nil, fmt.Errorf("%w: current skill catalog is required", errPersonaAuthorityScopeResolver)
	}
	return &PersonaAuthorityScopeResolver{Catalog: catalog}, nil
}

// ResolvePersonaScopes implements PersonaAuthorityScopeSource using only
// exact catalog pins and the installation's durable channel policy.
func (r *PersonaAuthorityScopeResolver) ResolvePersonaScopes(ctx context.Context, tenant values.TenantId, profile agentpersona.PersonaProfile, installation agentpersonastore.ActiveInstallation, policy agentpersonastore.ChannelPolicy) (agentinvoke.SkillScopes, agentinvoke.SkillScopes, agentinvoke.SkillScopes, error) {
	if r == nil || r.Catalog == nil || ctx == nil || strings.TrimSpace(string(tenant)) == "" {
		return nil, nil, nil, errPersonaAuthorityScopeResolver
	}
	if err := tenant.Validate(); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: tenant: %v", errPersonaAuthorityScopeResolver, err)
	}
	if strings.TrimSpace(profile.PersonaID) == "" || profile.Version == 0 || len(profile.SkillPins) == 0 || installation.PersonaID != profile.PersonaID || installation.PersonaVersion != int64(profile.Version) || strings.TrimSpace(installation.InstallationID) == "" || strings.TrimSpace(installation.ConversationID) == "" {
		return nil, nil, nil, fmt.Errorf("%w: persona and installation binding is incomplete", errPersonaAuthorityScopeResolver)
	}
	maxTier, err := parsePersonaAuthorityTier(policy.MaxTier)
	if err != nil || policy.AllowedDataClasses == nil {
		return nil, nil, nil, fmt.Errorf("%w: invalid channel policy", errPersonaAuthorityScopeResolver)
	}
	if !personaAllowsPlacement(profile, installation.ConversationClass, policy.PlacementClass) {
		return nil, nil, nil, errPersonaAuthorityScopeResolver
	}
	kind := agentpersona.ConversationChannel
	if installation.ConversationClass == agentpersonastore.ConversationOneToOne {
		kind = agentpersona.ConversationDirect
	}
	if installation.ConversationClass == agentpersonastore.ConversationGroupDM {
		kind = agentpersona.ConversationGroup
	}
	profileTier := profile.TierForConversation(kind)
	allowed := make(map[string]struct{}, len(policy.AllowedDataClasses))
	for _, class := range policy.AllowedDataClasses {
		if strings.TrimSpace(class) == "" || strings.TrimSpace(class) != class {
			return nil, nil, nil, fmt.Errorf("%w: invalid data-class policy", errPersonaAuthorityScopeResolver)
		}
		allowed[class] = struct{}{}
	}

	personaScopes := make(agentinvoke.SkillScopes, len(profile.SkillPins))
	for _, pin := range profile.SkillPins {
		record, err := r.resolveCurrentPin(pin)
		if err != nil {
			return nil, nil, nil, err
		}
		tier := record.Definition.SideEffectTier
		if record.HighestCapabilityTier > tier {
			tier = record.HighestCapabilityTier
		}
		if tier > profileTier || tier > maxTier || !dataClassesAllowed(record, allowed) {
			continue
		}
		scopes, err := exactCapabilityScopes(record)
		if err != nil {
			return nil, nil, nil, err
		}
		personaScopes[pin.ID] = scopes
	}
	if len(personaScopes) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: policy narrowed every pinned skill", errPersonaAuthorityScopeResolver)
	}
	return personaScopes.Clone(), personaScopes.Clone(), personaScopes.Clone(), nil
}

func (r *PersonaAuthorityScopeResolver) resolveCurrentPin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if strings.TrimSpace(pin.ID) == "" || pin.Version == 0 || strings.TrimSpace(pin.Digest) == "" {
		return agentskills.SkillRecord{}, fmt.Errorf("%w: incomplete skill pin", errPersonaAuthorityScopeResolver)
	}
	record, err := r.Catalog.ResolvePin(pin)
	if err != nil {
		return agentskills.SkillRecord{}, fmt.Errorf("%w: resolve skill pin: %v", errPersonaAuthorityScopeResolver, err)
	}
	if record.Definition.ID != pin.ID || record.Definition.Version != pin.Version || record.Digest != pin.Digest || record.Status == agentskills.StatusRetired {
		return agentskills.SkillRecord{}, fmt.Errorf("%w: catalog returned a forged or stale pin", errPersonaAuthorityScopeResolver)
	}
	found := 0
	for _, current := range r.Catalog.List() {
		if current.Definition.ID == pin.ID && current.Definition.Version == pin.Version && current.Digest == pin.Digest && current.Status == record.Status {
			found++
		}
	}
	if found != 1 {
		return agentskills.SkillRecord{}, fmt.Errorf("%w: pin is absent from the current catalog", errPersonaAuthorityScopeResolver)
	}
	return record, nil
}

func exactCapabilityScopes(record agentskills.SkillRecord) ([]string, error) {
	if len(record.ResolvedOperations) == 0 {
		return nil, fmt.Errorf("%w: skill %s has no exact capability scope", errPersonaAuthorityScopeResolver, record.Definition.Key())
	}
	seen := make(map[string]struct{})
	for _, operation := range record.ResolvedOperations {
		if !operation.HasCapability || strings.TrimSpace(operation.Capability.Definition.AuthZScopeRef) == "" {
			return nil, fmt.Errorf("%w: skill %s has an unresolved capability scope", errPersonaAuthorityScopeResolver, record.Definition.Key())
		}
		seen[operation.Capability.Definition.AuthZScopeRef] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("%w: skill %s has no exact capability scope", errPersonaAuthorityScopeResolver, record.Definition.Key())
	}
	out := make([]string, 0, len(seen))
	for scope := range seen {
		out = append(out, scope)
	}
	return out, nil
}

func dataClassesAllowed(record agentskills.SkillRecord, allowed map[string]struct{}) bool {
	for _, class := range append(append([]string{}, record.Definition.DataClassesRead...), record.Definition.DataClassesWritten...) {
		if _, ok := allowed[class]; !ok {
			return false
		}
	}
	return true
}

func parsePersonaAuthorityTier(raw string) (agentskills.SideEffectTier, error) {
	switch strings.TrimSpace(raw) {
	case "T0":
		return agentskills.TierT0, nil
	case "T1":
		return agentskills.TierT1, nil
	case "T2":
		return agentskills.TierT2, nil
	case "T3":
		return agentskills.TierT3, nil
	case "T4":
		return agentskills.TierT4, nil
	default:
		return 0, fmt.Errorf("unknown tier %q", raw)
	}
}

var _ PersonaAuthorityScopeSource = (*PersonaAuthorityScopeResolver)(nil)
