package journey_test

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// rt18Snapshot is a stored-grant administration world: ops_admin is a custom
// role with no administrator token behind it, durably assigned to rbac-fay,
// holding the roles page and its actions feature. Nothing here relies on a
// credential claim.
func rt18Snapshot() roleaccess.Snapshot {
	roles := append([]roleaccess.Role(nil), roleaccess.DefaultRoles()...)
	roles = append(roles, roleaccess.Role{Version: 1, ID: "ops_admin", Name: "Operations administrator", Active: true})
	return roleaccess.Snapshot{
		Roles: roles,
		Assignments: []roleaccess.Assignment{
			{Version: 1, WorkerRef: "rbac-fay", RoleIDs: []string{"ops_admin"}},
		},
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: "ops_admin", PageID: "roles", View: true, Create: true, Update: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: "ops_admin", PageID: "roles", FeatureID: "actions", View: true, Create: true, Update: true},
			{Version: 1, RoleID: "ops_admin", PageID: "roles", FeatureID: "feature_access", View: true, Update: true},
		},
	}
}

// TestTodo_RBAC_RT_018 is the PRIMARY matrix entry: role administration
// derives from stored grants, not token roles. A custom role holding the
// roles page administers; the same call without the stored grant refuses;
// self-assignment, over-granting and last-administrator removal refuse.
func TestTodo_RBAC_RT_018(t *testing.T) {
	t.Run("stored grants admit a custom role", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt18Snapshot()}})
		saved, err := client.SaveWorkerRoleAssignment(rt2CallContext(t, rt2FayToken), &journeyv1.SaveWorkerRoleAssignmentRequest{
			Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-otto", RoleIds: []string{"manager"}},
		})
		if err != nil {
			t.Fatalf("stored-grant SaveWorkerRoleAssignment: %v", err)
		}
		if saved.GetAssignment().GetVersion() == 0 {
			t.Fatal("stored-grant save returned no version")
		}
	})

	t.Run("administrator credential without stored grant administers nothing", func(t *testing.T) {
		snapshot := roleaccess.Snapshot{
			Roles: roleaccess.DefaultRoles(),
			PagePermissions: []roleaccess.PagePermission{
				{Version: 1, RoleID: "hcm_admin", PageID: "people", View: true},
			},
		}
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}})
		if _, err := client.SaveWorkerRoleAssignment(rt2CallContext(t, rt2AdminToken), &journeyv1.SaveWorkerRoleAssignmentRequest{
			Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-otto", RoleIds: []string{"manager"}},
		}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("grantless SaveWorkerRoleAssignment code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("self assignment refuses", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt18Snapshot()}})
		if _, err := client.SaveWorkerRoleAssignment(rt2CallContext(t, rt2FayToken), &journeyv1.SaveWorkerRoleAssignmentRequest{
			Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-fay", RoleIds: []string{"hcm_admin", "comp_admin"}},
		}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("self SaveWorkerRoleAssignment code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("grant beyond holding refuses", func(t *testing.T) {
		// The caller passes the roles/feature_access table gate through
		// its own rows, then confers a roles administration power it
		// does not hold: delete on role assignments.
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt18Snapshot()}})
		if _, err := client.SaveRoleFeaturePermission(rt2CallContext(t, rt2FayToken), &journeyv1.SaveRoleFeaturePermissionRequest{
			Permission: &journeyv1.RoleFeaturePermission{RoleId: "ops_admin", PageId: "roles", FeatureId: "role_assignments", CanView: true, CanDelete: true},
		}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("over-grant SaveRoleFeaturePermission code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("removing the last administrator refuses", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt18Snapshot()}})
		if _, err := client.SaveRolePagePermission(rt2CallContext(t, rt2FayToken), &journeyv1.SaveRolePagePermissionRequest{
			Permission: &journeyv1.RolePagePermission{Version: 1, RoleId: "ops_admin", PageId: "roles", CanView: true, CanCreate: true},
		}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("lockout SaveRolePagePermission code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("narrowing a non-final administrator allows", func(t *testing.T) {
		snapshot := rt18Snapshot()
		snapshot.Roles = append(snapshot.Roles, roleaccess.Role{Version: 1, ID: "ops_second", Name: "Second administrator", Active: true})
		snapshot.PagePermissions = append(snapshot.PagePermissions, roleaccess.PagePermission{Version: 1, RoleID: "ops_second", PageID: "roles", View: true, Update: true})
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}})
		saved, err := client.SaveRolePagePermission(rt2CallContext(t, rt2FayToken), &journeyv1.SaveRolePagePermissionRequest{
			Permission: &journeyv1.RolePagePermission{Version: 1, RoleId: "ops_second", PageId: "roles", CanView: true},
		})
		if err != nil {
			t.Fatalf("non-final narrowing SaveRolePagePermission: %v", err)
		}
		if saved.GetPermission().GetCanUpdate() {
			t.Fatal("narrowing kept the update grant")
		}
	})
}

