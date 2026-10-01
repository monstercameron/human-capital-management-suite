package productclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_UXBLIND_087_Regression walks the client-navigation path the WASM
// route renderer takes for three pages -- parse the address, load the view,
// compose the tab title -- and checks the title after each step. The
// greeting stays on the Home heading and never reaches the tab.
func TestTodo_UXBLIND_087_Regression(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{
				WorkerRef: "ir-001-walt-brennan", WorkerId: "IR-00001", LegalName: "Walt Brennan", PreferredName: "Walt", WorkerNumber: "IR-00001",
				JobCode: "EXEC1", Grade: "E1", OrgUnit: "Executive", PositionId: "pos-1", PayZone: "US-1", BasePay: "250000", Currency: "USD",
				BonusTarget: "0.20", HireDate: "2012-04-02", Source: "CREATED",
				ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_ORPHAN},
			}}}, nil
		},
	}
	session := Session{Tenant: "ironridge-demo", TenantName: "Ironridge Builders", Principal: "ir-001-walt-brennan"}
	for _, step := range []struct {
		path, want string
	}{
		{"/workspace/app/home", "Home · Ironridge Builders"},
		{"/workspace/app/people", "People · Ironridge Builders"},
		{"/workspace/app/journeys", "Journeys · Ironridge Builders"},
		{"/workspace/app/home", "Home · Ironridge Builders"},
	} {
		state, err := ParseState(step.path, "")
		if err != nil {
			t.Fatal(err)
		}
		view, err := Load(context.Background(), service, session, state)
		if err != nil {
			t.Fatalf("%s: %v", step.path, err)
		}
		if got := productui.DocumentTitle(productui.ResolveDocumentPageTitle(view), view.Appearance, view.Tenant); got != step.want {
			t.Errorf("after navigating to %s the title is %q, want %q", step.path, got, step.want)
		}
	}
}
