package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type currentMembershipFake struct {
	current bool
	err     error
}

func (f currentMembershipFake) ResolveCurrentMembership(context.Context, string, string) (bool, error) {
	return f.current, f.err
}

type invokerSkillsFake struct {
	skills agentinvoke.SkillScopes
	err    error
}

func (f invokerSkillsFake) ResolveInvokerSkills(context.Context, *trust.Principal, string) (agentinvoke.SkillScopes, error) {
	return f.skills, f.err
}

func trustedInvokerContext(t *testing.T, tenant, subject string) context.Context {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-1", AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-1", IssuedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-1", Purposes: []string{"persona-mention"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func TestTodo_AGENTP_008_TrustedInvokerSourceUsesVerifiedCurrentFacts(t *testing.T) {
	source, err := application.NewServerTrustedInvokerSource(application.TrustedInvokerSourceConfig{
		Membership: currentMembershipFake{current: true},
		Skills:     invokerSkillsFake{skills: agentinvoke.SkillScopes{"people.read": {"worker:team"}}},
		Purpose:    "persona-mention",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.ResolveTrustedInvoker(trustedInvokerContext(t, "tenant-a", "alice"), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !got.HumanMember || !got.AudienceMember || got.ID != "alice" || got.TenantID != "tenant-a" {
		t.Fatalf("trusted invoker = %+v", got)
	}
	if !agentinvoke.SkillScopesSubset(got.Discoverable, agentinvoke.SkillScopes{"people.read": {"worker:team"}}) {
		t.Fatalf("discoverable skills = %#v", got.Discoverable)
	}
}

func TestTodo_AGENTP_008_TrustedInvokerSourceFailsClosedForForgedOrStaleFacts(t *testing.T) {
	cases := []struct {
		name   string
		ctx    context.Context
		tenant string
		mem    bool
		skil   agentinvoke.SkillScopes
	}{
		{name: "no principal", ctx: context.Background(), tenant: "tenant-a", mem: true, skil: agentinvoke.SkillScopes{"read": {"x"}}},
		{name: "tenant mismatch", ctx: trustedInvokerContext(t, "tenant-a", "alice"), tenant: "tenant-b", mem: true, skil: agentinvoke.SkillScopes{"read": {"x"}}},
		{name: "left chat", ctx: trustedInvokerContext(t, "tenant-a", "alice"), tenant: "tenant-a", mem: false, skil: agentinvoke.SkillScopes{"read": {"x"}}},
		{name: "no discovered skills", ctx: trustedInvokerContext(t, "tenant-a", "alice"), tenant: "tenant-a", mem: true, skil: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate, e := application.NewServerTrustedInvokerSource(application.TrustedInvokerSourceConfig{Membership: currentMembershipFake{current: tc.mem}, Skills: invokerSkillsFake{skills: tc.skil}, Purpose: "persona-mention"})
			if e != nil {
				t.Fatal(e)
			}
			_, e = candidate.ResolveTrustedInvoker(tc.ctx, tc.tenant, "alice")
			if e == nil || !errors.Is(e, application.ErrPersonaTrustedInvokerUnavailable) {
				t.Fatalf("error = %v", e)
			}
		})
	}
}

func TestTodo_AGENTP_008_InvokerAuthorityFailsClosedWithoutPerSkillContract(t *testing.T) {
	invoker, err := application.NewServerTrustedInvokerSource(application.TrustedInvokerSourceConfig{
		Membership: currentMembershipFake{current: true}, Skills: invokerSkillsFake{skills: agentinvoke.SkillScopes{"people.read": {"worker:team"}}}, Purpose: "persona-mention",
	})
	if err != nil {
		t.Fatal(err)
	}
	authority := &application.ServerInvokerAuthoritySource{Invoker: invoker}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if _, err := authority.ResolveInvokerAuthority(trustedInvokerContext(t, "tenant-a", "alice"), "alice", "tenant-a", "persona-mention", now); err == nil {
		t.Fatal("authority unexpectedly flattened per-skill scopes")
	}
	if _, err := authority.ResolveInvokerAuthority(trustedInvokerContext(t, "tenant-a", "alice"), "alice", "tenant-a", "other-purpose", now); err == nil {
		t.Fatal("purpose mismatch unexpectedly authorized")
	}
}
