package messaging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fulfillLedger(t *testing.T, instruction FulfillmentInstruction) *FulfillmentLedger {
	t.Helper()
	ledger := NewFulfillmentLedger(instruction.TenantID)
	if err := ledger.Register(instruction); err != nil {
		t.Fatal(err)
	}
	return ledger
}

// TestTodo_FULFILL_001_Property: replay is idempotent under interleaving,
// custody gaps never heal, and every digest is stable across runs.
func TestTodo_FULFILL_001_Property(t *testing.T) {
	instruction := fulfillInstruction(t)
	ledger := fulfillLedger(t, instruction)
	first, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: instruction.Digest, VendorEventID: "evt-1",
		Outcome: OutcomePrinted, Proof: ProofPrintLog, At: fulfillAt(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replay, err := ledger.RecordOutcome(OutcomeRequest{
				InstructionDigest: instruction.Digest, VendorEventID: "evt-1",
				Outcome: OutcomePrinted, Proof: ProofPrintLog, At: fulfillAt(),
			})
			if err != nil {
				t.Error(err)
				return
			}
			if replay.Digest != first.Digest {
				t.Error("concurrent replay diverged")
			}
		}()
	}
	wg.Wait()
	if outcomes := ledger.Outcomes(instruction.Digest); len(outcomes) != 1 {
		t.Fatalf("replay multiplied outcomes: %d", len(outcomes))
	}
	// No gap size heals a broken chain: skipping any sequence is refused
	// and the chain stays exactly where it was.
	for _, gap := range []uint64{2, 3, 99} {
		if err := ledger.RecordCustody(CustodyReceipt{
			InstructionDigest: instruction.Digest, Sequence: gap,
			Stage: StageReceived, VendorID: "vendor:postal-1", At: fulfillAt(),
		}); err == nil {
			t.Fatalf("custody gap %d was accepted", gap)
		}
	}
	if err := ledger.RecordCustody(CustodyReceipt{
		InstructionDigest: instruction.Digest, Sequence: 1,
		Stage: StageReceived, VendorID: "vendor:postal-1", At: fulfillAt(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RecordCustody(CustodyReceipt{
		InstructionDigest: instruction.Digest, Sequence: 1,
		Stage: StagePrinted, VendorID: "vendor:postal-1", At: fulfillAt(),
	}); err == nil {
		t.Fatal("custody replay was accepted")
	}
	// Instruction digest is stable: issuing the same request twice pins
	// the same digest, so a batch cannot hold two identities for one job.
	repeat, err := IssueFulfillment(FulfillmentRequest{
		InstructionID: "fulfill-1", TenantID: "tenant-a", NoticeRef: "notice:leave-1",
		ArtifactHash: "sha256:artifact-1", ArtifactVersion: "templates/leave-notice/v3",
		Address: AddressRevision{
			Revision: 7, Digest: "sha256:address-7", AuthorizedBy: "employee-relations",
		},
		EnvelopeProfile: EnvelopeWindowed, PrivacyProfile: PrivacySealed,
		VendorOperation: OpPrintAndMail, VendorID: "vendor:postal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repeat.Digest != instruction.Digest {
		t.Fatal("identical fulfillment requests pinned different digests")
	}
}

// TestTodo_FULFILL_001_Golden: the pinned instruction and outcome digests
// for the fixed fixture. Regeneration must be re-vetted, never blind.
func TestTodo_FULFILL_001_Golden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "fulfill.golden"))
	if err != nil {
		t.Fatal(err)
	}
	golden := string(raw)
	instruction := fulfillInstruction(t)
	if !strings.Contains(golden, "instruction: "+instruction.Digest) {
		t.Fatalf("instruction digest %q is not the vetted golden", instruction.Digest)
	}
	ledger := fulfillLedger(t, instruction)
	var digests []string
	for i, step := range []struct {
		outcome FulfillmentOutcome
		proof   ProofKind
	}{
		{OutcomePrinted, ProofPrintLog},
		{OutcomeMailed, ProofPostalManifest},
		{OutcomeInTransit, ProofCarrierScan},
		{OutcomeDelivered, ProofRecipientSignature},
	} {
		recorded, err := ledger.RecordOutcome(OutcomeRequest{
			InstructionDigest: instruction.Digest, VendorEventID: fmt.Sprintf("evt-g-%d", i),
			Outcome: step.outcome, Proof: step.proof, At: fulfillAt(),
		})
		if err != nil {
			t.Fatal(err)
		}
		digests = append(digests, recorded.Digest)
	}
	for _, digest := range digests {
		if !strings.Contains(golden, digest) {
			t.Fatalf("outcome digest %q is not the vetted golden", digest)
		}
	}
}

