package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type requestGrantSource struct {
	invoker   application.TrustedInvoker
	admission agentinvoke.Admission
}

func (s requestGrantSource) ResolveTrustedInvoker(context.Context, string, string) (application.TrustedInvoker, error) {
	return s.invoker, nil
}

func (s requestGrantSource) ResolvePersonaAuthority(context.Context, agentinvoke.AdmissionRequest, application.TrustedInvoker) (agentinvoke.Admission, error) {
	return s.admission, nil
}

type requestGrantInvokerAuthority struct{ authority agentdelegation.UserAuthority }

func (s requestGrantInvokerAuthority) ResolveInvokerAuthority(context.Context, string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
	return s.authority, nil
}

type requestGrantStoreFactory struct {
	store  *agentdelegation.MemoryGrantStore
	ctx    context.Context
	tenant values.TenantId
	calls  int
}

func (f *requestGrantStoreFactory) ForTenant(ctx context.Context, tenant values.TenantId) (agentdelegation.GrantStore, error) {
	f.calls++
	f.ctx, f.tenant = ctx, tenant
	return f.store, nil
}

func requestGrantPrincipal(t *testing.T, tenant values.TenantId, subject string, kind trust.SubjectKind, now time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: subject, SubjectKind: kind,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-1", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "cred:request-grant",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func requestGrantIssuer(t *testing.T, now time.Time, factory *requestGrantStoreFactory) *application.RequestScopedPersonaGrantIssuer {
	t.Helper()
	authority := agentdelegation.UserAuthority{
		UserID: "user-1", Active: true,
		Authority: trust.AuthorityScope{
			Tenant: "tenant-a", OrganizationScopeID: "org-a", Assurance: trust.AssuranceSubstantial,
			NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(24 * time.Hour),
			SkillAuthorities: trust.SkillAuthorities{"read": {Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Fields: []string{"name"}, Purposes: []string{"persona-mention"}}},
		},
		SkillAuthorities: trust.SkillAuthorities{"read": {Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Fields: []string{"name"}, Purposes: []string{"persona-mention"}}},
	}
	source := requestGrantSource{
		invoker: application.TrustedInvoker{ID: "user-1", TenantID: "tenant-a", HumanMember: true, AudienceMember: true, Discoverable: agentinvoke.SkillScopes{"read": {"worker.read"}}},
		admission: agentinvoke.Admission{
			Persona:      agentinvoke.Persona{ID: "persona-1", Version: "v1", Current: true, PinnedSkills: agentinvoke.SkillScopes{"read": {"worker.read"}}},
			Installation: agentinvoke.Installation{ID: "install-1", Current: true, SkillCeiling: agentinvoke.SkillScopes{"read": {"worker.read"}}},
			Channel:      agentinvoke.ChannelPolicy{SkillCeiling: agentinvoke.SkillScopes{"read": {"worker.read"}}},
			Discoverable: agentinvoke.SkillScopes{"read": {"worker.read"}}, HumanMember: true, AudienceMember: true, PersonaInstalled: true,
		},
	}
	issuer, err := application.NewRequestScopedPersonaGrantIssuer(application.RequestScopedPersonaGrantIssuerConfig{
		Personas: source, Identity: source, Invokers: requestGrantInvokerAuthority{authority: authority}, Stores: factory, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new request-scoped issuer: %v", err)
	}
	return issuer
}

func requestGrantRequest() agentinvoke.GrantRequest {
	return agentinvoke.GrantRequest{
		InvocationID: "invocation-1", UserID: "user-1", TenantID: "tenant-a", AgentVersion: "v1",
		TargetAgentID: "agent:persona-1", PersonaID: "persona-1", PersonaVersion: "v1", InstallationID: "install-1",
		ConversationID: "conversation-1", Purpose: "persona-mention", Skills: agentinvoke.SkillScopes{"read": {"worker.read"}},
		Mode: agentinvoke.OnBehalfOf,
	}
}

func TestTodo_AGENTP_008_Security_RequestScopedGrantPreservesBindings(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	factory := &requestGrantStoreFactory{store: agentdelegation.NewMemoryGrantStore()}
	issuer := requestGrantIssuer(t, now, factory)
	ctx := trust.WithPrincipal(context.Background(), requestGrantPrincipal(t, "tenant-a", "user-1", trust.SubjectKindHuman, now))
	request := requestGrantRequest()
	request.ExpiresAt = now.Add(time.Hour)

	grant, err := issuer.CreateOnBehalfOfGrant(ctx, request)
	if err != nil {
		t.Fatalf("CreateOnBehalfOfGrant: %v", err)
	}
	if factory.calls != 1 || factory.tenant != "tenant-a" || factory.ctx != ctx {
		t.Fatalf("tenant store binding = calls %d tenant %q same context %t", factory.calls, factory.tenant, factory.ctx == ctx)
	}
	if grant.UserID != "user-1" || grant.TenantID != "tenant-a" || grant.TaskID != request.InvocationID || grant.TargetAgentID != request.TargetAgentID {
		t.Fatalf("grant lost request identity: %#v", grant)
	}
	stored, err := factory.store.Get(grant.ID)
	if err != nil {
		t.Fatalf("read stored grant: %v", err)
	}
	if stored.Tenant != "tenant-a" || stored.TargetAgentID != request.TargetAgentID || stored.Authority.Delegate != request.TargetAgentID || stored.TaskID != request.InvocationID {
		t.Fatalf("persisted target binding = %#v", stored)
	}
	if epoch := factory.store.CurrentRevocationEpoch("tenant-a", "user-1"); epoch == 0 || stored.RevocationEpoch != epoch {
		t.Fatalf("persisted epoch %d differs from current epoch %d", stored.RevocationEpoch, epoch)
	}
}

func TestTodo_AGENTP_008_Security_RequestScopedGrantRejectsUnboundIdentity(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		ctx    func(*testing.T) context.Context
		mutate func(*agentinvoke.GrantRequest)
		want   error
	}{
		{name: "missing principal", ctx: func(*testing.T) context.Context { return context.Background() }, want: application.ErrRequestScopedPersonaGrantIssuer},
		{name: "agent principal", ctx: func(t *testing.T) context.Context {
			return trust.WithPrincipal(context.Background(), requestGrantPrincipal(t, "tenant-a", "user-1", trust.SubjectKindAgent, now))
		}, want: application.ErrRequestScopedPersonaGrantIssuer},
		{name: "different subject", ctx: func(t *testing.T) context.Context {
			return trust.WithPrincipal(context.Background(), requestGrantPrincipal(t, "tenant-a", "someone-else", trust.SubjectKindHuman, now))
		}, want: application.ErrRequestScopedPersonaGrantIssuer},
		{name: "different tenant", ctx: func(t *testing.T) context.Context {
			return trust.WithPrincipal(context.Background(), requestGrantPrincipal(t, "tenant-b", "user-1", trust.SubjectKindHuman, now))
		}, want: application.ErrRequestScopedPersonaGrantIssuer},
		{name: "missing target", ctx: func(t *testing.T) context.Context {
			return trust.WithPrincipal(context.Background(), requestGrantPrincipal(t, "tenant-a", "user-1", trust.SubjectKindHuman, now))
		}, mutate: func(r *agentinvoke.GrantRequest) { r.TargetAgentID = " " }, want: agentinvoke.ErrInvalidRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factory := &requestGrantStoreFactory{store: agentdelegation.NewMemoryGrantStore()}
			issuer := requestGrantIssuer(t, now, factory)
			request := requestGrantRequest()
			if tc.mutate != nil {
				tc.mutate(&request)
			}
			if _, err := issuer.CreateOnBehalfOfGrant(tc.ctx(t), request); !errors.Is(err, tc.want) {
				t.Fatalf("CreateOnBehalfOfGrant error = %v, want %v", err, tc.want)
			}
			if factory.calls != 0 {
				t.Fatalf("tenant store accessed %d times before identity validation", factory.calls)
			}
		})
	}
}

func TestTodo_AGENTP_008_Security_RequestScopedGrantRejectsCanceledRequest(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	factory := &requestGrantStoreFactory{store: agentdelegation.NewMemoryGrantStore()}
	issuer := requestGrantIssuer(t, now, factory)
	base := trust.WithPrincipal(context.Background(), requestGrantPrincipal(t, "tenant-a", "user-1", trust.SubjectKindHuman, now))
	ctx, cancel := context.WithCancel(base)
	cancel()
	_, err := issuer.CreateOnBehalfOfGrant(ctx, requestGrantRequest())
	if err == nil || factory.calls != 0 {
		t.Fatalf("canceled request error=%v store calls=%d", err, factory.calls)
	}
}
