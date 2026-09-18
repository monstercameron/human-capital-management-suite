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
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// The sweeper closes the loop refusal alone leaves open: a wait whose close
// time passed with no accepted signal must reach its TIMED_OUT edge, not park
// forever. ExpireDue marks it EXPIRED and commits the timeout continuation
// and ready work as one unit, the way Receive commits receipt, disposition,
// continuation and ready work for an ACCEPTED signal.

func TestExpireDueExpiresTheDueWaitAndEnqueuesItsTimeout(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "expire-due-primary")
	instance := newInstance(t, db, tenant)
	futureInstance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	due := openSubscriptionFixture(store, tenant, instance)
	due.ClosesAt = signalAt.Add(-time.Minute)
	// One node attempt holds one wait, so the not-yet-due wait parks on
	// its own instance.
	future := openSubscriptionFixture(store, tenant, futureInstance)
	future.SubscriptionID = uuid.New()
	future.CorrelationValue = "intent-future"
	future.ClosesAt = signalAt.Add(time.Hour)
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if err := store.Subscribe(context.Background(), tx, due); err != nil {
			return err
		}
		return store.Subscribe(context.Background(), tx, future)
	})
	var expired []signals.ExpiredSubscription
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		expired, err = store.ExpireDue(context.Background(), tx, tenant, signalAt, 8)
		return err
	})
	if len(expired) != 1 || expired[0].SubscriptionID != due.SubscriptionID || expired[0].InstanceID != instance {
		t.Fatalf("expired = %+v, want exactly the due wait", expired)
	}
	if n := countSignalRows(t, db, `SELECT count(*) FROM workflow_signal_subscription WHERE subscription_id = $1 AND subscription_state = 'EXPIRED' AND closed_at IS NOT NULL`, due.SubscriptionID); n != 1 {
		t.Fatalf("expired subscription rows = %d, want 1 EXPIRED with a close instant", n)
	}
	if n := countSignalRows(t, db, `SELECT count(*) FROM workflow_signal_subscription WHERE subscription_id = $1 AND subscription_state = 'OPEN'`, future.SubscriptionID); n != 1 {
		t.Fatalf("future subscription rows = %d, want 1 still OPEN", n)
	}
	if n := countSignalRows(t, db, `SELECT count(*) FROM workflow_ready_work WHERE instance_id = $1 AND node_id = 'acknowledge_release' AND ready_state = 'READY'`, instance); n != 1 {
		t.Fatalf("timeout ready work rows = %d, want 1", n)
	}
	var routeKey, ref string
	if err := db.QueryRow(context.Background(), `SELECT route_key, ref FROM workflow_continuation WHERE tenant_id = $1 AND instance_id = $2 AND target_node_id = 'acknowledge_release' ORDER BY recorded_at DESC LIMIT 1`, tenant, instance).Scan(&routeKey, &ref); err != nil {
		t.Fatalf("load expiry continuation: %v", err)
	}
	if routeKey != "TIMED_OUT" || ref != signals.ExpiryContinuationRef(due.SubscriptionID) {
		t.Fatalf("expiry continuation = %s/%s, want TIMED_OUT/%s", routeKey, ref, signals.ExpiryContinuationRef(due.SubscriptionID))
	}
	// The expired wait is no longer receivable: the intake refuses it as a
	// stage refusal, exactly as for a satisfied wait.
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.OpenSubscriptionForNode(context.Background(), tx, tenant, instance, "acknowledge_release"); !errors.Is(err, signals.ErrNoOpenSubscription) {
			t.Fatalf("open subscription after expiry = %v, want ErrNoOpenSubscription", err)
		}
		return nil
	})
}

