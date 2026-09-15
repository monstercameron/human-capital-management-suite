package runtimestate_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
)

// TestTimerStoreForInstanceReturnsEverySettledAndPendingTimer proves the
// inspector's timer read returns the instance's whole timer history -- fired,
// cancelled and pending -- in creation order, and nothing of another
// instance or another tenant.
func TestTimerStoreForInstanceReturnsEverySettledAndPendingTimer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "timer-for-instance")
	other := newSchedulingFixture(t, db, "timer-for-instance-other")
	store := runtimestate.TimerStore{}

	mk := func(key, kind string, created time.Time) runtimestate.Timer {
		return runtimestate.Timer{
			TenantID: f.tenant, TimerID: uuid.New(), InstanceID: f.instance.InstanceID,
			NodeID: "node.wait", Key: key, Kind: kind,
			FiresAt: created.Add(time.Hour), CreatedAt: created,
		}
	}
	fired := mk("retry-1", runtimestate.TimerRetryBackoff, fixedInstant)
	cancelled := mk("deadline", runtimestate.TimerDeadline, fixedInstant.Add(time.Minute))
	pending := mk("retry-2", runtimestate.TimerRetryBackoff, fixedInstant.Add(2*time.Minute))
	for _, timer := range []runtimestate.Timer{pending, fired, cancelled} {
		f.do(t, func(tx dbport.Tx) error { return store.Set(ctx, tx, timer) })
	}
	f.do(t, func(tx dbport.Tx) error {
		return store.Fire(ctx, tx, f.tenant, fired.TimerID, 1, fixedInstant.Add(time.Hour))
	})
	f.do(t, func(tx dbport.Tx) error {
		return store.Cancel(ctx, tx, f.tenant, cancelled.TimerID, 1, fixedInstant.Add(time.Hour))
	})
	foreign := runtimestate.Timer{
		TenantID: other.tenant, TimerID: uuid.New(), InstanceID: other.instance.InstanceID,
		NodeID: "node.wait", Key: "foreign", Kind: runtimestate.TimerDelay,
		FiresAt: fixedInstant.Add(time.Hour), CreatedAt: fixedInstant,
	}
	other.do(t, func(tx dbport.Tx) error { return store.Set(ctx, tx, foreign) })

	var got []runtimestate.Timer
	f.do(t, func(tx dbport.Tx) error {
		var err error
		got, err = store.ForInstance(ctx, tx, f.tenant, f.instance.InstanceID)
		return err
	})
	if len(got) != 3 {
		t.Fatalf("ForInstance returned %d timers, want 3: %+v", len(got), got)
	}
	want := []struct {
		id    uuid.UUID
		state string
	}{
		{fired.TimerID, runtimestate.TimerFired},
		{cancelled.TimerID, runtimestate.TimerCancelled},
		{pending.TimerID, runtimestate.TimerPending},
	}
	for i, w := range want {
		if got[i].TimerID != w.id || got[i].State != w.state {
			t.Errorf("timer %d = %s %s, want %s %s", i, got[i].TimerID, got[i].State, w.id, w.state)
		}
	}

	// Another tenant asking for this instance sees nothing: the tenant filter
	// and row-level security both hold.
	var leaked []runtimestate.Timer
	other.do(t, func(tx dbport.Tx) error {
		var err error
		leaked, err = store.ForInstance(ctx, tx, other.tenant, f.instance.InstanceID)
		return err
	})
	if len(leaked) != 0 {
		t.Fatalf("another tenant read %d of this instance's timers", len(leaked))
	}
}
