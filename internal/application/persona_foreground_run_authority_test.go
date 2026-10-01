package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type foregroundAuthorityResolverFake struct{}

type foregroundRequestSourceFunc func(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error)

func (f foregroundRequestSourceFunc) ResolvePersonaRun(ctx context.Context, request agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
	return f(ctx, request)
}

func (foregroundAuthorityResolverFake) Resolve(context.Context, agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	return agentinvoke.Admission{}, errors.New("unexpected authority resolution")
}

type foregroundGrantFactoryFake struct{}

func (foregroundGrantFactoryFake) ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error) {
	return nil, errors.New("unexpected grant lookup")
}

type foregroundPolicyFake struct{}

func (foregroundPolicyFake) IsBoundT0Run(context.Context, agentinvoke.RunRequest) (bool, error) {
	return false, errors.New("unexpected policy resolution")
}

type foregroundPositivePersona struct{ admission agentinvoke.Admission }

func (f foregroundPositivePersona) Resolve(context.Context, agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	return f.admission, nil
}

type foregroundPositiveStore struct {
	grant agentdelegation.Grant
	epoch uint64
}

func (s foregroundPositiveStore) Save(agentdelegation.Grant) error { return nil }
func (s foregroundPositiveStore) Get(string) (agentdelegation.Grant, error) {
	return s.grant, nil
}
func (s foregroundPositiveStore) Revoke(string, string) error { return nil }
func (s foregroundPositiveStore) CurrentRevocationEpoch(values.TenantId, string) uint64 {
	return s.epoch
}
func (s foregroundPositiveStore) BumpRevocationEpoch(values.TenantId, string, string) (uint64, error) {
	return s.epoch, nil
}

type foregroundPositiveGrantFactory struct{ store agentdelegation.GrantStore }

func (f foregroundPositiveGrantFactory) ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error) {
	return f.store, nil
}

type foregroundPositivePolicy struct{ seen agentinvoke.RunRequest }

func (p *foregroundPositivePolicy) IsBoundT0Run(_ context.Context, request agentinvoke.RunRequest) (bool, error) {
	p.seen = request
	return request.Grant.TargetAgentID == "agent-a" && len(request.Skills) == 1 && !request.Grant.ExpiresAt.IsZero(), nil
}

func foregroundPositiveGrant(now time.Time) agentdelegation.Grant {
	scopes := map[string][]string{"skill.read": {"scope:read"}}
	return agentdelegation.Grant{
		GrantID: "grant-a", UserID: "user-a", Tenant: values.TenantId("tenant-a"), AgentVersion: "7", TargetAgentID: "agent-a",
		InstallationID: "install-a", TaskID: "invoke-a", PlanSkillSetDigest: skillDigest(scopes), Purpose: "persona-mention", OrganizationScopeID: "org-a",
		Skills: []string{"skill.read"}, SkillScopes: scopes, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RevocationEpoch: 1,
		Authority: trust.DelegationGrant{GrantID: "grant-a", RootID: "grant-a", Kind: trust.GrantKindDirect, Delegator: "user-a", Delegate: "agent-a", Tenant: values.TenantId("tenant-a"), OrganizationScopeID: "org-a", Capabilities: []string{"scope:read"}, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), RevocationEpoch: 1},
	}
}

func foregroundFactsFromRequest(request agentrun.Request) PersonaRunRequestFacts {
	return PersonaRunRequestFacts{TenantID: request.Source.TenantID, LegalEntityID: request.LegalEntity, Agent: request.Agent,
		AgentPrincipal: request.Principal.AgentPrincipalID, PersonaDigest: request.Persona.Digest, Audience: request.Audience,
		Context: request.Context, Deadline: request.Deadline, Budget: request.Budget, TriggerID: request.Source.Key}
}

