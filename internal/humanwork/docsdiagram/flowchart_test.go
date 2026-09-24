package docsdiagram

import (
	"math"
	"strings"
	"testing"
	"time"
)

func mustFlow(t *testing.T, src string) *flowchart {
	t.Helper()
	lines, err := prepare(src)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	_, rest, _ := headerKind(lines[0])
	fc, err := parseFlowchart(lines[0], rest, lines[1:])
	if err != nil {
		t.Fatalf("parseFlowchart: %v", err)
	}
	return fc
}

func (fc *flowchart) node(id string) *fcNode {
	for _, n := range fc.nodes {
		if n.id == id {
			return n
		}
	}
	return nil
}

func TestParseFlowchartShapes(t *testing.T) {
	cases := []struct {
		src   string
		shape fcShape
		label string
	}{
		{"A[Plain]", shapeRect, "Plain"},
		{"A(Rounded)", shapeRound, "Rounded"},
		{"A([Stadium])", shapeStadium, "Stadium"},
		{"A[[Sub]]", shapeSubroutine, "Sub"},
		{"A[(Database)]", shapeCylinder, "Database"},
		{"A((Circle))", shapeCircle, "Circle"},
		{"A(((Double)))", shapeDoubleCircle, "Double"},
		{"A{Decide?}", shapeDiamond, "Decide?"},
		{"A{{Hex}}", shapeHexagon, "Hex"},
		{"A[/Lean/]", shapeLeanRight, "Lean"},
		{"A[\\Lean\\]", shapeLeanLeft, "Lean"},
		{"A[/Trap\\]", shapeTrapezoid, "Trap"},
		{"A[\\Alt/]", shapeTrapezoidAlt, "Alt"},
		{"A>Flag]", shapeAsym, "Flag"},
		{`A["Quoted (with) [brackets]"]`, shapeRect, "Quoted (with) [brackets]"},
		{"A[#quot;hi#quot; #35;1]", shapeRect, `"hi" #1`},
		{"A[\"`Markdown`\"]", shapeRect, "Markdown"},
		{"A@{ shape: diamond, label: \"New syntax\" }", shapeDiamond, "New syntax"},
		{"A[Styled]:::hot", shapeRect, "Styled"},
		{"A", shapeRect, "A"},
	}
	for _, c := range cases {
		fc := mustFlow(t, "flowchart TD\n"+c.src)
		n := fc.node("A")
		if n == nil || n.shape != c.shape || n.label != c.label {
			t.Errorf("%s: got %+v, want shape %d label %q", c.src, n, c.shape, c.label)
		}
	}
}

func TestParseFlowchartEdges(t *testing.T) {
	cases := []struct {
		src    string
		stroke edgeStroke
		start  edgeEnd
		end    edgeEnd
		label  string
		minlen int
	}{
		{"A-->B", strokeSolid, endNone, endArrow, "", 1},
		{"A --- B", strokeSolid, endNone, endNone, "", 1},
		{"A ---> B", strokeSolid, endNone, endArrow, "", 2},
		{"A -.-> B", strokeDotted, endNone, endArrow, "", 1},
		{"A -..-> B", strokeDotted, endNone, endArrow, "", 2},
		{"A ==> B", strokeThick, endNone, endArrow, "", 1},
		{"A === B", strokeThick, endNone, endNone, "", 1},
		{"A --o B", strokeSolid, endNone, endCircle, "", 1},
		{"A --x B", strokeSolid, endNone, endCross, "", 1},
		{"A <--> B", strokeSolid, endArrow, endArrow, "", 1},
		{"A o--o B", strokeSolid, endCircle, endCircle, "", 1},
		{"A x--x B", strokeSolid, endCross, endCross, "", 1},
		{"A -->|yes| B", strokeSolid, endNone, endArrow, "yes", 1},
		{"A -- in line text --> B", strokeSolid, endNone, endArrow, "in line text", 1},
		{"A -. maybe .-> B", strokeDotted, endNone, endArrow, "maybe", 1},
		{"A == hard ==> B", strokeThick, endNone, endArrow, "hard", 1},
		{"A ~~~ B", strokeInvisible, endNone, endNone, "", 1},
		{"A " + strings.Repeat("-", 30) + "> B", strokeSolid, endNone, endArrow, "", 8},
	}
	for _, c := range cases {
		fc := mustFlow(t, "graph LR\n"+c.src)
		if len(fc.edges) != 1 {
			t.Fatalf("%s: %d edges", c.src, len(fc.edges))
		}
		e := fc.edges[0]
		if e.stroke != c.stroke || e.start != c.start || e.end != c.end || e.label != c.label || e.minlen != c.minlen {
			t.Errorf("%s: got %+v", c.src, e)
		}
		if fc.nodes[e.from].id != "A" || fc.nodes[e.to].id != "B" {
			t.Errorf("%s: wrong endpoints", c.src)
		}
	}
}

