package bootstrap_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// A created worker's revision is a durable fact, not the fixed corpus @1
// coordinate. The wire must refuse a caller-supplied mismatch and accept
// the exact coordinate published by the workforce list.
func TestTodo_PROMOUX_015_Recovery_CreatedWorkerRevisionBinding(t *testing.T) {
	c := newPromotionCell(t)
	created, err := c.harness.engine.CreateWorker(c.harness.operatorCtx(t), journeyWorkerInput())
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if created.SubjectRevision == "" {
		t.Fatal("created worker did not publish a subject revision")
	}
	stored := queryOne[string](t, c.harness.cell, `
		SELECT revision_stream || '@' || revision_sequence::text FROM journey_worker
		WHERE tenant_id = $1 AND worker_key = $2`, pgstore.TenantID(testTenant), created.WorkerRef)
	if created.SubjectRevision != stored {
		t.Fatalf("published revision %q differs from durable %q", created.SubjectRevision, stored)
	}
	ctx, cancel := c.callCtx(t)
	defer cancel()
	stale := promotionProposeRequest("created-stale")
	stale.SubjectWorkerRef = created.WorkerRef
	stale.ExpectedSubjectRevision = created.SubjectRevision + "-stale"
	if _, err := c.direct.ProposePromotion(ctx, stale); err == nil {
		t.Fatal("promotion accepted a stale created-worker revision")
	} else {
		assertPromotionRefusal(t, err, envelope.CodeFailedPrecondition)
	}
	workers, _, err := c.harness.engine.ListWorkers(c.harness.operatorCtx(t))
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	if len(workers) == 0 || workers[0].WorkerRef != created.WorkerRef || workers[0].SubjectRevision != stored {
		t.Fatalf("worker list did not publish the durable revision: %+v", workers)
	}
	fresh := promotionProposeRequest("created-fresh")
	fresh.SubjectWorkerRef = created.WorkerRef
	fresh.ExpectedSubjectRevision = workers[0].SubjectRevision
	if _, err := c.direct.ProposePromotion(ctx, fresh); err != nil {
		t.Fatalf("fresh created-worker revision could not propose: %v", err)
	}
}

func TestTodo_PROMOUX_015_Recovery_ReplayConfirmsOriginalGuard(t *testing.T) {
	c := newPromotionCell(t)
	ctx, cancel := c.callCtx(t)
	defer cancel()
	req := promotionProposeRequest("guard-replay-confirm")
	first, err := c.direct.ProposePromotion(ctx, req)
	if err != nil {
		t.Fatalf("first ProposePromotion: %v", err)
	}
	if _, err := c.harness.cell.pool.Exec(context.Background(), `
		UPDATE promotion_active_intent_guard SET intent_id = NULL
		WHERE tenant_id = $1 AND intent_id = $2::uuid`, pgstore.TenantID(testTenant), first.GetIntentId()); err != nil {
		t.Fatalf("inject lost confirmation: %v", err)
	}
	replay, err := c.direct.ProposePromotion(ctx, req)
	if err != nil {
		t.Fatalf("replay ProposePromotion: %v", err)
	}
	if replay.GetIntentId() != first.GetIntentId() {
		t.Fatalf("replay minted intent %s, want %s", replay.GetIntentId(), first.GetIntentId())
	}
	confirmed := queryOne[string](t, c.harness.cell, `
		SELECT intent_id::text FROM promotion_active_intent_guard
		WHERE tenant_id = $1 AND idempotency_key = $2`,
		pgstore.TenantID(testTenant), "promotion.propose:"+req.ClientRequestId)
	if confirmed != first.GetIntentId() {
		t.Fatalf("replay left original guard bound to %q, want %q", confirmed, first.GetIntentId())
	}
}
