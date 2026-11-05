package graphcanvas

import "sort"

// Canvas geometry limits. Zoom is a scale factor, clamped so one wheel
// gesture can neither lose the graph nor explode it.
const (
	MinZoom = 0.2
	MaxZoom = 3.0
)

// Point is a 2D coordinate in either screen (client pixels) or world
// (canvas units) space; the Viewport converts between them.
type Point struct {
	X, Y float64
}

// Rect is an axis-aligned rectangle in world space.
type Rect struct {
	X, Y, W, H float64
}

// Contains reports whether p falls inside the rect, edges inclusive.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X <= r.X+r.W && p.Y >= r.Y && p.Y <= r.Y+r.H
}

// Intersects reports whether two rects overlap.
func (r Rect) Intersects(o Rect) bool {
	return r.X <= o.X+o.W && o.X <= r.X+r.W && r.Y <= o.Y+o.H && o.Y <= r.Y+r.H
}

// Viewport is the camera: a pan offset in screen pixels and a zoom scale.
// WorldToScreen and ScreenToWorld are exact inverses.
type Viewport struct {
	PanX, PanY float64
	Zoom       float64
}

// Normalized clamps the zoom into range; a zero zoom reads as 1.
func (v Viewport) Normalized() Viewport {
	if v.Zoom == 0 {
		v.Zoom = 1
	}
	if v.Zoom < MinZoom {
		v.Zoom = MinZoom
	}
	if v.Zoom > MaxZoom {
		v.Zoom = MaxZoom
	}
	return v
}

// WorldToScreen maps world units to client pixels.
func (v Viewport) WorldToScreen(p Point) Point {
	v = v.Normalized()
	return Point{X: p.X*v.Zoom + v.PanX, Y: p.Y*v.Zoom + v.PanY}
}

// ScreenToWorld maps client pixels to world units.
func (v Viewport) ScreenToWorld(p Point) Point {
	v = v.Normalized()
	return Point{X: (p.X - v.PanX) / v.Zoom, Y: (p.Y - v.PanY) / v.Zoom}
}

// ZoomAt returns the viewport zoomed by factor around a screen anchor: the
// world point under the cursor stays under the cursor.
func (v Viewport) ZoomAt(anchor Point, factor float64) Viewport {
	v = v.Normalized()
	world := v.ScreenToWorld(anchor)
	v.Zoom *= factor
	v = v.Normalized()
	v.PanX = anchor.X - world.X*v.Zoom
	v.PanY = anchor.Y - world.Y*v.Zoom
	return v
}

// Transform renders the container's CSS transform for the viewport.
func (v Viewport) Transform() string {
	v = v.Normalized()
	return "translate(" + formatPx(v.PanX) + "," + formatPx(v.PanY) + ") scale(" + formatNum(v.Zoom) + ")"
}

// PortSide names which node edge a port sits on.
type PortSide string

const (
	PortNorth PortSide = "N"
	PortSouth PortSide = "S"
	PortEast  PortSide = "E"
	PortWest  PortSide = "W"
)

// Port is one connectable node endpoint. Offset runs 0..1 along its side.
type Port struct {
	ID     string
	Side   PortSide
	Offset float64
	Label  string
}

// Node is one labeled box with ports. Position is the top-left corner in
// world units.
type Node struct {
	ID    string
	Label string
	X, Y  float64
	W, H  float64
	Ports []Port
}

// Bounds returns the node's world rect.
func (n Node) Bounds() Rect { return Rect{X: n.X, Y: n.Y, W: n.W, H: n.H} }

// PortPosition returns a port's world position.
func (n Node) PortPosition(port Port) Point {
	switch port.Side {
	case PortNorth:
		return Point{X: n.X + port.Offset*n.W, Y: n.Y}
	case PortSouth:
		return Point{X: n.X + port.Offset*n.W, Y: n.Y + n.H}
	case PortEast:
		return Point{X: n.X + n.W, Y: n.Y + port.Offset*n.H}
	default: // PortWest and unknown read as west: fail visible, not silent.
		return Point{X: n.X, Y: n.Y + port.Offset*n.H}
	}
}

// Edge connects two ports. It carries no semantics beyond its endpoints.
type Edge struct {
	ID       string
	FromNode string
	FromPort string
	ToNode   string
	ToPort   string
}

// Graph is the canvas content: nodes in stable order plus edges.
type Graph struct {
	Nodes []Node
	Edges []Edge
}

