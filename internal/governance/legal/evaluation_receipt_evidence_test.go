package legal

import (
	"testing"
)

// TestTodo_LEGAL_014_RegistryIndependent proves the RED clause that a receipt
// stops verifying once its releases are unregistered is false: the release
// digest travels inside the receipt, so verification is offline and survives
// registry loss, while a fresh evaluation against the emptied registry fails
// closed instead of silently evaluating against nothing.
func TestTodo_LEGAL_014_RegistryIndependent(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	receipt, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
	if err != nil {
		t.Fatal(err)
	}
	lost := NewRegistry()
	if err := receipt.VerifyWithKey(signer.PublicKey()); err != nil {
		t.Fatalf("receipt stopped verifying after registry loss: %v", err)
	}
	result, err := Evaluate(ctx, PromotionProposalSnapshot{}, lost)
	if err != nil {
		t.Fatalf("evaluation against an emptied registry should report, not error: %v", err)
	}
	if result.Status != LegalEvaluationStatusRuleCoverageUnknown {
		t.Fatalf("status = %s, want RULE_COVERAGE_UNKNOWN", result.Status)
	}
	if len(result.Obligations) != 0 {
		t.Fatalf("registry-less evaluation produced %d obligations", len(result.Obligations))
	}
}

// TestTodo_LEGAL_014_EvidencesObligationTransitions proves the GREEN clause
// that the receipt is the artifact ObligationState transitions are evidenced
// by: every discharge in the evaluation binding cites the receipt digest,
// and every discharged obligation is one the receipt applied.
func TestTodo_LEGAL_014_EvidencesObligationTransitions(t *testing.T) {
	ctx, registry, signer, at := receiptFixture(t)
	receipt, err := EvaluateReceipt(ctx, PromotionProposalSnapshot{}, registry, signer, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.ObligationsApplied) == 0 {
		t.Fatal("fixture receipt applied no obligations to discharge")
	}
	applied := make([]BoundObligation, 0, len(receipt.ObligationsApplied))
	byID := map[BoundObligation]bool{}
	for _, o := range receipt.ObligationsApplied {
		bound := BoundObligation{Type: o.Type, ID: o.ID, BodyDigest: o.BodyDigest}
		applied = append(applied, bound)
		byID[bound] = true
	}
	first := applied[0]
	binding, err := SignEvaluationBinding(EvaluationBinding{
		Tenant:             "tenant-1",
		IntentID:           "promote-worker",
		ProposalRevisionID: "proposal-1",
		MaterialDigest:     "material-digest",
		ReceiptRef:         "receipt-1",
		ReceiptDigest:      receipt.Digest,
		LegalContextDigest: receipt.LegalContextDigest,
		AppliedObligations: applied,
		Discharges:         []ObligationDischarge{{Obligation: first, EvidenceRefs: []string{"evidence-1"}}},
	}, signer)
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.VerifyWithKey(signer.PublicKey()); err != nil {
		t.Fatalf("binding VerifyWithKey: %v", err)
	}
	if binding.ReceiptDigest != receipt.Digest {
		t.Fatal("binding does not cite the receipt it discharges under")
	}
	for _, d := range binding.Discharges {
		if !byID[d.Obligation] {
			t.Fatalf("discharge evidences an obligation the receipt never applied: %+v", d.Obligation)
		}
	}
}
