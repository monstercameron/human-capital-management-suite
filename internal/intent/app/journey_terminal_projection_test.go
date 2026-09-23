package app

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestBlockedJourneyTerminalProjection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record journeyRecord
		closed bool
	}{
		{name: "unexecuted proposal"},
		{name: "running instance", record: journeyRecord{instance: &runtime.Instance{}}},
		{name: "skipped terminal", record: journeyRecord{instance: &runtime.Instance{}, nodes: []runtime.NodeExecution{{NodeID: promotionexec.NodeEndBlocked, Status: runtime.NodeSkipped}}}},
		{name: "completed blocked terminal", record: journeyRecord{instance: &runtime.Instance{}, nodes: []runtime.NodeExecution{{NodeID: promotionexec.NodeEndBlocked, Status: runtime.NodeSucceeded}}}, closed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := journeyRecordViewerProjection(workspace.JourneyStageBlocked, true, nil, tc.record)
			if got.Closed != tc.closed {
				t.Fatalf("closure = %v, want %v", got.Closed, tc.closed)
			}
			if len(got.Relationships) != 1 || got.Relationships[0] != workspace.JourneyViewerInitiator {
				t.Fatalf("initiator relationship lost: %+v", got)
			}
			if tc.closed {
				if got.Responsibility != workspace.JourneyResponsibilityClosed || got.AwaitsPerson || got.NextStep != "" || got.NextStepOwner != "" {
					t.Fatalf("terminal journey still asks for action: %+v", got)
				}
			} else if got.Responsibility != workspace.JourneyResponsibilityActionRequired || got.NextStep != workspace.JourneyNextStepCorrectProposal || !got.AwaitsPerson {
				t.Fatalf("correctable proposal lost its action: %+v", got)
			}
		})
	}
}
