package journey_test

import (
	"context"
	"sync"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type decisionRecorder struct {
	mu     sync.Mutex
	events []workspace.JourneyDecisionEvent
	who    []string
}

func (r *decisionRecorder) AnnounceJourneyDecision(ctx context.Context, e workspace.JourneyDecisionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	if p, ok := trust.FromContext(ctx); ok && p != nil {
		r.who = append(r.who, p.Subject())
	}
}

// A decision that the engine accepted is announced once, as the person who
// decided, with the stage the journey reached and no reason (#3728).
func TestIssue3728_AcceptedDecisionIsAnnouncedWithTheResultingStage(t *testing.T) {
	for _, approve := range []bool{true, false} {
		engine := newFakeEngine()
		engine.detail.Summary.Stage = workspace.JourneyStageRejected
		engine.detail.Summary.ProposalRevisionID = "rev-3"
		rec := &decisionRecorder{}
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, Announcer: rec}))
		if _, err := client.DecideJourney(authorizedContext(t), &journeyv1.DecideJourneyRequest{IntentId: fixtureIntentID, Approve: approve, Reason: "private reason"}); err != nil {
			t.Fatalf("DecideJourney: %v", err)
		}
		if len(rec.events) != 1 {
			t.Fatalf("approve=%v announced %d times", approve, len(rec.events))
		}
		got := rec.events[0]
		want := workspace.JourneyDecisionEvent{IntentID: fixtureIntentID, Approve: approve, Stage: workspace.JourneyStageRejected, ProposalRevisionID: "rev-3"}
		if got != want {
			t.Fatalf("event %+v, want %+v", got, want)
		}
		if len(rec.who) != 1 || rec.who[0] == "" {
			t.Fatalf("the announcement did not carry the decider's identity: %v", rec.who)
		}
	}
}

// A decision the engine refused changed nothing, so nothing is announced.
func TestIssue3728_RefusedDecisionIsNotAnnounced_Security(t *testing.T) {
	engine := newFakeEngine()
	engine.decideErr = workspace.ErrJourneyStage
	rec := &decisionRecorder{}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, Announcer: rec}))
	if _, err := client.DecideJourney(authorizedContext(t), &journeyv1.DecideJourneyRequest{IntentId: fixtureIntentID, Approve: true}); err == nil {
		t.Fatal("decision should have been refused")
	}
	if len(rec.events) != 0 {
		t.Fatalf("announced a refused decision: %+v", rec.events)
	}
}

// Announcing is a courtesy after the decision is durable: no announcer at all
// is a valid composition and the decision still succeeds.
func TestIssue3728_NoAnnouncerStillDecides(t *testing.T) {
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: newFakeEngine()}))
	if _, err := client.DecideJourney(authorizedContext(t), &journeyv1.DecideJourneyRequest{IntentId: fixtureIntentID, Approve: true}); err != nil {
		t.Fatalf("DecideJourney: %v", err)
	}
}
