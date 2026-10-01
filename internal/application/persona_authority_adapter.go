package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	errPersonaAuthorityAdapter = errors.New("application: persona authority adapter unavailable")
	errPersonaAuthorityDenied  = errors.New("application: persona authority denied")
)

// TrustedInvoker is the server-owned projection of the authenticated human
// who typed a mention. Implementations must resolve it from current identity
// and policy state; callers cannot provide roles, membership, or skills.
type TrustedInvoker struct {
	ID             string
	TenantID       string
	HumanMember    bool
	AudienceMember bool
	Discoverable   agentinvoke.SkillScopes
}

// PersonaAuthoritySource resolves current persona, installation, and channel
// ceilings for one trusted invoker. It must re-read lifecycle and membership
// state on every call.
type PersonaAuthoritySource interface {
	ResolvePersonaAuthority(context.Context, agentinvoke.AdmissionRequest, TrustedInvoker) (agentinvoke.Admission, error)
}

// TrustedInvokerSource resolves the authenticated human identity and current
// audience/skill discovery facts used by mention admission.
type TrustedInvokerSource interface {
	ResolveTrustedInvoker(context.Context, string, string) (TrustedInvoker, error)
}

// InvokerAuthoritySource resolves current user authority for grant creation.
// The result must come from the policy decision point, never from a request.
type InvokerAuthoritySource interface {
	ResolveInvokerAuthority(context.Context, string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error)
}

// PersonaAuthorityAdapter implements both persona mention authority and the
// on-behalf-of grant issuer. It accepts identity references only and derives
// all authority from trusted server-side sources.
type PersonaAuthorityAdapter struct {
	Personas PersonaAuthoritySource
	Identity TrustedInvokerSource
	Invokers InvokerAuthoritySource
	Grants   *agentdelegation.Service
	Store    agentdelegation.GrantStore
	Now      func() time.Time
}

var _ agentinvoke.AuthorityResolver = (*PersonaAuthorityAdapter)(nil)
var _ agentinvoke.GrantIssuer = (*PersonaAuthorityAdapter)(nil)

// Resolve returns admission facts with effective skills equal to the exact
// intersection of persona pins, installation ceiling, channel ceiling, and
// the trusted invoker's current discoverable skills.
func (a *PersonaAuthorityAdapter) Resolve(ctx context.Context, request agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	if a == nil || a.Personas == nil || a.Identity == nil || strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.ConversationID) == "" || strings.TrimSpace(request.InvokerID) == "" || strings.TrimSpace(request.PersonaID) == "" {
		return agentinvoke.Admission{}, fmt.Errorf("%w: persona source and complete request are required", errPersonaAuthorityAdapter)
	}
	invoker, err := a.resolveTrustedInvoker(ctx, request)
	if err != nil {
		return agentinvoke.Admission{}, err
	}
	admission, err := a.Personas.ResolvePersonaAuthority(ctx, request, invoker)
	if err != nil {
		return agentinvoke.Admission{}, fmt.Errorf("resolve persona authority: %w", err)
	}
	admission.HumanMember = admission.HumanMember && invoker.HumanMember
	admission.AudienceMember = admission.AudienceMember && invoker.AudienceMember
	admission.Discoverable = agentinvoke.IntersectSkillScopes(admission.Persona.PinnedSkills, admission.Installation.SkillCeiling, admission.Channel.SkillCeiling, admission.Discoverable, invoker.Discoverable)
	return admission, nil
}

