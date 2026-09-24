package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Delivery errors are stable so callers can route an operation to retry,
// observation, or repair without inspecting provider-specific text.
var (
	ErrInvalidDelivery   = errors.New("subscription: invalid delivery request")
	ErrDeliveryConflict  = errors.New("subscription: delivery operation conflicts with an existing operation")
	ErrDeliveryGap       = errors.New("subscription: declared delivery sequence has a gap")
	ErrDeliveryReordered = errors.New("subscription: declared delivery sequence is already occupied")
	ErrDeliveryInFlight  = errors.New("subscription: delivery operation is already in flight")
	ErrDeliveryProvider  = errors.New("subscription: delivery provider refused the operation")
	ErrDeliveryReceipt   = errors.New("subscription: invalid provider receipt")
	ErrDeliveryAmbiguous = errors.New("subscription: provider outcome is ambiguous")
	ErrDeliveryNotFound  = errors.New("subscription: delivery operation not found")
	ErrDeliveryCapacity  = errors.New("subscription: delivery capacity unavailable")
	ErrInvalidCapacity   = errors.New("subscription: invalid delivery capacity policy")
)

// OrderingMode describes how a destination declares event ordering. Strict
// ordering is the safe default; independent delivery is available only when a
// destination has explicitly declared that events can commute.
type OrderingMode string

const (
	OrderingStrict      OrderingMode = "STRICT"
	OrderingIndependent OrderingMode = "INDEPENDENT"
)

// DeliveryRequest is the payload-free operation identity presented to a
// provider. Envelope is the canonical event envelope from SUB-003; no raw
// event payload crosses this package.
type DeliveryRequest struct {
	SubscriptionID       string
	SubscriptionRevision uint64
	Envelope             CanonicalEnvelope
	Destination          string
	OrderingKey          string
	OrderingMode         OrderingMode
	IdempotencyKey       string
	Signature            SignedDelivery
}

func (r DeliveryRequest) validate() (DeliveryRequest, error) {
	for name, value := range map[string]string{
		"subscription_id": r.SubscriptionID,
		"destination":     r.Destination,
		"ordering_key":    r.OrderingKey,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return DeliveryRequest{}, fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidDelivery, name)
		}
	}
	if r.SubscriptionRevision == 0 {
		return DeliveryRequest{}, fmt.Errorf("%w: subscription revision must be positive", ErrInvalidDelivery)
	}
	if err := r.Envelope.Validate(); err != nil {
		return DeliveryRequest{}, fmt.Errorf("%w: envelope: %v", ErrInvalidDelivery, err)
	}
	if r.OrderingMode == "" {
		r.OrderingMode = OrderingStrict
	}
	if r.OrderingMode != OrderingStrict && r.OrderingMode != OrderingIndependent {
		return DeliveryRequest{}, fmt.Errorf("%w: unknown ordering mode %q", ErrInvalidDelivery, r.OrderingMode)
	}
	if r.IdempotencyKey == "" {
		r.IdempotencyKey = deliveryKey(r)
	} else if strings.TrimSpace(r.IdempotencyKey) != r.IdempotencyKey {
		return DeliveryRequest{}, fmt.Errorf("%w: idempotency key may not be padded", ErrInvalidDelivery)
	}
	return r, nil
}

