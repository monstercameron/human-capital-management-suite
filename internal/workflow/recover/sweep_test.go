package recover_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	wfrecover "github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var sweeperIdentity = lease.Identity{WorkloadRef: "workload:hcmnext-serve-recovery", InstanceRef: "replica:sweeper"}

// orphanedInstance creates an instance whose start node is READY and whose
// WORKFLOW_INSTANCE lease worker A took at bootAt and never released: the
// exact signature a driver that died mid-drain leaves.
func orphanedInstance(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, key string) (uuid.UUID, lease.Fence) {
	t.Helper()
	ctx := context.Background()
	plan := referencePlan(t)
	inst, err := runtime.NewInstance(tenant, uuid.New(), "cell-local", plan, workflow.ModeSimulate, "sha256:input-snapshot", "corr-"+key, bootAt)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := plan.Node(plan.StartNodeID)
	var fence lease.Fence
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		store := runtime.Store{}
		stored, err := store.CreateInstance(ctx, tx, inst)
		if err != nil {
			return err
		}
		ready := runtime.NewNodeExecution(tenant, inst.InstanceID, node.ID, 1, node.Type, runtime.NodeReady)
		ready.RecordedAt = bootAt
		if _, _, err := store.RecordNodeExecution(ctx, tx, ready, stored.InstanceVersion); err != nil {
			return err
		}
		grant, err := lease.Manager{}.Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: tenant, Resource: lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: inst.InstanceID.String()},
			Holder: workerA, Now: bootAt, TTL: leaseTTL,
		})
		fence = grant.Fence
		return err
	})
	return inst.InstanceID, fence
}

// recordingRedeliverer records every redelivery and the fence its context
// carried, and fails while fail is set.
type recordingRedeliverer struct {
	mu     sync.Mutex
	calls  []wfrecover.Redelivery
	fences []runtime.Fence
	fail   error
	hold   time.Duration
}

