package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// UXBLIND-005: the start action is offered only when the server says this
// viewer may execute. An initiator without the execution role (the live Luis
// Ortega refusal) sees interventions but no start; an authorized operator who
// did not initiate still sees it. Fixtures without a viewer projection keep
// their legacy unfiltered shape.
func TestTodo_UXBLIND_005_CanExecute(t *testing.T) {
	preview := &journeyv1.PreviewJourneyInterventionResponse{Available: true}
	initiator := &journeyv1.JourneyViewerProjection{
		Relationships: []journeyv1.JourneyViewerRelationship{journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR},
	}

	t.Run("initiator without the execution role is not offered the start", func(t *testing.T) {
		detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
		detail.Journey.Viewer = initiator
		detail.CanExecute = false
		page := DetailPageWithInterventions(clockTestNow, testConfig(), detail, nil, nil, preview, preview, nil)
		for _, action := range page.Detail.Actions {
			if action.ID == ActionExecute {
				t.Fatalf("unauthorized initiator was offered the start: %+v", page.Detail.Actions)
			}
		}
	})

	t.Run("initiator with the execution role is offered the start", func(t *testing.T) {
		detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
		detail.Journey.Viewer = initiator
		detail.CanExecute = true
		page := DetailPageWithInterventions(clockTestNow, testConfig(), detail, nil, nil, preview, preview, nil)
		for _, action := range page.Detail.Actions {
			if action.ID == ActionExecute {
				return
			}
		}
		t.Fatalf("authorized initiator was not offered the start: %+v", page.Detail.Actions)
	})

	t.Run("authorized operator who did not initiate is offered the start", func(t *testing.T) {
		detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
		detail.Journey.Viewer = &journeyv1.JourneyViewerProjection{}
		detail.CanExecute = true
		page := DetailPageWithInterventions(clockTestNow, testConfig(), detail, nil, nil, nil, nil, nil)
		for _, action := range page.Detail.Actions {
			if action.ID == ActionExecute {
				return
			}
		}
		t.Fatalf("authorized operator was not offered the start: %+v", page.Detail.Actions)
	})

	t.Run("legacy fixtures without a viewer keep their shape", func(t *testing.T) {
		detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
		page := DetailPageWithInterventions(clockTestNow, testConfig(), detail, nil, nil, nil, nil, nil)
		for _, action := range page.Detail.Actions {
			if action.ID == ActionExecute {
				return
			}
		}
		t.Fatalf("legacy detail lost its start action: %+v", page.Detail.Actions)
	})
}
