package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaT0DynamicPolicy = errors.New("application: current persona T0 policy denied")

// PersonaT0GrantStoreFactory returns a delegation store bound to one tenant.
// The returned store must read durable grants and revocation epochs.
type PersonaT0GrantStoreFactory interface {
	ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error)
}

// PersonaT0DynamicPolicyConfig contains only current, server-owned sources.
// Persona stores, grants, authority, and run facts are re-read on every call.
type PersonaT0DynamicPolicyConfig struct {
	Personas         PersonaAuthorityInstallationStore
	Authority        agentinvoke.AuthorityResolver
	InvokerAuthority InvokerAuthoritySource
	GrantStores      PersonaT0GrantStoreFactory
	Facts            PersonaRunRequestSource
	Catalog          PersonaT0SkillCatalog
	Now              func() time.Time
	Background       personaForegroundRunPolicy
}

// DatabasePersonaT0SkillPolicy dynamically proves a persona run against its
// current immutable pins, active installation, current invoker authority,
// durable delegation grant and current revocation epoch.
type DatabasePersonaT0SkillPolicy struct {
	personas         PersonaAuthorityInstallationStore
	authority        agentinvoke.AuthorityResolver
	invokerAuthority InvokerAuthoritySource
	grantStores      PersonaT0GrantStoreFactory
	facts            PersonaRunRequestSource
	catalog          PersonaT0SkillCatalog
	now              func() time.Time
	background       personaForegroundRunPolicy
}

// NewDatabasePersonaT0SkillPolicy constructs a fail-closed served policy. It
// intentionally has no constructor-supplied skill bindings or test registry.
func NewDatabasePersonaT0SkillPolicy(cfg PersonaT0DynamicPolicyConfig) (*DatabasePersonaT0SkillPolicy, error) {
	if isNilPersonaOutputPort(cfg.Personas) || isNilPersonaOutputPort(cfg.Authority) || isNilPersonaOutputPort(cfg.InvokerAuthority) ||
		isNilPersonaOutputPort(cfg.GrantStores) || isNilPersonaOutputPort(cfg.Facts) || isNilPersonaOutputPort(cfg.Catalog) || cfg.Now == nil {
		return nil, errPersonaT0DynamicPolicy
	}
	return &DatabasePersonaT0SkillPolicy{personas: cfg.Personas, authority: cfg.Authority,
		invokerAuthority: cfg.InvokerAuthority, grantStores: cfg.GrantStores, facts: cfg.Facts,
		catalog: cfg.Catalog, now: cfg.Now, background: cfg.Background}, nil
}

