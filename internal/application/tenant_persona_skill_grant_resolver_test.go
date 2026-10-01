package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaGrantProviderFake struct {
	tenant values.TenantId
	key    agentskills.SkillKey
	rows   []agentgate.SkillGrant
	err    error
	calls  int
}

func (f *personaGrantProviderFake) Grants(_ context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]agentgate.SkillGrant, error) {
	f.calls++
	if tenant != f.tenant || key != f.key {
		return nil, errors.New("unexpected tenant or skill")
	}
	return append([]agentgate.SkillGrant(nil), f.rows...), f.err
}

func TestTodo_AGENTP_018_TenantSkillGrantResolverPreservesTuples(t *testing.T) {
	key := agentskills.SkillKey{ID: "skill.persona", Version: 3}
	grants := &personaGrantProviderFake{tenant: "tenant-a", key: key, rows: []agentgate.SkillGrant{
		{ID: "grant-b", Tenant: "tenant-a", Skill: key, Roles: []string{"auditor"}, Population: "contractors", OrganizationScopes: []string{"org-east"}, Purposes: []string{"persona-chat"}},
		{ID: "grant-a", Tenant: "tenant-a", Skill: key, Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-west"}, Purposes: []string{"persona-chat"}},
	}}
	resolver, err := NewTenantPersonaSkillGrantResolver(grants, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	pin := agentskills.SkillPin{ID: key.ID, Version: key.Version, Digest: "digest"}
	valid := agentpersona.Audience{Roles: []string{"manager"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-west"}}
	if allowed, err := resolver.AllowsAudience(pin, valid); err != nil || !allowed {
		t.Fatalf("row-covered audience = %v, %v", allowed, err)
	}
	widened := agentpersona.Audience{Roles: []string{"manager", "auditor"}, Populations: []string{"employees", "contractors"}, OrganizationScopes: []string{"org-west", "org-east"}}
	if allowed, err := resolver.AllowsAudience(pin, widened); err != nil || allowed {
		t.Fatalf("cross-product audience assembled from diagonal tuples = %v, %v; want denied", allowed, err)
	}
	ungranted := valid
	ungranted.OrganizationScopes = []string{"org-central"}
	if allowed, err := resolver.AllowsAudience(pin, ungranted); err != nil || allowed {
		t.Fatalf("ungranted organization audience = %v, %v", allowed, err)
	}
	if grants.calls != 3 {
		t.Fatalf("current grant reads = %d, want 3", grants.calls)
	}
}

func TestTodo_AGENTP_018_TenantSkillGrantResolverRejectsCrossTenantAndUsesSingleRowFallback(t *testing.T) {
	key := agentskills.SkillKey{ID: "skill.persona", Version: 1}
	provider := &personaGrantProviderFake{tenant: "tenant-a", key: key, rows: []agentgate.SkillGrant{
		{ID: "grant-z", Tenant: "tenant-a", Skill: key, Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-west"}, Purposes: []string{"chat"}},
		{ID: "grant-a", Tenant: "tenant-a", Skill: key, Roles: []string{"auditor"}, Population: "contractors", OrganizationScopes: []string{"org-east"}, Purposes: []string{"chat"}},
		{ID: "foreign", Tenant: "tenant-b", Skill: key, Roles: []string{"ceo"}, Population: "employees", OrganizationScopes: []string{"org-west"}, Purposes: []string{"chat"}},
	}}
	resolver, err := NewTenantPersonaSkillGrantResolver(provider, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := resolver.AudienceGrant(agentskills.SkillPin{ID: key.ID, Version: key.Version, Digest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fallback.Roles) != 1 || fallback.Roles[0] != "auditor" || len(fallback.Populations) != 1 || fallback.Populations[0] != "contractors" || len(fallback.OrganizationScopes) != 1 || fallback.OrganizationScopes[0] != "org-east" {
		t.Fatalf("fallback projection merged distinct tuples: %+v", fallback)
	}
	crossTenant, err := NewTenantPersonaSkillGrantResolver(provider, values.TenantId("tenant-b"))
	if err != nil {
		t.Fatalf("valid tenant binding rejected: %v", err)
	}
	allowed, err := crossTenant.AllowsAudience(agentskills.SkillPin{ID: key.ID, Version: key.Version, Digest: "digest"}, agentpersona.Audience{
		Roles: []string{"ceo"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-west"},
	})
	if allowed || !errors.Is(err, errTenantPersonaSkillGrant) {
		t.Fatalf("provider tenant mismatch returned %v, %v; want fail closed", allowed, err)
	}
}

func TestTodo_AGENTP_018_TenantSkillGrantResolverFailsClosedOnStoreError(t *testing.T) {
	key := agentskills.SkillKey{ID: "skill.persona", Version: 1}
	provider := &personaGrantProviderFake{tenant: "tenant-a", key: key, err: errors.New("database unavailable")}
	resolver, err := NewTenantPersonaSkillGrantResolver(provider, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := resolver.AllowsAudience(agentskills.SkillPin{ID: key.ID, Version: key.Version, Digest: "digest"}, agentpersona.Audience{Roles: []string{"manager"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-west"}})
	if allowed || !errors.Is(err, errTenantPersonaSkillGrant) {
		t.Fatalf("store failure returned %v, %v", allowed, err)
	}
}
