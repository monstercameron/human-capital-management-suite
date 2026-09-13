package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// TestTodo_UXAUDIT_017 is this package's contribution to the PRIMARY
// contract: UXAUDIT-017 GREEN requires "Journeys is a lifecycle tracker
// grouped by subject and status", not the flat, server-recency list the
// live audit found. groupJourneyCards is the computed, testable ordering
// that produces that grouping; this test drives it through the real
// ListPage entry point so the projection under test is exactly what the
// page renders.
func TestTodo_UXAUDIT_017(t *testing.T) {
	journeys := []*journeyv1.Journey{
		// Engine order (newest-updated first, as ListJourneys returns it):
		// Naomi, then Adrian's completed history, then Samuel, then
		// Adrian's currently open journey. A flat render of this order
		// would interleave Adrian's two journeys around Samuel's.
		{IntentId: "int-naomi", WorkerRef: "worker-naomi", WorkerName: "Naomi Chen", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL},
		{IntentId: "int-adrian-done", WorkerRef: "worker-adrian", WorkerName: "Adrian Fox", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED},
		{IntentId: "int-samuel", WorkerRef: "worker-samuel", WorkerName: "Samuel Ortiz", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL},
		{IntentId: "int-adrian-open", WorkerRef: "worker-adrian", WorkerName: "Adrian Fox", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED},
	}

	page := ListPage(testConfig(), ListData{Journeys: journeys}, nil, nil)
	cards := page.List.Journeys
	if len(cards) != 4 {
		t.Fatalf("Journeys = %d cards, want all 4", len(cards))
	}

	indexOf := func(intentID string) int {
		for i, c := range cards {
			if c.IntentID == intentID {
				return i
			}
		}
		t.Fatalf("card %s missing from %+v", intentID, cards)
		return -1
	}

	naomi := indexOf("int-naomi")
	samuel := indexOf("int-samuel")
	adrianOpen := indexOf("int-adrian-open")
	adrianDone := indexOf("int-adrian-done")

	// Subject grouping: Adrian's two journeys must be adjacent, wherever
	// the group as a whole lands.
	if diff := adrianOpen - adrianDone; diff != 1 && diff != -1 {
		t.Fatalf("Adrian's two journeys are not adjacent: open at %d, done at %d (cards=%+v)", adrianOpen, adrianDone, cards)
	}
	// Status within the subject group: the open journey precedes the
	// completed one, so the active thread reads before its own history.
	if adrianOpen > adrianDone {
		t.Fatalf("Adrian's completed journey sorted ahead of his open one: open at %d, done at %d", adrianOpen, adrianDone)
	}
	// Naomi and Samuel are unrelated subjects with exactly one journey
	// each; they must not be spliced into Adrian's group, and the group
	// order otherwise follows engine recency (Naomi's group first, then
	// Adrian's, then Samuel's, matching first-appearance in the input).
	if naomi > adrianOpen && naomi > adrianDone {
		t.Fatalf("Naomi's single journey was pushed after Adrian's group: naomi at %d, adrian at [%d,%d]", naomi, adrianOpen, adrianDone)
	}
	if samuel < adrianOpen && samuel < adrianDone {
		t.Fatalf("Samuel's single journey was pulled before Adrian's group, which appeared earlier in engine order: samuel at %d, adrian at [%d,%d]", samuel, adrianOpen, adrianDone)
	}

	// The GREEN clause names a real rendered grouping structure, not merely
	// an order: page.List.Groups must name three subjects (not four, one
	// per journey), with Adrian's carrying both his journeys and both his
	// distinct statuses. A fixture where every subject had exactly one
	// journey could not tell a grouped page from an ungrouped one -- this
	// fixture deliberately gives Adrian two, so the group/no-group
	// distinction is actually exercised.
	groups := page.List.Groups
	if len(groups) != 3 {
		t.Fatalf("List.Groups = %d groups, want 3 (Naomi, Adrian, Samuel), got %+v", len(groups), groups)
	}
	var adrianGroup *journey.JourneySubjectGroup
	for i := range groups {
		if groups[i].Subject == "Adrian Fox" {
			adrianGroup = &groups[i]
		}
	}
	if adrianGroup == nil {
		t.Fatalf("no group named Adrian Fox in %+v", groups)
	}
	if len(adrianGroup.Journeys) != 2 {
		t.Fatalf("Adrian's group carries %d journeys, want both of his", len(adrianGroup.Journeys))
	}
	if len(adrianGroup.Statuses) != 2 {
		t.Fatalf("Adrian's group must expose both his distinct statuses (Blocked and Completed), got %+v", adrianGroup.Statuses)
	}

	// The shared status dimension reaches the tracker's cards: an open
	// journey names its next step, a terminal one names none.
	if got := cards[adrianOpen].NextStep; got != "Correct the proposal" {
		t.Fatalf("Adrian's blocked journey NextStep = %q, want %q", got, "Correct the proposal")
	}
	if got := cards[samuel].NextStep; got != "Manager decision" {
		t.Fatalf("Samuel's manager-approval journey NextStep = %q, want %q", got, "Manager decision")
	}
	if got := cards[adrianDone].NextStep; got != "" {
		t.Fatalf("a completed journey must name no next step, got %q", got)
	}
}

