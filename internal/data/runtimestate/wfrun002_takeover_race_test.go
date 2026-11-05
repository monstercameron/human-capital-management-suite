package runtimestate_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// TestTodo_WF_RUN_002_NoLostUpdateOnReleaseTakeoverRace is the WF-RUN-002
// adversarial-review regression: a takeover Acquire racing a committed Release
// must not overwrite the RELEASED row to EXPIRED. The interleaving is
// deterministic: B blocks on the release row lock inside the retire-UPDATE
// (observed via pg_stat_activity), A commits, B resumes.
func TestTodo_WF_RUN_002_NoLostUpdateOnReleaseTakeoverRace(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun002-race")
	connA := appConn(t, db)
	connB := appConn(t, db)
	probe := db.NewConn(t)
	store := runtimestate.LeaseStore{}
	resource := "res-race-1"

	leaseA := runtimestate.Lease{TenantID: tenant, LeaseID: uuid.New(),
		ResourceKind: runtimestate.LeaseWorkflowInstance, ResourceID: resource,
		HolderID:   "worker:a",
		AcquiredAt: fixedInstant, ExpiresAt: fixedInstant.Add(30 * time.Second)}
	inTenantTx(t, connA, tenant, func(tx dbport.Tx) error {
		_, err := store.Acquire(ctx, tx, leaseA, fixedInstant)
		return err
	})

	releaseAt := fixedInstant.Add(30 * time.Second)
	txA, err := connA.Begin(ctx)
	if err != nil {
		t.Fatalf("begin A: %v", err)
	}
	if err := tenancy.WithTenant(ctx, txA, tenant); err != nil {
		t.Fatalf("scope A: %v", err)
	}
	if err := store.Release(ctx, txA, tenant, leaseA.LeaseID, 1, releaseAt); err != nil {
		t.Fatalf("release A: %v", err)
	}

	type grant struct {
		held runtimestate.Lease
		err  error
	}
	bDone := make(chan grant, 1)
	go func() {
		var g grant
		txB, err := connB.Begin(ctx)
		if err != nil {
			g.err = err
			bDone <- g
			return
		}
		defer func() { _ = txB.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, txB, tenant); err != nil {
			g.err = err
			bDone <- g
			return
		}
		if _, err := txB.Exec(ctx, `SET LOCAL application_name = 'wfrun002-takeover-B'`); err != nil {
			g.err = err
			bDone <- g
			return
		}
		leaseB := runtimestate.Lease{TenantID: tenant, LeaseID: uuid.New(),
			ResourceKind: runtimestate.LeaseWorkflowInstance, ResourceID: resource,
			HolderID:   "worker:b",
			AcquiredAt: fixedInstant.Add(31 * time.Second), ExpiresAt: fixedInstant.Add(61 * time.Second)}
		g.held, g.err = store.Acquire(ctx, txB, leaseB, fixedInstant.Add(31*time.Second))
		if g.err != nil {
			bDone <- g
			return
		}
		g.err = txB.Commit(ctx)
		bDone <- g
	}()

	deadline := time.Now().Add(30 * time.Second)
	for {
		var waiting bool
		if err := probe.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name='wfrun002-takeover-B' AND wait_event_type='Lock')`).Scan(&waiting); err != nil {
			t.Fatalf("probe lock wait: %v", err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("takeover Acquire never blocked on the release row lock")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("commit A: %v", err)
	}
	g := <-bDone
	if g.err != nil {
		t.Fatalf("takeover Acquire: %v", g.err)
	}
	if g.held.FenceToken != 2 || g.held.State != runtimestate.LeaseHeld {
		t.Fatalf("takeover = %+v, want HELD at fence token 2", g.held)
	}

	var state string
	var releasedAt time.Time
	var version int64
	inTenantTx(t, connA, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT lease_state, released_at, lease_version FROM workflow_lease WHERE tenant_id=$1 AND lease_id=$2`, tenant, leaseA.LeaseID).Scan(&state, &releasedAt, &version)
	})
	if state != runtimestate.LeaseReleased || !releasedAt.Equal(releaseAt) || version != 2 {
		t.Fatalf("released row = state %s released_at %s version %d, want RELEASED %s version 2", state, releasedAt.UTC().Format(time.RFC3339Nano), version, releaseAt.UTC().Format(time.RFC3339Nano))
	}
}
