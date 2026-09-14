package observetest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

func TestRecorderCapturesOperations(t *testing.T) {
	var r Recorder
	ctx := r.Context(context.Background())
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, op := observe.Start(ctx, "workflow.test.op", observe.Attrs{observe.KeyTenant: "t"})
			op.Set(observe.KeyNode, "n")
			op.Set(observe.KeyStatus, "")
			observe.Done(op, nil)
		})
	}
	wg.Wait()
	ops := r.Named("workflow.test.op")
	if len(ops) != 8 {
		t.Fatalf("ops = %d, want 8", len(ops))
	}
	for _, op := range ops {
		if op.Outcome != observe.OutcomeSuccess || op.Ended != 1 || op.Attrs[observe.KeyNode] != "n" || op.Attrs[observe.KeyTenant] != "t" {
			t.Errorf("op = %+v", op)
		}
		if _, ok := op.Attrs[observe.KeyStatus]; ok {
			t.Error("empty value recorded")
		}
	}

	_, op := observe.Start(ctx, "workflow.test.fail", nil)
	op.End(observe.OutcomeFailure, errors.New("boom"))
	op.End(observe.OutcomeSuccess, nil)
	got := r.Named("workflow.test.fail")[0]
	if got.Outcome != observe.OutcomeFailure || got.Code != "ERROR" || got.Ended != 2 {
		t.Errorf("double-ended op = %+v, want first outcome kept and both ends counted", got)
	}
	snapshot := r.Ops()
	snapshot[0].Attrs["mutated"] = "x"
	if _, ok := r.Ops()[0].Attrs["mutated"]; ok {
		t.Error("Ops snapshot aliases recorder state")
	}
	r.Reset()
	if len(r.Ops()) != 0 {
		t.Error("Reset kept operations")
	}
}
