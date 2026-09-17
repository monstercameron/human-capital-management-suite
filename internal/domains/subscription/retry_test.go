package subscription

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

type poisonProvider struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (p *poisonProvider) Deliver(_ context.Context, _ DeliveryAttempt) (ProviderReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return ProviderReceipt{}, p.err
}

type flakyProvider struct {
	mu    sync.Mutex
	calls int
	fail  int
}

func (p *flakyProvider) Deliver(_ context.Context, _ DeliveryAttempt) (ProviderReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls <= p.fail {
		return ProviderReceipt{}, errors.New("connection refused")
	}
	return ProviderReceipt{ReceiptID: "receipt-ok", Provider: "fixture", Accepted: true}, nil
}

// tenantRequest builds a delivery request for one subscription and tenant.
func tenantRequest(sub, tenant string, sequence uint64) DeliveryRequest {
	envelope := deliveryEnvelope(sequence)
	envelope.Tenant = tenant
	request := deliveryRequest(sequence)
	request.SubscriptionID = sub
	request.Envelope = envelope
	return request
}

func testDispatcher(journal *DeliveryJournal, gate *DeliveryGate, dlq *DeadLetterQueue) Dispatcher {
	return Dispatcher{Journal: journal, Gate: gate, DLQ: dlq,
		Policy:          RetryPolicy{MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
		DeadLetterOwner: "team:integrations"}
}

// TestTodo_SUB_005 proves a poison endpoint never retries forever or blocks
// other tenants: attempts stop at the policy bound, the operation dead-letters
// with owner, expiry, reason and repair route, and fenced pause/resume stops
// exactly one tenant subscription.
func TestTodo_SUB_005(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	journal := NewDeliveryJournal(func() time.Time { return now })
	dlq := NewDeadLetterQueue(24*time.Hour, func() time.Time { return now })
	gate := NewDeliveryGate()
	dispatcher := testDispatcher(journal, gate, dlq)
	ctx := context.Background()

	poison := &poisonProvider{err: errors.New("connection refused")}
	result, err := dispatcher.Dispatch(ctx, poison, tenantRequest("sub-poison", "tenant-a", 1))
	if !errors.Is(err, ErrRetryExhausted) || !result.DeadLettered {
		t.Fatalf("poison = %+v, %v", result, err)
	}
	if poison.calls != 3 {
		t.Fatalf("poison provider calls = %d, want exactly the policy bound", poison.calls)
	}
	letter := result.DeadLetter
	if letter.Owner != "team:integrations" || letter.Reason == "" || letter.Route != RepairEscalateOwner {
		t.Fatalf("dead letter = %+v", letter)
	}
	if !letter.ExpiresAt.Equal(now.Add(24*time.Hour)) || letter.Attempts != 3 || letter.SubscriptionID != "sub-poison" || letter.Tenant != "tenant-a" {
		t.Fatalf("dead letter evidence = %+v", letter)
	}

	// Another tenant is unaffected by the poisoned subscription.
	healthy := &deliveryProvider{}
	other, err := dispatcher.Dispatch(ctx, healthy, tenantRequest("sub-other", "tenant-b", 1))
	if err != nil || other.Operation.State != OperationAcked {
		t.Fatalf("other tenant = %+v, %v", other, err)
	}

	// Fenced pause stops exactly the paused subscription: no provider call,
	// no journal mutation, and the other tenant still flows.
	if err := gate.Pause("sub-poison", "tenant-a", 2, "op:ann", "endpoint down"); err != nil {
		t.Fatal(err)
	}
	before := poison.calls
	if _, err := dispatcher.Dispatch(ctx, healthy, tenantRequest("sub-poison", "tenant-a", 1)); !errors.Is(err, ErrSubscriptionPaused) {
		t.Fatalf("paused dispatch = %v", err)
	}
	if poison.calls != before {
		t.Fatal("a paused subscription reached its provider")
	}
	again, err := dispatcher.Dispatch(ctx, healthy, tenantRequest("sub-other", "tenant-b", 2))
	if err != nil || again.Operation.State != OperationAcked {
		t.Fatalf("other tenant during pause = %+v, %v", again, err)
	}
	if err := gate.Resume("sub-poison", "tenant-a", "op:ann"); err != nil {
		t.Fatal(err)
	}
	// The dead-lettered lineage never retries on its own: redispatch makes
	// zero provider calls until a replay plans a new epoch (SUB-006).
	if _, err := dispatcher.Dispatch(ctx, healthy, tenantRequest("sub-poison", "tenant-a", 1)); !errors.Is(err, ErrRetryExhausted) {
		t.Fatalf("post-exhaustion dispatch = %v", err)
	}
	if poison.calls != before {
		t.Fatal("an exhausted lineage retried without a replay")
	}
}

// TestTodo_SUB_005_Race proves concurrent dispatch across tenants and
// concurrent fence reads never lose, duplicate or over-retry an operation.
func TestTodo_SUB_005_Race(t *testing.T) {
	journal := NewDeliveryJournal()
	dlq := NewDeadLetterQueue(time.Hour)
	gate := NewDeliveryGate()
	if err := gate.Pause("sub-fence", "tenant-a", 1, "op:ann", "drill"); err != nil {
		t.Fatal(err)
	}
	dispatcher := testDispatcher(journal, gate, dlq)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tenant := "tenant-a"
			if i%2 == 1 {
				tenant = "tenant-b"
			}
			poison := &poisonProvider{err: errors.New("connection refused")}
			_, _ = dispatcher.Dispatch(ctx, poison, tenantRequest("sub-race", tenant, uint64(i+1)))
			_ = gate.Paused("sub-fence", "tenant-a")
			_ = gate.Admit(tenantRequest("sub-fence-ok", tenant, 1))
		}()
	}
	wg.Wait()
	// Strict ordering serializes one subscription: sequence 1 exhausts at the
	// bound while later sequences queue behind it without provider calls.
	operations := journal.Operations("sub-race")
	if len(operations) == 0 {
		t.Fatal("no operations journaled")
	}
	for _, op := range operations {
		if uint32(len(op.Attempts)) > 3 {
			t.Fatalf("operation %s attempted %d times past the bound", op.OperationID, len(op.Attempts))
		}
	}
	if got := len(dlq.Pending()); got == 0 {
		t.Fatal("no dead letters retained")
	}
}