func TestParseFlowchartStatements(t *testing.T) {
	fc := mustFlow(t, `flowchart LR
    %% a comment
    A --> B --> C ; C --> D %% trailing
    E & F --> G & H
    classDef hot fill:#f00
    class A hot
    style B fill:#0f0
    linkStyle 0 stroke:#f00
    click A "https://example.com"
    subgraph one [First group]
        direction TB
        I --> J
        subgraph inner
            K
        end
    end
    subgraph "Quoted title"
        L
    end
    I --> one
    one --> Z
    node-with-dash --> A`)
	if fc.dir != "LR" {
		t.Errorf("dir = %s", fc.dir)
	}
	var pairs []string
	for _, e := range fc.edges {
		pairs = append(pairs, fc.nodes[e.from].id+">"+fc.nodes[e.to].id)
	}
	want := "A>B B>C C>D E>G E>H F>G F>H I>J I>I K>Z node-with-dash>A"
	if got := strings.Join(pairs, " "); got != want {
		t.Errorf("edges = %s\nwant    %s", got, want)
	}
	if fc.node("one") != nil {
		t.Error("subgraph reference became a node")
	}
	if len(fc.groups) != 3 || fc.groups[0].title != "First group" || fc.groups[1].title != "inner" || fc.groups[1].parent != 0 || fc.groups[2].title != "Quoted title" {
		t.Errorf("groups = %+v %+v %+v", fc.groups[0], fc.groups[1], fc.groups[2])
	}
	if fc.node("K").group != 1 || fc.node("I").group != 0 || fc.node("A").group != -1 {
		t.Error("group membership wrong")
	}
	if got := fc.groupPath(1); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("groupPath = %v", got)
	}
}

func TestWrapLabel(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Short", []string{"Short"}},
		{"Manager review of the request", []string{"Manager review of", "the request"}},
		{"line one<br/>line two", []string{"line one", "line two"}},
		{"Supercalifragilisticexpialidocious", []string{"Supercalifragilistic", "expialidocious"}},
		{"", []string{""}},
	}
	for _, c := range cases {
		if got := wrapLabel(c.in, 20); strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("wrapLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if w := textWidth("漢字", 10); w != 20 {
		t.Errorf("wide glyph width = %v", w)
	}
}

type box struct{ x0, y0, x1, y1 float64 }

func geomBox(g fcGeom) box { return box{g.cx - g.w/2, g.cy - g.h/2, g.cx + g.w/2, g.cy + g.h/2} }

func overlaps(a, b box) bool { return a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1 }

const twelveNodes = `flowchart TD
    A[Submit request] --> B{Manager approves?}
    B -->|yes| C[HR review]
    B -->|no| D[Return to employee]
    D --> A
    C --> E[Payroll update]
    C --> F[Benefits update]
    E --> G[Notify employee]
    F --> G
    G --> H[(Records)]
    C --> I[Compliance check]
    I --> J{Passed?}
    J -->|no| K[Escalate]
    J -->|yes| L((Done))
    K --> C
    H --> L`