// NodeByID resolves a node by id.
func (g Graph) NodeByID(id string) (Node, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// PortByID resolves a node's port by id.
func PortByID(n Node, id string) (Port, bool) {
	for _, p := range n.Ports {
		if p.ID == id {
			return p, true
		}
	}
	return Port{}, false
}

// EdgePath renders one edge as a cubic bezier between its port positions,
// leaving along the port side's outward normal. It returns false when an
// endpoint does not resolve: a dangling edge draws nothing rather than a
// line to nowhere.
func EdgePath(g Graph, e Edge) (string, bool) {
	fromNode, ok := g.NodeByID(e.FromNode)
	if !ok {
		return "", false
	}
	toNode, ok := g.NodeByID(e.ToNode)
	if !ok {
		return "", false
	}
	fromPort, ok := PortByID(fromNode, e.FromPort)
	if !ok {
		return "", false
	}
	toPort, ok := PortByID(toNode, e.ToPort)
	if !ok {
		return "", false
	}
	return bezierPath(fromNode.PortPosition(fromPort), portNormal(fromPort.Side), toNode.PortPosition(toPort), portNormal(toPort.Side)), true
}

func portNormal(side PortSide) Point {
	switch side {
	case PortNorth:
		return Point{X: 0, Y: -1}
	case PortSouth:
		return Point{X: 0, Y: 1}
	case PortEast:
		return Point{X: 1, Y: 0}
	default:
		return Point{X: -1, Y: 0}
	}
}

func bezierPath(a Point, aNormal Point, b Point, bNormal Point) string {
	span := abs(a.X-b.X) + abs(a.Y-b.Y)
	bend := span / 2
	if bend < 24 {
		bend = 24
	}
	c1 := Point{X: a.X + aNormal.X*bend, Y: a.Y + aNormal.Y*bend}
	c2 := Point{X: b.X + bNormal.X*bend, Y: b.Y + bNormal.Y*bend}
	return "M" + formatNum(a.X) + " " + formatNum(a.Y) +
		" C" + formatNum(c1.X) + " " + formatNum(c1.Y) +
		" " + formatNum(c2.X) + " " + formatNum(c2.Y) +
		" " + formatNum(b.X) + " " + formatNum(b.Y)
}

// HitNode returns the topmost node (last in order) containing a world point.
func HitNode(nodes []Node, p Point) (Node, bool) {
	for i := len(nodes) - 1; i >= 0; i-- {
		if nodes[i].Bounds().Contains(p) {
			return nodes[i], true
		}
	}
	return Node{}, false
}

// HitPort returns the node port within radius (world units) of a point,
// preferring the closest. Zero or negative radius reads as 8.
func HitPort(nodes []Node, p Point, radius float64) (Node, Port, bool) {
	if radius <= 0 {
		radius = 8
	}
	best := radius
	var bestNode Node
	var bestPort Port
	found := false
	for _, n := range nodes {
		for _, port := range n.Ports {
			at := n.PortPosition(port)
			if d := dist(p, at); d <= best {
				best, bestNode, bestPort, found = d, n, port, true
			}
		}
	}
	return bestNode, bestPort, found
}

// MarqueeSelect returns the ids of nodes intersecting a world rect, in node
// order. The rect normalizes itself, so any drag direction works.
func MarqueeSelect(nodes []Node, rect Rect) []string {
	normalized := Rect{X: rect.X, Y: rect.Y, W: rect.W, H: rect.H}
	if normalized.W < 0 {
		normalized.X += normalized.W
		normalized.W = -normalized.W
	}
	if normalized.H < 0 {
		normalized.Y += normalized.H
		normalized.H = -normalized.H
	}
	var out []string
	for _, n := range nodes {
		if n.Bounds().Intersects(normalized) {
			out = append(out, n.ID)
		}
	}
	return out
}

// Snap rounds v to the grid; a non-positive grid disables snapping.
func Snap(v, grid float64) float64 {
	if grid <= 0 {
		return v
	}
	q := v / grid
	if q >= 0 {
		return float64(int64(q+0.5)) * grid
	}
	return float64(int64(q-0.5)) * grid
}

// ContentBounds returns the world rect enclosing every node, or false when
// the graph is empty.
func ContentBounds(nodes []Node) (Rect, bool) {
	if len(nodes) == 0 {
		return Rect{}, false
	}
	bounds := nodes[0].Bounds()
	for _, n := range nodes[1:] {
		b := n.Bounds()
		x0 := min(bounds.X, b.X)
		y0 := min(bounds.Y, b.Y)
		x1 := max(bounds.X+bounds.W, b.X+b.W)
		y1 := max(bounds.Y+bounds.H, b.Y+b.H)
		bounds = Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
	}
	return bounds, true
}

// Minimap maps content bounds into a fixed-size minimap: the scale that fits
// the content plus the offset centering it.
type Minimap struct {
	Scale    float64
	OffsetX  float64
	OffsetY  float64
	Width    float64
	Height   float64
	HasGraph bool
}

// FitMinimap fits content into width by height. Empty content yields no
// graph; degenerate bounds still fit.
func FitMinimap(bounds Rect, hasGraph bool, width, height float64) Minimap {
	m := Minimap{Width: width, Height: height, HasGraph: hasGraph}
	if !hasGraph {
		return m
	}
	w, h := bounds.W, bounds.H
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	m.Scale = min(width/w, height/h)
	m.OffsetX = (width - bounds.W*m.Scale) / 2
	m.OffsetY = (height - bounds.H*m.Scale) / 2
	return m
}

// Project maps a world point into minimap pixels.
func (m Minimap) Project(p Point, bounds Rect) Point {
	return Point{X: (p.X-bounds.X)*m.Scale + m.OffsetX, Y: (p.Y-bounds.Y)*m.Scale + m.OffsetY}
}

// ViewportRect projects the visible world rect into minimap pixels.
func (m Minimap) ViewportRect(view Viewport, screenW, screenH float64, bounds Rect) Rect {
	topLeft := view.ScreenToWorld(Point{X: 0, Y: 0})
	bottomRight := view.ScreenToWorld(Point{X: screenW, Y: screenH})
	a := m.Project(topLeft, bounds)
	b := m.Project(bottomRight, bounds)
	return Rect{X: a.X, Y: a.Y, W: b.X - a.X, H: b.Y - a.Y}
}

// SortedIDs returns selection ids in stable order.
func SortedIDs(ids map[string]bool) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func dist(a, b Point) float64 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return sqrt(dx*dx + dy*dy)
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
