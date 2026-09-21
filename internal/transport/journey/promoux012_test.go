package journey

import (
	"slices"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// TestTodo_PROMOUX_012 is the wire half of the PRIMARY: toJourney carries the
// engine's viewer projection token for token, for every value of each closed
// vocabulary, regardless of diagnostics authorization, and an unresolved
// projection stays absent rather than arriving as an all-zero message a client
// could mistake for "observing".
func TestTodo_PROMOUX_012(t *testing.T) {
	steps := []workspace.JourneyNextStep{
		workspace.JourneyNextStepStartApproval, workspace.JourneyNextStepCorrectProposal, workspace.JourneyNextStepApprovalDecision,
		workspace.JourneyNextStepManagerDecision, workspace.JourneyNextStepFinanceDecision, workspace.JourneyNextStepReapprovalDecision,
		workspace.JourneyNextStepRepair, workspace.JourneyNextStepAwaitEffectiveDate, workspace.JourneyNextStepSystemProcessing,
		workspace.JourneyNextStepAwaitAcknowledgement,
	}
	if len(steps) != len(journeyv1.JourneyNextStep_name)-1 {
		t.Fatalf("%d workspace next steps, %d wire values: a new step needs a mapping", len(steps), len(journeyv1.JourneyNextStep_name)-1)
	}
	for _, step := range steps {
		wire := toJourneyViewerProjection(workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityObserving, NextStep: step})
		if wire.GetNextStep() == journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_UNSPECIFIED || wire.GetNextStep().String() != "JOURNEY_NEXT_STEP_"+string(step) {
			t.Errorf("next step %s reached the wire as %s", step, wire.GetNextStep())
		}
	}
	owners := []workspace.JourneyStepOwner{
		workspace.JourneyStepOwnerProposer, workspace.JourneyStepOwnerApprover, workspace.JourneyStepOwnerManager,
		workspace.JourneyStepOwnerFinance, workspace.JourneyStepOwnerSystem,
	}
	if len(owners) != len(journeyv1.JourneyStepOwner_name)-1 {
		t.Fatalf("%d workspace owners, %d wire values", len(owners), len(journeyv1.JourneyStepOwner_name)-1)
	}
	for _, owner := range owners {
		if got := toJourneyViewerProjection(workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityTracking, NextStepOwner: owner}).GetNextStepOwner(); got.String() != "JOURNEY_STEP_OWNER_"+string(owner) {
			t.Errorf("owner %s reached the wire as %s", owner, got)
		}
	}
	for _, responsibility := range []workspace.JourneyViewerResponsibility{
		workspace.JourneyResponsibilityActionRequired, workspace.JourneyResponsibilityTracking,
		workspace.JourneyResponsibilityObserving, workspace.JourneyResponsibilityClosed,
	} {
		if got := toJourneyViewerProjection(workspace.JourneyViewerProjection{Responsibility: responsibility}).GetResponsibility(); got.String() != "JOURNEY_VIEWER_RESPONSIBILITY_"+string(responsibility) {
			t.Errorf("responsibility %s reached the wire as %s", responsibility, got)
		}
	}

	summary := workspace.JourneySummary{IntentID: "intent-12", Viewer: workspace.JourneyViewerProjection{
		Relationships:  []workspace.JourneyViewerRelationship{workspace.JourneyViewerAssignee, workspace.JourneyViewerCandidate, workspace.JourneyViewerInitiator},
		Responsibility: workspace.JourneyResponsibilityActionRequired,
		NextStep:       workspace.JourneyNextStepFinanceDecision, NextStepOwner: workspace.JourneyStepOwnerFinance, AwaitsPerson: true,
	}}
	wantRelationships := []journeyv1.JourneyViewerRelationship{
		journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_ASSIGNEE,
		journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_CANDIDATE,
		journeyv1.JourneyViewerRelationship_JOURNEY_VIEWER_RELATIONSHIP_INITIATOR,
	}
	for _, diag := range []bool{false, true} {
		wire := toJourney(summary, diag).GetViewer()
		if !slices.Equal(wire.GetRelationships(), wantRelationships) || !wire.GetAwaitsPerson() || wire.GetClosed() ||
			wire.GetResponsibility() != journeyv1.JourneyViewerResponsibility_JOURNEY_VIEWER_RESPONSIBILITY_ACTION_REQUIRED {
			t.Fatalf("diag=%t: viewer projection = %+v", diag, wire)
		}
	}
	closed := summary
	closed.Viewer = workspace.JourneyViewerProjection{Responsibility: workspace.JourneyResponsibilityClosed, Closed: true}
	if !toJourney(closed, false).GetViewer().GetClosed() {
		t.Fatal("closure did not reach the wire")
	}
	unresolved := summary
	unresolved.Viewer = workspace.JourneyViewerProjection{}
	if toJourney(unresolved, false).GetViewer() != nil {
		t.Fatal("an unresolved projection reached the wire as a message")
	}
	unknown := toJourneyViewerProjection(workspace.JourneyViewerProjection{
		Responsibility: workspace.JourneyResponsibilityObserving, Relationships: []workspace.JourneyViewerRelationship{"FOLLOWER"},
	})
	if len(unknown.GetRelationships()) != 0 {
		t.Fatalf("a relationship outside the vocabulary reached the wire: %v", unknown.GetRelationships())
	}
}
