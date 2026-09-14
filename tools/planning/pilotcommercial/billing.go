package pilotcommercial

// BillableEventKind is the closed vocabulary ComputeBillableTotal
// classifies. It is deliberately narrower than a real billing engine: this
// package only needs to prove RED's "bills replay or repair duplicates"
// clause is enforced, not implement transaction billing.
type BillableEventKind string

const (
	// EventNew is a genuinely new billable transaction.
	EventNew BillableEventKind = "NEW"
	// EventReplay is a retried or resubmitted attempt at a transaction this
	// package has already seen under the same IdempotencyKey (e.g. a client
	// retry after a timeout, or an at-least-once delivery redelivering the
	// same message).
	EventReplay BillableEventKind = "REPLAY"
	// EventRepair is a correction issued against a transaction this package
	// has already seen (e.g. internal/domains/repair's repair-plan flow
	// correcting a prior drifted or duplicated write).
	EventRepair BillableEventKind = "REPAIR"
)

// BillableEvent is one candidate billing event a pilot transaction produces.
// IdempotencyKey identifies the underlying business transaction a replay or
// repair refers back to; Kind classifies why the event exists.
type BillableEvent struct {
	EventID        string
	IdempotencyKey string
	Kind           BillableEventKind
	AmountCents    int64
}

// ComputeBillableTotal is the enforced half of RED's "bills replay or repair
// duplicates" clause (BillingPolicy is the declared half). It bills a
// transaction at most once: a REPLAY or REPAIR event is never billed
// regardless of its IdempotencyKey, and a duplicate NEW submission for an
// IdempotencyKey already billed is not billed again either, so a client
// retry that happens to arrive tagged NEW cannot double-charge. Events are
// processed in order; billed lists the EventIDs that were actually charged,
// in the order they were charged, so a caller can reconcile exactly which
// event produced revenue.
func ComputeBillableTotal(events []BillableEvent) (totalCents int64, billed []string) {
	seen := make(map[string]bool, len(events))
	for _, e := range events {
		if e.Kind != EventNew {
			continue
		}
		if seen[e.IdempotencyKey] {
			continue
		}
		seen[e.IdempotencyKey] = true
		totalCents += e.AmountCents
		billed = append(billed, e.EventID)
	}
	return totalCents, billed
}
