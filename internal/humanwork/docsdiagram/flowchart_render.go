package docsdiagram

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// rctx carries what every renderer needs besides its statements.
type rctx struct {
	loc locale
	id  string
}

// fcGeom is a node's drawn geometry in screen coordinates.
type fcGeom struct {
	cx, cy, w, h float64
	lines        []string
}

type flowDrawing struct {
	fc             *flowchart
	geom           []fcGeom
	out            *layoutOut
	lr             bool // rank axis is horizontal
	flip           bool // BT or RL
	width          float64
	height         float64
	edgeLabelLines [][]string
	edgeLabelSize  []point
}

func renderFlowchart(header srcLine, rest string, body []srcLine, rc rctx) (*drawing, error) {
	fc, err := parseFlowchart(header, rest, body)
	if err != nil {
		return nil, err
	}
	if len(fc.nodes) == 0 {
		return nil, ErrEmpty
	}
	fd, err := layoutFlowchart(fc)
	if err != nil {
		return nil, err
	}
	return fd.draw(rc), nil
}

func nodeSize(shape fcShape, lines []string) (float64, float64) {
	tw := maxLineWidth(lines, nodeFont)
	th := float64(len(lines)) * nodeLine
	w := math.Max(tw+28, 56)
	h := math.Max(th+18, 38)
	switch shape {
	case shapeStadium:
		w += h / 2
	case shapeSubroutine:
		w += 16
	case shapeCylinder:
		h += 16
	case shapeCircle, shapeDoubleCircle:
		d := math.Max(math.Hypot(tw+16, th+10), 48)
		if shape == shapeDoubleCircle {
			d += 8
		}
		w, h = d, d
	case shapeDiamond:
		p, q := (tw+12)/2, (th+6)/2
		w, h = math.Max(2*1.8*p, 56), math.Max(2*2.25*q, 44)
	case shapeHexagon, shapeLeanRight, shapeLeanLeft, shapeTrapezoid, shapeTrapezoidAlt:
		w += h / 2
	case shapeAsym:
		w += h / 4
	}
	return w, h
}

func layoutFlowchart(fc *flowchart) (*flowDrawing, error) {
	fd := &flowDrawing{fc: fc, lr: fc.dir == "LR" || fc.dir == "RL", flip: fc.dir == "BT" || fc.dir == "RL"}
	in := layoutInput{titleAtStart: fc.dir == "TB", titleAtEnd: fc.dir == "BT", titleAcross: fd.lr}
	fd.geom = make([]fcGeom, len(fc.nodes))
	selfLoop := make([]bool, len(fc.nodes))
	for _, e := range fc.edges {
		if e.from == e.to && e.stroke != strokeInvisible {
			selfLoop[e.from] = true
		}
	}
	for i, n := range fc.nodes {
		lines := wrapLabel(n.label, wrapChars)
		w, h := nodeSize(n.shape, lines)
		fd.geom[i] = fcGeom{w: w, h: h, lines: lines}
		ln := lnode{ow: w, rh: h, path: fc.groupPath(n.group), selfLoop: selfLoop[i]}
		if fd.lr {
			ln.ow, ln.rh = h, w
		}
		in.nodes = append(in.nodes, ln)
	}
	fd.edgeLabelLines = make([][]string, len(fc.edges))
	fd.edgeLabelSize = make([]point, len(fc.edges))
	for i, e := range fc.edges {
		le := ledge{from: e.from, to: e.to, minlen: e.minlen}
		if e.label != "" && e.stroke != strokeInvisible {
			lines := wrapLabel(e.label, 24)
			lw := maxLineWidth(lines, smallFont) + 12
			lh := float64(len(lines))*smallLine + 6
			fd.edgeLabelLines[i] = lines
			fd.edgeLabelSize[i] = point{lw, lh}
			le.hasLabel = true
			le.labelOW, le.labelRH = lw, lh
			if fd.lr {
				le.labelOW, le.labelRH = lh, lw
			}
		}
		in.edges = append(in.edges, le)
	}
	out, err := layout(in)
	if err != nil {
		return nil, err
	}
	fd.out = out
	fd.width, fd.height = out.width, out.height
	if fd.lr {
		fd.width, fd.height = out.height, out.width
	}
	for i := range fd.geom {
		p := fd.screen(out.pos[i])
		fd.geom[i].cx, fd.geom[i].cy = p.x, p.y
	}
	return fd, nil
}

