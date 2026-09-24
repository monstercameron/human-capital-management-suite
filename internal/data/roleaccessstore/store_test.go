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

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_UXAUDIT_014_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Persona Access','ACTIVE',$3)`, tenantID, "persona-access-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	ctx := context.Background()
	tenant := values.TenantId("persona-access-test")
	if err := store.Bootstrap(ctx, tenant, "system:bootstrap"); err != nil {
		t.Fatal(err)
	}
	permissions := func(roles ...string) []roleaccess.PagePermission {
		t.Helper()
		snapshot, err := store.Load(ctx, tenant, "org:persona-access-test:people-ops")
		if err != nil {
			t.Fatal(err)
		}
		return roleaccess.EffectivePagePermissions(snapshot, roles)
	}
	payrollRoles := []string{"payroll_manager", "promotion_operator"}
	orgPages := []string{"organization", "org-explorer", "org-outline", "org-responsive"}
	for _, page := range orgPages {
		if !roleaccess.CanPageAction(permissions(payrollRoles...), page, roleaccess.ActionView) {
			t.Fatalf("test did not start from broad historical %s access", page)
		}
	}
	if !roleaccess.CanPageAction(permissions(payrollRoles...), "journeys", roleaccess.ActionCreate) {
		t.Fatal("test did not start from the broad historical demo grants")
	}
	if err := store.BootstrapLocalDevPersonaPermissions(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if err := store.BootstrapLocalDevPersonaPermissions(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	for _, page := range orgPages {
		if roleaccess.CanPageAction(permissions(payrollRoles...), page, roleaccess.ActionView) {
			t.Fatalf("payroll demo persona retained %s browsing", page)
		}
	}
	if roleaccess.CanPageAction(permissions(payrollRoles...), "journeys", roleaccess.ActionCreate) {
		t.Fatal("payroll demo persona retained promotion initiation")
	}
	if !roleaccess.CanPageAction(permissions(payrollRoles...), "work", roleaccess.ActionView) ||
		!roleaccess.CanPageAction(permissions("hiring_manager", "manager", "intent_author"), "organization", roleaccess.ActionView) {
		t.Fatal("demo policy removed assigned-work review or hiring-manager organization access")
	}
	// An administrator's edited grant has a higher version and survives replay.
	snapshot, err := store.Load(ctx, tenant, "org:persona-access-test:people-ops")
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range snapshot.PagePermissions {
		if grant.RoleID == "payroll_manager" && grant.PageID == "organization" {
			grant.View = true
			if _, err := testSavePagePermission(store, ctx, tenant, "admin", grant); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if err := store.BootstrapLocalDevPersonaPermissions(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if !roleaccess.CanPageAction(permissions(payrollRoles...), "organization", roleaccess.ActionView) {
		t.Fatal("local-dev replay overwrote an administrator-edited role grant")
	}
}

func TestStorePersistsRolesAssignmentsAndScopedVisibilityWithCAS(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Access Test','ACTIVE',$3)`, tenantID, "role-access-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	ctx := context.Background()
	tenant := values.TenantId("role-access-test")
	if err := store.Bootstrap(ctx, tenant, "system:test"); err != nil {
		t.Fatal(err)
	}

	role, err := testSaveRole(store, ctx, tenant, "admin", roleaccess.Role{ID: "regional_auditor", Name: "Regional auditor", Description: "Supports regional reviews", Active: true})
	if err != nil || role.Version != 1 {
		t.Fatalf("save role = %+v, %v", role, err)
	}
	if _, err := testSaveRole(store, ctx, tenant, "admin", roleaccess.Role{ID: "regional_auditor", Name: "Duplicate", Active: true}); !errors.Is(err, roleaccess.ErrVersionConflict) {
		t.Fatalf("duplicate role = %v", err)
	}

	assignment, err := testSaveAssignment(store, ctx, tenant, "admin", roleaccess.Assignment{WorkerRef: "worker-1", RoleIDs: []string{"worker_self", "regional_auditor"}})
	if err != nil || assignment.Version != 1 {
		t.Fatalf("save assignment = %+v, %v", assignment, err)
	}
	updated := assignment
	updated.RoleIDs = []string{"regional_auditor"}
	updated, err = testSaveAssignment(store, ctx, tenant, "admin", updated)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update assignment = %+v, %v", updated, err)
	}
	if _, err := testSaveAssignment(store, ctx, tenant, "admin", assignment); !errors.Is(err, roleaccess.ErrVersionConflict) {
		t.Fatalf("stale assignment = %v", err)
	}

	policy, err := testSaveVisibility(store, ctx, tenant, "org:north", "admin", roleaccess.VisibilityPolicy{RoleID: "regional_auditor", Mode: roleaccess.VisibilityAllowlist, OrganizationUnits: []string{"Finance", "finance"}})
	if err != nil || policy.Version != 1 || len(policy.OrganizationUnits) != 1 {
		t.Fatalf("save visibility = %+v, %v", policy, err)
	}
	page, err := testSavePagePermission(store, ctx, tenant, "admin", roleaccess.PagePermission{RoleID: "regional_auditor", PageID: "insights", View: true})
	if err != nil || page.Version != 1 || page.Create {
		t.Fatalf("save page permission = %+v, %v", page, err)
	}
	page.Create = true
	page, err = testSavePagePermission(store, ctx, tenant, "admin", page)
	if err != nil || page.Version != 2 || !page.Create {
		t.Fatalf("update page permission = %+v, %v", page, err)
	}
	loaded, err := store.Load(ctx, tenant, "org:north")
	if err != nil || len(loaded.Roles) < len(roleaccess.DefaultRoles())+1 || len(loaded.Assignments) != 1 || len(loaded.Policies) != 1 || len(loaded.PagePermissions) < len(roleaccess.DefaultPagePermissions())+1 {
		t.Fatalf("load = %+v, %v", loaded, err)
	}
	other, err := store.Load(ctx, tenant, "org:south")
	if err != nil || len(other.Policies) != 0 || len(other.Assignments) != 1 {
		t.Fatalf("organization scoping = %+v, %v", other, err)
	}
}

func TestTodo_WEB_241_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Feature Access','ACTIVE',$3)`, tenantID, "feature-access-test", time.Now().UTC())
	features := []roleaccess.FeatureDefinition{
		{PageID: "people", FeatureID: "content", View: true},
		{PageID: "people", FeatureID: "actions", View: true, Create: true, Update: true, Delete: true},
		{PageID: "people", FeatureID: "workflow_actions", View: true, Create: true},
	}
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID }, features...)
	ctx := context.Background()
	tenant := values.TenantId("feature-access-test")
	if err := store.Bootstrap(ctx, tenant, "system:test"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load(ctx, tenant, "org:north")
	if err != nil {
		t.Fatal(err)
	}
	effectivePages := roleaccess.EffectivePagePermissions(snapshot, []string{"comp_admin"})
	effectiveFeatures := roleaccess.EffectiveFeaturePermissions(snapshot, []string{"comp_admin"})
	if !roleaccess.CanFeatureAction(effectivePages, effectiveFeatures, "people", "workflow_actions", roleaccess.ActionCreate) {
		t.Fatal("bootstrap did not preserve the manager's existing people/create grant")
	}
	var permission roleaccess.FeaturePermission
	for _, candidate := range snapshot.FeaturePermissions {
		if candidate.RoleID == "comp_admin" && candidate.PageID == "people" && candidate.FeatureID == "workflow_actions" {
			permission = candidate
			break
		}
	}
	if permission.Version != 1 {
		t.Fatalf("bootstrap feature permission = %#v", permission)
	}
	permission.Create = false
	updated, err := testSaveFeaturePermission(store, ctx, tenant, "admin", permission)
	if err != nil || updated.Version != 2 || updated.Create {
		t.Fatalf("update feature permission = %#v, %v", updated, err)
	}
	if _, err := testSaveFeaturePermission(store, ctx, tenant, "admin", permission); !errors.Is(err, roleaccess.ErrVersionConflict) {
		t.Fatalf("stale feature update = %v", err)
	}
	unsupported := updated
	unsupported.Delete = true
	if _, err := testSaveFeaturePermission(store, ctx, tenant, "admin", unsupported); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("feature grant beyond its declared ceiling = %v", err)
	}
	if _, err := testSaveFeaturePermission(store, ctx, tenant, "admin", roleaccess.FeaturePermission{RoleID: "comp_admin", PageID: "people", FeatureID: "unknown", View: true}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("unregistered feature = %v", err)
	}
	if _, err := testSaveFeaturePermission(store, ctx, tenant, "admin", roleaccess.FeaturePermission{RoleID: "comp_admin", PageID: "missing-page", FeatureID: "content", View: true}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("feature without page grant = %v", err)
	}
}

