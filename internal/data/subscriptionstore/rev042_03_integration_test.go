package subscriptionstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/subscriptionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

type rev04203BlockingProvider struct {
	entered chan subscription.DeliveryAttempt
	release chan struct{}
}

func (p *rev04203BlockingProvider) Deliver(ctx context.Context, attempt subscription.DeliveryAttempt) (subscription.ProviderReceipt, error) {
	select {
	case p.entered <- attempt:
	case <-ctx.Done():
		return subscription.ProviderReceipt{}, ctx.Err()
	}
	select {
	case <-p.release:
		return subscription.ProviderReceipt{ReceiptID: attempt.OperationKey, Provider: "fixture", Accepted: true}, nil
	case <-ctx.Done():
		return subscription.ProviderReceipt{}, ctx.Err()
	}
}

// TestTodo_REV_042_03_Integration reloads tenant-scoped active subscriptions
// from PostgreSQL before dispatch, then proves the served endpoint capacity
// isolates a noisy tenant while the destination is slow.
func TestTodo_REV_042_03_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantA := insertTenant(t, db, "rev042-03-persistent-a")
	tenantB := insertTenant(t, db, "rev042-03-persistent-b")
	for _, tenant := range []struct {
		id  uuid.UUID
		key string
	}{{tenantA, "sub-a"}, {tenantB, "sub-b"}} {
		store := subscriptionstore.New(appConn(t, db), tenant.id)
		draftRevision := draft(t, tenant.id, tenant.key, 1)
		activeRevision := active(t, draftRevision)
		if err := store.AppendRevision(context.Background(), tenant.id, draftRevision); err != nil {
			t.Fatal(err)
		}
		if err := store.AppendRevision(context.Background(), tenant.id, activeRevision); err != nil {
			t.Fatal(err)
		}
		if err := store.AppendAuthorizationEvent(context.Background(), tenant.id, authEvent(t, activeRevision, true), 1); err != nil {
			t.Fatal(err)
		}
	}

	loadActive := func(tenant uuid.UUID) subscription.EventSubscription {
		t.Helper()
		fresh := subscriptionstore.New(appConn(t, db), tenant)
		activeRevisions, err := fresh.LoadActive(context.Background(), tenant)
		if err != nil || len(activeRevisions) != 1 || activeRevisions[0].State != subscription.StateActive {
			t.Fatalf("served active subscriptions for %s = %#v, err=%v", tenant, activeRevisions, err)
		}
		return activeRevisions[0]
	}
	servedA, servedB := loadActive(tenantA), loadActive(tenantB)
	if servedA.DeliveryEndpointRef != servedB.DeliveryEndpointRef {
		t.Fatalf("persisted endpoints differ: %q, %q", servedA.DeliveryEndpointRef, servedB.DeliveryEndpointRef)
	}
	capacity, err := subscription.NewDeliveryCapacity(map[string]operation.ConnectorPolicy{
		servedA.DeliveryEndpointRef: {Quota: operation.ConnectorQuota{MaxConcurrent: 2}, PerTenantShare: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := subscription.Dispatcher{
		Journal: subscription.NewDeliveryJournal(), Gate: subscription.NewDeliveryGate(),
		DLQ: subscription.NewDeadLetterQueue(time.Hour), Capacity: capacity,
		Policy: subscription.RetryPolicy{MaxAttempts: 1}, DeadLetterOwner: "team:integrations",
	}
	provider := &rev04203BlockingProvider{entered: make(chan subscription.DeliveryAttempt, 2), release: make(chan struct{}, 2)}
	request := func(revision subscription.EventSubscription, tenant uuid.UUID, sequence uint64) subscription.DeliveryRequest {
		return subscription.DeliveryRequest{
			SubscriptionID: revision.SubscriptionID, SubscriptionRevision: revision.Revision,
			Envelope: subscription.CanonicalEnvelope{Tenant: tenant.String(), Kind: subscription.EventWorkerChanged,
				SchemaVersion: 1, SubjectRefs: []string{"worker:served"}, EffectiveAt: time.Unix(10, 0),
				KnownAt: time.Unix(11, 0), PayloadDigest: "sha256:served-event", ProvenanceRef: "event:served",
				Sequence: sequence},
			Destination: revision.DeliveryEndpointRef, OrderingKey: "worker:served",
		}
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := dispatcher.Dispatch(context.Background(), provider, request(servedA, tenantA, 1))
		firstDone <- err
	}()
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("persisted tenant-a subscription did not reach its served endpoint")
	}
	if _, err := dispatcher.Dispatch(context.Background(), provider, request(servedA, tenantA, 2)); !errors.Is(err, subscription.ErrDeliveryCapacity) {
		t.Fatalf("second tenant-a delivery error = %v, want tenant share bound", err)
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := dispatcher.Dispatch(context.Background(), provider, request(servedB, tenantB, 1))
		secondDone <- err
	}()
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("tenant-b persisted subscription missed the 1s fairness SLO")
	}
	provider.release <- struct{}{}
	provider.release <- struct{}{}
	if err := <-firstDone; err != nil {
		t.Fatalf("tenant-a served delivery = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("tenant-b served delivery = %v", err)
	}
}