// IsBoundT0Run permits only the exact current, read-only skill set that is
// still covered by the invoker's current authority and the durable grant.
func (p *DatabasePersonaT0SkillPolicy) IsBoundT0Run(ctx context.Context, request agentinvoke.RunRequest) (bool, error) {
	if p == nil || ctx == nil || !validPersonaT0DynamicRequest(request) {
		return false, errPersonaT0DynamicPolicy
	}
	principal, ok := trust.FromContext(ctx)
	if !ok && p.background != nil {
		return p.background.IsBoundT0Run(ctx, request)
	}
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman ||
		principal.Tenant().String() != request.TenantID || principal.Subject() != request.InvokerID {
		return false, errPersonaT0DynamicPolicy
	}
	ctx = withPersonaChatAuthorityTuple(ctx, request.TenantID, request.InvokerID, request.ConversationID, request.ThreadID, request.InvokingPostID)
	now := p.now().UTC()
	if now.IsZero() {
		return false, errPersonaT0DynamicPolicy
	}
	current, err := p.authority.Resolve(ctx, agentinvoke.AdmissionRequest{TenantID: request.TenantID,
		ConversationID: request.ConversationID, InvokerID: request.InvokerID, PersonaID: request.PersonaID})
	if err != nil || !current.Persona.Current || current.Persona.Suspended || !current.Installation.Current || current.Installation.Suspended ||
		!current.HumanMember || !current.AudienceMember || !current.PersonaInstalled || current.Persona.ID != request.PersonaID ||
		current.Persona.Version != request.PersonaVersion || current.Persona.InstallationID != request.InstallationID ||
		current.Installation.ID != request.InstallationID || !sameSkillScopes(current.Discoverable, request.Skills) {
		return false, fmt.Errorf("%w: current persona authority (error=%v persona_current=%t persona_suspended=%t installation_current=%t installation_suspended=%t human_member=%t audience_member=%t installed=%t version=%v/%v installation=%q/%q skills_match=%t discoverable=%d requested=%d)", errPersonaT0DynamicPolicy, err,
			current.Persona.Current, current.Persona.Suspended, current.Installation.Current, current.Installation.Suspended, current.HumanMember, current.AudienceMember, current.PersonaInstalled,
			current.Persona.Version, request.PersonaVersion, current.Installation.ID, request.InstallationID, sameSkillScopes(current.Discoverable, request.Skills), len(current.Discoverable), len(request.Skills))
	}
	facts, err := p.facts.ResolvePersonaRun(ctx, request)
	if err != nil || facts.TenantID != request.TenantID || facts.TriggerID != request.InvocationID ||
		facts.Agent.AgentID != request.Grant.TargetAgentID || facts.Agent.Version == "" || !validPersonaRunDigest(facts.Agent.Digest) {
		return false, fmt.Errorf("%w: current agent and persona facts", errPersonaT0DynamicPolicy)
	}
	pins, personaDigest, agentVersion, err := p.currentPins(ctx, request)
	if err != nil || facts.PersonaDigest != personaDigest || !personaAgentVersionMatches(facts.Agent.Version, agentVersion) || !pinnedT0ScopesMatch(p.catalog, pins, request.Skills) {
		return false, fmt.Errorf("%w: current immutable T0 pins (error=%v persona_digest_match=%t agent_version=%q/%q pins=%d scopes_match=%t)", errPersonaT0DynamicPolicy, err, facts.PersonaDigest == personaDigest, facts.Agent.Version, agentVersion, len(pins), pinnedT0ScopesMatch(p.catalog, pins, request.Skills))
	}
	userAuthority, err := p.invokerAuthority.ResolveInvokerAuthority(ctx, request.InvokerID,
		values.TenantId(request.TenantID), "persona-mention", now)
	if err != nil || !currentGrantAuthorityCovers(userAuthority, request, now) {
		return false, fmt.Errorf("%w: current invoker grants", errPersonaT0DynamicPolicy)
	}
	store, err := p.grantStores.ForTenant(ctx, values.TenantId(request.TenantID))
	if err != nil || isNilPersonaOutputPort(store) {
		return false, fmt.Errorf("%w: current durable delegation grant", errPersonaT0DynamicPolicy)
	}
	grant, err := store.Get(request.Grant.ID)
	epoch := store.CurrentRevocationEpoch(values.TenantId(request.TenantID), request.InvokerID)
	if err != nil || epoch == 0 || epoch == math.MaxUint64 || !currentPersonaT0Grant(grant, request, now, epoch) ||
		!personaT0StoredAuthorityCurrent(userAuthority, grant, request) {
		return false, fmt.Errorf("%w: current durable delegation grant", errPersonaT0DynamicPolicy)
	}
	agentUXSpeedCacheToolPins(ctx, request.InvocationID, pins)
	return true, nil
}