func TestStore_ValidationBootstrapAndUpdates(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Role Test','ACTIVE',$3)`, tenantID, "role-access-negative", time.Now().UTC())
	tenant := values.TenantId("role-access-negative")
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	ctx := context.Background()
	if _, err := store.Load(ctx, tenant, " "); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("empty organization load = %v", err)
	}
	if err := store.Bootstrap(ctx, tenant, " "); err != nil {
		t.Fatal(err)
	}
	if err := store.Bootstrap(ctx, tenant, "second-bootstrap"); err != nil {
		t.Fatal(err)
	}
	role := roleaccess.Role{ID: "custom_role", Name: "Custom", Description: "Description", Active: true}
	if _, err := testSaveRole(store, ctx, tenant, " ", role); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("blank role actor = %v", err)
	}
	if _, err := testSaveRole(store, ctx, tenant, "admin", roleaccess.Role{ID: "bad role", Name: "Bad", Active: true}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("invalid role = %v", err)
	}
	saved, err := testSaveRole(store, ctx, tenant, "admin", role)
	if err != nil || saved.Version != 1 {
		t.Fatalf("save custom role = %+v, %v", saved, err)
	}
	saved.Description = "Updated"
	saved, err = testSaveRole(store, ctx, tenant, "admin", saved)
	if err != nil || saved.Version != 2 || saved.Description != "Updated" {
		t.Fatalf("update custom role = %+v, %v", saved, err)
	}
	if _, err := testSaveAssignment(store, ctx, tenant, "admin", roleaccess.Assignment{WorkerRef: "worker", RoleIDs: nil}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("empty assignment = %v", err)
	}
	assignment, err := testSaveAssignment(store, ctx, tenant, "admin", roleaccess.Assignment{WorkerRef: "worker", RoleIDs: []string{"custom_role"}})
	if err != nil || assignment.Version != 1 {
		t.Fatalf("save assignment = %+v, %v", assignment, err)
	}
	updated := assignment
	updated.RoleIDs = []string{"custom_role"}
	updated, err = testSaveAssignment(store, ctx, tenant, "admin", updated)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update assignment = %+v, %v", updated, err)
	}
	if _, err := testSaveVisibility(store, ctx, tenant, "org", "admin", roleaccess.VisibilityPolicy{RoleID: "custom_role", Mode: "unknown"}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("invalid visibility = %v", err)
	}
	policy, err := testSaveVisibility(store, ctx, tenant, "org", "admin", roleaccess.VisibilityPolicy{RoleID: "custom_role", Mode: roleaccess.VisibilityOwnUnit, OrganizationUnits: []string{"Finance"}})
	if err != nil || policy.Version != 1 {
		t.Fatalf("save visibility = %+v, %v", policy, err)
	}
	policy.Mode = roleaccess.VisibilityDenylist
	policy, err = testSaveVisibility(store, ctx, tenant, "org", "admin", policy)
	if err != nil || policy.Version != 2 {
		t.Fatalf("update visibility = %+v, %v", policy, err)
	}
	if _, err := testSaveAssignment(store, ctx, tenant, "admin", roleaccess.Assignment{WorkerRef: "inactive", RoleIDs: []string{"custom_role"}}); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `UPDATE access_role SET active=false WHERE tenant_id=$1 AND role_id=$2`, tenantID, "custom_role")
	if _, err := testSaveAssignment(store, ctx, tenant, "admin", roleaccess.Assignment{WorkerRef: "blocked", RoleIDs: []string{"custom_role"}}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("inactive assigned role = %v", err)
	}
	if _, err := testSaveVisibility(store, ctx, tenant, "org", "admin", roleaccess.VisibilityPolicy{RoleID: "custom_role", Mode: roleaccess.VisibilityAll}); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("inactive visibility role = %v", err)
	}
}

func TestStore_UnavailableAndTenantMappingFailures(t *testing.T) {
	ctx := context.Background()
	if err := New(nil, nil).Bootstrap(ctx, "tenant", "actor"); !errors.Is(err, roleaccess.ErrUnavailable) {
		t.Fatalf("nil store bootstrap = %v", err)
	}
	if _, err := New(nil, func(values.TenantId) uuid.UUID { return uuid.Nil }).Load(ctx, "tenant", "org"); !errors.Is(err, roleaccess.ErrInvalid) {
		t.Fatalf("nil tenant mapping = %v", err)
	}
}
