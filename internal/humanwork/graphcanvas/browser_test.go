//go:build js && wasm

package graphcanvas

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/testkit/ssr"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_WF_UI_001_Browser mounts the canvas in a browser runtime and
// drives it end to end: structure and localized roles, real pointerdown
// selection, real keyboard moves, real toolbar zoom, and coordinate drag,
// connect and pan gestures on the mounted machine with DOM assertions.
//
// Harness note: the render fixture synthesizes DOM events without pointer
// coordinates, so coordinate delivery (the wasm adapters reading clientX/Y)
// cannot come through DispatchByID. Coordinate gestures therefore drive the
// mounted component's own machine through the same entry points the DOM
// handlers call, while selection, keyboard and toolbar go through real
// dispatched DOM events. The drag-path DOM writes run against a document
// mock through the exact functions the component calls.
func TestTodo_WF_UI_001_Browser(t *testing.T) {
	newFixture := func(t *testing.T, id string, machine *Machine, graph Graph, locale string, minimap bool, onGraph func(Graph), onView func(Viewport), onSelect func([]string)) *render.Fixture {
		t.Helper()
		f := render.New(t)
		f.Render(ui.CreateElement(Canvas, Props{
			ID: id, Graph: graph, Viewport: Viewport{Zoom: 1}, Grid: 10,
			Locale: locale, Minimap: minimap, Machine: machine,
			OnGraph: onGraph, OnView: onView, OnSelect: onSelect,
		}))
		return f
	}

	t.Run("structure", func(t *testing.T) {
		graph := browserGraph()
		f := newFixture(t, "browser", nil, graph, "en", true, nil, nil, nil)
		canvas := f.ByID("browser")
		if canvas == nil {
			t.Fatal("canvas root missing")
		}
		if canvas.Attr("role") != "group" || canvas.Attr("aria-label") != "Graph canvas" {
			t.Fatalf("canvas role/label = %q/%q", canvas.Attr("role"), canvas.Attr("aria-label"))
		}
		if canvas.Attr("dir") != "ltr" {
			t.Fatalf("canvas dir = %q, want ltr", canvas.Attr("dir"))
		}
		node := f.ByID("browser-node-a")
		if node == nil {
			t.Fatal("node a missing")
		}
		if node.Attr("role") != "button" || node.Attr("aria-pressed") != "false" {
			t.Fatalf("node role/pressed = %q/%q", node.Attr("role"), node.Attr("aria-pressed"))
		}
		// The fixture stores styles per-property with no style reader, so
		// node positions assert through SSR serialization of the same props.
		snap := ssr.Render(t, ui.CreateElement(Canvas, Props{ID: "browser", Graph: graph, Viewport: Viewport{Zoom: 1}, Grid: 10, Locale: "en", Minimap: true}))
		if !snap.Contains(`id="browser-node-a"`) || !snap.Contains("left:0px") {
			t.Fatal("node a did not render positioned")
		}
		if f.ByLabel("Zoom in") == nil || f.ByLabel("Zoom out") == nil || f.ByLabel("Reset view") == nil {
			t.Fatal("toolbar buttons missing")
		}
		if f.ByLabel("Minimap") == nil {
			t.Fatal("minimap missing")
		}
	})

	t.Run("select_and_keyboard", func(t *testing.T) {
		var selected [][]string
		var graphs []Graph
		f := newFixture(t, "keys", nil, browserGraph(), "en", false,
			func(g Graph) { graphs = append(graphs, g) }, nil, func(ids []string) { selected = append(selected, ids) })
		// Real pointerdown dispatch selects the node.
		f.DispatchByID("keys-node-a", "onpointerdown", render.Event{})
		f.Stabilize()
		if len(selected) != 1 || len(selected[0]) != 1 || selected[0][0] != "a" {
			t.Fatalf("selection callbacks = %+v, want [[a]]", selected)
		}
		if got := f.ByID("keys-node-a").Attr("aria-pressed"); got != "true" {
			t.Fatalf("aria-pressed = %q after select, want true", got)
		}
		// Real keydown dispatch moves the selection.
		f.DispatchByID("keys", "onkeydown", render.Event{Key: "ArrowRight"})
		f.Stabilize()
		if len(graphs) != 1 {
			t.Fatalf("graph commits = %d, want 1", len(graphs))
		}
		moved, ok := graphs[0].NodeByID("a")
		if !ok || moved.X != 10 || moved.Y != 0 {
			t.Fatalf("moved node = %+v/%v, want (10,0)", moved, ok)
		}
		movedSnap := ssr.Render(t, ui.CreateElement(Canvas, Props{ID: "keys", Graph: graphs[0], Viewport: Viewport{Zoom: 1}, Grid: 10, Locale: "en"}))
		if !movedSnap.Contains("left:10px") {
			t.Fatal("moved node did not render at left:10px")
		}
		// Real Escape cancels the in-progress drag from the pointerdown first,
		// then clears the selection.
		f.DispatchByID("keys", "onkeydown", render.Event{Key: "Escape"})
		f.Stabilize()
		f.DispatchByID("keys", "onkeydown", render.Event{Key: "Escape"})
		f.Stabilize()
		if got := f.ByID("keys-node-a").Attr("aria-pressed"); got != "false" {
			t.Fatalf("aria-pressed = %q after Escape, want false", got)
		}
		if last := selected[len(selected)-1]; len(last) != 0 {
			t.Fatalf("final selection callback = %+v, want empty", last)
		}
	})

	t.Run("toolbar_zoom", func(t *testing.T) {
		var views []Viewport
		f := newFixture(t, "zoom", nil, browserGraph(), "en", false, nil, func(v Viewport) { views = append(views, v) }, nil)
		zoomIn := f.ByLabel("Zoom in")
		if zoomIn == nil {
			t.Fatal("zoom-in button missing")
		}
		zoomIn.Click()
		if len(views) != 1 || views[0].Zoom != 1.2 {
			t.Fatalf("viewport callbacks = %+v, want one zoom to 1.2", views)
		}
	})

	t.Run("drag_connect_pan", func(t *testing.T) {
		machine := NewMachine(browserGraph(), Viewport{Zoom: 1}, 10)
		var graphs []Graph
		var views []Viewport
		f := newFixture(t, "gestures", machine, browserGraph(), "en", false,
			func(g Graph) { graphs = append(graphs, g) },
			func(v Viewport) { views = append(views, v) }, nil)

		// Drag node a by (+50,+20): one commit on drop, DOM follows.
		graphCommits := len(graphs)
		machine.PointerDown(PointerEvent{Kind: PointerDown, X: 50, Y: 30, Target: TargetNode, NodeID: "a"})
		f.Stabilize()
		machine.PointerMove(PointerEvent{Kind: PointerMove, X: 100, Y: 50})
		preview, ok := machine.DragPreview()
		if !ok || preview["a"].X != 50 || preview["a"].Y != 20 {
			t.Fatalf("drag preview = %+v/%v, want +50/+20", preview, ok)
		}
		if len(graphs) != graphCommits {
			t.Fatal("drag moves committed graph state")
		}
		machine.PointerUp(PointerEvent{Kind: PointerUp, X: 100, Y: 50})
		f.Stabilize()
		if len(graphs) != graphCommits+1 {
			t.Fatalf("graph commits = %d, want exactly one on drop", len(graphs)-graphCommits)
		}
		dropped, ok := graphs[len(graphs)-1].NodeByID("a")
		if !ok || dropped.X != 50 || dropped.Y != 20 {
			t.Fatalf("dropped node = %+v/%v, want (50,20)", dropped, ok)
		}
		dropSnap := ssr.Render(t, ui.CreateElement(Canvas, Props{ID: "gestures", Graph: graphs[len(graphs)-1], Viewport: Viewport{Zoom: 1}, Grid: 10, Locale: "en"}))
		if !dropSnap.Contains("left:50px") || !dropSnap.Contains("top:20px") {
			t.Fatal("dropped node did not render at left:50px top:20px")
		}

		// Connect a/out to b/in: the new edge path renders.
		machine.PointerDown(PointerEvent{Kind: PointerDown, X: 100, Y: 30, Target: TargetPort, NodeID: "a", PortID: "out"})
		machine.PointerMove(PointerEvent{Kind: PointerMove, X: 200, Y: 70, Target: TargetPort, NodeID: "b", PortID: "in"})
		machine.PointerUp(PointerEvent{Kind: PointerUp, X: 200, Y: 70, Target: TargetPort, NodeID: "b", PortID: "in"})
		f.Stabilize()
		if len(machine.Edges) != 2 {
			t.Fatalf("edges = %d, want the new connection", len(machine.Edges))
		}
		if f.ByID("gestures-edge-edge-a-out-b-in") == nil {
			t.Fatal("new edge path did not render")
		}

		// Pan the canvas: the viewport transform follows.
		machine.PointerDown(PointerEvent{Kind: PointerDown, X: 150, Y: 30, Target: TargetCanvas})
		machine.PointerMove(PointerEvent{Kind: PointerMove, X: 170, Y: 40})
		machine.PointerUp(PointerEvent{Kind: PointerUp, X: 170, Y: 40})
		f.Stabilize()
		if machine.Viewport.PanX != 20 || machine.Viewport.PanY != 10 {
			t.Fatalf("pan = %+v, want +20/+10", machine.Viewport)
		}
		if len(views) == 0 {
			t.Fatal("pan reported no viewport change")
		}
	})

	t.Run("direct_dom_transform", func(t *testing.T) {
		restore := installStyleCapture(t)
		defer restore()
		applyNodeTransform("gestures-node-a", 50, 20)
		writes := capturedStyleWrites(t)
		if len(writes) != 1 || writes[0].Element != "gestures-node-a" || writes[0].Property != "transform" {
			t.Fatalf("style writes = %+v, want one transform write", writes)
		}
		if !strings.Contains(writes[0].Value, "translate(50px,20px)") {
			t.Fatalf("transform value = %q", writes[0].Value)
		}
		clearNodeTransform("gestures-node-a")
		if writes := capturedStyleWrites(t); len(writes) != 2 || writes[1].Value != "" {
			t.Fatalf("clear writes = %+v, want the transform cleared", writes)
		}
	})
}