// groupPath lists a group and its ancestors, outermost first.
func (fc *flowchart) groupPath(g int) []int {
	var path []int
	for ; g >= 0; g = fc.groups[g].parent {
		path = append([]int{g}, path...)
	}
	return path
}

// screen maps an abstract (order, rank) point to drawing coordinates.
func (fd *flowDrawing) screen(p point) point {
	o, r := p.x, p.y
	if fd.flip {
		r = fd.out.height - r
	}
	if fd.lr {
		return point{r, o}
	}
	return point{o, r}
}

// port is where an edge meets a node: on the side facing the neighbouring
// point along the rank axis, offset across it so parallel edges fan out.
type portKey struct {
	node int
	high bool
}

type portUse struct {
	edge  int
	start bool
	cross float64 // order-axis coordinate of the neighbouring point
}

func (fd *flowDrawing) ports() map[[2]int]point {
	uses := map[portKey][]portUse{}
	for ei, e := range fd.fc.edges {
		r := fd.out.edges[ei]
		if r.self || e.stroke == strokeInvisible {
			continue
		}
		pts := r.pts
		src, dst := e.from, e.to
		srcNext, dstPrev := pts[1], pts[len(pts)-2]
		uses[portKey{src, srcNext.y > fd.out.pos[src].y}] = append(uses[portKey{src, srcNext.y > fd.out.pos[src].y}], portUse{ei, true, srcNext.x})
		uses[portKey{dst, dstPrev.y > fd.out.pos[dst].y}] = append(uses[portKey{dst, dstPrev.y > fd.out.pos[dst].y}], portUse{ei, false, dstPrev.x})
	}
	result := map[[2]int]point{}
	for key, list := range uses {
		sort.SliceStable(list, func(i, j int) bool { return list[i].cross < list[j].cross })
		n := fd.fc.nodes[key.node]
		ow, rh := fd.geom[key.node].w, fd.geom[key.node].h
		if fd.lr {
			ow, rh = rh, ow
		}
		spread := ow * 0.6
		if n.shape == shapeDiamond || n.shape == shapeCircle || n.shape == shapeDoubleCircle {
			spread = ow * 0.25
		}
		step := 0.0
		if len(list) > 1 {
			step = math.Min(14, spread/float64(len(list)-1))
		}
		c := fd.out.pos[key.node]
		for i, u := range list {
			off := (float64(i) - float64(len(list)-1)/2) * step
			reach := boundaryReach(n.shape, ow, rh, off, fd.lr)
			y := c.y - reach
			if key.high {
				y = c.y + reach
			}
			flag := 0
			if u.start {
				flag = 1
			}
			result[[2]int{u.edge, flag}] = fd.screen(point{c.x + off, y})
		}
	}
	return result
}

// boundaryReach is the rank-axis half extent of a shape at an order-axis
// offset from its centre.
func boundaryReach(shape fcShape, ow, rh, off float64, lr bool) float64 {
	switch shape {
	case shapeDiamond:
		return rh / 2 * math.Max(0, 1-math.Abs(off)/(ow/2))
	case shapeCircle, shapeDoubleCircle:
		r := ow / 2
		return math.Sqrt(math.Max(r*r-off*off, 0))
	case shapeHexagon:
		if lr {
			// The pointed ends face the rank axis.
			return rh/2 - (ow/4)*math.Min(1, math.Abs(off)/(ow/2))
		}
	}
	return rh / 2
}

