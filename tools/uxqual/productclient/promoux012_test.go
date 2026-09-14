package productclient

import (
	"context"
	"slices"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func promoux012Viewer(responsibility journeyv1.JourneyViewerResponsibility, step journeyv1.JourneyNextStep, owner journeyv1.JourneyStepOwner, person bool, relationships ...journeyv1.JourneyViewerRelationship) *journeyv1.JourneyViewerProjection {
	return &journeyv1.JourneyViewerProjection{Relationships: relationships, Responsibility: responsibility, NextStep: step, NextStepOwner: owner, AwaitsPerson: person}
}

// promoux012Journeys is the wire population PROMOUX-012 is proven over: an
// assigned finance approval, a manager approval the viewer proposed but does
// not hold, a passive wait the viewer proposed, a passive wait with no
// relationship, and a recorded journey (closed).
func promoux012Journeys() []*journeyv1.Journey {
	const (
		action   = journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED
		tracking = journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_TRACKING
		observe  = journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_OBSERVING
	)
	initiator := journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR
	return []*journeyv1.Journey{
		{IntentId: "assigned", WorkerRef: "worker-a", WorkerName: "Ana Lim", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL,
			CurrentWorkItem: &journeyv1.JourneyWorkItemSummary{ViewerMembership: "ASSIGNEE", ViewerPermittedActions: []string{"claim"}},
			Viewer: promoux012Viewer(action, journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_FINANCE_DECISION, journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_FINANCE, true,
				journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_ASSIGNEE)},
		{IntentId: "tracked", WorkerRef: "worker-b", WorkerName: "Ben Cho", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL,
			Viewer: promoux012Viewer(tracking, journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_MANAGER_DECISION, journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_MANAGER, true, initiator)},
		{IntentId: "passive", WorkerRef: "worker-c", WorkerName: "Cy Dore", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE, EffectiveDate: "2027-01-01",
			Viewer: promoux012Viewer(tracking, journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_AWAIT_EFFECTIVE_DATE, journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_SYSTEM, false, initiator)},
		{IntentId: "observed", WorkerRef: "worker-d", WorkerName: "Di Eze", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE,
			Viewer: promoux012Viewer(observe, journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_AWAIT_EFFECTIVE_DATE, journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_SYSTEM, false)},
		{IntentId: "recorded", WorkerRef: "worker-a", WorkerName: "Ana Lim", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED,
			Viewer: &journeyv1.JourneyViewerProjection{Relationships: []journeyv1.JourneyViewerRelationship{initiator},
				Responsibility: journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_CLOSED, Closed: true}},
	}
}

// TestTodo_PROMOUX_012 is the client half of the PRIMARY: My Work's items
// carry the server projection unchanged, the navigation badge counts only
// actionable work, and the tracked filter is a real route.
func TestTodo_PROMOUX_012(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{Journeys: promoux012Journeys()}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
	}
	session := Session{Tenant: "tenant", Principal: "priya"}

	state, err := ParseState("/workspace/app/work", "filter=")
	if err != nil {
		t.Fatal(err)
	}
	view, err := Load(context.Background(), service, session, state)
	if err != nil {
		t.Fatal(err)
	}
	if got := workNavigationCount(view.Navigation); got != 1 {
		t.Fatalf("My Work badge = %d, want 1: passive waits, tracked and observed journeys must not count", got)
	}
	byID := map[string]productui.WorkItem{}
	for _, item := range view.Work {
		byID[item.ID] = item
	}
	if item := byID["assigned"]; item.ViewerResponsibility != "ACTION_REQUIRED" || !slices.Equal(item.ViewerRelationships, []string{"ASSIGNEE"}) ||
		item.NextStep != "finance_decision" || item.WaitingOn != "finance" || !item.AwaitsPerson {
		t.Fatalf("assigned item lost the projection: %+v", item)
	}
	if item := byID["passive"]; item.ViewerResponsibility != "TRACKING" || !slices.Equal(item.ViewerRelationships, []string{"INITIATOR"}) ||
		item.WaitingOn != "system" || item.AwaitsPerson || !productui.WorkIsPassiveWait(item) {
		t.Fatalf("passive wait lost the projection: %+v", item)
	}
	if item := byID["recorded"]; !item.Terminal || item.ViewerResponsibility != "CLOSED" {
		t.Fatalf("recorded journey is not closed: %+v", item)
	}

	tracked, err := ParseState("/workspace/app/work", "filter=tracked")
	if err != nil {
		t.Fatalf("filter=tracked is not a valid route: %v", err)
	}
	trackedView, err := Load(context.Background(), service, session, tracked)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, item := range trackedView.Work {
		ids = append(ids, item.ID)
	}
	if !slices.Equal(ids, []string{"tracked", "passive"}) {
		t.Fatalf("tracked requests = %v, want the viewer's open proposals only", ids)
	}
	if CanonicalHref(tracked) != "/workspace/app/work?filter=tracked" {
		t.Fatalf("tracked route is not canonical: %s", CanonicalHref(tracked))
	}
}

// TestTodo_PROMOUX_012_Regression pins the two status vocabularies UXAUDIT-017
// recorded as disagreeing: for every stage, My Work's status label and
// closure equal the Journeys tracker card's, because both read one
// presentation table and the server's closure.
func TestTodo_PROMOUX_012_Regression(t *testing.T) {
	for value, name := range journeyv1.JourneyStage_name {
		stage := journeyv1.JourneyStage(value)
		closed := stage == journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED || stage == journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED ||
			stage == journeyv1.JourneyStage_JOURNEY_STAGE_FAILED || stage == journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED
		wire := &journeyv1.Journey{IntentId: "intent-" + name, WorkerRef: "worker-x", WorkerName: "X", Stage: stage,
			Viewer: &journeyv1.JourneyViewerProjection{Responsibility: journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_OBSERVING, Closed: closed}}
		items, err := projectJourneys([]*journeyv1.Journey{wire})
		if err != nil {
			t.Fatal(err)
		}
		cards := journeyclient.ListPage(journeyclient.Config{Tenant: "t", Subject: "s"}, journeyclient.ListData{Journeys: []*journeyv1.Journey{wire}}, nil, nil).List.Journeys
		if len(items) != 1 || len(cards) != 1 {
			t.Fatalf("%s: projected %d items and %d cards", name, len(items), len(cards))
		}
		if items[0].Status != cards[0].StageLabel {
			t.Errorf("%s: My Work says %q, Journeys says %q", name, items[0].Status, cards[0].StageLabel)
		}
		if items[0].Terminal != cards[0].Closed || items[0].Terminal != closed {
			t.Errorf("%s: My Work closed=%t, Journeys closed=%t, server closed=%t", name, items[0].Terminal, cards[0].Closed, closed)
		}
	}
}
