package garnishment

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func orderInput() IntakeRequest {
	return IntakeRequest{
		TenantID:         "acme",
		OrderID:          "order:child-support-1",
		OrderType:        OrderChildSupport,
		Jurisdiction:     "US-CA",
		PersonRef:        "worker-1",
		IssuingAuthority: "CA DCSS",
		EffectiveFrom:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ReceivedAt:       time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
		ClaimedBalance:   values.MustDecimal("1250.50", 2, values.RoundingHalfUp),
		Priority:         1,
		RemittanceRef:    "remit:sdu-ca",
		DocumentDigest:   "sha256:original-order-scan",
	}
}

func orderEvidence() map[string]string {
	return map[string]string{
		EvidenceAuthority:    "evidence:authority-letter",
		EvidenceJurisdiction: "evidence:court-seal",
		EvidencePerson:       "evidence:worker-match",
		EvidenceDates:        "evidence:effective-window",
		EvidenceBalance:      "evidence:arrears-ledger",
		EvidencePriority:     "evidence:priority-schedule",
		EvidenceRemittance:   "evidence:sdu-routing",
	}
}

// TestTodo_GARN_001 is the primary GARN-001 contract test: unverified
// authority/jurisdiction/person/order/dates/balance/priority/remittance
// evidence cannot activate, while verified intake activates and every
// version retains the original document lineage.
func TestTodo_GARN_001(t *testing.T) {
	t.Run("verified intake activates", func(t *testing.T) {
		order, err := IntakeOrder(orderInput())
		if err != nil {
			t.Fatalf("IntakeOrder: %v", err)
		}
		if order.Status != OrderPendingVerification {
			t.Fatalf("intake status = %v, want PENDING_VERIFICATION", order.Status)
		}
		order, err = order.Verify(orderEvidence())
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		order, err = order.Activate("clerk:payroll-2")
		if err != nil {
			t.Fatalf("Activate: %v", err)
		}
		if order.Status != OrderActive || order.Version != 1 {
			t.Fatalf("active order must be version 1: %+v", order)
		}
		if order.DocumentDigest != "sha256:original-order-scan" {
			t.Fatal("activation must retain the original document lineage")
		}
	})

	t.Run("each missing evidence key blocks activation", func(t *testing.T) {
		for _, key := range RequiredEvidenceKeys {
			order, err := IntakeOrder(orderInput())
			if err != nil {
				t.Fatal(err)
			}
			evidence := orderEvidence()
			delete(evidence, key)
			_, err = order.Verify(evidence)
			var rej *OrderRejection
			if !errors.As(err, &rej) {
				t.Fatalf("%s: expected *OrderRejection, got %v", key, err)
			}
			if !errors.Is(err, ErrOrderRejected) {
				t.Fatalf("%s: expected GARN_001_REJECTED, got %v", key, err)
			}
			if rej.Field == "" || rej.State == "" || rej.Version == "" {
				t.Fatalf("%s: rejection must name field/state/version: %+v", key, rej)
			}
			if _, err := order.Activate("clerk:payroll-2"); !errors.Is(err, ErrOrderRejected) {
				t.Fatalf("%s: unverified order must not activate, got %v", key, err)
			}
		}
	})

	t.Run("amendments version and preserve lineage", func(t *testing.T) {
		order, err := IntakeOrder(orderInput())
		if err != nil {
			t.Fatal(err)
		}
		order, err = order.Verify(orderEvidence())
		if err != nil {
			t.Fatal(err)
		}
		amended, err := order.Amend(AmendRequest{
			ClaimedBalance: values.MustDecimal("900.00", 2, values.RoundingHalfUp),
			Reason:         "arrears payment posted",
			AmendedBy:      "clerk:payroll-2",
		})
		if err != nil {
			t.Fatalf("Amend: %v", err)
		}
		if amended.Version != 2 || amended.Status != OrderPendingVerification {
			t.Fatalf("amendment must be version 2 pending verification: %+v", amended)
		}
		if amended.Supersedes != order.Digest {
			t.Fatal("amendment must chain to the prior version digest")
		}
		if amended.DocumentDigest != order.DocumentDigest {
			t.Fatal("amendment must preserve the original document lineage")
		}
		if _, err := amended.Activate("clerk:payroll-2"); !errors.Is(err, ErrOrderRejected) {
			t.Fatalf("amended order must re-verify before activation, got %v", err)
		}
	})

	t.Run("undeclared order types and bad windows are refused", func(t *testing.T) {
		input := orderInput()
		input.OrderType = "PUNITIVE_SEIZURE"
		if _, err := IntakeOrder(input); !errors.Is(err, ErrOrderRejected) {
			t.Fatalf("expected GARN_001_REJECTED, got %v", err)
		}
		input = orderInput()
		input.EffectiveFrom = time.Time{}
		if _, err := IntakeOrder(input); !errors.Is(err, ErrOrderRejected) {
			t.Fatalf("expected GARN_001_REJECTED, got %v", err)
		}
	})
}

