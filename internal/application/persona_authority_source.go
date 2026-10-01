package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaAuthoritySourceUnavailable = errors.New("application: persona authority source unavailable")

// PersonaAuthorityInstallationStore scopes persona reads to one tenant.
// Implementations must use the same transaction boundary for each reader.
type PersonaAuthorityInstallationStore interface {
	ForTenant(context.Context, values.TenantId) (PersonaAuthorityInstallationReader, error)
}

// PersonaAuthorityInstallationReader reads the current published catalog and
// active placements from one tenant-scoped persona store.
type PersonaAuthorityInstallationReader interface {
	ReadCurrentPersonaAuthority(context.Context, string, string) (agentpersonastore.PersonaVersion, agentpersonastore.ActiveInstallation, error)
}

// PersonaAuthorityScopeSource translates immutable pins and the durable
// installation policy into exact invocation scopes. There is deliberately no
// fallback: the durable policy does not itself contain SkillScopes.
type PersonaAuthorityScopeSource interface {
	ResolvePersonaScopes(context.Context, values.TenantId, agentpersona.PersonaProfile, agentpersonastore.ActiveInstallation, agentpersonastore.ChannelPolicy) (persona, installation, channel agentinvoke.SkillScopes, err error)
}

// DatabasePersonaAuthoritySource is the fail-closed authority projection used
// at persona mention admission. It re-reads current membership, audience,
// publication, installation, and policy facts on every call.
type DatabasePersonaAuthoritySource struct {
	Installations PersonaAuthorityInstallationStore
	Audience      PersonaAudienceSource
	Scopes        PersonaAuthorityScopeSource
}

var _ PersonaAuthoritySource = (*DatabasePersonaAuthoritySource)(nil)

