package application_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaAuthoritySourceFake struct {
	invoker   application.TrustedInvoker
	admission agentinvoke.Admission
}

func (f personaAuthoritySourceFake) ResolveTrustedInvoker(context.Context, string, string) (application.TrustedInvoker, error) {
	return f.invoker, nil
}

func (f personaAuthoritySourceFake) ResolvePersonaAuthority(context.Context, agentinvoke.AdmissionRequest, application.TrustedInvoker) (agentinvoke.Admission, error) {
	return f.admission, nil
}

type invokerAuthoritySourceFake struct{ authority agentdelegation.UserAuthority }

func (f invokerAuthoritySourceFake) ResolveInvokerAuthority(context.Context, string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
	return f.authority, nil
}

func TestTodo_AGENTP_008_AuthorityIntersectsTrustedInvoker(t *testing.T) {
	source := personaAuthoritySourceFake{
		invoker: application.TrustedInvoker{ID: "user-1", TenantID: "tenant-a", HumanMember: true, AudienceMember: true, Discoverable: agentinvoke.SkillScopes{"read": {"worker", "salary"}, "other": {"x"}}},
		admission: agentinvoke.Admission{
			Persona:      agentinvoke.Persona{ID: "persona-1", Version: "v1", PinnedSkills: agentinvoke.SkillScopes{"read": {"worker", "salary"}}, Current: true},
			Installation: agentinvoke.Installation{ID: "install-1", Current: true, SkillCeiling: agentinvoke.SkillScopes{"read": {"worker"}}}, Channel: agentinvoke.ChannelPolicy{SkillCeiling: agentinvoke.SkillScopes{"read": {"worker"}}}, Discoverable: agentinvoke.SkillScopes{"read": {"worker", "salary"}}, HumanMember: true, AudienceMember: true, PersonaInstalled: true,
		},
	}
	a := &application.PersonaAuthorityAdapter{Personas: source, Identity: source}
	got, err := a.Resolve(context.Background(), agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: "conv-1", InvokerID: "user-1", PersonaID: "persona-1"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Discoverable) != 1 || len(got.Discoverable["read"]) != 1 || got.Discoverable["read"][0] != "worker" {
		t.Fatalf("effective skills = %#v", got.Discoverable)
	}
}

