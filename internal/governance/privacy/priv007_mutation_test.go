package privacy

import (
	"errors"
	"testing"
)

// TestTodo_PRIV_007_Mutation is the MUTATION matrix test for PRIV-007. Each
// case moves one semantic input across a decision boundary and proves the
// boundary moves with it: a seeded mutant that weakens any of these checks
// must fail this test.
func TestTodo_PRIV_007_Mutation(t *testing.T) {
	t.Run("silence one second before the due instant retries; at the due instant it escalates", func(t *testing.T) {
		early, err := ReconcileFulfillment(
			fixtureFulfillmentRequest(t), nil, mustInstantAt(t, fxProcDue1-1))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		if early.Resolutions[0].Status != ItemPendingRetry {
			t.Errorf("status at due-1 = %q, want %q", early.Resolutions[0].Status, ItemPendingRetry)
		}
		atDue, err := ReconcileFulfillment(
			fixtureFulfillmentRequest(t), nil, mustInstantAt(t, fxProcDue1))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		if atDue.Resolutions[0].Status != ItemEscalated {
			t.Errorf("status at due = %q, want %q", atDue.Resolutions[0].Status, ItemEscalated)
		}
	})

	t.Run("flipping one ack from fulfilled to refused flips that item from resolved to exception", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[2] = ProcessorAck{
			ItemID: "copy-archive-c", Processor: "processor-backup",
			Outcome: AckRefused, At: mustInstantAt(t, fxProcAckAt),
			Detail: "copy under audit hold",
		}
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		if rec.Resolutions[2].Status != ItemException {
			t.Errorf("flipped item status = %q, want %q", rec.Resolutions[2].Status, ItemException)
		}
		for i, r := range rec.Resolutions[:2] {
			if r.Status != ItemResolved {
				t.Errorf("untouched item %d status = %q, want %q", i, r.Status, ItemResolved)
			}
		}
	})

	t.Run("all but one resolved still blocks Complete; all resolved certifies", func(t *testing.T) {
		partial := fixtureFulfilledAcks(t)[:2] // third copy silent
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), partial, mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		if _, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrFulfillmentIncomplete) {
			t.Fatalf("CertifyFulfillment(2 of 3) = %v, want %v", err, ErrFulfillmentIncomplete)
		}
		full, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), fixtureFulfilledAcks(t), mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		cert, err := CertifyFulfillment(full, "privacy-officer-1", mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("CertifyFulfillment(3 of 3): %v", err)
		}
		if !cert.Complete {
			t.Error("3-of-3 resolved certifies Complete = false")
		}
	})

	t.Run("a 63-hex-char receipt is refused; the 64-hex-char receipt resolves", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[0].ReceiptDigest = fxProcReceiptA[:63]
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(63-char receipt) = %v, want %v", err, ErrProcessorCertBlocked)
		}
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), fixtureFulfilledAcks(t), mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment(64-char receipt): %v", err)
		}
		if rec.Resolutions[0].Status != ItemResolved {
			t.Errorf("status with valid receipt = %q, want %q", rec.Resolutions[0].Status, ItemResolved)
		}
	})

	t.Run("a non-hex receipt byte is refused even at full length", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		bad := []byte(fxProcReceiptB)
		bad[0] = 'g' // outside [0-9a-f], length unchanged
		acks[1].ReceiptDigest = string(bad)
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(non-hex receipt) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("an exception preserved in the package keeps its reason word for word", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[1] = ProcessorAck{
			ItemID: "copy-analytics-b", Processor: "processor-analytics",
			Outcome: AckUnknownCopy, At: mustInstantAt(t, fxProcAckAt),
			Detail: "no such shard at this processor",
		}
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		cert, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("CertifyFulfillment: %v", err)
		}
		if len(cert.Exceptions) != 1 {
			t.Fatalf("exceptions = %d, want 1", len(cert.Exceptions))
		}
		ex := cert.Exceptions[0]
		if ex.Detail != "no such shard at this processor" || ex.Status != ItemException || ex.AckDigest == "" {
			t.Errorf("exception was not preserved verbatim: %+v", ex)
		}
	})
}
