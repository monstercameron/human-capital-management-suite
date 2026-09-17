package subscription

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Retry and dead-letter errors are stable so workers can route a poisoned
// subscription to observation or repair without inspecting provider text.
var (
	ErrInvalidRetryPolicy = errors.New("subscription: invalid retry policy")
	ErrRetryExhausted     = errors.New("subscription: delivery attempts exhausted")
	ErrSubscriptionPaused = errors.New("subscription: subscription delivery is paused")
	ErrGateFence          = errors.New("subscription: pause fence denied a cross-tenant resume")
	ErrDeliveryNotPaused  = errors.New("subscription: subscription delivery is not paused")
	ErrInvalidDeadLetter  = errors.New("subscription: invalid dead-letter entry")
)

// maxRetryAttempts bounds every policy. A poison endpoint stops here no
// matter how many workers redispatch its operations.
const maxRetryAttempts = 32

// RetryPolicy bounds redelivery of one operation lineage. The first attempt
// is immediate; Backoff reports the delay a worker waits before each later
// attempt. Dispatch retries without sleeping; workers use Backoff to
// schedule.
type RetryPolicy struct {
	MaxAttempts  uint32
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// Validate rejects unbounded, inverted or negative policies.
func (p RetryPolicy) Validate() error {
	if p.MaxAttempts == 0 || p.MaxAttempts > maxRetryAttempts {
		return fmt.Errorf("%w: max attempts must hold 1 to %d deliveries", ErrInvalidRetryPolicy, maxRetryAttempts)
	}
	if p.InitialDelay < 0 || p.MaxDelay < 0 {
		return fmt.Errorf("%w: delays may not be negative", ErrInvalidRetryPolicy)
	}
	if p.MaxDelay < p.InitialDelay {
		return fmt.Errorf("%w: max delay %v is below initial delay %v", ErrInvalidRetryPolicy, p.MaxDelay, p.InitialDelay)
	}
	return nil
}

// Backoff returns the delay before the given 1-based attempt number. Attempt
// 1 is immediate; later attempts double from the initial delay up to the max.
// It reports false past the attempt bound.
func (p RetryPolicy) Backoff(attempt uint32) (time.Duration, bool) {
	if attempt == 0 || attempt > p.MaxAttempts {
		return 0, false
	}
	if attempt == 1 {
		return 0, true
	}
	delay := p.InitialDelay
	for i := uint32(2); i < attempt; i++ {
		delay *= 2
		if delay < 0 {
			delay = time.Duration(math.MaxInt64)
			break
		}
	}
	if p.MaxDelay > 0 && delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	return delay, true
}

// RepairRoute is the closed set of human repair dispositions for a
// dead-lettered operation.
type RepairRoute string

const (
	RepairRetryEndpoint   RepairRoute = "RETRY_ENDPOINT"
	RepairEscalateOwner   RepairRoute = "ESCALATE_OWNER"
	RepairDiscardApproved RepairRoute = "DISCARD_WITH_APPROVAL"
)

func (r RepairRoute) valid() bool {
	return r == RepairRetryEndpoint || r == RepairEscalateOwner || r == RepairDiscardApproved
}

// DeadLetter is the retained evidence for one exhausted operation. It holds
// only canonical references and delivery evidence, never an event payload.
type DeadLetter struct {
	ID             string
	OperationID    string
	SubscriptionID string
	Tenant         string
	Attempts       uint32
	LastError      string
	Request        DeliveryRequest
	Owner          string
	Reason         string
	Route          RepairRoute
	EnqueuedAt     time.Time
	ExpiresAt      time.Time
}

// Expired reports whether the entry is past its retention horizon.
func (d DeadLetter) Expired(at time.Time) bool { return !at.Before(d.ExpiresAt) }

// Explain returns a redaction-safe dead-letter summary.
func (d DeadLetter) Explain() string {
	return fmt.Sprintf("subscription dead letter=%s operation=%s subscription=%s attempts=%d route=%s owner=%s", d.ID, d.OperationID, d.SubscriptionID, d.Attempts, d.Route, d.Owner)
}

// DeadLetterQueue retains exhausted operations with an accountable owner,
// an expiry and a repair route. It is concurrency-safe and intentionally
// does not contact a database or network.
type DeadLetterQueue struct {
	mu    sync.Mutex
	now   func() time.Time
	ttl   time.Duration
	items map[string]DeadLetter
	order []string
}

// NewDeadLetterQueue returns an empty queue. Entries expire ttl after they
// are enqueued. The optional clock makes expiry deterministic in tests.
func NewDeadLetterQueue(ttl time.Duration, now ...func() time.Time) *DeadLetterQueue {
	clock := func() time.Time { return time.Now().UTC() }
	if len(now) > 0 && now[0] != nil {
		clock = now[0]
	}
	return &DeadLetterQueue{now: clock, ttl: ttl, items: make(map[string]DeadLetter)}
}

// Enqueue retains one failed operation. Only FAILED journal operations with
// delivery evidence dead-letter; re-enqueueing one operation returns the
// stored entry so concurrent workers cannot duplicate it.
func (q *DeadLetterQueue) Enqueue(operation DeliveryOperation, owner, reason string, route RepairRoute) (DeadLetter, error) {
	if q == nil {
		return DeadLetter{}, fmt.Errorf("%w: queue is required", ErrInvalidDeadLetter)
	}
	if q.ttl <= 0 {
		return DeadLetter{}, fmt.Errorf("%w: dead-letter TTL is not configured", ErrInvalidDeadLetter)
	}
	if operation.State != OperationFailed || len(operation.Attempts) == 0 {
		return DeadLetter{}, fmt.Errorf("%w: only failed operations with attempts dead-letter", ErrInvalidDeadLetter)
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(owner) != owner {
		return DeadLetter{}, fmt.Errorf("%w: owner is required and may not be padded", ErrInvalidDeadLetter)
	}
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(reason) != reason {
		return DeadLetter{}, fmt.Errorf("%w: reason is required and may not be padded", ErrInvalidDeadLetter)
	}
	if !route.valid() {
		return DeadLetter{}, fmt.Errorf("%w: unknown repair route %q", ErrInvalidDeadLetter, route)
	}
	id := "dlq-" + operation.OperationID
	now := q.now().UTC()
	q.mu.Lock()
	defer q.mu.Unlock()
	if stored, ok := q.items[id]; ok {
		return stored, nil
	}
	letter := DeadLetter{ID: id, OperationID: operation.OperationID, SubscriptionID: operation.Request.SubscriptionID,
		Tenant: operation.Request.Envelope.Tenant, Attempts: uint32(len(operation.Attempts)), LastError: operation.LastError,
		Request: cloneDeliveryRequest(operation.Request), Owner: owner, Reason: reason, Route: route,
		EnqueuedAt: now, ExpiresAt: now.Add(q.ttl)}
	q.items[id] = letter
	q.order = append(q.order, id)
	return letter, nil
}

// Resolve removes one entry with an attributed resolver and disposition and
// returns it as repair evidence.
func (q *DeadLetterQueue) Resolve(id, resolver, disposition string) (DeadLetter, error) {
	if q == nil {
		return DeadLetter{}, fmt.Errorf("%w: queue is required", ErrInvalidDeadLetter)
	}
	for name, value := range map[string]string{"id": id, "resolver": resolver, "disposition": disposition} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return DeadLetter{}, fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidDeadLetter, name)
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	letter, ok := q.items[id]
	if !ok {
		return DeadLetter{}, fmt.Errorf("%w: %s: %w", ErrInvalidDeadLetter, id, ErrDeliveryNotFound)
	}
	delete(q.items, id)
	for i, key := range q.order {
		if key == id {
			q.order = append(q.order[:i], q.order[i+1:]...)
			break
		}
	}
	return letter, nil
}

// Get returns a detached dead-letter entry by id.
func (q *DeadLetterQueue) Get(id string) (DeadLetter, bool) {
	if q == nil {
		return DeadLetter{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	letter, ok := q.items[id]
	return letter, ok
}

// Pending returns every retained entry in enqueue order.
func (q *DeadLetterQueue) Pending() []DeadLetter {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]DeadLetter, 0, len(q.order))
	for _, id := range q.order {
		out = append(out, q.items[id])
	}
	return out
}

// gatePause is one fenced delivery halt. The tenant is part of the key: a
// pause never affects another tenant, even for the same subscription id.
type gatePause struct {
	revision  uint64
	requester string
	reason    string
}

// DeliveryGate fences pause and resume per tenant subscription. Paused
// operations never reach a provider; resume from another tenant is refused
// without effect. It is concurrency-safe.
type DeliveryGate struct {
	mu     sync.Mutex
	paused map[string]gatePause
}

// NewDeliveryGate returns an empty gate.
func NewDeliveryGate() *DeliveryGate { return &DeliveryGate{paused: make(map[string]gatePause)} }

func gateKey(subscriptionID, tenant string) string { return tenant + "\x00" + subscriptionID }

// Pause halts delivery for one tenant subscription. Re-pausing refreshes the
// attribution without duplicating the fence.
func (g *DeliveryGate) Pause(subscriptionID, tenant string, revision uint64, requester, reason string) error {
	if g == nil {
		return fmt.Errorf("%w: gate is required", ErrInvalidDelivery)
	}
	for name, value := range map[string]string{"subscription_id": subscriptionID, "tenant": tenant, "requester": requester, "reason": reason} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidDelivery, name)
		}
	}
	if revision == 0 {
		return fmt.Errorf("%w: subscription revision must be positive", ErrInvalidDelivery)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paused[gateKey(subscriptionID, tenant)] = gatePause{revision: revision, requester: requester, reason: reason}
	return nil
}

