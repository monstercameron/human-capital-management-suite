package pilotprovider

import (
	"strings"
	"testing"
)

// TestTodo_SELECT_002_Mutation is SELECT-002's named MUTATION test. Each
// case mutates one field of a structurally valid topology fixture and
// proves two things together, matching this repository's existing
// `_Mutation` convention (tools/planning/pilotjurisdiction's
// TestTodo_SELECT_001_Mutation, tools/planning/scopeceiling's
// TestTodo_PHASE_001_Mutation): the resulting CanonicalDigest moves away
// from the original (the mutation is byte-detectable, not silently
// absorbed), and Validate names the exact condition the mutation broke (a
// finding is traceable, not merely "something is wrong").
func TestTodo_SELECT_002_Mutation(t *testing.T) {
	original := validFixture()
	if v := original.Validate(); len(v) != 0 {
		t.Fatalf("fixture must start valid, got: %v", v)
	}
	originalDigest, err := original.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest(original): %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*ProviderTopology)
		wantHit string
	}{
		{
			name:    "the placeholder vendor id is quietly replaced with a normal-looking name",
			mutate:  func(p *ProviderTopology) { p.Provider.VendorID = "acme-hcm" },
			wantHit: "provider.vendor_id",
		},
		{
			name: "selection status is quietly promoted without any supporting confirmation",
			mutate: func(p *ProviderTopology) {
				p.SelectionStatus = StatusVendorConfirmed
			},
			wantHit: "provider.vendor_id",
		},
		{
			name:    "a fault class is silently dropped from the matrix",
			mutate:  func(p *ProviderTopology) { p.Faults = p.Faults[:len(p.Faults)-1] },
			wantHit: "missing required fault class",
		},
		{
			name: "an operation mapping's contract version is silently erased",
			mutate: func(p *ProviderTopology) {
				p.Operations[0].ContractVersion = ""
			},
			wantHit: "operations[0].contract_version",
		},
		{
			name:    "the cross-system observation is silently downgraded to trust the write acknowledgement",
			mutate:  func(p *ProviderTopology) { p.CrossSystemObservation.ProvesIndependentOfWriteAcknowledgement = false },
			wantHit: "proves_independent_of_write_acknowledgement",
		},
		{
			name:    "the timeout ambiguity policy is silently relaxed to a blind retry",
			mutate:  func(p *ProviderTopology) { p.Timeout.AmbiguousOutcomeAction = "RETRY_BLINDLY" },
			wantHit: "timeout.ambiguous_outcome_action",
		},
		{
			name:    "the exit plan's credential revocation step is silently erased",
			mutate:  func(p *ProviderTopology) { p.Exit.RevokeCredentials = "" },
			wantHit: "exit.revoke_credentials",
		},
		{
			name:    "the stop/reselect thresholds are removed",
			mutate:  func(p *ProviderTopology) { p.StopReselectThresholds = nil },
			wantHit: "stop_reselect_thresholds",
		},
		{
			name:    "the data-processing DPA reference is silently erased",
			mutate:  func(p *ProviderTopology) { p.DataProcessing.DPAReference = "" },
			wantHit: "data_processing.dpa_reference",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := deepCopy(original)
			tc.mutate(&mutated)

			violations := mutated.Validate()
			if len(violations) == 0 {
				t.Fatalf("mutation %q produced no violation - Validate silently absorbed it", tc.name)
			}
			found := false
			for _, v := range violations {
				if strings.Contains(v.String(), tc.wantHit) {
					found = true
				}
			}
			if !found {
				t.Errorf("mutation %q: expected a violation naming %q, got %v", tc.name, tc.wantHit, violations)
			}

			mutatedDigest, err := mutated.CanonicalDigest()
			if err != nil {
				t.Fatalf("CanonicalDigest(mutated): %v", err)
			}
			if mutatedDigest == originalDigest {
				t.Errorf("mutation %q: CanonicalDigest did not move - the mutation is not byte-detectable", tc.name)
			}
		})
	}
}

// TestTodo_SELECT_002_Mutation_RealSelectionFlipIsByteDetectableAndSatisfiesTheGate
// is the compensating case: unlike every mutation above, swapping in a
// complete, real vendor selection (real id, VENDOR_CONFIRMED status, full
// vendor_confirmation) is byte-detectable (digest moves) but produces ZERO
// Validate violations and DOES satisfy SatisfiesRealProviderSelectionGate -
// proving the schema supports a real selection as a pure data change, not
// just proving every deviation from the placeholder is an error.
func TestTodo_SELECT_002_Mutation_RealSelectionFlipIsByteDetectableAndSatisfiesTheGate(t *testing.T) {
	original := validFixture()
	originalDigest, err := original.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest(original): %v", err)
	}

	realSelection := deepCopy(original)
	realSelection.SelectionStatus = StatusVendorConfirmed
	realSelection.Provider.VendorID = "acme-hcm-production"
	realSelection.VendorConfirmation = VendorConfirmation{
		ConfirmedByContact:  "Jane Procurement",
		ContractDocumentRef: "contracts/acme-hcm-msa-2027.pdf",
		SignedEffectiveDate: "2027-01-01",
	}

	if v := realSelection.Validate(); len(v) != 0 {
		t.Fatalf("a complete real-vendor swap must validate clean, got: %v", v)
	}
	ok, violations := realSelection.SatisfiesRealProviderSelectionGate()
	if !ok {
		t.Fatalf("a complete real-vendor swap must satisfy the real selection gate, got violations: %v", violations)
	}

	newDigest, err := realSelection.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest(realSelection): %v", err)
	}
	if newDigest == originalDigest {
		t.Fatal("swapping in a real vendor selection did not move the canonical digest")
	}
}
