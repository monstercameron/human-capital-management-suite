package reconcile_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// TestPostgresStoreListForEffectReturnsEveryPolicyJob proves the inspector's
// reconciliation read returns every job watching one effect -- one per
// comparison policy, in policy order -- and nothing for an effect nobody
// watches or for another tenant.
func TestPostgresStoreListForEffectReturnsEveryPolicyJob(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	f := newReconcileFixture(t, db, "recon-list-effect")
	other := newReconcileFixture(t, db, "recon-list-effect-other")
	store := reconcile.PostgresStore{}
	coord := reconcile.Coordinator{Store: store, Fences: lease.Manager{}}

	effect := mandatoryEffect("effect/list-for-effect", "NONE")
	unrelated := mandatoryEffect("effect/unrelated", "NONE")
	trigger := func(fx reconcileFixture, node string, policy string) {
		t.Helper()
		e := effect
		if node == "unrelated" {
			e = unrelated
		}
		fx.do(t, func(tx dbport.Tx) error {
			_, err := coord.Trigger(ctx, tx, reconcile.TriggerRequest{
				TenantID: fx.tenant, Effect: e, PolicyRef: policy,
				IntendedRef: "proposal:1", RequiredFreshness: observe.FreshnessFresh,
				SLARef: "sla:list", Deadline: fixedInstant.Add(2 * time.Hour),
				Fence: fx.fence, Now: fixedInstant,
			})
			return err
		})
	}
	trigger(f, "effect", "policy/b")
	trigger(f, "effect", "policy/a")
	trigger(f, "unrelated", "policy/a")
	trigger(other, "effect", "policy/other-tenant")

	list := func(fx reconcileFixture, ref string) []reconcile.Job {
		t.Helper()
		var jobs []reconcile.Job
		fx.do(t, func(tx dbport.Tx) error {
			var err error
			jobs, err = store.ListForEffect(ctx, tx, fx.tenant, ref)
			return err
		})
		return jobs
	}

	jobs := list(f, effect.IdempotencyKey)
	if len(jobs) != 2 {
		t.Fatalf("ListForEffect returned %d jobs, want 2: %+v", len(jobs), jobs)
	}
	if jobs[0].PolicyRef != "policy/a" || jobs[1].PolicyRef != "policy/b" {
		t.Errorf("policies = %s, %s; want policy/a then policy/b", jobs[0].PolicyRef, jobs[1].PolicyRef)
	}
	for _, job := range jobs {
		if job.EffectRef != effect.IdempotencyKey || job.TenantID != f.tenant || job.Status != reconcile.StatusPending {
			t.Errorf("job = %+v, want a PENDING job of this tenant for %s", job, effect.IdempotencyKey)
		}
	}
	if none := list(f, "effect/nobody-watches"); len(none) != 0 {
		t.Fatalf("an unwatched effect returned %d jobs", len(none))
	}
	if theirs := list(other, effect.IdempotencyKey); len(theirs) != 1 || theirs[0].PolicyRef != "policy/other-tenant" {
		t.Fatalf("the other tenant's list = %+v, want only its own job", theirs)
	}
}
