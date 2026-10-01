package recovery

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/presentation"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

func TestRecoveryResolveDelegatesToProductionProjection(t *testing.T) {
	got := Resolve(Input{
		Dimensions: lifecycle.Dimensions{
			Request: lifecycle.RequestClosed, Execution: lifecycle.ExecutionCommitted,
			Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent,
			Obligation: lifecycle.ObligationSatisfied,
		},
		Authorization: presentation.AuthorizationAllowed,
		Freshness:     presentation.FreshnessFresh,
		Operational:   presentation.OperationalReady,
		HasResult:     true, ExternalOutcomeKnown: true,
	})
	if got.State != presentation.StateCompleted {
		t.Fatalf("state=%s, want %s", got.State, presentation.StateCompleted)
	}
	if len(got.Actions) != 2 || got.Actions[0].Action != presentation.ActionReview || got.Actions[1].Action != presentation.ActionCorrect {
		t.Fatalf("actions=%v, want review/correct", got.Actions)
	}
}