func (fd *flowDrawing) edgePath(ei int, ports map[[2]int]point) (string, point) {
	r := fd.out.edges[ei]
	e := fd.fc.edges[ei]
	if r.self {
		g := fd.geom[e.from]
		reach := selfLoopReach * 1.6
		if fd.lr {
			x0, y0 := g.cx-8, g.cy+g.h/2
			x1 := g.cx + 8
			return "M" + num(x0) + "," + num(y0) + " C" + num(x0-10) + "," + num(y0+reach) + " " + num(x1+10) + "," + num(y0+reach) + " " + num(x1) + "," + num(y0),
				point{g.cx, y0 + reach*0.75}
		}
		x0, y0 := g.cx+g.w/2, g.cy-8
		y1 := g.cy + 8
		return "M" + num(x0) + "," + num(y0) + " C" + num(x0+reach) + "," + num(y0-10) + " " + num(x0+reach) + "," + num(y1+10) + " " + num(x0) + "," + num(y1),
			point{x0 + reach*0.75, g.cy}
	}
	pts := make([]point, len(r.pts))
	for i, p := range r.pts {
		pts[i] = fd.screen(p)
	}
	pts[0] = ports[[2]int{ei, 1}]
	pts[len(pts)-1] = ports[[2]int{ei, 0}]
	var b strings.Builder
	b.WriteString("M" + num(pts[0].x) + "," + num(pts[0].y))
	for i := 1; i < len(pts); i++ {
		a, c := pts[i-1], pts[i]
		if fd.lr {
			m := (a.x + c.x) / 2
			b.WriteString(" C" + num(m) + "," + num(a.y) + " " + num(m) + "," + num(c.y) + " " + num(c.x) + "," + num(c.y))
		} else {
			m := (a.y + c.y) / 2
			b.WriteString(" C" + num(a.x) + "," + num(m) + " " + num(c.x) + "," + num(m) + " " + num(c.x) + "," + num(c.y))
		}
	}
	return b.String(), fd.screen(r.label)
}

func (fd *flowDrawing) draw(rc rctx) *drawing {
	fc := fd.fc
	var body []ui.Node
	body = append(body, fd.clusters()...)
	ports := fd.ports()
	markerID := func(kind string) string { return rc.id + "-" + kind }
	var edges, labels []ui.Node
	for ei, e := range fc.edges {
		if e.stroke == strokeInvisible {
			continue
		}
		d, lp := fd.edgePath(ei, ports)
		class := "dg-edge"
		switch e.stroke {
		case strokeDotted:
			class += " dg-edge-dotted"
		case strokeThick:
			class += " dg-edge-thick"
		}
		extra := attrs{}
		if m := endMarker(e.end); m != "" {
			extra["marker-end"] = "url(#" + markerID(m) + ")"
		}
		if m := endMarker(e.start); m != "" {
			extra["marker-start"] = "url(#" + markerID(m) + ")"
		}
		edges = append(edges, path(class, d, extra))
		if lines := fd.edgeLabelLines[ei]; lines != nil {
			sz := fd.edgeLabelSize[ei]
			x, y := lp.x-sz.x/2, lp.y-sz.y/2
			labels = append(labels, group("dg-edge-label",
				rect("dg-edge-label-bg", x, y, sz.x, sz.y, 4),
				label(x, y, sz.x, sz.y, lines, "", "dg-label-small"),
			))
		}
	}
	body = append(body, group("dg-edges", edges...))
	var nodes []ui.Node
	for i, n := range fc.nodes {
		nodes = append(nodes, drawShape(n.shape, fd.geom[i]))
	}
	body = append(body, group("dg-nodes", nodes...), group("dg-edge-labels", labels...))
	defs := []ui.Node{
		arrowMarker(markerID("arrow"), "dg-arrowhead"),
		el("marker", "", attrs{"id": markerID("circle"), "viewBox": "0 0 10 10", "refX": "5", "refY": "5", "markerWidth": "8", "markerHeight": "8", "markerUnits": "userSpaceOnUse", "orient": "auto"},
			circle("dg-arrowhead", 5, 5, 4)),
		el("marker", "", attrs{"id": markerID("cross"), "viewBox": "0 0 10 10", "refX": "5", "refY": "5", "markerWidth": "9", "markerHeight": "9", "markerUnits": "userSpaceOnUse", "orient": "auto"},
			path("dg-arrowhead-cross", "M1,1 L9,9 M9,1 L1,9", nil)),
	}
	name := fc.accTitle
	if name == "" {
		name = rc.loc.t("flowchart")
	}
	desc := fc.accDescr
	if desc == "" {
		desc = fd.summary(rc.loc)
	}
	return &drawing{
		kind: KindFlowchart, name: name, desc: desc,
		width: fd.width, height: fd.height, body: body, defs: defs,
		fallback: fd.fallback(rc.loc),
	}
}

