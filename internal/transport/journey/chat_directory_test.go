package journey_test

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type tenantChatDirectoryEngine struct {
	*fakeEngine
	populations map[string][]workspace.WorkerSummary
}

func (e *tenantChatDirectoryEngine) ListWorkers(ctx context.Context) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	principal, ok := trust.FromContext(ctx)
	if !ok {
		return nil, workspace.WorkforceOptions{}, trust.ErrNoPrincipal
	}
	return e.populations[string(principal.Tenant())], workspace.WorkforceOptions{}, nil
}

func TestListChatDirectoryReturnsBusinessProjectionAndReportingLinks(t *testing.T) {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		{WorkerRef: "viewer", WorkerID: "viewer-id", LegalName: "Vera Viewer", PreferredName: "Vera", OrgUnit: "People", ManagerDisposition: workspace.ManagerRelationshipRoot},
		{WorkerRef: "manager", WorkerID: "manager-id", LegalName: "Morgan Manager", PreferredName: "Morgan", JobTitle: "Director", OrgUnit: "Executive", ManagerRef: "viewer"},
		{WorkerRef: "report", WorkerID: "report-id", LegalName: "Riley Report", PreferredName: "Riley", JobTitle: "Analyst", OrgUnit: "Finance", ManagerRef: "manager", BasePay: "200000", Currency: "USD", BonusTarget: "0.2", PayBasis: "ANNUAL_SALARY", HireDate: "2020-01-01", WorkerNumber: "W-123", Grade: "G8", PositionID: "P-2", PayZone: "US", LifecycleStatus: "ACTIVE", WorkerType: "EMPLOYEE"},
		{WorkerRef: "external", WorkerID: "external-id", LegalName: "External", ManagerRef: "board:tenant"},
	}
	engine.options = fixtureWorkforceOptions()
	conn := startTestServer(t, journey.Dependencies{Engine: engine})
	client := journeyv1.NewJourneyServiceClient(conn)
	response, err := client.ListChatDirectory(testContext(t), &journeyv1.ListChatDirectoryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 4 {
		t.Fatalf("chat directory workers = %d, want full tenant population 4", len(response.GetWorkers()))
	}
	byRef := make(map[string]*journeyv1.Worker, len(response.GetWorkers()))
	for _, worker := range response.GetWorkers() {
		byRef[worker.GetWorkerRef()] = worker
	}
	manager := byRef["manager"]
	if manager == nil || manager.GetManagerRelationship().GetManagerWorkerRef() != "viewer" {
		t.Fatalf("manager link = %+v, want viewer endpoint", manager)
	}
	if got := byRef["report"].GetManagerRelationship().GetManagerWorkerRef(); got != "manager" {
		t.Fatalf("report manager = %q, want manager endpoint", got)
	}
	if got := byRef["external"].GetManagerRelationship().GetDisposition(); got != journeyv1.ManagerRelationshipProjection_DISPOSITION_ORPHAN {
		t.Fatalf("external manager disposition = %v, want ORPHAN", got)
	}
	report := byRef["report"]
	if report.GetLegalName() != "Riley Report" || report.GetPreferredName() != "Riley" || report.GetJobTitle() != "Analyst" || report.GetOrgUnit() != "Finance" {
		t.Fatalf("business fields missing from report projection: %+v", report)
	}
	if report.GetBasePay() != "" || report.GetBonusTarget() != "" || report.GetCurrency() != "" || report.GetPayBasis() != "" || report.GetHireDate() != "" || report.GetWorkerNumber() != "" || report.GetGrade() != "" || report.GetPositionId() != "" || report.GetPayZone() != "" || report.GetLifecycleStatus() != "" || report.GetWorkerType() != "" {
		t.Fatalf("chat directory disclosed non-chat fields: %+v", report)
	}
}

func TestListChatDirectoryUsesAuthenticatedTenantPopulation(t *testing.T) {
	engine := &tenantChatDirectoryEngine{
		fakeEngine: newFakeEngine(),
		populations: map[string][]workspace.WorkerSummary{
			fixtureTenant:  {{WorkerRef: "tenant-a-person", LegalName: "Tenant A"}},
			"other-tenant": {{WorkerRef: "tenant-b-person", LegalName: "Tenant B"}},
		},
	}
	client := journeyv1.NewJourneyServiceClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	for _, tc := range []struct{ token, want, forbidden string }{
		{fixtureManagerToken, "tenant-a-person", "tenant-b-person"},
		{fixtureOtherTenantToken, "tenant-b-person", "tenant-a-person"},
	} {
		response, err := client.ListChatDirectory(withToken(context.Background(), tc.token), &journeyv1.ListChatDirectoryRequest{})
		if err != nil {
			t.Fatalf("ListChatDirectory(%s): %v", tc.token, err)
		}
		if len(response.GetWorkers()) != 1 || response.GetWorkers()[0].GetWorkerRef() != tc.want {
			t.Fatalf("tenant directory = %+v, want only %q", response.GetWorkers(), tc.want)
		}
		if response.GetWorkers()[0].GetWorkerRef() == tc.forbidden {
			t.Fatalf("tenant directory leaked %q", tc.forbidden)
		}
	}
}
