package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_UXLIVE_044_Integration(t *testing.T) {
	data := ListData{Journeys: []*journeyv1.Journey{testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL)}}
	page := ListPage(testConfig(), data, nil, nil)
	markup, err := journey.RenderToString(page)
	if err != nil {
		t.Fatalf("render projected journey: %v", err)
	}
	for _, want := range []string{"Senior HR Business Partner", "OPS-HRBP3"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("authorized position identity did not reach the list page (%q): %s", want, markup)
		}
	}
	if strings.Contains(markup, testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL).GetTarget().GetPositionId()) {
		t.Fatal("list page exposed the raw position reference")
	}
}

func TestTodo_UXLIVE_044_Regression(t *testing.T) {
	j := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	j.Target.PositionId = "position-revision-7"
	j.Target.JobCode = ""
	page := ListPage(testConfig(), ListData{Journeys: []*journeyv1.Journey{j}}, nil, nil)
	markup, err := journey.RenderToString(page)
	if err != nil {
		t.Fatalf("render unresolved position: %v", err)
	}
	if strings.Contains(markup, "position-revision-7") || !strings.Contains(markup, "Position unavailable") {
		t.Fatalf("unresolved position was not rendered as an explicit safe state: %s", markup)
	}
}

func TestTodo_UXLIVE_044_Security(t *testing.T) {
	j := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	j.Target.PositionId = "70ae5423-59a9-55ba-abf3-00f495284b09"
	j.Target.JobCode = "OPS-UNKNOWN"
	page := ListPage(testConfig(), ListData{Journeys: []*journeyv1.Journey{j}}, nil, nil)
	markup, err := journey.RenderToString(page)
	if err != nil {
		t.Fatalf("render withheld position: %v", err)
	}
	if strings.Contains(markup, "70ae5423-59a9-55ba-abf3-00f495284b09") {
		t.Fatalf("raw position diagnostics crossed the client boundary: %s", markup)
	}
}

func TestTodo_UXLIVE_045_Regression(t *testing.T) {
	for _, stage := range []journeyv1.JourneyStage{
		journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED,
		journeyv1.JourneyStage_JOURNEY_STAGE_FAILED,
		journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED,
	} {
		page := ListPage(testConfig(), ListData{Journeys: []*journeyv1.Journey{testJourney(t, stage)}}, nil, nil)
		markup, err := journey.RenderToString(page)
		if err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		if strings.Contains(markup, ">Open request<") {
			t.Fatalf("terminal %s still used the generic card action: %s", stage, markup)
		}
	}
}