func TestTodo_AGENTP_008_ForegroundAuthority_acceptsCurrentDurableGrant(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	request := foregroundAuthorityRequest(now)
	grant := foregroundPositiveGrant(now)
	policy := &foregroundPositivePolicy{}
	authority := &PersonaForegroundRunAuthority{
		builder: &PersonaRunRequestBuilder{source: foregroundRequestSourceFunc(func(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
			return foregroundFactsFromRequest(request), nil
		})},
		persona: foregroundPositivePersona{admission: agentinvoke.Admission{
			Persona:      agentinvoke.Persona{ID: "persona-a", Version: "7", InstallationID: "install-a", Current: true},
			Installation: agentinvoke.Installation{ID: "install-a", Current: true}, Discoverable: map[string][]string{"skill.read": {"scope:read"}},
			HumanMember: true, AudienceMember: true, PersonaInstalled: true,
		}},
		grants: foregroundPositiveGrantFactory{store: foregroundPositiveStore{grant: grant, epoch: 1}}, policy: policy, now: func() time.Time { return now },
	}
	ctx := trust.WithPrincipal(context.Background(), foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), "persona-mention", "user-a", "tenant-a"))
	invocation := personaRunInvocation(request)
	bindForegroundGrant(&invocation, grant)
	built, buildErr := authority.builder.BuildPersonaChatAdmission(ctx, invocation)
	if buildErr != nil {
		t.Fatalf("builder: %v", buildErr)
	}
	if !personaForegroundRequestMatches(request, built, now) {
		t.Fatalf("built request differs: request=%+v built=%+v", request, built)
	}
	got, err := authority.VerifyAdmission(ctx, request)
	if err != nil {
		t.Fatalf("VerifyAdmission: %v", err)
	}
	if got.GrantRef != grant.GrantID || got.InstallationID != request.InstallationID || got.PolicyDigest == "" {
		t.Fatalf("snapshot = %+v, want current grant and policy proof", got)
	}
	if policy.seen.Grant.TargetAgentID != "agent-a" || policy.seen.Grant.ExpiresAt.IsZero() || len(policy.seen.Skills) != 1 {
		t.Fatalf("dynamic policy request = %+v, want durable grant bindings", policy.seen)
	}
	mutated := grant
	mutated.AgentVersion = request.Agent.Version
	denied := *authority
	denied.grants = foregroundPositiveGrantFactory{store: foregroundPositiveStore{grant: mutated, epoch: 1}}
	if _, err := denied.VerifyAdmission(ctx, request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("manifest version substituted for persona grant version: %v", err)
	}
}

func TestTodo_AGENTP_008_ForegroundAuthority_rejectsMalformedGrantBeforeOwnerFacts(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	request := foregroundAuthorityRequest(now)
	grant := foregroundPositiveGrant(now)
	grant.SkillScopes = nil
	grant.Skills = nil
	calls := 0
	authority := &PersonaForegroundRunAuthority{
		builder: &PersonaRunRequestBuilder{source: foregroundRequestSourceFunc(func(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
			calls++
			return foregroundFactsFromRequest(request), nil
		})},
		persona: foregroundAuthorityResolverFake{}, grants: foregroundPositiveGrantFactory{store: foregroundPositiveStore{grant: grant, epoch: 1}},
		policy: foregroundPolicyFake{}, now: func() time.Time { return now },
	}
	ctx := trust.WithPrincipal(context.Background(), foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), "persona-mention", "user-a", "tenant-a"))
	if _, err := authority.VerifyAdmission(ctx, request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("malformed grant error = %v, want refusal", err)
	}
	if calls != 0 {
		t.Fatalf("owner-facts source calls = %d, want zero before grant validation", calls)
	}
}

func TestTodo_AGENTP_008_ForegroundAuthority_requiresLiveMatchingHuman(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	authority := &PersonaForegroundRunAuthority{builder: &PersonaRunRequestBuilder{source: foregroundRequestSourceFunc(func(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
		return PersonaRunRequestFacts{}, errors.New("unexpected facts lookup")
	})}, persona: foregroundAuthorityResolverFake{}, grants: foregroundGrantFactoryFake{}, policy: foregroundPolicyFake{}, now: func() time.Time { return now }}
	request := foregroundAuthorityRequest(now)
	tests := []struct {
		name      string
		principal *trust.Principal
	}{
		{name: "missing principal"},
		{name: "expired principal", principal: foregroundPrincipal(t, now.Add(-2*time.Hour), now.Add(-time.Hour), "persona-mention", "user-a", "tenant-a")},
		{name: "wrong subject", principal: foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), "persona-mention", "user-b", "tenant-a")},
		{name: "wrong tenant", principal: foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), "persona-mention", "user-a", "tenant-b")},
		{name: "unauthorized purpose", principal: foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), "other-purpose", "user-a", "tenant-a")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.principal != nil {
				ctx = trust.WithPrincipal(ctx, tc.principal)
			}
			if _, err := authority.VerifyAdmission(ctx, request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
				t.Fatalf("VerifyAdmission error = %v, want authority refusal", err)
			}
		})
	}
}

