package messaging

import (
	"testing"
)

// TestTodo_FULFILL_001_Integration proves the full vendor lifecycle on one
// ledger: custody without gaps, then PRINTED/MAILED/IN_TRANSIT/DELIVERED in
// governed order with exact proofs.
func TestTodo_FULFILL_001_Integration(t *testing.T) {
	instruction := fulfillInstruction(t)
	ledger := NewFulfillmentLedger("tenant-a")
	if err := ledger.Register(instruction); err != nil {
		t.Fatal(err)
	}
	for i, stage := range []CustodyStage{StageReceived, StagePrinted, StageTenderedToCarrier} {
		if err := ledger.RecordCustody(CustodyReceipt{
			InstructionDigest: instruction.Digest, Sequence: uint64(i + 1),
			Stage: stage, VendorID: "vendor:postal-1", At: fulfillAt(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	chain := []struct {
		event   string
		outcome FulfillmentOutcome
		proof   ProofKind
	}{
		{"evt-print", OutcomePrinted, ProofPrintLog},
		{"evt-mail", OutcomeMailed, ProofPostalManifest},
		{"evt-transit", OutcomeInTransit, ProofCarrierScan},
		{"evt-deliver", OutcomeDelivered, ProofRecipientSignature},
	}
	for _, step := range chain {
		outcome, err := ledger.RecordOutcome(OutcomeRequest{
			InstructionDigest: instruction.Digest, VendorEventID: step.event,
			Outcome: step.outcome, Proof: step.proof, At: fulfillAt(),
		})
		if err != nil {
			t.Fatalf("%s: %v", step.event, err)
		}
		if outcome.Digest == "" || outcome.Fallback != nil {
			t.Fatalf("outcome=%+v", outcome)
		}
	}
	if got := len(ledger.Outcomes(instruction.Digest)); got != len(chain) {
		t.Fatalf("outcomes=%d, want %d", got, len(chain))
	}
}

// TestTodo_FULFILL_001_Recovery proves returned mail creates exactly one
// governed fallback and replays idempotently: the same vendor event returns
// the identical outcome, while a conflicting reuse is refused.
func TestTodo_FULFILL_001_Recovery(t *testing.T) {
	instruction := fulfillInstruction(t)
	ledger := NewFulfillmentLedger("tenant-a")
	if err := ledger.Register(instruction); err != nil {
		t.Fatal(err)
	}
	record := func(eventID string, outcome FulfillmentOutcome, proof ProofKind) DeliveryOutcome {
		t.Helper()
		result, err := ledger.RecordOutcome(OutcomeRequest{
			InstructionDigest: instruction.Digest, VendorEventID: eventID,
			Outcome: outcome, Proof: proof, At: fulfillAt(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	record("evt-print", OutcomePrinted, ProofPrintLog)
	record("evt-mail", OutcomeMailed, ProofPostalManifest)
	returned := record("evt-return", OutcomeReturned, ProofReturnScan)
	if returned.Fallback == nil || returned.Fallback.WorkID == "" {
		t.Fatalf("returned=%+v, want governed fallback", returned)
	}
	replay, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: instruction.Digest, VendorEventID: "evt-return",
		Outcome: OutcomeReturned, Proof: ProofReturnScan, At: fulfillAt(),
	})
	if err != nil || replay.Digest != returned.Digest {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if _, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: instruction.Digest, VendorEventID: "evt-return",
		Outcome: OutcomeDelivered, Proof: ProofRecipientSignature, At: fulfillAt(),
	}); err == nil {
		t.Fatal("conflicting vendor event reuse accepted")
	}
}

// TestTodo_FULFILL_001_Mutation proves forged and out-of-order outcomes
// never record: wrong proofs, skipped lifecycle steps and post-terminal
// writes are all refused.
func TestTodo_FULFILL_001_Mutation(t *testing.T) {
	instruction := fulfillInstruction(t)
	ledger := NewFulfillmentLedger("tenant-a")
	if err := ledger.Register(instruction); err != nil {
		t.Fatal(err)
	}
	record := func(eventID string, outcome FulfillmentOutcome, proof ProofKind) error {
		_, err := ledger.RecordOutcome(OutcomeRequest{
			InstructionDigest: instruction.Digest, VendorEventID: eventID,
			Outcome: outcome, Proof: proof, At: fulfillAt(),
		})
		return err
	}
	if err := record("evt-x", OutcomePrinted, ProofPostalManifest); err == nil {
		t.Fatal("wrong proof recorded")
	}
	if err := record("evt-x", OutcomeDelivered, ProofRecipientSignature); err == nil {
		t.Fatal("skipped lifecycle recorded")
	}
	if err := record("evt-print", OutcomePrinted, ProofPrintLog); err != nil {
		t.Fatal(err)
	}
	if err := record("evt-mail", OutcomeMailed, ProofPostalManifest); err != nil {
		t.Fatal(err)
	}
	if err := record("evt-transit", OutcomeInTransit, ProofCarrierScan); err != nil {
		t.Fatal(err)
	}
	if err := record("evt-deliver", OutcomeDelivered, ProofRecipientSignature); err != nil {
		t.Fatal(err)
	}
	if err := record("evt-late", OutcomeUnknown, ProofLossAttestation); err == nil {
		t.Fatal("post-terminal write recorded")
	}
}
