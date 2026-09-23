package roleaccessstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_RBAC_RT_015_Integration is the INTEGRATION matrix entry: every
// tenant passed for provisioning receives default roles, pages and features
// from one call, against the real store. A tenant left off the list would be
// served with no permission rows, so provisioning must enumerate tenants.
func TestTodo_RBAC_RT_015_Integration(t *testing.T) {
	db := pgtest.New(t)
	firstID, secondID := uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','First','ACTIVE',$3)`, firstID, "rt15-first", time.Now().UTC())
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Second','ACTIVE',$3)`, secondID, "rt15-second", time.Now().UTC())
	tenants := map[values.TenantId]uuid.UUID{
		"rt15-first":  firstID,
		"rt15-second": secondID,
	}
	features := []roleaccess.FeatureDefinition{
		{PageID: "journeys", FeatureID: "content", View: true},
		{PageID: "journeys", FeatureID: "journey_list", View: true},
		{PageID: "people", FeatureID: "directory", View: true, Create: true},
	}
	store := New(db.Conn, func(tenant values.TenantId) uuid.UUID { return tenants[tenant] }, features...)
	ctx := context.Background()

	if err := store.BootstrapTenants(ctx, []values.TenantId{"rt15-first", "rt15-second"}, "system:test"); err != nil {
		t.Fatalf("BootstrapTenants: %v", err)
	}

	for _, tenant := range []values.TenantId{"rt15-first", "rt15-second"} {
		snapshot, err := store.Load(ctx, tenant, "org:rt15")
		if err != nil {
			t.Fatalf("Load %s: %v", tenant, err)
		}
		if len(snapshot.Roles) == 0 {
			t.Fatalf("tenant %s bootstrapped with no roles", tenant)
		}
		if len(snapshot.PagePermissions) == 0 {
			t.Fatalf("tenant %s bootstrapped with no page rows", tenant)
		}
		grants := roleaccess.EffectiveFeaturePermissions(snapshot, assignedRoleIDs(snapshot))
		for _, want := range [][2]string{{"journeys", "content"}, {"journeys", "journey_list"}, {"people", "directory"}} {
			if !roleaccess.CanFeatureAction(roleaccess.EffectivePagePermissions(snapshot, assignedRoleIDs(snapshot)), grants, want[0], want[1], roleaccess.ActionView) {
				t.Fatalf("tenant %s missing bootstrapped grant %s/%s", tenant, want[0], want[1])
			}
		}
	}

	countPages := func(tenant values.TenantId) int {
		t.Helper()
		snapshot, err := store.Load(ctx, tenant, "org:rt15")
		if err != nil {
			t.Fatalf("Load %s: %v", tenant, err)
		}
		return len(snapshot.PagePermissions) + len(snapshot.FeaturePermissions)
	}
	before := countPages("rt15-first")
	if err := store.BootstrapTenants(ctx, []values.TenantId{"rt15-first", "rt15-second"}, "system:test"); err != nil {
		t.Fatalf("second BootstrapTenants: %v", err)
	}
	if got := countPages("rt15-first"); got != before {
		t.Fatalf("second bootstrap changed row count %d -> %d, want idempotent", before, got)
	}

	if err := store.BootstrapTenants(ctx, nil, "system:test"); err != nil {
		t.Fatalf("empty BootstrapTenants: %v", err)
	}
}

func assignedRoleIDs(snapshot roleaccess.Snapshot) []string {
	seen := map[string]bool{}
	var result []string
	for _, permission := range snapshot.PagePermissions {
		if !seen[permission.RoleID] {
			seen[permission.RoleID] = true
			result = append(result, permission.RoleID)
		}
	}
	return result
}
