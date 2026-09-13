package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// TestStageStatusDimensionTable pins StageStatusDimension to the stage
// contract comments in journey_service.proto, stage by stage, and fails when
// the enum grows a stage nobody has mapped.
func TestStageStatusDimensionTable(t *testing.T) {
	cases := []struct {
		stage  journeyv1.JourneyStage
		step   NextStep
		actor  StageActor
		person bool
	}{
		{journeyv1.JourneyStage_JOURNEY_STAGE_UNSPECIFIED, NextStepNone, StageActorUnstated, false},
		{journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED, NextStepStartApproval, StageActorProposer, true},
		{journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED, NextStepCorrectProposal, StageActorProposer, true},
		{journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL, NextStepApprovalDecision, StageActorApprover, true},
		{journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED, NextStepNone, StageActorUnstated, false},
		{journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED, NextStepNone, StageActorUnstated, false},
		{journeyv1.JourneyStage_JOURNEY_STAGE_FAILED, NextStepNone, StageActorUnstated, false},
		{journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL, NextStepFinanceDecision, StageActorFinance, true},
		{journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL, NextStepManagerDecision, StageActorManager, true},
		{journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE, NextStepAwaitEffectiveDate, StageActorSystem, false},
		{journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION, NextStepSystemProcessing, StageActorSystem, false},
		{journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL, NextStepReapprovalDecision, StageActorApprover, true},
		{journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED, NextStepSystemProcessing, StageActorSystem, false},
		{journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS, NextStepSystemProcessing, StageActorSystem, false},
		{journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED, NextStepNone, StageActorUnstated, false},
		{journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED, NextStepRepair, StageActorUnstated, true},
	}
	if len(cases) != len(journeyv1.JourneyStage_name) {
		t.Fatalf("table covers %d stages, the enum has %d: a new stage needs a reviewed dimension", len(cases), len(journeyv1.JourneyStage_name))
	}
	for _, c := range cases {
		got := StageStatusDimension(c.stage)
		if got.NextStep != c.step || got.WaitingOn != c.actor || got.AwaitsPerson != c.person {
			t.Errorf("%s: got %+v, want step=%q actor=%q person=%t", c.stage, got, c.step, c.actor, c.person)
		}
	}
}

// TestNextStepLabelVocabulary: every code a stage can produce has wording, and
// a code outside the vocabulary has none (never a guessed label).
func TestNextStepLabelVocabulary(t *testing.T) {
	for value := range journeyv1.JourneyStage_name {
		code := StageStatusDimension(journeyv1.JourneyStage(value)).NextStep
		if code != NextStepNone && NextStepLabel(code) == "" {
			t.Errorf("stage %d yields next step %q with no label", value, code)
		}
	}
	if got := NextStepLabel(NextStep("approve_everything")); got != "" {
		t.Fatalf("unknown code labeled %q", got)
	}
}
