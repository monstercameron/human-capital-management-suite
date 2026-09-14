package pilotcommercial

import "testing"

// TestTodo_COMMERCIAL_001_Security is COMMERCIAL-001's SECURITY test, the
// same shape as tools/planning/pilotprovider's TestTodo_SELECT_002_Security:
// it proves the real, checked-in, signed freeze verifies, then proves that
// tampering any single signed field - including, specifically, quietly
// promoting a jurisdiction or provider promise, quietly permitting
// replay/repair billing, or quietly claiming system-of-record ownership -
// invalidates the signature.
func TestTodo_COMMERCIAL_001_Security(t *testing.T) {
	original := mustLoadFreeze(t)

	ok, err := VerifyFreezeSignature(original)
	if err != nil {
		t.Fatalf("VerifyFreezeSignature(original): %v", err)
	}
	if !ok {
		t.Fatal("the checked-in commercial-001-pilot-package.yaml must verify against its own signature")
	}

	tamperCases := []struct {
		name   string
		tamper func(*PilotCommercialFreeze)
	}{
		{"jurisdiction promise is quietly promoted to confirmed", func(f *PilotCommercialFreeze) { f.Jurisdiction.Status = PromiseSelectedConfirmed }},
		{"provider promise is quietly promoted with a real-looking vendor", func(f *PilotCommercialFreeze) {
			f.Provider.Status = PromiseSelectedConfirmed
			f.Provider.VendorRef = "acme-hcm"
		}},
		{"an SLO is quietly promised", func(f *PilotCommercialFreeze) { f.Evidence.SLOStatus = "PROMISED" }},
		{"replay billing is quietly permitted", func(f *PilotCommercialFreeze) { f.Billing.ReplayBillable = true }},
		{"repair billing is quietly permitted", func(f *PilotCommercialFreeze) { f.Billing.RepairBillable = true }},
		{"a system-of-record ownership claim is quietly added", func(f *PilotCommercialFreeze) {
			f.Authority.WriteAuthority = true
			f.Authority.OwnedDomains = []string{"worker"}
		}},
		{"the pricing hypothesis flag is quietly cleared", func(f *PilotCommercialFreeze) { f.Pricing.IsHypothesis = false }},
		{"the stop threshold is quietly softened to equal the reprice threshold", func(f *PilotCommercialFreeze) {
			f.RepriceStop.StopThresholdPct = f.RepriceStop.RepriceThresholdPct
		}},
		{"the deletion certificate guarantee is quietly dropped", func(f *PilotCommercialFreeze) { f.Exit.DeletionCertificate = false }},
		{"an entitlement is quietly added", func(f *PilotCommercialFreeze) {
			f.Intent.Entitlements = append(f.Intent.Entitlements, Entitlement{IntentID: "hcmnext.made.up/v1", EffectClass: "READ_ONLY", Disposition: "INCLUDED"})
		}},
	}

	for _, tc := range tamperCases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := deepCopy(original)
			tc.tamper(&tampered)

			ok, err := VerifyFreezeSignature(tampered)
			if err != nil {
				t.Fatalf("VerifyFreezeSignature(tampered): %v", err)
			}
			if ok {
				t.Fatalf("tampered freeze (%s) must not verify against the original signature", tc.name)
			}
		})
	}
}
