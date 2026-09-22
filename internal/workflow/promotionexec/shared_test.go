package promotionexec

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestSharedGraphIsTheExecuteGraph proves the variant seam hands out the
// exact current execute graph: the shared nodes and edges equal the
// published definition's, and compiling the shared graph reproduces the
// published EXECUTE and SIMULATE digests, so a variant starts from the plan
// it claims to extend.
func TestSharedGraphIsTheExecuteGraph(t *testing.T) {
	def := Definition()
	nodes, edges := SharedGraph()
	if !reflect.DeepEqual(nodes, def.Nodes) {
		t.Fatal("SharedGraph nodes differ from the published execute definition")
	}
	if !reflect.DeepEqual(edges, def.Edges) {
		t.Fatal("SharedGraph edges differ from the published execute definition")
	}
	// The seam must hand out fresh slices: appending to a shared node must
	// not alias the next caller's graph.
	nodes[0].Inputs = append(nodes[0].Inputs, workflow.Field{Path: "probe", Type: workflow.ValueType{Kind: workflow.KindString}})
	again, _ := SharedGraph()
	for _, field := range again[0].Inputs {
		if field.Path == "probe" {
			t.Fatal("SharedGraph aliases node backing arrays across calls")
		}
	}
	shared := Definition()
	shared.Nodes, shared.Edges = SharedGraph()
	compiled, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	recompiled, err := Compile(shared)
	if err != nil {
		t.Fatalf("Compile(SharedGraph): %v", err)
	}
	if recompiled.Digest() != compiled.Digest() {
		t.Fatalf("shared-graph digest = %q, want the execute %q", recompiled.Digest(), compiled.Digest())
	}
	wantSim, err := CompileSimulation()
	if err != nil {
		t.Fatalf("CompileSimulation: %v", err)
	}
	simulated, err := CompileSimulation(shared)
	if err != nil {
		t.Fatalf("CompileSimulation(SharedGraph): %v", err)
	}
	if simulated.Digest() != wantSim.Digest() {
		t.Fatalf("shared-graph simulation digest = %q, want %q", simulated.Digest(), wantSim.Digest())
	}
	if !simulated.Effects.ZeroEffect {
		t.Fatalf("shared-graph simulation effects = %+v, want zero effects", simulated.Effects)
	}
}
