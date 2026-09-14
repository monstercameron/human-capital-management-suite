package pilotcommercial

import "testing"

// TestReplayAndRepairEventsAreNeverBilled is RED's "bills replay or repair
// duplicates" clause made concrete: a fixture of four events for two
// distinct underlying transactions - a NEW transaction, a REPLAY of it (a
// client retry redelivering the same transaction), a REPAIR of it (a
// correction issued against it), and a second, genuinely distinct NEW
// transaction - must bill exactly the two distinct transactions and no
// more, regardless of the replay and repair being present in the event
// stream at all.
func TestReplayAndRepairEventsAreNeverBilled(t *testing.T) {
	events := []BillableEvent{
		{EventID: "evt-1", IdempotencyKey: "txn-A", Kind: EventNew, AmountCents: 1000},
		{EventID: "evt-2", IdempotencyKey: "txn-A", Kind: EventReplay, AmountCents: 1000},
		{EventID: "evt-3", IdempotencyKey: "txn-A", Kind: EventRepair, AmountCents: 1000},
		{EventID: "evt-4", IdempotencyKey: "txn-B", Kind: EventNew, AmountCents: 2500},
		// txn-C and txn-D are never billed as NEW anywhere in this stream: a
		// REPLAY or REPAIR must be skipped by its own Kind, not merely
		// because ComputeBillableTotal happens to have already seen the
		// same idempotency key from a prior NEW event.
		{EventID: "evt-5", IdempotencyKey: "txn-C", Kind: EventReplay, AmountCents: 5000},
		{EventID: "evt-6", IdempotencyKey: "txn-D", Kind: EventRepair, AmountCents: 7500},
	}

	total, billed := ComputeBillableTotal(events)

	if want := int64(1000 + 2500); total != want {
		t.Fatalf("total billed = %d, want %d (replay and repair must never add revenue, whether or not their idempotency key was already seen)", total, want)
	}
	if len(billed) != 2 || billed[0] != "evt-1" || billed[1] != "evt-4" {
		t.Fatalf("billed events = %v, want exactly [evt-1 evt-4]", billed)
	}
}

// TestDuplicateNewSubmissionForAnAlreadyBilledTransactionIsNotBilledAgain
// covers the adjacent case: even an event tagged NEW (not REPLAY or REPAIR)
// is not billed twice if it shares an IdempotencyKey with a transaction
// already billed - a client that resubmits under the same key by mistake,
// still labeled NEW, must not double-charge either.
func TestDuplicateNewSubmissionForAnAlreadyBilledTransactionIsNotBilledAgain(t *testing.T) {
	events := []BillableEvent{
		{EventID: "evt-1", IdempotencyKey: "txn-A", Kind: EventNew, AmountCents: 1000},
		{EventID: "evt-2", IdempotencyKey: "txn-A", Kind: EventNew, AmountCents: 1000},
	}

	total, billed := ComputeBillableTotal(events)

	if total != 1000 {
		t.Fatalf("total billed = %d, want 1000 (a duplicate NEW submission for the same transaction must not double-bill)", total)
	}
	if len(billed) != 1 || billed[0] != "evt-1" {
		t.Fatalf("billed events = %v, want exactly [evt-1]", billed)
	}
}

// TestBillingPolicyOnTheCheckedInFreezeMatchesEnforcedBehavior proves the
// declared half (BillingPolicy) and the enforced half (ComputeBillableTotal)
// agree: the checked-in freeze's billing policy claims replay/repair are
// never billable, and ComputeBillableTotal's behavior on a real fixture
// bears that out.
func TestBillingPolicyOnTheCheckedInFreezeMatchesEnforcedBehavior(t *testing.T) {
	freeze := mustLoadFreeze(t)
	if freeze.Billing.ReplayBillable {
		t.Fatal("checked-in freeze's billing.replay_billable is true, contradicting ComputeBillableTotal's enforcement")
	}
	if freeze.Billing.RepairBillable {
		t.Fatal("checked-in freeze's billing.repair_billable is true, contradicting ComputeBillableTotal's enforcement")
	}

	total, _ := ComputeBillableTotal([]BillableEvent{
		{EventID: "evt-1", IdempotencyKey: "txn-A", Kind: EventReplay, AmountCents: 999999},
		{EventID: "evt-2", IdempotencyKey: "txn-B", Kind: EventRepair, AmountCents: 999999},
	})
	if total != 0 {
		t.Fatalf("a stream containing only replay/repair events must bill zero, got %d", total)
	}
}
