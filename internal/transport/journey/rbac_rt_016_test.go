package journey_test

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// rt16SettingsSnapshot grants the settings page and its content/actions
// features to role only. Callers outside role prove the gate checks grants,
// not just row presence.
func rt16SettingsSnapshot(role string) roleaccess.Snapshot {
	return roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: role, PageID: "settings", View: true, Create: true, Update: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: role, PageID: "settings", FeatureID: "content", View: true},
			{Version: 1, RoleID: role, PageID: "settings", FeatureID: "actions", View: true, Create: true, Update: true},
		},
	}
}

// TestTodo_RBAC_RT_016_Security proves the newly table-gated calls refuse
// without a grant: the preference reads and writes, the worker-id read, and
// the role-access read deny when the tenant holds no matching rows, and a
// settings grant for another role admits nothing.
func TestTodo_RBAC_RT_016_Security(t *testing.T) {
	t.Run("preference reads deny without grants", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt2Snapshot()}})
		ctx := rt2CallContext(t, rt2WorkerToken)
		if _, err := client.GetProductPreferences(ctx, &journeyv1.GetProductPreferencesRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("GetProductPreferences code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
		if _, err := client.RecordWorkflowUse(ctx, &journeyv1.RecordWorkflowUseRequest{WorkflowId: "promotion"}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("RecordWorkflowUse code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("another role's settings grant admits nothing", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt16SettingsSnapshot("manager")}})
		ctx := rt2CallContext(t, rt2WorkerToken)
		if _, err := client.GetProductPreferences(ctx, &journeyv1.GetProductPreferencesRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("GetProductPreferences code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("comp_admin without worker-id rows is refused the policy read", func(t *testing.T) {
		snapshot := rt2Snapshot()
		snapshot.Assignments = append(snapshot.Assignments, roleaccess.Assignment{Version: 1, WorkerRef: "rbac-rex", RoleIDs: []string{"comp_admin"}})
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}, WorkerIDs: &workerIDStoreSpy{}})
		if _, err := client.GetWorkerIDPolicy(rt2CallContext(t, rt2RevokedToken), &journeyv1.GetWorkerIDPolicyRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("GetWorkerIDPolicy code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("administrator without roles rows is refused the access read", func(t *testing.T) {
		snapshot := roleaccess.Snapshot{
			Roles: roleaccess.DefaultRoles(),
			PagePermissions: []roleaccess.PagePermission{
				{Version: 1, RoleID: "hcm_admin", PageID: "people", View: true},
			},
			FeaturePermissions: []roleaccess.FeaturePermission{
				{Version: 1, RoleID: "hcm_admin", PageID: "people", FeatureID: "directory", View: true},
			},
		}
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}})
		if _, err := client.GetRoleAccess(rt2CallContext(t, rt2AdminToken), &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("GetRoleAccess code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("page grant without admin duty is refused the access read", func(t *testing.T) {
		snapshot := roleaccess.Snapshot{
			Roles: roleaccess.DefaultRoles(),
			PagePermissions: []roleaccess.PagePermission{
				{Version: 1, RoleID: "worker_self", PageID: "roles", View: true},
			},
			FeaturePermissions: []roleaccess.FeaturePermission{
				{Version: 1, RoleID: "worker_self", PageID: "roles", FeatureID: "actions", View: true},
			},
		}
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}})
		if _, err := client.GetRoleAccess(rt2CallContext(t, rt2WorkerToken), &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("GetRoleAccess code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("empty workflow id stays invalid", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt16SettingsSnapshot("worker_self")}})
		if _, err := client.RecordWorkflowUse(rt2CallContext(t, rt2WorkerToken), &journeyv1.RecordWorkflowUseRequest{}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("RecordWorkflowUse code = %v, want InvalidArgument (err=%v)", status.Code(err), err)
		}
	})
}

// TestTodo_RBAC_RT_016_Integration proves the table-gated calls serve end to
// end over the wire once the grant exists: the preference read and write,
// the worker-id policy read, and the role-access read all succeed with
// matching page and feature rows behind the caller's roles.
func TestTodo_RBAC_RT_016_Integration(t *testing.T) {
	t.Run("granted preference read and write serve", func(t *testing.T) {
		spy := &preferenceSpy{snapshot: preferences.DefaultSnapshot()}
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), Preferences: spy, RoleAccess: &rt2Store{snapshot: rt16SettingsSnapshot("worker_self")}})
		ctx := rt2CallContext(t, rt2WorkerToken)
		loaded, err := client.GetProductPreferences(ctx, &journeyv1.GetProductPreferencesRequest{})
		if err != nil {
			t.Fatalf("granted GetProductPreferences: %v", err)
		}
		if loaded.GetUser() == nil {
			t.Fatal("granted GetProductPreferences returned no user")
		}
		used, err := client.RecordWorkflowUse(ctx, &journeyv1.RecordWorkflowUseRequest{WorkflowId: "promotion"})
		if err != nil {
			t.Fatalf("granted RecordWorkflowUse: %v", err)
		}
		if used.GetUser().GetWorkflowUses()["promotion"] != 1 {
			t.Fatalf("granted RecordWorkflowUse count = %d, want 1", used.GetUser().GetWorkflowUses()["promotion"])
		}
	})

	t.Run("granted worker-id policy read serves", func(t *testing.T) {
		snapshot := roleaccess.Snapshot{
			Roles: roleaccess.DefaultRoles(),
			Assignments: []roleaccess.Assignment{
				{Version: 1, WorkerRef: "rbac-rex", RoleIDs: []string{"comp_admin"}},
			},
			PagePermissions: []roleaccess.PagePermission{
				{Version: 1, RoleID: "comp_admin", PageID: "worker-ids", View: true},
			},
			FeaturePermissions: []roleaccess.FeaturePermission{
				{Version: 1, RoleID: "comp_admin", PageID: "worker-ids", FeatureID: "content", View: true},
			},
		}
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}, WorkerIDs: &workerIDStoreSpy{}})
		loaded, err := client.GetWorkerIDPolicy(rt2CallContext(t, rt2RevokedToken), &journeyv1.GetWorkerIDPolicyRequest{})
		if err != nil {
			t.Fatalf("granted GetWorkerIDPolicy: %v", err)
		}
		if loaded.GetPolicy() == nil {
			t.Fatal("granted GetWorkerIDPolicy returned no policy")
		}
	})

	t.Run("granted role-access read serves", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt2Snapshot()}})
		loaded, err := client.GetRoleAccess(rt2CallContext(t, rt2AdminToken), &journeyv1.GetRoleAccessRequest{})
		if err != nil {
			t.Fatalf("granted GetRoleAccess: %v", err)
		}
		if len(loaded.GetRoles()) == 0 {
			t.Fatal("granted GetRoleAccess returned no roles")
		}
	})
}
