package application

import (
	"context"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentTaskPersonaResolver applies the current persona invocation gate and
// returns only the safe identity fields persisted with a task.
type AgentTaskPersonaResolver interface {
	ResolveAgentTaskPersona(context.Context, *trust.Principal, string) (agentrun.TaskAgentIdentity, error)
}

type agentTaskPersonaResolver struct {
	authority agentinvoke.AuthorityResolver
	audience  CurrentPersonaAudienceResolver
	personas  *agentpersonastore.Store
}

func NewAgentTaskPersonaResolver(authority agentinvoke.AuthorityResolver, audience CurrentPersonaAudienceResolver, personas *agentpersonastore.Store) (AgentTaskPersonaResolver, error) {
	if isNilPersonaOutputPort(authority) || audience == nil || personas == nil {
		return nil, agentrun.ErrTaskAgentUnavailable
	}
	return &agentTaskPersonaResolver{authority: authority, audience: audience, personas: personas}, nil
}

func (r *agentTaskPersonaResolver) ResolveAgentTaskPersona(ctx context.Context, principal *trust.Principal, personaID string) (agentrun.TaskAgentIdentity, error) {
	personaID = strings.TrimSpace(personaID)
	verified, ok := trust.FromContext(ctx)
	if r == nil || r.authority == nil || r.audience == nil || r.personas == nil || !ok || verified == nil || principal == nil || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() || principal.SubjectKind() != trust.SubjectKindHuman || personaID == "" {
		return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentDenied
	}
	allowed, err := r.audience.ResolveAvailablePersonaInstallations(ctx, principal)
	if err != nil {
		return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentUnavailable
	}
	for _, placement := range allowed {
		if placement.PersonaID != personaID {
			continue
		}
		admission, resolveErr := r.authority.Resolve(ctx, agentinvoke.AdmissionRequest{TenantID: principal.Tenant().String(), ConversationID: placement.ConversationID, InvokerID: principal.Subject(), PersonaID: personaID})
		if resolveErr != nil {
			return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentDenied
		}
		if !agentTaskPersonaAdmissionAllowed(admission, placement) {
			continue
		}
		version, parseErr := strconv.ParseInt(admission.Persona.Version, 10, 64)
		if parseErr != nil || version <= 0 {
			return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentUnavailable
		}
		store, storeErr := r.personas.ForTenant(ctx, principal.Tenant())
		if storeErr != nil {
			return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentUnavailable
		}
		profile, readErr := store.GetVersion(ctx, personaID, version)
		if readErr != nil || profile.PersonaID != personaID || profile.Version != version || strings.TrimSpace(profile.DisplayName) == "" {
			return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentUnavailable
		}
		identity := agentrun.TaskAgentIdentity{ID: personaID, DisplayName: profile.DisplayName, Version: admission.Persona.Version}
		if identity.Validate() != nil {
			return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentUnavailable
		}
		return identity, nil
	}
	return agentrun.TaskAgentIdentity{}, agentrun.ErrTaskAgentDenied
}

func agentTaskPersonaAdmissionAllowed(admission agentinvoke.Admission, placement agentpersonastore.AvailableInstallation) bool {
	if !admission.HumanMember || !admission.AudienceMember || !admission.PersonaInstalled || !admission.Persona.Current || admission.Persona.Suspended || !admission.Installation.Current || admission.Installation.Suspended {
		return false
	}
	if admission.Persona.ID != placement.PersonaID || admission.Persona.Version != strconv.FormatInt(placement.PersonaVersion, 10) || admission.Installation.ID != placement.InstallationID || (admission.Persona.InstallationID != "" && admission.Persona.InstallationID != placement.InstallationID) {
		return false
	}
	effective := agentinvoke.IntersectSkillScopes(admission.Persona.PinnedSkills, admission.Installation.SkillCeiling, admission.Channel.SkillCeiling, admission.Discoverable)
	return len(effective) > 0
}