func deliveryKey(r DeliveryRequest) string {
	value := strings.Join([]string{
		"hcmnext.subscription.delivery/v1", r.SubscriptionID,
		fmt.Sprint(r.SubscriptionRevision), r.Destination, r.OrderingKey,
		string(r.OrderingMode), fmt.Sprint(r.Envelope.Sequence), r.Envelope.Digest(),
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return "del-" + hex.EncodeToString(sum[:])
}

// DeliveryAttempt is what the provider receives. Attempt numbers belong to
// one operation lineage and are never reset by a retry.
type DeliveryAttempt struct {
	Request      DeliveryRequest
	Attempt      uint32
	AttemptedAt  time.Time
	OperationKey string
}

// ProviderReceipt records what the destination accepted. A provider may mark
// Duplicate when it recognized the same idempotency key from an earlier send;
// that is still a successful, effectively-once delivery.
type ProviderReceipt struct {
	ReceiptID      string
	Provider       string
	IdempotencyKey string
	Destination    string
	Accepted       bool
	Duplicate      bool
	ReceivedAt     time.Time
}

// DeliveryProvider is the provider-neutral outbound port. Provider
// implementations must deduplicate DeliveryAttempt.OperationKey at their own
// boundary; retries always carry the same key.
type DeliveryProvider interface {
	Deliver(context.Context, DeliveryAttempt) (ProviderReceipt, error)
}

// DeliveryProviderFunc adapts a function to DeliveryProvider.
type DeliveryProviderFunc func(context.Context, DeliveryAttempt) (ProviderReceipt, error)

func (f DeliveryProviderFunc) Deliver(ctx context.Context, attempt DeliveryAttempt) (ProviderReceipt, error) {
	return f(ctx, attempt)
}

// AmbiguousProviderError lets a provider classify an error where the request
// may have been accepted before the connection failed. Such an outcome is
// retained as ambiguous; the caller cannot mistake it for a clean rejection.
type AmbiguousProviderError interface {
	error
	Ambiguous() bool
}

// DeliveryAttemptRecord is immutable evidence for one provider call.
type DeliveryAttemptRecord struct {
	Number     uint32
	StartedAt  time.Time
	FinishedAt time.Time
	Outcome    string
	Error      string
	Receipt    ProviderReceipt
}

const (
	AttemptQueued    = "QUEUED"
	AttemptAccepted  = "ACCEPTED"
	AttemptFailed    = "FAILED"
	AttemptAmbiguous = "AMBIGUOUS"
	OperationQueued  = "QUEUED"
	OperationSent    = "SENT"
	OperationFailed  = "FAILED"
	OperationAcked   = "ACKNOWLEDGED"
)

// DeliveryOperation is the per-subscription journal row. It contains only
// canonical references and delivery evidence, never an event payload.
type DeliveryOperation struct {
	OperationID     string
	Request         DeliveryRequest
	State           string
	Attempts        []DeliveryAttemptRecord
	ProviderReceipt ProviderReceipt
	LastError       string
	Ambiguous       bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// DeliveryResult reports whether the provider was called during this method.
// Duplicate is true when the journal already held an acknowledged receipt or
// the provider explicitly recognized the idempotency key.
type DeliveryResult struct {
	Operation DeliveryOperation
	Receipt   ProviderReceipt
	Duplicate bool
}

// DeliveryJournal is the kernel-pure operation journal used by tests and by
// adapters that supply durable storage around the same state machine. It is
// concurrency-safe and intentionally does not contact a database or network.
type DeliveryJournal struct {
	mu      sync.Mutex
	now     func() time.Time
	byKey   map[string]*DeliveryOperation
	byOrder map[string]map[uint64]string
	head    map[string]uint64
}

// NewDeliveryJournal returns an empty journal. The optional clock makes
// expiry and evidence deterministic in tests.
func NewDeliveryJournal(now ...func() time.Time) *DeliveryJournal {
	clock := func() time.Time { return time.Now().UTC() }
	if len(now) > 0 && now[0] != nil {
		clock = now[0]
	}
	return &DeliveryJournal{now: clock, byKey: make(map[string]*DeliveryOperation), byOrder: make(map[string]map[uint64]string), head: make(map[string]uint64)}
}

// NewJournal is the concise constructor used by delivery workers.
func NewJournal(now ...func() time.Time) *DeliveryJournal { return NewDeliveryJournal(now...) }

// Deliver records one provider attempt under one idempotency key. A gap is
// journaled as QUEUED and returns ErrDeliveryGap without calling the provider;
// once the predecessor is acknowledged, retrying the same request can send.
func (j *DeliveryJournal) Deliver(ctx context.Context, provider DeliveryProvider, request DeliveryRequest) (DeliveryResult, error) {
	if j == nil || provider == nil {
		return DeliveryResult{}, fmt.Errorf("%w: journal and provider are required", ErrInvalidDelivery)
	}
	request, err := request.validate()
	if err != nil {
		return DeliveryResult{}, err
	}
	now := j.now().UTC()
	orderID := request.SubscriptionID + "\x00" + request.OrderingKey

	j.mu.Lock()
	operation, exists := j.byKey[request.IdempotencyKey]
	if exists {
		if !sameDeliveryIdentity(operation.Request, request) {
			j.mu.Unlock()
			return DeliveryResult{}, fmt.Errorf("%w: idempotency key %s was reused", ErrDeliveryConflict, request.IdempotencyKey)
		}
		operation.Request.Signature = request.Signature
		if operation.State == OperationAcked {
			result := DeliveryResult{Operation: cloneDeliveryOperation(*operation), Receipt: operation.ProviderReceipt, Duplicate: true}
			j.mu.Unlock()
			return result, nil
		}
		if operation.State == OperationSent {
			j.mu.Unlock()
			return DeliveryResult{}, ErrDeliveryInFlight
		}
	} else {
		if prior, ok := j.byOrder[orderID][request.Envelope.Sequence]; ok && prior != request.IdempotencyKey {
			j.mu.Unlock()
			return DeliveryResult{}, fmt.Errorf("%w: sequence %d belongs to %s", ErrDeliveryReordered, request.Envelope.Sequence, prior)
		}
		operation = &DeliveryOperation{OperationID: request.IdempotencyKey, Request: cloneDeliveryRequest(request), State: OperationQueued, CreatedAt: now, UpdatedAt: now}
		j.byKey[request.IdempotencyKey] = operation
		if j.byOrder[orderID] == nil {
			j.byOrder[orderID] = make(map[uint64]string)
		}
		j.byOrder[orderID][request.Envelope.Sequence] = request.IdempotencyKey
	}

	if request.OrderingMode == OrderingStrict {
		expected := j.head[orderID] + 1
		if request.Envelope.Sequence > expected {
			operation.State = OperationQueued
			operation.UpdatedAt = now
			result := DeliveryResult{Operation: cloneDeliveryOperation(*operation)}
			j.mu.Unlock()
			return result, fmt.Errorf("%w: expected %d, received %d", ErrDeliveryGap, expected, request.Envelope.Sequence)
		}
		if request.Envelope.Sequence < expected {
			j.mu.Unlock()
			return DeliveryResult{}, fmt.Errorf("%w: expected %d, received %d", ErrDeliveryReordered, expected, request.Envelope.Sequence)
		}
	}

	attemptNumber := uint32(len(operation.Attempts) + 1)
	operation.State = OperationSent
	operation.UpdatedAt = now
	attempt := DeliveryAttempt{Request: cloneDeliveryRequest(request), Attempt: attemptNumber, AttemptedAt: now, OperationKey: request.IdempotencyKey}
	j.mu.Unlock()

	receipt, providerErr := provider.Deliver(ctx, attempt)
	finished := j.now().UTC()
	j.mu.Lock()
	defer j.mu.Unlock()
	current := j.byKey[request.IdempotencyKey]
	if current == nil {
		return DeliveryResult{}, ErrDeliveryNotFound
	}
	record := DeliveryAttemptRecord{Number: attemptNumber, StartedAt: now, FinishedAt: finished, Receipt: cloneReceipt(receipt)}
	if providerErr != nil {
		record.Outcome = AttemptFailed
		if deliveryErrorAmbiguous(providerErr) {
			current.Ambiguous = true
			record.Outcome = AttemptAmbiguous
		}
		record.Error = providerErr.Error()
		current.Attempts = append(current.Attempts, record)
		current.State = OperationFailed
		current.LastError = providerErr.Error()
		current.UpdatedAt = finished
		if current.Ambiguous {
			return DeliveryResult{Operation: cloneDeliveryOperation(*current)}, fmt.Errorf("%w: %v", ErrDeliveryAmbiguous, providerErr)
		}
		return DeliveryResult{Operation: cloneDeliveryOperation(*current)}, fmt.Errorf("%w: %v", ErrDeliveryProvider, providerErr)
	}
	if receipt.ReceiptID == "" {
		current.State = OperationFailed
		current.LastError = ErrDeliveryReceipt.Error() + ": receipt id is required"
		current.UpdatedAt = finished
		record.Outcome = AttemptFailed
		record.Error = current.LastError
		current.Attempts = append(current.Attempts, record)
		return DeliveryResult{Operation: cloneDeliveryOperation(*current)}, ErrDeliveryReceipt
	}
	if receipt.IdempotencyKey != "" && receipt.IdempotencyKey != request.IdempotencyKey {
		current.State = OperationFailed
		current.LastError = ErrDeliveryReceipt.Error() + ": idempotency key mismatch"
		current.UpdatedAt = finished
		record.Outcome = AttemptFailed
		record.Error = current.LastError
		current.Attempts = append(current.Attempts, record)
		return DeliveryResult{Operation: cloneDeliveryOperation(*current)}, ErrDeliveryReceipt
	}
	if receipt.Destination != "" && receipt.Destination != request.Destination {
		current.State = OperationFailed
		current.LastError = ErrDeliveryReceipt.Error() + ": destination mismatch"
		current.UpdatedAt = finished
		record.Outcome = AttemptFailed
		record.Error = current.LastError
		current.Attempts = append(current.Attempts, record)
		return DeliveryResult{Operation: cloneDeliveryOperation(*current)}, ErrDeliveryReceipt
	}
	if !receipt.Accepted {
		current.State = OperationFailed
		current.LastError = ErrDeliveryReceipt.Error() + ": provider did not accept the operation"
		current.UpdatedAt = finished
		record.Outcome = AttemptFailed
		record.Error = current.LastError
		current.Attempts = append(current.Attempts, record)
		return DeliveryResult{Operation: cloneDeliveryOperation(*current)}, ErrDeliveryReceipt
	}
	if receipt.IdempotencyKey == "" {
		receipt.IdempotencyKey = request.IdempotencyKey
	}
	if receipt.Destination == "" {
		receipt.Destination = request.Destination
	}
	if receipt.ReceivedAt.IsZero() {
		receipt.ReceivedAt = finished
	}
	record.Outcome = AttemptAccepted
	record.Receipt = cloneReceipt(receipt)
	current.Attempts = append(current.Attempts, record)
	current.ProviderReceipt = cloneReceipt(receipt)
	current.State = OperationAcked
	current.LastError = ""
	current.Ambiguous = false
	current.UpdatedAt = finished
	if request.OrderingMode == OrderingStrict && request.Envelope.Sequence > j.head[orderID] {
		j.head[orderID] = request.Envelope.Sequence
	}
	return DeliveryResult{Operation: cloneDeliveryOperation(*current), Receipt: cloneReceipt(receipt), Duplicate: receipt.Duplicate}, nil
}

func deliveryErrorAmbiguous(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ambiguous AmbiguousProviderError
	return errors.As(err, &ambiguous) && ambiguous.Ambiguous()
}

// DeliverNow is a context-free convenience for small adapters.
func (j *DeliveryJournal) DeliverNow(provider DeliveryProvider, request DeliveryRequest) (DeliveryResult, error) {
	return j.Deliver(context.Background(), provider, request)
}

// Operation returns a detached journal operation by idempotency key.
func (j *DeliveryJournal) Operation(idempotencyKey string) (DeliveryOperation, bool) {
	if j == nil {
		return DeliveryOperation{}, false
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	op, ok := j.byKey[idempotencyKey]
	if !ok {
		return DeliveryOperation{}, false
	}
	return cloneDeliveryOperation(*op), true
}

// Operations returns all operations for a subscription in declared sequence
// order. The returned values are detached from the journal.
func (j *DeliveryJournal) Operations(subscriptionID string) []DeliveryOperation {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]DeliveryOperation, 0)
	for _, op := range j.byKey {
		if op.Request.SubscriptionID == subscriptionID {
			out = append(out, cloneDeliveryOperation(*op))
		}
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].Request.Envelope.Sequence == out[k].Request.Envelope.Sequence {
			return out[i].OperationID < out[k].OperationID
		}
		return out[i].Request.Envelope.Sequence < out[k].Request.Envelope.Sequence
	})
	return out
}