// TestTodo_UXAUDIT_017_Regression pins groupJourneyCards directly and
// generically: built from synthetic subjects rather than named fixtures, so
// a projector that merely reproduces one observed order cannot pass by
// coincidence.
func TestTodo_UXAUDIT_017_Regression(t *testing.T) {
	populations := [][]journey.JourneyCard{
		syntheticJourneyCards(3, 2),
		syntheticJourneyCards(5, 1),
		syntheticJourneyCards(1, 4),
	}
	for populationIndex, cards := range populations {
		cards := cards
		t.Run(subjectPopulationName(populationIndex, len(cards)), func(t *testing.T) {
			grouped := groupJourneyCards(cards)
			if len(grouped) != len(cards) {
				t.Fatalf("grouping changed population size: got %d, want %d", len(grouped), len(cards))
			}
			seenSubjects := map[string]bool{}
			lastSubject := ""
			for index, card := range grouped {
				key := journeySubjectKey(card)
				if key != lastSubject {
					if seenSubjects[key] {
						t.Fatalf("subject %q reappeared at position %d after the group had already closed: %+v", key, index, grouped)
					}
					seenSubjects[key] = true
					lastSubject = key
				}
			}
			// Within every subject's now-adjacent run, open journeys must
			// precede terminal ones.
			for i := 1; i < len(grouped); i++ {
				if journeySubjectKey(grouped[i-1]) != journeySubjectKey(grouped[i]) {
					continue
				}
				if journeyOpenRank(grouped[i-1].Stage) > journeyOpenRank(grouped[i].Stage) {
					t.Fatalf("terminal journey sorted ahead of an open one within the same subject at position %d: %+v", i, grouped[i-1:i+1])
				}
			}
		})
	}
}

func subjectPopulationName(index, size int) string {
	return "synthetic population " + string(rune('A'+index)) + " " + string(rune('0'+size))
}

// syntheticJourneyCards builds subjectCount subjects with journeysPerSubject
// cards each, interleaved (not pre-grouped) and alternating stage so every
// subject has a mix of open and terminal journeys to order.
func syntheticJourneyCards(subjectCount, journeysPerSubject int) []journey.JourneyCard {
	cards := make([]journey.JourneyCard, 0, subjectCount*journeysPerSubject)
	for j := 0; j < journeysPerSubject; j++ {
		for s := 0; s < subjectCount; s++ {
			stage := stageManagerApproval
			if j%2 == 1 {
				stage = stageCompleted
			}
			cards = append(cards, journey.JourneyCard{
				IntentID:  "synthetic-" + string(rune('A'+s)) + "-" + string(rune('0'+j)),
				WorkerRef: "worker-" + string(rune('A'+s)),
				Stage:     stage,
			})
		}
	}
	return cards
}
