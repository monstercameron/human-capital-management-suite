package docsdiagram

import (
	"math"
	"sort"
)

// Layered (Sugiyama-style) layout.
//
// The layout works on two abstract axes: the rank axis, along which edges
// flow, and the order axis across it. Every rank is doubled so each edge
// spans at least two ranks and owns a dummy in the rank between; a labelled
// edge's label rides on such a dummy, which gives labels their own room in
// both directions. The steps are the classic ones: break cycles by reversing
// DFS back edges, assign longest-path ranks, insert dummies, reduce crossings
// with alternating barycenter sweeps and adjacent transposes (keeping every
// subgraph contiguous within a rank), then place nodes along the order axis
// by weighted isotonic regression toward their neighbours, which straightens
// long edges and centres parents over children.

const (
	layoutNodeGap    = 36.0 // order-axis gap between two real nodes
	layoutDummyGap   = 14.0 // order-axis gap when a dummy is involved
	layoutRankGap    = 22.0 // rank-axis gap between consecutive (doubled) ranks
	layoutMargin     = 16.0
	clusterPad       = 14.0
	clusterTitle     = 22.0
	selfLoopReach    = 26.0
	maxLayoutNodes   = 4000
	orderingSweeps   = 12
	placementPasses  = 10
	dummyWeight      = 6.0
	anchorWeightIdle = 0.05
)

type lnode struct {
	ow, rh   float64 // extents along the order and rank axes
	path     []int   // enclosing clusters, outermost first
	selfLoop bool
}

type ledge struct {
	from, to         int
	minlen           int
	labelOW, labelRH float64
	hasLabel         bool
}

type layoutInput struct {
	nodes []lnode
	edges []ledge
	// titleAtStart puts cluster titles before the cluster's first rank (TB);
	// titleAtEnd after its last rank (BT). Neither is set for LR/RL, where
	// titles sit across the order axis.
	titleAtStart, titleAtEnd bool
	titleAcross              bool
}

type routed struct {
	pts      []point // abstract (order, rank) points: source, dummies, target
	label    point
	reversed bool
	self     bool
}

type layoutOut struct {
	pos      []point // node centres (order, rank)
	rank     []int
	edges    []routed
	width    float64 // order-axis extent
	height   float64 // rank-axis extent
	layers   [][]int // final ordering of layout items per rank (for tests)
	crossing int
}

type item struct {
	node     int // real node index or -1
	edge     int // owning edge for dummies
	ow, rh   float64
	path     []int
	rank     int
	up, down []int // neighbouring items in rank-1 / rank+1
	order    int
	x        float64
	decl     int
}

type layoutState struct {
	in     layoutInput
	items  []*item
	layers [][]int
	chains [][]int // per edge: item ids from source to target (layered direction)
	rev    []bool
}

func layout(in layoutInput) (*layoutOut, error) {
	st := &layoutState{in: in}
	rank, rev := st.rankNodes()
	st.rev = rev
	if err := st.buildItems(rank); err != nil {
		return nil, err
	}
	st.order()
	st.place()
	return st.result(rank), nil
}