func TestTodo_AGENTP_008_GrantUsesCurrentUserAuthority(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	store := agentdelegation.NewMemoryGrantStore()
	userAuthority := agentdelegation.UserAuthority{UserID: "user-1", Active: true, Authority: trust.AuthorityScope{Tenant: values.TenantId("tenant-a"), OrganizationScopeID: "org-a", Capabilities: []string{"worker"}, Resources: []string{"worker:1"}, Fields: []string{"status"}, Purposes: []string{"persona-mention"}, SkillAuthorities: trust.SkillAuthorities{"read": {Capabilities: []string{"worker"}, Resources: []string{"worker:1"}, Fields: []string{"status"}, Purposes: []string{"persona-mention"}}}, Assurance: trust.AssuranceSubstantial, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(24 * time.Hour)}, SkillAuthorities: agentdelegation.SkillAuthorities{"read": {Capabilities: []string{"worker"}, Resources: []string{"worker:1"}, Fields: []string{"status"}, Purposes: []string{"persona-mention"}}}}
	service, err := agentdelegation.NewService(agentdelegation.Config{Store: store, Authority: agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		return userAuthority, nil
	}), Secret: []byte("0123456789abcdef"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new delegation service: %v", err)
	}
	personaSource := personaAuthoritySourceFake{invoker: application.TrustedInvoker{ID: "user-1", TenantID: "tenant-a", HumanMember: true, AudienceMember: true, Discoverable: agentinvoke.SkillScopes{"read": {"worker"}}}, admission: agentinvoke.Admission{Persona: agentinvoke.Persona{ID: "persona-1", Version: "v1", Current: true, PinnedSkills: agentinvoke.SkillScopes{"read": {"worker"}}}, Installation: agentinvoke.Installation{ID: "install-1", Current: true, SkillCeiling: agentinvoke.SkillScopes{"read": {"worker"}}}, Channel: agentinvoke.ChannelPolicy{SkillCeiling: agentinvoke.SkillScopes{"read": {"worker"}}}, Discoverable: agentinvoke.SkillScopes{"read": {"worker"}}, HumanMember: true, AudienceMember: true, PersonaInstalled: true}}
	a := &application.PersonaAuthorityAdapter{Personas: personaSource, Identity: personaSource, Invokers: invokerAuthoritySourceFake{authority: userAuthority}, Grants: service, Store: store, Now: func() time.Time { return now }}
	grant, err := a.CreateOnBehalfOfGrant(context.Background(), agentinvoke.GrantRequest{InvocationID: "inv-1", UserID: "user-1", TenantID: "tenant-a", ConversationID: "conv-1", AgentVersion: "v1", TargetAgentID: "agent:persona-1", PersonaID: "persona-1", PersonaVersion: "v1", InstallationID: "install-1", Purpose: "persona-mention", Skills: agentinvoke.SkillScopes{"read": {"worker"}}, Mode: agentinvoke.OnBehalfOf, ExpiresAt: now.Add(24 * time.Hour)})
	if err != nil {
		t.Fatalf("CreateOnBehalfOfGrant: %v", err)
	}
	if grant.UserID != "user-1" || grant.TenantID != "tenant-a" || grant.TaskID != "inv-1" || grant.TargetAgentID != "agent:persona-1" || len(grant.Skills["read"]) != 1 || grant.Skills["read"][0] != "worker" {
		t.Fatalf("grant = %#v", grant)
	}
	stored, err := store.Get(grant.ID)
	if err != nil || stored.AgentVersion != "v1" || stored.TargetAgentID != "agent:persona-1" || stored.Authority.Delegate != "agent:persona-1" {
		t.Fatalf("durable exact target = version %q, target %q, delegate %q, err %v", stored.AgentVersion, stored.TargetAgentID, stored.Authority.Delegate, err)
	}
	_, err = a.CreateOnBehalfOfGrant(context.Background(), agentinvoke.GrantRequest{InvocationID: "inv-2", UserID: "user-1", TenantID: "tenant-a", ConversationID: "conv-1", PersonaID: "persona-1", PersonaVersion: "v1", AgentVersion: "v1", InstallationID: "install-1", Purpose: "persona-mention", Skills: agentinvoke.SkillScopes{"read": {"salary"}}, Mode: agentinvoke.OnBehalfOf, ExpiresAt: now.Add(24 * time.Hour)})
	if err == nil || !errors.Is(err, agentdelegation.ErrScopeExpanded) {
		t.Fatalf("unauthorized scope error = %v", err)
	}
}

