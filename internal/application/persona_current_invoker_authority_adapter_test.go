package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type trustedSkillMembershipFake bool

func (f trustedSkillMembershipFake) ResolveCurrentMembership(context.Context, string, string) (bool, error) {
	return bool(f), nil
}

type trustedSkillDiscoveryFake struct{ skills map[string][]string }

func (f trustedSkillDiscoveryFake) ResolveInvokerSkills(context.Context, *trust.Principal, string) (agentinvoke.SkillScopes, error) {
	out := make(agentinvoke.SkillScopes, len(f.skills))
	for skill, scopes := range f.skills {
		out[skill] = append([]string(nil), scopes...)
	}
	return out, nil
}

type trustedSkillProjectionFake struct {
	authority agentdelegation.UserAuthority
	err       error
}

func (f trustedSkillProjectionFake) ResolveInvokerAuthority(context.Context, string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
	return f.authority, f.err
}

func TestTrustedSkillInvokerAuthority_IntersectsPerSkillWithoutCrossProduct(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-a", AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, Purposes: []string{"persona-mention"},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), SessionRef: "session-a", CredentialDigest: "digest-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	invoker, err := NewServerTrustedInvokerSource(TrustedInvokerSourceConfig{
		Membership: trustedSkillMembershipFake(true),
		Skills:     trustedSkillDiscoveryFake{skills: map[string][]string{"people.read": {"scope:people"}, "time.read": {"scope:time"}}},
		Purpose:    "persona-mention",
	})
	if err != nil {
		t.Fatal(err)
	}
	projected := trust.SkillAuthorities{
		"people.read": {Capabilities: []string{"scope:people", "scope:pay"}, Resources: []string{"worker:alice"}, Fields: []string{"job_title"}, Purposes: []string{"persona-mention"}},
		"time.read":   {Capabilities: []string{"scope:time"}, Resources: []string{"worker:alice"}, Fields: []string{"schedule"}, Purposes: []string{"persona-mention"}},
	}
	projection := trust.AuthorityScope{Tenant: "tenant-a", OrganizationScopeID: "org-a", SkillAuthorities: trust.CloneSkillAuthorities(projected)}
	source, err := NewTrustedSkillInvokerAuthoritySource(invoker, trustedSkillProjectionFake{authority: agentdelegation.UserAuthority{
		UserID: "alice", Active: true, Authority: projection, SkillAuthorities: projected,
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.ResolveInvokerAuthority(ctx, "alice", "tenant-a", "persona-mention", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SkillAuthorities) != 2 || len(got.SkillAuthorities["people.read"].Capabilities) != 1 || got.SkillAuthorities["people.read"].Capabilities[0] != "scope:people" {
		t.Fatalf("people skill authority = %#v", got.SkillAuthorities["people.read"])
	}
	if len(got.SkillAuthorities["time.read"].Capabilities) != 1 || got.SkillAuthorities["time.read"].Capabilities[0] != "scope:time" {
		t.Fatalf("time skill authority = %#v", got.SkillAuthorities["time.read"])
	}
	if len(got.Authority.Capabilities) != 2 || len(got.Authority.SkillAuthorities) != 2 {
		t.Fatalf("aggregate authority retained broader projection: %#v", got.Authority)
	}
}

func TestTrustedSkillInvokerAuthority_RejectsUntrustedOrMismatchedFacts(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-a", AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, Purposes: []string{"persona-mention"},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), SessionRef: "session-a", CredentialDigest: "digest-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	trusted, err := NewServerTrustedInvokerSource(TrustedInvokerSourceConfig{
		Membership: trustedSkillMembershipFake(false), Skills: trustedSkillDiscoveryFake{skills: map[string][]string{"people.read": {"scope:people"}}}, Purpose: "persona-mention",
	})
	if err != nil {
		t.Fatal(err)
	}
	baseAuthority := trust.SkillAuthorities{"people.read": {Capabilities: []string{"scope:people"}, Resources: []string{"worker:alice"}, Fields: []string{"job_title"}, Purposes: []string{"persona-mention"}}}
	projection := trust.AuthorityScope{Tenant: "tenant-a", OrganizationScopeID: "org-a", SkillAuthorities: trust.CloneSkillAuthorities(baseAuthority)}
	for _, tc := range []struct {
		name       string
		projection agentdelegation.UserAuthority
	}{
		{name: "membership revoked", projection: agentdelegation.UserAuthority{UserID: "alice", Active: true, Authority: projection, SkillAuthorities: baseAuthority}},
		{name: "unexpected skill", projection: agentdelegation.UserAuthority{UserID: "alice", Active: true, Authority: projection, SkillAuthorities: trust.SkillAuthorities{
			"secret.read": {Capabilities: []string{"scope:secret"}, Resources: []string{"worker:alice"}, Fields: []string{"salary"}, Purposes: []string{"persona-mention"}},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := NewTrustedSkillInvokerAuthoritySource(trusted, trustedSkillProjectionFake{authority: tc.projection})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := source.ResolveInvokerAuthority(ctx, "alice", "tenant-a", "persona-mention", now); err == nil {
				t.Fatal("untrusted authority was accepted")
			}
		})
	}
	denied := trustedSkillProjectionFake{err: errors.New("policy unavailable")}
	source, err := NewTrustedSkillInvokerAuthoritySource(trusted, denied)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.ResolveInvokerAuthority(ctx, "alice", "tenant-a", "persona-mention", now); err == nil {
		t.Fatal("policy failure was accepted")
	}
}
