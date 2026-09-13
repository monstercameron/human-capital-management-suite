package pilotcommercial

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
)

// TestTodo_COMMERCIAL_001_Conformance is COMMERCIAL-001's named CONFORMANCE
// test and this todo's load-bearing test: it drives every "package promises
// an unselected intent, provider, jurisdiction or SLO" check off the real,
// live contents of PHASE-001's scope ceiling and SELECT-001/SELECT-002's
// selections - never off a hardcoded list this package could silently drift
// away from. If any of those three artifacts changes (a slot fills, a
// jurisdiction gets reviewed, a provider gets confirmed), this test's
// expectations move with it automatically because it re-derives what may be
// promised on every run, exactly as
// tools/planning/pilotprovider.TestTodo_SELECT_002_Conformance re-derives
// its expectations from the live ceiling rather than restating them.
func TestTodo_COMMERCIAL_001_Conformance(t *testing.T) {
	freeze := mustLoadFreeze(t) // proves the checked-in file parses under this schema.

	ceiling, err := scopeceiling.LoadManifest(ceilingPath)
	if err != nil {
		t.Fatalf("scopeceiling.LoadManifest: %v", err)
	}

	t.Run("PHASE-001's slo slot names COMMERCIAL-001 among its fillers and remains unfilled", func(t *testing.T) {
		found := false
		for _, slot := range ceiling.SelectionSlots {
			if slot.Name != "slo" {
				continue
			}
			found = true
			if !strings.Contains(slot.FillingTodoID, "COMMERCIAL-001") {
				t.Errorf("ceiling's slo slot filling_todo_id %q does not name COMMERCIAL-001", slot.FillingTodoID)
			}
			if slot.Filled {
				t.Error("ceiling's slo slot is filled - PHASE-001 requires it stay empty; COMMERCIAL-001 fills its half by promising NONE, not by editing the ceiling")
			}
			if slot.Value != "" {
				t.Errorf("ceiling's slo slot carries a value %q - PHASE-001 requires it stay empty", slot.Value)
			}
		}
		if !found {
			t.Fatal("scope ceiling carries no slo selection slot")
		}
	})

	t.Run("this freeze promises no SLO while the slo slot is unfilled", func(t *testing.T) {
		if freeze.Evidence.SLOStatus != SLOStatusNone {
			t.Errorf("freeze evidence.slo_status = %q, want %s", freeze.Evidence.SLOStatus, SLOStatusNone)
		}
	})

	// The full cross-registry derivation: this is the actual enforcement,
	// not just the slo-slot spot check above.
	t.Run("the checked-in freeze conforms to every live registry it must derive from", func(t *testing.T) {
		mismatches, err := ConformsToLiveRegistries(freeze, ceilingPath, jurisdictionPath, providerPath)
		if err != nil {
			t.Fatalf("ConformsToLiveRegistries: %v", err)
		}
		if len(mismatches) != 0 {
			t.Errorf("freeze diverges from live registries: %v", mismatches)
		}
	})
}

// TestConformsToLiveRegistriesCatchesADivergingFreeze is
// TestTodo_COMMERCIAL_001_Conformance's own falsifiability proof: a
// derivation check that only ever runs against the one checked-in file that
// already agrees with the registries could be vacuous. This test builds a
// freeze that deliberately diverges from the live ceiling/registries in one
// field at a time and proves ConformsToLiveRegistries names each divergence.
func TestConformsToLiveRegistriesCatchesADivergingFreeze(t *testing.T) {
	base := mustLoadFreeze(t)
	if mismatches, err := ConformsToLiveRegistries(base, ceilingPath, jurisdictionPath, providerPath); err != nil || len(mismatches) != 0 {
		t.Fatalf("checked-in freeze must start conformant, got err=%v mismatches=%v", err, mismatches)
	}

	cases := []struct {
		name    string
		mutate  func(*PilotCommercialFreeze)
		wantHit string
	}{
		{
			name: "an entitlement not in the live ceiling is added",
			mutate: func(f *PilotCommercialFreeze) {
				f.Intent.Entitlements = append(f.Intent.Entitlements, Entitlement{IntentID: "hcmnext.made.up/v1", EffectClass: "READ_ONLY", Disposition: "INCLUDED"})
			},
			wantHit: "intent.entitlements",
		},
		{
			name:    "the scope ceiling digest is quietly changed",
			mutate:  func(f *PilotCommercialFreeze) { f.Intent.ScopeCeilingDigest = strings.Repeat("0", 64) },
			wantHit: "intent.scope_ceiling_digest",
		},
		{
			name:    "pricing quietly diverges from the live commercial registry",
			mutate:  func(f *PilotCommercialFreeze) { f.Pricing.MaximumCents = f.Pricing.MaximumCents * 2 },
			wantHit: "pricing",
		},
		{
			name:    "authority quietly claims an owned domain",
			mutate:  func(f *PilotCommercialFreeze) { f.Authority.OwnedDomains = []string{"worker"} },
			wantHit: "authority",
		},
		{
			name:    "exit terms quietly diverge from the live commercial registry",
			mutate:  func(f *PilotCommercialFreeze) { f.Exit.RetentionDays = 9999 },
			wantHit: "exit",
		},
		{
			name:    "the jurisdiction promise quietly claims confirmation",
			mutate:  func(f *PilotCommercialFreeze) { f.Jurisdiction.Status = PromiseSelectedConfirmed },
			wantHit: "jurisdiction.status",
		},
		{
			name: "the provider promise quietly claims a selection",
			mutate: func(f *PilotCommercialFreeze) {
				f.Provider.Status = PromiseSelectedConfirmed
				f.Provider.VendorRef = "acme-hcm"
			},
			wantHit: "provider.status",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := deepCopy(base)
			tc.mutate(&mutated)
			mismatches, err := ConformsToLiveRegistries(mutated, ceilingPath, jurisdictionPath, providerPath)
			if err != nil {
				t.Fatalf("ConformsToLiveRegistries: %v", err)
			}
			found := false
			for _, m := range mismatches {
				if strings.Contains(m.String(), tc.wantHit) {
					found = true
				}
			}
			if !found {
				t.Errorf("mutation %q: expected a mismatch naming %q, got %v", tc.name, tc.wantHit, mismatches)
			}
		})
	}
}
