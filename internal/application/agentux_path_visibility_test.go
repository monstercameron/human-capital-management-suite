package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTUX_PATH_T6_DemoRoleVisibility_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(ctx, personaChatSchemaDSN(t, db.URL, db.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	core, err := pgstore.New(pool, pgstore.WithCellID("agentux-path-visibility"))
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Bootstrap(ctx, localAgentDemoTenant); err != nil {
		t.Fatal(err)
	}
	tenant := values.TenantId(localAgentDemoTenant)
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	store := roleaccessstore.New(pool, mapper, productFeatureCatalog()...)
	if err := store.Bootstrap(ctx, tenant, "system:bootstrap"); err != nil {
		t.Fatal(err)
	}
	audience := agentpersona.Audience{Roles: []string{"hcm_admin"}, OrganizationScopes: []string{"org:agentux-path"}}
	for i := 0; i < 2; i++ {
		if err := ensureLocalAgentDemoRoleVisibility(ctx, store, tenant, audience); err != nil {
			t.Fatalf("visibility pass %d: %v", i+1, err)
		}
	}
	pack, ok := demoworkforce.PackFor(localAgentDemoTenant)
	if !ok {
		t.Fatal("local demo pack unavailable")
	}
	for _, organization := range []string{"org:agentux-path", pack.OrgScope()} {
		snapshot, err := store.Load(ctx, tenant, organization)
		if err != nil {
			t.Fatal(err)
		}
		matches := 0
		for _, policy := range snapshot.Policies {
			if policy.RoleID == "hcm_admin" {
				matches++
				if policy.Mode != roleaccess.VisibilityAll || policy.Version != 1 {
					t.Fatalf("organization %s policy=%+v", organization, policy)
				}
			}
		}
		if matches != 1 {
			t.Fatalf("organization %s policies=%+v", organization, snapshot.Policies)
		}
	}
}
