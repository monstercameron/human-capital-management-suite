package main

import (
	"strings"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func issue3986Journey(id, name string, stage journeyv1.JourneyStage, responsibility journeyv1.JourneyViewerResponsibility, relationships ...journeyv1.JourneyViewerRelationship) *journeyv1.Journey {
	return &journeyv1.Journey{IntentId: id, WorkerName: name, Stage: stage,
		Viewer: &journeyv1.JourneyViewerProjection{Responsibility: responsibility, Relationships: relationships}}
}

// The same journey is something to act on for its reviewer and something to
// follow for its requester; the viewer-specific responsibility decides, never
// the status text.
func TestIssue3986_ResponsibilityIsViewerSpecific(t *testing.T) {
	stage := journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL
	reviewer := issue3986Journey("j1", "Sam Lee", stage, journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED)
	requester := issue3986Journey("j1", "Sam Lee", stage, journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_TRACKING, journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR)
	if got := chatAttentionItems("en-US", []*journeyv1.Journey{reviewer}); len(got) != 1 || got[0].Responsibility != chatui.AttentionAction {
		t.Fatalf("reviewer: %+v", got)
	}
	if got := chatAttentionItems("en-US", []*journeyv1.Journey{requester}); len(got) != 1 || got[0].Responsibility != chatui.AttentionWaiting {
		t.Fatalf("requester: %+v", got)
	}
}

func TestIssue3986_UnstatedResponsibilityIsUnknownNotPassive(t *testing.T) {
	j := issue3986Journey("j2", "Kim", journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL, journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_UNSPECIFIED, journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR)
	got := chatAttentionItems("en-US", []*journeyv1.Journey{j})
	if len(got) != 1 || got[0].Responsibility != chatui.AttentionUnknown {
		t.Fatalf("got %+v", got)
	}
}

func TestIssue3986_OnlyTheViewersOwnOpenWork(t *testing.T) {
	stage := journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL
	visibleOnly := issue3986Journey("seen", "Someone Else", stage, journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_OBSERVING)
	closed := issue3986Journey("done", "Closed One", stage, journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED)
	closed.Viewer.Closed = true
	held := issue3986Journey("held", "Held One", stage, journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_TRACKING)
	held.CurrentWorkItem = &journeyv1.JourneyWorkItemSummary{ViewerMembership: "ASSIGNEE", DueAt: timestamppb.New(time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC))}
	got := chatAttentionItems("en-US", []*journeyv1.Journey{visibleOnly, closed, held, nil})
	if len(got) != 1 || got[0].ID != "held" {
		t.Fatalf("population: %+v", got)
	}
	if got[0].Due.IsZero() || got[0].Href == "" || !strings.HasPrefix(got[0].Href, "/") || got[0].Status == "" {
		t.Fatalf("facts missing from the owner's answer: %+v", got[0])
	}
}

func TestIssue3986_NoDueDateStaysUnset(t *testing.T) {
	j := issue3986Journey("j3", "Ray", journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL, journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED)
	if got := chatAttentionItems("de-DE", []*journeyv1.Journey{j}); len(got) != 1 || !got[0].Due.IsZero() || got[0].Source == "Promotion" {
		t.Fatalf("got %+v", got)
	}
}
