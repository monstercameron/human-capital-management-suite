package application_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/replay"
)

// TestTodo_WF_RUN_013_Served proves WF-RUN-013's deterministic-replay core is
// reachable through the served application boundary: default candidates
// recompute DECISION nodes, recomputability admits only pure deterministic
// steps, an in-memory record round-trips through the served source, and
// construction without a plan is refused with the typed options code.
func TestTodo_WF_RUN_013_Served(t *testing.T) {
	surface := application.NewServedReplaySurface()

	defaults := surface.DefaultCandidates()
	if _, ok := defaults.ByStepType[workflow.StepDecision]; !ok {
		t.Fatal("served default candidates do not recompute DECISION nodes")
	}
	if !surface.Recomputable(workflow.CompiledNode{Type: workflow.StepDecision, EffectClass: capability.EffectPure}) {
		t.Fatal("served replay does not recompute a pure DECISION node")
	}
	if surface.Recomputable(workflow.CompiledNode{Type: workflow.StepApproval, EffectClass: capability.EffectPure}) {
		t.Fatal("served replay recomputes an approval instead of taking its recorded outcome")
	}
	if surface.Recomputable(workflow.CompiledNode{Type: workflow.StepDecision, EffectClass: capability.EffectExternalMutation}) {
		t.Fatal("served replay recomputes an effecting node")
	}

	source := surface.NewMemorySource(replay.Record{})
	if got := source.Record(); got.CompiledPlanDigest != "" {
		t.Fatalf("served memory source holds %q, want the empty record", got.CompiledPlanDigest)
	}

	if _, err := surface.New(replay.Options{}); surface.CodeOf(err) != replay.CodeInvalidOptions {
		t.Fatalf("served New without a plan: code = %q (%v), want %s", surface.CodeOf(err), err, replay.CodeInvalidOptions)
	}
}

func TestServedReplayAppSurface(t *testing.T) {
	var app application.App
	if got := app.Replay(); got.New == nil || got.DefaultCandidates == nil {
		t.Fatal("composed application omitted Replay surface")
	}
	var nilApp *application.App
	if got := nilApp.Replay(); got.New != nil {
		t.Fatal("nil application exposed Replay capabilities")
	}
}