func (p *DatabasePersonaT0SkillPolicy) currentPins(ctx context.Context, request agentinvoke.RunRequest) ([]agentskills.SkillPin, string, string, error) {
	tenant := values.TenantId(request.TenantID)
	if tenant.Validate() != nil {
		return nil, "", "", errPersonaT0DynamicPolicy
	}
	reader, err := p.personas.ForTenant(ctx, tenant)
	if err != nil || isNilPersonaOutputPort(reader) {
		return nil, "", "", errPersonaT0DynamicPolicy
	}
	version, installation, err := reader.ReadCurrentPersonaAuthority(ctx, request.ConversationID, request.PersonaID)
	if err != nil || version.TenantID != tenant || version.PersonaID != request.PersonaID ||
		!personaRunVersionMatches(version.Version, request.PersonaVersion) || installation.InstallationID != request.InstallationID ||
		installation.PersonaID != request.PersonaID || installation.PersonaVersion != version.Version ||
		installation.ConversationID != request.ConversationID {
		return nil, "", "", errPersonaT0DynamicPolicy
	}
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(version.Profile, &profile); err != nil || profile.PersonaID != request.PersonaID ||
		fmt.Sprint(profile.Version) != strings.TrimPrefix(request.PersonaVersion, "v") || profile.TierCeiling < agentskills.TierT0 {
		return nil, "", "", errPersonaT0DynamicPolicy
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != version.ContentDigest {
		return nil, "", "", errPersonaT0DynamicPolicy
	}
	return slices.Clone(profile.SkillPins), sealed.Digest, version.AgentVersion, nil
}

// personaAgentVersionMatches compares the agent version of the run facts with
// the one stored on the persona version. The persona row stores it qualified
// by the manifest id ("agent.starter.policy_helper@2") while run facts carry
// the bare manifest version ("2"); the sealed persona digest, checked beside
// this, already binds the exact manifest, so the version components must
// simply agree.
func personaAgentVersionMatches(facts, stored string) bool {
	facts, stored = strings.TrimSpace(facts), strings.TrimSpace(stored)
	if facts == "" || stored == "" {
		return false
	}
	if facts == stored {
		return true
	}
	factsAt, storedAt := strings.LastIndex(facts, "@"), strings.LastIndex(stored, "@")
	if factsAt >= 0 && storedAt >= 0 {
		return false
	}
	if factsAt >= 0 {
		return facts[factsAt+1:] == stored
	}
	return stored[storedAt+1:] == facts
}

func pinnedT0ScopesMatch(catalog PersonaT0SkillCatalog, pins []agentskills.SkillPin, requested agentinvoke.SkillScopes) bool {
	if catalog == nil || len(pins) == 0 || len(requested) == 0 {
		return false
	}
	byID := make(map[string]agentskills.SkillPin, len(pins))
	for _, pin := range pins {
		if validatePersonaT0Pin(pin) != nil {
			return false
		}
		if _, duplicate := byID[pin.ID]; duplicate {
			return false
		}
		byID[pin.ID] = pin
	}
	for skill, scopes := range requested {
		pin, ok := byID[skill]
		if !ok {
			return false
		}
		record, err := catalog.ResolvePin(pin)
		if err != nil || record.Definition.ID != pin.ID || record.Definition.Version != pin.Version || record.Digest != pin.Digest ||
			record.Status != agentskills.StatusActive || record.Definition.SideEffectTier != agentskills.TierT0 || record.HighestCapabilityTier != agentskills.TierT0 {
			return false
		}
		capabilityScopes, err := exactCapabilityScopes(record)
		if err != nil || !sameScopes(capabilityScopes, scopes) {
			return false
		}
	}
	return true
}

func currentGrantAuthorityCovers(authority agentdelegation.UserAuthority, request agentinvoke.RunRequest, at time.Time) bool {
	if !authority.Active || authority.UserID != request.InvokerID || authority.Authority.Tenant != values.TenantId(request.TenantID) ||
		authority.Authority.OrganizationScopeID == "" || authority.Authority.NotBefore.IsZero() ||
		at.Before(authority.Authority.NotBefore) || !at.Before(authority.Authority.ExpiresAt) || len(authority.SkillAuthorities) == 0 {
		return false
	}
	for skill, scopes := range request.Skills {
		current, ok := authority.SkillAuthorities[skill]
		if !ok || !scopeSubset(scopes, current.Capabilities) {
			return false
		}
	}
	return true
}

func personaT0StoredAuthorityCurrent(current agentdelegation.UserAuthority, grant agentdelegation.Grant, request agentinvoke.RunRequest) bool {
	if current.Authority.OrganizationScopeID != grant.OrganizationScopeID ||
		!current.Authority.Assurance.AtLeast(grant.Authority.RequiredAssurance) ||
		grant.GrantID != personaGrantID(agentinvoke.GrantRequest{InvocationID: request.InvocationID, TenantID: request.TenantID}) {
		return false
	}
	for skill, granted := range grant.SkillAuthorities {
		present, ok := current.SkillAuthorities[skill]
		if !ok || !scopeSubset(granted.Capabilities, present.Capabilities) ||
			!personaT0StringSetSubset(granted.Resources, present.Resources) || !personaT0StringSetSubset(granted.Fields, present.Fields) ||
			!personaT0StringSetSubset(granted.Purposes, present.Purposes) {
			return false
		}
	}
	return len(grant.SkillAuthorities) == len(request.Skills)
}

func currentPersonaT0Grant(grant agentdelegation.Grant, request agentinvoke.RunRequest, now time.Time, epoch uint64) bool {
	tenant := values.TenantId(request.TenantID)
	if agentdelegation.ValidateGrant(grant) != nil || grant.GrantID != request.Grant.ID || grant.UserID != request.InvokerID ||
		grant.Tenant != tenant || grant.AgentVersion != request.PersonaVersion || grant.TargetAgentID != request.Grant.TargetAgentID ||
		grant.InstallationID != request.InstallationID || grant.TaskID != request.InvocationID || grant.Purpose != "persona-mention" ||
		grant.Revoked || epoch == 0 || grant.RevocationEpoch != epoch || now.Before(grant.NotBefore) || !now.Before(grant.ExpiresAt) ||
		!grant.ExpiresAt.Equal(request.Grant.ExpiresAt) || !sameSkillScopes(grant.SkillScopes, request.Skills) ||
		!sameSkillScopes(request.Grant.Skills, request.Skills) || !sameStringSet(grant.Skills, sortedKeys(request.Skills)) ||
		grant.PlanSkillSetDigest != skillDigest(request.Skills) || grant.Authority.GrantID != grant.GrantID ||
		grant.Authority.RootID != grant.GrantID || grant.Authority.Delegator != grant.UserID || grant.Authority.Revoked ||
		grant.Authority.Tenant != tenant || grant.Authority.OrganizationScopeID != grant.OrganizationScopeID ||
		grant.Authority.RevocationEpoch != grant.RevocationEpoch || !grant.Authority.NotBefore.Equal(grant.NotBefore) ||
		!grant.Authority.ExpiresAt.Equal(grant.ExpiresAt) || !sameScopes(grant.Authority.Capabilities, personaT0CapabilityScopes(grant.SkillScopes)) ||
		!sameStringSet(personaT0AuthorityKeys(grant.SkillAuthorities), sortedKeys(request.Skills)) ||
		!sameSkillAuthorities(grant.SkillAuthorities, grant.Authority.SkillAuthorities) ||
		!sameScopes(grant.Authority.Resources, skillAuthorityDimension(grant.SkillAuthorities, func(a trust.SkillAuthority) []string { return a.Resources })) ||
		!sameScopes(grant.Authority.Fields, skillAuthorityDimension(grant.SkillAuthorities, func(a trust.SkillAuthority) []string { return a.Fields })) ||
		!sameScopes(grant.Authority.Purposes, skillAuthorityDimension(grant.SkillAuthorities, func(a trust.SkillAuthority) []string { return a.Purposes })) {
		return false
	}
	return true
}

func skillAuthorityDimension(authorities trust.SkillAuthorities, selectValues func(trust.SkillAuthority) []string) []string {
	var out []string
	for _, authority := range authorities {
		out = append(out, selectValues(authority)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func personaT0AuthorityKeys(authorities trust.SkillAuthorities) []string {
	keys := make([]string, 0, len(authorities))
	for skill := range authorities {
		keys = append(keys, skill)
	}
	slices.Sort(keys)
	return keys
}

func personaT0CapabilityScopes(scopes map[string][]string) []string {
	var out []string
	for _, values := range scopes {
		out = append(out, values...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func validPersonaT0DynamicRequest(request agentinvoke.RunRequest) bool {
	if request.Mode != agentinvoke.OnBehalfOf || len(request.Skills) == 0 || request.Grant.ID == "" ||
		request.Grant.UserID != request.InvokerID || request.Grant.TenantID != request.TenantID || request.Grant.TaskID != request.InvocationID ||
		request.Actor.UserID != request.InvokerID || request.Actor.PersonaID != request.PersonaID || request.Actor.PersonaVersion != request.PersonaVersion ||
		request.Actor.InstallationID != request.InstallationID || request.Actor.ConversationID != request.ConversationID ||
		request.Actor.InvokingPostID != request.InvokingPostID || request.Actor.InvocationID != request.InvocationID {
		return false
	}
	if values.TenantId(request.TenantID).Validate() != nil {
		return false
	}
	for _, value := range []string{request.InvocationID, request.ConversationID, request.ThreadID, request.InvokingPostID,
		request.InvokerID, request.PersonaID, request.PersonaVersion, request.InstallationID, request.Grant.TargetAgentID} {
		if !required(value) {
			return false
		}
	}
	for skill, scopes := range request.Skills {
		if !required(skill) || len(scopes) == 0 || !slices.Equal(scopes, normalizePersonaT0Scopes(scopes)) {
			return false
		}
	}
	return true
}

func sameSkillScopes(a, b agentinvoke.SkillScopes) bool {
	if len(a) != len(b) {
		return false
	}
	for skill, scopes := range a {
		if !sameScopes(scopes, b[skill]) {
			return false
		}
	}
	return true
}

func sameScopes(a, b []string) bool {
	return slices.Equal(normalizePersonaT0Scopes(a), normalizePersonaT0Scopes(b))
}

func scopeSubset(child, parent []string) bool {
	allowed := make(map[string]struct{}, len(parent))
	for _, scope := range parent {
		allowed[scope] = struct{}{}
	}
	for _, scope := range child {
		if _, ok := allowed[scope]; !ok {
			return false
		}
	}
	return len(child) > 0
}

func personaT0StringSetSubset(child, parent []string) bool {
	allowed := make(map[string]struct{}, len(parent))
	for _, value := range parent {
		allowed[value] = struct{}{}
	}
	for _, value := range child {
		if _, ok := allowed[value]; !ok {
			return false
		}
	}
	return true
}

var _ personaT0SkillPolicy = (*DatabasePersonaT0SkillPolicy)(nil)
