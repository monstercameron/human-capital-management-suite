package journey_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// RBAC-RT-006 fixtures. rt2Store, rt2Snapshot, rt2Engine, startRT2Server and
// the rt2 tokens live in rbac_rt_002_test.go; these snapshots isolate the
// missing-data shapes: nothing stored at all, and page rows with no feature
// rows (the rolling-upgrade page-only path the fix removes).
func rt6PageOnlySnapshot() roleaccess.Snapshot {
	return roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: "worker_self", PageID: "people", View: true},
			{Version: 1, RoleID: "worker_self", PageID: "journeys", View: true, Create: true, Update: true},
		},
	}
}

// rt6ErrStore fails every Load: a missing or unreadable permission table
// must refuse, never admit.
type rt6ErrStore struct{ err error }

func (s *rt6ErrStore) Bootstrap(context.Context, values.TenantId, string) error { return nil }
func (s *rt6ErrStore) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return roleaccess.Snapshot{}, s.err
}
func (s *rt6ErrStore) SaveRole(_ context.Context, _ values.TenantId, _ string, value roleaccess.Role) (roleaccess.Role, error) {
	return value, nil
}
func (s *rt6ErrStore) SaveAssignment(_ context.Context, _ values.TenantId, _ string, value roleaccess.Assignment) (roleaccess.Assignment, error) {
	return value, nil
}
func (s *rt6ErrStore) SaveVisibility(_ context.Context, _ values.TenantId, _, _ string, value roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	return value, nil
}
func (s *rt6ErrStore) SavePagePermission(_ context.Context, _ values.TenantId, _ string, value roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	return value, nil
}
func (s *rt6ErrStore) SaveFeaturePermission(_ context.Context, _ values.TenantId, _ string, value roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	return value, nil
}

// TestTodo_RBAC_RT_006 is the PRIMARY matrix entry: an absent store, an
// empty permission table, or page rows without feature rows denies; a
// seeded page grant together with its feature grant still allows.
func TestTodo_RBAC_RT_006(t *testing.T) {
	t.Run("nil store denies page reads", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine()})
		if _, err := client.ListWorkers(rt2CallContext(t, rt2WorkerToken), &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("nil-store ListWorkers code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("empty permission table denies", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{}})
		if _, err := client.ListWorkers(rt2CallContext(t, rt2WorkerToken), &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("empty-table ListWorkers code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("page grant without feature rows denies reads", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt6PageOnlySnapshot()}})
		if _, err := client.ListWorkers(rt2CallContext(t, rt2WorkerToken), &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("page-only ListWorkers code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("page grant without feature rows denies writes", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt6PageOnlySnapshot()}})
		if _, err := client.ProposeJourney(rt2CallContext(t, rt2WorkerToken), &journeyv1.ProposeJourneyRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("page-only ProposeJourney code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("seeded page and feature grants still allow", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt2Snapshot()}})
		resp, err := client.ListWorkers(rt2CallContext(t, rt2WorkerToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("seeded ListWorkers: %v", err)
		}
		if len(resp.GetWorkers()) == 0 {
			t.Fatal("seeded ListWorkers returned no rows")
		}
	})
}

// TestTodo_RBAC_RT_006_Security proves no fail-open across missing-data
// shapes: every gate kind refuses when the store is absent or the table is
// empty, a page-level grant never authorizes a write on its own, and a
// transport failure loading the table refuses rather than admits.
func TestTodo_RBAC_RT_006_Security(t *testing.T) {
	stores := map[string]roleaccess.Store{
		"nil store":   nil,
		"empty table": &rt2Store{},
	}
	for name, store := range stores {
		t.Run("missing data denies every gate/"+name, func(t *testing.T) {
			client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: store})
			ctx := rt2CallContext(t, rt2WorkerToken)
			if _, err := client.ListWorkers(ctx, &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("ListWorkers code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
			}
			if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("ListJourneys code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
			}
			if _, err := client.ProposeJourney(ctx, &journeyv1.ProposeJourneyRequest{}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("ProposeJourney code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
			}
			if _, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{
				Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-fay", RoleIds: []string{"manager"}},
			}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("SaveWorkerRoleAssignment code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
			}
		})
	}

	t.Run("failing table load refuses without admitting", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt6ErrStore{err: context.DeadlineExceeded}})
		// The transport-error mapping (unspecified failure) predates this
		// todo; what matters here is that a load failure never admits.
		if _, err := client.ListWorkers(rt2CallContext(t, rt2WorkerToken), &journeyv1.ListWorkersRequest{}); err == nil {
			t.Fatal("ListWorkers allowed with a failing permission table")
		} else if code := status.Code(err); code != codes.PermissionDenied && code != codes.Unknown && code != codes.Internal && code != codes.Unavailable {
			t.Fatalf("ListWorkers code = %v, want a refusal", code)
		}
	})

	t.Run("page-level grant never authorizes a write alone", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt6PageOnlySnapshot()}})
		ctx := rt2CallContext(t, rt2WorkerToken)
		if _, err := client.ProposeJourney(ctx, &journeyv1.ProposeJourneyRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("page-only ProposeJourney code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
		if _, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{
			Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-fay", RoleIds: []string{"manager"}},
		}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("page-only SaveWorkerRoleAssignment code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})
}

// TestTodo_RBAC_RT_006_Integration proves the fail-closed gates end to end
// over the wire: a tenant with no rows and a durable assignment with no
// permission rows are both refused, while the seeded tenant is served.
func TestTodo_RBAC_RT_006_Integration(t *testing.T) {
	t.Run("tenant with no rows is refused", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{}})
		if _, err := client.ListWorkers(rt2CallContext(t, rt2WorkerToken), &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("empty ListWorkers code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
		if _, err := client.ListJourneys(rt2CallContext(t, rt2WorkerToken), &journeyv1.ListJourneysRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("empty ListJourneys code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("durable assignment without permission rows grants nothing", func(t *testing.T) {
		snapshot := roleaccess.Snapshot{
			Roles:       roleaccess.DefaultRoles(),
			Assignments: []roleaccess.Assignment{{Version: 1, WorkerRef: "rbac-eli", RoleIDs: []string{"comp_admin"}}},
		}
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}})
		if _, err := client.ListWorkers(rt2CallContext(t, rt2WorkerToken), &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("assignment-only ListWorkers code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("seeded tenant is served", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt2Snapshot()}})
		resp, err := client.ListWorkers(rt2CallContext(t, rt2WorkerToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("seeded ListWorkers: %v", err)
		}
		if len(resp.GetWorkers()) == 0 {
			t.Fatal("seeded ListWorkers returned no rows")
		}
	})
}
