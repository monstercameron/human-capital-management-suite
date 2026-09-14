package scopeceiling

import "testing"

// TestTodo_PHASE_001_Security is PHASE-001's SECURITY test, the same shape
// as tools/planning/gateevidence's TestTamperedP1AManifestFailsSignatureVerification:
// it proves the real, checked-in, signed ceiling verifies, then proves that
// tampering any single signed field - including, specifically, filling one
// of the four selection slots this manifest must always leave empty -
// invalidates the signature.
func TestTodo_PHASE_001_Security(t *testing.T) {
	original := mustLoadCeiling(t)

	ok, err := VerifyManifestSignature(original)
	if err != nil {
		t.Fatalf("VerifyManifestSignature(original): %v", err)
	}
	if !ok {
		t.Fatal("the checked-in phase1-scope-ceiling.yaml must verify against its own signature")
	}

	tamperCases := []struct {
		name   string
		tamper func(*ScopeCeilingManifest)
	}{
		{"a provider selection slot is quietly filled", func(m *ScopeCeilingManifest) {
			for i := range m.SelectionSlots {
				if m.SelectionSlots[i].Name == "provider" {
					m.SelectionSlots[i].Filled = true
					m.SelectionSlots[i].Value = "Workday HCM, edition Enterprise"
				}
			}
		}},
		{"an intent disposition is silently promoted", func(m *ScopeCeilingManifest) {
			for i := range m.Intents {
				if m.Intents[i].ID == "hcmnext.people.change_manager/v1" {
					m.Intents[i].Disposition = Include
				}
			}
		}},
		{"a rejected effect is silently included", func(m *ScopeCeilingManifest) {
			for i := range m.Effects {
				if m.Effects[i].Class == IrreversibleExternal {
					m.Effects[i].Disposition = Include
				}
			}
		}},
		{"an endpoint disposition is removed", func(m *ScopeCeilingManifest) {
			if len(m.Endpoints) > 0 {
				m.Endpoints[0].EndpointDisposition = ""
			}
		}},
		{"freshness window loosened", func(m *ScopeCeilingManifest) { m.FreshnessWindowDays = 3650 }},
		{"a deferred domain is dropped", func(m *ScopeCeilingManifest) {
			if len(m.DeferredDomains) > 0 {
				m.DeferredDomains = m.DeferredDomains[1:]
			}
		}},
	}

	for _, tc := range tamperCases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := deepCopy(original)
			tc.tamper(&tampered)

			ok, err := VerifyManifestSignature(tampered)
			if err != nil {
				t.Fatalf("VerifyManifestSignature(tampered): %v", err)
			}
			if ok {
				t.Fatalf("tampered manifest (%s) must not verify against the original signature", tc.name)
			}
		})
	}
}
