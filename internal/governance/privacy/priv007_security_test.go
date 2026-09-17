package privacy

import (
	"errors"
	"testing"
)

// TestTodo_PRIV_007_Security is the SECURITY matrix test for PRIV-007. Each
// case proves one integrity property of the signed fulfillment package: acks
// cannot cross processor boundaries, raw payload cannot stand in for a
// receipt, tampered packages fail validation, and certification needs a
// named certifier.
func TestTodo_PRIV_007_Security(t *testing.T) {
	t.Run("one processor cannot acknowledge another processor's copy", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		// processor-analytics answers for processor-backup's item, with an
		// otherwise valid receipt: the binding, not the shape, must fail it.
		acks[0] = ProcessorAck{
			ItemID: "copy-backup-a", Processor: "processor-analytics",
			Outcome: AckFulfilled, At: mustInstantAt(t, fxProcAckAt),
			ReceiptDigest: fxProcReceiptA,
		}
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(cross-processor ack) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("raw payload is refused as a receipt: digests only", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[1].ReceiptDigest = "deleted shard-7 contents: worker record dump"
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(raw-payload receipt) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("a refusal smuggling a receipt is refused", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[0] = ProcessorAck{
			ItemID: "copy-backup-a", Processor: "processor-backup",
			Outcome: AckRefused, At: mustInstantAt(t, fxProcAckAt),
			ReceiptDigest: fxProcReceiptA, Detail: "legal-hold",
		}
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(refusal with receipt) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("an acknowledgement outside the outcome vocabulary is refused", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[2].Outcome = "DELETED_MAYBE"
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(unknown outcome) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("certification without a named certifier is refused", func(t *testing.T) {
		rec := fixtureReconciliation(t)
		if _, err := CertifyFulfillment(rec, "", mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("CertifyFulfillment(anonymous) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("a certificate with its Complete flag flipped fails validation", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[0] = ProcessorAck{
			ItemID: "copy-backup-a", Processor: "processor-backup",
			Outcome: AckRefused, At: mustInstantAt(t, fxProcAckAt),
			Detail: "legal-hold: litigation-2026-04",
		}
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		cert, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("CertifyFulfillment: %v", err)
		}
		if cert.Complete {
			t.Fatal("partial fulfillment certified Complete before tampering")
		}
		forged := cert
		forged.Complete = true // flip the flag without re-deriving the digest
		if err := forged.Validate(); err == nil {
			t.Fatal("Validate passed on a certificate with a flipped Complete flag")
		}
	})

	t.Run("a certificate with a dropped exception fails validation", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[0] = ProcessorAck{
			ItemID: "copy-backup-a", Processor: "processor-backup",
			Outcome: AckRefused, At: mustInstantAt(t, fxProcAckAt),
			Detail: "legal-hold: litigation-2026-04",
		}
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		cert, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("CertifyFulfillment: %v", err)
		}
		forged := cert
		forged.Exceptions = nil // drop the preserved exception
		if err := forged.Validate(); err == nil {
			t.Fatal("Validate passed on a certificate with a dropped exception")
		}
	})

	t.Run("malformed packages fail validation before any digest check", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[0] = ProcessorAck{
			ItemID: "copy-backup-a", Processor: "processor-backup",
			Outcome: AckRefused, At: mustInstantAt(t, fxProcAckAt),
			Detail: "legal-hold: litigation-2026-04",
		}
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		cert, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstantAt(t, fxProcEarly))
		if err != nil {
			t.Fatalf("CertifyFulfillment: %v", err)
		}
		cases := map[string]func(FulfillmentCertificate) FulfillmentCertificate{
			"anonymous certifier": func(c FulfillmentCertificate) FulfillmentCertificate {
				c.Certifier = ""
				return c
			},
			"complete with preserved exceptions": func(c FulfillmentCertificate) FulfillmentCertificate {
				c.Complete = true
				return c
			},
			"non-exception preserved as exception": func(c FulfillmentCertificate) FulfillmentCertificate {
				c.Exceptions = append(c.Exceptions, ItemResolution{
					ItemID: "copy-analytics-b", Processor: "processor-analytics",
					Status: ItemResolved, AckDigest: fxProcReceiptB,
				})
				return c
			},
		}
		for name, mutate := range cases {
			if err := mutate(cert).Validate(); err == nil {
				t.Errorf("Validate(%s) passed on a malformed package", name)
			} else if !errors.Is(err, ErrProcessorCertBlocked) {
				t.Errorf("Validate(%s) = %v, want %v", name, err, ErrProcessorCertBlocked)
			}
		}
	})

	t.Run("certification over a structurally forged reconciliation is refused", func(t *testing.T) {
		rec := fixtureReconciliation(t)
		forged := rec
		forged.Resolutions = append(forged.Resolutions, ItemResolution{
			ItemID: "copy-invented", Processor: "processor-backup",
			Status: ItemResolved, AckDigest: fxProcReceiptA,
		})
		if _, err := CertifyFulfillment(forged, "privacy-officer-1", mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("CertifyFulfillment(forged reconciliation) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})
}
