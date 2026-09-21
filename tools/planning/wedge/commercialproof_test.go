package wedge

import (
	"os"
	"path/filepath"
	"testing"
)

func rev002Proofs() []PartnerProof {
	return []PartnerProof{
		{
			ManifestDigest: "9f2c4a6b8d1e3f5a7b9c0d2e4f6a8b1c3d5e7f9a1b3c5d7e9f1a3b5c7d9e1f3a5",
			KeyID:          "acme-design-partner-key-1",
			Gate:           "GATE_A",
			Decision:       "PROCEED",
			ProblemClass:   "promotion_compensation_cross_system_gap",
			SignatureValid: true,
		},
		{
			ManifestDigest: "1a3b5c7d9e1f3a59f2c4a6b8d1e3f5a7b9c0d2e4f6a8b1c3d5e7f9a1b3c5d7e9f",
			KeyID:          "globex-design-partner-key-7",
			Gate:           "GATE_B",
			Decision:       "PROCEED_LIMITED",
			ProblemClass:   "promotion_compensation_cross_system_gap",
			SignatureValid: true,
		},
	}
}

// TestTodo_REV_002_02 is the REV-002-02 primary: two distinct independently
// signed PROCEED decisions sharing one problem class yield MARKET_PROOF; a
// single partner yields SINGLE_CUSTOMER_ONLY; a duplicated manifest, a
// mismatched class, a non-proceed decision or a bad signature never yields
// market proof.
func TestTodo_REV_002_02(t *testing.T) {
	verdict, reasons := DecideCommercialProof(rev002Proofs())
	if verdict != ProofMarketProof {
		t.Errorf("two-partner proof = %q (%v), want MARKET_PROOF", verdict, reasons)
	}

	verdict, _ = DecideCommercialProof(rev002Proofs()[:1])
	if verdict != ProofSingleCustomer {
		t.Errorf("single-partner proof = %q, want SINGLE_CUSTOMER_ONLY", verdict)
	}

	dup := rev002Proofs()
	dup[1] = dup[0]
	if verdict, _ := DecideCommercialProof(dup); verdict == ProofMarketProof {
		t.Error("duplicated manifest accepted as market proof")
	}

	mixed := rev002Proofs()
	mixed[1].ProblemClass = "unrelated_problem_class"
	if verdict, _ := DecideCommercialProof(mixed); verdict == ProofMarketProof {
		t.Error("mismatched problem classes accepted as market proof")
	}

	blocked := rev002Proofs()
	blocked[1].Decision = "REMEDIATE"
	if verdict, _ := DecideCommercialProof(blocked); verdict == ProofMarketProof {
		t.Error("non-proceed decision accepted as market proof")
	}

	forged := rev002Proofs()
	forged[0].SignatureValid = false
	if verdict, reasons := DecideCommercialProof(forged); verdict == ProofMarketProof {
		t.Errorf("bad signature accepted as market proof (%v)", reasons)
	}
}

// TestTodo_REV_002_02_Golden pins the exact verdict bytes for the canonical
// two-partner fixture.
func TestTodo_REV_002_02_Golden(t *testing.T) {
	verdict, reasons := DecideCommercialProof(rev002Proofs())
	got := RenderProofVerdict(verdict, reasons)
	want, err := os.ReadFile(filepath.Join("testdata", "rev00202.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch:\n got: %q\nwant: %q", got, string(want))
	}
}
