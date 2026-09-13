package threatregister

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/productslice"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
)

// TestTodo_THREAT_001_Conformance is THREAT-001's named CONFORMANCE test:
// it cross-checks the checked-in register against the two live registries
// the todo names as the authorized enumeration of "every Phase 1 vertical
// slice" - definitions/planning/product-slices.yaml (ALIGN-001) and
// definitions/planning/gates/phase1-scope-ceiling.yaml (PHASE-001) - rather
// than trusting this register's own slice_id list. This mirrors
// tools/planning/pilotprovider's TestTodo_SELECT_002_Conformance, which
// cross-checks its topology against the live scope ceiling the same way.
func TestTodo_THREAT_001_Conformance(t *testing.T) {
	r := mustLoadRegister(t)

	root, err := productslice.RepoRoot()
	if err != nil {
		t.Fatalf("productslice.RepoRoot: %v", err)
	}

	slicesPath := root + "/definitions/planning/product-slices.yaml"
	liveSlices, err := productslice.LoadRegistryYAML(slicesPath)
	if err != nil {
		t.Fatalf("productslice.LoadRegistryYAML(%s): %v", slicesPath, err)
	}
	if err := liveSlices.VerifyDigest(); err != nil {
		t.Fatalf("checked-in product-slices.yaml digest is stale: %v", err)
	}

	t.Run("the register's slice set matches the live product-slices.yaml registry exactly", func(t *testing.T) {
		wantIDs := map[string]bool{}
		for _, s := range liveSlices.Slices {
			wantIDs[s.SliceID] = true
		}
		gotIDs := map[string]bool{}
		for _, s := range r.Slices {
			gotIDs[s.SliceID] = true
		}
		for id := range wantIDs {
			if !gotIDs[id] {
				t.Errorf("live product-slices.yaml admits slice %q but the threat register does not cover it", id)
			}
		}
		for id := range gotIDs {
			if !wantIDs[id] {
				t.Errorf("threat register covers slice %q but it is not (or no longer) an admitted product slice", id)
			}
		}
		if len(wantIDs) == 0 {
			t.Fatal("live product-slices.yaml admits no slices - this conformance check cannot fire against real data")
		}
	})

	ceilingPath := root + "/definitions/planning/gates/phase1-scope-ceiling.yaml"
	ceiling, err := scopeceiling.LoadManifest(ceilingPath)
	if err != nil {
		t.Fatalf("scopeceiling.LoadManifest(%s): %v", ceilingPath, err)
	}
	includeByIntent := map[string]scopeceiling.Disposition{}
	for _, it := range ceiling.Intents {
		includeByIntent[it.ID] = it.Disposition
	}

	t.Run("every business intent the promotion slice delivers is INCLUDE in the Phase 1 scope ceiling", func(t *testing.T) {
		var promotion *productslice.ProductSliceDefinition
		for i := range liveSlices.Slices {
			if liveSlices.Slices[i].SliceID == "promotion" {
				promotion = &liveSlices.Slices[i]
			}
		}
		if promotion == nil {
			t.Fatal("live product-slices.yaml has no promotion slice to cross-check")
		}
		if len(promotion.BusinessIntents) == 0 {
			t.Fatal("promotion slice names no business intents - this conformance check cannot fire against real data")
		}
		for _, intentID := range promotion.BusinessIntents {
			disposition, ok := includeByIntent[intentID]
			if !ok {
				t.Errorf("promotion slice names business intent %q that the Phase 1 scope ceiling does not carry at all", intentID)
				continue
			}
			if disposition != scopeceiling.Include {
				t.Errorf("promotion slice's business intent %q has scope-ceiling disposition %q, want INCLUDE", intentID, disposition)
			}
		}
	})
}
