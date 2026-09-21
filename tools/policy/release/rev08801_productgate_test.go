package release

// REV-088-01: the ALIGN-064 product-slice release gate decides the real
// release bundle. Build collects the eight gate digests as required inputs
// and refuses a bundle missing one; the admission decision is pinned in the
// manifest and VerifyBundle re-checks it.
//
// RED: conformance.DefaultReleaseGate had zero non-test callers and
// RequiredPolicyReports listed only driftgate, apigate, substratecoverage
// and cleancheckout, so no bundle ever reflected a ReleaseGate.Admit denial.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
)

func rev08801GateEvidence() map[string]string {
	out := make(map[string]string, len(ProductGateGates()))
	for _, name := range ProductGateGates() {
		out[name] = "rev08801-digest-" + name
	}
	return out
}

func TestTodo_REV_088_01(t *testing.T) {
	root := releaseRepoRoot(t)

	t.Run("MissingEvidenceRefused", func(t *testing.T) {
		fixture := releaseFixture(t, root)
		opts := fixture.options(filepath.Join(t.TempDir(), "bundle"))
		opts.ProductGateEvidence = nil
		if _, err := Build(root, opts); !errors.Is(err, ErrProductGateEvidence) {
			t.Fatalf("Build without gate evidence = %v, want ErrProductGateEvidence", err)
		} else if !errors.Is(err, conformance.ErrReleaseDenied) {
			t.Fatalf("Build without gate evidence = %v, want the gate denial underneath", err)
		}
	})

	t.Run("MissingGateDeniedByName", func(t *testing.T) {
		fixture := releaseFixture(t, root)
		opts := fixture.options(filepath.Join(t.TempDir(), "bundle"))
		evidence := rev08801GateEvidence()
		delete(evidence, "usability.zero_override")
		opts.ProductGateEvidence = evidence
		_, err := Build(root, opts)
		if err == nil || !strings.Contains(err.Error(), "usability.zero_override") {
			t.Fatalf("Build missing one gate = %v, want a denial naming usability.zero_override", err)
		}
	})

	t.Run("AdmittedDecisionPinned", func(t *testing.T) {
		fixture := releaseFixture(t, root)
		out := filepath.Join(t.TempDir(), "bundle")
		opts := fixture.options(out)
		opts.ProductGateEvidence = rev08801GateEvidence()
		manifest, err := Build(root, opts)
		if err != nil {
			t.Fatalf("Build with full gate evidence: %v", err)
		}
		if manifest.ProductGate == nil {
			t.Fatal("built manifest carries no product-gate record")
		}
		record := manifest.ProductGate
		if len(record.Required) != 8 || len(record.Evidence) != 8 {
			t.Fatalf("record holds %d required and %d evidence, want 8 and 8", len(record.Required), len(record.Evidence))
		}
		for i, want := range ProductGateGates() {
			if record.Required[i] != want {
				t.Fatalf("record gate %d = %q, want %q", i, record.Required[i], want)
			}
		}
		// The pinned decision is exactly the gate's own verdict for the
		// recorded evidence: a bundle cannot claim an admission the gate
		// would not compute.
		decision, err := conformance.DefaultReleaseGate().Admit(record.Evidence)
		if err != nil || !decision.Admitted {
			t.Fatalf("recorded evidence admits = %v, err = %v", decision.Admitted, err)
		}
		if decision.Digest != record.Decision {
			t.Fatalf("pinned decision %q != gate verdict %q", record.Decision, decision.Digest)
		}
		// The gate report ships as a manifest-covered bundle artifact.
		raw, err := os.ReadFile(filepath.Join(out, ProductGateFileName))
		if err != nil {
			t.Fatalf("read bundled gate report: %v", err)
		}
		want, err := MarshalProductGate(*record)
		if err != nil {
			t.Fatalf("marshal gate record: %v", err)
		}
		if string(raw) != string(want) {
			t.Fatalf("bundled gate report differs from the pinned record")
		}
	})

	t.Run("RecordRecheck", func(t *testing.T) {
		if err := VerifyProductGateRecord(nil); !errors.Is(err, ErrProductGateDecision) {
			t.Fatalf("nil record = %v, want ErrProductGateDecision", err)
		}
		decision, evidence, err := AdmitProductGate(rev08801GateEvidence())
		if err != nil {
			t.Fatalf("admit fixture evidence: %v", err)
		}
		good := SealProductGate(decision, evidence)
		if err := VerifyProductGateRecord(&good); err != nil {
			t.Fatalf("sealed record: %v", err)
		}
		tampered := good
		tampered.Decision = "sha256:" + strings.Repeat("0", 64)
		if err := VerifyProductGateRecord(&tampered); !errors.Is(err, ErrProductGateDecision) {
			t.Fatalf("tampered decision = %v, want ErrProductGateDecision", err)
		}
		narrowed := good
		narrowed.Required = narrowed.Required[:7]
		if err := VerifyProductGateRecord(&narrowed); !errors.Is(err, ErrProductGateDecision) {
			t.Fatalf("narrowed gate set = %v, want ErrProductGateDecision", err)
		}
	})
}

