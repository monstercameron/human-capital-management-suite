package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// TestJourneyStatusDimensionTable pins JourneyStatusDimension to the server's
// wire vocabulary, value by value, and fails when either enum grows a value
// nobody has mapped. The stage-to-step mapping itself is the server's
// (internal/intent/app TestTodo_PROMOUX_012); this is only its projection.
func TestJourneyStatusDimensionTable(t *testing.T) {
	steps := map[journeyv1.JourneyNextStep]NextStep{
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_UNSPECIFIED:          NextStepNone,
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_START_APPROVAL:       NextStepStartApproval,
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_CORRECT_PROPOSAL:     NextStepCorrectProposal,
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_APPROVAL_DECISION:    NextStepApprovalDecision,
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_MANAGER_DECISION:     NextStepManagerDecision,
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_FINANCE_DECISION:     NextStepFinanceDecision,
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_REAPPROVAL_DECISION:  NextStepReapprovalDecision,
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_REPAIR:               NextStepRepair,
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_AWAIT_EFFECTIVE_DATE: NextStepAwaitEffectiveDate,
		journeyv1.JourneyNextStep_JOURNEY_NEXT_STEP_SYSTEM_PROCESSING:    NextStepSystemProcessing,
	}
	if len(steps) != len(journeyv1.JourneyNextStep_name) {
		t.Fatalf("table covers %d next steps, the enum has %d", len(steps), len(journeyv1.JourneyNextStep_name))
	}
	for wire, want := range steps {
		got := JourneyStatusDimension(&journeyv1.Journey{Viewer: &journeyv1.JourneyViewerProjection{NextStep: wire, AwaitsPerson: true}})
		if got.NextStep != want || !got.AwaitsPerson {
			t.Errorf("%s: got %+v, want step %q", wire, got, want)
		}
	}
	actors := map[journeyv1.JourneyStepOwner]StageActor{
		journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_UNSPECIFIED: StageActorUnstated,
		journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_PROPOSER:    StageActorProposer,
		journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_APPROVER:    StageActorApprover,
		journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_MANAGER:     StageActorManager,
		journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_FINANCE:     StageActorFinance,
		journeyv1.JourneyStepOwner_JOURNEY_STEP_OWNER_SYSTEM:      StageActorSystem,
	}
	if len(actors) != len(journeyv1.JourneyStepOwner_name) {
		t.Fatalf("table covers %d owners, the enum has %d", len(actors), len(journeyv1.JourneyStepOwner_name))
	}
	for wire, want := range actors {
		if got := JourneyStatusDimension(&journeyv1.Journey{Viewer: &journeyv1.JourneyViewerProjection{NextStepOwner: wire}}).WaitingOn; got != want {
			t.Errorf("%s: WaitingOn = %q, want %q", wire, got, want)
		}
	}
	// No projection: nothing is inferred from the stage.
	if got := JourneyStatusDimension(&journeyv1.Journey{Stage: journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL}); got != (StatusDimension{}) {
		t.Fatalf("a journey with no server projection read as %+v", got)
	}
	if JourneyClosed(&journeyv1.Journey{Stage: journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED}) {
		t.Fatal("closure was inferred from the stage instead of read from the projection")
	}
}

// TestNextStepLabelVocabulary: every code the wire can produce has wording,
// and a code outside the vocabulary has none (never a guessed label).
func TestNextStepLabelVocabulary(t *testing.T) {
	for value := range journeyv1.JourneyNextStep_name {
		code := JourneyStatusDimension(&journeyv1.Journey{Viewer: &journeyv1.JourneyViewerProjection{NextStep: journeyv1.JourneyNextStep(value)}}).NextStep
		if code != NextStepNone && NextStepLabel(code) == "" {
			t.Errorf("next step %d yields code %q with no label", value, code)
		}
	}
	if got := NextStepLabel(NextStep("approve_everything")); got != "" {
		t.Fatalf("unknown code labeled %q", got)
	}
}