func browserGraph() Graph {
	return Graph{
		Nodes: []Node{
			{ID: "a", Label: "Alpha", X: 0, Y: 0, W: 100, H: 60, Ports: []Port{{ID: "out", Side: PortEast, Offset: 0.5}}},
			{ID: "b", Label: "Beta", X: 200, Y: 40, W: 100, H: 60, Ports: []Port{{ID: "in", Side: PortWest, Offset: 0.5}}},
		},
		Edges: []Edge{{ID: "e1", FromNode: "a", FromPort: "out", ToNode: "b", ToPort: "in"}},
	}
}

// styleWrite records one captured style.setProperty call.
type styleWrite struct {
	Element  string
	Property string
	Value    string
}

var browserStyleWrites []styleWrite

// installStyleCapture replaces the global document with a mock capturing
// style writes, returning a restore function.
func installStyleCapture(t *testing.T) func() {
	t.Helper()
	browserStyleWrites = nil
	global := js.Global()
	previous := global.Get("document")
	mock := js.Global().Get("Object").New()
	elements := map[string]js.Value{}
	mock.Set("getElementById", js.FuncOf(func(_ js.Value, args []js.Value) any {
		id := args[0].String()
		if element, ok := elements[id]; ok {
			return element
		}
		style := js.Global().Get("Object").New()
		style.Set("setProperty", js.FuncOf(func(_ js.Value, propArgs []js.Value) any {
			browserStyleWrites = append(browserStyleWrites, styleWrite{Element: id, Property: propArgs[0].String(), Value: propArgs[1].String()})
			return nil
		}))
		element := js.Global().Get("Object").New()
		element.Set("style", style)
		elements[id] = element
		return element
	}))
	global.Set("document", mock)
	return func() { global.Set("document", previous) }
}

func capturedStyleWrites(t *testing.T) []styleWrite {
	t.Helper()
	return append([]styleWrite(nil), browserStyleWrites...)
}
