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

// The intake builds its signal from the parked wait itself, so it needs the
// open subscription row for one instance node -- and nothing else. A settled
// wait is a stage refusal, not an intake.

func openSubscriptionFixture(store signals.Store, tenant, instance uuid.UUID) signals.Subscription {
	return signals.Subscription{
		TenantID: tenant, SubscriptionID: uuid.New(), InstanceID: instance,
		NodeID: "acknowledge_release", NodeAttempt: 1, EventType: "hcmnext.events.promotion_ack",
		CorrelationKey: "proposal.intent_id", CorrelationValue: "intent-1", ExpectedSchemaRef: "PromotionAckPayload/v1",
		AcceptedSources: []string{"hcmnext.integrations.hris"}, Ordering: stepSignal.OrderingNone,
		ClosesAt: signalAt.Add(14 * 24 * time.Hour), CreatedAt: signalAt,
	}
}

func TestOpenSubscriptionForNodeReturnsTheParkedWait(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "open-subscription-primary")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	want := openSubscriptionFixture(store, tenant, instance)
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		return store.Subscribe(context.Background(), tx, want)
	})
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		got, err := store.OpenSubscriptionForNode(context.Background(), tx, tenant, instance, "acknowledge_release")
		if err != nil {
			return err
		}
		if got.ID != want.SubscriptionID || got.InstanceID != instance || got.NodeID != "acknowledge_release" {
			t.Fatalf("open subscription = %+v, want the parked wait", got)
		}
		if got.EventType != want.EventType || got.CorrelationKey != want.CorrelationKey ||
			got.CorrelationValue != want.CorrelationValue || got.ExpectedSchemaRef != want.ExpectedSchemaRef {
			t.Fatalf("open subscription correlates %+v, want the pinned wait", got)
		}
		if len(got.AcceptedSources) != 1 || got.AcceptedSources[0] != "hcmnext.integrations.hris" {
			t.Fatalf("open subscription sources = %v, want the pinned allowlist", got.AcceptedSources)
		}
		if got.ClosesAt == nil || !got.ClosesAt.Equal(want.ClosesAt) {
			t.Fatalf("open subscription closes at %v, want %v", got.ClosesAt, want.ClosesAt)
		}
		return nil
	})
}

func TestOpenSubscriptionForCorrelationResolvesTheParkedInstance(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "open-subscription-correlation")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	want := openSubscriptionFixture(store, tenant, instance)
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		return store.Subscribe(context.Background(), tx, want)
	})
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		got, err := store.OpenSubscriptionForCorrelation(context.Background(), tx, tenant, "acknowledge_release", "intent-1")
		if err != nil {
			return err
		}
		if got.ID != want.SubscriptionID || got.InstanceID != instance {
			t.Fatalf("open subscription by correlation = %+v, want the parked wait resolving its instance", got)
		}
		return nil
	})
}

func TestOpenSubscriptionForNodeRefusesWithoutAnOpenWait(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "open-subscription-absent")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.OpenSubscriptionForNode(context.Background(), tx, tenant, instance, "acknowledge_release"); !errors.Is(err, signals.ErrNoOpenSubscription) {
			t.Fatalf("open subscription without a wait = %v, want ErrNoOpenSubscription", err)
		}
		return nil
	})
}

func TestOpenSubscriptionForNodeRefusesASettledWait(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "open-subscription-settled")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	want := openSubscriptionFixture(store, tenant, instance)
	request := signalRequest(tenant, "attempt-settled", []byte(`{"acknowledged":true}`))
	request.Signal.EventType = want.EventType
	request.Signal.Source = "hcmnext.integrations.hris"
	request.Signal.CorrelationKey = want.CorrelationKey
	request.Signal.CorrelationValue = want.CorrelationValue
	request.Signal.SchemaRef = want.ExpectedSchemaRef
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if err := store.Subscribe(context.Background(), tx, want); err != nil {
			return err
		}
		got, err := store.Receive(context.Background(), tx, request, acceptingVerifier{})
		if err != nil {
			return err
		}
		if len(got.Dispositions) != 1 || got.Dispositions[0].Status != stepSignal.StatusAccepted {
			t.Fatalf("receive = %+v, want one ACCEPTED disposition", got)
		}
		return nil
	})
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.OpenSubscriptionForNode(context.Background(), tx, tenant, instance, "acknowledge_release"); !errors.Is(err, signals.ErrNoOpenSubscription) {
			t.Fatalf("open subscription after accept = %v, want ErrNoOpenSubscription: a settled wait is a stage refusal", err)
		}
		return nil
	})
}
