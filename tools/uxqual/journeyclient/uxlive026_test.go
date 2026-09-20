package journeyclient

import (
	"strings"
	"testing"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
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
	if _, shown := startedActions[ActionWithdraw]; shown {
		t.Fatalf("the page still shows withdraw on a started blocked run")
	}
	if startedActions[ActionCancel].Disabled {
		t.Fatalf("the page refuses cancel on a started blocked run")
	}

	unstartedActions := uxlive026Actions(t, unstarted)
	if unstartedActions[ActionWithdraw].Disabled {
		t.Fatalf("the page refuses withdraw on a blocked proposal that never started")
	}
	if _, shown := unstartedActions[ActionCancel]; shown {
		t.Fatalf("the page shows cancel on a blocked proposal that never started")
	}
}

// TestTodo_UXLIVE_026_Browser keeps the description truthful: a run whose
// approvals are recorded is never described as one where none were.
func TestTodo_UXLIVE_026_Browser(t *testing.T) {
	started := uxlive026Actions(t, uxlive026Card(stageBlocked, "ee2abfa8"))
	// A started run never shows a withdraw that claims no approval was
	// recorded: the withdraw is omitted and Cancel, which applies, is offered.
	if withdraw, shown := started[ActionWithdraw]; shown {
		t.Fatalf("a started run still shows withdraw: %+v", withdraw)
	}
	if cancel, shown := started[ActionCancel]; !shown || cancel.Disabled {
		t.Fatalf("a started run does not offer the cancel that applies: %+v", cancel)
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

// TestInterventionActionsFollowTheLocale: Withdraw, Request cancellation and
// Edit proposal were English on German and Arabic pages, and their review
// triggers read "withdraw prüfen" -- an English verb inside German copy.
func TestInterventionActionsFollowTheLocale(t *testing.T) {
	for _, stage := range []string{stageProposed, stageFinanceApproval, stageCompleted} {
		head := uxlive026Card(stage, "")
		if stage != stageProposed {
			head.InstanceID = "ee2abfa8-f84d-5040-a571-65269faed1f3"
		}
		english := map[string]journey.Action{}
		for _, action := range interventionActions(nil, head, nil, nil, nil) {
			english[action.ID] = action
		}
		for _, locale := range []string{"de-DE", "ar"} {
			for _, action := range interventionActionsLocale(locale, nil, head, nil, nil, nil) {
				en := english[action.ID]
				if action.Label == en.Label || action.Description == en.Description ||
					(action.Disabled && action.DisabledReason == en.DisabledReason) ||
					(!action.Disabled && (action.ReviewLabel == "" || action.ReviewLabel == en.ReviewLabel)) {
					t.Errorf("%s %s action %s is still English: %+v", locale, stage, action.ID, action)
				}
			}
		}
	}
}

// TestWithdrawalNoticeSaysWithdrawn: the success notice answered a
// withdrawal with "The proposal was cancelled. Evidence: <uuid>." -- the other
// action's word, in English on every locale, with a raw id in the sentence.
func TestWithdrawalNoticeSaysWithdrawn(t *testing.T) {
	notice := interventionNotice(journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_WITHDRAW,
		commonv1.InterventionOutcome_INTERVENTION_OUTCOME_APPLIED, "1f10de0c-2a83-5c32-a401-6cf797662edf")
	if notice.TitleKey != "journey.iv_withdrawn_title" || strings.Contains(notice.Detail, "1f10de0c") ||
		notice.SupportReference != "1f10de0c-2a83-5c32-a401-6cf797662edf" {
		t.Fatalf("withdrawal notice = %+v", notice)
	}
	cancel := interventionNotice(journeyv1.JourneyInterventionKind_JOURNEY_INTERVENTION_KIND_CANCEL,
		commonv1.InterventionOutcome_INTERVENTION_OUTCOME_APPLIED, "")
	if cancel.TitleKey != "journey.iv_cancelled_title" {
		t.Fatalf("cancellation notice = %+v", cancel)
	}
}
