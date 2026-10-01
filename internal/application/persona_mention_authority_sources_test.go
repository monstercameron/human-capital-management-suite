package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type personaMentionScopeSourceFake struct{ calls int }

func (f *personaMentionScopeSourceFake) ResolvePersonaScopes(context.Context, values.TenantId, agentpersona.PersonaProfile, agentpersonastore.ActiveInstallation, agentpersonastore.ChannelPolicy) (agentinvoke.SkillScopes, agentinvoke.SkillScopes, agentinvoke.SkillScopes, error) {
	f.calls++
	scope := agentinvoke.SkillScopes{"people.read": {"workers:read"}}
	return scope, scope, scope, nil
}

type personaMentionAudienceFake struct {
	conversations []PersonaAudienceConversation
	err           error
}

type personaMentionPersonaStoreFake struct{}

func (personaMentionPersonaStoreFake) ForTenant(context.Context, values.TenantId) (PersonaAuthorityInstallationReader, error) {
	return nil, errors.New("unused")
}

func (f personaMentionAudienceFake) ListCurrentPersonaAudience(context.Context, string, string) ([]PersonaAudienceConversation, error) {
	return f.conversations, f.err
}

func (personaMentionAudienceFake) ListCurrentPersonaInstallations(context.Context, string, string) ([]PersonaAudienceInstallation, error) {
	return nil, nil
}

type personaMentionSkillDiscovererFake struct {
	records []agentskills.SkillRecord
	err     error
}

func (f personaMentionSkillDiscovererFake) Discover(context.Context, *trust.Principal, string) ([]agentskills.SkillRecord, error) {
	return f.records, f.err
}

