package esign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/documents/evidence"
)

func ceremonyFixture(t *testing.T) Ceremony {
	t.Helper()
	ceremony, err := Request("cer-1", "sha256:artifact", "signer:s1", 100, 200)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	return ceremony
}

func signedCeremony(t *testing.T) Ceremony {
	t.Helper()
	ceremony := ceremonyFixture(t)
	var err error
	ceremony, err = ceremony.Deliver("provider:ok", "cb-deliver", 110)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	ceremony, err = ceremony.Acknowledge("sha256:artifact", 120)
	if err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	ceremony, err = ceremony.Sign(SignerProofFor("signer:s1", "sha256:artifact"), "sha256:artifact", "cb-sign", 130)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return ceremony
}

func TestTodo_DOC_SIGN_001(t *testing.T) {
	ceremony := signedCeremony(t)
	if ceremony.State != StateSigned {
		t.Fatalf("state = %q", ceremony.State)
	}
	if ceremony.SignerProof == "" || ceremony.ProviderReceipt == "" || len(ceremony.CeremonyProof) != 4 {
		t.Fatalf("ceremony=%+v", ceremony)
	}
	if err := ceremony.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: every hostile path refuses to report signed.
	substituted := ceremonyFixture(t)
	substituted, _ = substituted.Deliver("provider:ok", "cb-1", 110)
	substituted, _ = substituted.Acknowledge("sha256:artifact", 120)
	forged, err := substituted.Sign(SignerProofFor("signer:mallory", "sha256:artifact"), "sha256:artifact", "cb-2", 130)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if forged.State != StateFailed || forged.State == StateSigned {
		t.Fatalf("substituted signer state = %q", forged.State)
	}
	fresh, _ := ceremonyFixture(t).Deliver("provider:ok", "cb-fresh", 110)
	stale, err := fresh.Acknowledge("sha256:other", 120)
	if err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if stale.State != StateFailed {
		t.Fatalf("stale artifact state = %q", stale.State)
	}
	expired, err := substituted.Sign(SignerProofFor("signer:s1", "sha256:artifact"), "sha256:artifact", "cb-2", 999)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if expired.State != StateExpired {
		t.Fatalf("expired request state = %q", expired.State)
	}
	replayed, err := substituted.Sign(SignerProofFor("signer:s1", "sha256:artifact"), "sha256:artifact", "cb-1", 130)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if replayed.State != StateAmbiguous {
		t.Fatalf("replayed callback state = %q", replayed.State)
	}
	// Provider acceptance alone never reports signed.
	notify, err := ceremonyFixture(t).ProviderNotify("provider:ok", "cb-early")
	if err != nil {
		t.Fatalf("ProviderNotify: %v", err)
	}
	if notify.State == StateSigned {
		t.Fatal("provider acceptance reported signed")
	}
	if _, err := notify.Sign(SignerProofFor("signer:s1", "sha256:artifact"), "sha256:artifact", "cb-sign", 130); err == nil {
		t.Fatal("unsigned path reached signature")
	}
}

func TestTodo_DOC_SIGN_001_Golden(t *testing.T) {
	ceremony := signedCeremony(t)
	lines := []string{
		"ceremony=" + ceremony.ID,
		"state=" + ceremony.State,
		"artifact=" + ceremony.ArtifactHash,
		"signer=" + ceremony.SignerID,
		"receipt=" + ceremony.ProviderReceipt,
		"journal=" + strings.Join(ceremony.Journal, "|"),
		"digest=" + ceremony.Digest,
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "doc_sign001_ceremony.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_DOC_SIGN_001_Integration(t *testing.T) {
	ceremony := signedCeremony(t)
	pkg := evidence.Package{
		Issuer: "hr-signing", Freshness: "2026-09-10", Signature: ceremony.SignerProof,
		Classification: "CONFIDENTIAL_HR", Purpose: "leave-start", Retention: "7y",
		Hold: "none", Method: "e-signature", Schema: "esign/v1", Rule: "consent/v1",
		Redaction: "none", VerifierAuthority: "hr-compliance",
	}
	for i, item := range ceremony.EvidenceItems() {
		pkg.Items = append(pkg.Items, evidence.EvidenceItem{ID: item.ID, Kind: item.Kind, Digest: item.Digest, Sequence: i})
	}
	pkg.Digest = pkg.ContentDigest()
	verification := pkg.Verify()
	if verification.Status != evidence.Complete {
		t.Fatalf("verification=%+v", verification)
	}
	if len(pkg.Items) != 4 {
		t.Fatalf("items = %d, want artifact, ceremony, signer and receipt", len(pkg.Items))
	}
}

func TestTodo_DOC_SIGN_001_Fault(t *testing.T) {
	if _, err := Request("", "sha256:a", "signer:s1", 100, 200); err == nil {
		t.Fatal("hollow request opened")
	}
	if _, err := Request("cer-x", "sha256:a", "signer:s1", 200, 100); err == nil {
		t.Fatal("inverted expiry opened")
	}
	ceremony := ceremonyFixture(t)
	if _, err := ceremony.Acknowledge("sha256:artifact", 110); err == nil {
		t.Fatal("acknowledgement skipped delivery")
	}
	declined, err := ceremony.Deliver("provider:ok", "cb-1", 110)
	if err != nil {
		t.Fatal(err)
	}
	declined, err = declined.Decline("changed mind", 115)
	if err != nil || declined.State != StateDeclined {
		t.Fatalf("declined=%+v err=%v", declined, err)
	}
	if _, err := declined.Sign(SignerProofFor("signer:s1", "sha256:artifact"), "sha256:artifact", "cb-2", 120); err == nil {
		t.Fatal("declined ceremony signed")
	}
	expired := ceremonyFixture(t).Expire(999)
	if expired.State != StateExpired {
		t.Fatalf("expired=%+v", expired)
	}
}

func TestTodo_DOC_SIGN_001_Recovery(t *testing.T) {
	requested := ceremonyFixture(t)
	delivered, err := requested.Deliver("provider:ok", "cb-deliver", 110)
	if err != nil {
		t.Fatal(err)
	}
	acknowledged, err := delivered.Acknowledge("sha256:artifact", 120)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := acknowledged.Sign(SignerProofFor("signer:s1", "sha256:artifact"), "sha256:artifact", "cb-sign", 130)
	if err != nil {
		t.Fatal(err)
	}
	// Crash after acknowledgement: replay recovers exactly.
	recovered, err := Replay([]Ceremony{requested, delivered, acknowledged})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if recovered.State != StateAcknowledged || recovered.Digest != acknowledged.Digest {
		t.Fatalf("recovered=%+v", recovered)
	}
	// Full replay reaches the signed state with no duplication.
	complete, err := Replay([]Ceremony{requested, delivered, acknowledged, signed})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if complete.State != StateSigned || len(complete.Journal) != len(signed.Journal) {
		t.Fatalf("complete=%+v", complete)
	}
	// Duplicated events and invented states refuse.
	if _, err := Replay([]Ceremony{requested, delivered, delivered}); err == nil {
		t.Fatal("duplicated event replayed")
	}
	if _, err := Replay([]Ceremony{requested, signed}); err == nil {
		t.Fatal("journal gap replayed")
	}
	if _, err := Replay(nil); err == nil {
		t.Fatal("empty replay recovered")
	}
	forged := signed
	forged.Journal = append(append([]string(nil), signed.Journal...), "t99:SIGNED:forged")
	if _, err := Replay([]Ceremony{requested, delivered, acknowledged, forged}); err == nil {
		t.Fatal("forged journal replayed")
	}
}