// Explain returns a redaction-safe operation summary.
func (o DeliveryOperation) Explain() string {
	return fmt.Sprintf("subscription delivery operation=%s state=%s sequence=%d attempts=%d receipt=%s ambiguous=%t", o.OperationID, o.State, o.Request.Envelope.Sequence, len(o.Attempts), o.ProviderReceipt.ReceiptID, o.Ambiguous)
}

func sameDeliveryIdentity(a, b DeliveryRequest) bool {
	return a.SubscriptionID == b.SubscriptionID && a.SubscriptionRevision == b.SubscriptionRevision && a.Destination == b.Destination && a.OrderingKey == b.OrderingKey && a.OrderingMode == b.OrderingMode && a.Envelope.Sequence == b.Envelope.Sequence && a.Envelope.Digest() == b.Envelope.Digest()
}

func cloneDeliveryRequest(r DeliveryRequest) DeliveryRequest {
	r.Envelope = cloneEnvelope(r.Envelope)
	return r
}

func cloneEnvelope(e CanonicalEnvelope) CanonicalEnvelope {
	e.SubjectRefs = append([]string(nil), e.SubjectRefs...)
	return e
}

func cloneReceipt(r ProviderReceipt) ProviderReceipt { return r }

func cloneDeliveryOperation(o DeliveryOperation) DeliveryOperation {
	o.Request = cloneDeliveryRequest(o.Request)
	o.Attempts = append([]DeliveryAttemptRecord(nil), o.Attempts...)
	for i := range o.Attempts {
		o.Attempts[i].Receipt = cloneReceipt(o.Attempts[i].Receipt)
	}
	o.ProviderReceipt = cloneReceipt(o.ProviderReceipt)
	return o
}
