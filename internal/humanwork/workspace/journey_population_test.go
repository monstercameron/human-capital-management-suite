package workspace_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestSummarizeJourneysReconcilesTheVisibleSet pins the UXLIVE-027 summary:
// every bucket is a count over exactly the journeys supplied, Total is
// Active + Closed, and the freshness coordinate is the newest change.
func TestSummarizeJourneysReconcilesTheVisibleSet(t *testing.T) {
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	journeys := []workspace.JourneySummary{
		{IntentID: "a", Stage: workspace.JourneyStageProposed, UpdatedAt: base,
			Viewer: workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityActionRequired}},
		{IntentID: "b", Stage: workspace.JourneyStageManagerApproval, UpdatedAt: base.Add(time.Hour),
			Viewer: workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityTracking,
				Relationships: []workspace.JourneyViewerRelationship{workspace.JourneyViewerInitiator}}},
		{IntentID: "c", Stage: workspace.JourneyStageBlocked, UpdatedAt: base.Add(2 * time.Hour),
			Viewer: workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityActionRequired}},
		{IntentID: "d", Stage: workspace.JourneyStageRecorded, UpdatedAt: base.Add(3 * time.Hour),
			Viewer: workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityClosed, Closed: true,
				Relationships: []workspace.JourneyViewerRelationship{workspace.JourneyViewerInitiator}}},
		{IntentID: "e", Stage: workspace.JourneyStageFailed, UpdatedAt: base.Add(-time.Hour),
			Viewer: workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityClosed, Closed: true}},
		{IntentID: "f", Stage: workspace.JourneyStageProposed, UpdatedAt: base,
			Viewer: workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityObserving}},
	}
	computed := base.Add(24 * time.Hour)
	got := workspace.SummarizeJourneys(journeys, computed)
	want := workspace.JourneyPopulation{
		Total: 6, Active: 4, Closed: 2, NeedsAction: 2, Tracking: 1, Exceptions: 2,
		Stages: []workspace.JourneyStageCount{
			{Stage: workspace.JourneyStageBlocked, Count: 1},
			{Stage: workspace.JourneyStageFailed, Count: 1},
			{Stage: workspace.JourneyStageManagerApproval, Count: 1},
			{Stage: workspace.JourneyStageProposed, Count: 2},
			{Stage: workspace.JourneyStageRecorded, Count: 1},
		},
		LatestUpdate: base.Add(3 * time.Hour),
		ComputedAt:   computed,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SummarizeJourneys = %+v\nwant %+v", got, want)
	}
	if got.Total != got.Active+got.Closed {
		t.Fatalf("Total %d != Active %d + Closed %d", got.Total, got.Active, got.Closed)
	}

	empty := workspace.SummarizeJourneys(nil, computed)
	if empty.Total != 0 || !empty.LatestUpdate.IsZero() || len(empty.Stages) != 0 || !empty.ComputedAt.Equal(computed) {
		t.Fatalf("an empty set summarized to %+v", empty)
	}
	for _, stage := range []workspace.JourneyStage{workspace.JourneyStageBlocked, workspace.JourneyStageFailed, workspace.JourneyStageRepairRequired} {
		if !workspace.JourneyStageIsException(stage) {
			t.Fatalf("%s is an exception", stage)
		}
	}
	if workspace.JourneyStageIsException(workspace.JourneyStageProposed) {
		t.Fatal("PROPOSED is not an exception")
	}
}
