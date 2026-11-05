package graphcanvas

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/testkit/ssr"
)

func testGraph() Graph {
	return Graph{
		Nodes: []Node{
			{ID: "a", Label: "Alpha", X: 0, Y: 0, W: 100, H: 60, Ports: []Port{{ID: "out", Side: PortEast, Offset: 0.5}}},
			{ID: "b", Label: "Beta", X: 200, Y: 40, W: 100, H: 60, Ports: []Port{{ID: "in", Side: PortWest, Offset: 0.5}}},
			{ID: "c", Label: "Gamma", X: 400, Y: 0, W: 100, H: 60},
		},
		Edges: []Edge{{ID: "e1", FromNode: "a", FromPort: "out", ToNode: "b", ToPort: "in"}},
	}
}

// TestTodo_WF_UI_001 proves the PRIMARY matrix of WF-UI-001: the viewport
// transforms invert, hit-testing finds nodes and ports, marquees normalize,
// snapping and minimaps project, edges path between ports, layout layers
// deterministically, and the interaction machine drags (preview without
// commit), pans, zooms, marquees, connects and keyboard-drives the graph.
func TestTodo_WF_UI_001(t *testing.T) {
	t.Run("viewport", func(t *testing.T) {
		view := Viewport{PanX: 10, PanY: -20, Zoom: 2}
		world := Point{X: 30, Y: 45}
		screen := view.WorldToScreen(world)
		back := view.ScreenToWorld(screen)
		if back != world {
			t.Fatalf("round trip = %+v, want %+v", back, world)
		}
		zoomed := view.ZoomAt(screen, 2)
		if kept := zoomed.ScreenToWorld(screen); kept != world {
			t.Fatalf("zoom moved the anchor: %+v, want %+v", kept, world)
		}
		if got := (Viewport{Zoom: 99}).Normalized().Zoom; got != MaxZoom {
			t.Fatalf("zoom clamp = %v, want %v", got, MaxZoom)
		}
		if got := (Viewport{PanX: 10, PanY: 20, Zoom: 2}).Transform(); got != "translate(10px,20px) scale(2)" {
			t.Fatalf("transform = %q", got)
		}
	})

	t.Run("hit_test", func(t *testing.T) {
		graph := testGraph()
		node, ok := HitNode(graph.Nodes, Point{X: 50, Y: 30})
		if !ok || node.ID != "a" {
			t.Fatalf("hit = %+v/%v, want node a", node, ok)
		}
		if _, ok := HitNode(graph.Nodes, Point{X: 150, Y: 30}); ok {
			t.Fatal("gap between nodes hit")
		}
		// Last in order wins on overlap.
		overlap := []Node{{ID: "low", X: 0, Y: 0, W: 100, H: 100}, {ID: "high", X: 50, Y: 50, W: 100, H: 100}}
		if top, _ := HitNode(overlap, Point{X: 75, Y: 75}); top.ID != "high" {
			t.Fatalf("overlap hit = %s, want high", top.ID)
		}
		portNode, port, ok := HitPort(graph.Nodes, Point{X: 102, Y: 30}, 8)
		if !ok || portNode.ID != "a" || port.ID != "out" {
			t.Fatalf("port hit = %s/%s/%v, want a/out", portNode.ID, port.ID, ok)
		}
		if _, _, ok := HitPort(graph.Nodes, Point{X: 150, Y: 30}, 8); ok {
			t.Fatal("empty space hit a port")
		}
	})

	t.Run("marquee_snap_minimap", func(t *testing.T) {
		graph := testGraph()
		// Reversed drag direction still selects.
		if ids := MarqueeSelect(graph.Nodes, Rect{X: 350, Y: 100, W: -400, H: -120}); len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
			t.Fatalf("marquee = %v, want [a b]", ids)
		}
		if got := Snap(27, 10); got != 30 {
			t.Fatalf("snap = %v, want 30", got)
		}
		if got := Snap(27, 0); got != 27 {
			t.Fatalf("zero grid snap = %v, want 27", got)
		}
		bounds, ok := ContentBounds(graph.Nodes)
		if !ok || bounds.W != 500 || bounds.H != 100 {
			t.Fatalf("bounds = %+v/%v, want 500x100", bounds, ok)
		}
		mini := FitMinimap(bounds, true, 200, 100)
		if mini.Scale != 0.4 {
			t.Fatalf("minimap scale = %v, want 0.4", mini.Scale)
		}
		view := mini.ViewportRect(Viewport{Zoom: 1}, 200, 100, bounds)
		if view.W != 80 || view.H != 40 {
			t.Fatalf("minimap viewport = %+v, want 80x40", view)
		}
		if empty := FitMinimap(Rect{}, false, 200, 100); empty.HasGraph || empty.Scale != 0 {
			t.Fatalf("empty minimap = %+v, want no graph", empty)
		}
	})

	t.Run("edge_path", func(t *testing.T) {
		graph := testGraph()
		path, ok := EdgePath(graph, graph.Edges[0])
		if !ok {
			t.Fatal("edge did not resolve")
		}
		if !strings.HasPrefix(path, "M100 30 C") || !strings.HasSuffix(path, "200 70") {
			t.Fatalf("edge path = %q, want bezier from a/out to b/in", path)
		}
		if _, ok := EdgePath(graph, Edge{FromNode: "a", FromPort: "out", ToNode: "ghost", ToPort: "in"}); ok {
			t.Fatal("dangling edge drew")
		}
	})

	t.Run("layout", func(t *testing.T) {
		graph := testGraph()
		first := LayeredLayout(graph.Nodes, graph.Edges, LayoutOptions{})
		second := LayeredLayout(graph.Nodes, graph.Edges, LayoutOptions{})
		for id, p := range first {
			if second[id] != p {
				t.Fatal("layout is not deterministic")
			}
		}
		if first["a"].Y != 0 || first["c"].Y != 0 || first["b"].Y <= 0 {
			t.Fatalf("layers = %+v, want a above b with c beside a", first)
		}
		if first["c"].X <= first["a"].X {
			t.Fatalf("order = %+v, want c past a on layer zero", first)
		}
		// Cycles cannot stall layering.
		cyclic := Graph{
			Nodes: []Node{{ID: "x"}, {ID: "y"}},
			Edges: []Edge{{FromNode: "x", ToNode: "y"}, {FromNode: "y", ToNode: "x"}},
		}
		if got := LayeredLayout(cyclic.Nodes, cyclic.Edges, LayoutOptions{}); len(got) != 2 {
			t.Fatalf("cyclic layout = %+v, want both nodes placed", got)
		}
	})

	t.Run("interact", func(t *testing.T) {
		var changes []Change
		canvas := NewMachine(testGraph(), Viewport{Zoom: 1}, 10)
		canvas.OnChange = func(c Change) { changes = append(changes, c) }

		// Node drag previews without committing, then commits once on drop.
		down := PointerEvent{Kind: PointerDown, X: 50, Y: 30, Target: TargetNode, NodeID: "a"}
		canvas.PointerDown(down)
		moves := canvas.Commits()
		for i := 1; i <= 5; i++ {
			canvas.PointerMove(PointerEvent{Kind: PointerMove, X: 50 + float64(i*10), Y: 30})
		}
		preview, ok := canvas.DragPreview()
		if !ok || preview["a"].X != 50 {
			t.Fatalf("drag preview = %+v/%v, want +50x", preview, ok)
		}
		if canvas.Commits() != moves {
			t.Fatal("drag moves committed state")
		}
		canvas.PointerUp(PointerEvent{Kind: PointerUp, X: 100, Y: 30})
		if canvas.Commits() != moves+1 {
			t.Fatalf("commits = %d, want exactly one on drop", canvas.Commits()-moves)
		}
		if x := canvas.Nodes[canvas.index["a"]].X; x != 50 {
			t.Fatalf("node a x = %v, want 50", x)
		}
		if _, ok := canvas.DragPreview(); ok {
			t.Fatal("preview survived the drop")
		}

		// Pan commits live; wheel zooms around the cursor.
		canvas.PointerDown(PointerEvent{Kind: PointerDown, X: 150, Y: 30, Target: TargetCanvas})
		canvas.PointerMove(PointerEvent{Kind: PointerMove, X: 170, Y: 40})
		canvas.PointerUp(PointerEvent{Kind: PointerUp, X: 170, Y: 40})
		if canvas.Viewport.PanX != 20 || canvas.Viewport.PanY != 10 {
			t.Fatalf("pan = %+v, want +20/+10", canvas.Viewport)
		}
		canvas.Wheel(WheelEvent{DeltaY: -250, X: 100, Y: 100})
		if canvas.Viewport.Zoom != 1.5 {
			t.Fatalf("zoom = %v, want 1.5", canvas.Viewport.Zoom)
		}

		// Shift marquee adds; plain click clears.
		marquee := NewMachine(testGraph(), Viewport{Zoom: 1}, 0)
		marquee.PointerDown(PointerEvent{Kind: PointerDown, X: 150, Y: 100, Target: TargetCanvas, Shift: true})
		marquee.PointerMove(PointerEvent{Kind: PointerMove, X: 450, Y: -20})
		marquee.PointerUp(PointerEvent{Kind: PointerUp, X: 450, Y: -20, Shift: true})
		if ids := marquee.SelectedIDs(); len(ids) != 2 || ids[0] != "b" || ids[1] != "c" {
			t.Fatalf("marquee selection = %v, want [b c]", ids)
		}

		// Port drag connects; self-port drops cancel.
		connect := NewMachine(testGraph(), Viewport{Zoom: 1}, 0)
		connect.PointerDown(PointerEvent{Kind: PointerDown, X: 100, Y: 30, Target: TargetPort, NodeID: "a", PortID: "out"})
		connect.PointerMove(PointerEvent{Kind: PointerMove, X: 200, Y: 70, Target: TargetPort, NodeID: "b", PortID: "in"})
		if !connect.DraftActive {
			t.Fatal("no edge draft during port drag")
		}
		connect.PointerUp(PointerEvent{Kind: PointerUp, X: 200, Y: 70, Target: TargetPort, NodeID: "b", PortID: "in"})
		if len(connect.Edges) != 2 || connect.Edges[1].ToNode != "b" {
			t.Fatalf("edges = %+v, want the new connection", connect.Edges)
		}

		// Keyboard: arrows move visually, Escape clears, 0 resets.
		keys := NewMachine(testGraph(), Viewport{Zoom: 2}, 10)
		keys.Selection["c"] = true
		keys.Key(KeyEvent{Key: "ArrowLeft"})
		if x := keys.Nodes[keys.index["c"]].X; x != 390 {
			t.Fatalf("keyboard move x = %v, want 390", x)
		}
		keys.Key(KeyEvent{Key: "Escape"})
		if len(keys.Selection) != 0 {
			t.Fatal("Escape left a selection")
		}
		keys.Key(KeyEvent{Key: "0"})
		if keys.Viewport.Zoom != 1 || keys.Viewport.PanX != 0 {
			t.Fatalf("reset viewport = %+v", keys.Viewport)
		}
	})

	t.Run("render", func(t *testing.T) {
		snap := ssr.Render(t, Canvas(Props{
			ID: "test", Graph: testGraph(),
			Viewport: Viewport{PanX: 10, PanY: 20, Zoom: 2},
			Locale:   "en", Minimap: true,
		}))
		for _, want := range []string{
			`id="test"`, `class="gc-canvas"`, `role="group"`, `aria-label="Graph canvas"`,
			`tabIndex="0"`, `dir="ltr"`, `translate(10px,20px) scale(2)`,
			`<svg`, `<path`, `data-edge="e1"`, `d="M100 30 C`,
			`data-node="a"`, `id="test-node-a"`, `Alpha`, `data-port="out"`,
			`aria-pressed="false"`, `aria-label="Zoom in"`, `aria-label="Zoom out"`,
			`aria-label="Reset view"`, `gc-minimap`, `gc-mini-view`,
		} {
			if !snap.Contains(want) {
				t.Fatalf("render misses %q", want)
			}
		}
		ar := ssr.Render(t, Canvas(Props{ID: "test-ar", Graph: testGraph(), Locale: "ar-EG", Minimap: true}))
		for _, want := range []string{`dir="rtl"`, "لوحة الرسم البياني", "خريطة مصغرة"} {
			if !ar.Contains(want) {
				t.Fatalf("arabic render misses %q", want)
			}
		}
		empty := ssr.Render(t, Canvas(Props{ID: "empty", Locale: "en"}))
		if !empty.Contains("Empty graph") {
			t.Fatal("empty graph renders no empty label")
		}
		sheet := Stylesheet()
		for _, want := range []string{".gc-canvas", ".gc-node", ".gc-edge", ".gc-port", ".gc-marquee", ".gc-minimap", "inline-start", "inline-end"} {
			if !strings.Contains(sheet, want) {
				t.Fatalf("stylesheet misses %q", want)
			}
		}
	})

	t.Run("async_layout", func(t *testing.T) {
		worker := NewLayoutWorker()
		worker.Start()
		defer worker.Stop()
		select {
		case positions := <-worker.Request(LayoutRequest{Nodes: testGraph().Nodes, Edges: testGraph().Edges}):
			if len(positions) != 3 || positions["b"].Y <= 0 {
				t.Fatalf("async layout = %+v", positions)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("layout worker did not answer")
		}
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		select {
		case positions := <-worker.RequestContext(cancelled, LayoutRequest{Nodes: testGraph().Nodes}):
			if positions != nil {
				t.Fatalf("cancelled layout answered %+v", positions)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("cancelled layout hung")
		}
	})
}