func endMarker(e edgeEnd) string {
	switch e {
	case endArrow:
		return "arrow"
	case endCircle:
		return "circle"
	case endCross:
		return "cross"
	}
	return ""
}

func (fd *flowDrawing) clusters() []ui.Node {
	fc := fd.fc
	if len(fc.groups) == 0 {
		return nil
	}
	type box struct {
		x0, y0, x1, y1 float64
		ok             bool
	}
	boxes := make([]box, len(fc.groups))
	extend := func(b *box, x0, y0, x1, y1 float64) {
		if !b.ok {
			*b = box{x0, y0, x1, y1, true}
			return
		}
		b.x0, b.y0 = math.Min(b.x0, x0), math.Min(b.y0, y0)
		b.x1, b.y1 = math.Max(b.x1, x1), math.Max(b.y1, y1)
	}
	for i, n := range fc.nodes {
		if n.group >= 0 {
			g := fd.geom[i]
			extend(&boxes[n.group], g.cx-g.w/2, g.cy-g.h/2, g.cx+g.w/2, g.cy+g.h/2)
		}
	}
	// Children are declared after their parents, so a reverse pass sees
	// every nested box before its parent.
	for gi := len(fc.groups) - 1; gi >= 0; gi-- {
		b := &boxes[gi]
		if !b.ok {
			continue
		}
		b.x0 -= clusterPad
		b.x1 += clusterPad
		b.y0 -= clusterPad + clusterTitle
		b.y1 += clusterPad
		if p := fc.groups[gi].parent; p >= 0 {
			extend(&boxes[p], b.x0, b.y0, b.x1, b.y1)
		}
	}
	var out []ui.Node
	for gi, g := range fc.groups {
		b := boxes[gi]
		if !b.ok {
			continue
		}
		w := b.x1 - b.x0
		out = append(out, group("dg-cluster",
			rect("dg-cluster-box", b.x0, b.y0, w, b.y1-b.y0, 6),
			label(b.x0+8, b.y0+3, w-16, clusterTitle-4, []string{truncateRunes(plainLabel(g.title), 60)}, "start", "dg-label-small dg-cluster-title"),
		))
	}
	return out
}