// rankNodes breaks cycles and assigns longest-path ranks (doubled).
func (st *layoutState) rankNodes() ([]int, []bool) {
	n := len(st.in.nodes)
	rev := make([]bool, len(st.in.edges))
	out := make([][]int, n)
	for i, e := range st.in.edges {
		if e.from != e.to {
			out[e.from] = append(out[e.from], i)
		}
	}
	// Iterative DFS marks edges into an ancestor on the stack as back edges.
	state := make([]int, n) // 0 new, 1 on stack, 2 done
	type frame struct{ v, next int }
	for root := 0; root < n; root++ {
		if state[root] != 0 {
			continue
		}
		stack := []frame{{v: root}}
		state[root] = 1
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next >= len(out[top.v]) {
				state[top.v] = 2
				stack = stack[:len(stack)-1]
				continue
			}
			ei := out[top.v][top.next]
			top.next++
			w := st.in.edges[ei].to
			switch state[w] {
			case 0:
				state[w] = 1
				stack = append(stack, frame{v: w})
			case 1:
				rev[ei] = true
			}
		}
	}
	// DAG adjacency after reversal.
	succ := make([][]int, n)
	indeg := make([]int, n)
	pred := make([][]int, n)
	for i, e := range st.in.edges {
		if e.from == e.to {
			continue
		}
		a, b := e.from, e.to
		if rev[i] {
			a, b = b, a
		}
		succ[a] = append(succ[a], i)
		pred[b] = append(pred[b], i)
		indeg[b]++
	}
	var topo []int
	queue := []int{}
	for v := 0; v < n; v++ {
		if indeg[v] == 0 {
			queue = append(queue, v)
		}
	}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		topo = append(topo, v)
		for _, ei := range succ[v] {
			w := st.dagTo(ei, rev)
			indeg[w]--
			if indeg[w] == 0 {
				queue = append(queue, w)
			}
		}
	}
	rank := make([]int, n)
	for _, v := range topo {
		for _, ei := range succ[v] {
			w := st.dagTo(ei, rev)
			if r := rank[v] + 2*st.in.edges[ei].minlen; r > rank[w] {
				rank[w] = r
			}
		}
	}
	// Pull sources down next to their nearest successor, which shortens
	// edges from nodes declared late but drawn early.
	for i := len(topo) - 1; i >= 0; i-- {
		v := topo[i]
		if len(pred[v]) > 0 || len(succ[v]) == 0 {
			continue
		}
		best := math.MaxInt
		for _, ei := range succ[v] {
			w := st.dagTo(ei, rev)
			if r := rank[w] - 2*st.in.edges[ei].minlen; r < best {
				best = r
			}
		}
		if best > rank[v] {
			rank[v] = best
		}
	}
	return rank, rev
}

func (st *layoutState) dagTo(ei int, rev []bool) int {
	e := st.in.edges[ei]
	if rev[ei] {
		return e.from
	}
	return e.to
}

func (st *layoutState) buildItems(rank []int) error {
	maxRank := 0
	for v, nd := range st.in.nodes {
		ow := nd.ow
		if nd.selfLoop {
			ow += 2 * selfLoopReach
		}
		st.items = append(st.items, &item{node: v, edge: -1, ow: ow, rh: nd.rh, path: nd.path, rank: rank[v], decl: v})
		if rank[v] > maxRank {
			maxRank = rank[v]
		}
	}
	st.chains = make([][]int, len(st.in.edges))
	for ei, e := range st.in.edges {
		if e.from == e.to {
			continue
		}
		a, b := e.from, e.to
		if st.rev[ei] {
			a, b = b, a
		}
		ra, rb := rank[a], rank[b]
		common := commonPrefix(st.in.nodes[a].path, st.in.nodes[b].path)
		labelRank := -1
		if e.hasLabel {
			labelRank = ra + (rb-ra)/2
			if labelRank%2 == 0 {
				labelRank--
			}
		}
		chain := []int{a}
		prev := a
		for r := ra + 1; r < rb; r++ {
			if len(st.items) >= maxLayoutNodes {
				return tooLarge("layout positions", maxLayoutNodes)
			}
			it := &item{node: -1, edge: ei, rank: r, path: common, decl: len(st.in.nodes) + ei}
			if r == labelRank {
				it.ow, it.rh = e.labelOW, e.labelRH
			}
			id := len(st.items)
			st.items = append(st.items, it)
			st.link(prev, id)
			chain = append(chain, id)
			prev = id
		}
		st.link(prev, b)
		chain = append(chain, b)
		st.chains[ei] = chain
	}
	st.layers = make([][]int, maxRank+1)
	for id, it := range st.items {
		st.layers[it.rank] = append(st.layers[it.rank], id)
	}
	return nil
}

func (st *layoutState) link(a, b int) {
	st.items[a].down = append(st.items[a].down, b)
	st.items[b].up = append(st.items[b].up, a)
}

func commonPrefix(a, b []int) []int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return a[:n]
}

