package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

var errPersonaCurrentAuthorityProjection = errors.New("application: current persona authority projection unavailable")

// GateInvokerAuthoritySourceConfig supplies the current policy sources used
// to project an invoker's authority. Purpose is composition-owned and is
// never accepted from the invocation as an authority input.
type GateInvokerAuthoritySourceConfig struct {
	Gate        *agentgate.Gate
	Skills      agentgate.SkillCatalog
	Current     AgentDiscoveryContext
	Purpose     string
	PrivateChat agentgate.PrivateChatScopeAuthorizer
	ChatScope   agentgate.PersonaChatScopeAuthorizer
}

// GateInvokerAuthoritySource projects exact per-skill authority from the
// current gate decision and verified discovery context. Its SkillAuthorities
// map is the canonical authority; flat fields are compatibility summaries.
type GateInvokerAuthoritySource struct {
	gate      *agentgate.Gate
	skills    agentgate.SkillCatalog
	current   AgentDiscoveryContext
	purpose   string
	chat      agentgate.PrivateChatScopeAuthorizer
	chatScope agentgate.PersonaChatScopeAuthorizer
}

// CurrentInvokerAuthoritySource is the additive per-skill authority seam
// consumable wherever InvokerAuthoritySource is required.
type CurrentInvokerAuthoritySource = GateInvokerAuthoritySource

// NewGateInvokerAuthoritySource constructs a fail-closed current authority
// projector.
func NewGateInvokerAuthoritySource(cfg GateInvokerAuthoritySourceConfig) (*GateInvokerAuthoritySource, error) {
	if cfg.Gate == nil || cfg.Skills == nil || cfg.Current == nil || strings.TrimSpace(cfg.Purpose) == "" {
		return nil, errPersonaCurrentAuthorityProjection
	}
	return &GateInvokerAuthoritySource{gate: cfg.Gate, skills: cfg.Skills, current: cfg.Current, purpose: cfg.Purpose, chat: cfg.PrivateChat, chatScope: cfg.ChatScope}, nil
}

var _ InvokerAuthoritySource = (*GateInvokerAuthoritySource)(nil)

// ResolveInvokerAuthority returns only authority proven by a fresh exact
// skill projection. Denied catalog candidates are omitted; missing trusted
// identity or discovery data fails the complete projection closed.
func (s *GateInvokerAuthoritySource) ResolveInvokerAuthority(ctx context.Context, userID string, tenant values.TenantId, purpose string, at time.Time) (agentdelegation.UserAuthority, error) {
	denied := agentdelegation.UserAuthority{UserID: userID}
	if s == nil || s.gate == nil || s.skills == nil || s.current == nil || strings.TrimSpace(userID) == "" || tenant.Validate() != nil || purpose != s.purpose || at.IsZero() {
		return denied, errPersonaCurrentAuthorityProjection
	}
	principal, err := verifiedHumanPrincipal(ctx, tenant.String(), userID, s.purpose)
	if err != nil {
		return denied, err
	}
	if at.Before(principal.IssuedAt()) || !at.Before(principal.ExpiresAt()) {
		return denied, errPersonaCurrentAuthorityProjection
	}
	user, subjects, fields, err := s.current.Resolve(ctx, principal, s.purpose)
	if err != nil {
		return denied, fmt.Errorf("resolve current discovery context: %w", err)
	}
	if user.Principal == nil || user.Principal.Subject() != principal.Subject() || user.Principal.Tenant() != principal.Tenant() || user.Principal.SubjectKind() != trust.SubjectKindHuman {
		return denied, errPersonaCurrentAuthorityProjection
	}
	organization, ok := currentOrganizationScope(principal, user.OrganizationScopes)
	tuple, chatBound := ctx.Value(personaChatAuthorityTupleKey{}).(PersonaChatAuthorityRequest)
	if chatBound && (tuple.Tenant != tenant || tuple.InvokerID != userID || tuple.Purpose != purpose) {
		return denied, errPersonaCurrentAuthorityProjection
	}
	if !ok || (!chatBound && (len(subjects) == 0 || len(fields) == 0)) {
		return denied, fmt.Errorf("%w: organization scope resolved=%t subjects=%d fields=%d", errPersonaCurrentAuthorityProjection, ok, len(subjects), len(fields))
	}
	recordSubjectsCurrent := subjectsInOrganization(subjects, principal.Tenant(), organization)
	if !recordSubjectsCurrent && !chatBound {
		return denied, errPersonaCurrentAuthorityProjection
	}

	projected := make(trust.SkillAuthorities)
	for _, record := range s.skills.List() {
		if record.Status != agentskills.StatusActive {
			continue
		}
		key := record.Definition.Key()
		if chatBound && (!isNilPersonaOutputPort(s.chat) || !isNilPersonaOutputPort(s.chatScope)) && (key.ID == "persona.chat_reply" || key.ID == personaPolicyHelperSkillID || key.ID == personaWorkspaceSearchSkillID) {
			tuple.Pins = []agentskills.SkillPin{{ID: key.ID, Version: key.Version, Digest: record.Digest}}
			tuple.At = at
			chatSource := &PersonaPrivateChatInvokerAuthoritySource{Gate: s.gate, Users: personaPrivateChatDiscoveryUserResolver{current: s.current}, Chat: s.chat, Scope: s.chatScope}
			current, projectErr := chatSource.ResolvePersonaChatInvokerAuthority(ctx, tuple)
			if projectErr != nil {
				var denial *agentgate.DeniedError
				if errors.As(projectErr, &denial) {
					slog.InfoContext(ctx, "hcmnext.persona_skill_authority_denied", "skill", key.ID, "conversation_id", tuple.ConversationID, "reason", projectErr.Error())
					continue
				}
				return denied, projectErr
			}
			if _, duplicate := projected[key.ID]; duplicate {
				return denied, errPersonaCurrentAuthorityProjection
			}
			projected[key.ID] = current.SkillAuthorities[key.ID]
			continue
		}
		if !recordSubjectsCurrent {
			continue
		}
		projection, projectErr := s.gate.ProjectSkillAuthorization(ctx, agentgate.DiscoveryRequest{User: user, Purpose: s.purpose, Subjects: subjects, Fields: fields, At: at}, key)
		if projectErr != nil {
			var deniedErr *agentgate.DeniedError
			if errors.As(projectErr, &deniedErr) {
				continue
			}
			return denied, fmt.Errorf("project skill %s: %w", key.String(), projectErr)
		}
		name := key.ID
		if _, exists := projected[name]; exists {
			return denied, errPersonaCurrentAuthorityProjection
		}
		authority, valid := skillAuthorityFromProjection(projection, principal.Tenant())
		if !valid {
			return denied, errPersonaCurrentAuthorityProjection
		}
		projected[name] = authority
	}
	if len(projected) == 0 {
		return denied, errPersonaCurrentAuthorityProjection
	}
	return agentdelegation.UserAuthority{UserID: userID, Active: true, Authority: authorityScopeFromSkills(principal, organization, projected, at), SkillAuthorities: trust.CloneSkillAuthorities(projected)}, nil
}

