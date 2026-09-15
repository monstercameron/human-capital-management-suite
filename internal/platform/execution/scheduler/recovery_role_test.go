package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	wfrecover "github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
	wfruntime "github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// countingRecoveryRole reports a fixed count, or an error, per call.
type countingRecoveryRole struct {
	mu    sync.Mutex
	calls int
	count int
	err   error
}

func (r *countingRecoveryRole) RunRecoveryRole(context.Context, lease.AcquireRequest, time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.count, r.err
}

// TestRecoveryRoleReportsItsRedeliveries pins the seam: a nil role redelivers
// nothing, a role's count reaches the tick result and its log line, and a
// role error is returned rather than swallowed.
func TestRecoveryRoleReportsItsRedeliveries(t *testing.T) {
	claim := claimFixture(uuid.New(), "replica:recovery")
	logger := &recordingLogger{}
	if n, err := runRecoveryRole(context.Background(), nil, logger, claim, fixtureAt); n != 0 || err != nil {
		t.Fatalf("nil role = %d, %v; want 0, nil", n, err)
	}
	quiet := &countingRecoveryRole{}
	if n, err := runRecoveryRole(context.Background(), quiet, logger, claim, fixtureAt); n != 0 || err != nil || quiet.calls != 1 {
		t.Fatalf("idle role = %d, %v (calls %d)", n, err, quiet.calls)
	}
	if logger.count("scheduler.recovery_role_tick") != 0 {
		t.Fatal("an idle recovery role logged a tick line")
	}
	busy := &countingRecoveryRole{count: 2}
	if n, err := runRecoveryRole(context.Background(), busy, logger, claim, fixtureAt); n != 2 || err != nil {
		t.Fatalf("busy role = %d, %v; want 2", n, err)
	}
	failing := &countingRecoveryRole{err: errors.New("sweep failed")}
	if _, err := runRecoveryRole(context.Background(), failing, logger, claim, fixtureAt); err == nil {
		t.Fatal("a failing recovery role returned no error")
	}
	if logger.count("scheduler.recovery_role_tick") != 1 || logger.count("scheduler.recovery_role_failed") != 1 {
		t.Fatalf("recovery role lines = %d tick / %d failed, want 1/1",
			logger.count("scheduler.recovery_role_tick"), logger.count("scheduler.recovery_role_failed"))
	}
	if (TickResult{Redelivered: 1}).Idle() {
		t.Fatal("a tick that redelivered an orphan reports idle")
	}
	total := TickResult{}
	total.add(TickResult{Redelivered: 2})
	total.add(TickResult{Redelivered: 3})
	if total.Redelivered != 5 {
		t.Fatalf("summed redeliveries = %d, want 5", total.Redelivered)
	}
}

// TestTodo_WF_RUN_003_SchedulerRecoveryRole runs the production recovery
// sweep as the scheduler's recovery role against PostgreSQL: an instance a
// dead driver left READY under a lapsed instance lease is redelivered once by
// a tick -- even a tick that serves no dispatcher -- and not again.
func TestTodo_WF_RUN_003_SchedulerRecoveryRole(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun003-scheduler-role", fixtureAt)
	_, plan, _ := publishActiveWaitPlan(t, fixtureFireAt, fixtureAt)
	conn := appConn(t, db)
	ctx := context.Background()

	inst, err := wfruntime.NewInstance(tenant, uuid.New(), "cell-local", plan, workflow.ModeExecute, "sha256:input", "corr-wfrun003-role", fixtureAt)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := plan.Node(plan.StartNodeID)
	dead := lease.Identity{WorkloadRef: "workload:hcmnext-execution", InstanceRef: "replica:dead"}
	inTenantTx(t, db, tenant, func(tx dbport.Tx) error {
		stored, err := (wfruntime.Store{}).CreateInstance(ctx, tx, inst)
		if err != nil {
			return err
		}
		ready := wfruntime.NewNodeExecution(tenant, inst.InstanceID, node.ID, 1, node.Type, wfruntime.NodeReady)
		if _, _, err := (wfruntime.Store{}).RecordNodeExecution(ctx, tx, ready, stored.InstanceVersion); err != nil {
			return err
		}
		_, err = lease.Manager{}.Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: tenant, Resource: lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: inst.InstanceID.String()},
			Holder: dead, Now: fixtureAt, TTL: time.Minute,
		})
		return err
	})

	var (
		mu     sync.Mutex
		fences []wfruntime.Fence
	)
	sweeper, err := wfrecover.NewSweeper(wfrecover.SweeperOptions{
		DB: conn, Holder: lease.Identity{WorkloadRef: "workload:hcmnext-serve-recovery", InstanceRef: "replica:role"},
		LeaseTTL: time.Minute, Leases: lease.Manager{},
		Redeliver: wfrecover.RedelivererFunc(func(ctx context.Context, req wfrecover.Redelivery) error {
			fence, _ := execute.FenceFromContext(ctx)
			mu.Lock()
			defer mu.Unlock()
			fences = append(fences, fence)
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := fixtureAt.Add(10 * time.Minute)
	s, err := New(Config{
		DB: conn, Claims: []lease.AcquireRequest{claimFixture(tenant, "replica:role")},
		Leases: lease.Manager{}, Misfire: catchUpOnce(), RecoveryRole: sweeper,
		Clock: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.Tick(ctx)
	if err != nil || first.Redelivered != 1 || first.Idle() {
		t.Fatalf("first tick = %+v, %v; want one redelivery", first, err)
	}
	mu.Lock()
	got := append([]wfruntime.Fence(nil), fences...)
	mu.Unlock()
	if len(got) != 1 || got[0].ResourceID != inst.InstanceID.String() || got[0].Token != 2 {
		t.Fatalf("redelivery fences = %+v, want one takeover fence at token 2", got)
	}
	clock = clock.Add(time.Hour)
	second, err := s.Tick(ctx)
	if err != nil || second.Redelivered != 0 {
		t.Fatalf("second tick = %+v, %v; want nothing left to redeliver", second, err)
	}

	// A failing sweep is logged and retried later; it never stops the tick.
	failing := &countingRecoveryRole{err: errors.New("redelivery failed")}
	logger := &recordingLogger{}
	withFailure, err := New(Config{
		DB: conn, Claims: []lease.AcquireRequest{claimFixture(tenant, "replica:role")},
		Leases: lease.Manager{}, Misfire: catchUpOnce(), RecoveryRole: failing, Logger: logger,
		Clock: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	if tick, err := withFailure.Tick(ctx); err != nil || failing.calls != 1 || logger.count("scheduler.recovery_role_failed") != 1 {
		t.Fatalf("tick with a failing recovery role = %+v, %v (calls %d); want the failure logged, not returned", tick, err, failing.calls)
	}
}
