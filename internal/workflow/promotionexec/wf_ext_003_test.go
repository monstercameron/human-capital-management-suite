package promotionexec

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// promotionSimulatePlanDigest is the current 1.2.0 zero-effect SIMULATE projection.
// It moved last with the Promotion helpers it replaces; WF-EXT-003 pins it
// here so the compiler-owned projection can never drift from it.
const promotionSimulatePlanDigest = "e889b8f99cb96a0049a4740c1dab24c00a4de652169a6f725bd796da29871617"

// promotionSimulatePlanDigestV1_0 is the frozen 1.0.0 SIMULATE projection,
// pinned for the same reason beside the frozen EXECUTE digest.
const promotionSimulatePlanDigestV1_0 = "0929b9b73194932766031eee551a666327c3034afa1d878933f8242ea8c14f94"

// TestTodo_WF_EXT_003_Golden proves planning/todos.md WF-EXT-003: moving
// route aliases and the SIMULATE projection from the Promotion helpers into
// the compiler leaves every Promotion plan digest unchanged — the EXECUTE
// and SIMULATE projections of the current graph and of the frozen 1.0.0
// graph.
func TestTodo_WF_EXT_003_Golden(t *testing.T) {
	exec, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := exec.Digest(); got != promotionExecutePlanDigest {
		t.Fatalf("EXECUTE digest = %q, want the pinned %q", got, promotionExecutePlanDigest)
	}
	sim, err := CompileSimulation()
	if err != nil {
		t.Fatalf("CompileSimulation: %v", err)
	}
	if got := sim.Digest(); got != promotionSimulatePlanDigest {
		t.Fatalf("SIMULATE digest = %q, want the pinned %q", got, promotionSimulatePlanDigest)
	}
	if sim.Digest() == exec.Digest() {
		t.Fatal("SIMULATE projection must be a distinct plan from EXECUTE")
	}
	if !sim.Effects.ZeroEffect {
		t.Fatalf("SIMULATE effects = %+v, want zero effects", sim.Effects)
	}
	exec10, err := CompileV1_0()
	if err != nil {
		t.Fatalf("CompileV1_0: %v", err)
	}
	if got := exec10.Digest(); got != promotionExecutePlanDigestV1_0 {
		t.Fatalf("1.0.0 EXECUTE digest = %q, want the frozen %q", got, promotionExecutePlanDigestV1_0)
	}
	sim10, err := CompileSimulationV1_0()
	if err != nil {
		t.Fatalf("CompileSimulationV1_0: %v", err)
	}
	if got := sim10.Digest(); got != promotionSimulatePlanDigestV1_0 {
		t.Fatalf("1.0.0 SIMULATE digest = %q, want the pinned %q", got, promotionSimulatePlanDigestV1_0)
	}
	if !sim10.Effects.ZeroEffect {
		t.Fatalf("1.0.0 SIMULATE effects = %+v, want zero effects", sim10.Effects)
	}
	// The projection suppresses exactly the two writes the helpers used to
	// special-case, and nothing else changes mode.
	for _, id := range []string{NodeExecutePromotion, NodeCompensateHold} {
		node, ok := sim.Node(id)
		if !ok || node.EffectClass.IsWrite() || node.EffectRole != "" {
			t.Fatalf("SIMULATE node %s = %+v, want a role-free read", id, node)
		}
		if node.Capability == nil || node.Capability.OperationMode != workflow.ModeSimulate {
			t.Fatalf("SIMULATE node %s capability = %+v, want SIMULATE mode", id, node.Capability)
		}
	}
	snapshot, _ := sim.Node(NodeSnapshotWorker)
	if snapshot.Capability == nil || snapshot.Capability.OperationMode != workflow.ModeExecute {
		t.Fatalf("SIMULATE read capability = %+v, want the EXECUTE binding untouched", snapshot.Capability)
	}
}
