package roleaccessstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_RBAC_RT_017_Integration pins RBAC-RT-017 against the real store:
// system roles cannot be renamed, deactivated or stripped of their system
// designation, and an inactive role contributes no grant anywhere.
func TestTodo_RBAC_RT_017_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','RT017 Test','ACTIVE',$3)`, tenantID, "role-access-rt017", time.Now().UTC())
	features := []roleaccess.FeatureDefinition{{PageID: "people", FeatureID: "content", View: true}}
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID }, features...)
	ctx := context.Background()
	tenant := values.TenantId("role-access-rt017")
	if err := store.Bootstrap(ctx, tenant, "system:test"); err != nil {
		t.Fatal(err)
	}

	roleOf := func(id string) roleaccess.Role {
		t.Helper()
		snapshot, err := store.Load(ctx, tenant, "org:rt017")
		if err != nil {
			t.Fatal(err)
		}
		for _, role := range snapshot.Roles {
			if role.ID == id {
				return role
			}
		}
		t.Fatalf("no role %s", id)
		return roleaccess.Role{}
	}

	admin := roleOf("hcm_admin")
	if !admin.System || !admin.Active {
		t.Fatalf("bootstrap hcm_admin is not an active system role: %#v", admin)
	}
	renamed := admin
	renamed.Name = "HCM administrator (renamed)"
	if _, err := store.SaveRole(ctx, tenant, "admin", renamed); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("system role rename = %v", err)
	}
	deactivated := admin
	deactivated.Active = false
	if _, err := store.SaveRole(ctx, tenant, "admin", deactivated); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("system role deactivation = %v", err)
	}
	desystemed := admin
	desystemed.System = false
	if _, err := store.SaveRole(ctx, tenant, "admin", desystemed); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("system role designation stripping = %v", err)
	}
	if kept := roleOf("hcm_admin"); kept.Name != admin.Name || !kept.Active || !kept.System || kept.Version != admin.Version {
		t.Fatalf("refused system role write changed the row: %#v", kept)
	}
	edited := admin
	edited.Description = "Updated duty description."
	edited, err := store.SaveRole(ctx, tenant, "admin", edited)
	if err != nil || edited.Version != admin.Version+1 || !edited.System || !edited.Active || edited.Description != "Updated duty description." {
		t.Fatalf("system role description edit = %#v, %v", edited, err)
	}

	custom, err := store.SaveRole(ctx, tenant, "admin", roleaccess.Role{ID: "rt017_viewer", Name: "RT017 viewer", Description: "Inactive-role probe.", Active: true})
	if err != nil || custom.Version != 1 {
		t.Fatalf("save custom role = %+v, %v", custom, err)
	}
	page, err := store.SavePagePermission(ctx, tenant, "admin", roleaccess.PagePermission{RoleID: "rt017_viewer", PageID: "people", View: true})
	if err != nil || page.Version != 1 {
		t.Fatalf("save custom page grant = %+v, %v", page, err)
	}
	feature, err := store.SaveFeaturePermission(ctx, tenant, "admin", roleaccess.FeaturePermission{RoleID: "rt017_viewer", PageID: "people", FeatureID: "content", View: true})
	if err != nil || feature.Version != 1 {
		t.Fatalf("save custom feature grant = %+v, %v", feature, err)
	}
	if _, err := store.SaveVisibility(ctx, tenant, "org:rt017", "admin", roleaccess.VisibilityPolicy{RoleID: "rt017_viewer", Mode: roleaccess.VisibilityAllowlist, OrganizationUnits: []string{"engineering"}}); err != nil {
		t.Fatalf("save custom visibility = %v", err)
	}
	if _, err := store.SaveAssignment(ctx, tenant, "admin", roleaccess.Assignment{WorkerRef: "rt017-worker", RoleIDs: []string{"rt017_viewer"}}); err != nil {
		t.Fatalf("save custom assignment = %v", err)
	}
	loaded, err := store.Load(ctx, tenant, "org:rt017")
	if err != nil {
		t.Fatal(err)
	}
	if !roleaccess.CanPageAction(roleaccess.EffectivePagePermissions(loaded, []string{"rt017_viewer"}), "people", roleaccess.ActionView) {
		t.Fatal("active custom role granted nothing before deactivation")
	}

	custom.Active = false
	custom, err = store.SaveRole(ctx, tenant, "admin", custom)
	if err != nil || custom.Active {
		t.Fatalf("custom role deactivation = %+v, %v", custom, err)
	}
	loaded, err = store.Load(ctx, tenant, "org:rt017")
	if err != nil {
		t.Fatal(err)
	}
	if got := roleaccess.EffectivePagePermissions(loaded, []string{"rt017_viewer"}); len(got) != 0 {
		t.Fatalf("inactive role kept page grants: %#v", got)
	}
	if got := roleaccess.EffectiveFeaturePermissions(loaded, []string{"rt017_viewer"}); len(got) != 0 {
		t.Fatalf("inactive role kept feature grants: %#v", got)
	}
	if got := roleaccess.PoliciesForRoles(loaded, []string{"rt017_viewer"}); len(got) != 0 {
		t.Fatalf("inactive role kept visibility: %#v", got)
	}
	if _, err := store.SaveAssignment(ctx, tenant, "admin", roleaccess.Assignment{WorkerRef: "rt017-other", RoleIDs: []string{"rt017_viewer"}}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("inactive role assignment = %v", err)
	}
	if _, err := store.SaveVisibility(ctx, tenant, "org:rt017", "admin", roleaccess.VisibilityPolicy{RoleID: "rt017_viewer", Mode: roleaccess.VisibilityAll}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("inactive role visibility = %v", err)
	}
	if _, err := store.SavePagePermission(ctx, tenant, "admin", roleaccess.PagePermission{RoleID: "rt017_viewer", PageID: "people", View: true}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("inactive role page grant = %v", err)
	}

	custom.Active = true
	if _, err := store.SaveRole(ctx, tenant, "admin", custom); err != nil {
		t.Fatalf("custom role reactivation = %v", err)
	}
	loaded, err = store.Load(ctx, tenant, "org:rt017")
	if err != nil {
		t.Fatal(err)
	}
	if !roleaccess.CanPageAction(roleaccess.EffectivePagePermissions(loaded, []string{"rt017_viewer"}), "people", roleaccess.ActionView) {
		t.Fatal("reactivation did not restore the stored grant")
	}
}
