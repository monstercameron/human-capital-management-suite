package settlement

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	// ErrSubmitRejected identifies a submission that cannot be journaled.
	ErrSubmitRejected            = errors.New("settlement: submission rejected")
	ErrManualFulfillmentRequired = errors.New("settlement: rail requires governed non-provider fulfillment")
)

// ProviderOutcome is the provider-side verdict observed for one send.
type ProviderOutcome string

const (
	ProviderAccepted ProviderOutcome = "ACCEPTED"
	ProviderRejected ProviderOutcome = "REJECTED"
	ProviderTimeout  ProviderOutcome = "TIMEOUT"
)

// JournalOutcome is the journaled state of one idempotency key.
type JournalOutcome string

const (
	OutcomePending   JournalOutcome = "PENDING"
	OutcomeAccepted  JournalOutcome = "ACCEPTED"
	OutcomeRejected  JournalOutcome = "REJECTED"
	OutcomeAmbiguous JournalOutcome = "AMBIGUOUS"
)

// ProviderResponse is the provider adapter answer for one send. Timeout
// means the provider may or may not have executed: the journal keeps the
// attempt AMBIGUOUS until an explicit reconciliation confirms it.
type ProviderResponse struct {
	Outcome     ProviderOutcome
	ProviderRef string
}

// Provider is the settlement edge: one send per call, journaled first.
type Provider interface {
	Send(ctx context.Context, instruction PaymentInstruction) (ProviderResponse, error)
}

// CredentialLease binds one submission to a live credential. The lease is
// single-instruction: replaying it against another instruction is refused.
type CredentialLease struct {
	LeaseRef       string
	InstructionRef string
	Scope          string
	IssuedAt       time.Time
	ExpiresAt      time.Time
}

func (l CredentialLease) Validate(instruction PaymentInstruction, now time.Time) error {
	if strings.TrimSpace(l.LeaseRef) == "" || strings.TrimSpace(l.Scope) == "" {
		return fmt.Errorf("%w: lease needs a reference and a scope", ErrSubmitRejected)
	}
	if l.InstructionRef != instruction.InstructionID {
		return fmt.Errorf("%w: lease is bound to another instruction", ErrSubmitRejected)
	}
	if now.IsZero() || l.ExpiresAt.IsZero() || !now.Before(l.ExpiresAt) {
		return fmt.Errorf("%w: credential lease is expired", ErrSubmitRejected)
	}
	return nil
}

// SubmitRequest is one idempotent submission. An empty IdempotencyKey
// defaults to the instruction natural key.
type SubmitRequest struct {
	Instruction    PaymentInstruction
	IdempotencyKey string
	Lease          CredentialLease
	Now            time.Time
}

// ReconcileRequest confirms a previously ambiguous attempt after an
// out-of-band provider reconciliation.
type ReconcileRequest struct {
	IdempotencyKey string
	Outcome        JournalOutcome
	ProviderRef    string
	Now            time.Time
}

// JournalEntry is the durable submission fact: identity, credential lease,
// attempt count and provider response, journaled before observation.
type JournalEntry struct {
	IdempotencyKey    string
	InstructionID     string
	InstructionDigest string
	LeaseRef          string
	Attempt           int
	Outcome           JournalOutcome
	ProviderRef       string
	ObservedAt        time.Time
}

// keyState is one journaled key plus the completion signal waiters use
// while a send is in flight.
type keyState struct {
	entry JournalEntry
	done  chan struct{}
}

// Journal is the SETTLE-004 idempotent submission log. One key maps to one
// provider send: retries with the same key return the journaled outcome and
// never resend, so timeout-after-send cannot blind-duplicate. Concurrent
// submitters for an in-flight key wait for the single send to complete.
type Journal struct {
	mu      sync.Mutex
	entries map[string]*keyState
}

// NewJournal builds an empty submission journal.
func NewJournal() *Journal { return &Journal{entries: map[string]*keyState{}} }

// Lookup serves the journaled entry for one key.
func (j *Journal) Lookup(key string) (JournalEntry, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, ok := j.entries[key]
	if !ok {
		return JournalEntry{}, false
	}
	return state.entry, true
}

