package pilotcommercial

import (
	"strings"
	"testing"
)

// TestTodo_COMMERCIAL_001_Mutation is COMMERCIAL-001's named MUTATION test.
// Each case mutates one field of a structurally valid freeze fixture and
// proves two things together, matching this repository's existing
// `_Mutation` convention (tools/planning/pilotprovider's
// TestTodo_SELECT_002_Mutation, tools/planning/pilotjurisdiction's
// TestTodo_SELECT_001_Mutation, tools/planning/scopeceiling's
// TestTodo_PHASE_001_Mutation): the resulting CanonicalDigest moves away
// from the original (the mutation is byte-detectable, not silently
// absorbed), and Validate names the exact condition the mutation broke (a
// finding is traceable, not merely "something is wrong").
func TestTodo_COMMERCIAL_001_Mutation(t *testing.T) {
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
		mutate  func(*PilotCommercialFreeze)
		wantHit string
	}{
		{
			name: "the jurisdiction promise is quietly promoted while its review status is silently erased",
			mutate: func(f *PilotCommercialFreeze) {
				f.Jurisdiction.Status = PromiseSelectedConfirmed
				f.Jurisdiction.ReviewStatus = ""
			},
			wantHit: "jurisdiction.review_status",
		},
		{
			name:    "the provider promise is quietly promoted without a vendor ref",
			mutate:  func(f *PilotCommercialFreeze) { f.Provider.Status = PromiseSelectedConfirmed },
			wantHit: "provider.vendor_ref",
		},
		{
			name:    "an SLO is quietly promised",
			mutate:  func(f *PilotCommercialFreeze) { f.Evidence.SLOStatus = "PROMISED" },
			wantHit: "evidence.slo_status",
		},
		{
			name:    "replay billing is quietly permitted",
			mutate:  func(f *PilotCommercialFreeze) { f.Billing.ReplayBillable = true },
			wantHit: "billing.replay_billable",
		},
		{
			name:    "repair billing is quietly permitted",
			mutate:  func(f *PilotCommercialFreeze) { f.Billing.RepairBillable = true },
			wantHit: "billing.repair_billable",
		},
		{
			name: "a domain is quietly claimed as both owned and observed",
			mutate: func(f *PilotCommercialFreeze) {
				f.Authority.WriteAuthority = true
				f.Authority.OwnedDomains = []string{"compensation"}
			},
			wantHit: "conflates overlay authority",
		},
		{
			name:    "the pricing hypothesis flag is silently cleared",
			mutate:  func(f *PilotCommercialFreeze) { f.Pricing.IsHypothesis = false },
			wantHit: "pricing.is_hypothesis",
		},
		{
			name:    "the stop threshold is silently softened to the reprice threshold",
			mutate:  func(f *PilotCommercialFreeze) { f.RepriceStop.StopThresholdPct = f.RepriceStop.RepriceThresholdPct },
			wantHit: "reprice_stop.stop_threshold_pct",
		},
		{
			name:    "the deletion certificate guarantee is silently dropped",
			mutate:  func(f *PilotCommercialFreeze) { f.Exit.DeletionCertificate = false },
			wantHit: "exit.deletion_certificate",
		},
		{
			name:    "the dpa reference is silently erased",
			mutate:  func(f *PilotCommercialFreeze) { f.DataProcessing.DPAReference = "" },
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
