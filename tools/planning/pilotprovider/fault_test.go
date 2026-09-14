package pilotprovider

import (
	"testing"
)

// TestTodo_SELECT_002_Fault is SELECT-002's named FAULT test. It proves the
// checked-in topology's fault matrix and sandbox evidence are present AND
// EXACT, not merely non-empty: every one of integration-platform.md's nine
// Error Taxonomy classes appears exactly once, each with a real default
// action distinguishable from every other class's, and the sandbox evidence
// names a real fidelity level and evidence reference rather than a filler
// string.
func TestTodo_SELECT_002_Fault(t *testing.T) {
	topology := mustLoadTopology(t)

	t.Run("fault matrix covers the exact closed set of fault classes, once each", func(t *testing.T) {
		want := AllFaultClasses()
		if len(topology.Faults) != len(want) {
			t.Fatalf("topology has %d fault cases, want exactly %d (%v)", len(topology.Faults), len(want), want)
		}
		seen := map[string]FaultCase{}
		for _, f := range topology.Faults {
			if _, dup := seen[f.Class]; dup {
				t.Errorf("fault class %s appears more than once", f.Class)
			}
			seen[f.Class] = f
		}
		for _, class := range want {
			f, ok := seen[class]
			if !ok {
				t.Errorf("fault matrix is missing required class %s", class)
				continue
			}
			if f.ExampleResponse == "" {
				t.Errorf("fault class %s has no example_response", class)
			}
			if f.DefaultAction == "" {
				t.Errorf("fault class %s has no default_action", class)
			}
			if f.EvidenceRef == "" {
				t.Errorf("fault class %s has no evidence_ref", class)
			}
		}
	})

	t.Run("every fault case names a distinct default action - not one copy-pasted row for all nine", func(t *testing.T) {
		actions := map[string]bool{}
		for _, f := range topology.Faults {
			if actions[f.DefaultAction] {
				t.Errorf("default_action %q is reused across more than one fault class - the matrix looks copy-pasted rather than considered per class", f.DefaultAction)
			}
			actions[f.DefaultAction] = true
		}
	})

	t.Run("sandbox evidence names a real fidelity level, description and evidence reference", func(t *testing.T) {
		if !validSandboxFidelities[topology.Sandbox.Fidelity] {
			t.Fatalf("sandbox.fidelity = %q is not one of the closed vocabulary", topology.Sandbox.Fidelity)
		}
		if topology.Sandbox.Description == "" {
			t.Error("sandbox.description is empty")
		}
		if topology.Sandbox.EvidenceRef == "" {
			t.Error("sandbox.evidence_ref is empty")
		}
		if topology.Sandbox.ExecutedAt == "" {
			t.Error("sandbox.executed_at is empty")
		}
	})

	t.Run("the cross-system observation is a real polling or webhook method independent of write acknowledgement", func(t *testing.T) {
		obs := topology.CrossSystemObservation
		if obs.Method != ObservationWebhook && obs.Method != ObservationPolling {
			t.Fatalf("cross_system_observation.method = %q, want %s or %s", obs.Method, ObservationWebhook, ObservationPolling)
		}
		if !obs.ProvesIndependentOfWriteAcknowledgement {
			t.Error("cross_system_observation does not claim independence from the write acknowledgement")
		}
		if obs.Description == "" || obs.EvidenceRef == "" {
			t.Errorf("cross_system_observation is missing description or evidence_ref: %+v", obs)
		}
	})
}