// CreateOnBehalfOfGrant creates a durable run-bound grant after resolving the
// invoker's current authority. Requested skills are retained only when they
// remain inside that authority; the delegation service performs the final
// capability intersection and revocation-epoch check.
func (a *PersonaAuthorityAdapter) CreateOnBehalfOfGrant(ctx context.Context, request agentinvoke.GrantRequest) (agentinvoke.DelegationGrant, error) {
	if a == nil || a.Invokers == nil || a.Grants == nil || a.Store == nil || a.Now == nil || request.Mode != agentinvoke.OnBehalfOf {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: grant sources and clock are required", errPersonaAuthorityAdapter)
	}
	if strings.TrimSpace(request.InvocationID) == "" || strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.AgentVersion) == "" || strings.TrimSpace(request.InstallationID) == "" || strings.TrimSpace(request.Purpose) == "" || len(request.Skills) == 0 {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: incomplete grant request", agentinvoke.ErrInvalidRequest)
	}
	ctx = withPersonaChatAuthorityTuple(ctx, request.TenantID, request.UserID, request.ConversationID, request.ThreadID, request.InvokingPostID)
	admission, err := a.Resolve(ctx, agentinvoke.AdmissionRequest{TenantID: request.TenantID, ConversationID: request.ConversationID, InvokerID: request.UserID, PersonaID: request.PersonaID})
	if err != nil {
		return agentinvoke.DelegationGrant{}, err
	}
	if !admission.HumanMember || !admission.AudienceMember || !admission.PersonaInstalled || !admission.Persona.Current || !admission.Installation.Current {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: persona admission is not current", errPersonaAuthorityDenied)
	}
	if admission.Persona.ID != request.PersonaID || admission.Persona.Version != request.PersonaVersion || admission.Installation.ID != request.InstallationID {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: persona installation binding mismatch", errPersonaAuthorityDenied)
	}
	effective := agentinvoke.IntersectSkillScopes(request.Skills, admission.Discoverable)
	if len(effective) == 0 || !sameSkills(effective, request.Skills) {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: requested skills exceed persona admission", agentdelegation.ErrScopeExpanded)
	}
	now := a.Now().UTC()
	authority, err := a.Invokers.ResolveInvokerAuthority(ctx, request.UserID, values.TenantId(request.TenantID), request.Purpose, now)
	if err != nil {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("resolve invoker authority: %w", err)
	}
	if !authority.Active || authority.UserID != request.UserID || authority.Authority.Tenant != values.TenantId(request.TenantID) || strings.TrimSpace(authority.Authority.OrganizationScopeID) == "" {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: invoker has no current authority", errPersonaAuthorityDenied)
	}
	if authority.SkillAuthorities == nil {
		return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: per-skill authority is required", errPersonaAuthorityDenied)
	}
	authorityScope := authority.Authority
	authorityScope.SkillAuthorities = trust.CloneSkillAuthorities(authority.SkillAuthorities)
	scopedAuthorities := make(agentdelegation.SkillAuthorities, len(effective))
	for skill, scopes := range effective {
		current, ok := authority.SkillAuthorities[skill]
		if !ok {
			return agentinvoke.DelegationGrant{}, fmt.Errorf("%w: no current authority for skill %q", errPersonaAuthorityDenied, skill)
		}
		current.Capabilities = slices.Clone(scopes)
		scopedAuthorities[skill] = current
	}
	request.Skills = request.Skills.Clone()
	digest := skillDigest(request.Skills)
	expiresAt := request.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		expiresAt = now.Add(agentdelegation.MaxGrantLifetime)
	}
	request.ExpiresAt = expiresAt.Truncate(time.Microsecond)
	grantID := personaGrantID(request)
	granted, err := a.Grants.CreateScopedGrant(agentdelegation.GrantRequest{
		GrantID: grantID, UserID: request.UserID, Tenant: values.TenantId(request.TenantID), AgentVersion: request.AgentVersion, TargetAgentID: request.TargetAgentID,
		InstallationID: request.InstallationID, TaskID: request.InvocationID, PlanSkillSetDigest: digest,
		Purpose: request.Purpose, OrganizationScopeID: authority.Authority.OrganizationScopeID,
		Skills: sortedKeys(effective), SkillScopes: effective.Clone(), NotBefore: personaGrantNotBefore(now),
		ExpiresAt: request.ExpiresAt, UserAuthority: authorityScope, SkillAuthorities: scopedAuthorities,
	})
	if err != nil {
		// A retried invocation is idempotent. Only reuse a durable grant after
		// checking every binding that the invocation supplied.
		stored, getErr := a.Store.Get(grantID)
		epoch := a.Store.CurrentRevocationEpoch(values.TenantId(request.TenantID), request.UserID)
		if epoch == 0 {
			epoch = 1
		}
		if getErr != nil || !grantMatchesRequest(stored, request, grantID, digest, effective, scopedAuthorities, authority.Authority.OrganizationScopeID, epoch) {
			return agentinvoke.DelegationGrant{}, fmt.Errorf("create delegation grant: %w", err)
		}
		granted = stored
	}
	return agentinvoke.DelegationGrant{ID: granted.GrantID, UserID: granted.UserID, TenantID: granted.Tenant.String(), TaskID: granted.TaskID, TargetAgentID: granted.TargetAgentID, Skills: cloneSkillScopes(granted.SkillScopes), ExpiresAt: granted.ExpiresAt}, nil
}