// Resume lifts the fence for one tenant subscription. A resume naming
// another tenant is refused and the fence stays up.
func (g *DeliveryGate) Resume(subscriptionID, tenant, requester string) error {
	if g == nil {
		return fmt.Errorf("%w: gate is required", ErrInvalidDelivery)
	}
	for name, value := range map[string]string{"subscription_id": subscriptionID, "tenant": tenant, "requester": requester} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidDelivery, name)
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	key := gateKey(subscriptionID, tenant)
	if _, ok := g.paused[key]; !ok {
		for fenced := range g.paused {
			if strings.HasSuffix(fenced, "\x00"+subscriptionID) {
				return fmt.Errorf("%w: resume names tenant %s", ErrGateFence, tenant)
			}
		}
		return ErrDeliveryNotPaused
	}
	delete(g.paused, key)
	return nil
}

// Paused reports whether delivery is fenced for one tenant subscription.
func (g *DeliveryGate) Paused(subscriptionID, tenant string) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.paused[gateKey(subscriptionID, tenant)]
	return ok
}

// Admit refuses a request whose tenant subscription is paused. Invalid
// requests fail closed before the fence is consulted.
func (g *DeliveryGate) Admit(request DeliveryRequest) error {
	if g == nil {
		return nil
	}
	valid, err := request.validate()
	if err != nil {
		return err
	}
	if g.Paused(valid.SubscriptionID, valid.Envelope.Tenant) {
		return fmt.Errorf("%w: %s", ErrSubscriptionPaused, valid.SubscriptionID)
	}
	return nil
}