func TestTodo_AGENTP_008_ScopedGrantKeepsSkillResourcePairings(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	store := agentdelegation.NewMemoryGrantStore()
	authority := agentdelegation.UserAuthority{UserID: "user-1", Active: true, Authority: trust.AuthorityScope{Tenant: values.TenantId("tenant-a"), OrganizationScopeID: "org-a", SkillAuthorities: trust.SkillAuthorities{
		"read":  {Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Fields: []string{"name"}, Purposes: []string{"persona-mention"}},
		"write": {Capabilities: []string{"worker.write"}, Resources: []string{"worker:2"}, Fields: []string{"status"}, Purposes: []string{"persona-mention"}},
	}, Assurance: trust.AssuranceSubstantial, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(24 * time.Hour)}, SkillAuthorities: agentdelegation.SkillAuthorities{
		"read":  {Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Fields: []string{"name"}, Purposes: []string{"persona-mention"}},
		"write": {Capabilities: []string{"worker.write"}, Resources: []string{"worker:2"}, Fields: []string{"status"}, Purposes: []string{"persona-mention"}},
	}}
	service, err := agentdelegation.NewService(agentdelegation.Config{Store: store, Authority: agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		return authority, nil
	}), Secret: []byte("0123456789abcdef"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new delegation service: %v", err)
	}
	personaSource := personaAuthoritySourceFake{invoker: application.TrustedInvoker{ID: "user-1", TenantID: "tenant-a", HumanMember: true, AudienceMember: true, Discoverable: agentinvoke.SkillScopes{"read": {"worker.read"}, "write": {"worker.write"}}}, admission: agentinvoke.Admission{Persona: agentinvoke.Persona{ID: "persona-1", Version: "v1", Current: true, PinnedSkills: agentinvoke.SkillScopes{"read": {"worker.read"}, "write": {"worker.write"}}}, Installation: agentinvoke.Installation{ID: "install-1", Current: true, SkillCeiling: agentinvoke.SkillScopes{"read": {"worker.read"}, "write": {"worker.write"}}}, Channel: agentinvoke.ChannelPolicy{SkillCeiling: agentinvoke.SkillScopes{"read": {"worker.read"}, "write": {"worker.write"}}}, Discoverable: agentinvoke.SkillScopes{"read": {"worker.read"}, "write": {"worker.write"}}, HumanMember: true, AudienceMember: true, PersonaInstalled: true}}
	a := &application.PersonaAuthorityAdapter{Personas: personaSource, Identity: personaSource, Invokers: invokerAuthoritySourceFake{authority: authority}, Grants: service, Store: store, Now: func() time.Time { return now }}
	grant, err := a.CreateOnBehalfOfGrant(context.Background(), agentinvoke.GrantRequest{InvocationID: "inv-cross", UserID: "user-1", TenantID: "tenant-a", ConversationID: "conv-1", AgentVersion: "v1", PersonaID: "persona-1", PersonaVersion: "v1", InstallationID: "install-1", Purpose: "persona-mention", Skills: agentinvoke.SkillScopes{"read": {"worker.read"}, "write": {"worker.write"}}, Mode: agentinvoke.OnBehalfOf, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("CreateOnBehalfOfGrant: %v", err)
	}
	if got := grant.Skills["read"]; len(got) != 1 || got[0] != "worker.read" {
		t.Fatalf("read scopes = %#v", got)
	}
	stored, err := store.Get(grant.ID)
	if err != nil {
		t.Fatalf("get grant: %v", err)
	}
	if !slices.Equal(stored.SkillAuthorities["read"].Resources, []string{"worker:1"}) || !slices.Equal(stored.SkillAuthorities["write"].Resources, []string{"worker:2"}) {
		t.Fatalf("cross-product resources = %#v", stored.SkillAuthorities)
	}
	if _, err := a.CreateOnBehalfOfGrant(context.Background(), agentinvoke.GrantRequest{InvocationID: "inv-cross", UserID: "user-1", TenantID: "tenant-a", ConversationID: "conv-1", AgentVersion: "v1", PersonaID: "persona-1", PersonaVersion: "v1", InstallationID: "install-1", Purpose: "persona-mention", Skills: agentinvoke.SkillScopes{"read": {"worker.read"}, "write": {"worker.write"}}, Mode: agentinvoke.OnBehalfOf, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if _, err := a.CreateOnBehalfOfGrant(context.Background(), agentinvoke.GrantRequest{InvocationID: "inv-cross", UserID: "user-1", TenantID: "tenant-a", ConversationID: "conv-1", AgentVersion: "v2", PersonaID: "persona-1", PersonaVersion: "v1", InstallationID: "install-1", Purpose: "persona-mention", Skills: agentinvoke.SkillScopes{"read": {"worker.read"}, "write": {"worker.write"}}, Mode: agentinvoke.OnBehalfOf, ExpiresAt: now.Add(time.Hour)}); err == nil {
		t.Fatal("replay with changed agent version unexpectedly accepted")
	}
	if _, err := a.CreateOnBehalfOfGrant(context.Background(), agentinvoke.GrantRequest{InvocationID: "inv-cross", UserID: "user-1", TenantID: "tenant-a", ConversationID: "conv-1", AgentVersion: "v1", PersonaID: "persona-1", PersonaVersion: "v1", InstallationID: "install-1", Purpose: "persona-mention", Skills: agentinvoke.SkillScopes{"read": {"worker.read"}, "write": {"worker.write"}}, Mode: agentinvoke.OnBehalfOf, ExpiresAt: now.Add(2 * time.Hour)}); err == nil {
		t.Fatal("replay with changed expiry unexpectedly accepted")
	}
}

func TestTodo_AGENTP_008_RejectsFlatAuthorityAndExactSkillSubset(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	flat := agentdelegation.UserAuthority{UserID: "user-1", Active: true, Authority: trust.AuthorityScope{Tenant: values.TenantId("tenant-a"), OrganizationScopeID: "org-a", Assurance: trust.AssuranceSubstantial}}
	personaSource := personaAuthoritySourceFake{invoker: application.TrustedInvoker{ID: "user-1", TenantID: "tenant-a", HumanMember: true, AudienceMember: true, Discoverable: agentinvoke.SkillScopes{"read": {"worker"}, "write": {"worker"}}}, admission: agentinvoke.Admission{Persona: agentinvoke.Persona{ID: "persona-1", Version: "v1", Current: true, PinnedSkills: agentinvoke.SkillScopes{"read": {"worker"}, "write": {"worker"}}}, Installation: agentinvoke.Installation{ID: "install-1", Current: true, SkillCeiling: agentinvoke.SkillScopes{"read": {"worker"}, "write": {"worker"}}}, Channel: agentinvoke.ChannelPolicy{SkillCeiling: agentinvoke.SkillScopes{"read": {"worker"}, "write": {"worker"}}}, Discoverable: agentinvoke.SkillScopes{"read": {"worker"}, "write": {"worker"}}, HumanMember: true, AudienceMember: true, PersonaInstalled: true}}
	store := agentdelegation.NewMemoryGrantStore()
	service, err := agentdelegation.NewService(agentdelegation.Config{Store: store, Authority: agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		return flat, nil
	}), Secret: []byte("0123456789abcdef"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new delegation service: %v", err)
	}
	a := &application.PersonaAuthorityAdapter{Personas: personaSource, Identity: personaSource, Invokers: invokerAuthoritySourceFake{authority: flat}, Grants: service, Store: store, Now: func() time.Time { return now }}
	_, err = a.CreateOnBehalfOfGrant(context.Background(), agentinvoke.GrantRequest{InvocationID: "inv-flat", UserID: "user-1", TenantID: "tenant-a", ConversationID: "conv-1", AgentVersion: "v1", PersonaID: "persona-1", PersonaVersion: "v1", InstallationID: "install-1", Purpose: "persona-mention", Skills: agentinvoke.SkillScopes{"read": {"worker"}, "write": {"worker"}}, Mode: agentinvoke.OnBehalfOf, ExpiresAt: now.Add(time.Hour)})
	if err == nil {
		t.Fatal("flat authority unexpectedly accepted")
	}
}

func TestTodo_AGENTP_008_RejectsUntrustedInvokerBinding(t *testing.T) {
	a := &application.PersonaAuthorityAdapter{Personas: personaAuthoritySourceFake{invoker: application.TrustedInvoker{ID: "other", TenantID: "tenant-a"}}, Identity: personaAuthoritySourceFake{invoker: application.TrustedInvoker{ID: "other", TenantID: "tenant-a"}}}
	_, err := a.Resolve(context.Background(), agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: "conv", InvokerID: "user-1", PersonaID: "persona-1"})
	if err == nil {
		t.Fatalf("binding error = %v", err)
	}
}
