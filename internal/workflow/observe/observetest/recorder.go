// Package observetest is an in-memory [observe.Recorder] for tests that
// prove a workflow operation is instrumented: which operations opened, with
// which attributes, and how each one ended.
package observetest

import (
	"context"
	"maps"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Op is one recorded operation.
type Op struct {
	Name    string
	Attrs   observe.Attrs
	Outcome string
	Code    string
	Ended   int
}

// Recorder records every operation it is asked to open. It is safe for
// concurrent use.
type Recorder struct {
	mu  sync.Mutex
	ops []*Op
}

var _ observe.Recorder = (*Recorder)(nil)

// Context returns ctx carrying r.
func (r *Recorder) Context(ctx context.Context) context.Context {
	return observe.WithRecorder(ctx, r)
}

// Start implements observe.Recorder.
func (r *Recorder) Start(ctx context.Context, name string, attrs observe.Attrs) (context.Context, observe.Operation) {
	op := &Op{Name: name, Attrs: observe.Attrs{}}
	maps.Copy(op.Attrs, attrs)
	r.mu.Lock()
	r.ops = append(r.ops, op)
	r.mu.Unlock()
	return ctx, operation{r: r, op: op}
}

// Ops returns a snapshot of every recorded operation in start order.
func (r *Recorder) Ops() []Op {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Op, 0, len(r.ops))
	for _, op := range r.ops {
		cp := *op
		cp.Attrs = maps.Clone(op.Attrs)
		out = append(out, cp)
	}
	return out
}

// Named returns the recorded operations called name.
func (r *Recorder) Named(name string) []Op {
	var out []Op
	for _, op := range r.Ops() {
		if op.Name == name {
			out = append(out, op)
		}
	}
	return out
}

// Reset forgets every recorded operation.
func (r *Recorder) Reset() {
	r.mu.Lock()
	r.ops = nil
	r.mu.Unlock()
}

type operation struct {
	r  *Recorder
	op *Op
}

func (o operation) Set(key, value string) {
	if value == "" {
		return
	}
	o.r.mu.Lock()
	o.op.Attrs[key] = value
	o.r.mu.Unlock()
}

func (o operation) End(outcome string, err error) {
	o.r.mu.Lock()
	defer o.r.mu.Unlock()
	o.op.Ended++
	if o.op.Ended == 1 {
		o.op.Outcome, o.op.Code = outcome, observe.ErrorCode(err)
	}
}
