package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// ClockSignalReceiver adapts the durable signal store to the time-clock
// runtime. It owns one tenant-scoped transaction per delivery and commits the
// receipt, match disposition, and continuation enqueue together.
type ClockSignalReceiver struct {
	DB     dbport.Beginner
	Verify stepSignal.Verifier
	Now    func() time.Time
}

// Receive records and correlates one clock signal through the platform signal
// store. Duplicate observation IDs are resolved by the store's idempotency
// key and return the original durable signal identity.
func (r ClockSignalReceiver) Receive(ctx context.Context, delivery clockservice.SignalDelivery) (clockservice.SignalReceipt, error) {
	if r.DB == nil || r.Verify == nil || r.Now == nil {
		return clockservice.SignalReceipt{}, errors.New("clock signal receive: database and verifier are required")
	}
	if delivery.TenantID == uuid.Nil {
		return clockservice.SignalReceipt{}, errors.New("clock signal receive: tenant is required")
	}
	if strings.TrimSpace(delivery.EventType) == "" || strings.TrimSpace(delivery.CorrelationKey) == "" || strings.TrimSpace(delivery.CorrelationValue) == "" || delivery.OccurredAt.IsZero() || delivery.ExpectedInstanceID == uuid.Nil {
		return clockservice.SignalReceipt{}, errors.New("clock signal receive: event, correlation, and occurred time are required")
	}
	received := r.Now().UTC()
	if received.IsZero() {
		return clockservice.SignalReceipt{}, errors.New("clock signal receive: trusted clock returned zero")
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return clockservice.SignalReceipt{}, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, delivery.TenantID); err != nil {
		return clockservice.SignalReceipt{}, err
	}
	receipt, err := (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{
		Signal: stepSignal.Signal{
			Tenant: values.TenantId(delivery.TenantID.String()), Source: delivery.Source,
			EventType: delivery.EventType, SchemaRef: delivery.SchemaRef,
			CorrelationKey: delivery.CorrelationKey, CorrelationValue: delivery.CorrelationValue,
			IdempotencyKey: delivery.IdempotencyKey, Payload: append([]byte(nil), delivery.Payload...),
			ReceivedAt: values.NewInstant(received),
		}, ReceivedAt: received,
	}, r.Verify)
	if err != nil {
		return clockservice.SignalReceipt{}, err
	}
	if len(receipt.Dispositions) == 0 || receipt.Dispositions[0].SubscriptionID == uuid.Nil {
		return clockservice.SignalReceipt{}, errors.New("clock signal receive: no active workflow subscription matched")
	}
	for _, disposition := range receipt.Dispositions {
		// A refused delivery has a subscription too; resuming on it would only
		// surface later as an unexplained drift, so name the refusal here.
		if strings.HasPrefix(string(disposition.Status), "REFUSED") {
			return clockservice.SignalReceipt{}, errors.New("clock signal receive: the workflow refused the signal: " + string(disposition.Status) + ": " + disposition.Reason)
		}
		var instanceID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT instance_id FROM workflow_signal_subscription WHERE tenant_id=$1 AND subscription_id=$2`, delivery.TenantID, disposition.SubscriptionID).Scan(&instanceID); err != nil {
			return clockservice.SignalReceipt{}, err
		}
		if instanceID != delivery.ExpectedInstanceID {
			return clockservice.SignalReceipt{}, errors.New("clock signal receive: matched subscription belongs to another workflow instance")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return clockservice.SignalReceipt{}, err
	}
	return clockservice.SignalReceipt{SignalID: receipt.SignalID, SubscriptionID: firstSubscription(receipt)}, nil
}

func firstSubscription(receipt signals.Receipt) uuid.UUID {
	return receipt.Dispositions[0].SubscriptionID
}
