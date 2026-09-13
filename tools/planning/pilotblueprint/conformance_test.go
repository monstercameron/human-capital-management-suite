package pilotblueprint

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
)

// TestTodo_CUSTOMER_001_Conformance is CUSTOMER-001's named CONFORMANCE
// test: it cross-checks the checked-in blueprint against the real, live
// artifacts it claims to bind to - PHASE-001's scope ceiling, SELECT-002's
// provider topology and SELECT-001's jurisdiction profile - rather than
// trusting the blueprint's own bookkeeping. This mirrors
// tools/planning/pilotjurisdiction's TestTodo_SELECT_001_Conformance and
// tools/planning/pilotprovider's TestTodo_SELECT_002_Conformance.
func TestTodo_CUSTOMER_001_Conformance(t *testing.T) {
	bp := mustLoadBlueprint(t)

	t.Run("provider_topology_ref names a real, loadable SELECT-002 topology", func(t *testing.T) {
		tp, err := pilotprovider.LoadTopology(repoRoot + "/" + bp.ProviderTopologyRef)
		if err != nil {
			t.Fatalf("LoadTopology(%s): %v", bp.ProviderTopologyRef, err)
		}
		if tp.TodoID != "SELECT-002" {
			t.Errorf("provider_topology_ref names a file with todo_id %q, want SELECT-002", tp.TodoID)
		}
	})

	t.Run("jurisdiction_profile_ref names a real, loadable SELECT-001 profile", func(t *testing.T) {
		p, err := pilotjurisdiction.LoadProfile(repoRoot + "/" + bp.JurisdictionProfileRef)
		if err != nil {
			t.Fatalf("LoadProfile(%s): %v", bp.JurisdictionProfileRef, err)
		}
		if p.TodoID != "SELECT-001" {
			t.Errorf("jurisdiction_profile_ref names a file with todo_id %q, want SELECT-001", p.TodoID)
		}
	})

	t.Run("scope_ceiling_ref names the real PHASE-001 ceiling, which names CUSTOMER-001 as a live dependency", func(t *testing.T) {
		ceiling, err := scopeceiling.LoadManifest(repoRoot + "/" + bp.ScopeCeilingRef)
		if err != nil {
			t.Fatalf("scopeceiling.LoadManifest(%s): %v", bp.ScopeCeilingRef, err)
		}
		if ceiling.TodoID != "PHASE-001" {
			t.Errorf("scope_ceiling_ref names a file with todo_id %q, want PHASE-001", ceiling.TodoID)
		}

		found := false
		for _, e := range ceiling.Effects {
			if e.Class != scopeceiling.OutboundMessageIntent {
				continue
			}
			found = true
			if !strings.Contains(e.Rationale, "CUSTOMER-001") {
				t.Errorf("ceiling's OUTBOUND_MESSAGE_INTENT effect rationale does not name CUSTOMER-001: %q", e.Rationale)
			}
		}
		if !found {
			t.Fatal("scope ceiling carries no OUTBOUND_MESSAGE_INTENT effect item")
		}
	})

	t.Run("every workstream naming a PROVIDER prerequisite points its evidence at the real provider topology path", func(t *testing.T) {
		for _, w := range bp.Workstreams {
			for _, d := range w.Prerequisites {
				if d.Party != PartyProvider {
					continue
				}
				if !strings.Contains(d.EvidenceRef, "select-002-provider-topology.yaml") {
					t.Errorf("workstream %s names a PROVIDER prerequisite whose evidence_ref %q does not point at the real signed topology", w.Kind, d.EvidenceRef)
				}
			}
		}
	})
}
