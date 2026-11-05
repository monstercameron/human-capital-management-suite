package graphcanvas

// Pointer and wheel input: normalized from DOM events by the component, so
// the state machine below is pure Go and unit-testable without a browser.

// PointerKind names the pointer phase.
type PointerKind string

const (
	PointerDown   PointerKind = "down"
	PointerMove   PointerKind = "move"
	PointerUp     PointerKind = "up"
	PointerCancel PointerKind = "cancel"
)

// TargetKind names what a pointer press landed on.
type TargetKind string

const (
	TargetCanvas  TargetKind = "canvas"
	TargetNode    TargetKind = "node"
	TargetPort    TargetKind = "port"
	TargetMinimap TargetKind = "minimap"
)

// PointerEvent is one normalized pointer phase with client-pixel coordinates.
type PointerEvent struct {
	Kind     PointerKind
	X, Y     float64
	Button   int // 0 is the primary button; others never start a gesture.
	Shift    bool
	Target   TargetKind
	NodeID   string
	PortID   string
	Minimap  Point // world point under a minimap press, precomputed by render.
	HasWorld bool
	World    Point // world point under the press, when the caller knows it.
}

// WheelEvent is one normalized wheel gesture in pixel deltas around a
// client-pixel anchor.
type WheelEvent struct {
	DeltaX, DeltaY float64
	X, Y           float64
}

// KeyEvent is one normalized key press.
type KeyEvent struct {
	Key   string
	Shift bool
}

// ChangeKind names one committed state change.
type ChangeKind string

const (
	ChangeNodesMoved ChangeKind = "nodes-moved"
	ChangeViewport   ChangeKind = "viewport"
	ChangeSelection  ChangeKind = "selection"
	ChangeEdgeAdded  ChangeKind = "edge-added"
	ChangeEdgeDraft  ChangeKind = "edge-draft"
	ChangeMarquee    ChangeKind = "marquee"
)

// Change is one committed change delivered to OnChange.
type Change struct {
	Kind   ChangeKind
	NodeID string
	Edge   Edge
}

// DragMode names the in-progress gesture. The zero value is DragNone, so a
// zero Canvas starts idle.
type DragMode string

const (
	DragNone    DragMode = ""
	DragNode    DragMode = "node"
	DragPan     DragMode = "pan"
	DragMarquee DragMode = "marquee"
	DragPort    DragMode = "port"
)

// dragThresholdPx is how far a press must travel before it becomes a drag;
// below it, the gesture is a click.
const dragThresholdPx = 3

// Machine is the interaction state machine: viewport, nodes, edges, selection
// and the in-progress gesture. Node moves preview through DragPreview (the
// component paints them as direct DOM transforms) and commit exactly once on
// drop; pan, zoom, marquee and keyboard moves commit live.
type Machine struct {
	Viewport  Viewport
	Nodes     []Node
	Edges     []Edge
	Selection map[string]bool
	Grid      float64
	OnChange  func(Change)

	MarqueeActive bool
	Marquee       Rect
	EdgeDraft     Edge
	DraftActive   bool
	// DraftPoint is the cursor in world units during a port drag, so the
	// draft edge draws to the cursor until it lands on a port.
	DraftPoint Point

	drag      DragMode
	pressX    float64
	pressY    float64
	nodeID    string
	portID    string
	nodeStart map[string]Point
	preview   map[string]Point
	moved     bool
	commits   int
	index     map[string]int
}

// NewMachine builds a canvas over a graph copy.
func NewMachine(graph Graph, viewport Viewport, grid float64) *Machine {
	c := &Machine{
		Viewport: viewport.Normalized(), Grid: grid,
		Selection: map[string]bool{}, nodeStart: map[string]Point{},
	}
	c.Nodes = append([]Node(nil), graph.Nodes...)
	c.Edges = append([]Edge(nil), graph.Edges...)
	c.reindex()
	return c
}

func (c *Machine) reindex() {
	c.index = make(map[string]int, len(c.Nodes))
	for i, n := range c.Nodes {
		c.index[n.ID] = i
	}
}

// Commits counts state commits delivered through OnChange.
func (c *Machine) Commits() int { return c.commits }

func (c *Machine) emit(kind ChangeKind, nodeID string, edge Edge) {
	c.commits++
	if c.OnChange != nil {
		c.OnChange(Change{Kind: kind, NodeID: nodeID, Edge: edge})
	}
}

// SelectedIDs returns the selection in stable order.
func (c *Machine) SelectedIDs() []string { return SortedIDs(c.Selection) }

// DragPreview returns the dragged nodes' live offsets in world units while a
// node drag is past threshold, for direct DOM transforms. Nothing is
// committed: the preview evaporates if the gesture cancels.
func (c *Machine) DragPreview() (map[string]Point, bool) {
	if c.drag != DragNode || !c.moved {
		return nil, false
	}
	out := make(map[string]Point, len(c.nodeStart))
	for id := range c.nodeStart {
		if _, ok := c.index[id]; !ok {
			continue
		}
		out[id] = c.preview[id]
	}
	return out, true
}

