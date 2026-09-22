package signal_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// Rejected deliveries remain audit evidence, not evidence of completed work.
func TestTodo_WF_STEP_020(t *testing.T) {
	for _, differentPayload := range []bool{false, true} {
		name := "same_payload"
		if differentPayload {
			name = "corrected_payload"
		}
		t.Run(name, func(t *testing.T) {
			sub := baseSubscription()
			log := signal.NewSignalLog()
			bad := baseSignal(t, []byte(`{"result":"CLEAR"}`))
			bad.Signature = []byte("invalid")
			got, err := log.Accept(sub, bad, testVerifier, testNow)
			requireStatus(t, got, err, signal.StatusRefusedInvalidSignature)
			good := baseSignal(t, bad.Payload)
			if differentPayload {
				good.Payload = []byte(`{"result":"REVIEW"}`)
				good.Signature = testVerifier.sign(good.Payload)
			}
			got, err = log.Accept(sub, good, testVerifier, testNow)
			if err != nil || got.Status != signal.StatusAccepted || !got.Continuation {
				t.Fatalf("first valid delivery must schedule a continuation: %+v, %v", got, err)
			}
			got, err = log.Accept(sub, good, testVerifier, testNow)
			requireStatus(t, got, err, signal.StatusDuplicateSameBytes)
			if got.Continuation {
				t.Fatal("accepted replay must not schedule another continuation")
			}
			conflict := baseSignal(t, []byte(`{"result":"DIFFERENT"}`))
			got, err = log.Accept(sub, conflict, testVerifier, testNow)
			requireStatus(t, got, err, signal.StatusRefusedDuplicateDifferentBytes)
			entries := log.For(sub.Digest())
			if len(entries) != 4 || entries[0].Status != signal.StatusRefusedInvalidSignature || entries[1].Status != signal.StatusAccepted {
				t.Fatalf("acceptance must not overwrite refusal evidence: %+v", entries)
			}
		})
	}
}

func TestTodo_WF_STEP_020_Security(t *testing.T) {
	t.Run("wrong_tenant_cannot_poison_key", func(t *testing.T) {
		sub := baseSubscription()
		log := signal.NewSignalLog()
		sig := baseSignal(t, []byte(`{"result":"CLEAR"}`))
		wrong := sig
		wrong.Tenant = "other-tenant"
		got, err := log.Accept(sub, wrong, testVerifier, testNow)
		requireStatus(t, got, err, signal.StatusRefusedWrongTenant)
		got, err = log.Accept(sub, sig, testVerifier, testNow)
		if err != nil || got.Status != signal.StatusAccepted || !got.Continuation {
			t.Fatalf("wrong-tenant audit record suppressed valid continuation: %+v, %v", got, err)
		}
	})
	t.Run("repeated_late_delivery_stays_refused", func(t *testing.T) {
		sub := baseSubscription()
		sub.ClosesAt = testNow
		log := signal.NewSignalLog()
		sig := baseSignal(t, []byte(`{"result":"CLEAR"}`))
		for range 2 {
			got, err := log.Accept(sub, sig, testVerifier, testNow)
			requireStatus(t, got, err, signal.StatusRefusedLate)
			if got.Continuation || got.Status.Accepted() {
				t.Fatal("late replay became accepted evidence")
			}
		}
	})
	t.Run("repeated_out_of_order_delivery_stays_refused", func(t *testing.T) {
		sub := baseSubscription()
		log := signal.NewSignalLog()
		sig := baseSignal(t, []byte(`{"result":"CLEAR"}`))
		sig.SequenceNumber = 2
		got, err := log.Accept(sub, sig, testVerifier, testNow)
		if err != nil || got.Status != signal.StatusAccepted || !got.Continuation {
			t.Fatalf("initial ordered delivery: %+v, %v", got, err)
		}
		sig.SequenceNumber = 1
		sig.IdempotencyKey = "older-delivery"
		for range 2 {
			got, err = log.Accept(sub, sig, testVerifier, testNow)
			requireStatus(t, got, err, signal.StatusRefusedWrongOrder)
			if got.Continuation {
				t.Fatal("out-of-order replay resumed workflow")
			}
		}
	})
}
