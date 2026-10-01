package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	executionscheduler "github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// fakeRecoveryRole reports a fixed count or error and records its calls.
type fakeRecoveryRole struct {
	mu    sync.Mutex
	count int
	err   error
	calls int
	order *[]string
	name  string
}

func (r *fakeRecoveryRole) RunRecoveryRole(context.Context, lease.AcquireRequest, time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.order != nil {
		*r.order = append(*r.order, r.name)
	}
	return r.count, r.err
}

// fakeWaker records every tick and reports a fixed count or error.
type fakeWaker struct {
	mu      sync.Mutex
	count   int
	err     error
	tenants []string
	nows    []time.Time
}

func (w *fakeWaker) TickTenant(_ context.Context, tenant string, now time.Time) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tenants = append(w.tenants, tenant)
	w.nows = append(w.nows, now)
	return w.count, w.err
}

func (w *fakeWaker) ticks() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.tenants)
}

var schedulerRoleNow = time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

// TestTodo_AGENT2_011_SchedulerRole proves the recovery role the scheduler is
// composed with runs the workflow sweep and then the agent wake role, sums
// their counts, joins their errors so neither hides the other, tolerates a
// cell with no agent runtime, and is safe under concurrent ticks.
func TestTodo_AGENT2_011_SchedulerRole(t *testing.T) {
	ctx := context.Background()
	claim := lease.AcquireRequest{}

	t.Run("runs both roles in order and sums counts", func(t *testing.T) {
		var order []string
		wf := &fakeRecoveryRole{count: 2, order: &order, name: "workflow"}
		waker := &fakeWaker{count: 3}
		role := newSchedulerRecoveryRole(wf, "acme-corp", &app.Cell{AgentWaker: waker})
		n, err := role.RunRecoveryRole(ctx, claim, schedulerRoleNow)
		if err != nil || n != 5 {
			t.Fatalf("role = %d, %v, want 5 (2 workflow + 3 agent)", n, err)
		}
		if wf.calls != 1 || waker.ticks() != 1 || waker.tenants[0] != "acme-corp" || !waker.nows[0].Equal(schedulerRoleNow) {
			t.Fatalf("workflow calls %d, waker ticks %v at %v, want one each for the served tenant at the tick time", wf.calls, waker.tenants, waker.nows)
		}
		if len(order) != 1 || order[0] != "workflow" {
			t.Fatalf("order = %v", order)
		}
	})
	t.Run("joins errors and neither hides the other", func(t *testing.T) {
		wfErr, agentErr := errors.New("workflow sweep failed"), errors.New("agent tick failed")
		wf := &fakeRecoveryRole{count: 1, err: wfErr}
		waker := &fakeWaker{count: 4, err: agentErr}
		n, err := newSchedulerRecoveryRole(wf, "acme-corp", &app.Cell{AgentWaker: waker}).RunRecoveryRole(ctx, claim, schedulerRoleNow)
		if !errors.Is(err, wfErr) || !errors.Is(err, agentErr) || n != 5 {
			t.Fatalf("role = %d, %v, want both errors joined and both counts summed", n, err)
		}
		if waker.ticks() != 1 {
			t.Fatal("a failing workflow sweep stopped the agent tick")
		}
		wf2 := &fakeRecoveryRole{}
		_, err = newSchedulerRecoveryRole(wf2, "acme-corp", &app.Cell{AgentWaker: &fakeWaker{err: agentErr}}).RunRecoveryRole(ctx, claim, schedulerRoleNow)
		if !errors.Is(err, agentErr) || wf2.calls != 1 {
			t.Fatalf("agent-only failure = %v (workflow calls %d)", err, wf2.calls)
		}
	})
	t.Run("a cell without an agent runtime ticks nothing", func(t *testing.T) {
		wf := &fakeRecoveryRole{count: 2}
		for name, cell := range map[string]*app.Cell{"nil waker": {}, "nil cell": nil} {
			n, err := newSchedulerRecoveryRole(wf, "acme-corp", cell).RunRecoveryRole(ctx, claim, schedulerRoleNow)
			if err != nil || n != 2 {
				t.Fatalf("%s: role = %d, %v, want the workflow count and no error", name, n, err)
			}
		}
		if n, err := (agentWakeRole{}).RunRecoveryRole(ctx, claim, schedulerRoleNow); n != 0 || err != nil {
			t.Fatalf("zero role = %d, %v", n, err)
		}
	})
	t.Run("the waker is read at tick time", func(t *testing.T) {
		cell := &app.Cell{}
		role := newSchedulerRecoveryRole(&fakeRecoveryRole{}, "acme-corp", cell)
		if _, err := role.RunRecoveryRole(ctx, claim, schedulerRoleNow); err != nil {
			t.Fatal(err)
		}
		waker := &fakeWaker{count: 1}
		cell.AgentWaker = waker
		if n, err := role.RunRecoveryRole(ctx, claim, schedulerRoleNow); err != nil || n != 1 || waker.ticks() != 1 {
			t.Fatalf("after the waker was installed: %d, %v, ticks %d", n, err, waker.ticks())
		}
	})
	t.Run("concurrent ticks all reach the waker", func(t *testing.T) {
		waker := &fakeWaker{count: 1}
		role := newSchedulerRecoveryRole(&fakeRecoveryRole{}, "acme-corp", &app.Cell{AgentWaker: waker})
		var wg sync.WaitGroup
		errs := make(chan error, 16)
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := role.RunRecoveryRole(ctx, claim, schedulerRoleNow)
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if waker.ticks() != 16 {
			t.Fatalf("waker ticks = %d, want every replica tick delivered (dedupe is the runtime's job)", waker.ticks())
		}
	})
}