// PointerDown begins a gesture.
func (c *Machine) PointerDown(e PointerEvent) {
	if e.Button != 0 {
		return
	}
	c.pressX, c.pressY = e.X, e.Y
	c.moved = false
	switch e.Target {
	case TargetNode:
		if e.Shift {
			if c.Selection[e.NodeID] {
				delete(c.Selection, e.NodeID)
			} else {
				c.Selection[e.NodeID] = true
			}
			c.emit(ChangeSelection, e.NodeID, Edge{})
		} else if !c.Selection[e.NodeID] {
			c.Selection = map[string]bool{e.NodeID: true}
			c.emit(ChangeSelection, e.NodeID, Edge{})
		}
		c.drag, c.nodeID = DragNode, e.NodeID
		c.nodeStart = map[string]Point{}
		for id := range c.Selection {
			if i, ok := c.index[id]; ok {
				c.nodeStart[id] = Point{X: c.Nodes[i].X, Y: c.Nodes[i].Y}
			}
		}
		c.preview = map[string]Point{}
	case TargetPort:
		c.drag, c.nodeID, c.portID = DragPort, e.NodeID, e.PortID
		c.EdgeDraft = Edge{FromNode: e.NodeID, FromPort: e.PortID}
		c.DraftActive = false
	case TargetMinimap:
		world := e.Minimap
		c.centerOn(world)
	case TargetCanvas:
		if e.Shift {
			c.drag = DragMarquee
			world := c.Viewport.ScreenToWorld(Point{X: e.X, Y: e.Y})
			c.Marquee = Rect{X: world.X, Y: world.Y}
			c.MarqueeActive = false
		} else {
			c.drag = DragPan
		}
	}
}

// PointerMove advances the gesture. Node moves preview only: state commits
// once on drop.
func (c *Machine) PointerMove(e PointerEvent) {
	if c.drag == DragNone {
		return
	}
	if abs(e.X-c.pressX) >= dragThresholdPx || abs(e.Y-c.pressY) >= dragThresholdPx {
		c.moved = true
	}
	switch c.drag {
	case DragNode:
		if !c.moved {
			return
		}
		dx := (e.X - c.pressX) / c.Viewport.Normalized().Zoom
		dy := (e.Y - c.pressY) / c.Viewport.Normalized().Zoom
		for id := range c.nodeStart {
			c.preview[id] = Point{X: dx, Y: dy}
		}
	case DragPan:
		if !c.moved {
			return
		}
		c.Viewport.PanX += e.X - c.pressX
		c.Viewport.PanY += e.Y - c.pressY
		c.pressX, c.pressY = e.X, e.Y
		c.emit(ChangeViewport, "", Edge{})
	case DragMarquee:
		if !c.moved {
			return
		}
		world := c.Viewport.ScreenToWorld(Point{X: e.X, Y: e.Y})
		c.Marquee.W = world.X - c.Marquee.X
		c.Marquee.H = world.Y - c.Marquee.Y
		c.MarqueeActive = true
		c.emit(ChangeMarquee, "", Edge{})
	case DragPort:
		if !c.moved {
			return
		}
		c.DraftActive = true
		c.DraftPoint = c.Viewport.ScreenToWorld(Point{X: e.X, Y: e.Y})
		c.EdgeDraft.ToNode, c.EdgeDraft.ToPort = "", ""
		if e.Target == TargetPort && e.NodeID != "" {
			c.EdgeDraft.ToNode, c.EdgeDraft.ToPort = e.NodeID, e.PortID
		}
		c.emit(ChangeEdgeDraft, "", c.EdgeDraft)
	}
}

