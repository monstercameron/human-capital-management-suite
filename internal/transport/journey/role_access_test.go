package journey_test

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type roleAccessSpy struct{ snapshot roleaccess.Snapshot }

func (s *roleAccessSpy) Bootstrap(context.Context, values.TenantId, string) error { return nil }
func (s *roleAccessSpy) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return s.snapshot, nil
}
func (s *roleAccessSpy) SaveRole(_ context.Context, _ values.TenantId, _ string, value roleaccess.Role) (roleaccess.Role, error) {
	value.Version++
	return value, nil
}
func (s *roleAccessSpy) SaveAssignment(_ context.Context, _ values.TenantId, _ string, value roleaccess.Assignment) (roleaccess.Assignment, error) {
	value.Version++
	return value, nil
}
func (s *roleAccessSpy) SaveVisibility(_ context.Context, _ values.TenantId, _, _ string, value roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	value.Version++
	return value, nil
}
func (s *roleAccessSpy) SavePagePermission(_ context.Context, _ values.TenantId, _ string, value roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	value.Version++
	return value, nil
}

// SaveFeaturePermission keeps the spy implementing roleaccess.Store while the
// feature-permission lane is in flight: same Version++ passthrough as every
// other Save stub on this spy, no behavior of its own.
func (s *roleAccessSpy) SaveFeaturePermission(_ context.Context, _ values.TenantId, _ string, value roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	value.Version++
	return value, nil
}

func TestRoleAccessAdministrationRequiresAdminAndRoundTrips(t *testing.T) {
	spy := &roleAccessSpy{snapshot: roleaccess.Snapshot{
		Roles:       []roleaccess.Role{{Version: 1, ID: "manager", Name: "Manager", Active: true}},
		Assignments: []roleaccess.Assignment{{Version: 2, WorkerRef: "jane", RoleIDs: []string{"manager"}}},
		Policies:    []roleaccess.VisibilityPolicy{{Version: 3, RoleID: "manager", Mode: roleaccess.VisibilityOwnUnit}},
		PagePermissions: []roleaccess.PagePermission{
			{Version: 4, RoleID: "manager", PageID: "insights", View: true},
			{Version: 1, RoleID: "comp_admin", PageID: "roles", View: true, Create: true, Update: true, Delete: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 5, RoleID: "manager", PageID: "insights", FeatureID: "content", View: true},
			{Version: 1, RoleID: "comp_admin", PageID: "roles", FeatureID: "content", View: true},
			{Version: 1, RoleID: "comp_admin", PageID: "roles", FeatureID: "actions", View: true, Create: true, Update: true, Delete: true},
			{Version: 1, RoleID: "comp_admin", PageID: "roles", FeatureID: "feature_access", View: true, Create: true, Update: true, Delete: true},
		},
	}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{RoleAccess: spy}))
	if _, err := client.GetRoleAccess(testContext(t), &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("non-admin GetRoleAccess = %v", err)
	}
	ctx := withToken(context.Background(), fixtureAppearanceAdminToken)
	loaded, err := client.GetRoleAccess(ctx, &journeyv1.GetRoleAccessRequest{})
	if err != nil || len(loaded.GetRoles()) != 1 || len(loaded.GetAssignments()) != 1 || len(loaded.GetVisibilityPolicies()) != 1 || len(loaded.GetPagePermissions()) != 2 || len(loaded.GetFeaturePermissions()) != 4 {
		t.Fatalf("GetRoleAccess = %+v, %v", loaded, err)
	}
	saved, err := client.SaveAccessRole(ctx, &journeyv1.SaveAccessRoleRequest{Role: &journeyv1.AccessRole{RoleId: "recruiter", Name: "Recruiter", Active: true}})
	if err != nil || saved.GetRole().GetVersion() != 1 {
		t.Fatalf("SaveAccessRole = %+v, %v", saved, err)
	}
	assigned, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "jane", RoleIds: []string{"manager"}}})
	if err != nil || assigned.GetAssignment().GetVersion() != 1 {
		t.Fatalf("SaveWorkerRoleAssignment = %+v, %v", assigned, err)
	}
	permission, err := client.SaveRolePagePermission(ctx, &journeyv1.SaveRolePagePermissionRequest{Permission: &journeyv1.RolePagePermission{RoleId: "manager", PageId: "insights", CanView: true}})
	if err != nil || permission.GetPermission().GetVersion() != 1 || !permission.GetPermission().GetCanView() {
		t.Fatalf("SaveRolePagePermission = %+v, %v", permission, err)
	}
	feature, err := client.SaveRoleFeaturePermission(ctx, &journeyv1.SaveRoleFeaturePermissionRequest{Permission: &journeyv1.RoleFeaturePermission{RoleId: "manager", PageId: "insights", FeatureId: "content", CanView: true}})
	if err != nil || feature.GetPermission().GetVersion() != 1 || feature.GetPermission().GetFeatureId() != "content" {
		t.Fatalf("SaveRoleFeaturePermission = %+v, %v", feature, err)
	}
}

func TestPageActionPermissionDeniesMutationBeforeEngineAndAllowsReadOnlyReports(t *testing.T) {
	spy := &roleAccessSpy{snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{
		{RoleID: "intent_author", PageID: "insights", View: true},
	}}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{RoleAccess: spy}))
	_, err := client.ProposeJourney(testContext(t), &journeyv1.ProposeJourneyRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("read-only report role ProposeJourney code = %v, want PermissionDenied; err=%v", status.Code(err), err)
	}

	spy.snapshot.PagePermissions = append(spy.snapshot.PagePermissions, roleaccess.PagePermission{RoleID: "intent_author", PageID: "journeys", View: true, Create: true})
	spy.snapshot.FeaturePermissions = append(spy.snapshot.FeaturePermissions, roleaccess.FeaturePermission{RoleID: "intent_author", PageID: "journeys", FeatureID: "promotion_request", View: true, Create: true})
	_, err = client.ProposeJourney(testContext(t), &journeyv1.ProposeJourneyRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("create-enabled role ProposeJourney code = %v, want downstream Unavailable; err=%v", status.Code(err), err)
	}
}

func TestTodo_WEB_241_Security_Transport(t *testing.T) {
	spy := &roleAccessSpy{snapshot: roleaccess.Snapshot{
		PagePermissions: []roleaccess.PagePermission{{RoleID: "intent_author", PageID: "journeys", View: true, Create: true}},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{RoleID: "intent_author", PageID: "journeys", FeatureID: "content", View: true},
			{RoleID: "intent_author", PageID: "journeys", FeatureID: "promotion_request", View: true},
		},
	}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{RoleAccess: spy}))
	_, err := client.ProposeJourney(testContext(t), &journeyv1.ProposeJourneyRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("feature-denied ProposeJourney code = %v, want PermissionDenied; err=%v", status.Code(err), err)
	}

	spy.snapshot.FeaturePermissions[1].Create = true
	_, err = client.ProposeJourney(testContext(t), &journeyv1.ProposeJourneyRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("feature-enabled ProposeJourney code = %v, want downstream Unavailable; err=%v", status.Code(err), err)
	}
}
