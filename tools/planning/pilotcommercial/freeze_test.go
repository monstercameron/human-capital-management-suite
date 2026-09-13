package pilotcommercial

import "testing"

// TestPilotCommercialPackageMatchesReleaseEntitlementsCostsRisksAndExitTerms
// is COMMERCIAL-001's PRIMARY test. It loads the real, checked-in
// definitions/planning/gates/commercial-001-pilot-package.yaml and proves:
// every RED element is structurally present and honestly bounded (no
// unselected intent/provider/jurisdiction/SLO, overlay authority never
// confused with system-of-record ownership, implementation/support/provider
// cost all named, replay/repair never billable, data-processing/retention/
// exit terms present); every GREEN element holds (entitlements and
// exclusions mapped to the Phase 1 digest, price/usage/support/
// implementation assumptions declared, legal/authority boundaries declared,
// provider pass-throughs declared, evidence/SLO statement declared,
// termination/export obligations declared, quantitative reprice/stop
// criteria declared); and that the checked-in file's entitlements, costs
// (pricing/exit terms) and authority-boundary risk posture match
// internal/commercial's live registry exactly, which is what the test name
// asserts and what makes REFACTOR's "views consume registries" real rather
// than aspirational.
func TestPilotCommercialPackageMatchesReleaseEntitlementsCostsRisksAndExitTerms(t *testing.T) {
	freeze := mustLoadFreeze(t)

	violations := freeze.Validate()
	if len(violations) != 0 {
		t.Fatalf("the checked-in freeze must validate as structurally complete, got: %v", violations)
	}

	// --- RED: no unselected intent/provider/jurisdiction/SLO ---
	if freeze.Jurisdiction.Status == PromiseSelectedConfirmed {
		t.Error("checked-in freeze claims SELECTED_CONFIRMED jurisdiction, but SELECT-001 is UNREVIEWED")
	}
	if freeze.Provider.Status != PromiseNone {
		t.Errorf("checked-in freeze's provider.status = %q, want %s while SELECT-002 remains a placeholder", freeze.Provider.Status, PromiseNone)
	}
	if freeze.Evidence.SLOStatus != SLOStatusNone {
		t.Errorf("checked-in freeze's evidence.slo_status = %q, want %s while PHASE-001's slo slot is unfilled", freeze.Evidence.SLOStatus, SLOStatusNone)
	}

	// --- RED: confuses overlay authority with system-of-record ownership ---
	if freeze.Authority.WriteAuthority {
		t.Error("checked-in freeze claims write_authority: true, which would make it a system-of-record claim, not an overlay")
	}
	if len(freeze.Authority.OwnedDomains) != 0 {
		t.Errorf("checked-in freeze claims owned_domains %v; an overlay package must own nothing", freeze.Authority.OwnedDomains)
	}
	if len(freeze.Authority.ObservedDomains) == 0 {
		t.Error("checked-in freeze names no observed domains")
	}

	// --- RED: omits implementation/support/provider cost ---
	if freeze.Pricing.ImplementationCost == "" || freeze.Pricing.SupportModel == "" || freeze.Pricing.ProviderPassThrough == "" {
		t.Fatalf("checked-in freeze omits an implementation/support/provider cost assumption: %+v", freeze.Pricing)
	}
	if !freeze.Pricing.IsHypothesis {
		t.Error("checked-in freeze's pricing.is_hypothesis must be true - pricing is a hypothesis, not an agreed rate card")
	}

	// --- RED: bills replay or repair duplicates ---
	if freeze.Billing.ReplayBillable || freeze.Billing.RepairBillable {
		t.Errorf("checked-in freeze's billing policy allows billing a replay or repair: %+v", freeze.Billing)
	}

	// --- RED: lacks data-processing/retention/exit terms ---
	if freeze.Exit.RetentionDays <= 0 || !freeze.Exit.DeletionCertificate {
		t.Fatalf("checked-in freeze's exit terms are incomplete: %+v", freeze.Exit)
	}
	if freeze.DataProcessing.ResidencyRegion == "" || freeze.DataProcessing.DPAReference == "" {
		t.Fatalf("checked-in freeze's data-processing terms are incomplete: %+v", freeze.DataProcessing)
	}

	// --- GREEN: quantitative reprice/stop criteria ---
	if freeze.RepriceStop.RepriceThresholdPct <= 0 || freeze.RepriceStop.StopThresholdPct <= freeze.RepriceStop.RepriceThresholdPct {
		t.Fatalf("checked-in freeze's reprice/stop criteria are not a sane quantitative ladder: %+v", freeze.RepriceStop)
	}

	// Signature verifies.
	ok, err := VerifyFreezeSignature(freeze)
	if err != nil {
		t.Fatalf("VerifyFreezeSignature: %v", err)
	}
	if !ok {
		t.Fatal("the checked-in freeze must verify against its own signature")
	}

	// --- The test's own name: entitlements, costs and exit terms match the
	// live release/entitlement/cost registry exactly. ---
	mismatches, err := ConformsToLiveRegistries(freeze, ceilingPath, jurisdictionPath, providerPath)
	if err != nil {
		t.Fatalf("ConformsToLiveRegistries: %v", err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("checked-in freeze does not match the live registries: %v", mismatches)
	}
}