func TestExpireDueSkipsSatisfiedWaitsAndRepeatsAsNoOp(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "expire-due-settled")
	settledInstance := newInstance(t, db, tenant)
	dueInstance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	settled := openSubscriptionFixture(store, tenant, settledInstance)
	settled.CorrelationValue = "intent-settled"
	due := openSubscriptionFixture(store, tenant, dueInstance)
	due.CorrelationValue = "intent-due"
	due.ClosesAt = signalAt.Add(-time.Minute)
	request := signalRequest(tenant, "attempt-expire-settled", []byte(`{"acknowledged":true}`))
	request.Signal.EventType = settled.EventType
	request.Signal.Source = "hcmnext.integrations.hris"
	request.Signal.CorrelationKey = settled.CorrelationKey
	request.Signal.CorrelationValue = settled.CorrelationValue
	request.Signal.SchemaRef = settled.ExpectedSchemaRef
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		ctx := context.Background()
		if err := store.Subscribe(ctx, tx, settled); err != nil {
			return err
		}
		if err := store.Subscribe(ctx, tx, due); err != nil {
			return err
		}
		got, err := store.Receive(ctx, tx, request, acceptingVerifier{})
		if err != nil {
			return err
		}
		if len(got.Dispositions) != 1 || got.Dispositions[0].Status != stepSignal.StatusAccepted {
			t.Fatalf("receive = %+v, want one ACCEPTED disposition", got)
		}
		expired, err := store.ExpireDue(ctx, tx, tenant, signalAt, 8)
		if err != nil {
			return err
		}
		if len(expired) != 1 || expired[0].SubscriptionID != due.SubscriptionID {
			t.Fatalf("expired = %+v, want exactly the due wait: the ACCEPTED path owns the satisfied wakeup", expired)
		}
		return nil
	})
	// A retried sweep over an already-expired wait is a no-op, never a
	// second wakeup.
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		expired, err := store.ExpireDue(context.Background(), tx, tenant, signalAt, 8)
		if err != nil {
			return err
		}
		if len(expired) != 0 {
			t.Fatalf("second sweep expired = %+v, want nothing", expired)
		}
		return nil
	})
}

func TestPendingExpiredAndLoadExpiredSubscription(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "expire-due-pending")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	due := openSubscriptionFixture(store, tenant, instance)
	due.ClosesAt = signalAt.Add(-time.Minute)
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		return store.Subscribe(context.Background(), tx, due)
	})
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.ExpireDue(context.Background(), tx, tenant, signalAt, 8); err != nil {
			return err
		}
		return nil
	})
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		pending, err := store.PendingExpired(context.Background(), tx, tenant, signalAt, 8)
		if err != nil {
			return err
		}
		if len(pending) != 1 || pending[0].Subscription.SubscriptionID != due.SubscriptionID ||
			pending[0].Ready.InstanceID != instance || pending[0].Ready.NodeID != "acknowledge_release" {
			t.Fatalf("pending expiries = %+v, want the one expired wait with its ready work", pending)
		}
		loaded, err := store.LoadExpiredSubscription(context.Background(), tx, tenant, due.SubscriptionID)
		if err != nil {
			return err
		}
		if loaded.InstanceID != instance || loaded.NodeID != "acknowledge_release" || loaded.NodeAttempt != 1 {
			t.Fatalf("loaded expired subscription = %+v, want the expired wait", loaded)
		}
		return nil
	})
	// An OPEN wait is not loadable as an expiry: only an EXPIRED row drives
	// a timeout resume. It parks on its own instance: one node attempt
	// holds one wait.
	open := openSubscriptionFixture(store, tenant, newInstance(t, db, tenant))
	open.SubscriptionID = uuid.New()
	open.CorrelationValue = "intent-open"
	open.ClosesAt = signalAt.Add(time.Hour)
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		return store.Subscribe(context.Background(), tx, open)
	})
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.LoadExpiredSubscription(context.Background(), tx, tenant, open.SubscriptionID); !errors.Is(err, signals.ErrNoExpiredSubscription) {
			t.Fatalf("load OPEN as expired = %v, want ErrNoExpiredSubscription", err)
		}
		return nil
	})
}

func countSignalRows(t *testing.T, db *pgtest.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}
