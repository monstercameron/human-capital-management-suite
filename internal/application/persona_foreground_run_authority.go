package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaForegroundRunAuthority = errors.New("application: foreground persona run authority unavailable")

type personaForegroundRunPolicy interface {
	IsBoundT0Run(context.Context, agentinvoke.RunRequest) (bool, error)
}

// PersonaForegroundRunAuthority re-resolves persona-chat admission while the
// authenticated human request is still live. It deliberately cannot recover
// authority after that verified principal expires or is absent.
type PersonaForegroundRunAuthority struct {
	builder *PersonaRunRequestBuilder
	persona agentinvoke.AuthorityResolver
	grants  PersonaGrantTenantStoreFactory
	policy  personaForegroundRunPolicy
	now     func() time.Time
}

// PersonaForegroundRunAuthorityConfig supplies live, server-owned authority
// sources. T0Policy must re-resolve the current skill pins and delegation epoch.
type PersonaForegroundRunAuthorityConfig struct {
	Builder  *PersonaRunRequestBuilder
	Persona  agentinvoke.AuthorityResolver
	Grants   PersonaGrantTenantStoreFactory
	T0Policy personaForegroundRunPolicy
	Now      func() time.Time
}

// NewPersonaForegroundRunAuthority constructs the fail-closed synchronous
// verifier for persona chat runs.
func NewPersonaForegroundRunAuthority(cfg PersonaForegroundRunAuthorityConfig) (*PersonaForegroundRunAuthority, error) {
	if cfg.Builder == nil || cfg.Persona == nil || cfg.Grants == nil || cfg.T0Policy == nil || cfg.Now == nil {
		return nil, errPersonaForegroundRunAuthority
	}
	return &PersonaForegroundRunAuthority{builder: cfg.Builder, persona: cfg.Persona, grants: cfg.Grants, policy: cfg.T0Policy, now: cfg.Now}, nil
}

var _ agentrun.Authority = (*PersonaForegroundRunAuthority)(nil)

// VerifyAdmission accepts only a live, verified human foreground request and
// rechecks its exact owner facts, installation, delegation, and current T0 pins.
func (a *PersonaForegroundRunAuthority) VerifyAdmission(ctx context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if a == nil || ctx == nil || a.builder == nil || a.persona == nil || a.grants == nil || a.policy == nil || a.now == nil || !validPersonaRunAdmissionRequest(request) {
		return agentrun.AuthoritySnapshot{}, errPersonaForegroundRunAuthority
	}
	now := a.now().UTC()
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman ||
		principal.Tenant().String() != request.Source.TenantID || principal.Subject() != request.Principal.InvokerID ||
		!principal.AuthorizesPurpose(request.Purpose) || principal.SessionRef() == "" || principal.IssuedAt().After(now) || !now.Before(principal.ExpiresAt()) || now.IsZero() {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: current verified human session is required", agentrun.ErrAuthorityRefusal)
	}
	invocation := personaRunInvocation(request)
	ctx = withPersonaChatAuthorityTuple(ctx, request.Source.TenantID, request.Principal.InvokerID, request.Audience.ID, request.Context.ID, request.Source.Ref)
	store, err := a.grants.ForTenant(ctx, values.TenantId(invocation.TenantID))
	if err != nil || isNilPersonaOutputPort(store) {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: current tenant grant store unavailable", agentrun.ErrAuthorityRefusal)
	}
	grant, err := store.Get(request.Principal.DelegatedCredentialRef)
	epoch := store.CurrentRevocationEpoch(values.TenantId(invocation.TenantID), invocation.InvokerID)
	if err != nil {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: current durable delegation grant unavailable", agentrun.ErrAuthorityRefusal)
	}
	if !personaForegroundGrantEnvelope(grant, request, now, epoch) {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: durable delegation grant is malformed or outside the request binding", agentrun.ErrAuthorityRefusal)
	}
	bindForegroundGrant(&invocation, grant)
	if invocation.Actor.Validate() != nil || !validPersonaRunInvocation(invocation) {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: request does not bind a complete persona invocation", agentrun.ErrAuthorityRefusal)
	}
	currentRequest, err := a.builder.BuildPersonaChatAdmission(ctx, invocation)
	if err != nil || !personaForegroundRequestMatches(request, currentRequest, now) {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: current persona, manifest, post, audience, context, or policy facts changed", agentrun.ErrAuthorityRefusal)
	}
	current, err := a.persona.Resolve(ctx, agentinvoke.AdmissionRequest{TenantID: invocation.TenantID, ConversationID: invocation.ConversationID, InvokerID: invocation.InvokerID, PersonaID: invocation.PersonaID})
	if err != nil || !current.HumanMember || !current.AudienceMember || !current.PersonaInstalled || !current.Persona.Current || current.Persona.Suspended ||
		!current.Installation.Current || current.Installation.Suspended || current.Persona.ID != invocation.PersonaID || current.Persona.Version != invocation.PersonaVersion ||
		current.Persona.InstallationID != invocation.InstallationID || current.Installation.ID != invocation.InstallationID {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: current persona installation or audience is not eligible", agentrun.ErrAuthorityRefusal)
	}
	if err != nil || !personaForegroundGrantCurrent(grant, request, current, now, epoch) {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: current delegation is revoked, expired, mismatched, or broadened", agentrun.ErrAuthorityRefusal)
	}
	bound, err := a.policy.IsBoundT0Run(ctx, invocation)
	if err != nil || !bound {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: current dynamic T0 skill policy denied", agentrun.ErrAuthorityRefusal)
	}
	digest, err := personaForegroundPolicyDigest(request, grant, epoch, principal.Fingerprint())
	if err != nil {
		return agentrun.AuthoritySnapshot{}, fmt.Errorf("%w: current authority proof could not be pinned", errPersonaForegroundRunAuthority)
	}
	return agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal,
		Audience: request.Audience, Context: request.Context, BudgetCeiling: currentRequest.Budget, GrantRef: grant.GrantID, PolicyDigest: digest}, nil
}

