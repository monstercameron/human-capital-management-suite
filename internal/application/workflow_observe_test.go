package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

// TestWorkflowEngineOperationsAreObservableEndToEnd proves the workflow
// engine's telemetry reaches every layer a real timer wake crosses: one
// scheduler tick over the composed server opens an operation for the claim it
// serves, the queue lease it takes, the timer it fires, the driver resume the
// dispatcher runs and the runtime advancement beneath it -- all through the
// recorder on the tick's context, each ending once with its outcome. A second
// replica contending for the same queue is recorded as a governed refusal
// with its stable lease code, not as a silent no-op.
func TestWorkflowEngineOperationsAreObservableEndToEnd(t *testing.T) {
	h := promoux015Compose(t)
	h.runSeparatedPromotion()
	late := h.afterEffectiveDate()()
	clock := func() time.Time { return late }

	var rec observetest.Recorder
	ctx := rec.Context(context.Background())
	tick, err := h.replica("replica-observed", clock).Tick(ctx)
	if err != nil || tick.Leased != 1 || tick.Fired != 1 {
		t.Fatalf("observed tick = %+v, %v; want the queue leased and the due timer fired", tick, err)
	}

	for _, name := range []string{
		"workflow.scheduler.serve",
		"workflow.lease.acquire",
		"workflow.timer.fire",
		"workflow.execute.resume_timer",
	} {
		ops := rec.Named(name)
		if len(ops) == 0 {
			t.Errorf("no %s operation was recorded; recorded %v", name, opNames(rec.Ops()))
			continue
		}
		if ops[0].Outcome != observe.OutcomeSuccess {
			t.Errorf("%s ended %s (%s), want SUCCESS", name, ops[0].Outcome, ops[0].Code)
		}
	}
	if acquire := rec.Named("workflow.lease.acquire"); len(acquire) > 0 &&
		(acquire[0].Attrs[observe.KeyFence] == "" || acquire[0].Attrs[observe.KeyResource] != string(lease.ResourceQueue)) {
		t.Errorf("queue acquire attrs = %v, want its fence token and QUEUE resource", acquire[0].Attrs)
	}
	advanced := append(rec.Named("workflow.runtime.advance"), rec.Named("workflow.runtime.advance_fenced")...)
	if len(advanced) == 0 {
		t.Errorf("the timer resume recorded no runtime advancement; recorded %v", opNames(rec.Ops()))
	}
	for _, op := range advanced {
		if op.Attrs[observe.KeyInstance] == "" {
			t.Errorf("advancement without instance id: %+v", op)
		}
	}
	for _, op := range rec.Ops() {
		if op.Ended != 1 {
			t.Errorf("%s ended %d times, want exactly once", op.Name, op.Ended)
		}
	}

	rec.Reset()
	if _, err := h.replica("replica-contender", clock).Tick(ctx); err != nil {
		t.Logf("contending tick error (acceptable): %v", err)
	}
	refused := false
	for _, op := range rec.Named("workflow.lease.acquire") {
		if op.Outcome == observe.OutcomeRefused && op.Code == lease.CodeLeaseHeld {
			refused = true
		}
	}
	if !refused {
		t.Errorf("the contending replica's queue acquire was not recorded as REFUSED %s: %v", lease.CodeLeaseHeld, rec.Named("workflow.lease.acquire"))
	}
}

func opNames(ops []observetest.Op) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		out = append(out, op.Name+"="+op.Outcome)
	}
	return out
}
