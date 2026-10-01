package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaMentionAuthoritySources = errors.New("application: persona mention authority sources unavailable")

// PersonaMentionAuthoritySourcesConfig supplies current production authority
// sources. Audience must read current chat memberships and tenant directory
// facts; Skills, Gate, Catalog, and Discovery must use the same current skill
// registry and policy view.
type PersonaMentionAuthoritySourcesConfig struct {
	Audience    PersonaAudienceSource
	Personas    PersonaAuthorityInstallationStore
	Skills      AgentSkillDiscoverer
	Gate        *agentgate.Gate
	Catalog     agentgate.SkillCatalog
	Discovery   AgentDiscoveryContext
	Purpose     string
	PrivateChat agentgate.PrivateChatScopeAuthorizer
	ChatScope   agentgate.PersonaChatScopeAuthorizer
	Now         func() time.Time
}

// PersonaMentionAuthoritySources contains the current identity and grant
// authority projections used by PersonaAuthorityAdapter.
type PersonaMentionAuthoritySources struct {
	Personas PersonaAuthoritySource
	Identity *ServerTrustedInvokerSource
	Invokers *TrustedSkillInvokerAuthoritySource
}

// NewPersonaMentionAuthoritySources binds production audience, skill
// discovery, and policy projection into per-invoker authorities. It does not
// infer scopes from persona labels, message content, or request claims.
func NewPersonaMentionAuthoritySources(cfg PersonaMentionAuthoritySourcesConfig) (*PersonaMentionAuthoritySources, error) {
	if cfg.Audience == nil || cfg.Personas == nil || cfg.Skills == nil || cfg.Gate == nil || cfg.Catalog == nil || cfg.Discovery == nil || strings.TrimSpace(cfg.Purpose) == "" {
		return nil, errPersonaMentionAuthoritySources
	}
	scopeResolver, err := NewPersonaAuthorityScopeResolver(cfg.Catalog)
	if err != nil {
		return nil, fmt.Errorf("%w: persona scope resolver: %v", errPersonaMentionAuthoritySources, err)
	}
	personas := &DatabasePersonaAuthoritySource{Installations: cfg.Personas, Audience: cfg.Audience, Scopes: personaMentionChannelPolicyScopes{next: scopeResolver}}
	projection, err := NewGateInvokerAuthoritySource(GateInvokerAuthoritySourceConfig{
		Gate: cfg.Gate, Skills: cfg.Catalog, Current: cfg.Discovery, Purpose: cfg.Purpose, PrivateChat: cfg.PrivateChat, ChatScope: cfg.ChatScope,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: policy projection: %v", errPersonaMentionAuthoritySources, err)
	}
	if (!isNilPersonaOutputPort(cfg.PrivateChat) || !isNilPersonaOutputPort(cfg.ChatScope)) && cfg.Now == nil {
		return nil, errPersonaMentionAuthoritySources
	}
	identity, err := NewServerTrustedInvokerSource(TrustedInvokerSourceConfig{
		Membership: currentPersonaMentionMembership{audience: cfg.Audience},
		Skills:     currentPersonaMentionSkills{skills: cfg.Skills, projection: projection, now: cfg.Now},
		Purpose:    cfg.Purpose,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: identity projection: %v", errPersonaMentionAuthoritySources, err)
	}
	invokers, err := NewTrustedSkillInvokerAuthoritySource(identity, projection)
	if err != nil {
		return nil, fmt.Errorf("%w: per-skill projection: %v", errPersonaMentionAuthoritySources, err)
	}
	return &PersonaMentionAuthoritySources{Personas: personas, Identity: identity, Invokers: invokers}, nil
}

// NewPersonaMentionAuthoritySourcesFromAgentSkills reuses the exact skill
// registry, grant provider, gate, and discovery context already composed for
// the tenant's persona catalog.
func NewPersonaMentionAuthoritySourcesFromAgentSkills(audience PersonaAudienceSource, personas PersonaAuthorityInstallationStore, skills *AgentSkillSource, purpose string) (*PersonaMentionAuthoritySources, error) {
	if skills == nil || skills.skills == nil || skills.gate == nil || skills.current == nil {
		return nil, errPersonaMentionAuthoritySources
	}
	return NewPersonaMentionAuthoritySources(PersonaMentionAuthoritySourcesConfig{
		Audience: audience, Personas: personas, Skills: skills, Gate: skills.gate,
		Catalog: skills.skills, Discovery: skills.current, Purpose: purpose,
	})
}

type personaMentionChannelPolicyScopes struct{ next PersonaAuthorityScopeSource }

func (s personaMentionChannelPolicyScopes) ResolvePersonaScopes(ctx context.Context, tenant values.TenantId, profile agentpersona.PersonaProfile, installation agentpersonastore.ActiveInstallation, policy agentpersonastore.ChannelPolicy) (agentinvoke.SkillScopes, agentinvoke.SkillScopes, agentinvoke.SkillScopes, error) {
	class := installation.ConversationClass
	if s.next == nil || !slices.Contains(policy.AllowedChannelClasses, class) || !personaAllowsChannelClass(profile, class) || !personaAllowsPlacement(profile, class, policy.PlacementClass) {
		return nil, nil, nil, errPersonaMentionAuthoritySources
	}
	switch class {
	case agentpersonastore.ConversationExternal:
		if !policy.AllowExternalMembers {
			return nil, nil, nil, errPersonaMentionAuthoritySources
		}
	case agentpersonastore.ConversationCrossCompany:
		// The persona channel-class contract has no distinct cross-company
		// value, so a private/public label cannot stand in for bilateral scope.
		return nil, nil, nil, errPersonaMentionAuthoritySources
	}
	return s.next.ResolvePersonaScopes(ctx, tenant, profile, installation, policy)
}

func personaAllowsChannelClass(profile agentpersona.PersonaProfile, class agentpersonastore.ConversationClass) bool {
	switch class {
	case agentpersonastore.ConversationPublic:
		return slices.Contains(profile.ChannelClasses, agentpersona.ChannelPublic)
	case agentpersonastore.ConversationExternal:
		return slices.Contains(profile.ChannelClasses, agentpersona.ChannelExternal)
	case agentpersonastore.ConversationPrivate, agentpersonastore.ConversationGroupDM, agentpersonastore.ConversationOneToOne:
		return slices.Contains(profile.ChannelClasses, agentpersona.ChannelPrivate)
	default:
		return false
	}
}

// AgentPersonaAuthorityStore adapts the durable tenant persona store to the
// current mention authority projection.
type AgentPersonaAuthorityStore struct{ Store *agentpersonastore.Store }

// ForTenant creates a persona authority reader scoped to one tenant.
func (s AgentPersonaAuthorityStore) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaAuthorityInstallationReader, error) {
	if s.Store == nil || ctx == nil || tenant.Validate() != nil {
		return nil, errPersonaMentionAuthoritySources
	}
	scoped, err := s.Store.Scoped(tenant)
	if err != nil {
		return nil, fmt.Errorf("scope persona authority store: %w", err)
	}
	return scoped, nil
}

type currentPersonaMentionMembership struct{ audience PersonaAudienceSource }

func (s currentPersonaMentionMembership) ResolveCurrentMembership(ctx context.Context, tenantID, subjectID string) (bool, error) {
	if s.audience == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(subjectID) == "" {
		return false, errPersonaMentionAuthoritySources
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != tenantID || principal.Subject() != subjectID {
		return false, errPersonaMentionAuthoritySources
	}
	conversations, err := s.audience.ListCurrentPersonaAudience(ctx, tenantID, subjectID)
	if err != nil {
		return false, fmt.Errorf("%w: read current audience membership: %v", errPersonaMentionAuthoritySources, err)
	}
	seen := make(map[string]struct{}, len(conversations))
	member := false
	for _, conversation := range conversations {
		if conversation.TenantID != tenantID || strings.TrimSpace(conversation.ConversationID) == "" {
			return false, errPersonaMentionAuthoritySources
		}
		if _, duplicate := seen[conversation.ConversationID]; duplicate {
			return false, errPersonaMentionAuthoritySources
		}
		seen[conversation.ConversationID] = struct{}{}
		matching := 0
		for _, candidate := range conversation.Members {
			if candidate.SubjectID != subjectID {
				continue
			}
			matching++
			if !nonemptyFacts(candidate.Roles) || !nonemptyFacts(candidate.Populations) || strings.TrimSpace(candidate.OrganizationScope) == "" {
				return false, errPersonaMentionAuthoritySources
			}
		}
		if matching > 1 {
			return false, errPersonaMentionAuthoritySources
		}
		member = member || matching == 1
	}
	return member, nil
}

type currentPersonaMentionSkills struct {
	skills     AgentSkillDiscoverer
	projection InvokerAuthoritySource
	now        func() time.Time
}

func (s currentPersonaMentionSkills) ResolveInvokerSkills(ctx context.Context, principal *trust.Principal, purpose string) (agentinvoke.SkillScopes, error) {
	if ctx == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || strings.TrimSpace(purpose) == "" {
		return nil, errPersonaMentionAuthoritySources
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() {
		return nil, errPersonaMentionAuthoritySources
	}
	if _, bound := ctx.Value(personaChatAuthorityTupleKey{}).(PersonaChatAuthorityRequest); bound && !isNilPersonaOutputPort(s.projection) && s.now != nil {
		current, err := s.projection.ResolveInvokerAuthority(ctx, principal.Subject(), principal.Tenant(), purpose, s.now().UTC())
		if err != nil || !current.Active || current.UserID != principal.Subject() || current.Authority.Tenant != principal.Tenant() {
			return nil, errPersonaMentionAuthoritySources
		}
		result := agentinvoke.SkillScopes{}
		for skill, authority := range current.SkillAuthorities {
			result[skill] = slices.Clone(authority.Capabilities)
		}
		if len(result) == 0 {
			return nil, errPersonaMentionAuthoritySources
		}
		return result, nil
	}
	if s.skills == nil {
		return nil, errPersonaMentionAuthoritySources
	}
	records, err := s.skills.Discover(ctx, principal, purpose)
	if err != nil {
		return nil, fmt.Errorf("%w: discover current skills: %v", errPersonaMentionAuthoritySources, err)
	}
	result := make(agentinvoke.SkillScopes, len(records))
	for _, record := range records {
		if record.Status != agentskills.StatusActive || strings.TrimSpace(record.Definition.ID) == "" {
			return nil, errPersonaMentionAuthoritySources
		}
		if _, duplicate := result[record.Definition.ID]; duplicate {
			return nil, errPersonaMentionAuthoritySources
		}
		scopes, err := exactCapabilityScopes(record)
		if err != nil {
			return nil, fmt.Errorf("%w: resolve exact scope for %s: %v", errPersonaMentionAuthoritySources, record.Definition.Key(), err)
		}
		result[record.Definition.ID] = scopes
	}
	if len(result) == 0 {
		return nil, errPersonaMentionAuthoritySources
	}
	return result, nil
}

var _ PersonaCurrentMembershipSource = currentPersonaMentionMembership{}
var _ PersonaInvokerSkillSource = currentPersonaMentionSkills{}
var _ PersonaAuthorityInstallationStore = AgentPersonaAuthorityStore{}
