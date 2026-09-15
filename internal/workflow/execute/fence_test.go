package execute

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type fakeVerifier struct {
	err  error
	seen []runtime.Fence
}

func (v *fakeVerifier) VerifyFence(_ context.Context, _ runtime.Executor, _ uuid.UUID, f runtime.Fence) error {
	v.seen = append(v.seen, f)
	return v.err
}

type fakeLeaser struct {
	acquireErr error
	releaseErr error
	acquired   int
	released   int
	fence      runtime.Fence
}

func (l *fakeLeaser) AcquireInstance(_ context.Context, _ runtime.Executor, _, instanceID uuid.UUID, at time.Time) (runtime.Fence, error) {
	l.acquired++
	if l.acquireErr != nil {
		return runtime.Fence{}, l.acquireErr
	}
	l.fence = runtime.Fence{ResourceKind: "WORKFLOW_INSTANCE", ResourceID: instanceID.String(), LeaseID: uuid.New(), HolderID: "workload:test#replica:1", Token: 7, At: at}
	return l.fence, nil
}

func (l *fakeLeaser) ReleaseInstance(_ context.Context, _ runtime.Executor, _ uuid.UUID, f runtime.Fence, _ time.Time) error {
	l.released++
	if f.LeaseID != l.fence.LeaseID {
		return errors.New("released a fence that was not acquired")
	}
	return l.releaseErr
}

// TestFencedRunsVerifyBeforeStepsAndLeaseAroundTheRun proves WF-RUN-036's
// driver contract: a refused fence stops the run before any in-transaction
// step or advancement; a caller-held fence on the context takes precedence
// over leasing; a configured leaser acquires the instance lease before the
// run and releases it afterwards even when the run fails; a refused
// acquisition advances nothing; and Leases without a verifier is refused at
// construction.
func TestFencedRunsVerifyBeforeStepsAndLeaseAroundTheRun(t *testing.T) {
	ctx := context.Background()

	// A caller-held stale fence is refused before the claimed step runs.
	scn := newOBSScenario(t, "")
	runner := &txStepRunner{claim: "end", scn: scn}
	verifier := &fakeVerifier{err: errors.New("FENCE_STALE")}
	d := scn.driver(t)
	d.opts.Steps, d.opts.FenceVerifier = runner, verifier
	held := runtime.Fence{ResourceKind: "WORKFLOW_INSTANCE", ResourceID: scn.instanceID.String(), LeaseID: uuid.New(), HolderID: "workload:sched#replica:9", Token: 3}
	_, err := d.Resume(WithFence(ctx, held), scn.req)
	if !errors.Is(err, ErrFenceRefused) || scn.advanceCalls != 0 || runner.txCalls != 0 {
		t.Fatalf("stale caller fence: err %v, advances %d, in-tx steps %d; want refusal before anything ran", err, scn.advanceCalls, runner.txCalls)
	}
	if len(verifier.seen) != 1 || verifier.seen[0].Token != 3 || verifier.seen[0].At.IsZero() {
		t.Fatalf("verified fences = %+v, want the caller's token stamped with the call instant", verifier.seen)
	}

	// A caller-held fence means the driver takes no lease of its own.
	scn = newOBSScenario(t, "")
	leaser := &fakeLeaser{}
	d = scn.driver(t)
	d.opts.FenceVerifier, d.opts.Leases = &fakeVerifier{err: errors.New("stop after verification")}, leaser
	if _, err := d.Resume(WithFence(ctx, held), scn.req); err == nil || leaser.acquired != 0 {
		t.Fatalf("caller fence present: err %v, acquired %d; want no lease taken", err, leaser.acquired)
	}

	// Without a caller fence the leaser acquires, the run presents that
	// fence, and the lease is released even though the run failed.
	scn = newOBSScenario(t, "")
	leaser = &fakeLeaser{}
	verifier = &fakeVerifier{err: errors.New("stop after verification")}
	d = scn.driver(t)
	d.opts.FenceVerifier, d.opts.Leases = verifier, leaser
	if _, err := d.Resume(ctx, scn.req); err == nil {
		t.Fatal("run unexpectedly succeeded")
	}
	if leaser.acquired != 1 || leaser.released != 1 || len(verifier.seen) != 1 || verifier.seen[0].LeaseID != leaser.fence.LeaseID {
		t.Fatalf("leased run: acquired %d, released %d, verified %+v; want the acquired fence presented and released", leaser.acquired, leaser.released, verifier.seen)
	}

	// A release failure is reported without masking the run's own error.
	scn = newOBSScenario(t, "")
	leaser = &fakeLeaser{releaseErr: errors.New("lease store unavailable")}
	d = scn.driver(t)
	d.opts.FenceVerifier, d.opts.Leases = &fakeVerifier{err: errors.New("FENCE_STALE")}, leaser
	if _, err := d.Resume(ctx, scn.req); !errors.Is(err, ErrFenceRefused) || !strings.Contains(err.Error(), "release instance lease") {
		t.Fatalf("release failure = %v; want both the refusal and the release error", err)
	}

	// A refused acquisition advances nothing.
	scn = newOBSScenario(t, "")
	leaser = &fakeLeaser{acquireErr: errors.New("LEASE_HELD")}
	d = scn.driver(t)
	d.opts.FenceVerifier, d.opts.Leases = &fakeVerifier{}, leaser
	if _, err := d.Resume(ctx, scn.req); !errors.Is(err, ErrFenceRefused) || scn.advanceCalls != 0 || leaser.released != 0 {
		t.Fatalf("contended instance: err %v, advances %d, released %d", err, scn.advanceCalls, leaser.released)
	}

	// A fence presented with no verifier configured is invalid, never skipped.
	scn = newOBSScenario(t, "")
	d = scn.driver(t)
	if _, err := d.Resume(WithFence(ctx, held), scn.req); err == nil || !strings.Contains(err.Error(), "no FenceVerifier") {
		t.Fatalf("fence without verifier = %v", err)
	}

	if _, err := New(Options{DB: oneBeginner{&memoryTx{}}, Steps: noStepRunner{}, Leases: &fakeLeaser{}}); err == nil {
		t.Fatal("Leases without a FenceVerifier was accepted")
	}
	if _, err := New(Options{DB: oneBeginner{&memoryTx{}}, Steps: noStepRunner{}, Leases: &fakeLeaser{}, FenceVerifier: &fakeVerifier{}}); err != nil {
		t.Fatalf("Leases with a verifier = %v", err)
	}
	if _, err := New(Options{DB: oneBeginner{&memoryTx{}}, Steps: noStepRunner{}, FenceVerifier: &fakeVerifier{}}); err == nil {
		t.Fatal("a verifier with neither a fence nor leases was accepted")
	}
	if f, ok := fenceFromContext(WithFence(ctx, runtime.Fence{})); ok || f.ResourceID != "" {
		t.Fatal("an empty fence on the context was treated as held")
	}
	d = scn.driver(t)
	if _, _, err := d.acquireInstanceLease(ctx, uuid.Nil, uuid.New()); err != nil {
		t.Fatalf("no leaser configured: %v", err)
	}
	d.opts.Leases = &fakeLeaser{}
	if _, _, err := d.acquireInstanceLease(ctx, uuid.Nil, uuid.New()); err == nil {
		t.Fatal("a lease was requested without a tenant")
	}
}
