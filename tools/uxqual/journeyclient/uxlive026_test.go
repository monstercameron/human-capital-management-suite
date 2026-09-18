package journeyclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// UXLIVE-026's RED was measured on the running server: journey
// 01a0b189-e04c-7265-8926-909531dd6530 completed both approvals before
// revalidation refused, and its Actions section still offered Withdraw
// under "Stops this proposal before any approval has been recorded. No
// business effect has occurred." while disabling Cancel with "This proposal
// has not started its approval workflow yet."
//
// The cause was that BLOCKED was classified as unstarted. It is reachable
// both from a proposal that never started and from a run whose approvals
// are recorded, so the stage alone cannot decide; the run's own instance
// can, and the card already carries it.

func uxlive026Card(stage string, instanceID string) journey.JourneyCard {
	return journey.JourneyCard{IntentID: "int-1", Stage: stage, InstanceID: instanceID}
}

func uxlive026Actions(t *testing.T, head journey.JourneyCard) map[string]journey.Action {
	t.Helper()
	out := map[string]journey.Action{}
	for _, action := range interventionActions(nil, head, nil, nil, nil) {
		out[action.ID] = action
	}
	return out
}

// TestTodo_UXLIVE_026 is the primary red/green test: a blocked run that
// started its workflow is not treated as one that never started.
func TestTodo_UXLIVE_026(t *testing.T) {
	started := uxlive026Card(stageBlocked, "ee2abfa8-f84d-5040-a571-65269faed1f3")
	unstarted := uxlive026Card(stageBlocked, "")

	if _, ok := InterventionAvailability(ActionWithdraw, stageBlocked, true); ok {
		t.Fatalf("withdraw is offered on a blocked run whose approvals are recorded")
	}
	if _, ok := InterventionAvailability(ActionWithdraw, stageBlocked, false); !ok {
		t.Fatalf("withdraw is refused on a blocked proposal that never started")
	}
	if _, ok := InterventionAvailability(ActionCancel, stageBlocked, true); !ok {
		t.Fatalf("cancel is refused on a blocked run that did start")
	}
	if ref, ok := InterventionAvailability(ActionCancel, stageBlocked, false); ok || ref != reasonNotYetStarted {
		t.Fatalf("cancel on a blocked proposal that never started = (%q, %v), want the not-yet-started refusal", ref, ok)
	}

	// The page follows the rule.
	startedActions := uxlive026Actions(t, started)
	if !startedActions[ActionWithdraw].Disabled {
		t.Fatalf("the page still offers withdraw on a started blocked run")
	}
	if startedActions[ActionCancel].Disabled {
		t.Fatalf("the page refuses cancel on a started blocked run")
	}

	unstartedActions := uxlive026Actions(t, unstarted)
	if unstartedActions[ActionWithdraw].Disabled {
		t.Fatalf("the page refuses withdraw on a blocked proposal that never started")
	}
	if !unstartedActions[ActionCancel].Disabled {
		t.Fatalf("the page offers cancel on a blocked proposal that never started")
	}
}

// TestTodo_UXLIVE_026_Browser keeps the description truthful: a run whose
// approvals are recorded is never described as one where none were.
func TestTodo_UXLIVE_026_Browser(t *testing.T) {
	started := uxlive026Actions(t, uxlive026Card(stageBlocked, "ee2abfa8"))
	withdraw := started[ActionWithdraw]
	if strings.Contains(withdraw.Description, "before any approval has been recorded") && !withdraw.Disabled {
		t.Fatalf("an offered withdraw claims no approval was recorded on a started run: %q", withdraw.Description)
	}
	if withdraw.DisabledReason == "" {
		t.Fatalf("the refused withdraw names no reason")
	}
	if withdraw.DisabledReason != interventionReasonText(reasonAlreadyStarted) {
		t.Fatalf("the refused withdraw's reason is %q, want the already-started one", withdraw.DisabledReason)
	}
}

// TestTodo_UXLIVE_026_Security keeps every other stage's decision exactly as
// it was, so this change narrows one wrong offer rather than widening the
// rule generally.
func TestTodo_UXLIVE_026_Security(t *testing.T) {
	stages := []string{
		stageProposed, stageAwaitingApproval, stageFinanceApproval, stageManagerApproval,
		stageReapproval, stageWaitingEffective, stageRevalidation, stageExecuted,
		stageObservingEffects, stageCompleted, stageRecorded, stageRejected, stageFailed,
		stageRepairRequired,
	}
	for _, stage := range stages {
		for _, kind := range []string{ActionWithdraw, ActionCancel} {
			refFalse, okFalse := InterventionAvailability(kind, stage, false)
			refTrue, okTrue := InterventionAvailability(kind, stage, true)
			if refFalse != refTrue || okFalse != okTrue {
				t.Fatalf("%s at %s changed with the started fact: (%q,%v) vs (%q,%v); only BLOCKED may depend on it",
					kind, stage, refFalse, okFalse, refTrue, okTrue)
			}
		}
	}
}
