package promotionexec

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// SharedGraph returns the current execute graph's nodes and edges for variant
// plans to extend. The execute definition stays the sole owner of the graph:
// a variant copies no node literal, it starts from this exact set and adds
// or narrows nodes. The returned slices are freshly built on every call, so
// a variant may append to a node's fields without aliasing the execute
// definition's own backing arrays.
//
// The nodes carry their declared outcome aliases and SIMULATE mode overlays
// (WF-EXT-003), so a variant that starts here compiles through the same
// compiler canonicalization and projection as the execute plan itself.
func SharedGraph() ([]workflow.Node, []workflow.Edge) {
	return promotionNodes(true), promotionEdges(true)
}
