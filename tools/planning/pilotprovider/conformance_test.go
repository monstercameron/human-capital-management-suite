package pilotprovider

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
)

// TestTodo_SELECT_002_Conformance is SELECT-002's named CONFORMANCE test: it
// cross-checks the checked-in topology against PHASE-001's real, live
// definitions/planning/gates/phase1-scope-ceiling.yaml, the sibling
// document that named SELECT-002 as the filler of its provider, topology
// and (jointly with COMMERCIAL-001) slo selection slots, rather than
// trusting this topology's own bookkeeping. This mirrors
// tools/planning/pilotjurisdiction's TestTodo_SELECT_001_Conformance, which
// cross-checks its profile against the live jurisdiction slot the same way.
func TestTodo_SELECT_002_Conformance(t *testing.T) {
	mustLoadTopology(t) // proves the checked-in file parses under this schema.

	ceiling, err := scopeceiling.LoadManifest(repoRoot + "/definitions/planning/gates/phase1-scope-ceiling.yaml")
	if err != nil {
		t.Fatalf("scopeceiling.LoadManifest: %v", err)
	}

	t.Run("PHASE-001's provider and topology slots name SELECT-002 and remain unfilled", func(t *testing.T) {
		wantSlots := map[string]bool{"provider": false, "topology": false}
		for _, slot := range ceiling.SelectionSlots {
			if _, relevant := wantSlots[slot.Name]; !relevant {
				continue
			}
			wantSlots[slot.Name] = true
			if slot.FillingTodoID != "SELECT-002" {
				t.Errorf("ceiling's %s slot names filling_todo_id %q, want SELECT-002", slot.Name, slot.FillingTodoID)
			}
			if slot.Filled {
				t.Errorf("ceiling's %s slot is filled - PHASE-001 requires it stay empty; SELECT-002 fills it externally via this topology, not by editing the ceiling", slot.Name)
			}
			if slot.Value != "" {
				t.Errorf("ceiling's %s slot carries a value %q - PHASE-001 requires it stay empty", slot.Name, slot.Value)
			}
		}
		for name, found := range wantSlots {
			if !found {
				t.Errorf("scope ceiling carries no %q selection slot", name)
			}
		}
	})

	t.Run("PHASE-001's slo slot jointly names SELECT-002 among its fillers", func(t *testing.T) {
		found := false
		for _, slot := range ceiling.SelectionSlots {
			if slot.Name != "slo" {
				continue
			}
			found = true
			if !strings.Contains(slot.FillingTodoID, "SELECT-002") {
				t.Errorf("ceiling's slo slot filling_todo_id %q does not name SELECT-002", slot.FillingTodoID)
			}
		}
		if !found {
			t.Fatal("scope ceiling carries no slo selection slot")
		}
	})

	t.Run("the ceiling's EXTERNAL_PROVIDER_WRITE effect is still unbound and INCLUDE, which is what this topology's WRITE operation depends on", func(t *testing.T) {
		found := false
		for _, e := range ceiling.Effects {
			if e.Class != scopeceiling.ExternalProviderWrite {
				continue
			}
			found = true
			if e.Disposition != scopeceiling.Include {
				t.Errorf("EXTERNAL_PROVIDER_WRITE effect has disposition %q, want INCLUDE - a provider topology declaring WRITE operations would be inconsistent with a REJECTed effect class", e.Disposition)
			}
			if e.ProviderDependency != scopeceiling.UnboundProviderSentinel {
				t.Errorf("EXTERNAL_PROVIDER_WRITE effect names provider_dependency %q, want the unbound sentinel %q - PHASE-001's ceiling must never itself name a concrete provider", e.ProviderDependency, scopeceiling.UnboundProviderSentinel)
			}
		}
		if !found {
			t.Fatal("scope ceiling carries no EXTERNAL_PROVIDER_WRITE effect item")
		}
	})
}
