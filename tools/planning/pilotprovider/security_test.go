package pilotprovider

import "testing"

// TestTodo_SELECT_002_Security is SELECT-002's SECURITY test, the same
// shape as tools/planning/pilotjurisdiction's TestTodo_SELECT_001_Security
// and tools/planning/scopeceiling's TestTodo_PHASE_001_Security: it proves
// the real, checked-in, signed topology verifies, then proves that
// tampering any single signed field - including, specifically, quietly
// promoting the selection status or quietly relaxing a fault/exit/quota
// guarantee - invalidates the signature.
func TestTodo_SELECT_002_Security(t *testing.T) {
	original := mustLoadTopology(t)

	ok, err := VerifyTopologySignature(original)
	if err != nil {
		t.Fatalf("VerifyTopologySignature(original): %v", err)
	}
	if !ok {
		t.Fatal("the checked-in select-002-provider-topology.yaml must verify against its own signature")
	}

	tamperCases := []struct {
		name   string
		tamper func(*ProviderTopology)
	}{
		{"selection status is quietly promoted", func(p *ProviderTopology) { p.SelectionStatus = StatusVendorConfirmed }},
		{"the placeholder suffix is quietly stripped from the vendor id", func(p *ProviderTopology) {
			p.Provider.VendorID = "acme-hcm-production"
		}},
		{"a vendor confirmation contact is quietly filled in", func(p *ProviderTopology) {
			p.VendorConfirmation.ConfirmedByContact = "Jane Procurement"
		}},
		{"a fault class default action is quietly softened", func(p *ProviderTopology) {
			if len(p.Faults) > 0 {
				p.Faults[0].DefaultAction = "ignore and continue"
			}
		}},
		{"the ambiguous-outcome timeout policy is quietly relaxed", func(p *ProviderTopology) {
			p.Timeout.AmbiguousOutcomeAction = "RETRY_BLINDLY"
		}},
		{"a quota limit is quietly raised", func(p *ProviderTopology) { p.Quota.RequestLimit = 999999 }},
		{"the cross-system observation independence claim is quietly dropped", func(p *ProviderTopology) {
			p.CrossSystemObservation.ProvesIndependentOfWriteAcknowledgement = false
		}},
		{"a stop/reselect threshold action is quietly softened", func(p *ProviderTopology) {
			if len(p.StopReselectThresholds) > 0 {
				p.StopReselectThresholds[0].Action = ActionProceed
			}
		}},
		{"the exit plan's fallback activation step is quietly erased", func(p *ProviderTopology) { p.Exit.ActivateFallback = "" }},
	}

	for _, tc := range tamperCases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := deepCopy(original)
			tc.tamper(&tampered)

			ok, err := VerifyTopologySignature(tampered)
			if err != nil {
				t.Fatalf("VerifyTopologySignature(tampered): %v", err)
			}
			if ok {
				t.Fatalf("tampered topology (%s) must not verify against the original signature", tc.name)
			}
		})
	}
}
