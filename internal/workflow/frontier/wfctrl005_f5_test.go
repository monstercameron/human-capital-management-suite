package frontier_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

// TestTodo_WF_CTRL_005_F5_UnknownRoutesToFallback proves the route-taking
// half of #3620: a CAPABILITY node answering UNKNOWN (the fallback answer a
// kill-switched in-flight node returns) advances along its declared UNKNOWN
// edge, here to the reference plan unknown end.
func TestTodo_WF_CTRL_005_F5_UnknownRoutesToFallback(t *testing.T) {
	plan := promotionPlan(t)
	state := seed(t, plan)
	tr := step(t, plan, state, frontier.NodeOutcome{
		NodeID:       workflow.PromotionNodeSnapshotWorker,
		Outcome:      workflow.OutcomeUnknown,
		OutputDigest: "sha256:killed",
	})
	if ids := tr.SuccessorIDs(); !equal(ids, []string{workflow.PromotionNodeEndUnknown}) {
		t.Fatalf("UNKNOWN routed to %v, want [%s]", ids, workflow.PromotionNodeEndUnknown)
	}
}