// TestTodo_RBAC_RT_018_Security proves each refusal carries its own reason:
// a denied caller can tell self-dealing, over-granting and lockout apart,
// and a view-only roles grant never passes for the administration duty.
func TestTodo_RBAC_RT_018_Security(t *testing.T) {
	selfClient := func(t *testing.T) journeyv1.JourneyServiceClient {
		t.Helper()
		return startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt18Snapshot()}})
	}

	t.Run("self assignment names its reason", func(t *testing.T) {
		_, err := selfClient(t).SaveWorkerRoleAssignment(rt2CallContext(t, rt2FayToken), &journeyv1.SaveWorkerRoleAssignmentRequest{
			Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "RBAC-FAY", RoleIds: []string{"manager"}},
		})
		owned := assertOwnedCode(t, err, envelope.CodePermissionDenied)
		if owned.ReasonRef() != "journey.role_access.self_assignment" {
			t.Fatalf("reason = %s, want journey.role_access.self_assignment", owned.ReasonRef())
		}
	})

	t.Run("over-grant names its reason", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt18Snapshot()}})
		_, err := client.SaveRoleFeaturePermission(rt2CallContext(t, rt2FayToken), &journeyv1.SaveRoleFeaturePermissionRequest{
			Permission: &journeyv1.RoleFeaturePermission{RoleId: "ops_admin", PageId: "roles", FeatureId: "role_assignments", CanView: true, CanDelete: true},
		})
		owned := assertOwnedCode(t, err, envelope.CodePermissionDenied)
		if owned.ReasonRef() != "journey.role_access.grant_exceeds_holding" {
			t.Fatalf("reason = %s, want journey.role_access.grant_exceeds_holding", owned.ReasonRef())
		}
	})

	t.Run("lockout names its reason", func(t *testing.T) {
		_, err := selfClient(t).SaveAccessRole(rt2CallContext(t, rt2FayToken), &journeyv1.SaveAccessRoleRequest{
			Role: &journeyv1.AccessRole{Version: 1, RoleId: "ops_admin", Name: "Operations administrator", Active: false},
		})
		owned := assertOwnedCode(t, err, envelope.CodePermissionDenied)
		if owned.ReasonRef() != "journey.role_access.last_administrator" {
			t.Fatalf("reason = %s, want journey.role_access.last_administrator", owned.ReasonRef())
		}
	})

	t.Run("view-only roles grant never administers", func(t *testing.T) {
		snapshot := roleaccess.Snapshot{
			Roles: roleaccess.DefaultRoles(),
			Assignments: []roleaccess.Assignment{
				{Version: 1, WorkerRef: "rbac-fay", RoleIDs: []string{"ops_admin"}},
			},
			PagePermissions: []roleaccess.PagePermission{
				{Version: 1, RoleID: "ops_admin", PageID: "roles", View: true},
			},
			FeaturePermissions: []roleaccess.FeaturePermission{
				{Version: 1, RoleID: "ops_admin", PageID: "roles", FeatureID: "actions", View: true},
			},
		}
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}})
		if _, err := client.GetRoleAccess(rt2CallContext(t, rt2FayToken), &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("view-only GetRoleAccess code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})
}

// TestTodo_RBAC_RT_018_Integration proves the stored-grant administration
// end to end: the custom role's assignment survives in the store and reads
// back through role administration on the next call.
func TestTodo_RBAC_RT_018_Integration(t *testing.T) {
	store := &rt2Store{snapshot: rt18Snapshot()}
	client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: store})
	ctx := rt2CallContext(t, rt2FayToken)
	if _, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{
		Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-otto", RoleIds: []string{"manager"}},
	}); err != nil {
		t.Fatalf("stored-grant save: %v", err)
	}
	loaded, err := client.GetRoleAccess(rt2CallContext(t, rt2FayToken), &journeyv1.GetRoleAccessRequest{})
	if err != nil {
		t.Fatalf("stored-grant readback: %v", err)
	}
	found := false
	for _, assignment := range loaded.GetAssignments() {
		if assignment.GetWorkerRef() == "rbac-otto" {
			found = true
		}
	}
	if !found {
		t.Fatal("saved assignment did not read back through role administration")
	}
}
