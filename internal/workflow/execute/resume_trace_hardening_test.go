package execute

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type resumedTraceKey struct{}
type resumedTraceInstrumentation struct{ recordingStarter }

func (r *resumedTraceInstrumentation) TraceID(ctx context.Context) string {
	if id, ok := ctx.Value(resumedTraceKey{}).(string); ok {
		return id
	}
	return "dispatch-trace"
}

func TestResumeSpanReportsCyclicLookupFailure(t *testing.T) {
	selection, record := timerResumeSelection(t)
	selection.Plan.Limits.DeclaredCycles = []workflow.CycleDeclaration{{}}
	record.CompiledPlanDigest = selection.Plan.Digest()
	starter := &recordingStarter{}
	advanced := false
	d, err := resumeTimerDriver(fakeCausalTimerReader{row: withCausal(firedRow())}, starter,
		func(ctx context.Context, ex runtime.Executor, req runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			advanced = true
			return completeAdvance(ctx, ex, req)
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ResumeTimer(context.Background(), timerResumeRequest(selection, record)); err == nil {
		t.Fatal("expected the injected node lookup failure")
	}
	ends := starter.ends()
	if advanced || len(ends) != 1 || !ends[0].failed || ends[0].outcome != OutcomeFailure {
		t.Fatalf("advanced=%v resume endings=%v", advanced, ends)
	}
}

func (r *resumedTraceInstrumentation) StartResumeSpan(ctx context.Context, req ResumeSpanRequest) (context.Context, Span) {
	ctx, span := r.recordingStarter.StartResumeSpan(ctx, req)
	return context.WithValue(ctx, resumedTraceKey{}, "resumed-trace"), span
}

func TestResumeAdvancementPersistsActiveTrace(t *testing.T) {
	selection, record := timerResumeSelection(t)
	starter := &resumedTraceInstrumentation{}
	var persistedTrace string
	d, err := resumeTimerDriver(fakeCausalTimerReader{row: withCausal(firedRow())}, &starter.recordingStarter,
		func(ctx context.Context, ex runtime.Executor, req runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			persistedTrace = req.TraceID
			return completeAdvance(ctx, ex, req)
		})
	if err != nil {
		t.Fatal(err)
	}
	d.opts.Instrumentation = starter
	if _, err := d.ResumeTimer(context.Background(), timerResumeRequest(selection, record)); err != nil {
		t.Fatal(err)
	}
	if persistedTrace != "resumed-trace" {
		t.Fatalf("persisted trace=%q want the active resume trace", persistedTrace)
	}
}