// ResolvePersonaAuthority returns authority only for a current published
// version installed in the requested conversation and allowed for the
// verified invoker. The invoker's identity is a binding, not an authority
// input; membership and audience are read again from the trusted source.
func (s *DatabasePersonaAuthoritySource) ResolvePersonaAuthority(ctx context.Context, request agentinvoke.AdmissionRequest, invoker TrustedInvoker) (agentinvoke.Admission, error) {
	if s == nil || ctx == nil || s.Installations == nil || s.Audience == nil || s.Scopes == nil || strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.ConversationID) == "" || strings.TrimSpace(request.InvokerID) == "" || strings.TrimSpace(request.PersonaID) == "" {
		return agentinvoke.Admission{}, errPersonaAuthoritySourceUnavailable
	}
	tenant := values.TenantId(request.TenantID)
	if err := tenant.Validate(); err != nil {
		return agentinvoke.Admission{}, fmt.Errorf("%w: invalid tenant: %v", errPersonaAuthoritySourceUnavailable, err)
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant() != tenant || verified.Subject() != request.InvokerID {
		return agentinvoke.Admission{}, fmt.Errorf("%w: verified human principal required", errPersonaAuthoritySourceUnavailable)
	}
	if invoker.ID != request.InvokerID || invoker.TenantID != request.TenantID {
		return agentinvoke.Admission{}, fmt.Errorf("%w: invoker binding mismatch", errPersonaAuthoritySourceUnavailable)
	}
	member, err := s.currentMember(ctx, request)
	if err != nil {
		return agentinvoke.Admission{}, err
	}
	store, err := s.Installations.ForTenant(ctx, tenant)
	if err != nil || store == nil {
		return agentinvoke.Admission{}, fmt.Errorf("%w: scope persona store: %v", errPersonaAuthoritySourceUnavailable, err)
	}
	version, installation, err := store.ReadCurrentPersonaAuthority(ctx, request.ConversationID, request.PersonaID)
	if err != nil || version.TenantID != tenant || version.PersonaID != request.PersonaID || version.Version <= 0 || installation.PersonaID != request.PersonaID || installation.ConversationID != request.ConversationID || installation.PersonaVersion != version.Version || installation.InstallationID == "" || !installationAudienceCurrent(ctx, s.Audience, request, installation.InstallationID, version.Version) {
		return agentinvoke.Admission{}, fmt.Errorf("%w: installation is not current or audience eligible", errPersonaAuthoritySourceUnavailable)
	}
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(version.Profile, &profile); err != nil || profile.PersonaID != version.PersonaID || int64(profile.Version) != version.Version {
		return agentinvoke.Admission{}, fmt.Errorf("%w: published persona profile integrity failure", errPersonaAuthoritySourceUnavailable)
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != version.ContentDigest {
		return agentinvoke.Admission{}, fmt.Errorf("%w: published persona digest mismatch", errPersonaAuthoritySourceUnavailable)
	}
	personaScopes, installationScopes, channelScopes, err := s.Scopes.ResolvePersonaScopes(ctx, tenant, profile, installation, installation.ChannelPolicy)
	if err != nil {
		return agentinvoke.Admission{}, fmt.Errorf("%w: resolve exact persona ceilings: %v", errPersonaAuthoritySourceUnavailable, err)
	}
	return agentinvoke.Admission{
		Persona:          agentinvoke.Persona{ID: version.PersonaID, Version: fmt.Sprint(version.Version), PinnedSkills: personaScopes, Current: true, InstallationID: installation.InstallationID},
		Installation:     agentinvoke.Installation{ID: installation.InstallationID, Current: true, SkillCeiling: installationScopes},
		Channel:          agentinvoke.ChannelPolicy{SkillCeiling: channelScopes},
		Discoverable:     invoker.Discoverable,
		HumanMember:      member.SubjectID == request.InvokerID,
		AudienceMember:   true,
		PersonaInstalled: true,
	}, nil
}

func (s *DatabasePersonaAuthoritySource) currentMember(ctx context.Context, request agentinvoke.AdmissionRequest) (PersonaAudienceMember, error) {
	conversations, err := s.Audience.ListCurrentPersonaAudience(ctx, request.TenantID, request.InvokerID)
	if err != nil {
		return PersonaAudienceMember{}, fmt.Errorf("%w: current audience: %v", errPersonaAuthoritySourceUnavailable, err)
	}
	for _, conversation := range conversations {
		if conversation.TenantID != request.TenantID || conversation.ConversationID != request.ConversationID {
			continue
		}
		for _, member := range conversation.Members {
			if member.SubjectID == request.InvokerID && member.SubjectID != "" && len(member.Roles) > 0 && len(member.Populations) > 0 && member.OrganizationScope != "" {
				return member, nil
			}
		}
	}
	return PersonaAudienceMember{}, fmt.Errorf("%w: invoker is not a verified current member", errPersonaAuthoritySourceUnavailable)
}

func latestPublished(items []agentpersonastore.PersonaVersion, tenant, persona string) (agentpersonastore.PersonaVersion, bool) {
	var latest agentpersonastore.PersonaVersion
	for _, item := range items {
		if item.TenantID.String() != tenant || item.PersonaID != persona || item.Version <= latest.Version {
			continue
		}
		latest = item
	}
	return latest, latest.Version > 0
}

func matchingInstallation(items []agentpersonastore.ActiveInstallation, request agentinvoke.AdmissionRequest, version int64) (agentpersonastore.ActiveInstallation, bool) {
	var found agentpersonastore.ActiveInstallation
	for _, item := range items {
		if item.ConversationID != request.ConversationID || item.PersonaID != request.PersonaID || item.PersonaVersion != version || item.InstallationID == "" {
			continue
		}
		if found.InstallationID != "" {
			return agentpersonastore.ActiveInstallation{}, false
		}
		found = item
	}
	return found, found.InstallationID != ""
}

func installationAudienceCurrent(ctx context.Context, source PersonaAudienceSource, request agentinvoke.AdmissionRequest, installationID string, version int64) bool {
	items, err := source.ListCurrentPersonaInstallations(ctx, request.TenantID, request.ConversationID)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.Tuple.PersonaID == request.PersonaID && item.Tuple.PersonaVersion == version && item.Tuple.InstallationID == installationID && item.Active && item.CurrentVersion {
			return true
		}
	}
	return false
}
