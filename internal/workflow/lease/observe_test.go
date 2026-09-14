package lease_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

// TestLeaseOperationsAreObservable proves every lease transition reports its
// status and issues: a granted acquire carries the minted fence token, a
// contended acquire and a superseded renew are governed refusals with their
// stable codes, and a release succeeds -- each through the context recorder.
func TestLeaseOperationsAreObservable(t *testing.T) {
	db := pgtest.New(t)
	f := newLeaseFixture(t, db, "observe")
	var rec observetest.Recorder
	ctx := rec.Context(context.Background())
	acquire := func(holder lease.Identity, now time.Time) (lease.Grant, error) {
		var g lease.Grant
		err := f.try(func(tx dbport.Tx) error {
			var e error
			g, e = f.manager.Acquire(ctx, tx, lease.AcquireRequest{TenantID: f.tenant, Resource: f.resource, Holder: holder, Now: now, TTL: time.Minute})
			return e
		})
		return g, err
	}

	grantA, err := acquire(holderA, fixedInstant)
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	if _, err := acquire(holderB, fixedInstant.Add(time.Second)); err == nil {
		t.Fatal("contended acquire granted")
	}
	grantB, err := acquire(holderB, fixedInstant.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if err := f.try(func(tx dbport.Tx) error {
		_, e := f.manager.Renew(ctx, tx, grantA.Fence, fixedInstant.Add(2*time.Minute), time.Minute)
		return e
	}); err == nil {
		t.Fatal("stale renew accepted")
	}
	f.do(t, func(tx dbport.Tx) error {
		_, e := f.manager.Release(ctx, tx, grantB.Fence, fixedInstant.Add(2*time.Minute))
		return e
	})

	acquires := rec.Named("workflow.lease.acquire")
	if len(acquires) != 3 {
		t.Fatalf("acquire operations = %d, want 3", len(acquires))
	}
	first := acquires[0]
	if first.Outcome != observe.OutcomeSuccess || first.Attrs[observe.KeyFence] != "1" ||
		first.Attrs[observe.KeyTenant] != f.tenant.String() || first.Attrs[observe.KeyResource] != string(lease.ResourceWorkflowInstance) {
		t.Errorf("granted acquire = %+v", first)
	}
	if acquires[1].Outcome != observe.OutcomeRefused || acquires[1].Code != lease.CodeLeaseHeld {
		t.Errorf("contended acquire = %+v, want REFUSED %s", acquires[1], lease.CodeLeaseHeld)
	}
	if acquires[2].Attrs[observe.KeyFence] != "2" {
		t.Errorf("takeover fence attr = %q, want 2", acquires[2].Attrs[observe.KeyFence])
	}
	renew := rec.Named("workflow.lease.renew")
	if len(renew) != 1 || renew[0].Outcome != observe.OutcomeRefused || renew[0].Code == "" {
		t.Errorf("stale renew = %+v, want one REFUSED with a code", renew)
	}
	release := rec.Named("workflow.lease.release")
	if len(release) != 1 || release[0].Outcome != observe.OutcomeSuccess {
		t.Errorf("release = %+v", release)
	}
	for _, op := range rec.Ops() {
		if op.Ended != 1 {
			t.Errorf("%s ended %d times", op.Name, op.Ended)
		}
	}
}
