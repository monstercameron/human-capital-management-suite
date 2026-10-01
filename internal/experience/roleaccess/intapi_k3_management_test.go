package roleaccess

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type intapiK3RoleStore struct{ snapshot Snapshot }

func (s *intapiK3RoleStore) Bootstrap(context.Context, values.TenantId, string) error { return nil }
func (s *intapiK3RoleStore) Load(context.Context, values.TenantId, string) (Snapshot, error) {
	return s.snapshot, nil
}
func (s *intapiK3RoleStore) SaveRole(_ context.Context, _ values.TenantId, _ string, role Role) (Role, error) {
	s.snapshot.Roles = []Role{role}
	return role, nil
}
func (s *intapiK3RoleStore) SaveAssignment(_ context.Context, _ values.TenantId, _ string, assignment Assignment) (Assignment, error) {
	s.snapshot.Assignments = []Assignment{assignment}
	return assignment, nil
}
func (s *intapiK3RoleStore) SaveVisibility(context.Context, values.TenantId, string, string, VisibilityPolicy) (VisibilityPolicy, error) {
	return VisibilityPolicy{}, nil
}
func (s *intapiK3RoleStore) SavePagePermission(context.Context, values.TenantId, string, PagePermission) (PagePermission, error) {
	return PagePermission{}, nil
}
func (s *intapiK3RoleStore) SaveFeaturePermission(context.Context, values.TenantId, string, FeaturePermission) (FeaturePermission, error) {
	return FeaturePermission{}, nil
}

func TestTodo_INTAPI_012_RoleBindings(t *testing.T) {
	store := &intapiK3RoleStore{snapshot: Snapshot{Roles: []Role{{ID: "custom", Name: "Custom", Description: "role", Active: true, Version: 1}}}}
	management := Management{Store: store}
	updated, err := management.UpdateRole(context.Background(), values.TenantId("tenant-a"), "scope", Role{ID: "custom", Name: "Renamed", Description: "role", Active: true}, 1)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update role = %+v, err=%v", updated, err)
	}
	if _, err := management.UpdateRole(context.Background(), values.TenantId("tenant-a"), "scope", Role{ID: "custom", Name: "stale", Description: "role", Active: true}, 1); !errors.Is(err, ErrManagementRevision) {
		t.Fatalf("stale role update = %v", err)
	}
}

func TestTodo_INTAPI_012_RoleBindings_Security(t *testing.T) {
	management := Management{}
	if _, err := management.RetireRole(context.Background(), values.TenantId("tenant-a"), "scope", "custom", 1); !errors.Is(err, ErrManagementRevision) {
		t.Fatalf("nil store = %v", err)
	}
}