func TestTodo_AGENTP_008_ForegroundAuthority_requiresCurrentOwnerFacts(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	authority := &PersonaForegroundRunAuthority{builder: &PersonaRunRequestBuilder{source: foregroundRequestSourceFunc(func(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
		return PersonaRunRequestFacts{}, errors.New("source unavailable")
	})}, persona: foregroundAuthorityResolverFake{}, grants: foregroundGrantFactoryFake{}, policy: foregroundPolicyFake{}, now: func() time.Time { return now }}
	request := foregroundAuthorityRequest(now)
	principal := foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), "persona-mention", "user-a", "tenant-a")
	ctx := trust.WithPrincipal(context.Background(), principal)
	if _, err := authority.VerifyAdmission(ctx, request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("VerifyAdmission error = %v, want owner-fact refusal", err)
	}
}

func TestTodo_AGENTP_008_ForegroundAuthority_pinsOwnerFactsAndAuthorityDigest(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	request := foregroundAuthorityRequest(now)
	current := request
	if !personaForegroundRequestMatches(request, current, now) {
		t.Fatal("identical current owner facts did not match")
	}
	changed := current
	changed.Context.Digest = foregroundDigest("changed")
	if personaForegroundRequestMatches(request, changed, now) {
		t.Fatal("changed thread snapshot was accepted")
	}
	changed = current
	changed.Budget.MaxOutputTokens--
	if personaForegroundRequestMatches(request, changed, now) {
		t.Fatal("lower current policy ceiling was accepted")
	}
	changed = current
	changed.Deadline = request.Deadline.Add(-time.Second)
	if personaForegroundRequestMatches(request, changed, now) {
		t.Fatal("shorter current policy deadline was accepted")
	}
	grant := agentdelegation.Grant{GrantID: "grant-a", PlanSkillSetDigest: "skills-digest"}
	first, err := personaForegroundPolicyDigest(request, grant, 4, "principal-a")
	if err != nil || !personaRunAuthorityDigest(first) {
		t.Fatalf("policy digest = %q, err = %v", first, err)
	}
	same, err := personaForegroundPolicyDigest(request, grant, 4, "principal-a")
	if err != nil || same != first {
		t.Fatalf("same authority proof changed digest: %q, %v", same, err)
	}
	changedDigest, err := personaForegroundPolicyDigest(request, grant, 5, "principal-a")
	if err != nil || changedDigest == first {
		t.Fatalf("changed grant epoch did not change digest: %q, %v", changedDigest, err)
	}
}

func foregroundAuthorityRequest(now time.Time) agentrun.Request {
	return agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourcePersonaMention, Key: "invoke-a", Ref: "post-a"},
		Persona: &agentrun.PersonaRef{ID: "persona-a", Version: "7", Digest: foregroundDigest("persona")}, LegalEntity: "entity-a",
		Agent: agentrun.VersionRef{AgentID: "agent-a", Version: "1", Digest: foregroundDigest("agent")}, InstallationID: "install-a",
		Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "service-a", InvokerID: "user-a", DelegatedCredentialRef: "grant-a"},
		Purpose:   "persona-mention", Audience: agentrun.AudienceScope{ID: "conversation-a", SnapshotID: "audience-snapshot", Digest: foregroundDigest("audience")},
		Context:  agentrun.ContextScope{ID: "thread-a", SnapshotID: "context-snapshot", Digest: foregroundDigest("context")},
		Deadline: now.Add(time.Hour), Budget: agentrun.Budget{MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1}, CauseID: "invoke-a"}
}

func foregroundDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func foregroundPrincipal(t *testing.T, issuedAt, expiresAt time.Time, purpose, subject, tenant string) *trust.Principal {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a",
		Purposes: []string{purpose}, IssuedAt: issuedAt, ExpiresAt: expiresAt, CredentialDigest: "credential-a"})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return principal
}