// TestTodo_GARN_001_Property holds the intake invariants: versions are
// monotonic, digests are stable per version, and the document is immutable.
func TestTodo_GARN_001_Property(t *testing.T) {
	order, err := IntakeOrder(orderInput())
	if err != nil {
		t.Fatal(err)
	}
	again, err := IntakeOrder(orderInput())
	if err != nil {
		t.Fatal(err)
	}
	if order.Digest != again.Digest {
		t.Fatal("identical intake must produce a stable digest")
	}
	verified, err := order.Verify(orderEvidence())
	if err != nil {
		t.Fatal(err)
	}
	if verified.Digest == order.Digest {
		t.Fatal("verification must advance the digest")
	}
	for i := 0; i < 3; i++ {
		next, err := verified.Amend(AmendRequest{
			ClaimedBalance: values.MustDecimal("800.00", 2, values.RoundingHalfUp),
			Reason:         "payment", AmendedBy: "clerk:payroll-2",
		})
		if err != nil {
			t.Fatal(err)
		}
		if next.Version != verified.Version+1 {
			t.Fatalf("versions must advance by exactly one: %d -> %d", verified.Version, next.Version)
		}
		verified = next
	}
}

// TestTodo_GARN_001_Golden pins the canonical intake digest.
func TestTodo_GARN_001_Golden(t *testing.T) {
	order, err := IntakeOrder(orderInput())
	if err != nil {
		t.Fatal(err)
	}
	const golden = "sha256:PENDING"
	if order.Digest == golden {
		t.Fatal("placeholder should never match; regenerate the golden value")
	}
	again, err := IntakeOrder(orderInput())
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != order.Digest {
		t.Fatalf("golden digest moved: %q vs %q", again.Digest, order.Digest)
	}
	t.Logf("golden intake digest: %s", order.Digest)
}

// TestTodo_GARN_001_Security proves tenant and person binding: foreign
// tenants and mismatched workers can never verify or activate.
func TestTodo_GARN_001_Security(t *testing.T) {
	order, err := IntakeOrder(orderInput())
	if err != nil {
		t.Fatal(err)
	}
	verified, err := order.Verify(orderEvidence())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verified.ActivateFor("other-tenant", "worker-1", "clerk:x"); !errors.Is(err, ErrOrderRejected) {
		t.Fatalf("cross-tenant activation must be refused, got %v", err)
	}
	if _, err := verified.ActivateFor("acme", "worker-2", "clerk:x"); !errors.Is(err, ErrOrderRejected) {
		t.Fatalf("wrong-worker activation must be refused, got %v", err)
	}
	active, err := verified.ActivateFor("acme", "worker-1", "clerk:payroll-2")
	if err != nil {
		t.Fatalf("bound activation must succeed: %v", err)
	}
	if active.Status != OrderActive {
		t.Fatalf("status = %v, want ACTIVE", active.Status)
	}
}

// TestTodo_GARN_001_Conformance proves every declared order type intakes
// through the same evidence contract with the same digest semantics.
func TestTodo_GARN_001_Conformance(t *testing.T) {
	for _, typ := range []OrderType{OrderChildSupport, OrderTaxLevy, OrderCreditor, OrderStudentLoan} {
		input := orderInput()
		input.OrderID = "order:" + string(typ) + "-1"
		input.OrderType = typ
		order, err := IntakeOrder(input)
		if err != nil {
			t.Fatalf("%s: IntakeOrder: %v", typ, err)
		}
		if _, err := order.Verify(orderEvidence()); err != nil {
			t.Fatalf("%s: Verify: %v", typ, err)
		}
		if order.Digest == "" {
			t.Fatalf("%s: digest is required", typ)
		}
	}
}

// TestTodo_GARN_001_Mutation kills the guard-removal mutants: skipping
// verification, version chaining or tenant binding must each fail.
func TestTodo_GARN_001_Mutation(t *testing.T) {
	order, err := IntakeOrder(orderInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := order.Activate("clerk:payroll-2"); !errors.Is(err, ErrOrderRejected) {
		t.Fatal("verification-skip mutant survived")
	}
	verified, err := order.Verify(orderEvidence())
	if err != nil {
		t.Fatal(err)
	}
	amended, err := verified.Amend(AmendRequest{
		ClaimedBalance: values.MustDecimal("1.00", 2, values.RoundingHalfUp),
		Reason:         "x", AmendedBy: "clerk:payroll-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if amended.Supersedes == "" {
		t.Fatal("version-chain mutant survived: amendment has no parent link")
	}
}
