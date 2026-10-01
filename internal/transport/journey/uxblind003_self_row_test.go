package journey_test

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// uxblind003Workers is a durable population: the WorkerRef is a display
// slug and the signed-in principal's subject is the stored worker key, which
// the listing carries as SubjectID.
func uxblind003Workers() []workspace.WorkerSummary {
	return []workspace.WorkerSummary{
		{WorkerRef: "jane-5c1e0a2b", WorkerID: "5c1e0a2b-0000-5000-8000-000000000001", SubjectID: fixtureSubject, WorkerNumber: "HC-21051", LegalName: "Jane Manager", OrgUnit: "People", BasePay: "132000.00", Currency: "USD"},
		{WorkerRef: "peer-7d2f1c3a", WorkerID: "7d2f1c3a-0000-5000-8000-000000000002", SubjectID: "hc-052-peer", WorkerNumber: "HC-21052", LegalName: "Peer Worker", OrgUnit: "People", BasePay: "90000.00", Currency: "USD"},
		{WorkerRef: "outside-9a8b7c6d", WorkerID: "9a8b7c6d-0000-5000-8000-000000000003", SubjectID: "hc-060-outside", WorkerNumber: "HC-21060", LegalName: "Outside Worker", OrgUnit: "Finance"},
	}
}

// TestTodo_UXBLIND_003 pins both halves of the Myself binding defect at the
// directory boundary Myself reads:
//
//   - a self-service role (worker_self, and the finance partner) holds no
//     People directory grant, so ListWorkers refused it outright and Myself
//     had no row to bind; it is now served exactly its own row;
//   - a durable worker's own principal never matched its row, because the
//     self match ignored SubjectID (the stored worker key the credential
//     carries), so own-unit visibility and self disclosure silently failed.
func TestTodo_UXBLIND_003(t *testing.T) {
	selfService := roleaccess.Snapshot{
		Roles:       roleaccess.DefaultRoles(),
		Assignments: []roleaccess.Assignment{{WorkerRef: fixtureSubject, RoleIDs: []string{"worker_self"}}},
		PagePermissions: []roleaccess.PagePermission{
			{RoleID: "worker_self", PageID: "myself", View: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{RoleID: "worker_self", PageID: "myself", FeatureID: "employment_profile", View: true},
		},
	}

	t.Run("a self-service role reads exactly its own row", func(t *testing.T) {
		engine := newFakeEngine()
		engine.workers = uxblind003Workers()
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, RoleAccess: &roleAccessSpy{snapshot: selfService}}))
		response, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers as worker_self: %v", err)
		}
		if got := response.GetWorkers(); len(got) != 1 || got[0].GetWorkerNumber() != "HC-21051" || got[0].GetSubjectId() != fixtureSubject {
			t.Fatalf("self-service listing = %+v, want only the principal's own row", got)
		}
		if response.GetOptions() != nil {
			t.Fatalf("self-service listing disclosed workforce options: %+v", response.GetOptions())
		}
	})

	t.Run("a role with neither the directory nor Myself stays refused", func(t *testing.T) {
		engine := newFakeEngine()
		engine.workers = uxblind003Workers()
		none := selfService
		none.PagePermissions, none.FeaturePermissions = nil, nil
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, RoleAccess: &roleAccessSpy{snapshot: none}}))
		if _, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("ListWorkers with no grant = %v, want PermissionDenied", err)
		}
	})

	t.Run("own-unit visibility finds the principal by its worker key", func(t *testing.T) {
		engine := newFakeEngine()
		engine.workers = uxblind003Workers()
		access := &roleAccessSpy{snapshot: roleaccess.Snapshot{
			Roles:              roleaccess.DefaultRoles(),
			PagePermissions:    []roleaccess.PagePermission{{RoleID: "intent_author", PageID: "people", View: true}},
			FeaturePermissions: []roleaccess.FeaturePermission{{RoleID: "intent_author", PageID: "people", FeatureID: "directory", View: true}},
		}}
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, RoleAccess: access}))
		response, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		numbers := map[string]bool{}
		for _, worker := range response.GetWorkers() {
			numbers[worker.GetWorkerNumber()] = true
		}
		if len(numbers) != 2 || !numbers["HC-21051"] || !numbers["HC-21052"] {
			t.Fatalf("own-unit listing = %v, want the principal and its unit peer", numbers)
		}
	})
}
