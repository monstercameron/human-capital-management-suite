package agentsystem

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_AGENT2_011_DeliveryRecoveryRestart parks a task on a durable timer
// in PostgreSQL, restarts the platform over fresh connections, and ticks it
// from two "replicas" at once: the due timer resumes the task exactly once
// through the durable dedupe inbox, and a lost wake settles as LOST_WAKE.
func TestTodo_AGENT2_011_DeliveryRecoveryRestart(t *testing.T) {
	db := pgtest.New(t)
	tenantUUID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantUUID, tenantKey, tenantKey)
	mapper := func(key values.TenantId) uuid.UUID {
		if key == tenantKey {
			return tenantUUID
		}
		return uuid.Nil
	}
	appConn := func() *pgxadapter.Conn {
		conn := db.NewConn(t)
		if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
			t.Fatalf("assume app role: %v", err)
		}
		return conn
	}
	stores := func() (*agentdelegationstore.Store, *agentrunstore.Store) {
		grants, err := agentdelegationstore.New(appConn(), mapper)
		if err != nil {
			t.Fatal(err)
		}
		tasks, err := agentrunstore.New(appConn(), mapper)
		if err != nil {
			t.Fatal(err)
		}
		return grants, tasks
	}

	grants, tasks := stores()
	f := newFixtureWith(t, fixtureStores{Grants: grants, Tasks: tasks})
	f.defaultOwner(t)
	due := fixedNow.Add(time.Hour)
	parked := f.startParked(t, "task-pg-timer", timerWait("wait", due), signalWait("hold", "later", "", time.Time{}))
	lost := f.startParked(t, "task-pg-lost", signalWait("wait", "never", "", fixedNow.Add(2*time.Hour)))
	if got := f.get(t, "task-pg-timer"); got.Wake == nil || got.Wake.Kind != agentrun.WakeTimer || !got.Wake.DueAt.Equal(due) || got.Plan.Steps[0].Wait == nil || got.Plan.Steps[0].Wait.Key != "promotion-due" {
		t.Fatalf("stored parked task = %+v, want the timer condition and the step's wait spec to round-trip", got.Wake)
	}

	// Restart: new connections, new stores, new platforms. Nothing but the
	// database survives. Each replica owns its connection, as a separate
	// process would.
	cfg := f.platform.cfg
	var replicas []*Platform
	for range 4 {
		g, tk := stores()
		cfg.Grants, cfg.Tasks = g, tk
		p, err := NewPlatform(cfg)
		if err != nil {
			t.Fatal(err)
		}
		replicas = append(replicas, p)
	}
	restarted := replicas[0]

	if n, err := restarted.TickTenant(context.Background(), tenantKey, fixedNow); err != nil || n != 0 {
		t.Fatalf("not-due tick after restart = %d, %v", n, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(replicas))
	for _, p := range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.TickTenant(context.Background(), tenantKey, due.Add(time.Minute))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("replica tick: %v", err)
		}
	}
	got := f.get(t, "task-pg-timer")
	if got.State != agentrun.StateWaiting || got.Plan.Steps[0].State != agentrun.StepCompleted || got.CurrentStep != 1 || got.Plan.Steps[1].State != agentrun.StepWaiting {
		t.Fatalf("task = %s step0 %s cursor %d, want the timer step completed and the task parked on the next wait", got.State, got.Plan.Steps[0].State, got.CurrentStep)
	}
	if wantRef := fmt.Sprintf("wake:timer:task-pg-timer:%d:%d", parked.Version, due.UnixNano()); got.Plan.Steps[0].ResultRef != wantRef || stepResults(got) != 1 {
		t.Fatalf("timer result %q with %d ledger results, want the deterministic event %q resumed once", got.Plan.Steps[0].ResultRef, stepResults(got), wantRef)
	}

	// The lost wake: past its stale deadline the sweeper resolves it.
	if n, err := restarted.TickTenant(context.Background(), tenantKey, lost.Wake.StaleAfter.Add(time.Second)); err != nil || n != 1 {
		t.Fatalf("stale tick = %d, %v, want the lost wake settled", n, err)
	}
	if gone := f.get(t, "task-pg-lost"); gone.State != agentrun.StateFailed || gone.FailureCode != "LOST_WAKE" {
		t.Fatalf("lost task = %s %s, want FAILED LOST_WAKE", gone.State, gone.FailureCode)
	}
}
