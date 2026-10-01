package application

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func TestCurrentAuthorityProjection_ConstructorFailsClosed(t *testing.T) {
	if _, err := NewGateInvokerAuthoritySource(GateInvokerAuthoritySourceConfig{Purpose: "persona-mention"}); !errors.Is(err, errPersonaCurrentAuthorityProjection) {
		t.Fatalf("constructor error = %v", err)
	}
}

func TestCurrentAuthorityProjection_ResolveFailsClosedWithoutSources(t *testing.T) {
	var source *GateInvokerAuthoritySource
	if _, err := source.ResolveInvokerAuthority(nil, "alice", "tenant-a", "persona-mention", time.Now()); !errors.Is(err, errPersonaCurrentAuthorityProjection) {
		t.Fatalf("resolve error = %v", err)
	}
}

func TestCurrentAuthorityProjection_PreservesSkillDimensions(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	resource := values.EntityRef{Tenant: tenant, Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000001"}
	projection := agentgate.SkillAuthorizationProjection{
		Skill:   agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: "people.read", Version: 1}, ResolvedOperations: []agentskills.ResolvedOperation{{Capability: capability.Record{Definition: capability.Definition{ID: "people.read_worker", Version: 1, AuthZScopeRef: "scope:people.read"}}, HasCapability: true}}},
		Purpose: "persona-mention",
		Capabilities: []agentgate.CapabilityDecision{
			{Capability: capability.Key{ID: "people.read_worker", Version: 1}, Allowed: true, Subjects: []agentgate.SubjectDecision{{Subject: resource, Fields: map[authz.FieldID]authz.Effect{authz.FieldWorkerNumber: authz.EffectAllow, authz.FieldJobTitle: authz.EffectRedacted}}}},
		},
	}
	authority, ok := skillAuthorityFromProjection(projection, tenant)
	if !ok {
		t.Fatal("projection was rejected")
	}
	if len(authority.Capabilities) != 1 || authority.Capabilities[0] != "scope:people.read" {
		t.Fatalf("capabilities = %#v", authority.Capabilities)
	}
	if len(authority.Resources) != 1 || authority.Resources[0] != resource.String() {
		t.Fatalf("resources = %#v", authority.Resources)
	}
	if len(authority.Fields) != 1 || authority.Fields[0] != string(authz.FieldWorkerNumber) || authority.Purposes[0] != "persona-mention" {
		t.Fatalf("fields/purposes = %#v/%#v", authority.Fields, authority.Purposes)
	}
}

func TestCurrentAuthorityProjection_RejectsInvalidProjection(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	resource := values.EntityRef{Tenant: tenant, Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000001"}
	cases := []struct {
		name string
		edit func(*agentgate.SkillAuthorizationProjection)
	}{
		{name: "denied capability", edit: func(p *agentgate.SkillAuthorizationProjection) { p.Capabilities[0].Allowed = false }},
		{name: "foreign tenant resource", edit: func(p *agentgate.SkillAuthorizationProjection) {
			p.Capabilities[0].Subjects[0].Subject = values.EntityRef{Tenant: "tenant-b", Kind: resource.Kind, Id: resource.Id}
		}},
		{name: "missing capability", edit: func(p *agentgate.SkillAuthorizationProjection) { p.Capabilities = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projection := agentgate.SkillAuthorizationProjection{Purpose: "persona-mention", Capabilities: []agentgate.CapabilityDecision{{Capability: capability.Key{ID: "people.read_worker", Version: 1}, Allowed: true, Subjects: []agentgate.SubjectDecision{{Subject: resource, Fields: map[authz.FieldID]authz.Effect{authz.FieldWorkerNumber: authz.EffectAllow}}}}}}
			tc.edit(&projection)
			projection.Skill = agentskills.SkillRecord{ResolvedOperations: []agentskills.ResolvedOperation{{Capability: capability.Record{Definition: capability.Definition{ID: "people.read_worker", Version: 1, AuthZScopeRef: "scope:people.read"}}, HasCapability: true}}}
			if _, ok := skillAuthorityFromProjection(projection, tenant); ok {
				t.Fatal("invalid projection was accepted")
			}
		})
	}
}

func TestCurrentAuthorityProjection_OrganizationBinding(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-a", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, Purposes: []string{"persona-mention"}, IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), SessionRef: "session", CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := currentOrganizationScope(principal, []string{"org-b"}); ok || got != "org-a" {
		t.Fatalf("mismatched organization = %q, %v", got, ok)
	}
	if got, ok := currentOrganizationScope(principal, []string{"org-a"}); !ok || got != "org-a" {
		t.Fatalf("matching organization = %q, %v", got, ok)
	}
}