func TestLayoutTwelveNodesDoNotOverlap(t *testing.T) {
	for _, dir := range []string{"TD", "LR", "BT", "RL"} {
		fc := mustFlow(t, strings.Replace(twelveNodes, "TD", dir, 1))
		if len(fc.nodes) != 12 {
			t.Fatalf("nodes = %d", len(fc.nodes))
		}
		fd, err := layoutFlowchart(fc)
		if err != nil {
			t.Fatal(err)
		}
		for i := range fd.geom {
			bi := geomBox(fd.geom[i])
			if bi.x0 < 0 || bi.y0 < 0 || bi.x1 > fd.width || bi.y1 > fd.height {
				t.Errorf("%s: node %s outside the drawing: %+v in %vx%v", dir, fc.nodes[i].id, bi, fd.width, fd.height)
			}
			for j := i + 1; j < len(fd.geom); j++ {
				if overlaps(bi, geomBox(fd.geom[j])) {
					t.Errorf("%s: nodes %s and %s overlap", dir, fc.nodes[i].id, fc.nodes[j].id)
				}
			}
		}
		// Ranks respect edge direction except for reversed (cycle) edges.
		for ei, e := range fc.edges {
			if fd.out.edges[ei].reversed {
				continue
			}
			if fd.out.rank[e.to] <= fd.out.rank[e.from] {
				t.Errorf("%s: edge %s->%s not downward", dir, fc.nodes[e.from].id, fc.nodes[e.to].id)
			}
		}
	}
}

func TestLayoutEdgesMeetNodeBoundaries(t *testing.T) {
	fc := mustFlow(t, twelveNodes)
	fd, err := layoutFlowchart(fc)
	if err != nil {
		t.Fatal(err)
	}
	ports := fd.ports()
	reversed := 0
	for ei, e := range fc.edges {
		start, end := ports[[2]int{ei, 1}], ports[[2]int{ei, 0}]
		src, dst := fd.geom[e.from], fd.geom[e.to]
		if fd.out.edges[ei].reversed {
			reversed++
			// A back edge leaves the top of its source and enters the
			// bottom of its target.
			if !near(start.y, src.cy-src.h/2, fc.nodes[e.from].shape, src, start) || !near(end.y, dst.cy+dst.h/2, fc.nodes[e.to].shape, dst, end) {
				t.Errorf("back edge %s->%s ports %v %v", fc.nodes[e.from].id, fc.nodes[e.to].id, start, end)
			}
			continue
		}
		if !near(start.y, src.cy+src.h/2, fc.nodes[e.from].shape, src, start) {
			t.Errorf("edge %s->%s starts at %v, not on the bottom of %+v", fc.nodes[e.from].id, fc.nodes[e.to].id, start, src)
		}
		if !near(end.y, dst.cy-dst.h/2, fc.nodes[e.to].shape, dst, end) {
			t.Errorf("edge %s->%s ends at %v, not on the top of %+v", fc.nodes[e.from].id, fc.nodes[e.to].id, end, dst)
		}
		if math.Abs(start.x-src.cx) > src.w/2 || math.Abs(end.x-dst.cx) > dst.w/2 {
			t.Errorf("edge %s->%s port outside node width", fc.nodes[e.from].id, fc.nodes[e.to].id)
		}
	}
	if reversed != 2 {
		t.Errorf("reversed edges = %d, want 2 (D->A and K->C close cycles)", reversed)
	}
	d, _ := fd.edgePath(0, ports)
	if !strings.HasPrefix(d, "M"+num(ports[[2]int{0, 1}].x)+","+num(ports[[2]int{0, 1}].y)) || !strings.Contains(d, " C") {
		t.Errorf("edge path %q does not start at the port with a curve", d)
	}
}

// near checks a port against the shape boundary: rectangles meet their top
// or bottom side exactly; diamonds and circles meet it at their slanted or
// curved edge, which lies between the side and the centre.
func near(got, side float64, shape fcShape, g fcGeom, p point) bool {
	switch shape {
	case shapeDiamond, shapeCircle, shapeDoubleCircle:
		lo, hi := math.Min(side, g.cy), math.Max(side, g.cy)
		return got >= lo-0.5 && got <= hi+0.5
	}
	return math.Abs(got-side) < 0.51
}

