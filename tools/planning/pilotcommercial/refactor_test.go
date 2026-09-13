package pilotcommercial

import (
	"strings"
	"testing"
)

// TestNoEntitlementInThePackageGrantsUnauthorizedCapability is
// COMMERCIAL-001's REFACTOR enforcement test: "contract prose cannot
// independently enable capability" means a package cannot sell an
// entitlement PHASE-001's live scope ceiling has not authorized with
// disposition INCLUDE. This test proves it with a real, currently-deferred
// intent from the live ceiling - hcmnext.people.change_manager/v1, disposed
// DEFER, not INCLUDE - rather than a made-up identifier, so the check is
// proven against an actual boundary this repository has already drawn, not
// a hypothetical one.
func TestNoEntitlementInThePackageGrantsUnauthorizedCapability(t *testing.T) {
	base := mustLoadFreeze(t)

	const deferredIntent = "hcmnext.people.change_manager/v1"
	selling := deepCopy(base)
	selling.Intent.Entitlements = append(selling.Intent.Entitlements, Entitlement{
		IntentID:    deferredIntent,
		EffectClass: "READ_ONLY",
		Disposition: "INCLUDED",
	})

	mismatches, err := ConformsToLiveRegistries(selling, ceilingPath, jurisdictionPath, providerPath)
	if err != nil {
		t.Fatalf("ConformsToLiveRegistries: %v", err)
	}
	found := false
	for _, m := range mismatches {
		if strings.Contains(m.Issue, deferredIntent) && strings.Contains(m.Issue, "not INCLUDE") {
			found = true
		}
	}
	if !found {
		t.Fatalf("selling a DEFER-disposed intent (%s) must be reported as an unauthorized entitlement, got %v", deferredIntent, mismatches)
	}
}

// TestEntitlementSetIsExactlyTheLiveCommercialRegistrysNoMoreNoLess proves
// the other direction of REFACTOR's clause: the package cannot merely be a
// subset of what the ceiling authorizes while independently inventing its
// own entitlement set - it must equal internal/commercial's live registry
// exactly, entitlement-for-entitlement. Dropping one live entitlement from
// the freeze (a package that sells less than the registry grants, silently)
// is exactly as much an independent restatement as adding one the registry
// does not grant, so it must be caught too.
func TestEntitlementSetIsExactlyTheLiveCommercialRegistrysNoMoreNoLess(t *testing.T) {
	base := mustLoadFreeze(t)
	if len(base.Intent.Entitlements) == 0 {
		t.Fatal("checked-in freeze names no entitlements to drop")
	}

	dropped := deepCopy(base)
	dropped.Intent.Entitlements = dropped.Intent.Entitlements[1:]

	mismatches, err := ConformsToLiveRegistries(dropped, ceilingPath, jurisdictionPath, providerPath)
	if err != nil {
		t.Fatalf("ConformsToLiveRegistries: %v", err)
	}
	found := false
	for _, m := range mismatches {
		if m.Field == "intent.entitlements" {
			found = true
		}
	}
	if !found {
		t.Fatalf("silently dropping a live-registry entitlement must be reported, got %v", mismatches)
	}
}