// PointerUp ends the gesture, committing exactly once for node moves,
// marquees and completed connections.
func (c *Machine) PointerUp(e PointerEvent) {
	defer c.endGesture()
	switch c.drag {
	case DragNode:
		if !c.moved {
			// A click without shift collapses a multi-selection to the node.
			if !e.Shift && len(c.Selection) > 1 {
				c.Selection = map[string]bool{c.nodeID: true}
				c.emit(ChangeSelection, c.nodeID, Edge{})
			}
			return
		}
		for id, start := range c.nodeStart {
			i, ok := c.index[id]
			if !ok {
				continue
			}
			c.Nodes[i].X = Snap(start.X+c.preview[id].X, c.Grid)
			c.Nodes[i].Y = Snap(start.Y+c.preview[id].Y, c.Grid)
		}
		c.emit(ChangeNodesMoved, c.nodeID, Edge{})
	case DragMarquee:
		if !c.moved {
			c.Selection = map[string]bool{}
			c.emit(ChangeSelection, "", Edge{})
			return
		}
		ids := MarqueeSelect(c.Nodes, c.Marquee)
		if e.Shift {
			for _, id := range ids {
				c.Selection[id] = true
			}
		} else {
			c.Selection = map[string]bool{}
			for _, id := range ids {
				c.Selection[id] = true
			}
		}
		c.emit(ChangeSelection, "", Edge{})
	case DragPort:
		if !c.moved || c.EdgeDraft.ToNode == "" {
			return
		}
		if c.EdgeDraft.FromNode == c.EdgeDraft.ToNode && c.EdgeDraft.FromPort == c.EdgeDraft.ToPort {
			return
		}
		c.EdgeDraft.ID = "edge-" + c.EdgeDraft.FromNode + "-" + c.EdgeDraft.FromPort + "-" + c.EdgeDraft.ToNode + "-" + c.EdgeDraft.ToPort
		c.Edges = append(c.Edges, c.EdgeDraft)
		c.emit(ChangeEdgeAdded, "", c.EdgeDraft)
	case DragPan:
		if !c.moved && e.Target == TargetCanvas && !e.Shift {
			if len(c.Selection) > 0 {
				c.Selection = map[string]bool{}
				c.emit(ChangeSelection, "", Edge{})
			}
		}
	}
}

// PointerCancel abandons the gesture with no commit.
func (c *Machine) PointerCancel() { c.endGesture() }

func (c *Machine) endGesture() {
	c.drag = DragNone
	c.moved = false
	c.nodeID, c.portID = "", ""
	c.nodeStart = map[string]Point{}
	c.preview = map[string]Point{}
	c.MarqueeActive = false
	c.DraftActive = false
}

// Wheel zooms around the cursor anchor.
func (c *Machine) Wheel(e WheelEvent) {
	factor := 1 - e.DeltaY/500
	if factor <= 0 {
		factor = 0.1
	}
	before := c.Viewport
	c.Viewport = c.Viewport.ZoomAt(Point{X: e.X, Y: e.Y}, factor)
	if c.Viewport != before {
		c.emit(ChangeViewport, "", Edge{})
	}
}

// centerOn pans the viewport so a world point sits centered in a viewport of
// the given screen size.
func (c *Machine) centerOn(world Point) {
	c.Viewport.PanX = -world.X * c.Viewport.Normalized().Zoom
	c.Viewport.PanY = -world.Y * c.Viewport.Normalized().Zoom
	c.emit(ChangeViewport, "", Edge{})
}

// CenterOn re-centers the minimap target for a known screen size.
func (c *Machine) CenterOn(world Point, screenW, screenH float64) {
	z := c.Viewport.Normalized().Zoom
	c.Viewport.PanX = screenW/2 - world.X*z
	c.Viewport.PanY = screenH/2 - world.Y*z
	c.emit(ChangeViewport, "", Edge{})
}

// Key handles keyboard graph operation: arrows move the selection visually,
// Escape clears selection or cancels the gesture, + and - zoom about the
// origin, 0 resets the viewport. Arrow directions are visual and identical
// under LTR and RTL: the canvas is a spatial surface, not text.
func (c *Machine) Key(e KeyEvent) {
	step := c.Grid
	if step <= 0 {
		step = 8
	}
	switch e.Key {
	case "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown":
		if len(c.Selection) == 0 {
			return
		}
		dx, dy := 0.0, 0.0
		switch e.Key {
		case "ArrowLeft":
			dx = -step
		case "ArrowRight":
			dx = step
		case "ArrowUp":
			dy = -step
		case "ArrowDown":
			dy = step
		}
		ids := c.SelectedIDs()
		for _, id := range ids {
			i, ok := c.index[id]
			if !ok {
				continue
			}
			c.Nodes[i].X = Snap(c.Nodes[i].X+dx, c.Grid)
			c.Nodes[i].Y = Snap(c.Nodes[i].Y+dy, c.Grid)
		}
		c.emit(ChangeNodesMoved, ids[0], Edge{})
	case "Escape":
		if c.drag != DragNone {
			c.endGesture()
			return
		}
		if len(c.Selection) > 0 {
			c.Selection = map[string]bool{}
			c.emit(ChangeSelection, "", Edge{})
		}
	case "+", "=":
		c.Viewport = c.Viewport.ZoomAt(Point{X: 0, Y: 0}, 1.2)
		c.emit(ChangeViewport, "", Edge{})
	case "-", "_":
		c.Viewport = c.Viewport.ZoomAt(Point{X: 0, Y: 0}, 1/1.2)
		c.emit(ChangeViewport, "", Edge{})
	case "0":
		c.Viewport = Viewport{Zoom: 1}
		c.emit(ChangeViewport, "", Edge{})
	}
}
