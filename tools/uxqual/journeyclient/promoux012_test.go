package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// TestTodo_PROMOUX_012 is the Journeys tracker's share of the PRIMARY: its
// cards and per-employee open counts read the next step and closure off the
// server's viewer projection. The fixture makes the two sources disagree on
// purpose -- a RECORDED stage (which the tracker's old stage table treated as
// open) and a projection whose next step differs from what the stage alone
// would suggest -- so a projector that consulted the stage would fail.
func TestTodo_PROMOUX_012(t *testing.T) {
	recorded := &journeyv1.Journey{IntentId: "recorded", WorkerRef: "omar-reyes", WorkerName: "Omar Reyes", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED,
		Viewer: &journeyv1.JourneyViewerProjection{Closed: true, Responsibility: journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_CLOSED}}
	waiting := &journeyv1.Journey{IntentId: "waiting", WorkerRef: "omar-reyes", WorkerName: "Omar Reyes", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE,
		Viewer: &journeyv1.JourneyViewerProjection{
			Responsibility: journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_TRACKING,
			NextStep:       journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_SYSTEM_PROCESSING, NextStepOwner: journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_SYSTEM,
		}}
	data := testListData(t, "")
	data.Journeys = []*journeyv1.Journey{recorded, waiting}
	page := ListPage(testConfig(), data, nil, nil)

	cards := map[string]int{}
	for index, card := range page.List.Journeys {
		cards[card.IntentID] = index
	}
	recordedCard, waitingCard := page.List.Journeys[cards["recorded"]], page.List.Journeys[cards["waiting"]]
	if !recordedCard.Closed || recordedCard.NextStep != "" {
		t.Fatalf("recorded card = closed %t next %q, want closed with no next step", recordedCard.Closed, recordedCard.NextStep)
	}
	if waitingCard.Closed || waitingCard.NextStep != NextStepLabel(NextStepSystemProcessing) {
		t.Fatalf("waiting card next step = %q, want the projection's %q", waitingCard.NextStep, NextStepLabel(NextStepSystemProcessing))
	}
	if cards["waiting"] > cards["recorded"] {
		t.Fatal("the closed journey sorted ahead of the open one in its subject group")
	}
	omar, ok := cardFor(page.List.People, "omar-reyes")
	if !ok || omar.OpenJourneys != 1 {
		t.Fatalf("Omar's open journeys = %d, want 1: a RECORDED journey is closed", omar.OpenJourneys)
	}
	if recordedCard.StageLabel != "Recorded" || waitingCard.StageLabel != "Waiting for effective date" {
		t.Fatalf("labels = %q / %q", recordedCard.StageLabel, waitingCard.StageLabel)
	}

	if got := JourneyViewerRelationships(&journeyv1.Journey{Viewer: &journeyv1.JourneyViewerProjection{Relationships: []journeyv1.JourneyViewerRelationship{
		journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_UNSPECIFIED,
		journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_CANDIDATE,
		journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR,
	}}}); len(got) != 2 || got[0] != RelationshipCandidate || got[1] != RelationshipInitiator {
		t.Fatalf("relationships = %v", got)
	}
	for wire, want := range map[journeyv1.JourneyViewerResponsibility]string{
		journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_UNSPECIFIED:     "",
		journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED: ResponsibilityActionRequired,
		journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_TRACKING:        ResponsibilityTracking,
		journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_OBSERVING:       ResponsibilityObserving,
		journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_CLOSED:          ResponsibilityClosed,
	} {
		if got := JourneyViewerResponsibility(&journeyv1.Journey{Viewer: &journeyv1.JourneyViewerProjection{Responsibility: wire}}); got != want {
			t.Errorf("%s -> %q, want %q", wire, got, want)
		}
	}
	if JourneyViewerResponsibility(&journeyv1.Journey{}) != "" || JourneyViewerRelationships(nil) != nil {
		t.Fatal("an absent projection produced a responsibility or relationships")
	}
}

// TestTodo_PROMOUX_012_Regression: StagePresentation is total over the stage
// enum and names every known stage, so neither surface can fall back to a
// label of its own.
func TestTodo_PROMOUX_012_Regression(t *testing.T) {
	for value, name := range journeyv1.JourneyStage_name {
		label, tone := StagePresentation(journeyv1.JourneyStage(value))
		if value != 0 && label == "Status unavailable" {
			t.Errorf("%s has no shared label", name)
		}
		switch tone {
		case toneNeutral, toneWarning, toneSuccess, toneDanger:
		default:
			t.Errorf("%s has tone %q outside the shared set", name, tone)
		}
	}
}
