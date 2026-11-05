package graphcanvas

import (
	"context"
	"sort"
	"sync"
)

// LayoutOptions tunes the layered layout. Zero values read as the defaults.
type LayoutOptions struct {
	LayerSpacing float64 // vertical gap between layers; default 120
	NodeSpacing  float64 // horizontal gap between nodes; default 40
	NodeWidth    float64 // assumed node width when a node has none; default 160
	NodeHeight   float64 // assumed node height when a node has none; default 64
	Sweeps       int     // barycenter ordering sweeps; default 2
}

func (o LayoutOptions) withDefaults() LayoutOptions {
	if o.LayerSpacing <= 0 {
		o.LayerSpacing = 120
	}
	if o.NodeSpacing <= 0 {
		o.NodeSpacing = 40
	}
	if o.NodeWidth <= 0 {
		o.NodeWidth = 160
	}
	if o.NodeHeight <= 0 {
		o.NodeHeight = 64
	}
	if o.Sweeps <= 0 {
		o.Sweeps = 2
	}
	return o
}

// LayeredLayout positions every node in layers: a node's layer is one past
// its deepest positioned predecessor, nodes without incoming edges sit on
// layer zero, and each layer orders by barycenter sweeps to shorten edges.
// The result is deterministic for a node order: ties break by id. Cycles
// cannot stall it: an edge into a node already on the current path is
// ignored for layering (it still draws).
func LayeredLayout(nodes []Node, edges []Edge, opts LayoutOptions) map[string]Point {
	opts = opts.withDefaults()
	ids := make([]string, 0, len(nodes))
	byID := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
		byID[n.ID] = n
	}
	incoming := map[string][]string{}
	for _, e := range edges {
		if _, ok := byID[e.FromNode]; !ok {
			continue
		}
		if _, ok := byID[e.ToNode]; !ok {
			continue
		}
		if e.FromNode == e.ToNode {
			continue
		}
		incoming[e.ToNode] = append(incoming[e.ToNode], e.FromNode)
	}
	for id := range incoming {
		sort.Strings(incoming[id])
	}

	layer := map[string]int{}
	state := map[string]int{} // 0 unvisited, 1 on path, 2 done
	var visit func(id string) int
	visit = func(id string) int {
		if state[id] == 2 {
			return layer[id]
		}
		if state[id] == 1 {
			return layer[id] // cycle edge: keep the current (partial) layer.
		}
		state[id] = 1
		deepest := -1
		for _, from := range incoming[id] {
			if state[from] == 1 {
				continue // ignore the back edge for layering.
			}
			if l := visit(from); l > deepest {
				deepest = l
			}
		}
		state[id] = 2
		layer[id] = deepest + 1
		return layer[id]
	}
	sort.Strings(ids)
	for _, id := range ids {
		visit(id)
	}

	layers := map[int][]string{}
	maxLayer := 0
	for _, id := range ids {
		l := layer[id]
		layers[l] = append(layers[l], id)
		if l > maxLayer {
			maxLayer = l
		}
	}
	position := map[string]int{}
	for l := 0; l <= maxLayer; l++ {
		row := layers[l]
		sort.Strings(row)
		for i, id := range row {
			position[id] = i
		}
	}
	for sweep := 0; sweep < opts.Sweeps; sweep++ {
		for l := 1; l <= maxLayer; l++ {
			row := layers[l]
			center := map[string]float64{}
			for _, id := range row {
				preds := incoming[id]
				if len(preds) == 0 {
					center[id] = float64(position[id])
					continue
				}
				sum := 0
				for _, from := range preds {
					sum += position[from]
				}
				center[id] = float64(sum) / float64(len(preds))
			}
			sort.SliceStable(row, func(i, j int) bool {
				if center[row[i]] == center[row[j]] {
					return row[i] < row[j]
				}
				return center[row[i]] < center[row[j]]
			})
			for i, id := range row {
				position[id] = i
			}
		}
	}

	out := make(map[string]Point, len(nodes))
	for l := 0; l <= maxLayer; l++ {
		x := 0.0
		for _, id := range layers[l] {
			n := byID[id]
			w := n.W
			if w <= 0 {
				w = opts.NodeWidth
			}
			out[id] = Point{X: x, Y: float64(l) * (opts.NodeHeight + opts.LayerSpacing)}
			x += w + opts.NodeSpacing
		}
	}
	return out
}

// LayoutRequest is one off-thread layout job.
type LayoutRequest struct {
	Nodes []Node
	Edges []Edge
	Opts  LayoutOptions
}

// LayoutWorker runs layered layout off the UI thread: requests queue on a
// channel served by one background goroutine, and each result delivers on
// the request's own channel. In wasm the worker yields cooperatively with
// the render loop; on native builds it is a real second thread.
type LayoutWorker struct {
	mu      sync.Mutex
	queue   chan layoutJob
	started bool
}

type layoutJob struct {
	req  LayoutRequest
	done chan map[string]Point
}

// NewLayoutWorker returns a stopped worker; Start begins it.
func NewLayoutWorker() *LayoutWorker {
	return &LayoutWorker{queue: make(chan layoutJob, 16)}
}

// Start begins the background lane. It is idempotent.
func (w *LayoutWorker) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return
	}
	w.started = true
	go func() {
		for job := range w.queue {
			job.done <- LayeredLayout(job.req.Nodes, job.req.Edges, job.req.Opts)
		}
	}()
}

// Stop drains the lane. Requests after Stop block until Start.
func (w *LayoutWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		return
	}
	w.started = false
	close(w.queue)
	w.queue = make(chan layoutJob, 16)
}

// Request queues one layout; the result arrives on the returned channel.
// Callers must Start the worker first.
func (w *LayoutWorker) Request(req LayoutRequest) <-chan map[string]Point {
	done := make(chan map[string]Point, 1)
	w.queue <- layoutJob{req: req, done: done}
	return done
}

// RequestContext queues one layout that also unblocks on context
// cancellation with a nil result.
func (w *LayoutWorker) RequestContext(ctx context.Context, req LayoutRequest) <-chan map[string]Point {
	out := make(chan map[string]Point, 1)
	go func() {
		select {
		case positions := <-w.Request(req):
			out <- positions
		case <-ctx.Done():
			out <- nil
		}
	}()
	return out
}
