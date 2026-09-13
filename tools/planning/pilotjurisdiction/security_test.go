package pilotjurisdiction

import "testing"

// TestTodo_SELECT_001_Security is SELECT-001's SECURITY test, the same shape
// as tools/planning/scopeceiling's TestTodo_PHASE_001_Security: it proves
// the real, checked-in, signed profile verifies, then proves that tampering
// any single signed field - including, specifically, quietly filling in a
// reviewer name or quietly relaxing an exclusion - invalidates the
// signature.
func TestTodo_SELECT_001_Security(t *testing.T) {
	original := mustLoadProfile(t)

	ok, err := VerifyProfileSignature(original)
	if err != nil {
		t.Fatalf("VerifyProfileSignature(original): %v", err)
	}
	if !ok {
		t.Fatal("the checked-in select-001-jurisdiction-profile.yaml must verify against its own signature")
	}

	tamperCases := []struct {
		name   string
		tamper func(*JurisdictionProfile)
	}{
		{"a reviewer name is quietly filled in", func(p *JurisdictionProfile) {
			p.Reviewer.Name = "Jane Doe, Esq."
			p.Reviewer.Qualification = "California-barred employment counsel"
		}},
		{"review status is quietly promoted", func(p *JurisdictionProfile) { p.ReviewStatus = "COUNSEL_APPROVED" }},
		{"a jurisdiction exclusion is quietly dropped", func(p *JurisdictionProfile) {
			var kept []Exclusion
			for _, e := range p.Exclusions {
				if e.Kind != ExclusionKindJurisdiction {
					kept = append(kept, e)
				}
			}
			p.Exclusions = kept
		}},
		{"an obligation mapping is quietly added for an excluded kind", func(p *JurisdictionProfile) {
			p.ObligationMappings = append(p.ObligationMappings, ObligationMapping{
				Kind: "JOB_SECURITY", IntentID: pilotIntentID, EvidencePath: "legal.AppliedObligation.Bindings",
			})
		}},
		{"the out-of-scope uncertainty status is quietly relaxed", func(p *JurisdictionProfile) {
			p.Uncertainty.OutOfScopeStatus = "PROCEED_ANYWAY"
		}},
		{"the effective window is quietly moved earlier", func(p *JurisdictionProfile) { p.Window.EffectiveStart = "2020-01-01" }},
		{"a stop/reselect threshold action is quietly softened", func(p *JurisdictionProfile) {
			if len(p.StopReselectThresholds) > 0 {
				p.StopReselectThresholds[0].Action = ActionProceed
			}
		}},
	}

	for _, tc := range tamperCases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := deepCopy(original)
			tc.tamper(&tampered)

			ok, err := VerifyProfileSignature(tampered)
			if err != nil {
				t.Fatalf("VerifyProfileSignature(tampered): %v", err)
			}
			if ok {
				t.Fatalf("tampered profile (%s) must not verify against the original signature", tc.name)
			}
		})
	}
}
