package agentskillgrantstore

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_AGENT2_005_GrantProviderCurrentTenantSkillAndRevocation(t *testing.T) {
	f := newGrantFixture(t, "grant-owner", "grant-other")
	owner := f.store(t, "grant-owner")
	other := f.store(t, "grant-other")
	ctx := context.Background()
	activeID := f.insert(t, "grant-owner", "active", "skill.read", 2, "CURRENT_TIMESTAMP - interval '1 hour'", "NULL", "NULL")
	f.insert(t, "grant-owner", "revoked", "skill.read", 2, "CURRENT_TIMESTAMP - interval '1 hour'", "NULL", "CURRENT_TIMESTAMP")
	f.insert(t, "grant-owner", "expired", "skill.read", 2, "CURRENT_TIMESTAMP - interval '2 hours'", "CURRENT_TIMESTAMP - interval '1 hour'", "NULL")
	f.insert(t, "grant-owner", "future", "skill.read", 2, "CURRENT_TIMESTAMP + interval '1 hour'", "NULL", "NULL")
	f.insert(t, "grant-owner", "wrong-version", "skill.read", 1, "CURRENT_TIMESTAMP - interval '1 hour'", "NULL", "NULL")
	f.insert(t, "grant-owner", "other-tenant", "skill.read", 2, "CURRENT_TIMESTAMP - interval '1 hour'", "NULL", "NULL", "grant-other")

	got, err := owner.Grants(ctx, "grant-owner", agentskills.SkillKey{ID: "skill.read", Version: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		id       string
		roles    []string
		pop      string
		scopes   []string
		purposes []string
	}{{activeID, []string{"manager", "hr_partner"}, "managers", []string{"org-a", "org-b"}, []string{"workforce:read"}}}
	if len(got) != len(want) {
		t.Fatalf("Grants() returned %+v, want one current grant", got)
	}
	if got[0].ID != want[0].id || got[0].Tenant != "grant-owner" || got[0].Skill != (agentskills.SkillKey{ID: "skill.read", Version: 2}) ||
		!reflect.DeepEqual(got[0].Roles, want[0].roles) || got[0].Population != want[0].pop ||
		!reflect.DeepEqual(got[0].OrganizationScopes, want[0].scopes) || !reflect.DeepEqual(got[0].Purposes, want[0].purposes) || !got[0].ConsentRequired {
		t.Fatalf("current grant projection = %+v, does not preserve stored authorization dimensions", got[0])
	}

	otherTenant, err := other.Grants(ctx, "grant-other", agentskills.SkillKey{ID: "skill.read", Version: 2})
	if err != nil || len(otherTenant) != 1 || otherTenant[0].ID != "other-tenant" {
		t.Fatalf("other tenant Grants() = %+v, %v; want its one isolated row", otherTenant, err)
	}
	if _, err := owner.Grants(ctx, "grant-other", agentskills.SkillKey{ID: "skill.read", Version: 2}); err == nil {
		t.Fatal("tenant-bound provider accepted a request for another tenant")
	}

	// Check database-enforced isolation independently of the explicit tenant
	// predicate used by the provider.
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM agent_skill_grant`).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("agent-skill RLS without tenant context exposed %d rows", visible)
	}

	// A live revocation is observed by the next provider call and cannot be
	// undone or rewritten by the database trigger.
	f.db.Exec(t, `UPDATE agent_skill_grant SET revoked_at=CURRENT_TIMESTAMP, revoked_by='admin', revoked_reason='removed'
		WHERE tenant_id=$1 AND grant_id=$2`, f.ids["grant-owner"], activeID)
	got, err = owner.Grants(ctx, "grant-owner", agentskills.SkillKey{ID: "skill.read", Version: 2})
	if err != nil || len(got) != 0 {
		t.Fatalf("Grants() after revocation = %+v, %v; want no current grants", got, err)
	}
	admin := f.db.NewConn(t)
	if _, err := admin.Exec(ctx, `UPDATE agent_skill_grant SET revoked_at=NULL, revoked_by=NULL, revoked_reason=NULL
		WHERE tenant_id=$1 AND grant_id=$2`, f.ids["grant-owner"], activeID); err == nil {
		t.Fatal("database allowed a grant revocation to be reversed")
	}
}

func TestTodo_AGENT2_005_GrantProviderRejectsInvalidRequest(t *testing.T) {
	f := newGrantFixture(t, "grant-invalid")
	store := f.store(t, "grant-invalid")
	for name, request := range map[string]struct {
		ctx    context.Context
		tenant values.TenantId
		key    agentskills.SkillKey
	}{
		"nil context":  {nil, "grant-invalid", agentskills.SkillKey{ID: "skill.read", Version: 1}},
		"blank skill":  {context.Background(), "grant-invalid", agentskills.SkillKey{Version: 1}},
		"zero version": {context.Background(), "grant-invalid", agentskills.SkillKey{ID: "skill.read"}},
		"wrong tenant": {context.Background(), "another", agentskills.SkillKey{ID: "skill.read", Version: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Grants(request.ctx, request.tenant, request.key); err == nil {
				t.Fatal("Grants() accepted invalid request")
			}
		})
	}
	conn := f.db.NewConn(t)
	root, err := New(conn, func(tenant values.TenantId) uuid.UUID { return f.ids[tenant] })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Scoped("unknown"); err == nil {
		t.Fatal("Scoped() accepted an unknown tenant")
	}
}

type grantFixture struct {
	db  *pgtest.DB
	ids map[values.TenantId]uuid.UUID
}

func newGrantFixture(t *testing.T, tenants ...values.TenantId) *grantFixture {
	t.Helper()
	f := &grantFixture{db: pgtest.New(t), ids: map[values.TenantId]uuid.UUID{}}
	for _, tenant := range tenants {
		id := uuid.New()
		f.ids[tenant] = id
		f.db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from)
			VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, string(tenant), string(tenant))
	}
	return f
}

func (f *grantFixture) store(t *testing.T, tenant values.TenantId) *TenantStore {
	t.Helper()
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	root, err := New(conn, func(key values.TenantId) uuid.UUID { return f.ids[key] })
	if err != nil {
		t.Fatal(err)
	}
	store, err := root.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func (f *grantFixture) insert(t *testing.T, tenant values.TenantId, id, skill string, version int, starts, expires, revoked string, tenantOverride ...values.TenantId) string {
	t.Helper()
	tenantID := f.ids[tenant]
	if len(tenantOverride) > 0 {
		tenantID = f.ids[tenantOverride[0]]
	}
	f.db.Exec(t, `INSERT INTO agent_skill_grant
		(tenant_id,grant_id,skill_id,skill_version,roles,population,organization_scopes,purposes,consent_required,
		 not_before,expires_at,granted_by,granted_at,admin_evidence_ref,revoked_at,revoked_by,revoked_reason)
		VALUES ($1,$2,$3,$4,ARRAY['manager','hr_partner'],'managers',ARRAY['org-a','org-b'],ARRAY['workforce:read'],true,
		 `+starts+`,`+expires+`,'admin-1',CURRENT_TIMESTAMP,'evidence-1',`+revoked+`,
		 CASE WHEN `+revoked+` IS NULL THEN NULL ELSE 'admin-1' END,
		 CASE WHEN `+revoked+` IS NULL THEN NULL ELSE 'removed' END)`, tenantID, id, skill, version)
	return id
}