// order reduces crossings, keeping the best ordering seen.
func (st *layoutState) order() {
	for _, layer := range st.layers {
		sort.SliceStable(layer, func(i, j int) bool { return st.items[layer[i]].decl < st.items[layer[j]].decl })
		st.groupLayer(layer, func(id int) float64 { return float64(st.items[id].decl) })
		st.renumber(layer)
	}
	best := st.snapshot()
	bestCross := st.crossings()
	for sweep := 0; sweep < orderingSweeps && bestCross > 0; sweep++ {
		if sweep%2 == 0 {
			for r := 1; r < len(st.layers); r++ {
				st.barycenter(st.layers[r], true)
			}
		} else {
			for r := len(st.layers) - 2; r >= 0; r-- {
				st.barycenter(st.layers[r], false)
			}
		}
		st.transpose()
		if c := st.crossings(); c < bestCross {
			bestCross = c
			best = st.snapshot()
		}
	}
	st.restore(best)
}

func (st *layoutState) snapshot() [][]int {
	out := make([][]int, len(st.layers))
	for i, l := range st.layers {
		out[i] = append([]int(nil), l...)
	}
	return out
}

func (st *layoutState) restore(s [][]int) {
	for i := range st.layers {
		copy(st.layers[i], s[i])
		st.renumber(st.layers[i])
	}
}

func (st *layoutState) renumber(layer []int) {
	for i, id := range layer {
		st.items[id].order = i
	}
}

func (st *layoutState) barycenter(layer []int, fromAbove bool) {
	bary := map[int]float64{}
	for _, id := range layer {
		it := st.items[id]
		nb := it.down
		if fromAbove {
			nb = it.up
		}
		if len(nb) == 0 {
			bary[id] = float64(it.order)
			continue
		}
		sum := 0.0
		for _, n := range nb {
			sum += float64(st.items[n].order)
		}
		bary[id] = sum / float64(len(nb))
	}
	sort.SliceStable(layer, func(i, j int) bool { return bary[layer[i]] < bary[layer[j]] })
	st.groupLayer(layer, func(id int) float64 { return bary[id] })
	st.renumber(layer)
}

// groupLayer reorders a layer so that members of each cluster are
// contiguous, placing each cluster by the mean key of its members.
func (st *layoutState) groupLayer(layer []int, key func(int) float64) {
	ordered := st.groupAt(layer, 0, key)
	copy(layer, ordered)
}

func (st *layoutState) groupAt(ids []int, depth int, key func(int) float64) []int {
	type unit struct {
		ids []int
		key float64
		pos int
	}
	var units []*unit
	byCluster := map[int]*unit{}
	for i, id := range ids {
		p := st.items[id].path
		if len(p) <= depth {
			units = append(units, &unit{ids: []int{id}, key: key(id), pos: i})
			continue
		}
		u, ok := byCluster[p[depth]]
		if !ok {
			u = &unit{pos: i}
			byCluster[p[depth]] = u
			units = append(units, u)
		}
		u.ids = append(u.ids, id)
	}
	for _, u := range units {
		if len(u.ids) > 1 || len(st.items[u.ids[0]].path) > depth {
			sum := 0.0
			for _, id := range u.ids {
				sum += key(id)
			}
			u.key = sum / float64(len(u.ids))
			u.ids = st.groupAt(u.ids, depth+1, key)
		}
	}
	sort.SliceStable(units, func(i, j int) bool {
		if units[i].key != units[j].key {
			return units[i].key < units[j].key
		}
		return units[i].pos < units[j].pos
	})
	out := make([]int, 0, len(ids))
	for _, u := range units {
		out = append(out, u.ids...)
	}
	return out
}

// transpose swaps adjacent items of the same cluster when that lowers the
// crossings with both neighbouring ranks.
func (st *layoutState) transpose() {
	for pass := 0; pass < 4; pass++ {
		improved := false
		for _, layer := range st.layers {
			for i := 0; i+1 < len(layer); i++ {
				a, b := st.items[layer[i]], st.items[layer[i+1]]
				if !samePath(a.path, b.path) {
					continue
				}
				if st.pairCross(b, a) < st.pairCross(a, b) {
					layer[i], layer[i+1] = layer[i+1], layer[i]
					a.order, b.order = i+1, i
					improved = true
				}
			}
		}
		if !improved {
			return
		}
	}
}

