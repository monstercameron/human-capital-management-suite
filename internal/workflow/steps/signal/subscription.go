package signal

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// OrderingExpectation names how a subscription expects successive signals on
// the same correlation to be numbered.
type OrderingExpectation string

// The declared ordering expectations.
const (
	// OrderingNone imposes no sequence requirement.
	OrderingNone OrderingExpectation = "NONE"
	// OrderingMonotonicSequence requires each accepted signal's
	// SequenceNumber to be strictly greater than every previously accepted
	// signal's on the same subscription.
	OrderingMonotonicSequence OrderingExpectation = "MONOTONIC_SEQUENCE"
)

// Valid reports whether o names a declared ordering expectation.
func (o OrderingExpectation) Valid() bool {
	switch o {
	case OrderingNone, OrderingMonotonicSequence:
		return true
	default:
		return false
	}
}

// SignalSubscription is the durable, typed record WF-STEP-006 registers in
// place of a poller: what tenant/instance/node is waiting, what it is
// waiting for (event type + correlation key/value + schema), who may supply
// it, what ordering it expects, and when it stops listening.
type SignalSubscription struct {
	Tenant             values.TenantId
	WorkflowInstanceID string
	NodeID             string

	EventType        string
	CorrelationKey   string
	CorrelationValue string

	ExpectedSchemaRef string
	AcceptedSources   []string
	Ordering          OrderingExpectation

	// ClosesAt, when set, is the instant after which a signal is late. Unset
	// means the subscription never closes on its own (only Accept/cancel
	// closes it).
	ClosesAt values.Instant

	// DeadlineDefault, when its schema is set, is the declared
	// non-response payload a late signal completes with on TIMED_OUT.
	DeadlineDefault values.DeadlineDefault
}

// Validate reports whether the subscription is complete enough to evaluate a
// signal against.
func (s SignalSubscription) Validate() error {
	if s.Tenant == "" || s.WorkflowInstanceID == "" || s.NodeID == "" {
		return ErrSubscriptionIdentityRequired
	}
	if err := s.Tenant.Validate(); err != nil {
		return err
	}
	if s.EventType == "" || s.CorrelationKey == "" {
		return ErrSubscriptionCorrelationRequired
	}
	if s.ExpectedSchemaRef == "" {
		return ErrSubscriptionSchemaRequired
	}
	if len(s.AcceptedSources) == 0 {
		return ErrSubscriptionSourcesRequired
	}
	if !s.Ordering.Valid() {
		return ErrSubscriptionOrderingInvalid
	}
	if s.DeadlineDefault.Schema != "" {
		if err := s.DeadlineDefault.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// sortedSources returns AcceptedSources sorted, for canonical digesting and
// deterministic membership scanning.
func (s SignalSubscription) sortedSources() []string {
	out := append([]string(nil), s.AcceptedSources...)
	sort.Strings(out)
	return out
}

func (s SignalSubscription) acceptsSource(source string) bool {
	for _, a := range s.AcceptedSources {
		if a == source {
			return true
		}
	}
	return false
}

// Digest is the subscription's content identity.
func (s SignalSubscription) Digest() string { return computeSubscriptionDigest(s) }

// ClosesAtText returns the canonical text of ClosesAt, or "" when unset.
func (s SignalSubscription) ClosesAtText() string { return instantText(s.ClosesAt) }

func instantText(i values.Instant) string {
	if !i.IsSet() {
		return ""
	}
	return i.String()
}