// TestTodo_REV_088_01_Integration proves a bundle reflects a real
// ReleaseGate.Admit denial end to end: a fully-evidenced bundle verifies, a
// bundle whose gate report is altered or removed does not.
func TestTodo_REV_088_01_Integration(t *testing.T) {
	root := releaseRepoRoot(t)
	fixture := releaseFixture(t, root)
	out := filepath.Join(t.TempDir(), "bundle")
	opts := fixture.options(out)
	opts.ProductGateEvidence = rev08801GateEvidence()
	manifest, err := Build(root, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	receipt, err := VerifyBundle(out, VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyBundle: %v", err)
	}
	digest, err := manifest.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if receipt.ManifestDigest != digest {
		t.Fatalf("verified digest = %s, built digest = %s", receipt.ManifestDigest, digest)
	}
	// Altering the gate report breaks the bundle's integrity check by name.
	gatePath := filepath.Join(out, ProductGateFileName)
	original, err := os.ReadFile(gatePath)
	if err != nil {
		t.Fatalf("read gate report: %v", err)
	}
	if err := os.WriteFile(gatePath, []byte("altered\n"), 0o644); err != nil {
		t.Fatalf("alter gate report: %v", err)
	}
	if _, err := VerifyBundle(out, VerifyOptions{}); err == nil || !strings.Contains(err.Error(), ProductGateFileName) {
		t.Fatalf("altered gate report error = %v, want named %s", err, ProductGateFileName)
	}
	if err := os.WriteFile(gatePath, original, 0o644); err != nil {
		t.Fatalf("restore gate report: %v", err)
	}
	if _, err := VerifyBundle(out, VerifyOptions{}); err != nil {
		t.Fatalf("VerifyBundle after restore: %v", err)
	}
	// Removing the gate report breaks the required-artifact check by name.
	if err := os.Remove(gatePath); err != nil {
		t.Fatalf("remove gate report: %v", err)
	}
	if _, err := VerifyBundle(out, VerifyOptions{}); err == nil || !strings.Contains(err.Error(), ProductGateFileName) {
		t.Fatalf("removed gate report error = %v, want named %s", err, ProductGateFileName)
	}
}

// TestTodo_REV_088_01_Conformance pins the eight gates every bundle must
// prove: the bundle contract cannot silently narrow the ALIGN-064 gate.
func TestTodo_REV_088_01_Conformance(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "rev08801_gates.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	want := strings.TrimSpace(string(raw))
	got := strings.Join(ProductGateGates(), "\n")
	if got != want {
		t.Fatalf("product-gate set drifted:\n got: %q\nwant: %q", got, want)
	}
}
