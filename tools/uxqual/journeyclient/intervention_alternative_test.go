package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// TestInterventionThatCannotApplyIsOmittedBesideItsAlternative is the live
// finding on a journey awaiting manager approval: the Actions section showed
// a Withdraw card with a disabled button and "Use Cancel instead of
// Withdraw" directly above the Cancel card. A stop that cannot apply is now
// omitted whenever the other stop is offered; a refusal with no alternative
// (committed or terminal) still renders with its reason.
func TestInterventionThatCannotApplyIsOmittedBesideItsAlternative(t *testing.T) {
	cfg := testConfig()
	for stage, want := range map[journeyv1.JourneyStage][2]string{
		journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL: {ActionCancel, ActionWithdraw},
		journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED:         {ActionWithdraw, ActionCancel},
	} {
		page := DetailPage(cfg, testDetail(t, stage), nil, nil)
		shown, hidden := want[0], want[1]
		if a := findAction(t, page.Detail.Actions, shown); a.Disabled {
			t.Fatalf("%s: %s is not offered: %+v", stageOf(stage), shown, a)
		}
		if hasAction(page.Detail.Actions, hidden) {
			t.Fatalf("%s: %s still renders beside %s", stageOf(stage), hidden, shown)
		}
		markup := mustRender(t, page)
		if strings.Contains(markup, "instead of") {
			t.Fatalf("%s: the page still tells the reader to use the other stop:\n%s", stageOf(stage), markup)
		}
	}
	committed := DetailPage(cfg, testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED), nil, nil)
	for _, id := range []string{ActionWithdraw, ActionCancel} {
		if a := findAction(t, committed.Detail.Actions, id); !a.Disabled || a.DisabledReason == "" {
			t.Fatalf("with no alternative, %s must still state why it is refused: %+v", id, a)
		}
	}
}
