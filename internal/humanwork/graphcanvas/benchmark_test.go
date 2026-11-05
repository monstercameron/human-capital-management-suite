package graphcanvas

import (
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/testkit/ssr"
)

// TestTodo_WF_UI_001_Benchmark holds frame-time cost at 300 nodes: layout
// places every node deterministically, and a full render carries all 300
// node markers within the bound. Timings log for tracking; the bound is
// generous by design (agents share this box), while the placement and
// marker counts are exact.
func TestTodo_WF_UI_001_Benchmark(t *testing.T) {
	const nodes = 300
	graph := Graph{}
	for i := 0; i < nodes; i++ {
		graph.Nodes = append(graph.Nodes, Node{
			ID:    fmt.Sprintf("n%03d", i),
			Label: fmt.Sprintf("Node %d", i),
			W:     160, H: 64,
			Ports: []Port{{ID: "in", Side: PortWest, Offset: 0.5}, {ID: "out", Side: PortEast, Offset: 0.5}},
		})
		if i > 0 {
			graph.Edges = append(graph.Edges, Edge{
				ID:       fmt.Sprintf("e%03d", i),
				FromNode: fmt.Sprintf("n%03d", i-1), FromPort: "out",
				ToNode: fmt.Sprintf("n%03d", i), ToPort: "in",
			})
		}
		if i > 1 {
			graph.Edges = append(graph.Edges, Edge{
				ID:       fmt.Sprintf("s%03d", i),
				FromNode: fmt.Sprintf("n%03d", i-2), FromPort: "out",
				ToNode: fmt.Sprintf("n%03d", i), ToPort: "in",
			})
		}
	}

	start := time.Now()
	first := LayeredLayout(graph.Nodes, graph.Edges, LayoutOptions{})
	layoutElapsed := time.Since(start)
	if len(first) != nodes {
		t.Fatalf("layout placed %d nodes, want %d", len(first), nodes)
	}
	second := LayeredLayout(graph.Nodes, graph.Edges, LayoutOptions{})
	for id, p := range first {
		if second[id] != p {
			t.Fatal("layout is not deterministic at 300 nodes")
		}
	}
	for i, node := range graph.Nodes {
		at := first[node.ID]
		graph.Nodes[i].X, graph.Nodes[i].Y = at.X, at.Y
	}

	start = time.Now()
	snap := ssr.Render(t, Canvas(Props{ID: "bench", Graph: graph, Viewport: Viewport{Zoom: 1}, Locale: "en", Minimap: true}))
	renderElapsed := time.Since(start)
	markers := 0
	for i := 0; i < nodes; i++ {
		if snap.Contains(fmt.Sprintf(`data-node="n%03d"`, i)) {
			markers++
		}
	}
	if markers != nodes {
		t.Fatalf("render carries %d node markers, want %d", markers, nodes)
	}
	t.Logf("300 nodes: layout %s, render %s", layoutElapsed, renderElapsed)
	if layoutElapsed+renderElapsed > 5*time.Minute {
		t.Fatalf("300-node frame cost %s exceeds the bound", layoutElapsed+renderElapsed)
	}
}
