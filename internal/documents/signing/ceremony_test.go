package signing

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/esign"
)

// The shim must drive the leaf's real lifecycle: a ceremony opened, delivered,
// acknowledged and signed through these names replays to the same seal.
func TestShimDrivesTheLeafLifecycle(t *testing.T) {
	requested, err := Request("ack-1", "sha256:offer", "worker-1", 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := requested.Deliver("rcpt-1", "cb-1", 110)
	if err != nil {
		t.Fatal(err)
	}
	acknowledged, err := delivered.Acknowledge("sha256:offer", 120)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := acknowledged.Sign(SignerProofFor("worker-1", "sha256:offer"), "sha256:offer", "cb-2", 130)
	if err != nil {
		t.Fatal(err)
	}
	if StateSigned != esign.StateSigned || signed.State != esign.StateSigned {
		t.Fatalf("state = %q, want SIGNED from both paths", signed.State)
	}
	if signed.Verify() != nil {
		t.Fatal("signed ceremony does not verify")
	}

	replayed, err := Replay([]Ceremony{requested, delivered, acknowledged, signed})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if replayed.Digest != signed.Digest {
		t.Fatalf("replayed digest %q != signed digest %q", replayed.Digest, signed.Digest)
	}
}

func TestShimProofRefusesSubstitutionAsFailure(t *testing.T) {
	if got, want := SignerProofFor("worker-1", "sha256:offer"), esign.SignerProofFor("worker-1", "sha256:offer"); got != want {
		t.Fatalf("SignerProofFor = %q, want %q", got, want)
	}
	requested, err := Request("ack-3", "sha256:offer", "worker-1", 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := requested.Deliver("rcpt-3", "cb-3", 110)
	if err != nil {
		t.Fatal(err)
	}
	acknowledged, err := delivered.Acknowledge("sha256:offer", 120)
	if err != nil {
		t.Fatal(err)
	}
	refused, err := acknowledged.Sign(SignerProofFor("someone-else", "sha256:offer"), "sha256:offer", "cb-4", 130)
	if err != nil {
		t.Fatal(err)
	}
	if refused.State != StateFailed || refused.FailReason != "signer-substitution" {
		t.Fatalf("state = %q reason = %q, want FAILED signer-substitution", refused.State, refused.FailReason)
	}
}
