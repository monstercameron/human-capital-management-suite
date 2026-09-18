package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// UXLIVE-006's RED was read off the running server: journey
// 01a0b18f-94ce-7560-b01f-71a6af3b581b showed the chip "Needs repair" and the
// next step "Governed repair", while its Actions section offered only
// Withdraw (disabled), Request cancellation and Edit proposal. Nothing on the
// page requested, tracked or explained the repair the journey itself named,
// which strands the run and its subject.
//
// The repair is presented and never submitted from here. That is the
// finding's real shape, not a shortcut: the corrective effect runs against an
// authored, simulated repair plan through the operator door under a JIT grant
// with dual control, and a promotion page holds none of that. What the page
// owed the reader is what it was withholding -- that the repair exists, what
// it demands, who may authorize it, and what its fence can and cannot
// establish.

func uxlive006Card(stage string) journey.JourneyCard {
	return journey.JourneyCard{IntentID: "int-1", Stage: stage, InstanceID: "ee2abfa8"}
}

func uxlive006Actions(t *testing.T, stage string, repair *journeyv1.PreviewJourneyInterventionResponse) map[string]journey.Action {
	t.Helper()
	out := map[string]journey.Action{}
	for _, action := range interventionActions(nil, uxlive006Card(stage), nil, nil, repair) {
		out[action.ID] = action
	}
	return out
}

func uxlive006Preview(reasonRef string) *journeyv1.PreviewJourneyInterventionResponse {
	return &journeyv1.PreviewJourneyInterventionResponse{
		Available:            false,
		UnavailableReasonRef: reasonRef,
		ConsequenceSummary: "A governed repair revalidates this journey's failed step against independently loaded current truth " +
			"and, only if that still holds, drives one corrective effect behind an admission fence.",
		RequiresDualControl: true,
		RequiresSimulation:  true,
		AuthorityRoleRefs:   []string{"INTEGRITY_REPAIR"},
	}
}

// TestTodo_UXLIVE_006 is the primary red/green test: a repair-required
// journey names the repair it is waiting on, and no other journey does.
func TestTodo_UXLIVE_006(t *testing.T) {
	actions := uxlive006Actions(t, stageRepairRequired, uxlive006Preview(reasonRepairAuthorityRequired))
	repair, offered := actions[ActionRepair]
	if !offered {
		t.Fatalf("a repair-required journey still says nothing about its repair; it offers %v", keysOf(actions))
	}
	if repair.Label == "" {
		t.Fatalf("the repair action has no label")
	}
	if !repair.Disabled || repair.DisabledReason == "" {
		t.Fatalf("the repair is offered as runnable from this page, or refuses without saying why: %+v", repair)
	}

	// The requirements are stated, and they come from the server rather than
	// from this page's memory of them.
	for _, required := range []string{"second approver", "simulation", "INTEGRITY_REPAIR"} {
		if !strings.Contains(repair.Description, required) {
			t.Errorf("the repair action does not state %q: %q", required, repair.Description)
		}
	}
	if !strings.Contains(repair.Description, "admission fence") {
		t.Fatalf("the repair action does not say what it actually does: %q", repair.Description)
	}

	// Every other stage is silent about repair: an action naming a repair a
	// journey is not waiting on is noise, and the stage chip already says so.
	for _, stage := range []string{stageProposed, stageAwaitingApproval, stageExecuted, stageCompleted, stageFailed} {
		if _, present := uxlive006Actions(t, stage, nil)[ActionRepair]; present {
			t.Errorf("%s names a governed repair it is not waiting on", stage)
		}
	}
}

// TestTodo_UXLIVE_006_Browser is GREEN's "names who can act and why the
// viewer cannot": each refusal is a different fact, and a reader trying to
// unstick a promotion has to be able to tell them apart.
func TestTodo_UXLIVE_006_Browser(t *testing.T) {
	seen := map[string]string{}
	for _, ref := range []string{reasonRepairDoorUnavailable, reasonRepairAuthorityRequired, reasonRepairPlanRequired} {
		repair := uxlive006Actions(t, stageRepairRequired, uxlive006Preview(ref))[ActionRepair]
		if strings.TrimSpace(repair.DisabledReason) == "" {
			t.Fatalf("%s renders no reason", ref)
		}
		if other, clash := seen[repair.DisabledReason]; clash {
			t.Errorf("%s and %s read identically (%q); they are different facts", ref, other, repair.DisabledReason)
		}
		seen[repair.DisabledReason] = ref
	}

	// The one a viewer who may act sees points them at the door rather than
	// leaving them stuck on this page.
	mayAct := uxlive006Actions(t, stageRepairRequired, uxlive006Preview(reasonRepairPlanRequired))[ActionRepair]
	if !strings.Contains(mayAct.DisabledReason, "operator repair door") {
		t.Fatalf("an authorized viewer is not told where the repair runs: %q", mayAct.DisabledReason)
	}

	// With no preview answer the page still says the true, viewer-independent
	// part, and does not guess at authority.
	unanswered := uxlive006Actions(t, stageRepairRequired, nil)[ActionRepair]
	if !strings.Contains(unanswered.DisabledReason, "has not been answered") {
		t.Fatalf("an unanswered preview invented an authority answer: %q", unanswered.DisabledReason)
	}
	if strings.Contains(unanswered.Description, "requires") {
		t.Fatalf("an unanswered preview stated requirements it was never told: %q", unanswered.Description)
	}
}

// TestTodo_UXLIVE_006_Security keeps the repair a governed door rather than a
// button. Nothing this page renders may submit a repair, and the reason
// vocabulary stays in agreement with the server's, which is what
// TestTodo_PROMOUX_013_ClientServerAvailabilityAgreement deliberately stops
// covering for this kind.
func TestTodo_UXLIVE_006_Security(t *testing.T) {
	repair := uxlive006Actions(t, stageRepairRequired, uxlive006Preview(reasonRepairPlanRequired))[ActionRepair]
	if len(repair.Fields) != 0 {
		t.Fatalf("the repair action carries submittable fields: %+v", repair.Fields)
	}
	if len(repair.Confirmation) != 0 || repair.ConfirmationNote != "" {
		t.Fatalf("the repair action opens a confirmation it cannot honour: %+v", repair)
	}
	if !repair.Disabled {
		t.Fatalf("the repair action is submittable from this page")
	}

	// Every reason reference this page can render for a repair has its own
	// sentence: an unmapped reference would fall through to the generic
	// "not available right now", which is exactly the silence this todo is
	// about.
	generic := interventionReasonText("journey.repair.unavailable.something_new")
	for _, ref := range []string{reasonRepairNotRequired, reasonRepairDoorUnavailable, reasonRepairAuthorityRequired, reasonRepairPlanRequired} {
		if interventionReasonText(ref) == generic {
			t.Errorf("%s has no sentence of its own and falls through to the generic refusal", ref)
		}
	}

	// A preview that claims the repair is available does not make it
	// submittable here. The client never offers a repair it cannot carry
	// out, whatever the server said.
	claimed := uxlive006Preview("")
	claimed.Available = true
	claimed.UnavailableReasonRef = ""
	if offered := uxlive006Actions(t, stageRepairRequired, claimed)[ActionRepair]; !offered.Disabled {
		t.Fatalf("a preview claiming availability made the repair submittable from this page: %+v", offered)
	}
}

func keysOf(m map[string]journey.Action) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