// Dispatcher is the delivery worker loop: one journal, one fence, one
// dead-letter queue and one bounded policy. Attempts are counted on the
// journal lineage, so concurrent workers still stop at the policy bound;
// exhausted lineages dead-letter instead of retrying, and dead-lettered
// lineages never retry without a replay (SUB-006).
type Dispatcher struct {
	Journal         *DeliveryJournal
	Gate            *DeliveryGate
	DLQ             *DeadLetterQueue
	Policy          RetryPolicy
	DeadLetterOwner string
}

// Validate rejects a dispatcher that could retry without evidence or
// dead-letter without an accountable owner.
func (d Dispatcher) Validate() error {
	if d.Journal == nil || d.DLQ == nil {
		return fmt.Errorf("%w: journal and dead-letter queue are required", ErrInvalidRetryPolicy)
	}
	if err := d.Policy.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(d.DeadLetterOwner) == "" || strings.TrimSpace(d.DeadLetterOwner) != d.DeadLetterOwner {
		return fmt.Errorf("%w: dead-letter owner is required and may not be padded", ErrInvalidRetryPolicy)
	}
	return nil
}

// DispatchResult reports one worker pass over a request. DeadLettered is
// true when the lineage exhausted its bound and was retained.
type DispatchResult struct {
	DeliveryResult
	DeadLetter   DeadLetter
	DeadLettered bool
}

// Dispatch delivers one request to at most the policy bound of provider
// calls, then dead-letters the lineage. Paused subscriptions never reach
// the provider; ambiguous outcomes stop without dead-lettering because they
// need reconciliation, not a retry verdict.
func (d Dispatcher) Dispatch(ctx context.Context, provider DeliveryProvider, request DeliveryRequest) (DispatchResult, error) {
	if err := d.Validate(); err != nil {
		return DispatchResult{}, err
	}
	effective, err := request.validate()
	if err != nil {
		return DispatchResult{}, err
	}
	if d.Gate != nil {
		if err := d.Gate.Admit(effective); err != nil {
			return DispatchResult{}, err
		}
	}
	for {
		if operation, ok := d.Journal.Operation(effective.IdempotencyKey); ok &&
			operation.State != OperationAcked && uint32(len(operation.Attempts)) >= d.Policy.MaxAttempts {
			return d.exhaust(operation)
		}
		result, err := d.Journal.Deliver(ctx, provider, effective)
		if err == nil {
			return DispatchResult{DeliveryResult: result}, nil
		}
		if errors.Is(err, ErrDeliveryAmbiguous) {
			return DispatchResult{DeliveryResult: result}, err
		}
		if !errors.Is(err, ErrDeliveryProvider) && !errors.Is(err, ErrDeliveryReceipt) {
			return DispatchResult{DeliveryResult: result}, err
		}
		if uint32(len(result.Operation.Attempts)) >= d.Policy.MaxAttempts {
			return d.exhaust(result.Operation)
		}
	}
}

// exhaust retains an exhausted lineage. An already dead-lettered lineage
// returns the same entry; either way no further provider call follows.
func (d Dispatcher) exhaust(operation DeliveryOperation) (DispatchResult, error) {
	reason := operation.LastError
	if reason == "" {
		reason = "delivery attempts exhausted"
	}
	letter, err := d.DLQ.Enqueue(operation, d.DeadLetterOwner, reason, RepairEscalateOwner)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("%w: %v", ErrRetryExhausted, err)
	}
	journaled, _ := d.Journal.Operation(operation.OperationID)
	return DispatchResult{
		DeliveryResult: DeliveryResult{Operation: journaled, Receipt: operation.ProviderReceipt},
		DeadLetter:     letter,
		DeadLettered:   true,
	}, fmt.Errorf("%w: %s", ErrRetryExhausted, letter.ID)
}
