package execute

import (
	"context"
	"errors"
	"sync"
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

func TestTodo_WF_RUN_036_Fault(t *testing.T) {
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

// TestTodo_WF_RUN_036_Race proves concurrent resumes keep each caller's
// fence attached to its own request. A shared or overwritten context fence
// could let one holder's authority be checked for another instance.
func TestTodo_WF_RUN_036_Race(t *testing.T) {
	type result struct {
		instance uuid.UUID
		seen     []runtime.Fence
		err      error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for i := range results {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			scn := newOBSScenario(t, "")
			verifier := &fakeVerifier{err: errors.New("FENCE_STALE")}
			d := scn.driver(t)
			d.opts.FenceVerifier = verifier
			fence := runtime.Fence{ResourceKind: "WORKFLOW_INSTANCE", ResourceID: scn.instanceID.String(), LeaseID: uuid.New(), HolderID: "workload:test#replica:race", Token: uint64(i + 1)}
			_, err := d.Resume(WithFence(context.Background(), fence), scn.req)
			results[i] = result{instance: scn.instanceID, seen: verifier.seen, err: err}
		}()
	}
	wg.Wait()
	for i, got := range results {
		if !errors.Is(got.err, ErrFenceRefused) || len(got.seen) != 1 || got.seen[0].ResourceID != got.instance.String() || got.seen[0].Token != uint64(i+1) {
			t.Errorf("request %d: instance=%s verified=%+v err=%v", i, got.instance, got.seen, got.err)
		}
	}
}

// TestTodo_WF_RUN_036_Mutation proves the caller-held instance fence is
// checked by the same transaction that would commit the advancement. The
// verifier intentionally refuses after recording its inputs; if the driver
// drops fence propagation, verifies outside the transaction, or advances
// before verification, one of the assertions below fails.
func TestTodo_WF_RUN_036_Mutation(t *testing.T) {
	scn := newOBSScenario(t, "")
	d := scn.driver(t)
	tx := &memoryTx{}
	d.opts.DB = oneBeginner{tx}
	wantFence := runtime.Fence{
		ResourceKind: "WORKFLOW_INSTANCE",
		ResourceID:   scn.instanceID.String(),
		LeaseID:      uuid.New(),
		HolderID:     "workload:test#replica:mutation",
		Token:        19,
	}
	verifier := &transactionFenceVerifier{wantTx: tx, wantTenant: scn.tenantID, wantFence: wantFence, err: errors.New("FENCE_STALE")}
	d.opts.FenceVerifier = verifier

	_, err := d.Resume(WithFence(context.Background(), wantFence), scn.req)
	if !errors.Is(err, ErrFenceRefused) {
		t.Fatalf("Resume error = %v, want a refusal from the stale fence", err)
	}
	if verifier.calls != 1 || verifier.gotTx != tx || verifier.gotTenant != scn.tenantID {
		t.Fatalf("fence verifier calls=%d tx=%p tenant=%s; want one call in advancement tx %p for tenant %s",
			verifier.calls, verifier.gotTx, verifier.gotTenant, tx, scn.tenantID)
	}
	if verifier.gotFence.ResourceKind != wantFence.ResourceKind || verifier.gotFence.ResourceID != wantFence.ResourceID ||
		verifier.gotFence.LeaseID != wantFence.LeaseID || verifier.gotFence.HolderID != wantFence.HolderID ||
		verifier.gotFence.Token != wantFence.Token || verifier.gotFence.At.IsZero() {
		t.Fatalf("verified fence = %+v, want caller identity/token with a stamped call time", verifier.gotFence)
	}
	if tx.committed || !tx.rolledBack || scn.advanceCalls != 0 || len(scn.stepRunner.traceIDs) != 0 || scn.terminal.calls != 0 {
		t.Fatalf("commit=%v rollback=%v advances=%d steps=%d terminal=%d; refusal must precede effects and roll back",
			tx.committed, tx.rolledBack, scn.advanceCalls, len(scn.stepRunner.traceIDs), scn.terminal.calls)
	}
}

type transactionFenceVerifier struct {
	wantTx     runtime.Executor
	wantTenant uuid.UUID
	wantFence  runtime.Fence
	err        error

	calls     int
	gotTx     runtime.Executor
	gotTenant uuid.UUID
	gotFence  runtime.Fence
}

func (v *transactionFenceVerifier) VerifyFence(_ context.Context, ex runtime.Executor, tenantID uuid.UUID, fence runtime.Fence) error {
	v.calls++
	v.gotTx, v.gotTenant, v.gotFence = ex, tenantID, fence
	return v.err
}
