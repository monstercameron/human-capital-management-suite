package graphcanvas

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Component element ids and classes. Node elements carry stable ids so drag
// previews can transform them directly in the DOM.
const (
	ClassCanvas   = "gc-canvas"
	ClassViewport = "gc-viewport"
	ClassEdges    = "gc-edges"
	ClassNode     = "gc-node"
	ClassSelected = "gc-selected"
	ClassPort     = "gc-port"
	ClassMarquee  = "gc-marquee"
	ClassMinimap  = "gc-minimap"
	ClassToolbar  = "gc-toolbar"
)

func nodeElementID(canvasID, nodeID string) string { return canvasID + "-node-" + nodeID }

// Props seeds the canvas. The component is uncontrolled by default: props
// seed the interaction machine on mount, and commits report through the
// callbacks. Pass Machine for controlled mode: the component drives the
// caller's machine (wiring commit callbacks and redraws onto it) instead of
// owning one. Later prop changes never rewrite gesture state.
type Props struct {
	ID       string
	Graph    Graph
	Viewport Viewport
	Grid     float64
	Locale   string
	Minimap  bool
	Machine  *Machine
	OnGraph  func(Graph)
	OnView   func(Viewport)
	OnSelect func([]string)
}

// Canvas renders the graph canvas: one transformed viewport container with
// an SVG edge layer and HTML nodes, a marquee, a toolbar and a minimap. Drag
// previews paint as direct DOM transforms during the gesture; node state
// commits once on drop.
func Canvas(props Props) ui.Node {
	if props.ID == "" {
		props.ID = "graphcanvas"
	}
	copy := CopyFor(props.Locale)
	owned := ui.UseRef((*Machine)(nil))
	version := ui.UseState(0)
	redraw := func() { version.Set(version.Get() + 1) }
	canvas := props.Machine
	if canvas == nil {
		canvas = owned.Get()
		if canvas == nil {
			canvas = NewMachine(props.Graph, props.Viewport, props.Grid)
			owned.Set(canvas)
		}
	}
	canvas.OnChange = func(change Change) {
		switch change.Kind {
		case ChangeNodesMoved, ChangeEdgeAdded:
			if props.OnGraph != nil {
				props.OnGraph(Graph{Nodes: append([]Node(nil), canvas.Nodes...), Edges: append([]Edge(nil), canvas.Edges...)})
			}
		case ChangeViewport:
			if props.OnView != nil {
				props.OnView(canvas.Viewport)
			}
		case ChangeSelection:
			if props.OnSelect != nil {
				props.OnSelect(canvas.SelectedIDs())
			}
		}
		redraw()
	}

	canvasID := props.ID
	downOn := func(target TargetKind, nodeID, portID string) ui.Handler {
		return ui.UseEvent(func(event ui.MouseEvent) {
			x, y, button, shift := pointerCoords(canvasID, event)
			event.StopPropagation()
			canvas.PointerDown(PointerEvent{Kind: PointerDown, X: x, Y: y, Button: button, Shift: shift, Target: target, NodeID: nodeID, PortID: portID})
		})
	}
	moveOn := ui.UseEvent(func(event ui.MouseEvent) {
		x, y, _, shift := pointerCoords(canvasID, event)
		nodeID, portID := candidatesOf(canvas, x, y)
		canvas.PointerMove(PointerEvent{Kind: PointerMove, X: x, Y: y, Shift: shift, Target: moveTarget(nodeID, portID), NodeID: nodeID, PortID: portID})
		if preview, ok := canvas.DragPreview(); ok {
			zoom := canvas.Viewport.Normalized().Zoom
			for id, offset := range preview {
				applyNodeTransform(nodeElementID(canvasID, id), offset.X*zoom, offset.Y*zoom)
			}
		}
		if canvas.MarqueeActive || canvas.DraftActive {
			redraw()
		}
	})
	upOn := ui.UseEvent(func(event ui.MouseEvent) {
		x, y, _, shift := pointerCoords(canvasID, event)
		dragged, _ := canvas.DragPreview()
		canvas.PointerUp(PointerEvent{Kind: PointerUp, X: x, Y: y, Shift: shift})
		for id := range dragged {
			clearNodeTransform(nodeElementID(canvasID, id))
		}
		redraw()
	})
	wheelOn := ui.UseEvent(func(event ui.MouseEvent) {
		dx, dy, x, y := wheelDeltas(canvasID, event)
		event.PreventDefault()
		canvas.Wheel(WheelEvent{DeltaX: dx, DeltaY: dy, X: x, Y: y})
	})
	keyOn := ui.UseEvent(func(event ui.KeyboardEvent) {
		key := event.GetKey()
		if key == "ArrowLeft" || key == "ArrowRight" || key == "ArrowUp" || key == "ArrowDown" {
			event.PreventDefault()
		}
		canvas.Key(KeyEvent{Key: key})
	})

	viewportStyle := map[string]string{
		"transform":        canvas.Viewport.Transform(),
		"transform-origin": "0 0",
	}
	edgeNodes := make([]ui.Node, 0, len(canvas.Edges)+1)
	for _, edge := range canvas.Edges {
		path, ok := EdgePath(Graph{Nodes: canvas.Nodes}, edge)
		if !ok {
			continue
		}
		edgeNodes = append(edgeNodes, html.Path(html.Props{
			ID: canvasID + "-edge-" + edge.ID, Class: "gc-edge", Data: map[string]string{"edge": edge.ID},
			Raw: map[string]any{"d": path},
		}))
	}
	if canvas.DraftActive {
		if from, ok := draftFrom(canvas); ok {
			to := canvas.DraftPoint
			if canvas.EdgeDraft.ToNode != "" {
				if n, ok := (Graph{Nodes: canvas.Nodes}).NodeByID(canvas.EdgeDraft.ToNode); ok {
					if p, ok := PortByID(n, canvas.EdgeDraft.ToPort); ok {
						to = n.PortPosition(p)
					}
				}
			}
			edgeNodes = append(edgeNodes, html.Path(html.Props{
				Class: "gc-edge gc-draft",
				Raw:   map[string]any{"d": bezierPath(from, Point{X: 1, Y: 0}, to, Point{X: -1, Y: 0})},
			}))
		}
	}

	nodeNodes := make([]ui.Node, 0, len(canvas.Nodes))
	for _, node := range canvas.Nodes {
		node := node
		selected := canvas.Selection[node.ID]
		class := ClassNode
		label := copy.NodeLabel(node.Label)
		if selected {
			class += " " + ClassSelected
			label = copy.SelectedLabel(node.Label)
		}
		ports := make([]ui.Node, 0, len(node.Ports))
		for _, port := range node.Ports {
			port := port
			at := node.PortPosition(port)
			ports = append(ports, html.Span(html.Props{
				Class: ClassPort + " gc-port-" + string(port.Side),
				Style: map[string]string{
					"left": formatPx(at.X - node.X),
					"top":  formatPx(at.Y - node.Y),
				},
				Role:          "button",
				Aria:          map[string]string{"label": copy.PortLabel(node.Label, port.Label)},
				Data:          map[string]string{"node": node.ID, "port": port.ID},
				OnPointerDown: downOn(TargetPort, node.ID, port.ID),
			}))
		}
		children := append([]ui.Node{html.Span(html.Props{Class: "gc-label"}, ui.Text(node.Label))}, ports...)
		nodeNodes = append(nodeNodes, html.Div(html.Props{
			ID:    nodeElementID(canvasID, node.ID),
			Class: class,
			Style: map[string]string{
				"left":   formatPx(node.X),
				"top":    formatPx(node.Y),
				"width":  formatPx(node.W),
				"height": formatPx(node.H),
			},
			Role:          "button",
			TabIndex:      -1,
			Aria:          map[string]string{"label": label, "pressed": formatBool(selected)},
			Data:          map[string]string{"node": node.ID},
			OnPointerDown: downOn(TargetNode, node.ID, ""),
		}, children...))
	}

	layer := []ui.Node{
		html.Svg(html.Props{Class: ClassEdges, Aria: map[string]string{"hidden": "true"}}, edgeNodes...),
	}
	layer = append(layer, nodeNodes...)
	if canvas.MarqueeActive {
		r := canvas.Marquee
		layer = append(layer, html.Div(html.Props{
			Class: ClassMarquee,
			Style: map[string]string{
				"left":   formatPx(min(r.X, r.X+r.W)),
				"top":    formatPx(min(r.Y, r.Y+r.H)),
				"width":  formatPx(abs(r.W)),
				"height": formatPx(abs(r.H)),
			},
		}))
	}

	toolbar := html.Div(html.Props{Class: ClassToolbar, Role: "toolbar", Aria: map[string]string{"label": copy.CanvasLabel}},
		html.Button(html.Props{Aria: map[string]string{"label": copy.ZoomInLabel}, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			canvas.Key(KeyEvent{Key: "+"})
		})}, ui.Text("+")),
		html.Button(html.Props{Aria: map[string]string{"label": copy.ZoomOutLabel}, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			canvas.Key(KeyEvent{Key: "-"})
		})}, ui.Text("-")),
		html.Button(html.Props{Aria: map[string]string{"label": copy.ResetLabel}, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			canvas.Key(KeyEvent{Key: "0"})
		})}, ui.Text(copy.ResetLabel)),
	)

	root := []ui.Node{
		html.Div(html.Props{
			Class:         ClassViewport,
			Style:         viewportStyle,
			OnPointerDown: downOn(TargetCanvas, "", ""),
			OnPointerMove: moveOn,
			OnPointerUp:   upOn,
			OnWheel:       wheelOn,
		}, layer...),
		toolbar,
	}
	if props.Minimap {
		root = append(root, minimap(canvasID, canvas, copy))
	}
	if len(canvas.Nodes) == 0 {
		root = append(root, html.Div(html.Props{Class: "gc-empty"}, ui.Text(copy.EmptyLabel)))
	}
	// Pointer-cancel is not expressible in declarative props: hosts that
	// need it wire ui.UsePointerEvents onto the canvas element and call the
	// machine's PointerCancel, which abandons the gesture without commit.
	return html.Div(html.Props{
		ID:        canvasID,
		Class:     ClassCanvas,
		Role:      "group",
		Aria:      map[string]string{"label": copy.CanvasLabel, "roledescription": "graph canvas"},
		TabIndex:  html.TabIndexZero,
		Data:      map[string]string{"locale": props.Locale, "dir": DirFor(props.Locale)},
		Raw:       map[string]any{"dir": DirFor(props.Locale)},
		OnKeyDown: keyOn,
	}, root...)
}

