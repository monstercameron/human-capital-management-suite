package privacy

import (
	"errors"
	"testing"
)

// TestTodo_PRIV_007_Fault is the FAULT matrix test for PRIV-007. Each case
// injects one failure-mode input -- duplicate, conflicting, misaddressed or
// future-dated evidence, or an unresolvable certification -- and proves the
// reconciler reaches an allowed state with no silent completion.
func TestTodo_PRIV_007_Fault(t *testing.T) {
	t.Run("a duplicate acknowledgement for one item is refused, not merged", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks = append(acks, acks[0]) // same item acknowledged twice
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(duplicate ack) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("conflicting outcomes for one item are refused, not last-writer-wins", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks = append(acks, ProcessorAck{
			ItemID: "copy-backup-a", Processor: "processor-backup",
			Outcome: AckRefused, At: mustInstantAt(t, fxProcAckAt),
			Detail: "second, conflicting story",
		})
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(conflicting acks) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("an ack for an item the request never named is refused", func(t *testing.T) {
		acks := append(fixtureFulfilledAcks(t), ProcessorAck{
			ItemID: "copy-ghost", Processor: "processor-backup",
			Outcome: AckFulfilled, At: mustInstantAt(t, fxProcAckAt),
			ReceiptDigest: fxProcReceiptA,
		})
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(unknown item ack) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("an ack from the wrong processor for an item is refused", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[0].Processor = "processor-analytics" // item belongs to processor-backup
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(wrong processor) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("a future-dated acknowledgement cannot reconcile the past", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[0].At = mustInstantAt(t, fxProcLate) // ack arrives after "now"
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(future ack) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("a fulfilled ack without a receipt digest is refused", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[2].ReceiptDigest = "" // "deleted, trust me"
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(receiptless fulfillment) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("a refusal without a reason is refused: exceptions must be reviewable", func(t *testing.T) {
		acks := fixtureFulfilledAcks(t)
		acks[0] = ProcessorAck{
			ItemID: "copy-backup-a", Processor: "processor-backup",
			Outcome: AckRefused, At: mustInstantAt(t, fxProcAckAt),
			// no Detail: an unexplained refusal cannot become an exception
		}
		if _, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), acks, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(reasonless refusal) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("an empty request reconciles nothing", func(t *testing.T) {
		req := fixtureFulfillmentRequest(t)
		req.Items = nil
		if _, err := ReconcileFulfillment(req, nil, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(empty request) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("duplicate item ids in one request are refused", func(t *testing.T) {
		req := fixtureFulfillmentRequest(t)
		req.Items = append(req.Items, req.Items[0])
		if _, err := ReconcileFulfillment(req, nil, mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
			t.Fatalf("ReconcileFulfillment(duplicate items) = %v, want %v", err, ErrProcessorCertBlocked)
		}
	})

	t.Run("certification names every unresolved item in its error", func(t *testing.T) {
		rec, err := ReconcileFulfillment(fixtureFulfillmentRequest(t), nil, mustInstantAt(t, fxProcLate))
		if err != nil {
			t.Fatalf("ReconcileFulfillment: %v", err)
		}
		err = mustFailCertify(t, rec)
		if !errors.Is(err, ErrFulfillmentIncomplete) {
			t.Fatalf("CertifyFulfillment = %v, want %v", err, ErrFulfillmentIncomplete)
		}
		for _, id := range []string{"copy-backup-a", "copy-analytics-b", "copy-archive-c"} {
			if !containsSubstring(err.Error(), id) {
				t.Errorf("incomplete error %q names no %q", err.Error(), id)
			}
		}
	})

	t.Run("structurally invalid reconciliations never certify", func(t *testing.T) {
		base := fixtureReconciliation(t)
		cases := map[string]func(Reconciliation) Reconciliation{
			"no digest": func(r Reconciliation) Reconciliation { r.Digest = ""; return r },
			"no request binding": func(r Reconciliation) Reconciliation {
				r.RequestDigest = ""
				return r
			},
			"unknown status": func(r Reconciliation) Reconciliation {
				r.Resolutions[0].Status = "COMPLETE_MAYBE"
				return r
			},
			"resolved with no ack": func(r Reconciliation) Reconciliation {
				r.Resolutions[0].Status = ItemResolved
				r.Resolutions[0].AckDigest = ""
				return r
			},
		}
		for name, mutate := range cases {
			// Clone the resolutions: a struct copy would share base's
			// backing array and let one case corrupt the next.
			fresh := base
			fresh.Resolutions = append([]ItemResolution(nil), base.Resolutions...)
			if _, err := CertifyFulfillment(mutate(fresh), "privacy-officer-1", mustInstantAt(t, fxProcEarly)); !errors.Is(err, ErrProcessorCertBlocked) {
				t.Errorf("CertifyFulfillment(%s) = %v, want %v", name, err, ErrProcessorCertBlocked)
			}
		}
	})
}

// mustFailCertify certifies at fxProcLate and fails the test when
// certification unexpectedly succeeds; the caller asserts on the error.
func mustFailCertify(t *testing.T, rec Reconciliation) error {
	t.Helper()
	_, err := CertifyFulfillment(rec, "privacy-officer-1", mustInstantAt(t, fxProcLate))
	if err == nil {
		t.Fatal("CertifyFulfillment succeeded on an unresolvable reconciliation")
	}
	return err
}

// containsSubstring reports whether s contains sub. It is named to avoid the
// existing containsString([]string, string) helper in this package's tests.
func containsSubstring(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
