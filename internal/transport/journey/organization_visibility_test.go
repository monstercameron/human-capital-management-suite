package journey_test

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListWorkersEnforcesOrganizationVisibilityBeforeTheResponse(t *testing.T) {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		{WorkerRef: fixtureSubject, WorkerID: "self-id", PreferredName: "Jane", OrgUnit: "People", ManagerRef: "leader-hidden"},
		{WorkerRef: "peer", WorkerID: "peer-id", PreferredName: "Peer", OrgUnit: "People", ManagerRef: fixtureSubject},
		{WorkerRef: "leader-hidden", WorkerID: "leader-id", PreferredName: "Leader", OrgUnit: "Executive", ManagerRef: "board:harborcare"},
		{WorkerRef: "finance", WorkerID: "finance-id", PreferredName: "Finance", OrgUnit: "Finance", ManagerDisposition: workspace.ManagerRelationshipRoot},
	}
	engine.options.OrgUnits = []string{"People", "Executive", "Finance"}
	spy := &preferenceSpy{snapshot: preferences.DefaultSnapshot()}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, Preferences: spy}))

	spy.snapshot.OrganizationVisibility = preferences.OrganizationVisibility{Mode: preferences.OrganizationVisibilityOwnUnit}
	response, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 2 || response.GetWorkers()[0].GetManagerRef() != "" {
		t.Fatalf("own-unit response leaked a worker or hidden manager: %+v", response.GetWorkers())
	}
	selfRelationship := response.GetWorkers()[0].GetManagerRelationship()
	if selfRelationship.GetDisposition() != journeyv1.ManagerRelationshipProjection_DISPOSITION_WITHHELD || selfRelationship.GetManagerWorkerRef() != "" {
		t.Fatalf("hidden manager relationship = %+v, want WITHHELD with no endpoint", selfRelationship)
	}
	peerRelationship := response.GetWorkers()[1].GetManagerRelationship()
	if peerRelationship.GetDisposition() != journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE || peerRelationship.GetManagerWorkerRef() != fixtureSubject || response.GetWorkers()[1].GetManagerRef() != "" {
		t.Fatalf("visible manager relationship = %+v, want exact self endpoint", peerRelationship)
	}
	if got := response.GetOptions().GetOrgUnits(); len(got) != 1 || got[0] != "People" {
		t.Fatalf("own-unit options leaked units: %v", got)
	}

	spy.snapshot.OrganizationVisibility = preferences.OrganizationVisibility{Mode: preferences.OrganizationVisibilityAllowlist, OrganizationUnits: []string{"Finance"}}
	response, err = client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 2 || response.GetWorkers()[0].GetWorkerRef() != fixtureSubject || response.GetWorkers()[1].GetWorkerRef() != "finance" {
		t.Fatalf("allowlist did not preserve self plus the qualified unit: %+v", response.GetWorkers())
	}

	spy.snapshot.OrganizationVisibility = preferences.OrganizationVisibility{Mode: preferences.OrganizationVisibilityDenylist, OrganizationUnits: []string{"Executive"}}
	response, err = client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 3 {
		t.Fatalf("denylist response = %+v", response.GetWorkers())
	}

	adminResponse, err := client.ListWorkers(withToken(context.Background(), fixtureAppearanceAdminToken), &journeyv1.ListWorkersRequest{})
	if err != nil || len(adminResponse.GetWorkers()) != 4 {
		t.Fatalf("administrator must retain the complete configuration population: count=%d err=%v", len(adminResponse.GetWorkers()), err)
	}
	adminSelf := adminResponse.GetWorkers()[0].GetManagerRelationship()
	if adminSelf.GetDisposition() != journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE || adminSelf.GetManagerWorkerRef() != "leader-hidden" {
		t.Fatalf("administrator visible relationship = %+v", adminSelf)
	}
	adminExternal := adminResponse.GetWorkers()[2].GetManagerRelationship()
	if adminExternal.GetDisposition() != journeyv1.ManagerRelationshipProjection_DISPOSITION_ORPHAN || adminExternal.GetManagerWorkerRef() != "" || adminResponse.GetWorkers()[2].GetManagerRef() != "" {
		t.Fatalf("external relationship leaked or was not classified: worker=%+v relationship=%+v", adminResponse.GetWorkers()[2], adminExternal)
	}
}