func personaForegroundGrantEnvelope(grant agentdelegation.Grant, request agentrun.Request, now time.Time, epoch uint64) bool {
	return agentdelegation.ValidateGrant(grant) == nil && epoch != 0 && epoch != ^uint64(0) && !grant.Revoked && !grant.Authority.Revoked &&
		!grant.NotBefore.After(now) && now.Before(grant.ExpiresAt) && grant.RevocationEpoch == epoch &&
		grant.GrantID == request.Principal.DelegatedCredentialRef && grant.UserID == request.Principal.InvokerID &&
		grant.Tenant.String() == request.Source.TenantID && request.Persona != nil && grant.AgentVersion == request.Persona.Version &&
		grant.TargetAgentID == request.Agent.AgentID && grant.InstallationID == request.InstallationID && grant.TaskID == request.Source.Key &&
		grant.Purpose == request.Purpose && grant.OrganizationScopeID != "" && len(grant.SkillScopes) > 0 &&
		sameStringSet(grant.Skills, sortedKeys(grant.SkillScopes)) && grant.PlanSkillSetDigest == skillDigest(grant.SkillScopes) &&
		grant.Authority.Delegate == request.Agent.AgentID && grant.Authority.Delegator == request.Principal.InvokerID &&
		grant.Authority.Tenant.String() == request.Source.TenantID && grant.Authority.OrganizationScopeID == grant.OrganizationScopeID &&
		grant.Authority.RevocationEpoch == epoch && grant.Authority.NotBefore.Equal(grant.NotBefore) && grant.Authority.ExpiresAt.Equal(grant.ExpiresAt)
}

func bindForegroundGrant(invocation *agentinvoke.RunRequest, grant agentdelegation.Grant) {
	if invocation == nil {
		return
	}
	invocation.Grant = agentinvoke.DelegationGrant{ID: grant.GrantID, UserID: grant.UserID, TenantID: grant.Tenant.String(), TaskID: grant.TaskID,
		TargetAgentID: grant.TargetAgentID, Skills: cloneSkillScopes(grant.SkillScopes), ExpiresAt: grant.ExpiresAt}
	invocation.Skills = cloneSkillScopes(grant.SkillScopes)
}