func TestCrossingReductionBeatsDeclarationOrder(t *testing.T) {
	// Declared so that the naive order crosses every edge pair.
	in := layoutInput{}
	for i := 0; i < 8; i++ {
		in.nodes = append(in.nodes, lnode{ow: 60, rh: 30})
	}
	// 0..3 top, 4..7 bottom; i -> 7-i gives the maximum crossings.
	for i := 0; i < 4; i++ {
		in.edges = append(in.edges, ledge{from: i, to: 7 - i, minlen: 1})
	}
	st := &layoutState{in: in}
	rank, rev := st.rankNodes()
	st.rev = rev
	if err := st.buildItems(rank); err != nil {
		t.Fatal(err)
	}
	for _, layer := range st.layers {
		st.renumber(layer)
	}
	naive := st.crossings()
	st.order()
	after := st.crossings()
	if naive != 6 || after != 0 {
		t.Fatalf("crossings naive=%d after=%d, want 6 and 0", naive, after)
	}
}

func TestLayoutKeepsClustersContiguous(t *testing.T) {
	fc := mustFlow(t, `flowchart TD
    A --> X1
    A --> Y1
    A --> X2
    A --> Y2
    subgraph xs [X]
        X1
        X2
    end`)
	fd, err := layoutFlowchart(fc)
	if err != nil {
		t.Fatal(err)
	}
	x1, x2 := fd.geom[fc.indexOf("X1")], fd.geom[fc.indexOf("X2")]
	lo, hi := math.Min(x1.cx, x2.cx), math.Max(x1.cx, x2.cx)
	for _, id := range []string{"Y1", "Y2"} {
		y := fd.geom[fc.indexOf(id)]
		if y.cx > lo && y.cx < hi {
			t.Errorf("%s sits between the cluster members", id)
		}
	}
	out := renderHTML(t, `flowchart LR
    subgraph g [Group]
        A --> A
    end
    A --> B`, Options{})
	if strings.Count(out, `class="dg-cluster"`) != 1 || strings.Count(out, `<path class="dg-edge`) != 2 {
		t.Error("self loop or cluster missing in LR drawing")
	}
}

func (fc *flowchart) indexOf(id string) int {
	for i, n := range fc.nodes {
		if n.id == id {
			return i
		}
	}
	return -1
}

func TestIsotonic(t *testing.T) {
	got := isotonic([]float64{3, 1, 2, 5}, []float64{1, 1, 1, 1})
	want := []float64{2, 2, 2, 5}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("isotonic = %v, want %v", got, want)
		}
	}
}

func TestLayoutRefusesTooManyDummies(t *testing.T) {
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	// A long chain plus edges from the head to every chain node forces
	// quadratic dummy counts.
	for i := 0; i < 99; i++ {
		b.WriteString("  c" + itoa(i) + " ------> c" + itoa(i+1) + "\n")
	}
	for i := 2; i < 100; i++ {
		b.WriteString("  c0 --> c" + itoa(i) + "\n")
	}
	if _, err := Render(b.String(), Options{}); err == nil || !strings.Contains(err.Error(), "layout positions") {
		t.Fatalf("err = %v, want layout cap", err)
	}
}

func TestAllShapesDraw(t *testing.T) {
	out := renderHTML(t, `flowchart BT
    a[r] --> b(r) --> c([s]) --> d[[s]] --> e[(c)] --> f((c)) --> g(((d))) --> h{d}
    h --> i{{h}} --> j[/l/] --> k[\l\] --> l[/t\] --> m[\t/] --> n>a]`, Options{})
	for _, want := range []string{"<polygon", "<circle", "<path", "<rect", "<line"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Count(out, `class="dg-node-group"`) != 14 {
		t.Error("not every shape drew")
	}
}

func TestLayoutDenseGraphAtTheCapsIsFast(t *testing.T) {
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	seed := uint32(7)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	for i := 0; i < MaxElements; i++ {
		a := next(60)
		c := a + 1 + next(20)
		b.WriteString("  n" + itoa(a) + "[Step number " + itoa(a) + "] --> n" + itoa(c) + "\n")
	}
	start := time.Now()
	out := renderHTML(t, b.String(), Options{})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("dense layout took %v", elapsed)
	}
	if strings.Count(out, `<path class="dg-edge`) != MaxElements {
		t.Error("edges lost in dense layout")
	}
}