func TestTodo_UXAUDIT_004_Integration(t *testing.T) {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		{WorkerRef: "manager-a", WorkerID: "manager-id", PreferredName: "Alex", OrgUnit: "People", ManagerDisposition: workspace.ManagerRelationshipRoot},
		{WorkerRef: "report-a", WorkerID: "report-a-id", PreferredName: "Alex", OrgUnit: "People", ManagerRef: "manager-id"},
		{WorkerRef: "root-a", WorkerID: "root-id", PreferredName: "Root", OrgUnit: "People", ManagerDisposition: workspace.ManagerRelationshipRoot},
		{WorkerRef: "external-a", WorkerID: "external-id", PreferredName: "External", OrgUnit: "People", ManagerRef: "board:harborcare"},
	}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	response, err := client.ListWorkers(withToken(context.Background(), fixtureAppearanceAdminToken), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != len(engine.workers) {
		t.Fatalf("workers = %d, want %d", len(response.GetWorkers()), len(engine.workers))
	}
	want := []struct {
		disposition journeyv1.ManagerRelationshipProjection_Disposition
		managerRef  string
	}{
		{journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT, ""},
		{journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE, "manager-a"},
		{journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT, ""},
		{journeyv1.ManagerRelationshipProjection_DISPOSITION_ORPHAN, ""},
	}
	for index, worker := range response.GetWorkers() {
		relationship := worker.GetManagerRelationship()
		if relationship == nil || relationship.GetDisposition() != want[index].disposition || relationship.GetManagerWorkerRef() != want[index].managerRef || worker.GetManagerRef() != "" {
			t.Errorf("worker %q relationship = %+v, want %v/%q", worker.GetWorkerRef(), relationship, want[index].disposition, want[index].managerRef)
		}
	}
}

func TestListWorkersUsesDurableEmployeeRolesAndAdditiveRolePolicies(t *testing.T) {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		{WorkerRef: fixtureSubject, OrgUnit: "People"},
		{WorkerRef: "finance", OrgUnit: "Finance"},
		{WorkerRef: "executive", OrgUnit: "Executive"},
		{WorkerRef: "sales", OrgUnit: "Sales"},
	}
	access := &roleAccessSpy{snapshot: roleaccess.Snapshot{
		Assignments: []roleaccess.Assignment{{WorkerRef: fixtureSubject, RoleIDs: []string{"finance_partner", "executive_partner"}}},
		Policies: []roleaccess.VisibilityPolicy{
			{RoleID: "finance_partner", Mode: roleaccess.VisibilityAllowlist, OrganizationUnits: []string{"Finance"}},
			{RoleID: "executive_partner", Mode: roleaccess.VisibilityAllowlist, OrganizationUnits: []string{"Executive"}},
		},
	}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, RoleAccess: access}))
	response, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 3 || response.GetWorkers()[0].GetWorkerRef() != fixtureSubject || response.GetWorkers()[1].GetWorkerRef() != "finance" || response.GetWorkers()[2].GetWorkerRef() != "executive" {
		t.Fatalf("additive role visibility = %+v", response.GetWorkers())
	}
}

func TestListWorkersDefaultsAnUnconfiguredActiveRoleToOwnUnit(t *testing.T) {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		{WorkerRef: fixtureSubject, OrgUnit: "People"},
		{WorkerRef: "peer", OrgUnit: "People"},
		{WorkerRef: "outside", OrgUnit: "Finance"},
	}
	access := &roleAccessSpy{snapshot: roleaccess.Snapshot{Roles: roleaccess.DefaultRoles()}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, RoleAccess: access}))
	response, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 2 {
		t.Fatalf("unconfigured active-role response leaked workers: %+v", response.GetWorkers())
	}
}

func TestOrganizationVisibilityWriteRequiresAdministrator(t *testing.T) {
	spy := &preferenceSpy{snapshot: preferences.DefaultSnapshot()}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Preferences: spy}))
	request := &journeyv1.SaveOrganizationVisibilityRequest{Policy: &journeyv1.OrganizationVisibilityPolicy{Mode: preferences.OrganizationVisibilityOwnUnit}}
	if _, err := client.SaveOrganizationVisibility(testContext(t), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ordinary user save code=%v err=%v", status.Code(err), err)
	}
	for _, invalid := range []*journeyv1.SaveOrganizationVisibilityRequest{
		nil,
		{},
		{Policy: &journeyv1.OrganizationVisibilityPolicy{Mode: "unexpected"}},
	} {
		if _, err := client.SaveOrganizationVisibility(withToken(context.Background(), fixtureAppearanceAdminToken), invalid); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid administrator save code=%v err=%v", status.Code(err), err)
		}
	}
	response, err := client.SaveOrganizationVisibility(withToken(context.Background(), fixtureAppearanceAdminToken), request)
	if err != nil || response.GetPolicy().GetVersion() != 1 || response.GetPolicy().GetMode() != preferences.OrganizationVisibilityOwnUnit {
		t.Fatalf("administrator save = %+v err=%v", response, err)
	}
}
