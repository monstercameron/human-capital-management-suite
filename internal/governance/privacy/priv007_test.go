package privacy

import (
	"errors"
	"testing"
)

// TestTodo_PRIV_007 is the PRIMARY test for planning/todos.md PRIV-007:
// "Reconcile processor acknowledgements and certify fulfillment."
//
// RED (todos.md PRIV-007): "timeout/refusal/unknown copy/unacknowledged
// deletion is marked complete."
//
// GREEN (todos.md PRIV-007): "each processor returns receipt or visible
// retry/escalation; signed package certifies only fully resolved items and
// preserves exceptions."
func TestTodo_PRIV_007(t *testing.T) {
	t.Run("GREEN: every processor receipt reconciles to RESOLVED and certifies Complete", func(t *testing.T) {
		req := fixtureFulfillmentRequest(t)
		rec, err := ReconcileFulfillment(req, fixtureFulfilledAcks(t), mustInstant(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		if len(rec.Resolutions) != len(req.Items) {
			t.Fatalf("resolutions = %d, want %d (one per item)", len(rec.Resolutions), len(req.Items))
		}
		for _, r := range rec.Resolutions {
			if r.Status != ItemResolved {
				t.Errorf("item %q status = %q, want %q", r.ItemID, r.Status, ItemResolved)
			}
			if r.AckDigest == "" {
				t.Errorf("item %q resolved with no ack digest", r.ItemID)
			}
		}
		if rec.Digest == "" {
			t.Error("reconciliation carries no digest")
		}

		// Determinism: the same evidence derives the same digest.
		rec2, err := ReconcileFulfillment(req, fixtureFulfilledAcks(t), mustInstant(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment (repeat): %v", err)
		}
		if rec2.Digest != rec.Digest {
			t.Errorf("repeat digest = %q, want %q", rec2.Digest, rec.Digest)
		}

		cert, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstant(t, fxProcEarly))
		if err != nil {
			t.Fatalf("CertifyFulfillment: %v", err)
		}
		if !cert.Complete {
			t.Error("fully resolved fulfillment certifies Complete = false")
		}
		if len(cert.CertifiedItems) != len(req.Items) {
			t.Errorf("certified items = %d, want %d", len(cert.CertifiedItems), len(req.Items))
		}
		if len(cert.Exceptions) != 0 {
			t.Errorf("complete certificate preserves %d exceptions, want 0", len(cert.Exceptions))
		}
		if err := cert.Validate(); err != nil {
			t.Errorf("certificate does not validate: %v", err)
		}
	})

	t.Run("RED: a timed-out processor is escalated, never marked complete", func(t *testing.T) {
		rec, err := ReconcileFulfillment(
			fixtureFulfillmentRequest(t),
			nil, // no acknowledgement ever arrived
			mustInstant(t, fxProcLate),
		)
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		for _, r := range rec.Resolutions {
			if r.Status != ItemEscalated {
				t.Errorf("item %q status = %q, want %q", r.ItemID, r.Status, ItemEscalated)
			}
		}
		if _, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstant(t, fxProcLate)); !errors.Is(err, ErrFulfillmentIncomplete) {
			t.Fatalf("CertifyFulfillment(escalated) = %v, want %v", err, ErrFulfillmentIncomplete)
		}
	})

	t.Run("RED: a refused deletion is an exception, never a certified item", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[0] = ProcessorAck{
			ItemID: "copy-backup-a", Processor: "processor-backup",
			Outcome: AckRefused, At: mustInstant(t, fxProcAckAt),
			Detail: "legal-hold: litigation-2026-04",
		}
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstant(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		if rec.Resolutions[0].Status != ItemException {
			t.Fatalf("refused item status = %q, want %q", rec.Resolutions[0].Status, ItemException)
		}
		cert, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstant(t, fxProcEarly))
		if err != nil {
			t.Fatalf("CertifyFulfillment: %v", err)
		}
		if cert.Complete {
			t.Error("fulfillment with a refused deletion certifies Complete = true")
		}
		for _, id := range cert.CertifiedItems {
			if id == "copy-backup-a" {
				t.Error("refused item appears in CertifiedItems")
			}
		}
		if len(cert.Exceptions) != 1 || cert.Exceptions[0].ItemID != "copy-backup-a" {
			t.Errorf("exceptions = %+v, want the refused item preserved", cert.Exceptions)
		}
		if cert.Exceptions[0].Detail != "legal-hold: litigation-2026-04" {
			t.Errorf("exception detail = %q, refusal reason was not preserved", cert.Exceptions[0].Detail)
		}
	})

	t.Run("RED: an unknown copy is an exception, never a certified item", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[1] = ProcessorAck{
			ItemID: "copy-analytics-b", Processor: "processor-analytics",
			Outcome: AckUnknownCopy, At: mustInstant(t, fxProcAckAt),
			Detail: "no such shard at this processor",
		}
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstant(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		if rec.Resolutions[1].Status != ItemException {
			t.Fatalf("unknown-copy item status = %q, want %q", rec.Resolutions[1].Status, ItemException)
		}
		cert, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstant(t, fxProcEarly))
		if err != nil {
			t.Fatalf("CertifyFulfillment: %v", err)
		}
		if cert.Complete {
			t.Error("fulfillment with an unknown copy certifies Complete = true")
		}
		for _, id := range cert.CertifiedItems {
			if id == "copy-analytics-b" {
				t.Error("unknown-copy item appears in CertifiedItems")
			}
		}
	})

	t.Run("RED: an unacknowledged deletion before its due date retries visibly and blocks certification", func(t *testing.T) {
		var acks []ProcessorAck // silence: nothing arrived yet
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstant(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		for _, r := range rec.Resolutions {
			if r.Status != ItemPendingRetry {
				t.Errorf("item %q status = %q, want %q", r.ItemID, r.Status, ItemPendingRetry)
			}
		}
		if _, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstant(t, fxProcEarly)); !errors.Is(err, ErrFulfillmentIncomplete) {
			t.Fatalf("CertifyFulfillment(pending retry) = %v, want %v", err, ErrFulfillmentIncomplete)
		}
	})
}
