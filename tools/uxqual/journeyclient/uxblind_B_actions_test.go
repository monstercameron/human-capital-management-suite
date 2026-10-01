package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

func TestTodo_UXBLIND_005(t *testing.T) {
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	detail.Journey.Viewer = &journeyv1.JourneyViewerProjection{}
	page := DetailPageWithInterventions(testConfig(), detail, nil, nil, nil, nil, nil)
	if len(page.Detail.Actions) != 0 {
		t.Fatalf("viewer without a server relationship received journey actions: %+v", page.Detail.Actions)
	}
}

func TestTodo_UXBLIND_005_Browser(t *testing.T) {
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	detail.Journey.Viewer = &journeyv1.JourneyViewerProjection{
		Relationships: []journeyv1.JourneyViewerRelationship{journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR},
	}
	preview := &journeyv1.PreviewJourneyInterventionResponse{Available: true}
	page := DetailPageWithInterventions(testConfig(), detail, nil, nil, preview, preview, nil)
	for _, action := range page.Detail.Actions {
		if action.ID == ActionWithdraw || action.ID == ActionCancel || action.ID == ActionEditProposal {
			return
		}
	}
	t.Fatal("proposer did not receive an authorized intervention action")
}

func TestTodo_UXBLIND_005_Security(t *testing.T) {
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	detail.Journey.Viewer = &journeyv1.JourneyViewerProjection{}
	page := DetailPageWithInterventions(testConfig(), detail, nil, nil, nil, nil, nil)
	for _, action := range page.Detail.Actions {
		if action.ID == ActionExecute || action.ID == ActionWithdraw || action.ID == ActionCancel || action.ID == ActionEditProposal {
			t.Fatalf("unauthorized viewer received %q", action.ID)
		}
	}
}
