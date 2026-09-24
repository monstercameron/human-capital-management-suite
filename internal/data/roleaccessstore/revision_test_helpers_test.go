package roleaccessstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// These helpers make the test action rationale explicit while keeping the
// existing store behavior tests focused on their version and authorization
// assertions. The dedicated RBAC-RT-021 tests pin human-readable reasons.
func testSaveRole(store *Store, ctx context.Context, tenant values.TenantId, actor string, role roleaccess.Role) (roleaccess.Role, error) {
	if role.Reason == "" {
		role.Reason = "Test fixture: exercise role create and update behavior"
	}
	return store.SaveRole(ctx, tenant, actor, role)
}

func testSaveAssignment(store *Store, ctx context.Context, tenant values.TenantId, actor string, assignment roleaccess.Assignment) (roleaccess.Assignment, error) {
	if assignment.Reason == "" {
		assignment.Reason = "Test fixture: exercise worker role assignment behavior"
	}
	return store.SaveAssignment(ctx, tenant, actor, assignment)
}

func testSaveVisibility(store *Store, ctx context.Context, tenant values.TenantId, organization, actor string, policy roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	if policy.Reason == "" {
		policy.Reason = "Test fixture: exercise organization visibility behavior"
	}
	return store.SaveVisibility(ctx, tenant, organization, actor, policy)
}

func testSavePagePermission(store *Store, ctx context.Context, tenant values.TenantId, actor string, permission roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	if permission.Reason == "" {
		permission.Reason = "Test fixture: exercise page permission behavior"
	}
	return store.SavePagePermission(ctx, tenant, actor, permission)
}

func testSaveFeaturePermission(store *Store, ctx context.Context, tenant values.TenantId, actor string, permission roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	if permission.Reason == "" {
		permission.Reason = "Test fixture: exercise feature permission behavior"
	}
	return store.SaveFeaturePermission(ctx, tenant, actor, permission)
}