// PostgreSQL persists microsecond timestamps. Round the start upward so its
// column and embedded authority agree without making the grant valid early.
func personaGrantNotBefore(at time.Time) time.Time {
	start := at.UTC().Truncate(time.Microsecond)
	if start.Before(at) {
		start = start.Add(time.Microsecond)
	}
	return start
}

func (a *PersonaAuthorityAdapter) resolveTrustedInvoker(ctx context.Context, request agentinvoke.AdmissionRequest) (TrustedInvoker, error) {
	invoker, err := a.Identity.ResolveTrustedInvoker(ctx, request.TenantID, request.InvokerID)
	if err != nil {
		return TrustedInvoker{}, fmt.Errorf("resolve trusted invoker: %w", err)
	}
	if invoker.ID != request.InvokerID || invoker.TenantID != request.TenantID {
		return TrustedInvoker{}, fmt.Errorf("%w: trusted invoker binding mismatch", errPersonaAuthorityDenied)
	}
	return invoker, nil
}

func personaGrantID(r agentinvoke.GrantRequest) string {
	// The key identifies the invocation delivery. Mutable grant bindings such
	// as agent version are checked against the stored grant on a retry rather
	// than allowing a changed request to mint a second grant.
	s := strings.Join([]string{r.InvocationID, r.TenantID}, "\x00")
	h := sha256.Sum256([]byte("persona-delegation/v1\x00" + s))
	return "pgrant_" + hex.EncodeToString(h[:])
}

func skillDigest(skills agentinvoke.SkillScopes) string {
	h := sha256.New()
	for _, skill := range sortedKeys(skills) {
		h.Write([]byte(skill + "\x00"))
		for _, scope := range sortedScopes(skills[skill]) {
			h.Write([]byte(scope + "\x00"))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sortedScopes(scopes []string) []string {
	out := slices.Clone(scopes)
	slices.Sort(out)
	return out
}

func grantMatchesRequest(g agentdelegation.Grant, request agentinvoke.GrantRequest, grantID, digest string, scopes agentinvoke.SkillScopes, authorities agentdelegation.SkillAuthorities, organizationScopeID string, epoch uint64) bool {
	return !g.Revoked && g.GrantID == grantID && g.UserID == request.UserID && g.Tenant == values.TenantId(request.TenantID) && g.AgentVersion == request.AgentVersion && g.TargetAgentID == request.TargetAgentID && g.InstallationID == request.InstallationID && g.TaskID == request.InvocationID && g.PlanSkillSetDigest == digest && g.Purpose == request.Purpose && g.OrganizationScopeID == organizationScopeID && g.ExpiresAt.Equal(request.ExpiresAt) && g.RevocationEpoch == epoch && sameSkills(g.SkillScopes, scopes) && sameStringSet(g.Skills, sortedKeys(scopes)) && sameSkillAuthorities(g.SkillAuthorities, authorities) && sameSkillAuthorities(g.Authority.SkillAuthorities, authorities)
}

func sameSkills(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for skill, scopes := range a {
		if !slices.Equal(sortedScopes(scopes), sortedScopes(b[skill])) {
			return false
		}
	}
	return true
}

func sameStringSet(a, b []string) bool {
	return slices.Equal(sortedScopes(a), sortedScopes(b))
}

func sameSkillAuthorities(a, b trust.SkillAuthorities) bool {
	if len(a) != len(b) {
		return false
	}
	for skill, left := range a {
		right, ok := b[skill]
		if !ok || !slices.Equal(sortedScopes(left.Capabilities), sortedScopes(right.Capabilities)) || !slices.Equal(sortedScopes(left.Resources), sortedScopes(right.Resources)) || !slices.Equal(sortedScopes(left.Fields), sortedScopes(right.Fields)) || !slices.Equal(sortedScopes(left.Purposes), sortedScopes(right.Purposes)) {
			return false
		}
	}
	return true
}

func sortedKeys(skills agentinvoke.SkillScopes) []string {
	keys := make([]string, 0, len(skills))
	for key := range skills {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func cloneSkillScopes(scopes map[string][]string) agentinvoke.SkillScopes {
	out := make(agentinvoke.SkillScopes, len(scopes))
	for key, values := range scopes {
		out[key] = slices.Clone(values)
	}
	return out
}