func subjectsInOrganization(subjects []agentgate.Subject, tenant values.TenantId, organization string) bool {
	for _, subject := range subjects {
		if subject.Ref.Tenant != tenant || subject.Organization.Tenant != tenant || subject.Organization.ID != organization {
			return false
		}
	}
	return true
}

func currentOrganizationScope(principal *trust.Principal, scopes []string) (string, bool) {
	if principal.OrganizationScopeID() != "" {
		return principal.OrganizationScopeID(), slices.Contains(scopes, principal.OrganizationScopeID())
	}
	if len(scopes) != 1 || strings.TrimSpace(scopes[0]) == "" {
		return "", false
	}
	return scopes[0], true
}

func skillAuthorityFromProjection(projection agentgate.SkillAuthorizationProjection, tenant values.TenantId) (trust.SkillAuthority, bool) {
	authority := trust.SkillAuthority{Purposes: []string{projection.Purpose}}
	for _, decision := range projection.Capabilities {
		if !decision.Allowed || decision.Capability.ID == "" || decision.Capability.Version == 0 {
			return trust.SkillAuthority{}, false
		}
		scope, ok := capabilityScope(projection.Skill, decision.Capability)
		if !ok {
			return trust.SkillAuthority{}, false
		}
		authority.Capabilities = append(authority.Capabilities, scope)
		for _, subject := range decision.Subjects {
			if subject.Subject.Tenant != tenant || subject.Subject.String() == "" {
				return trust.SkillAuthority{}, false
			}
			authority.Resources = append(authority.Resources, subject.Subject.String())
			for field, effect := range subject.Fields {
				if effect == authz.EffectRedacted {
					continue
				}
				if effect != authz.EffectAllow {
					return trust.SkillAuthority{}, false
				}
				authority.Fields = append(authority.Fields, string(field))
			}
		}
	}
	if len(authority.Capabilities) == 0 || len(authority.Resources) == 0 || len(authority.Fields) == 0 || len(authority.Purposes) == 0 {
		return trust.SkillAuthority{}, false
	}
	authority.Capabilities = projectionUniqueSorted(authority.Capabilities)
	authority.Resources = projectionUniqueSorted(authority.Resources)
	authority.Fields = projectionUniqueSorted(authority.Fields)
	return authority, true
}

func capabilityScope(skill agentskills.SkillRecord, key capability.Key) (string, bool) {
	for _, operation := range skill.ResolvedOperations {
		if operation.HasCapability && operation.Capability.Definition.Key() == key {
			scope := strings.TrimSpace(operation.Capability.Definition.AuthZScopeRef)
			if scope != "" {
				return scope, true
			}
		}
	}
	return "", false
}

func authorityScopeFromSkills(principal *trust.Principal, organization string, skills trust.SkillAuthorities, at time.Time) trust.AuthorityScope {
	var capabilities, resources, fields, purposes []string
	for _, authority := range skills {
		capabilities = append(capabilities, authority.Capabilities...)
		resources = append(resources, authority.Resources...)
		fields = append(fields, authority.Fields...)
		purposes = append(purposes, authority.Purposes...)
	}
	return trust.AuthorityScope{Tenant: principal.Tenant(), OrganizationScopeID: organization, Capabilities: projectionUniqueSorted(capabilities), Resources: projectionUniqueSorted(resources), Fields: projectionUniqueSorted(fields), Purposes: projectionUniqueSorted(purposes), SkillAuthorities: trust.CloneSkillAuthorities(skills), Assurance: principal.Assurance(), NotBefore: at, ExpiresAt: principal.ExpiresAt()}
}

func projectionUniqueSorted(values []string) []string {
	values = slices.Clone(values)
	slices.Sort(values)
	return slices.Compact(values)
}
