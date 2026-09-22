package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_WF_RUN_041_Security(t *testing.T) {
	for _, source := range []string{"context", "static"} {
		for _, kind := range []string{"foreign instance", "queue", "empty"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				scn := newOBSScenario(t, "")
				d := scn.driver(t)
				var recorder observetest.Recorder
				d.opts.Recorder = &recorder
				verifier := &fakeVerifier{}
				d.opts.FenceVerifier = verifier
				fence := runtime.Fence{ResourceKind: "WORKFLOW_INSTANCE", ResourceID: uuid.NewString(), LeaseID: uuid.New(), HolderID: "workload:test#replica:1", Token: 1}
				if kind == "queue" {
					fence.ResourceKind, fence.ResourceID = "QUEUE", scn.instanceID.String()
				}
				if kind == "empty" {
					fence = runtime.Fence{}
				}
				ctx := context.Background()
				if source == "context" {
					ctx = WithFence(ctx, fence)
				} else {
					d.opts.Fence = &fence
				}
				_, err := d.Resume(ctx, scn.req)
				if !errors.Is(err, ErrFenceRefused) || len(verifier.seen) != 0 || scn.advanceCalls != 0 || len(scn.stepRunner.traceIDs) != 0 || scn.terminal.calls != 0 {
					t.Fatalf("error=%v verified=%d advanced=%d steps=%d terminal=%d", err, len(verifier.seen), scn.advanceCalls, len(scn.stepRunner.traceIDs), scn.terminal.calls)
				}
				ops := recorder.Named("workflow.execute.resume")
				if len(ops) != 1 || ops[0].Ended != 1 || ops[0].Outcome != observe.OutcomeRefused || ops[0].Attrs[observe.KeyCorrelation] != scn.req.Start.CorrelationID {
					t.Fatalf("refusal telemetry=%+v", ops)
				}
			})
		}
	}
}

type foreignInstanceLeaser struct{ fakeLeaser }

func (l *foreignInstanceLeaser) AcquireInstance(ctx context.Context, ex runtime.Executor, tenantID, _ uuid.UUID, at time.Time) (runtime.Fence, error) {
	return l.fakeLeaser.AcquireInstance(ctx, ex, tenantID, uuid.New(), at)
}

func TestAcquiredForeignFenceRollsBackBeforeRunning(t *testing.T) {
	scn := newOBSScenario(t, "")
	d := scn.driver(t)
	leaser := &foreignInstanceLeaser{}
	verifier := &fakeVerifier{}
	tx := &memoryTx{}
	d.opts.DB, d.opts.Leases, d.opts.FenceVerifier = oneBeginner{tx}, leaser, verifier
	_, err := d.Resume(context.Background(), scn.req)
	if !errors.Is(err, ErrFenceRefused) || tx.committed || !tx.rolledBack || len(verifier.seen) != 0 || scn.advanceCalls != 0 || leaser.released != 0 {
		t.Fatalf("error=%v commit=%v rollback=%v verified=%d advances=%d released=%d", err, tx.committed, tx.rolledBack, len(verifier.seen), scn.advanceCalls, leaser.released)
	}
}