func personaMentionAuthorityContext(t *testing.T, tenant, subject string) context.Context {
	t.Helper()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-1", AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-1", IssuedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-1", Purposes: []string{"persona-mention"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

func TestTodo_AGENTP_008_PersonaMentionMembershipUsesCurrentVerifiedAudience(t *testing.T) {
	cases := []struct {
		name          string
		ctx           context.Context
		tenant        string
		subject       string
		conversations []PersonaAudienceConversation
		want          bool
		wantErr       bool
	}{
		{
			name: "current member",
			ctx:  personaMentionAuthorityContext(t, "tenant-a", "alice"), tenant: "tenant-a", subject: "alice",
			conversations: []PersonaAudienceConversation{{TenantID: "tenant-a", ConversationID: "room-1", Members: []PersonaAudienceMember{{SubjectID: "alice", Roles: []string{"employee"}, Populations: []string{"employees"}, OrganizationScope: "org-1"}}}}, want: true,
		},
		{
			name: "not a member", ctx: personaMentionAuthorityContext(t, "tenant-a", "alice"), tenant: "tenant-a", subject: "alice",
			conversations: []PersonaAudienceConversation{{TenantID: "tenant-a", ConversationID: "room-1", Members: []PersonaAudienceMember{{SubjectID: "bob", Roles: []string{"employee"}, Populations: []string{"employees"}, OrganizationScope: "org-1"}}}},
		},
		{
			name: "wrong tenant", ctx: personaMentionAuthorityContext(t, "tenant-a", "alice"), tenant: "tenant-b", subject: "alice", wantErr: true,
		},
		{
			name: "incomplete directory facts", ctx: personaMentionAuthorityContext(t, "tenant-a", "alice"), tenant: "tenant-a", subject: "alice",
			conversations: []PersonaAudienceConversation{{TenantID: "tenant-a", ConversationID: "room-1", Members: []PersonaAudienceMember{{SubjectID: "alice", Roles: []string{"employee"}}}}}, wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := currentPersonaMentionMembership{audience: personaMentionAudienceFake{conversations: tc.conversations}}
			got, err := source.ResolveCurrentMembership(tc.ctx, tc.tenant, tc.subject)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ResolveCurrentMembership error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("ResolveCurrentMembership = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestTodo_AGENTP_008_PersonaMentionSkillsPreserveExactPerSkillScopes(t *testing.T) {
	ctx := personaMentionAuthorityContext(t, "tenant-a", "alice")
	principal, _ := trust.FromContext(ctx)
	source := currentPersonaMentionSkills{skills: personaMentionSkillDiscovererFake{records: []agentskills.SkillRecord{
		personaMentionSkillRecord("people.read", "workers:read"),
		personaMentionSkillRecord("documents.search", "documents:search"),
	}}}
	got, err := source.ResolveInvokerSkills(ctx, principal, "persona-mention")
	if err != nil {
		t.Fatal(err)
	}
	want := agentinvoke.SkillScopes{"people.read": {"workers:read"}, "documents.search": {"documents:search"}}
	if len(got) != len(want) || !agentinvoke.SkillScopesSubset(got, want) || !agentinvoke.SkillScopesSubset(want, got) {
		t.Fatalf("ResolveInvokerSkills = %#v, want %#v", got, want)
	}
	got["people.read"][0] = "mutated"
	if source.skills.(personaMentionSkillDiscovererFake).records[0].ResolvedOperations[0].Capability.Definition.AuthZScopeRef == "mutated" {
		t.Fatal("resolved scope aliased the skill registry result")
	}
}

func TestTodo_AGENTP_008_PersonaMentionSkillsRejectIncompleteOrMismatchedSources(t *testing.T) {
	ctx := personaMentionAuthorityContext(t, "tenant-a", "alice")
	principal, _ := trust.FromContext(ctx)
	cases := []struct {
		name string
		ctx  context.Context
		who  *trust.Principal
		fake personaMentionSkillDiscovererFake
	}{
		{name: "missing verified principal", ctx: context.Background(), who: principal, fake: personaMentionSkillDiscovererFake{records: []agentskills.SkillRecord{personaMentionSkillRecord("read", "workers:read")}}},
		{name: "no current skills", ctx: ctx, who: principal},
		{name: "retired skill", ctx: ctx, who: principal, fake: personaMentionSkillDiscovererFake{records: []agentskills.SkillRecord{{Definition: agentskills.SkillDefinition{ID: "read"}, Status: agentskills.StatusRetired}}}},
		{name: "scope missing", ctx: ctx, who: principal, fake: personaMentionSkillDiscovererFake{records: []agentskills.SkillRecord{{Definition: agentskills.SkillDefinition{ID: "read"}, Status: agentskills.StatusActive}}}},
		{name: "discover error", ctx: ctx, who: principal, fake: personaMentionSkillDiscovererFake{err: errors.New("policy unavailable")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (currentPersonaMentionSkills{skills: tc.fake}).ResolveInvokerSkills(tc.ctx, tc.who, "persona-mention")
			if err == nil {
				t.Fatal("ResolveInvokerSkills unexpectedly accepted incomplete authority")
			}
		})
	}
}

func TestTodo_AGENTP_008_NewPersonaMentionAuthoritySourcesRequiresEveryTrustedProvider(t *testing.T) {
	valid := PersonaMentionAuthoritySourcesConfig{
		Audience: personaMentionAudienceFake{}, Personas: personaMentionPersonaStoreFake{}, Skills: personaMentionSkillDiscovererFake{},
		Gate: &agentgate.Gate{}, Catalog: personaMentionSkillCatalogFake{}, Discovery: personaMentionDiscoveryFake{}, Purpose: "persona-mention",
	}
	if _, err := NewPersonaMentionAuthoritySources(valid); err != nil {
		t.Fatalf("complete composition error = %v", err)
	}
	cases := []struct {
		name string
		edit func(*PersonaMentionAuthoritySourcesConfig)
	}{
		{name: "audience", edit: func(c *PersonaMentionAuthoritySourcesConfig) { c.Audience = nil }},
		{name: "persona store", edit: func(c *PersonaMentionAuthoritySourcesConfig) { c.Personas = nil }},
		{name: "skill discovery", edit: func(c *PersonaMentionAuthoritySourcesConfig) { c.Skills = nil }},
		{name: "gate", edit: func(c *PersonaMentionAuthoritySourcesConfig) { c.Gate = nil }},
		{name: "catalog", edit: func(c *PersonaMentionAuthoritySourcesConfig) { c.Catalog = nil }},
		{name: "current policy context", edit: func(c *PersonaMentionAuthoritySourcesConfig) { c.Discovery = nil }},
		{name: "purpose", edit: func(c *PersonaMentionAuthoritySourcesConfig) { c.Purpose = " " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := valid
			tc.edit(&candidate)
			if _, err := NewPersonaMentionAuthoritySources(candidate); err == nil {
				t.Fatal("constructor unexpectedly accepted incomplete authorities")
			}
		})
	}
}

func TestTodo_AGENTP_008_PersonaMentionAuthoritySourcesReuseComposedAgentSkills(t *testing.T) {
	current := personaMentionDiscoveryFake{}
	skills, err := NewAgentSkillSource(personaMentionSkillCatalogFake{}, agentgate.StaticGrants(nil), current)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := NewPersonaMentionAuthoritySourcesFromAgentSkills(personaMentionAudienceFake{}, personaMentionPersonaStoreFake{}, skills, "persona-mention")
	if err != nil {
		t.Fatalf("NewPersonaMentionAuthoritySourcesFromAgentSkills error = %v", err)
	}
	if sources == nil || sources.Personas == nil || sources.Identity == nil || sources.Invokers == nil {
		t.Fatalf("authority sources = %+v, want complete composition", sources)
	}
	if _, err := NewPersonaMentionAuthoritySourcesFromAgentSkills(personaMentionAudienceFake{}, personaMentionPersonaStoreFake{}, nil, "persona-mention"); err == nil {
		t.Fatal("nil skill runtime unexpectedly composed persona authority")
	}
}

func TestTodo_AGENTP_008_PersonaMentionScopesEnforceExactChannelClassAndExternalPolicy(t *testing.T) {
	cases := []struct {
		name      string
		profile   agentpersona.PersonaProfile
		class     agentpersonastore.ConversationClass
		policy    agentpersonastore.ChannelPolicy
		wantCalls int
	}{
		{name: "private channel allowed", profile: agentpersona.PersonaProfile{ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}}, class: agentpersonastore.ConversationPrivate, policy: agentpersonastore.ChannelPolicy{AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}}, wantCalls: 1},
		{name: "class absent from channel policy", profile: agentpersona.PersonaProfile{ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}}, class: agentpersonastore.ConversationPrivate, policy: agentpersonastore.ChannelPolicy{}},
		{name: "class absent from persona", profile: agentpersona.PersonaProfile{}, class: agentpersonastore.ConversationPrivate, policy: agentpersonastore.ChannelPolicy{AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}}},
		{name: "external policy denied", profile: agentpersona.PersonaProfile{ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelExternal}}, class: agentpersonastore.ConversationExternal, policy: agentpersonastore.ChannelPolicy{AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationExternal}}},
		{name: "external policy allowed", profile: agentpersona.PersonaProfile{ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelExternal}}, class: agentpersonastore.ConversationExternal, policy: agentpersonastore.ChannelPolicy{AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationExternal}, AllowExternalMembers: true}, wantCalls: 1},
		{name: "cross-company class is not represented by persona contract", profile: agentpersona.PersonaProfile{ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}}, class: agentpersonastore.ConversationCrossCompany, policy: agentpersonastore.ChannelPolicy{AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationCrossCompany}, AllowCrossCompanyMembers: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := &personaMentionScopeSourceFake{}
			resolver := personaMentionChannelPolicyScopes{next: next}
			_, _, _, err := resolver.ResolvePersonaScopes(context.Background(), "tenant-a", tc.profile,
				agentpersonastore.ActiveInstallation{ConversationClass: tc.class}, tc.policy)
			if tc.wantCalls == 0 && err == nil {
				t.Fatal("channel policy mismatch unexpectedly resolved scopes")
			}
			if tc.wantCalls == 1 && err != nil {
				t.Fatalf("ResolvePersonaScopes error = %v", err)
			}
			if next.calls != tc.wantCalls {
				t.Fatalf("downstream resolver calls = %d, want %d", next.calls, tc.wantCalls)
			}
		})
	}
}

type personaMentionSkillCatalogFake struct{}

func (personaMentionSkillCatalogFake) List() []agentskills.SkillRecord { return nil }
func (personaMentionSkillCatalogFake) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	return agentskills.SkillRecord{}, errors.New("unused")
}

type personaMentionDiscoveryFake struct{}

func (personaMentionDiscoveryFake) Resolve(context.Context, *trust.Principal, string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error) {
	return agentgate.UserContext{}, nil, nil, errors.New("unused")
}

func personaMentionSkillRecord(id, scope string) agentskills.SkillRecord {
	return agentskills.SkillRecord{
		Definition: agentskills.SkillDefinition{ID: id, Version: 1}, Digest: "digest-" + id, Status: agentskills.StatusActive,
		ResolvedOperations: []agentskills.ResolvedOperation{{HasCapability: true, Capability: capability.Record{Definition: capability.Definition{ID: "cap." + id, Version: 1, AuthZScopeRef: scope}}}},
	}
}

var _ agentgate.SkillCatalog = personaMentionSkillCatalogFake{}
