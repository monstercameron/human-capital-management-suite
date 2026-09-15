package signals_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

func matchedSubscription(tenant, instance uuid.UUID) signals.Subscription {
	return signals.Subscription{
		TenantID: tenant, SubscriptionID: signals.SubscriptionIDFor(tenant, instance, "await", 1), InstanceID: instance,
		NodeID: "await", NodeAttempt: 1, EventType: "event.matched", CorrelationKey: "subject",
		CorrelationValue: "subject-1", ExpectedSchemaRef: "event/v1", AcceptedSources: []string{"source"},
		Ordering: stepSignal.OrderingNone, CreatedAt: signalAt,
	}
}

func matchedSignal(tenant uuid.UUID, key string, at time.Time) signals.ReceiveRequest {
	return signals.ReceiveRequest{ReceivedAt: at, Signal: stepSignal.Signal{
		Tenant: values.TenantId(tenant.String()), Source: "source", EventType: "event.matched", SchemaRef: "event/v1",
		CorrelationKey: "subject", CorrelationValue: "subject-1", IdempotencyKey: key,
		Payload: []byte(`{"ok":true}`), ReceivedAt: values.NewInstant(at),
	}}
}

// TestTodo_WF_RUN_005_MatchedReceipt proves the read side a resumer depends
// on: the deterministic subscription replays instead of colliding, a matched
// receipt is found only after an ACCEPTED receive, the pending continuation is
// visible exactly once and only for an admissible instance, and a second
// differently keyed signal after the wait was settled is REFUSED_LATE with no
// second receipt.
func TestTodo_WF_RUN_005_MatchedReceipt(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "wf-run-005-matched")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	ctx := context.Background()
	sub := matchedSubscription(tenant, instance)

	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if replay, err := store.SubscribeReplay(ctx, tx, sub); err != nil || replay {
			t.Fatalf("first subscribe = replay %t, %v; want a new row", replay, err)
		}
		if replay, err := store.SubscribeReplay(ctx, tx, sub); err != nil || !replay {
			t.Fatalf("replayed subscribe = replay %t, %v; want the same row recognised", replay, err)
		}
		other := sub
		other.SubscriptionID = uuid.New()
		if _, err := store.SubscribeReplay(ctx, tx, other); !errors.Is(err, signals.ErrSubscriptionReplayMismatch) {
			t.Fatalf("a second subscription for the same wait = %v, want ErrSubscriptionReplayMismatch", err)
		}
		if _, err := store.LoadMatchedReceipt(ctx, tx, tenant, uuid.New(), sub.SubscriptionID); !errors.Is(err, signals.ErrNoMatchedReceipt) {
			t.Fatalf("receipt before any signal = %v, want ErrNoMatchedReceipt", err)
		}
		if _, found, err := store.MatchedForNode(ctx, tx, tenant, instance, "await", 1); err != nil || found {
			t.Fatalf("matched node before any signal = %t, %v; want none", found, err)
		}
		return nil
	})

	var accepted signals.Receipt
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		accepted, err = store.Receive(ctx, tx, matchedSignal(tenant, "first", signalAt), acceptingVerifier{})
		return err
	})
	if len(accepted.Dispositions) != 1 || accepted.Dispositions[0].Status != stepSignal.StatusAccepted {
		t.Fatalf("matching receive = %+v, want ACCEPTED", accepted)
	}

	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		row, err := store.LoadMatchedReceipt(ctx, tx, tenant, accepted.SignalID, sub.SubscriptionID)
		if err != nil {
			return err
		}
		if !row.Settled() || row.InstanceID != instance || row.NodeID != "await" || row.NodeAttempt != 1 ||
			row.ContinuationRef != signals.ContinuationRef(accepted.SignalID) || row.PayloadDigest == "" {
			t.Fatalf("matched receipt = %+v, want a settled reference-only match for await/1", row)
		}
		forNode, found, err := store.MatchedForNode(ctx, tx, tenant, instance, "await", 1)
		if err != nil || !found || forNode.SignalID != accepted.SignalID {
			t.Fatalf("matched node = %+v, %t, %v; want the accepted signal", forNode, found, err)
		}
		pending, err := store.PendingMatched(ctx, tx, tenant, signalAt, 10)
		if err != nil {
			return err
		}
		if len(pending) != 1 || pending[0].Ready.InstanceID != instance || pending[0].Ready.Attempt != 1 ||
			pending[0].Receipt.SignalID != accepted.SignalID || pending[0].Ready.Version == 0 {
			t.Fatalf("pending matched = %+v, want the one enqueued continuation", pending)
		}
		if early, err := store.PendingMatched(ctx, tx, tenant, signalAt.Add(-time.Second), 10); err != nil || len(early) != 0 {
			t.Fatalf("pending before eligibility = %d, %v; want none", len(early), err)
		}
		return nil
	})

	db.Exec(t, `UPDATE workflow_instance SET runtime_status = 'PAUSED' WHERE instance_id = $1`, instance)
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if pending, err := store.PendingMatched(ctx, tx, tenant, signalAt, 10); err != nil || len(pending) != 0 {
			t.Fatalf("pending for a paused instance = %d, %v; want none dispatched", len(pending), err)
		}
		return nil
	})

	var late signals.Receipt
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		late, err = store.Receive(ctx, tx, matchedSignal(tenant, "second", signalAt.Add(time.Minute)), acceptingVerifier{})
		return err
	})
	if len(late.Dispositions) != 1 || late.Dispositions[0].Status != stepSignal.StatusRefusedLate || late.Dispositions[0].ContinuationRef != "" {
		t.Fatalf("second signal after the wait was settled = %+v, want REFUSED_LATE with no continuation", late)
	}
	assertCounts(t, conn, tenant, instance, sub.SubscriptionID, 2, 1, 1, "accepted plus late: one continuation row, one ready row")
	var receipts int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM workflow_signal_receipt WHERE instance_id = $1`, instance).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("receipts = %d, want only the accepted signal's", receipts)
	}
}

func TestMatchedReads_RefuseIncompleteIdentityWithoutADatabase(t *testing.T) {
	store := signals.Store{}
	if _, err := store.LoadMatchedReceipt(context.Background(), nil, uuid.Nil, uuid.New(), uuid.New()); err == nil {
		t.Fatal("LoadMatchedReceipt accepted a nil tenant")
	}
	if _, err := store.PendingMatched(context.Background(), nil, uuid.New(), signalAt, 0); err == nil {
		t.Fatal("PendingMatched accepted a zero limit")
	}
	a := signals.SubscriptionIDFor(uuid.Nil, uuid.Nil, "await", 1)
	if a == signals.SubscriptionIDFor(uuid.Nil, uuid.Nil, "await", 2) || a != signals.SubscriptionIDFor(uuid.Nil, uuid.Nil, "await", 1) {
		t.Fatal("SubscriptionIDFor is not a deterministic per-attempt identity")
	}
	id := uuid.New()
	for name, row := range map[string]signals.MatchedReceipt{
		"refused":       {SignalID: id, DispositionStatus: stepSignal.StatusRefusedLate, SubscriptionState: "SATISFIED", ContinuationRef: signals.ContinuationRef(id)},
		"still open":    {SignalID: id, DispositionStatus: stepSignal.StatusAccepted, SubscriptionState: "OPEN", ContinuationRef: signals.ContinuationRef(id)},
		"foreign ref":   {SignalID: id, DispositionStatus: stepSignal.StatusAccepted, SubscriptionState: "SATISFIED", ContinuationRef: signals.ContinuationRef(uuid.New())},
		"payload bytes": {SignalID: id, DispositionStatus: stepSignal.StatusAccepted, SubscriptionState: "SATISFIED", ContinuationRef: `{"ok":true}`},
	} {
		if row.Settled() {
			t.Fatalf("%s receipt reported settled", name)
		}
	}
}
