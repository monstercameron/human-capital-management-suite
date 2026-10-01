package productclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func uxblindAF117Promotion(id, current, proposed, basis string) *journeyv1.Journey {
	return &journeyv1.Journey{
		IntentId: id, WorkerRef: "worker-" + id, WorkerName: "Worker " + id,
		CurrentBase: current, ProposedBase: proposed, Currency: "USD",
		CurrentPayBasis: basis, ProposedPayBasis: basis,
		Stage: journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL,
		Viewer: &journeyv1.JourneyViewerProjection{
			Responsibility: journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED,
			Relationships:  []journeyv1.JourneyViewerRelationship{journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_ASSIGNEE},
		},
	}
}

func TestTodo_UXBLIND_117(t *testing.T) {
	items, err := projectJourneys([]*journeyv1.Journey{
		uxblindAF117Promotion("annual", "132000.00", "160000.00", "ANNUAL_SALARY"),
		uxblindAF117Promotion("hourly", "34.50", "40.00", "HOURLY_RATE"),
	})
	if err != nil {
		t.Fatalf("projectJourneys: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("projected %d work items, want annual and hourly promotions", len(items))
	}
	wants := []struct {
		id, current, proposed, basis string
	}{
		{id: "annual", current: "132000.00 USD", proposed: "160000.00 USD", basis: "ANNUAL_SALARY"},
		{id: "hourly", current: "34.50 USD", proposed: "40.00 USD", basis: "HOURLY_RATE"},
	}
	for index, want := range wants {
		item := items[index]
		if item.ID != want.id || item.CurrentBase.String() != want.current || item.ProposedBase.String() != want.proposed {
			t.Fatalf("item %d projection = id %q current %q proposed %q", index, item.ID, item.CurrentBase, item.ProposedBase)
		}
		if item.CurrentPayBasis != want.basis || item.ProposedPayBasis != want.basis {
			t.Fatalf("item %q pay bases = %q/%q, want %q/%q", item.ID, item.CurrentPayBasis, item.ProposedPayBasis, want.basis, want.basis)
		}
	}
}

func TestTodo_UXBLIND_117_Browser(t *testing.T) {
	items, err := projectJourneys([]*journeyv1.Journey{
		uxblindAF117Promotion("annual", "132000.00", "160000.00", "ANNUAL_SALARY"),
		uxblindAF117Promotion("hourly", "34.50", "40.00", "HOURLY_RATE"),
	})
	if err != nil {
		t.Fatalf("projectJourneys: %v", err)
	}
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		for _, item := range items {
			view := productui.NewView(productui.PageWork, "tenant", "principal", "scope")
			view.Locale = productui.ResolveProductLocale(localeName)
			view.Viewer = productui.ViewerProfile{PersonID: "viewer"}
			view.Work = items
			view.SelectedWork = item.ID
			markup, renderErr := productui.Render(view)
			if renderErr != nil {
				t.Fatalf("render %s/%s: %v", localeName, item.ID, renderErr)
			}
			basis := "annual"
			if item.CurrentPayBasis == "HOURLY_RATE" {
				basis = "hourly_rate"
			}
			for _, amount := range []string{item.CurrentBase.Amount().String(), item.ProposedBase.Amount().String()} {
				want := view.Locale.FormatMoneyWithUnit(amount, "USD", basis, 2)
				if !strings.Contains(markup, want) {
					t.Errorf("%s/%s rail omitted %q", localeName, item.ID, want)
				}
			}
		}
	}
}