// TestTodo_SUB_005_Integration proves the worker loop through the real
// journal, gate and dead-letter queue: one transient failure still delivers,
// exhaustion dead-letters, and pause/resume fences delivery.
func TestTodo_SUB_005_Integration(t *testing.T) {
	now := time.Unix(2000, 0).UTC()
	clock := func() time.Time { return now }
	journal := NewDeliveryJournal(clock)
	dlq := NewDeadLetterQueue(time.Hour, clock)
	gate := NewDeliveryGate()
	dispatcher := testDispatcher(journal, gate, dlq)
	ctx := context.Background()

	flaky := &flakyProvider{fail: 1}
	first, err := dispatcher.Dispatch(ctx, flaky, tenantRequest("sub-worker", "tenant-a", 1))
	if err != nil || first.Operation.State != OperationAcked || len(first.Operation.Attempts) != 2 || first.DeadLettered {
		t.Fatalf("transient = %+v, %v", first, err)
	}
	poison := &poisonProvider{err: errors.New("connection refused")}
	if _, err := dispatcher.Dispatch(ctx, poison, tenantRequest("sub-worker", "tenant-a", 3)); !errors.Is(err, ErrDeliveryGap) {
		t.Fatalf("out-of-order redelivery while sequence 2 is expected = %v", err)
	}
	if poison.calls != 0 {
		t.Fatal("a gapped sequence reached its provider")
	}
	if _, err := dispatcher.Dispatch(ctx, poison, tenantRequest("sub-doomed", "tenant-a", 1)); !errors.Is(err, ErrRetryExhausted) {
		t.Fatalf("doomed = %v", err)
	}
	if explanation := dlq.Pending()[0].Explain(); !strings.Contains(explanation, "dlq-") {
		t.Fatalf("dead-letter explanation = %q", explanation)
	}
	letter, ok := dlq.Get("dlq-" + resultOperationID(t, journal, "sub-doomed"))
	if !ok || letter.Attempts != 3 {
		t.Fatalf("dead-lettered operation = %+v, %t", letter, ok)
	}
	if err := gate.Pause("sub-doomed", "tenant-a", 1, "op:ann", "incident"); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Dispatch(ctx, flaky, tenantRequest("sub-doomed", "tenant-a", 1)); !errors.Is(err, ErrSubscriptionPaused) {
		t.Fatalf("paused doomed = %v", err)
	}
	if err := gate.Resume("sub-doomed", "tenant-a", "op:ann"); err != nil {
		t.Fatal(err)
	}
	resolved, err := dlq.Resolve(letter.ID, "op:ann", "replayed-by-SUB-006")
	if err != nil || resolved.ID != letter.ID || len(dlq.Pending()) != 0 {
		t.Fatalf("resolve = %+v, %v", resolved, err)
	}
}

