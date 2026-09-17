package subscription

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrInvalidCompleteness identifies a completeness expectation refused at
// the planning boundary. It wraps no provider state: a bad expectation is
// a caller defect, never a delivery verdict.
var ErrInvalidCompleteness = errors.New("subscription: invalid delivery completeness expectation")

// CompletenessExpectation declares the event range one subscriber must hold
// for one ordering key. FromSequence and ThroughSequence are inclusive and
// 1-based; an empty or inverted range is refused rather than reported
// complete.
type CompletenessExpectation struct {
	SubscriptionID  string
	Tenant          string
	OrderingKey     string
	FromSequence    uint64
	ThroughSequence uint64
}

func (e CompletenessExpectation) validate() (CompletenessExpectation, error) {
	for name, value := range map[string]string{"subscription_id": e.SubscriptionID, "tenant": e.Tenant, "ordering_key": e.OrderingKey} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return CompletenessExpectation{}, fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidCompleteness, name)
		}
	}
	if e.FromSequence == 0 || e.ThroughSequence < e.FromSequence {
		return CompletenessExpectation{}, fmt.Errorf("%w: range [%d, %d] is empty", ErrInvalidCompleteness, e.FromSequence, e.ThroughSequence)
	}
	return e, nil
}

// CompletenessReport classifies every expected sequence against journaled
// delivery evidence. Acked sequences carry an acknowledged receipt. Gaps were
// never attempted. Unacked sequences were attempted or queued but hold no
// acknowledgement. Unknown sequences hold an ambiguous provider outcome and
// need observation first: they are never blind-retried, so they never appear
// in NeedsRepair. Provider acceptance of a later sequence never heals an
// earlier gap.
type CompletenessReport struct {
	SubscriptionID  string
	Tenant          string
	OrderingKey     string
	FromSequence    uint64
	ThroughSequence uint64
	Acked           []uint64
	Gaps            []uint64
	Unacked         []uint64
	Unknown         []uint64
}

// Complete reports whether every expected sequence holds an acknowledged
// receipt. An empty expectation cannot be complete; validate refuses one.
func (r CompletenessReport) Complete() bool {
	return len(r.Acked) > 0 && len(r.Gaps) == 0 && len(r.Unacked) == 0 && len(r.Unknown) == 0 &&
		uint64(len(r.Acked)) == r.ThroughSequence-r.FromSequence+1
}

// NeedsRepair returns the sequences a worker may redrive: gaps and unacked
// deliveries, in ascending order. Ambiguous sequences are excluded; they
// need observation, not another attempt.
func (r CompletenessReport) NeedsRepair() []uint64 {
	out := append(append([]uint64(nil), r.Gaps...), r.Unacked...)
	sort.Slice(out, func(i, k int) bool { return out[i] < out[k] })
	return out
}

// Explain returns a redaction-safe completeness summary. It names sequences
// and states only, never payloads or destinations.
func (r CompletenessReport) Explain() string {
	return fmt.Sprintf("subscription completeness subscription=%s range=[%d,%d] acked=%d gaps=%v unacked=%v unknown=%v complete=%t",
		r.SubscriptionID, r.FromSequence, r.ThroughSequence, len(r.Acked), r.Gaps, r.Unacked, r.Unknown, r.Complete())
}

// ReconcileDelivery compares one expected event range against journaled
// delivery evidence. It is pure: it neither calls a provider nor mutates the
// journal. An operation recorded under another tenant for the same
// subscription, ordering key and sequence fails closed instead of counting
// toward completeness.
func ReconcileDelivery(journal *DeliveryJournal, expectation CompletenessExpectation) (CompletenessReport, error) {
	if journal == nil {
		return CompletenessReport{}, fmt.Errorf("%w: journal is required", ErrInvalidCompleteness)
	}
	effective, err := expectation.validate()
	if err != nil {
		return CompletenessReport{}, err
	}
	bySequence := make(map[uint64]DeliveryOperation, len(journal.Operations(effective.SubscriptionID)))
	for _, operation := range journal.Operations(effective.SubscriptionID) {
		if operation.Request.OrderingKey != effective.OrderingKey {
			continue
		}
		if operation.Request.Envelope.Tenant != effective.Tenant {
			return CompletenessReport{}, fmt.Errorf("%w: sequence %d recorded under another tenant", ErrInvalidCompleteness, operation.Request.Envelope.Sequence)
		}
		sequence := operation.Request.Envelope.Sequence
		if _, seen := bySequence[sequence]; !seen {
			bySequence[sequence] = operation
		}
	}
	report := CompletenessReport{SubscriptionID: effective.SubscriptionID, Tenant: effective.Tenant, OrderingKey: effective.OrderingKey,
		FromSequence: effective.FromSequence, ThroughSequence: effective.ThroughSequence}
	for sequence := effective.FromSequence; sequence <= effective.ThroughSequence; sequence++ {
		operation, ok := bySequence[sequence]
		if !ok {
			report.Gaps = append(report.Gaps, sequence)
			continue
		}
		switch {
		case operation.State == OperationAcked:
			report.Acked = append(report.Acked, sequence)
		case operation.Ambiguous:
			report.Unknown = append(report.Unknown, sequence)
		default:
			report.Unacked = append(report.Unacked, sequence)
		}
	}
	return report, nil
}
