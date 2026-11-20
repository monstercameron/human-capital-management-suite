package application

import (
	"testing"

	"google.golang.org/grpc/codes"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// An approver decides against the revision they read (#3724). Against the real
// composition: the routed approver's own read reports the revision, a decision
// naming any other revision is refused as a failed precondition before the
// approval is claimed, and a decision naming the read revision lands.
func TestIssue3724_DecisionIsBoundToTheReviewedRevision_Integration(t *testing.T) {
	h := promoux015ComposeWithWorkforce(t)
	id := h.proposeAndExecute()

	read, err := h.client.InspectJourney(h.rpc("finance-partner"), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("InspectJourney as the routed approver: %v", err)
	}
	revision := read.GetDetail().GetJourney().GetProposalRevisionId()
	if revision == "" {
		t.Fatal("the routed approver's read carries no proposal revision to bind a decision to")
	}

	before := h.items(id)[promotionexec.NodeApproveFinance]
	_, err = h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{
		IntentId: id, Approve: true, Reason: "reviewed an older proposal", ExpectedProposalRevisionId: revision + "-edited",
	})
	promoux015Code(t, err, codes.FailedPrecondition, "DecideJourney naming a revision that is not current")
	if after := h.items(id)[promotionexec.NodeApproveFinance]; after.version != before.version || after.transitions != before.transitions || after.claimedBy != "" {
		t.Fatalf("a stale decision wrote to the approval: %+v -> %+v", before, after)
	}

	decided, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{
		IntentId: id, Approve: true, Reason: "finance approves", ExpectedProposalRevisionId: revision,
	})
	if err != nil {
		t.Fatalf("DecideJourney naming the reviewed revision: %v", err)
	}
	if got := decided.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL {
		t.Fatalf("after the bound decision stage = %s, want MANAGER_APPROVAL", got)
	}
	if done := h.items(id)[promotionexec.NodeApproveFinance]; done.status != string(workitem.StatusCompleted) {
		t.Fatalf("finance approval = %+v, want completed", done)
	}
}
