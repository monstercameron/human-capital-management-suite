package journey_test

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// The reviewed proposal revision travels from the wire to the engine port
// unchanged, so the owning engine can refuse a decision on a proposal that
// changed after the approver read it (#3724).
func TestIssue3724_DecideForwardsTheReviewedRevision(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	_, err := client.DecideJourney(authorizedContext(t), &journeyv1.DecideJourneyRequest{
		IntentId: fixtureIntentID, Approve: false, Reason: "scope unclear",
		ExpectedProposalRevisionId: "rev-reviewed-7",
	})
	if err != nil {
		t.Fatalf("DecideJourney: %v", err)
	}
	engine.mu.Lock()
	got := engine.lastDecision
	engine.mu.Unlock()
	want := workspace.Decision{Approve: false, Reason: "scope unclear", ExpectedProposalRevisionID: "rev-reviewed-7"}
	if got != want {
		t.Fatalf("the engine saw %+v, want %+v", got, want)
	}
}

// A stale-revision refusal from the engine reaches the caller as a failed
// precondition with the stage reason, never as a success or a generic error.
func TestIssue3724_StaleRevisionRefusalIsAFailedPrecondition_Security(t *testing.T) {
	engine := newFakeEngine()
	engine.decideErr = fmt.Errorf("%w: the proposal changed after it was reviewed", workspace.ErrJourneyStage)
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	_, err := client.DecideJourney(authorizedContext(t), &journeyv1.DecideJourneyRequest{
		IntentId: fixtureIntentID, Approve: true, ExpectedProposalRevisionId: "rev-old",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale decision = %v, want FailedPrecondition", err)
	}
}