func personaRunInvocation(request agentrun.Request) agentinvoke.RunRequest {
	return agentinvoke.RunRequest{InvocationID: request.Source.Key, TenantID: request.Source.TenantID, ConversationID: request.Audience.ID,
		ThreadID: request.Context.ID, InvokingPostID: request.Source.Ref, InvokerID: request.Principal.InvokerID,
		PersonaID: request.Persona.ID, PersonaVersion: request.Persona.Version, InstallationID: request.InstallationID,
		Mode: agentinvoke.OnBehalfOf, Grant: agentinvoke.DelegationGrant{ID: request.Principal.DelegatedCredentialRef,
			UserID: request.Principal.InvokerID, TenantID: request.Source.TenantID, TaskID: request.Source.Key,
			TargetAgentID: request.Agent.AgentID},
		Actor: agentinvoke.ActorChain{UserID: request.Principal.InvokerID, PersonaID: request.Persona.ID,
			PersonaVersion: request.Persona.Version, InstallationID: request.InstallationID, ConversationID: request.Audience.ID,
			InvokingPostID: request.Source.Ref, InvocationID: request.Source.Key}}
}

func personaForegroundRequestMatches(request, current agentrun.Request, now time.Time) bool {
	return request.Source == current.Source && request.Persona != nil && current.Persona != nil && *request.Persona == *current.Persona &&
		request.LegalEntity == current.LegalEntity && request.Agent == current.Agent && request.InstallationID == current.InstallationID &&
		request.Principal == current.Principal && request.Purpose == current.Purpose && request.Audience == current.Audience &&
		request.Context == current.Context && request.CauseID == current.CauseID && request.Deadline.After(now) &&
		!current.Deadline.Before(request.Deadline) && personaRunBudgetWithin(request.Budget, current.Budget)
}

func personaForegroundGrantCurrent(grant agentdelegation.Grant, request agentrun.Request, current agentinvoke.Admission, now time.Time, epoch uint64) bool {
	if agentdelegation.ValidateGrant(grant) != nil || epoch == 0 || epoch == ^uint64(0) || grant.Revoked || grant.Authority.Revoked ||
		!grant.NotBefore.IsZero() && now.Before(grant.NotBefore) || !now.Before(grant.ExpiresAt) || grant.RevocationEpoch != epoch ||
		grant.GrantID != request.Principal.DelegatedCredentialRef || grant.UserID != request.Principal.InvokerID ||
		grant.Tenant.String() != request.Source.TenantID || request.Persona == nil || grant.AgentVersion != request.Persona.Version ||
		grant.TargetAgentID != request.Agent.AgentID || grant.InstallationID != request.InstallationID || grant.TaskID != request.Source.Key ||
		grant.Purpose != request.Purpose || grant.OrganizationScopeID == "" || len(grant.SkillScopes) == 0 ||
		!sameSkills(grant.SkillScopes, current.Discoverable) || !sameStringSet(grant.Skills, sortedKeys(grant.SkillScopes)) ||
		grant.PlanSkillSetDigest != skillDigest(grant.SkillScopes) {
		return false
	}
	return grant.Authority.Delegate == request.Agent.AgentID && grant.Authority.Delegator == request.Principal.InvokerID &&
		grant.Authority.Tenant.String() == request.Source.TenantID && grant.Authority.OrganizationScopeID == grant.OrganizationScopeID &&
		grant.Authority.RevocationEpoch == epoch && grant.Authority.NotBefore.Equal(grant.NotBefore) && grant.Authority.ExpiresAt.Equal(grant.ExpiresAt)
}

func personaForegroundPolicyDigest(request agentrun.Request, grant agentdelegation.Grant, epoch uint64, principalFingerprint string) (string, error) {
	proof := struct {
		Request              agentrun.Request `json:"request"`
		GrantID              string           `json:"grant_id"`
		Epoch                uint64           `json:"epoch"`
		SkillSetDigest       string           `json:"skill_set_digest"`
		PrincipalFingerprint string           `json:"principal_fingerprint"`
	}{Request: request, GrantID: grant.GrantID, Epoch: epoch, SkillSetDigest: grant.PlanSkillSetDigest, PrincipalFingerprint: principalFingerprint}
	data, err := json.Marshal(proof)
	if err != nil || strings.TrimSpace(principalFingerprint) == "" {
		return "", errPersonaForegroundRunAuthority
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