// TestTodo_AGENT2_011_SchedulerRoleTick invokes the fan-out from a real
// executionscheduler tick over PostgreSQL, the way composeSchedulerWorkload
// wires it: the tick reaches the agent waker for the served tenant with the
// tick's clock, counts what the waker moved, and survives its failure.
func TestTodo_AGENT2_011_SchedulerRoleTick(t *testing.T) {
	db := pgtest.New(t)
	const tenantKey = "agent-wake-role"
	tenantID := pgstore.TenantID(tenantKey)
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', $4)`, tenantID, tenantKey, tenantKey, schedulerRoleNow.Add(-time.Hour))
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	claim := lease.AcquireRequest{
		TenantID: tenantID,
		Resource: lease.Resource{Kind: lease.ResourceQueue, ID: executionscheduler.DefaultQueueKey},
		Holder:   lease.Identity{WorkloadRef: "workload:hcmnext-serve", InstanceRef: "replica:agent-wake"},
	}
	waker := &fakeWaker{count: 2}
	wf := &fakeRecoveryRole{count: 1}
	newScheduler := func(cell *app.Cell) *executionscheduler.Scheduler {
		s, err := executionscheduler.New(executionscheduler.Config{
			DB: conn, Claims: []lease.AcquireRequest{claim}, Leases: lease.Manager{},
			Misfire:      schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour},
			RecoveryRole: newSchedulerRecoveryRole(wf, tenantKey, cell),
			Clock:        func() time.Time { return schedulerRoleNow },
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := newScheduler(&app.Cell{AgentWaker: waker})
	tick, err := s.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if tick.Redelivered != 3 || tick.Idle() {
		t.Fatalf("tick redelivered %d, want the workflow and agent counts summed to 3", tick.Redelivered)
	}
	if waker.ticks() != 1 || waker.tenants[0] != tenantKey || !waker.nows[0].Equal(schedulerRoleNow) {
		t.Fatalf("waker ticks %v at %v, want the served tenant at the scheduler clock", waker.tenants, waker.nows)
	}

	waker.err = errors.New("agent runtime unavailable")
	if _, err := s.Tick(context.Background()); err != nil {
		t.Fatalf("a failing agent tick stopped the scheduler tick: %v", err)
	}
	if waker.ticks() != 2 || wf.calls != 2 {
		t.Fatalf("after a failing agent tick: waker %d, workflow %d, want both to keep running every tick", waker.ticks(), wf.calls)
	}
	if _, err := newScheduler(&app.Cell{}).Tick(context.Background()); err != nil {
		t.Fatalf("tick with no agent runtime: %v", err)
	}
	if waker.ticks() != 2 {
		t.Fatal("a cell with no waker reached a waker")
	}
}
