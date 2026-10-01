package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

func TestTodo_UXBLIND_108(t *testing.T) {
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	detail.Journey.WorkerName = "Ana Flores"

	page := DetailPage(testConfig(), detail, nil, nil)
	if got, want := page.Detail.BackLink.Label, "Back to Ana Flores' profile"; got != want {
		t.Fatalf("back link for a name ending in s = %q, want %q", got, want)
	}
}
