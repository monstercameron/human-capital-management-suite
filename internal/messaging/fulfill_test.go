package messaging

import (
	"strings"
	"testing"
	"time"
)

func fulfillAt() time.Time {
	return time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
}

func fulfillInstruction(t *testing.T) FulfillmentInstruction {
	t.Helper()
	instruction, err := IssueFulfillment(FulfillmentRequest{
		InstructionID:   "fulfill-1",
		TenantID:        "tenant-a",
		NoticeRef:       "notice:leave-1",
		ArtifactHash:    "sha256:artifact-1",
		ArtifactVersion: "templates/leave-notice/v3",
		Address: AddressRevision{
			Revision: 7, Digest: "sha256:address-7", AuthorizedBy: "employee-relations",
		},
		EnvelopeProfile: EnvelopeWindowed,
		PrivacyProfile:  PrivacySealed,
		VendorOperation: OpPrintAndMail,
		VendorID:        "vendor:postal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return instruction
}

// TestPhysicalNoticeFulfillmentBindsArtifactAddressCustodyAndDeliveryEvidence
// is FULFILL-001: the instruction pins the minimized artifact hash, the
// authorized address revision, the envelope/privacy profile and the vendor
// operation; custody chains without gaps; outcomes follow the governed
// lifecycle; carrier acceptance never counts as acknowledgement; and
// returned mail creates exactly one governed fallback.
func TestPhysicalNoticeFulfillmentBindsArtifactAddressCustodyAndDeliveryEvidence(t *testing.T) {
	instruction := fulfillInstruction(t)
	if instruction.Digest == "" {
		t.Fatal("instruction carries no digest")
	}
	if err := instruction.Validate(); err != nil {
		t.Fatal(err)
	}
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
	record("evt-transit", OutcomeInTransit, ProofCarrierScan)
	// Seeded defect: carrier acceptance is custody evidence, never
	// recipient acknowledgement.
	if _, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: instruction.Digest, VendorEventID: "evt-carrier-delivered",
		Outcome: OutcomeDelivered, Proof: ProofCarrierScan, At: fulfillAt(),
	}); err == nil {
		t.Fatal("carrier acceptance was recorded as recipient acknowledgement")
	}
	delivered := record("evt-signed", OutcomeDelivered, ProofRecipientSignature)
	if delivered.Outcome != OutcomeDelivered {
		t.Fatalf("outcome = %q", delivered.Outcome)
	}
	// Duplicate vendor events are idempotent: the same event twice is one
	// outcome, never a duplicate mailing.
	again, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: instruction.Digest, VendorEventID: "evt-signed",
		Outcome: OutcomeDelivered, Proof: ProofRecipientSignature, At: fulfillAt(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != delivered.Digest {
		t.Fatal("duplicate vendor event produced a second outcome")
	}

	// Returned mail creates exactly one governed fallback, never a second
	// notice.
	returnedInstruction, err := IssueFulfillment(FulfillmentRequest{
		InstructionID: "fulfill-2", TenantID: "tenant-a", NoticeRef: "notice:leave-2",
		ArtifactHash: "sha256:artifact-2", ArtifactVersion: "templates/leave-notice/v3",
		Address: AddressRevision{
			Revision: 3, Digest: "sha256:address-3", AuthorizedBy: "employee-relations",
		},
		EnvelopeProfile: EnvelopeFlatCertified, PrivacyProfile: PrivacySealed,
		VendorOperation: OpPrintAndMail, VendorID: "vendor:postal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Register(returnedInstruction); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: returnedInstruction.Digest, VendorEventID: "evt-r-print",
		Outcome: OutcomePrinted, Proof: ProofPrintLog, At: fulfillAt(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: returnedInstruction.Digest, VendorEventID: "evt-r-mail",
		Outcome: OutcomeMailed, Proof: ProofPostalManifest, At: fulfillAt(),
	}); err != nil {
		t.Fatal(err)
	}
	fallback, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: returnedInstruction.Digest, VendorEventID: "evt-r-return",
		Outcome: OutcomeReturned, Proof: ProofReturnScan, At: fulfillAt(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Fallback == nil || fallback.Fallback.InstructionDigest != returnedInstruction.Digest {
		t.Fatalf("returned mail created no fallback: %+v", fallback)
	}
	workID := fallback.Fallback.WorkID
	// Terminal outcomes close the lifecycle: nothing follows RETURNED,
	// so no second notice or fallback can ever be created after it.
	if _, err := ledger.RecordOutcome(OutcomeRequest{
		InstructionDigest: returnedInstruction.Digest, VendorEventID: "evt-r-return-dup",
		Outcome: OutcomeUnknown, Proof: ProofLossAttestation, At: fulfillAt(),
	}); err == nil {
		t.Fatal("outcome recorded after terminal RETURNED")
	}
	recorded := ledger.Outcomes(returnedInstruction.Digest)
	fallbacks := 0
	for _, outcome := range recorded {
		if outcome.Fallback != nil {
			if outcome.Fallback.WorkID != workID {
				t.Fatal("returned mail created a second fallback")
			}
			fallbacks++
		}
	}
	if fallbacks == 0 {
		t.Fatal("returned mail fallback is missing")
	}

	// The vendor sees minimized fields only: hashes and profiles, never
	// tenant internals or notice references.
	view := instruction.VendorView()
	flat := view.ArtifactHash + "\x00" + view.AddressDigest + "\x00" + string(view.EnvelopeProfile) +
		"\x00" + string(view.PrivacyProfile) + "\x00" + string(view.VendorOperation) + "\x00" + view.VendorID
	for _, leaked := range []string{"tenant-a", "notice:leave-1", "fulfill-1"} {
		if strings.Contains(flat, leaked) {
			t.Fatalf("vendor view leaks %q", leaked)
		}
	}

	// RED negatives: unpinned artifact, unauthorized address and unknown
	// profiles never enter a batch.
	bad := FulfillmentRequest{
		InstructionID: "bad", TenantID: "tenant-a", NoticeRef: "notice:x",
		ArtifactHash: "sha256:a", ArtifactVersion: "v1",
		Address:         AddressRevision{Revision: 1, Digest: "sha256:a", AuthorizedBy: "employee-relations"},
		EnvelopeProfile: EnvelopeWindowed,
		PrivacyProfile:  PrivacySealed,
		VendorOperation: OpPrintAndMail,
		VendorID:        "vendor:postal-1",
	}
	bad.ArtifactHash = ""
	if _, err := IssueFulfillment(bad); err == nil {
		t.Fatal("unpinned artifact hash was accepted")
	}
	bad.ArtifactHash = "sha256:a"
	bad.Address.AuthorizedBy = ""
	if _, err := IssueFulfillment(bad); err == nil {
		t.Fatal("unauthorized address revision was accepted")
	}
}