func drawShape(shape fcShape, g fcGeom) ui.Node {
	x, y, w, h := g.cx-g.w/2, g.cy-g.h/2, g.w, g.h
	cls := "dg-node"
	var parts []ui.Node
	poly := func(ps ...point) ui.Node { return el("polygon", cls, attrs{"points": pointsAttr(ps)}) }
	textX, textW := x+6, w-12
	textY, textH := y, h
	switch shape {
	case shapeRound:
		parts = append(parts, rect(cls, x, y, w, h, 10))
	case shapeStadium:
		cls += " dg-node-terminal"
		parts = append(parts, rect(cls, x, y, w, h, h/2))
	case shapeSubroutine:
		parts = append(parts, rect(cls, x, y, w, h, 2),
			line("dg-node-detail", x+8, y, x+8, y+h), line("dg-node-detail", x+w-8, y, x+w-8, y+h))
	case shapeCylinder:
		ry := math.Min(8, w/10)
		rx := w / 2
		d := "M" + num(x) + "," + num(y+ry) +
			" A" + num(rx) + "," + num(ry) + " 0 0 1 " + num(x+w) + "," + num(y+ry) +
			" V" + num(y+h-ry) +
			" A" + num(rx) + "," + num(ry) + " 0 0 1 " + num(x) + "," + num(y+h-ry) + " Z"
		parts = append(parts, path(cls, d, nil),
			path("dg-node-detail", "M"+num(x)+","+num(y+ry)+" A"+num(rx)+","+num(ry)+" 0 0 0 "+num(x+w)+","+num(y+ry), nil))
		textY, textH = y+2*ry, h-2*ry
	case shapeCircle:
		cls += " dg-node-terminal"
		parts = append(parts, circle(cls, g.cx, g.cy, w/2))
	case shapeDoubleCircle:
		cls += " dg-node-terminal"
		parts = append(parts, circle(cls, g.cx, g.cy, w/2), circle("dg-node-detail", g.cx, g.cy, w/2-4))
	case shapeDiamond:
		cls += " dg-node-decision"
		parts = append(parts, poly(point{g.cx, y}, point{x + w, g.cy}, point{g.cx, y + h}, point{x, g.cy}))
		textX, textW = x+w*0.2, w*0.6
	case shapeHexagon:
		cls += " dg-node-decision"
		k := h / 4
		parts = append(parts, poly(point{x + k, y}, point{x + w - k, y}, point{x + w, g.cy}, point{x + w - k, y + h}, point{x + k, y + h}, point{x, g.cy}))
	case shapeLeanRight:
		k := h / 4
		parts = append(parts, poly(point{x + k, y}, point{x + w, y}, point{x + w - k, y + h}, point{x, y + h}))
	case shapeLeanLeft:
		k := h / 4
		parts = append(parts, poly(point{x, y}, point{x + w - k, y}, point{x + w, y + h}, point{x + k, y + h}))
	case shapeTrapezoid:
		k := h / 4
		parts = append(parts, poly(point{x + k, y}, point{x + w - k, y}, point{x + w, y + h}, point{x, y + h}))
	case shapeTrapezoidAlt:
		k := h / 4
		parts = append(parts, poly(point{x, y}, point{x + w, y}, point{x + w - k, y + h}, point{x + k, y + h}))
	case shapeAsym:
		parts = append(parts, poly(point{x, y}, point{x + w, y}, point{x + w, y + h}, point{x, y + h}, point{x + h/4, g.cy}))
		textX, textW = x+h/4, w-h/4-6
	default:
		parts = append(parts, rect(cls, x, y, w, h, 3))
	}
	parts = append(parts, label(textX, textY, textW, textH, g.lines, "", ""))
	return group("dg-node-group", parts...)
}

// summary lists the steps in drawing order.
func (fd *flowDrawing) summary(loc locale) string {
	idx := make([]int, len(fd.fc.nodes))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ra, rb := fd.out.rank[idx[a]], fd.out.rank[idx[b]]
		if ra != rb {
			return ra < rb
		}
		return fd.out.pos[idx[a]].x < fd.out.pos[idx[b]].x
	})
	names := make([]string, len(idx))
	for i, v := range idx {
		names[i] = truncateRunes(plainLabel(fd.fc.nodes[v].label), 60)
	}
	visible := 0
	for _, e := range fd.fc.edges {
		if e.stroke != strokeInvisible {
			visible++
		}
	}
	return loc.t("flow.summary", "n", strconv.Itoa(len(names)), "e", strconv.Itoa(visible),
		"list", joinSummary(names, " → ", 6, loc))
}

func (fd *flowDrawing) fallback(loc locale) ui.Node {
	fc := fd.fc
	name := func(v int) string {
		n := fc.nodes[v]
		s := plainLabel(n.label)
		if n.group >= 0 {
			s += " (" + loc.t("flow.in", "group", plainLabel(fc.groups[n.group].title)) + ")"
		}
		return s
	}
	var items []string
	connected := make([]bool, len(fc.nodes))
	for _, e := range fc.edges {
		if e.stroke == strokeInvisible {
			continue
		}
		connected[e.from], connected[e.to] = true, true
		s := loc.t("flow.connects", "from", name(e.from), "to", name(e.to))
		if e.label != "" {
			s += ": " + plainLabel(e.label)
		}
		items = append(items, s)
	}
	for v := range fc.nodes {
		if !connected[v] {
			items = append(items, loc.t("flow.alone", "node", name(v)))
		}
	}
	return fallbackList(loc.t("flow.list"), false, items)
}
