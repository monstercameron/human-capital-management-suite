package app

import (
	"strconv"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// TestTodo_PROMOUX_013_ClientServerAvailabilityAgreement closes the one gap
// PROMOUX-013's own report left open: journeyclient.InterventionAvailability
// is a hand-written, browser-side mirror of this package's own
// interventionUnavailableAtStage (see that function's doc comment for why
// the mirror exists at all rather than a network round trip -- availability
// must not go stale between watch pushes, and the client has already passed
// InspectJourney's own authorization boundary, so a per-stage reason
// discloses nothing new). A mirror with no test proving the two agree is a
// mirror that drifts silently: the day a stage is added or a kind's
// eligibility rule changes on one side and not the other, a user is either
// offered an action the server will refuse, or denied one it would have
// allowed, with a confidently-worded reason either way.
//
// This test is total over the wire enums, not a hardcoded stage/kind list:
// it reads every value straight out of journeyv1.JourneyStage_name and
// journeyv1.JourneyInterventionKind_name (the generated enum's own name
// map), the same technique tools/gen/contracts' own enum-conformance tests
// use, so a stage or kind added to the proto later is exercised
// automatically -- this test fails loudly instead of silently skipping it.
//
// journeyclient.InterventionAvailability is exported for exactly this
// import; internal/intent/app's own interventionUnavailableAtStage stays
// unexported, called here only because this file lives in package app.
func TestTodo_PROMOUX_013_ClientServerAvailabilityAgreement(t *testing.T) {
	var stages []workspace.JourneyStage
	for value, name := range journeyv1.JourneyStage_name {
		if value == 0 { // JOURNEY_STAGE_UNSPECIFIED: never a real stage.
			continue
		}
		stages = append(stages, workspace.JourneyStage(strings.TrimPrefix(name, "JOURNEY_STAGE_")))
	}
	if len(stages) == 0 {
		t.Fatal("no journey stages found in the generated wire enum; this test would pass vacuously")
	}

	// clientKindOf is the one place this test names both sides' spelling for
	// the same kind (the server's typed workspace.JourneyInterventionKind
	// values are "WITHDRAW"/"CANCEL"; the client's own action ids are
	// journeyclient.ActionWithdraw/ActionCancel, "withdraw"/"cancel"). A
	// wire kind this map does not cover fails the test below rather than
	// being silently skipped.
	clientKindOf := map[workspace.JourneyInterventionKind]string{
		workspace.JourneyInterventionWithdraw: journeyclient.ActionWithdraw,
		workspace.JourneyInterventionCancel:   journeyclient.ActionCancel,
	}
	// REPAIR (UXLIVE-006) is deliberately outside this cross-check, and that
	// is a property rather than a gap. This test proves the client can
	// recompute the server's answer from the stage alone; REPAIR's answer
	// depends on whether the deployment composed the repair door and whether
	// the viewer holds a current JIT grant, neither of which the browser
	// knows or should be told to guess. The client therefore renders REPAIR
	// from the server's own preview. What the two sides must still share is
	// the reason vocabulary, which TestTodo_UXLIVE_006_Security checks.
	notStageOnly := map[workspace.JourneyInterventionKind]bool{
		workspace.JourneyInterventionRepair: true,
	}
	var kinds []workspace.JourneyInterventionKind
	for value, name := range journeyv1.JourneyInterventionKind_name {
		if value == 0 { // JOURNEY_INTERVENTION_KIND_UNSPECIFIED
			continue
		}
		kind := workspace.JourneyInterventionKind(strings.TrimPrefix(name, "JOURNEY_INTERVENTION_KIND_"))
		if notStageOnly[kind] {
			continue
		}
		if _, ok := clientKindOf[kind]; !ok {
			t.Fatalf("the wire declares intervention kind %q with no client-side mapping in this test; "+
				"add one to clientKindOf before this cross-check can be trusted", kind)
		}
		kinds = append(kinds, kind)
	}
	if len(kinds) == 0 {
		t.Fatal("no intervention kinds found in the generated wire enum; this test would pass vacuously")
	}

	// The two implementations must agree for a journey that started its
	// workflow and one that did not: BLOCKED resolves differently between
	// them (UXLIVE-026), so a cross-check over stages alone would no longer
	// cover the rule.
	for _, kind := range kinds {
		for _, stage := range stages {
			for _, started := range []bool{false, true} {
				t.Run(string(kind)+"/"+string(stage)+"/started="+strconv.FormatBool(started), func(t *testing.T) {
					serverReason, serverUnavailable := interventionUnavailableAtStage(kind, stage, started)
					clientReason, clientAvailable := journeyclient.InterventionAvailability(clientKindOf[kind], string(stage), started)
					serverAvailable := !serverUnavailable
					if serverAvailable != clientAvailable {
						t.Fatalf("%s at %s (started=%v): server available=%v, client available=%v -- the two implementations disagree on whether this action may be offered",
							kind, stage, started, serverAvailable, clientAvailable)
					}
					if serverReason != clientReason {
						t.Fatalf("%s at %s (started=%v): server reason=%q, client reason=%q -- the two implementations disagree on why",
							kind, stage, started, serverReason, clientReason)
					}
				})
			}
		}
	}
}
