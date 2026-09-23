package journey_test

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// TestTodo_RBAC_RT_015_Integration proves per-page judgment over the wire:
// a tenant whose rows cover only the journeys page serves journey reads
// while refusing people reads, so whether one page is judged never depends
// on which other pages hold rows.
func TestTodo_RBAC_RT_015_Integration(t *testing.T) {
	journeysOnly := roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: "worker_self", PageID: "journeys", View: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: "worker_self", PageID: "journeys", FeatureID: "content", View: true},
			{Version: 1, RoleID: "worker_self", PageID: "journeys", FeatureID: "journey_list", View: true},
		},
	}
	client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: journeysOnly}})
	ctx := rt2CallContext(t, rt2WorkerToken)

	if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{}); err != nil {
		t.Fatalf("journeys-only ListJourneys: %v", err)
	}
	if _, err := client.ListWorkers(ctx, &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("journeys-only ListWorkers code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
	}
}