func (r *recordingRedeliverer) Redeliver(ctx context.Context, req wfrecover.Redelivery) error {
	fence, _ := execute.FenceFromContext(ctx)
	if r.hold > 0 {
		time.Sleep(r.hold)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	r.fences = append(r.fences, fence)
	return r.fail
}

func (r *recordingRedeliverer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func newTestSweeper(t *testing.T, conn *pgxadapter.Conn, redeliver wfrecover.Redeliverer, fp wfrecover.Failpoint) wfrecover.Sweeper {
	t.Helper()
	s, err := wfrecover.NewSweeper(wfrecover.SweeperOptions{
		DB: conn, Holder: sweeperIdentity, LeaseTTL: time.Minute, Redeliver: redeliver, Failpoints: fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func latestInstanceLease(t *testing.T, db *pgtest.DB, tenant, instanceID uuid.UUID) (state, holder string, token int64) {
	t.Helper()
	if err := db.QueryRow(context.Background(), `SELECT lease_state, holder_id, fence_token FROM workflow_lease
		WHERE tenant_id = $1 AND resource_kind = 'WORKFLOW_INSTANCE' AND resource_id = $2 ORDER BY fence_token DESC LIMIT 1`,
		tenant, instanceID.String()).Scan(&state, &holder, &token); err != nil {
		t.Fatalf("read instance lease: %v", err)
	}
	return state, holder, token
}

func findOrphans(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, at time.Time) []wfrecover.Orphan {
	t.Helper()
	var out []wfrecover.Orphan
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		out, err = wfrecover.FindOrphans(context.Background(), tx, tenant, at, 10)
		return err
	})
	return out
}

// TestNewSweeperValidatesWiring refuses a sweeper that could not claim or
// redeliver honestly.
func TestNewSweeperValidatesWiring(t *testing.T) {
	redeliver := wfrecover.RedelivererFunc(func(context.Context, wfrecover.Redelivery) error { return nil })
	for name, opts := range map[string]wfrecover.SweeperOptions{
		"no database":     {Holder: sweeperIdentity, LeaseTTL: time.Minute, Redeliver: redeliver},
		"no redeliverer":  {DB: &pgxadapter.Conn{}, Holder: sweeperIdentity, LeaseTTL: time.Minute},
		"no lease ttl":    {DB: &pgxadapter.Conn{}, Holder: sweeperIdentity, Redeliver: redeliver},
		"negative limit":  {DB: &pgxadapter.Conn{}, Holder: sweeperIdentity, LeaseTTL: time.Minute, Redeliver: redeliver, Limit: -1},
		"bare host owner": {DB: &pgxadapter.Conn{}, Holder: lease.Identity{WorkloadRef: "host-1", InstanceRef: "1"}, LeaseTTL: time.Minute, Redeliver: redeliver},
	} {
		if _, err := wfrecover.NewSweeper(opts); !errors.Is(err, wfrecover.ErrInvalid) {
			t.Errorf("%s: NewSweeper = %v, want ErrInvalid", name, err)
		}
	}
	s, err := wfrecover.NewSweeper(wfrecover.SweeperOptions{DB: &pgxadapter.Conn{}, Holder: sweeperIdentity, LeaseTTL: time.Minute, Redeliver: redeliver})
	if err != nil {
		t.Fatalf("valid sweeper refused: %v", err)
	}
	if _, err := s.Sweep(context.Background(), uuid.Nil, deadAt); !errors.Is(err, wfrecover.ErrInvalid) {
		t.Fatalf("Sweep without a tenant = %v, want ErrInvalid", err)
	}
	if _, err := s.Sweep(context.Background(), uuid.New(), time.Time{}); !errors.Is(err, wfrecover.ErrInvalid) {
		t.Fatalf("Sweep without an instant = %v, want ErrInvalid", err)
	}
	if _, err := wfrecover.FindOrphans(context.Background(), nil, uuid.New(), deadAt, 0); !errors.Is(err, wfrecover.ErrInvalid) {
		t.Fatalf("FindOrphans with no limit = %v, want ErrInvalid", err)
	}
	for _, phase := range wfrecover.SweepPhases() {
		if phase.Valid() {
			t.Fatalf("sweep phase %s is listed among Recoverer.Recover's own boundaries", phase)
		}
	}
	receipt := wfrecover.SweepReceipt{Items: []wfrecover.SweepItem{{Outcome: wfrecover.SweepRedelivered}, {Outcome: wfrecover.SweepContended}, {Outcome: wfrecover.SweepRedelivered}}}
	if receipt.Count(wfrecover.SweepRedelivered) != 2 || receipt.Count(wfrecover.SweepFailed) != 0 {
		t.Fatalf("receipt counts = %d/%d, want 2/0", receipt.Count(wfrecover.SweepRedelivered), receipt.Count(wfrecover.SweepFailed))
	}
}

// TestTodo_WF_RUN_003_Sweep proves the orphan signature and the claim: only an
// instance whose lease lapsed with READY work and no ready-work claim is
// found, the sweep takes it over under a fresh fence and hands that fence to
// the redelivery, and a redelivered instance is not found again.
func TestTodo_WF_RUN_003_Sweep(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun003-sweep")
	conn := appConn(t, db)
	ctx := context.Background()

	orphan, deadFence := orphanedInstance(t, conn, tenant, "orphan")
	claimed, _ := orphanedInstance(t, conn, tenant, "claimed")
	released, releasedFence := orphanedInstance(t, conn, tenant, "released")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		// The scheduler owns an instance with a live ready-work row.
		if err := (runtimestate.ReadyWorkStore{}).Enqueue(ctx, tx, runtimestate.ReadyWork{
			TenantID: tenant, ReadyWorkID: uuid.New(), InstanceID: claimed, NodeID: referencePlan(t).StartNodeID,
			Attempt: 1, State: runtimestate.ReadyReady, Priority: 100, EligibleAt: bootAt, EnqueuedAt: bootAt,
		}); err != nil {
			return err
		}
		// A driver that finished (or failed and returned) released its lease.
		_, err := lease.Manager{}.Release(ctx, tx, releasedFence, bootAt.Add(time.Second))
		return err
	})

	if got := findOrphans(t, conn, tenant, bootAt.Add(leaseTTL/2)); len(got) != 0 {
		t.Fatalf("orphans while the lease is live = %+v, want none", got)
	}
	got := findOrphans(t, conn, tenant, deadAt)
	if len(got) != 1 || got[0].InstanceID != orphan || got[0].HolderID != workerA.HolderID() || got[0].Token != deadFence.Token ||
		got[0].Status != runtime.InstanceCreated || len(got[0].Nodes) != 1 || got[0].Nodes[0].Status != runtime.NodeReady {
		t.Fatalf("orphans after the lapse = %+v, want only %s (not %s or %s)", got, orphan, claimed, released)
	}
	if other := findOrphans(t, conn, insertTenant(t, db, "wfrun003-sweep-other"), deadAt); len(other) != 0 {
		t.Fatalf("another tenant sees orphans %+v", other)
	}

	redeliver := &recordingRedeliverer{}
	receipt, err := newTestSweeper(t, conn, redeliver, nil).Sweep(ctx, tenant, deadAt)
	if err != nil || receipt.Count(wfrecover.SweepRedelivered) != 1 || redeliver.count() != 1 {
		t.Fatalf("Sweep = %+v, %v (redeliveries %d), want exactly one", receipt, err, redeliver.count())
	}
	fence := redeliver.fences[0]
	if fence.Token != deadFence.Token+1 || fence.HolderID != sweeperIdentity.HolderID() || fence.ResourceID != orphan.String() ||
		receipt.Items[0].Fence.Token != fence.Token || redeliver.calls[0].Lease.Kind != lease.TransitionTakenOver {
		t.Fatalf("redelivery fence = %+v (lease %+v), want the takeover's token %d", fence, redeliver.calls[0].Lease, deadFence.Token+1)
	}
	if state, holder, token := latestInstanceLease(t, db, tenant, orphan); state != "RELEASED" || holder != sweeperIdentity.HolderID() || token != int64(deadFence.Token+1) {
		t.Fatalf("lease after the sweep = %s/%s/%d, want RELEASED by the sweeper", state, holder, token)
	}
	again, err := newTestSweeper(t, conn, redeliver, nil).RunRecoveryRole(ctx, lease.AcquireRequest{TenantID: tenant}, deadAt.Add(time.Hour))
	if err != nil || again != 0 || redeliver.count() != 1 {
		t.Fatalf("second sweep redelivered %d, %v; want nothing", again, err)
	}
}

// TestTodo_WF_RUN_003_SweepFault proves every sweep failure leaves work
// recoverable: a failed redelivery and a sweeper that dies after claiming or
// after redelivering both leave the sweeper's own lease to lapse, and the
// next sweep past it takes the instance over again.
func TestTodo_WF_RUN_003_SweepFault(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun003-sweep-fault")
	conn := appConn(t, db)
	ctx := context.Background()
	orphan, deadFence := orphanedInstance(t, conn, tenant, "fault")

	failing := &recordingRedeliverer{fail: errors.New("cell unavailable")}
	receipt, err := newTestSweeper(t, conn, failing, nil).Sweep(ctx, tenant, deadAt)
	if !errors.Is(err, wfrecover.ErrEffect) || receipt.Count(wfrecover.SweepFailed) != 1 || receipt.Items[0].Err == nil {
		t.Fatalf("failed redelivery = %+v, %v; want one FAILED item and ErrEffect", receipt, err)
	}
	if state, holder, _ := latestInstanceLease(t, db, tenant, orphan); state != "HELD" || holder != sweeperIdentity.HolderID() {
		t.Fatalf("lease after a failed redelivery = %s/%s, want the sweeper's claim left to lapse", state, holder)
	}
	if got := findOrphans(t, conn, tenant, deadAt.Add(30*time.Second)); len(got) != 0 {
		t.Fatalf("orphans inside the sweeper's own claim = %+v, want none", got)
	}

	at := deadAt
	for i, phase := range wfrecover.SweepPhases() {
		at = at.Add(2 * time.Minute)
		redeliver := &recordingRedeliverer{}
		receipt, err := newTestSweeper(t, conn, redeliver, &wfrecover.CrashAt{Phase: phase}).Sweep(ctx, tenant, at)
		if !errors.Is(err, wfrecover.ErrCrashed) || wfrecover.PhaseOf(err) != phase || receipt.CrashedAt != phase {
			t.Fatalf("crash at %s = %+v, %v", phase, receipt, err)
		}
		wantCalls := 0
		if phase == wfrecover.SweepPhaseAfterRedelivery {
			wantCalls = 1
		}
		if redeliver.count() != wantCalls {
			t.Fatalf("crash at %s redelivered %d times, want %d", phase, redeliver.count(), wantCalls)
		}
		if state, holder, token := latestInstanceLease(t, db, tenant, orphan); state != "HELD" || holder != sweeperIdentity.HolderID() || token != int64(deadFence.Token)+int64(i)+2 {
			t.Fatalf("lease after a crash at %s = %s/%s/%d", phase, state, holder, token)
		}
	}

	at = at.Add(2 * time.Minute)
	redeliver := &recordingRedeliverer{}
	receipt, err = newTestSweeper(t, conn, redeliver, nil).Sweep(ctx, tenant, at)
	if err != nil || receipt.Count(wfrecover.SweepRedelivered) != 1 || redeliver.count() != 1 {
		t.Fatalf("sweep after every crash = %+v, %v; want the instance redelivered", receipt, err)
	}
}

// TestTodo_WF_RUN_003_SweepRace runs two sweepers on separate connections
// against the same orphan and proves exactly one redelivers it.
func TestTodo_WF_RUN_003_SweepRace(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun003-sweep-race")
	setup := appConn(t, db)
	orphan, _ := orphanedInstance(t, setup, tenant, "race")
	redeliver := &recordingRedeliverer{hold: 200 * time.Millisecond}

	const sweepers = 4
	receipts := make([]wfrecover.SweepReceipt, sweepers)
	errs := make([]error, sweepers)
	var ready, done sync.WaitGroup
	ready.Add(1)
	for i := 0; i < sweepers; i++ {
		s := newTestSweeper(t, appConn(t, db), redeliver, nil)
		done.Add(1)
		go func(i int) {
			defer done.Done()
			ready.Wait()
			receipts[i], errs[i] = s.Sweep(context.Background(), tenant, deadAt)
		}(i)
	}
	ready.Done()
	done.Wait()
	total := 0
	for i := range receipts {
		if errs[i] != nil {
			t.Fatalf("sweeper %d: %v", i, errs[i])
		}
		total += receipts[i].Count(wfrecover.SweepRedelivered)
	}
	if total != 1 || redeliver.count() != 1 || redeliver.calls[0].Orphan.InstanceID != orphan {
		t.Fatalf("concurrent sweepers redelivered %d times (calls %d): %+v", total, redeliver.count(), receipts)
	}
}

// TestTodo_WF_RUN_003_SweepStaleClaim proves a ready-work row whose node
// attempt already finished does not hide the READY successor a dispatcher
// that died mid-drain never ran, while the same row for an unfinished
// attempt still does.
func TestTodo_WF_RUN_003_SweepStaleClaim(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun003-sweep-stale")
	conn := appConn(t, db)
	ctx := context.Background()
	plan := referencePlan(t)
	instanceID, _ := orphanedInstance(t, conn, tenant, "stale")

	var successor workflow.CompiledNode
	for _, node := range plan.Nodes {
		if node.ID != plan.StartNodeID {
			successor = node
			break
		}
	}
	start := plan.StartNodeID
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		// The dispatched claim on the start node is live while its attempt is.
		return (runtimestate.ReadyWorkStore{}).Enqueue(ctx, tx, runtimestate.ReadyWork{
			TenantID: tenant, ReadyWorkID: uuid.New(), InstanceID: instanceID, NodeID: start,
			Attempt: 1, State: runtimestate.ReadyReady, Priority: 100, EligibleAt: bootAt, EnqueuedAt: bootAt,
		})
	})
	if got := findOrphans(t, conn, tenant, deadAt); len(got) != 0 {
		t.Fatalf("orphans behind a live claim = %+v, want none", got)
	}

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		store := runtime.Store{}
		inst, err := store.LoadInstance(ctx, tx, tenant, instanceID)
		if err != nil {
			return err
		}
		version := inst.InstanceVersion
		for _, status := range []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded} {
			step := runtime.NodeTransition{TenantID: tenant, InstanceID: instanceID, NodeID: start, Attempt: 1, ExpectedInstanceVersion: version, Status: status}
			if status == runtime.NodeSucceeded {
				at := bootAt
				step.CompletedAt = &at
			}
			if _, version, err = store.RecordNodeTransition(ctx, tx, step); err != nil {
				return err
			}
		}
		_, _, err = store.RecordNodeExecution(ctx, tx, runtime.NewNodeExecution(tenant, instanceID, successor.ID, 1, successor.Type, runtime.NodeReady), version)
		return err
	})
	got := findOrphans(t, conn, tenant, deadAt)
	if len(got) != 1 || got[0].InstanceID != instanceID || len(got[0].Nodes) != 1 || got[0].Nodes[0].NodeID != successor.ID {
		t.Fatalf("orphans behind a stale claim = %+v, want %s open at %s", got, instanceID, successor.ID)
	}
	redeliver := &recordingRedeliverer{}
	receipt, err := newTestSweeper(t, conn, redeliver, nil).Sweep(ctx, tenant, deadAt)
	if err != nil || receipt.Count(wfrecover.SweepRedelivered) != 1 {
		t.Fatalf("Sweep behind a stale claim = %+v, %v; want the successor redelivered", receipt, err)
	}
}