func samePath(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// pairCross counts crossings between edges of a and b with a placed left of b.
func (st *layoutState) pairCross(a, b *item) int {
	count := 0
	for _, nbs := range [2][2][]int{{a.up, b.up}, {a.down, b.down}} {
		for _, x := range nbs[0] {
			for _, y := range nbs[1] {
				if st.items[x].order > st.items[y].order {
					count++
				}
			}
		}
	}
	return count
}

// crossings counts edge crossings between every pair of adjacent ranks.
func (st *layoutState) crossings() int {
	total := 0
	for r := 0; r+1 < len(st.layers); r++ {
		type seg struct{ a, b int }
		var segs []seg
		for _, id := range st.layers[r] {
			it := st.items[id]
			for _, d := range it.down {
				segs = append(segs, seg{it.order, st.items[d].order})
			}
		}
		for i := 0; i < len(segs); i++ {
			for j := i + 1; j < len(segs); j++ {
				if (segs[i].a-segs[j].a)*(segs[i].b-segs[j].b) < 0 {
					total++
				}
			}
		}
	}
	return total
}

// separation is the minimum centre distance between adjacent items.
func (st *layoutState) separation(a, b *item) float64 {
	gap := layoutNodeGap
	if a.node < 0 || b.node < 0 {
		gap = layoutDummyGap
	}
	common := len(commonPrefix(a.path, b.path))
	levels := float64(len(a.path) - common + len(b.path) - common)
	extra := levels * clusterPad
	if st.in.titleAcross {
		extra += float64(len(b.path)-common) * clusterTitle
	}
	return (a.ow+b.ow)/2 + gap + extra
}

// place assigns order-axis coordinates.
func (st *layoutState) place() {
	for _, layer := range st.layers {
		x := 0.0
		for i, id := range layer {
			it := st.items[id]
			if i > 0 {
				x += st.separation(st.items[layer[i-1]], it)
			}
			it.x = x
		}
	}
	for pass := 0; pass < placementPasses; pass++ {
		switch pass % 3 {
		case 0:
			for r := 1; r < len(st.layers); r++ {
				st.align(st.layers[r], true, false)
			}
		case 1:
			for r := len(st.layers) - 2; r >= 0; r-- {
				st.align(st.layers[r], false, true)
			}
		default:
			for r := range st.layers {
				st.align(st.layers[r], true, true)
			}
		}
	}
	minX := math.Inf(1)
	for _, it := range st.items {
		reach := float64(len(it.path)) * clusterPad
		if st.in.titleAcross {
			reach += float64(len(it.path)) * clusterTitle
		}
		minX = math.Min(minX, it.x-it.ow/2-reach)
	}
	for _, it := range st.items {
		it.x -= minX - layoutMargin
	}
}

// align moves a layer toward its neighbours' positions while keeping order
// and separation, solved exactly by pool-adjacent-violators.
func (st *layoutState) align(layer []int, useUp, useDown bool) {
	n := len(layer)
	if n == 0 {
		return
	}
	targets := make([]float64, n)
	weights := make([]float64, n)
	offsets := make([]float64, n)
	for i, id := range layer {
		it := st.items[id]
		if i > 0 {
			offsets[i] = offsets[i-1] + st.separation(st.items[layer[i-1]], it)
		}
		var nb []int
		if useUp {
			nb = append(nb, it.up...)
		}
		if useDown {
			nb = append(nb, it.down...)
		}
		if len(nb) == 0 {
			targets[i], weights[i] = it.x, anchorWeightIdle
		} else {
			sum := 0.0
			for _, m := range nb {
				sum += st.items[m].x
			}
			targets[i] = sum / float64(len(nb))
			weights[i] = float64(len(nb))
			if it.node < 0 {
				weights[i] *= dummyWeight
			}
		}
		targets[i] -= offsets[i]
	}
	ys := isotonic(targets, weights)
	for i, id := range layer {
		st.items[id].x = ys[i] + offsets[i]
	}
}

// isotonic returns the non-decreasing sequence closest to v in weighted
// least squares.
func isotonic(v, w []float64) []float64 {
	type block struct {
		mean, weight float64
		count        int
	}
	var blocks []block
	for i := range v {
		blocks = append(blocks, block{v[i], w[i], 1})
		for len(blocks) > 1 && blocks[len(blocks)-2].mean > blocks[len(blocks)-1].mean {
			a, b := blocks[len(blocks)-2], blocks[len(blocks)-1]
			tw := a.weight + b.weight
			blocks = blocks[:len(blocks)-2]
			blocks = append(blocks, block{(a.mean*a.weight + b.mean*b.weight) / tw, tw, a.count + b.count})
		}
	}
	out := make([]float64, 0, len(v))
	for _, b := range blocks {
		for k := 0; k < b.count; k++ {
			out = append(out, b.mean)
		}
	}
	return out
}

// rankCoords places ranks along the rank axis, leaving room for cluster
// padding and titles where clusters begin and end.
func (st *layoutState) rankCoords(rank []int) ([]float64, float64) {
	nr := len(st.layers)
	thick := make([]float64, nr)
	for _, it := range st.items {
		thick[it.rank] = math.Max(thick[it.rank], it.rh)
	}
	before := make([]float64, nr)
	after := make([]float64, nr)
	// Cluster rank spans from real members (including nested clusters).
	type span struct{ lo, hi int }
	spans := map[int]*span{}
	for v, nd := range st.in.nodes {
		for _, c := range nd.path {
			s, ok := spans[c]
			if !ok {
				spans[c] = &span{rank[v], rank[v]}
				continue
			}
			s.lo = min(s.lo, rank[v])
			s.hi = max(s.hi, rank[v])
		}
	}
	for _, s := range spans {
		before[s.lo] += clusterPad
		after[s.hi] += clusterPad
		if st.in.titleAtStart {
			before[s.lo] += clusterTitle
		}
		if st.in.titleAtEnd {
			after[s.hi] += clusterTitle
		}
	}
	centers := make([]float64, nr)
	cursor := layoutMargin
	for r := 0; r < nr; r++ {
		cursor += before[r]
		centers[r] = cursor + thick[r]/2
		cursor += thick[r] + after[r]
		if r+1 < nr {
			cursor += layoutRankGap
		}
	}
	return centers, cursor + layoutMargin
}

func (st *layoutState) result(rank []int) *layoutOut {
	centers, height := st.rankCoords(rank)
	out := &layoutOut{rank: rank, height: height}
	maxX := 0.0
	for _, it := range st.items {
		maxX = math.Max(maxX, it.x+it.ow/2+float64(len(it.path))*clusterPad)
	}
	out.width = maxX + layoutMargin
	out.pos = make([]point, len(st.in.nodes))
	for v := range st.in.nodes {
		it := st.items[v]
		out.pos[v] = point{it.x, centers[it.rank]}
	}
	out.edges = make([]routed, len(st.in.edges))
	for ei, e := range st.in.edges {
		if e.from == e.to {
			out.edges[ei] = routed{self: true, pts: []point{out.pos[e.from]}}
			continue
		}
		chain := st.chains[ei]
		pts := make([]point, len(chain))
		for i, id := range chain {
			it := st.items[id]
			pts[i] = point{it.x, centers[it.rank]}
		}
		r := routed{pts: pts, reversed: st.rev[ei]}
		if e.hasLabel {
			for _, id := range chain {
				it := st.items[id]
				if it.node < 0 && (it.ow > 0 || it.rh > 0) {
					r.label = point{it.x, centers[it.rank]}
				}
			}
		}
		if r.reversed {
			for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
				pts[i], pts[j] = pts[j], pts[i]
			}
		}
		out.edges[ei] = r
	}
	for _, layer := range st.layers {
		out.layers = append(out.layers, append([]int(nil), layer...))
	}
	out.crossing = st.crossings()
	return out
}