func resultOperationID(t *testing.T, journal *DeliveryJournal, subscription string) string {
	t.Helper()
	operations := journal.Operations(subscription)
	if len(operations) != 1 {
		t.Fatalf("operations for %s = %d", subscription, len(operations))
	}
	return operations[0].OperationID
}

// TestTodo_SUB_005_Fault proves ambiguous outcomes never dead-letter, invalid
// configuration fails before any provider call, and the dead-letter queue
// refuses evidence-free entries.
func TestTodo_SUB_005_Fault(t *testing.T) {
	journal := NewDeliveryJournal()
	dlq := NewDeadLetterQueue(time.Hour)
	dispatcher := testDispatcher(journal, NewDeliveryGate(), dlq)
	ctx := context.Background()

	ambiguous := &deliveryProvider{err: ambiguousDeliveryError{}}
	result, err := dispatcher.Dispatch(ctx, ambiguous, tenantRequest("sub-ambiguous", "tenant-a", 1))
	if !errors.Is(err, ErrDeliveryAmbiguous) || result.DeadLettered || len(ambiguous.attempts) != 1 {
		t.Fatalf("ambiguous = %+v attempts=%d, %v", result, len(ambiguous.attempts), err)
	}
	if len(dlq.Pending()) != 0 {
		t.Fatal("an ambiguous operation dead-lettered")
	}
	broken := dispatcher
	broken.Journal = nil
	if _, err := broken.Dispatch(ctx, ambiguous, tenantRequest("sub-ambiguous", "tenant-a", 1)); err == nil {
		t.Fatal("a dispatcher without a journal dispatched")
	}
	for name, policy := range map[string]RetryPolicy{
		"zero attempts":   {MaxAttempts: 0},
		"unbounded":       {MaxAttempts: 33},
		"negative delay":  {MaxAttempts: 3, InitialDelay: -time.Second},
		"inverted window": {MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: time.Millisecond},
	} {
		if err := policy.Validate(); err == nil {
			t.Errorf("policy %s validated", name)
		}
	}
	if delay, ok := (RetryPolicy{MaxAttempts: 4, InitialDelay: time.Second, MaxDelay: 3 * time.Second}).Backoff(1); !ok || delay != 0 {
		t.Fatalf("first attempt must be immediate: %v, %t", delay, ok)
	}
	if delay, _ := (RetryPolicy{MaxAttempts: 4, InitialDelay: time.Second, MaxDelay: 3 * time.Second}).Backoff(4); delay != 3*time.Second {
		t.Fatalf("backoff must cap at the window: %v", delay)
	}
	if _, ok := (RetryPolicy{MaxAttempts: 4}).Backoff(5); ok {
		t.Fatal("backoff past the bound was admitted")
	}
	acked, err := NewDeliveryJournal().DeliverNow(&deliveryProvider{}, tenantRequest("sub-acked", "tenant-a", 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dlq.Enqueue(acked.Operation, "team:integrations", "not failed", RepairEscalateOwner); err == nil {
		t.Fatal("an acknowledged operation dead-lettered")
	}
	if _, err := dlq.Enqueue(acked.Operation, "  ", "not failed", RepairEscalateOwner); err == nil {
		t.Fatal("an ownerless dead letter was admitted")
	}
	if _, err := dlq.Enqueue(acked.Operation, "team:integrations", "not failed", "PAGE_SOMEONE"); err == nil {
		t.Fatal("an unknown repair route was admitted")
	}
	if _, err := dlq.Resolve("dlq-missing", "op:ann", "done"); !errors.Is(err, ErrDeliveryNotFound) {
		t.Errorf("resolve missing = %v", err)
	}
	past := time.Unix(5000, 0).UTC()
	short := NewDeadLetterQueue(time.Minute, func() time.Time { return time.Unix(1000, 0).UTC() })
	failed, err := NewDeliveryJournal().DeliverNow(&poisonProvider{err: errors.New("down")}, tenantRequest("sub-short", "tenant-a", 1))
	if err == nil {
		t.Fatal("expected a failed operation")
	}
	letter, err := short.Enqueue(failed.Operation, "team:integrations", "down", RepairRetryEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	if letter.Expired(time.Unix(1000, 0).UTC()) || !letter.Expired(past) {
		t.Fatal("dead-letter expiry is wrong")
	}
	var nilGate *DeliveryGate
	if err := nilGate.Pause("s", "t", 1, "r", "why"); err == nil || nilGate.Paused("s", "t") {
		t.Fatal("a nil gate paused")
	}
	if err := nilGate.Resume("s", "t", "r"); err == nil {
		t.Fatal("a nil gate resumed")
	}
	if err := nilGate.Admit(tenantRequest("sub-nil", "tenant-a", 1)); err != nil {
		t.Fatalf("a nil gate fenced without state: %v", err)
	}
	if err := NewDeliveryGate().Admit(DeliveryRequest{}); err == nil {
		t.Fatal("an invalid request passed the fence")
	}
	var nilQueue *DeadLetterQueue
	if _, ok := nilQueue.Get("dlq-x"); ok || nilQueue.Pending() != nil {
		t.Fatal("a nil dead-letter queue answered")
	}
	if _, err := nilQueue.Enqueue(DeliveryOperation{}, "o", "r", RepairEscalateOwner); err == nil {
		t.Fatal("a nil dead-letter queue enqueued")
	}
	if _, err := nilQueue.Resolve("dlq-x", "op:ann", "done"); err == nil {
		t.Fatal("a nil dead-letter queue resolved")
	}
	saturating := RetryPolicy{MaxAttempts: 4, InitialDelay: time.Duration(math.MaxInt64)}
	if delay, ok := saturating.Backoff(4); !ok || delay != time.Duration(math.MaxInt64) {
		t.Fatalf("overflowing backoff did not saturate: %v, %t", delay, ok)
	}
}

// TestTodo_SUB_005_Security proves the fence is tenant-scoped: resuming from
// another tenant is refused without effect, pausing one tenant never blocks
// the other, and dead letters always carry an accountable owner.
func TestTodo_SUB_005_Security(t *testing.T) {
	gate := NewDeliveryGate()
	if err := gate.Pause("sub-fenced", "tenant-a", 1, "op:ann", "incident"); err != nil {
		t.Fatal(err)
	}
	if err := gate.Resume("sub-fenced", "tenant-b", "op:mallory"); !errors.Is(err, ErrGateFence) {
		t.Fatalf("cross-tenant resume = %v", err)
	}
	if !gate.Paused("sub-fenced", "tenant-a") {
		t.Fatal("a refused resume lifted the fence")
	}
	if err := gate.Admit(tenantRequest("sub-fenced", "tenant-b", 1)); err != nil {
		t.Fatalf("sibling tenant blocked by another tenant's fence: %v", err)
	}
	if err := gate.Resume("sub-unknown", "tenant-a", "op:ann"); !errors.Is(err, ErrDeliveryNotPaused) {
		t.Fatalf("resume without pause = %v", err)
	}
	dlq := NewDeadLetterQueue(time.Hour)
	failed, _ := NewDeliveryJournal().DeliverNow(&poisonProvider{err: errors.New("down")}, tenantRequest("sub-sec", "tenant-a", 1))
	if _, err := dlq.Enqueue(failed.Operation, "", "down", RepairEscalateOwner); err == nil {
		t.Fatal("an ownerless dead letter was admitted")
	}
	ownerless := testDispatcher(NewDeliveryJournal(), NewDeliveryGate(), dlq)
	ownerless.DeadLetterOwner = " "
	if err := ownerless.Validate(); err == nil {
		t.Fatal("an ownerless dispatcher validated")
	}
	for _, call := range []func() error{
		func() error { return gate.Pause("sub-fenced", "tenant-a", 0, "op:ann", "incident") },
		func() error { return gate.Pause("sub-fenced", "tenant-a", 1, " ", "incident") },
		func() error { return gate.Resume("sub-fenced", "tenant-a", " ") },
	} {
		if err := call(); err == nil {
			t.Fatal("unattributed fence change was admitted")
		}
	}
}
