package journeyclient

import (
	"testing"

	journey "github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_UXAUDIT_017_ProjectorUsesTypedLifecycleGroups(t *testing.T) {
	tests := []struct {
		stage string
		group journey.JourneyGroup
	}{
		{stageFinanceApproval, journey.JourneyGroupReview},
		{stageWaitingEffective, journey.JourneyGroupWaiting},
		{stageRepairRequired, journey.JourneyGroupIssue},
		{stageRecorded, journey.JourneyGroupClosed},
	}
	for _, tt := range tests {
		if got := journeyGroup(tt.stage); got != tt.group {
			t.Errorf("stage %s group %s, want %s", tt.stage, got, tt.group)
		}
	}
}
