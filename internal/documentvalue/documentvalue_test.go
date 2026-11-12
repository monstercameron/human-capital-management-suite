package documentvalue

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/esign"
)

// acknowledgedSignature drives a real ceremony to ACKNOWLEDGED so the tests
// exercise the sealed value, not a hand-built struct.
func acknowledgedSignature(t *testing.T, level AssuranceLevel) Signature {
	t.Helper()
	ceremony, err := esign.Request("ack-1", "sha256:offer", "worker-1", 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	ceremony, err = ceremony.Deliver("rcpt-1", "cb-1", 110)
	if err != nil {
		t.Fatal(err)
	}
	ceremony, err = ceremony.Acknowledge("sha256:offer", 120)
	if err != nil {
		t.Fatal(err)
	}
	return Signature{Ceremony: ceremony, AssuranceLevel: level}
}

func TestAssuranceLevelVocabularyIsClosed(t *testing.T) {
	for _, level := range []AssuranceLevel{AssuranceBasic, AssuranceEnhanced, AssuranceQualified} {
		if !level.Valid() {
			t.Errorf("%q should be valid", level)
		}
	}
	for _, level := range []AssuranceLevel{"", "basic", "NONE", "ENHANCED "} {
		if level.Valid() {
			t.Errorf("%q should be refused", level)
		}
	}
}

func TestSignatureSignCompletesCeremonyAndKeepsAssurance(t *testing.T) {
	sig := acknowledgedSignature(t, AssuranceEnhanced)
	proof := esign.SignerProofFor("worker-1", "sha256:offer")

	signed, err := sig.Sign(proof, "sha256:offer", "cb-2", 130)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed.Ceremony.State != esign.StateSigned {
		t.Fatalf("state = %s, want SIGNED", signed.Ceremony.State)
	}
	if signed.AssuranceLevel != AssuranceEnhanced || signed.AssuranceLabel() != "ENHANCED" {
		t.Fatalf("assurance = %q (%q), want ENHANCED", signed.AssuranceLevel, signed.AssuranceLabel())
	}
	if err := signed.Validate(); err != nil {
		t.Fatalf("Validate on a signed ceremony: %v", err)
	}
}

func TestSignatureSignWrongSignerFailsClosedInsideTheCeremony(t *testing.T) {
	sig := acknowledgedSignature(t, AssuranceBasic)

	refused, err := sig.Sign(esign.SignerProofFor("someone-else", "sha256:offer"), "sha256:offer", "cb-2", 130)
	if err != nil {
		t.Fatalf("Sign returned an error instead of a failed ceremony: %v", err)
	}
	if refused.Ceremony.State != esign.StateFailed || refused.Ceremony.FailReason != "signer-substitution" {
		t.Fatalf("state = %s reason = %q, want FAILED signer-substitution", refused.Ceremony.State, refused.Ceremony.FailReason)
	}
	if err := refused.Validate(); err != nil {
		t.Fatalf("a failed but sealed ceremony still verifies: %v", err)
	}
}

func TestSignatureSignRequiresAcknowledgedCeremony(t *testing.T) {
	requested, err := esign.Request("ack-2", "sha256:offer", "worker-1", 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Signature{Ceremony: requested, AssuranceLevel: AssuranceBasic}.Sign(esign.SignerProofFor("worker-1", "sha256:offer"), "sha256:offer", "cb-9", 130)
	if err == nil {
		t.Fatal("signing a REQUESTED ceremony was accepted")
	}
}

func TestSignatureValidateFailsClosed(t *testing.T) {
	signed, err := acknowledgedSignature(t, AssuranceQualified).Sign(esign.SignerProofFor("worker-1", "sha256:offer"), "sha256:offer", "cb-2", 130)
	if err != nil {
		t.Fatal(err)
	}

	unknownLevel := signed
	unknownLevel.AssuranceLevel = "NONE"
	if err := unknownLevel.Validate(); !errors.Is(err, ErrAssuranceLevel) {
		t.Fatalf("unknown level: err = %v, want ErrAssuranceLevel", err)
	}

	anonymous := signed
	anonymous.Ceremony.ID = " "
	if err := anonymous.Validate(); !errors.Is(err, ErrAssuranceLevel) {
		t.Fatalf("blank ceremony id: err = %v, want ErrAssuranceLevel", err)
	}

	tampered := signed
	tampered.Ceremony.FailReason = "forged"
	if err := tampered.Validate(); err == nil || errors.Is(err, ErrAssuranceLevel) {
		t.Fatalf("tampered seal: err = %v, want a seal error", err)
	}

	if err := (Signature{}).Validate(); !errors.Is(err, ErrAssuranceLevel) {
		t.Fatalf("zero signature: err = %v, want ErrAssuranceLevel", err)
	}
}

// refusedEvidence stands in for the server's evidence reference: the seam
// passes its verdict through unchanged.
type refusedEvidence struct{ err error }

func (r refusedEvidence) Check() error { return r.err }

func TestAttachmentReferenceCarriesTheAdmissionSeam(t *testing.T) {
	sentinel := errors.New("revoked")
	ref := AttachmentReference{ID: "doc-1", ArtifactID: "art-1", DocumentType: "PDF", Evidence: refusedEvidence{err: sentinel}}

	if err := ref.Evidence.Check(); !errors.Is(err, sentinel) {
		t.Fatalf("Check = %v, want the seam's verdict", err)
	}
	var none AttachmentReference
	if none.Evidence != nil {
		t.Fatal("a zero reference must hold no admission, so a consumer cannot Check it by accident")
	}
}
