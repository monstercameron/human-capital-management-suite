package subscription

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

type capacityBlockingProvider struct {
	entered chan struct{}
	release <-chan struct{}
}

func (p capacityBlockingProvider) Deliver(ctx context.Context, attempt DeliveryAttempt) (ProviderReceipt, error) {
	select {
	case p.entered <- struct{}{}:
	case <-ctx.Done():
		return ProviderReceipt{}, ctx.Err()
	}
	select {
	case <-p.release:
		return ProviderReceipt{ReceiptID: "receipt:" + attempt.OperationKey, Provider: "fixture", Accepted: true}, nil
	case <-ctx.Done():
		return ProviderReceipt{}, ctx.Err()
	}
}

func TestDeliveryCapacityClaimsAreScopedToTenantAndDestination(t *testing.T) {
	for _, tc := range []struct {
		name              string
		secondTenant      string
		secondDestination string
	}{
		{name: "different tenant same destination", secondTenant: "tenant-b", secondDestination: "endpoint:slow"},
		{name: "same tenant different destination", secondTenant: "tenant-a", secondDestination: "endpoint:fast"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capacity, err := NewDeliveryCapacity(map[string]operation.ConnectorPolicy{
				"endpoint:slow": {Quota: operation.ConnectorQuota{MaxConcurrent: 2}, PerTenantShare: 1},
				"endpoint:fast": {Quota: operation.ConnectorQuota{MaxConcurrent: 2}, PerTenantShare: 1},
			})
			if err != nil {
				t.Fatal(err)
			}
			dispatcher := Dispatcher{
				Journal: NewDeliveryJournal(), Gate: NewDeliveryGate(), DLQ: NewDeadLetterQueue(time.Hour), Capacity: capacity,
				Policy: RetryPolicy{MaxAttempts: 1, InitialDelay: time.Millisecond, MaxDelay: time.Second}, DeadLetterOwner: "team:integrations",
			}
			release := make(chan struct{})
			released := false
			defer func() {
				if !released {
					close(release)
				}
			}()
			provider := capacityBlockingProvider{entered: make(chan struct{}, 2), release: release}
			first := tenantRequest("sub-a", "tenant-a", 1)
			first.Destination, first.IdempotencyKey = "endpoint:slow", "shared-idempotency-key"
			firstDone := make(chan error, 1)
			go func() {
				_, err := dispatcher.Dispatch(context.Background(), provider, first)
				firstDone <- err
			}()
			select {
			case <-provider.entered:
			case <-time.After(time.Second):
				t.Fatal("first operation did not reserve capacity and reach its provider")
			}

			second := tenantRequest("sub-b", tc.secondTenant, 1)
			second.Destination, second.IdempotencyKey = tc.secondDestination, first.IdempotencyKey
			if _, err := dispatcher.Dispatch(context.Background(), provider, second); !errors.Is(err, ErrDeliveryConflict) {
				t.Fatalf("second operation error = %v, want journal idempotency conflict after independent capacity admission", err)
			}
			if got := capacity.InFlight("endpoint:slow"); got != 1 {
				t.Fatalf("slow destination reservations = %d, want first operation only", got)
			}
			if got := capacity.InFlight("endpoint:fast"); got != 0 {
				t.Fatalf("fast destination reservations = %d, want released second reservation", got)
			}
			close(release)
			released = true
			if err := <-firstDone; err != nil {
				t.Fatalf("first operation error = %v", err)
			}
		})
	}
}
