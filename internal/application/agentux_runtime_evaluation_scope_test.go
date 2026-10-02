package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type scopedEvaluationRunner struct{ available bool }

func (r scopedEvaluationRunner) RunPersonaEvaluation(context.Context, PersonaAdminCommandActor, string) (productui.PersonaAdminEvaluationResult, error) {
	return productui.PersonaAdminEvaluationResult{}, nil
}

func (r scopedEvaluationRunner) PersonaEvaluationAvailable(context.Context) bool { return r.available }

type unscopedEvaluationRunner struct{}

func (unscopedEvaluationRunner) RunPersonaEvaluation(context.Context, PersonaAdminCommandActor, string) (productui.PersonaAdminEvaluationResult, error) {
	return productui.PersonaAdminEvaluationResult{}, nil
}

// A cell that serves two tenants binds one tenant's local evaluator. The
// other tenant's administrators must see evaluation as unavailable, and the
// bound tenant must be offered it; before this the evaluator was bound only
// when the process's single default tenant was the demo tenant, so a
// two-tenant cell could never evaluate or publish a version.
func TestPersonaAdminEvaluationAvailabilityFollowsTheRunnerScope(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		runner PersonaAdminEvaluationRunner
		want   bool
	}{
		"no runner":                 {nil, false},
		"runner for this tenant":    {scopedEvaluationRunner{available: true}, true},
		"runner for another tenant": {scopedEvaluationRunner{available: false}, false},
		"runner without a scope":    {unscopedEvaluationRunner{}, true},
	} {
		executor := &PersonaAdminLifecycleExecutor{Evaluations: tc.runner}
		if got := executor.PersonaAdminCommandAvailable(ctx, PersonaAdminRunEvaluation); got != tc.want {
			t.Errorf("%s: available = %v, want %v", name, got, tc.want)
		}
	}
	var runner *localPersonaAdminEvaluationRunner
	if runner.PersonaEvaluationAvailable(ctx) {
		t.Error("a nil local runner reported itself available")
	}
	if (&localPersonaAdminEvaluationRunner{tenant: "ironridge-demo"}).PersonaEvaluationAvailable(ctx) {
		t.Error("a caller without a verified principal was offered evaluation")
	}
}