// Submit journals the attempt, sends once through the provider, then journals
// the observed response. Terminal outcomes replay from the journal without
// another send; ambiguous outcomes replay as AMBIGUOUS until reconciled.
func (j *Journal) Submit(ctx context.Context, req SubmitRequest, provider Provider) (JournalEntry, error) {
	if err := req.Instruction.Validate(); err != nil {
		return JournalEntry{}, fmt.Errorf("%w: %v", ErrSubmitRejected, err)
	}
	if req.Instruction.FulfillmentAction() != ActionSubmitPayment {
		return JournalEntry{}, fmt.Errorf("%w: %s", ErrManualFulfillmentRequired, req.Instruction.FulfillmentAction())
	}
	if req.Now.IsZero() {
		return JournalEntry{}, fmt.Errorf("%w: reference time is required", ErrSubmitRejected)
	}
	if err := req.Lease.Validate(req.Instruction, req.Now); err != nil {
		return JournalEntry{}, err
	}
	key := req.IdempotencyKey
	if key == "" {
		key = req.Instruction.IdempotencyKey()
	}
	if provider == nil {
		return JournalEntry{}, fmt.Errorf("%w: provider adapter is required", ErrSubmitRejected)
	}
	j.mu.Lock()
	if existing, ok := j.entries[key]; ok {
		if existing.entry.InstructionDigest != req.Instruction.CanonicalDigest {
			j.mu.Unlock()
			return JournalEntry{}, fmt.Errorf("%w: key %q already journals another instruction", ErrIdempotencyConflict, key)
		}
		if existing.entry.Outcome != OutcomePending {
			replay := existing.entry
			j.mu.Unlock()
			return replay, nil
		}
		wait := existing.done
		j.mu.Unlock()
		select {
		case <-ctx.Done():
			return JournalEntry{}, fmt.Errorf("%w: waiting for the in-flight send: %v", ErrSubmitRejected, ctx.Err())
		case <-wait:
		}
		j.mu.Lock()
		settled := j.entries[key].entry
		j.mu.Unlock()
		return settled, nil
	}
	state := &keyState{
		entry: JournalEntry{
			IdempotencyKey: key, InstructionID: req.Instruction.InstructionID,
			InstructionDigest: req.Instruction.CanonicalDigest, LeaseRef: req.Lease.LeaseRef,
			Attempt: 1, Outcome: OutcomePending, ObservedAt: req.Now.UTC(),
		},
		done: make(chan struct{}),
	}
	j.entries[key] = state
	j.mu.Unlock()

	response, err := provider.Send(ctx, req.Instruction)
	j.mu.Lock()
	defer j.mu.Unlock()
	if err != nil {
		state.entry.Outcome = OutcomeAmbiguous
		state.entry.ObservedAt = req.Now.UTC()
		close(state.done)
		return state.entry, nil
	}
	switch response.Outcome {
	case ProviderAccepted:
		state.entry.Outcome = OutcomeAccepted
	case ProviderRejected:
		state.entry.Outcome = OutcomeRejected
	default:
		state.entry.Outcome = OutcomeAmbiguous
	}
	state.entry.ProviderRef = response.ProviderRef
	state.entry.ObservedAt = req.Now.UTC()
	close(state.done)
	return state.entry, nil
}

// Reconcile confirms an ambiguous attempt after out-of-band provider
// confirmation. Settled keys never change outcome through reconciliation.
func (j *Journal) Reconcile(req ReconcileRequest) (JournalEntry, error) {
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.Now.IsZero() {
		return JournalEntry{}, fmt.Errorf("%w: key and reference time are required", ErrSubmitRejected)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, ok := j.entries[req.IdempotencyKey]
	if !ok {
		return JournalEntry{}, fmt.Errorf("%w: unknown idempotency key", ErrSubmitRejected)
	}
	if state.entry.Outcome != OutcomeAmbiguous {
		return JournalEntry{}, fmt.Errorf("%w: only ambiguous attempts reconcile", ErrSubmitRejected)
	}
	switch req.Outcome {
	case OutcomeAccepted, OutcomeRejected:
		state.entry.Outcome = req.Outcome
	default:
		return JournalEntry{}, fmt.Errorf("%w: reconciliation needs a terminal outcome", ErrSubmitRejected)
	}
	state.entry.ProviderRef = req.ProviderRef
	state.entry.ObservedAt = req.Now.UTC()
	return state.entry, nil
}
