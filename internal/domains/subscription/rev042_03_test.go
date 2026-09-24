package subscription

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

type blockedDeliveryProvider struct {
	entered chan DeliveryAttempt
	release chan struct{}
}

func (p *blockedDeliveryProvider) Deliver(ctx context.Context, attempt DeliveryAttempt) (ProviderReceipt, error) {
	select {
	case p.entered <- attempt:
	case <-ctx.Done():
		return ProviderReceipt{}, ctx.Err()
	}
	select {
	case <-p.release:
		return ProviderReceipt{ReceiptID: "receipt", Provider: "fixture", Accepted: true}, nil
	case <-ctx.Done():
		return ProviderReceipt{}, ctx.Err()
	}
}

func fairDispatcher(tenantShare, destinationCapacity int) Dispatcher {
	capacity, err := NewDeliveryCapacity(map[string]operation.ConnectorPolicy{
		"endpoint:slow": {Quota: operation.ConnectorQuota{MaxConcurrent: destinationCapacity}, PerTenantShare: tenantShare},
		"endpoint:fast": {Quota: operation.ConnectorQuota{MaxConcurrent: destinationCapacity}, PerTenantShare: tenantShare},
	})
	if err != nil {
		panic(err)
	}
	return Dispatcher{
		Journal: NewDeliveryJournal(), Gate: NewDeliveryGate(), DLQ: NewDeadLetterQueue(time.Hour),
		Policy:          RetryPolicy{MaxAttempts: 2, InitialDelay: time.Millisecond, MaxDelay: time.Second},
		DeadLetterOwner: "team:integrations",
		Capacity:        capacity,
	}
}

func TestTodo_REV_042_03_Security(t *testing.T) {
	invalid := []operation.ConnectorPolicy{
		{Quota: operation.ConnectorQuota{MaxConcurrent: 1}, PerTenantShare: 1},
		{Quota: operation.ConnectorQuota{MaxConcurrent: 2}},
		{Quota: operation.ConnectorQuota{MaxConcurrent: 2}, PerTenantShare: 2},
		{Quota: operation.ConnectorQuota{MaxConcurrent: 4}, PerTenantShare: 4},
		{Quota: operation.ConnectorQuota{MaxConcurrent: 4}, PerTenantShare: -1},
	}
	for i, policy := range invalid {
		if _, err := NewDeliveryCapacity(map[string]operation.ConnectorPolicy{"endpoint:x": policy}); !errors.Is(err, ErrInvalidCapacity) {
			t.Errorf("unsafe policy %d error = %v", i, err)
		}
	}

	// An unknown destination is fail-closed by the reused connector ledger:
	// one concurrent reservation, and the next request is rate-limited.
	capacity, err := NewDeliveryCapacity(nil)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	candidate := operation.ScheduleCandidate{OperationID: id, TenantID: "tenant-a", ConnectionID: "endpoint:unknown", ResourceKey: "endpoint:unknown", Criticality: "P2"}
	if ok, reason := capacity.tryReserve(time.Now(), candidate); !ok || reason != "" {
		t.Fatalf("unknown first reservation = %t %q", ok, reason)
	}
	if ok, reason := capacity.tryReserve(time.Now(), operation.ScheduleCandidate{OperationID: uuid.New(), TenantID: "tenant-b", ConnectionID: "endpoint:unknown", ResourceKey: "endpoint:unknown", Criticality: "P2"}); ok || reason != operation.ScheduleReasonRateLimited {
		t.Fatalf("unknown fallback allowed a second tenant: %t %q", ok, reason)
	}
}

// TestTodo_REV_042_03 proves a noisy tenant cannot reserve every slot at a
// destination and that a separate destination retains independent capacity.
func TestTodo_REV_042_03(t *testing.T) {
	dispatcher := fairDispatcher(1, 2)
	provider := &blockedDeliveryProvider{entered: make(chan DeliveryAttempt, 4), release: make(chan struct{}, 4)}
	ctx := context.Background()
	results := make(chan error, 1)
	go func() {
		request := tenantRequest("sub-a1", "tenant-a", 1)
		request.Destination = "endpoint:slow"
		_, err := dispatcher.Dispatch(ctx, provider, request)
		results <- err
	}()
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("first tenant did not reach provider")
	}

	// A second request from tenant-a is bounded by its share; tenant-b uses
	// the free destination slot immediately while tenant-a's request is slow.
	noisy := tenantRequest("sub-a2", "tenant-a", 1)
	noisy.Destination = "endpoint:slow"
	if _, err := dispatcher.Dispatch(ctx, provider, noisy); !errors.Is(err, ErrDeliveryCapacity) {
		t.Fatalf("noisy tenant second reservation = %v, want capacity refusal", err)
	}
	otherDone := make(chan error, 1)
	go func() {
		request := tenantRequest("sub-b", "tenant-b", 1)
		request.Destination = "endpoint:slow"
		_, err := dispatcher.Dispatch(ctx, provider, request)
		otherDone <- err
	}()
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("tenant-b exceeded the 1s fairness SLO behind tenant-a")
	}

	// The slow endpoint cannot consume the separate fast destination's budget.
	fast := DeliveryProviderFunc(func(_ context.Context, _ DeliveryAttempt) (ProviderReceipt, error) {
		return ProviderReceipt{ReceiptID: "fast", Provider: "fixture", Accepted: true}, nil
	})
	fastReq := tenantRequest("sub-fast", "tenant-c", 1)
	fastReq.Destination = "endpoint:fast"
	if _, err := dispatcher.Dispatch(ctx, fast, fastReq); err != nil {
		t.Fatalf("independent destination = %v", err)
	}

	provider.release <- struct{}{}
	provider.release <- struct{}{}
	if err := <-results; err != nil {
		t.Fatalf("slow dispatch = %v", err)
	}
	if err := <-otherDone; err != nil {
		t.Fatalf("tenant-b dispatch = %v", err)
	}
}

// TestTodo_REV_042_03_Race exercises simultaneous reservations and releases.
func TestTodo_REV_042_03_Race(t *testing.T) {
	dispatcher := fairDispatcher(2, 4)
	provider := &deliveryProvider{}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tenant := "tenant-a"
			if i%2 != 0 {
				tenant = "tenant-b"
			}
			_, _ = dispatcher.Dispatch(context.Background(), provider, tenantRequest(fmt.Sprintf("sub-%d", i), tenant, 1))
		}(i)
	}
	wg.Wait()
	if got := dispatcher.Capacity.InFlight("endpoint:slow"); got != 0 {
		t.Fatalf("reservations leaked: %d", got)
	}
}

// TestTodo_REV_042_03_Integration drives admission, delivery journaling and
// provider completion through the production dispatcher path.
func TestTodo_REV_042_03_Integration(t *testing.T) {
	dispatcher := fairDispatcher(1, 2)
	provider := &deliveryProvider{}
	result, err := dispatcher.Dispatch(context.Background(), provider, tenantRequest("sub-int", "tenant-int", 1))
	if err != nil || result.Operation.State != OperationAcked || len(result.Operation.Attempts) != 1 {
		t.Fatalf("integration dispatch = %+v, %v", result, err)
	}
	if got := dispatcher.Capacity.InFlight("endpoint:slow"); got != 0 {
		t.Fatalf("completed delivery retained capacity: %d", got)
	}
}
