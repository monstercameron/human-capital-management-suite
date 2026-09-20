package promotionexec

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// SharedGraph returns the current execute graph's nodes and edges for variant
// plans to extend. The execute definition stays the sole owner of the graph:
// a variant copies no node literal, it starts from this exact set and adds
// or narrows nodes. The returned slices are freshly built on every call, so
// a variant may append to a node's fields without aliasing the execute
// definition's own backing arrays.
func SharedGraph() ([]workflow.Node, []workflow.Edge) {
	return promotionNodes(true), promotionEdges(true)
}

// ProjectMode returns def projected onto one execution mode: the declared
// modes narrow to mode and, for SIMULATE, the two writes become read-only
// with no core role to classify. EXECUTE returns def with only the mode
// narrowed.
func ProjectMode(def workflow.Definition, mode workflow.ExecutionMode) workflow.Definition {
	def.DeclaredModes = []workflow.ExecutionMode{mode}
	def.Nodes = append([]workflow.Node(nil), def.Nodes...)
	def.Edges = canonicalEdges(def.Edges, def.Nodes)
	if mode == workflow.ModeSimulate {
		for i := range def.Nodes {
			if def.Nodes[i].ID != NodeExecutePromotion && def.Nodes[i].ID != NodeCompensateHold {
				continue
			}
			def.Nodes[i].DeclaredEffect = capability.EffectReadOnly
			// A read-only projection mutates nothing, so it has no core to
			// classify (WF-RUN-037).
			def.Nodes[i].EffectRole = ""
			if def.Nodes[i].Capability != nil {
				ref := *def.Nodes[i].Capability
				ref.OperationMode = workflow.ModeSimulate
				def.Nodes[i].Capability = &ref
			}
		}
	}
	return def
}
