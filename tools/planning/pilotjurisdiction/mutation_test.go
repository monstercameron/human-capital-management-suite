package pilotjurisdiction

import (
	"strings"
	"testing"
)

// TestTodo_SELECT_001_Mutation is SELECT-001's named MUTATION test. Each
// case mutates one field of a structurally valid profile fixture and proves
// two things together, matching this repository's existing `_Mutation`
// convention (e.g. tools/planning/scopeceiling's TestTodo_PHASE_001_Mutation):
// the resulting CanonicalDigest moves away from the original (the mutation
// is byte-detectable, not silently absorbed), and Validate names the exact
// condition the mutation broke (a finding is traceable, not merely
// "something is wrong").
func TestTodo_SELECT_001_Mutation(t *testing.T) {
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
		mutate  func(*JurisdictionProfile)
		wantHit string
	}{
		{
			name:    "the reviewer name is silently erased",
			mutate:  func(p *JurisdictionProfile) { p.Reviewer.Name = "" },
			wantHit: "reviewer.name: missing",
		},
		{
			name: "an obligation mapping's evidence path is silently erased",
			mutate: func(p *JurisdictionProfile) {
				p.ObligationMappings[0].EvidencePath = ""
			},
			wantHit: "obligation_mappings[0].evidence_path: missing",
		},
		{
			name: "an excluded obligation kind is quietly re-added as a mapping without dropping its exclusion",
			mutate: func(p *JurisdictionProfile) {
				var excludedKind string
				for _, e := range p.Exclusions {
					if e.Kind == ExclusionKindObligation {
						excludedKind = e.Value
						break
					}
				}
				p.ObligationMappings = append(p.ObligationMappings, ObligationMapping{
					Kind: excludedKind, IntentID: pilotIntentID, EvidencePath: "legal.AppliedObligation.Bindings",
				})
			},
			wantHit: "declared both mapped",
		},
		{
			name: "the jurisdiction exclusion is dropped",
			mutate: func(p *JurisdictionProfile) {
				var kept []Exclusion
				for _, e := range p.Exclusions {
					if e.Kind != ExclusionKindJurisdiction {
						kept = append(kept, e)
					}
				}
				p.Exclusions = kept
			},
			wantHit: "JURISDICTION exclusion",
		},
		{
			name: "the legal-advice disclaimer is replaced with reassuring prose",
			mutate: func(p *JurisdictionProfile) {
				p.LegalAdviceDisclaimer = "This profile is comprehensive and trustworthy."
			},
			wantHit: "legal_advice_disclaimer",
		},
		{
			name:    "the uncertainty policy is quietly relaxed to a made-up status",
			mutate:  func(p *JurisdictionProfile) { p.Uncertainty.OutOfScopeStatus = "BEST_GUESS" },
			wantHit: "uncertainty.out_of_scope_status",
		},
		{
			name:    "the stop/reselect thresholds are removed",
			mutate:  func(p *JurisdictionProfile) { p.StopReselectThresholds = nil },
			wantHit: "stop_reselect_thresholds",
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