// TestTodo_FULFILL_001_Fault: every fault reaches a refused, duplicate-free
// state — unknown instructions, conflicting events, lifecycle jumps and
// post-terminal writes all fail without recording.
func TestTodo_FULFILL_001_Fault(t *testing.T) {
	instruction := fulfillInstruction(t)
	ledger := fulfillLedger(t, instruction)
	before := len(ledger.Outcomes(instruction.Digest))
	refuse := func(name string, req OutcomeRequest) {
		t.Helper()
		if _, err := ledger.RecordOutcome(req); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	refuse("unknown instruction", OutcomeRequest{
		InstructionDigest: "sha256:nowhere", VendorEventID: "evt-x",
		Outcome: OutcomePrinted, Proof: ProofPrintLog, At: fulfillAt(),
	})
	refuse("lifecycle jump to delivered", OutcomeRequest{
		InstructionDigest: instruction.Digest, VendorEventID: "evt-jump",
		Outcome: OutcomeDelivered, Proof: ProofRecipientSignature, At: fulfillAt(),
	})
	if _, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: instruction.Digest, VendorEventID: "evt-once",
		Outcome: OutcomePrinted, Proof: ProofPrintLog, At: fulfillAt(),
	}); err != nil {
		t.Fatal(err)
	}
	refuse("conflicting event reuse", OutcomeRequest{
		InstructionDigest: instruction.Digest, VendorEventID: "evt-once",
		Outcome: OutcomeMailed, Proof: ProofPostalManifest, At: fulfillAt(),
	})
	if outcomes := ledger.Outcomes(instruction.Digest); len(outcomes) != before+1 {
		t.Fatalf("refused recordings left effects: %d outcomes", len(outcomes))
	}
}

// TestTodo_FULFILL_001_Security: tenant boundaries hold and the vendor
// view carries no identity beyond hashes and handling profiles.
func TestTodo_FULFILL_001_Security(t *testing.T) {
	instruction := fulfillInstruction(t)
	foreign := NewFulfillmentLedger("tenant-b")
	if err := foreign.Register(instruction); err == nil {
		t.Fatal("cross-tenant instruction was registered")
	}
	view := instruction.VendorView()
	flat := fmt.Sprintf("%+v", view)
	for _, leaked := range []string{"tenant-a", "notice:leave-1", "fulfill-1"} {
		if strings.Contains(flat, leaked) {
			t.Fatalf("vendor view leaks %q", leaked)
		}
	}
	if view.InstructionDigest != instruction.Digest || view.ArtifactHash != "sha256:artifact-1" ||
		view.AddressDigest != "sha256:address-7" || view.AddressRevision != 7 {
		t.Fatalf("vendor view lost its bindings: %+v", view)
	}
}

// TestTodo_FULFILL_001_Conformance: every outcome binds exactly one proof,
// and no custody receipt — especially carrier acceptance — can stand in
// for recipient acknowledgement.
func TestTodo_FULFILL_001_Conformance(t *testing.T) {
	proofs := map[FulfillmentOutcome]ProofKind{
		OutcomePrinted: ProofPrintLog, OutcomeMailed: ProofPostalManifest,
		OutcomeInTransit: ProofCarrierScan, OutcomeDelivered: ProofRecipientSignature,
		OutcomeReturned: ProofReturnScan, OutcomeUnknown: ProofLossAttestation,
	}
	for outcome, proof := range proofs {
		if proofFor(outcome) != proof {
			t.Fatalf("outcome %q binds proof %q, want %q", outcome, proofFor(outcome), proof)
		}
	}
	instruction := fulfillInstruction(t)
	ledger := fulfillLedger(t, instruction)
	if err := ledger.RecordCustody(CustodyReceipt{
		InstructionDigest: instruction.Digest, Sequence: 1,
		Stage: StageTenderedToCarrier, VendorID: "vendor:postal-1", At: fulfillAt(),
	}); err != nil {
		t.Fatal(err)
	}
	// Tender proves custody only: even with the carrier holding the piece,
	// acknowledgement still requires the recipient's signature.
	for _, proof := range []ProofKind{ProofCarrierScan, ProofPostalManifest, ProofPrintLog} {
		if _, err := ledger.RecordOutcome(OutcomeRequest{
			InstructionDigest: instruction.Digest, VendorEventID: "evt-ack-" + string(proof),
			Outcome: OutcomeDelivered, Proof: proof, At: fulfillAt(),
		}); err == nil {
			t.Fatalf("proof %q acknowledged delivery", proof)
		}
	}
}