// draftFrom resolves the draft edge's start port position.
func draftFrom(canvas *Machine) (Point, bool) {
	graph := Graph{Nodes: canvas.Nodes}
	from, ok := graph.NodeByID(canvas.EdgeDraft.FromNode)
	if !ok {
		return Point{}, false
	}
	port, ok := PortByID(from, canvas.EdgeDraft.FromPort)
	if !ok {
		return Point{}, false
	}
	return from.PortPosition(port), true
}

// moveTarget maps a hit candidate to its pointer target.
func moveTarget(nodeID, portID string) TargetKind {
	if portID != "" {
		return TargetPort
	}
	if nodeID != "" {
		return TargetNode
	}
	return TargetCanvas
}

// candidatesOf is the move-time hit probe. Coordinates come from the DOM
// event; the machine needs the target under the cursor for port drafts.
func candidatesOf(canvas *Machine, x, y float64) (string, string) {
	world := canvas.Viewport.ScreenToWorld(Point{X: x, Y: y})
	if n, p, ok := HitPort(canvas.Nodes, world, 8); ok {
		return n.ID, p.ID
	}
	if n, ok := HitNode(canvas.Nodes, world); ok {
		return n.ID, ""
	}
	return "", ""
}

// minimap renders the scaled content overview with the viewport rect. A
// click recenters the canvas on the clicked world point.
func minimap(canvasID string, canvas *Machine, copy CanvasCopy) ui.Node {
	const width, height = 200, 120
	bounds, ok := ContentBounds(canvas.Nodes)
	fit := FitMinimap(bounds, ok, width, height)
	rects := make([]ui.Node, 0, len(canvas.Nodes)+1)
	if ok {
		for _, node := range canvas.Nodes {
			at := fit.Project(Point{X: node.X, Y: node.Y}, bounds)
			rects = append(rects, html.Tag("rect", html.Props{Raw: map[string]any{
				"x": at.X, "y": at.Y,
				"width":  node.W * fit.Scale,
				"height": node.H * fit.Scale,
				"class":  "gc-mini-node",
			}}))
		}
		view := fit.ViewportRect(canvas.Viewport, 800, 600, bounds)
		rects = append(rects, html.Tag("rect", html.Props{Raw: map[string]any{
			"x": view.X, "y": view.Y, "width": view.W, "height": view.H,
			"class": "gc-mini-view",
		}}))
	}
	return html.Div(html.Props{
		Class: ClassMinimap, Role: "img", Aria: map[string]string{"label": copy.MinimapLabel},
		Data: map[string]string{"minimap": canvasID},
		OnClick: ui.UseEvent(func(event ui.MouseEvent) {
			ox, oy := clickOffset(canvasID, event)
			world := Point{X: bounds.X + (ox-fit.OffsetX)/fit.Scale, Y: bounds.Y + (oy-fit.OffsetY)/fit.Scale}
			canvas.CenterOn(world, 800, 600)
		}),
	}, html.Svg(html.Props{Raw: map[string]any{"width": width, "height": height}}, rects...))
}

func formatBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// Stylesheet returns the canvas stylesheet, built on the repo's surface and
// logical-property conventions: chrome spacing uses logical properties so
// RTL locales mirror; canvas geometry stays physical.
func Stylesheet() string {
	return `:root{}
.gc-canvas{position:relative;overflow:hidden;inline-size:100%;block-size:100%;background:var(--surface-canvas,var(--surface,#fff));color:var(--on-surface,#111)}
.gc-viewport{position:absolute;inset-block-start:0;inset-inline-start:0;transform-origin:0 0}
.gc-edges{position:absolute;inset-block-start:0;inset-inline-start:0;overflow:visible;pointer-events:none}
.gc-edge{fill:none;stroke:var(--graph-edge,#5b6b7b);stroke-width:2}
.gc-edge.gc-draft{stroke-dasharray:6 4}
.gc-node{position:absolute;background:var(--surface-node,#fff);border:1px solid var(--border,#c8d0da);border-radius:8px;padding-block:8px;padding-inline:12px;cursor:grab;box-sizing:border-box;user-select:none}
.gc-node.gc-selected{border-color:var(--accent,#2563eb);box-shadow:0 0 0 2px var(--accent-soft,rgba(37,99,235,.25))}
.gc-label{font:inherit;pointer-events:none}
.gc-port{position:absolute;inline-size:12px;block-size:12px;margin-inline-start:-6px;margin-block-start:-6px;border-radius:50%;background:var(--surface-node,#fff);border:2px solid var(--accent,#2563eb);cursor:crosshair}
.gc-marquee{position:absolute;border:1px dashed var(--accent,#2563eb);background:var(--accent-soft,rgba(37,99,235,.12));pointer-events:none}
.gc-toolbar{position:absolute;inset-block-start:8px;inset-inline-end:8px;display:flex;gap:4px}
.gc-minimap{position:absolute;inset-block-end:8px;inset-inline-end:8px;border:1px solid var(--border,#c8d0da);background:var(--surface-node,#fff);cursor:pointer}
.gc-mini-node{fill:var(--graph-edge,#5b6b7b)}
.gc-mini-view{fill:none;stroke:var(--accent,#2563eb);stroke-width:1}
.gc-empty{position:absolute;inset-block-start:16px;inset-inline-start:16px;color:var(--on-surface-muted,#666)}
`
}
