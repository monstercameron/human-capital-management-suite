package signal

import (
	"bytes"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Status is the typed result Accept produces for one signal against one
// subscription.
type Status string

// The declared acceptance statuses.
const (
	// StatusAccepted says the signal matched the subscription in every
	// dimension and its continuation is scheduled.
	StatusAccepted Status = "ACCEPTED"
	// StatusDuplicateSameBytes says a signal with this IdempotencyKey was
	// already accepted with byte-identical payload. It is recorded but does
	// not repeat the continuation (exactly-once).
	StatusDuplicateSameBytes Status = "DUPLICATE_SAME_BYTES"

	// StatusRefusedWrongTenant says the signal's tenant does not match the
	// subscription's.
	StatusRefusedWrongTenant Status = "REFUSED_WRONG_TENANT"
	// StatusRefusedUnmatched says the signal does not even address this
	// subscription's event type or correlation dimension.
	StatusRefusedUnmatched Status = "REFUSED_UNMATCHED"
	// StatusRefusedWrongCorrelation says the signal names the right
	// correlation key but the wrong value.
	StatusRefusedWrongCorrelation Status = "REFUSED_WRONG_CORRELATION"
	// StatusRefusedWrongSchema says the signal's schema ref does not match
	// what the subscription expects.
	StatusRefusedWrongSchema Status = "REFUSED_WRONG_SCHEMA"
	// StatusRefusedInvalidSignature says the Verifier rejected the signal's
	// signature.
	StatusRefusedInvalidSignature Status = "REFUSED_INVALID_SIGNATURE"
	// StatusRefusedWrongSource says the signal's source is not in the
	// subscription's accepted-sources allowlist.
	StatusRefusedWrongSource Status = "REFUSED_WRONG_SOURCE"
	// StatusRefusedWrongOrder says the signal violates the subscription's
	// declared ordering expectation.
	StatusRefusedWrongOrder Status = "REFUSED_WRONG_ORDER"
	// StatusRefusedDuplicateDifferentBytes says a signal with this
	// IdempotencyKey was already accepted with different payload bytes. This
	// is the security-incident case: the same scoped identity cannot mean two
	// different things.
	StatusRefusedDuplicateDifferentBytes Status = "REFUSED_DUPLICATE_DIFFERENT_BYTES"
	// StatusRefusedLate says the signal arrived after the subscription
	// closed.
	StatusRefusedLate Status = "REFUSED_LATE"
)

// Accepted reports whether s represents a signal that was (or already had
// been) accepted — the two statuses that carry a schedulable/scheduled
// continuation rather than a refusal.
func (s Status) Accepted() bool {
	return s == StatusAccepted || s == StatusDuplicateSameBytes
}

// Result is what Accept returns for one evaluation: the typed status, whether
// this specific call causes a new continuation to be scheduled (true only for
// a first ACCEPTED, never for a replayed duplicate or a refusal), and a
// human-readable reason for a refusal.
type Result struct {
	Status       Status
	Continuation bool
	Reason       string
}

// matchedPrior finds, among prior (every previously recorded LogEntry for
// this subscription), an accepted entry sharing sig's IdempotencyKey. Refusals
// are audit evidence only: they cannot claim an identity or prove a prior
// continuation. It returns
// ok=false when sig.IdempotencyKey is empty, since an empty key carries no
// idempotency identity to deduplicate against.
func matchedPrior(prior []LogEntry, sig Signal) (LogEntry, bool) {
	if sig.IdempotencyKey == "" {
		return LogEntry{}, false
	}
	for _, e := range prior {
		if e.Status.Accepted() && e.Signal.IdempotencyKey == sig.IdempotencyKey {
			return e, true
		}
	}
	return LogEntry{}, false
}

// maxAcceptedSequence returns the highest SequenceNumber among prior entries
// whose status was accepted (ACCEPTED or DUPLICATE_SAME_BYTES), and whether
// any such entry exists.
func maxAcceptedSequence(prior []LogEntry) (uint64, bool) {
	var max uint64
	found := false
	for _, e := range prior {
		if !e.Status.Accepted() {
			continue
		}
		if !found || e.Signal.SequenceNumber > max {
			max = e.Signal.SequenceNumber
			found = true
		}
	}
	return max, found
}

// Accept is WF-STEP-006's pure decision function. Given a subscription, one
// inbound signal, every prior LogEntry already recorded against that
// subscription, a signature Verifier, and the caller-supplied instant "now",
// it returns exactly one typed Status.
//
// Checks run in this fixed order, so a signal wrong in more than one
// dimension always reports the same status: tenant, event-type/correlation-
// key match (unmatched), correlation value, schema, source, signature,
// idempotency (duplicate-same-bytes short-circuits everything below it;
// duplicate-different-bytes is refused outright), ordering, then lateness.
// Only a signal that clears every check is accepted.
func Accept(sub SignalSubscription, sig Signal, prior []LogEntry, verify Verifier, now values.Instant) (Result, error) {
	if err := sub.Validate(); err != nil {
		return Result{}, err
	}
	if verify == nil {
		return Result{}, ErrVerifierRequired
	}
	if !now.IsSet() {
		return Result{}, ErrNowRequired
	}
	for i, e := range prior {
		if e.SubscriptionDigest != sub.Digest() {
			return Result{}, fmt.Errorf("%w: entry %d", ErrPriorEntryWrongSubscription, i)
		}
	}

	switch {
	case sig.Tenant != sub.Tenant:
		return refuse(StatusRefusedWrongTenant, "signal tenant %q does not match subscription tenant %q", sig.Tenant, sub.Tenant), nil
	case sig.EventType != sub.EventType || sig.CorrelationKey != sub.CorrelationKey:
		return refuse(StatusRefusedUnmatched, "signal event_type/correlation_key %q/%q does not address this subscription's %q/%q",
			sig.EventType, sig.CorrelationKey, sub.EventType, sub.CorrelationKey), nil
	case sig.CorrelationValue != sub.CorrelationValue:
		return refuse(StatusRefusedWrongCorrelation, "signal correlation value %q does not match subscription value %q", sig.CorrelationValue, sub.CorrelationValue), nil
	case sig.SchemaRef != sub.ExpectedSchemaRef:
		return refuse(StatusRefusedWrongSchema, "signal schema %q does not match expected schema %q", sig.SchemaRef, sub.ExpectedSchemaRef), nil
	case !sub.acceptsSource(sig.Source):
		return refuse(StatusRefusedWrongSource, "source %q is not in the subscription's accepted-sources allowlist", sig.Source), nil
	}

	if err := verify.Verify(sig); err != nil {
		return refuse(StatusRefusedInvalidSignature, "signature verification failed: %v", err), nil
	}

	if match, found := matchedPrior(prior, sig); found {
		if bytes.Equal(match.Signal.copyPayload(), sig.copyPayload()) {
			return Result{Status: StatusDuplicateSameBytes, Continuation: false,
				Reason: "idempotency key already accepted with identical payload"}, nil
		}
		return refuse(StatusRefusedDuplicateDifferentBytes,
			"idempotency key %q was already used with different payload bytes: this is a security incident, not a retry", sig.IdempotencyKey), nil
	}

	if sub.Ordering == OrderingMonotonicSequence {
		if maxSeq, found := maxAcceptedSequence(prior); found && sig.SequenceNumber <= maxSeq {
			return refuse(StatusRefusedWrongOrder, "sequence %d does not exceed the last accepted sequence %d", sig.SequenceNumber, maxSeq), nil
		}
	}

	if sub.ClosesAt.IsSet() && !now.Before(sub.ClosesAt) {
		return refuse(StatusRefusedLate, "signal arrived at/after the subscription's close time %s", sub.ClosesAt), nil
	}

	return Result{Status: StatusAccepted, Continuation: true}, nil
}

func refuse(status Status, format string, args ...any) Result {
	return Result{Status: status, Continuation: false, Reason: fmt.Sprintf(format, args...)}
}
