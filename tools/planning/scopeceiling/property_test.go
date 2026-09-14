package scopeceiling

import (
	"strings"
	"testing"
)

// TestTodo_PHASE_001_Property is PHASE-001's PROPERTY test. It proves the
// GREEN invariant - "every scope item lacking explicit include/defer/reject
// rationale, or breaking one of RED's other named conditions, fails
// Validate" - holds across the whole schema space (every category, not just
// the one checked-in fixture), and that a structurally complete manifest
// with none of those defects validates clean.
func TestTodo_PHASE_001_Property(t *testing.T) {
	base := validFixture()
	if v := base.Validate(); len(v) != 0 {
		t.Fatalf("validFixture() must validate clean, got: %v", v)
	}

	cases := []struct {
		name    string
		mutate  func(*ScopeCeilingManifest)
		wantHit string
	}{
		{"intent missing disposition", func(m *ScopeCeilingManifest) { m.Intents[0].Disposition = "" }, "intents[0].disposition"},
		{"intent missing rationale", func(m *ScopeCeilingManifest) { m.Intents[0].Rationale = "" }, "intents[0].rationale"},
		{"intent missing owner", func(m *ScopeCeilingManifest) { m.Intents[0].OwnerDomain = "" }, "intents[0].owner_domain"},
		{"intent unknown gate", func(m *ScopeCeilingManifest) { m.Intents[0].Gate = "GATE_X" }, "intents[0].gate"},
		{"intent payroll ownership included", func(m *ScopeCeilingManifest) {
			m.Intents = append(m.Intents, IntentItem{
				ID: "hcmnext.payroll.run_payroll/v1", OwnerDomain: "PAYROLL", Gate: GateP1B,
				Disposition: Include, Rationale: "should never validate",
			})
		}, "future native payroll/WFM/talent ownership"},
		{"capability missing owner", func(m *ScopeCeilingManifest) { m.Capabilities[0].OwnerDomain = "" }, "capabilities[0].owner_domain"},
		{"capability unknown effect class", func(m *ScopeCeilingManifest) { m.Capabilities[0].EffectClass = "WRITE_EVERYTHING" }, "capabilities[0].effect_class"},
		{"capability missing rationale", func(m *ScopeCeilingManifest) { m.Capabilities[0].Rationale = "" }, "capabilities[0].rationale"},
		{"workflow missing vertical slice", func(m *ScopeCeilingManifest) { m.Workflows[0].VerticalSlice = "" }, "workflow without vertical slice"},
		{"workflow missing disposition", func(m *ScopeCeilingManifest) { m.Workflows[0].Disposition = "" }, "workflows[0].disposition"},
		{"user flow missing rationale", func(m *ScopeCeilingManifest) { m.UserFlows[0].Rationale = "" }, "user_flows[0].rationale"},
		{"endpoint missing endpoint_disposition", func(m *ScopeCeilingManifest) { m.Endpoints[0].EndpointDisposition = "" }, "endpoint without disposition"},
		{"endpoint unknown endpoint_disposition", func(m *ScopeCeilingManifest) { m.Endpoints[0].EndpointDisposition = "SOMETIMES_PUBLIC" }, "endpoint without disposition"},
		{"endpoint duplicate id", func(m *ScopeCeilingManifest) { m.Endpoints = append(m.Endpoints, m.Endpoints[0]) }, "duplicate endpoint"},
		{"model missing rationale", func(m *ScopeCeilingManifest) { m.Models[0].Rationale = "" }, "models[0].rationale"},
		{"effect missing disposition", func(m *ScopeCeilingManifest) { m.Effects[0].Disposition = "" }, "effects[0].disposition"},
		{"effect hidden provider dependency", func(m *ScopeCeilingManifest) { m.Effects[0].ProviderDependency = "Workday HCM" }, "hidden provider dependency"},
		{"selection slot filled", func(m *ScopeCeilingManifest) { m.SelectionSlots[0].Filled = true }, "must never carry a filled selection slot"},
		{"selection slot has a value", func(m *ScopeCeilingManifest) { m.SelectionSlots[0].Value = "Workday" }, "must never carry a selected value"},
		{"selection slot missing filling todo", func(m *ScopeCeilingManifest) { m.SelectionSlots[0].FillingTodoID = "" }, "selection_slots[0].filling_todo_id"},
		{"selection slots incomplete", func(m *ScopeCeilingManifest) { m.SelectionSlots = m.SelectionSlots[:2] }, "selection_slots"},
		{"deferred domain missing rationale", func(m *ScopeCeilingManifest) { m.DeferredDomains[0].Rationale = "" }, "deferred_domains[0].rationale"},
		{"deferred domain missing source ref", func(m *ScopeCeilingManifest) { m.DeferredDomains[0].SourceRef = "" }, "deferred_domains[0].source_ref"},
		{"no signature", func(m *ScopeCeilingManifest) { m.Signature = nil }, "signature"},
		{"wrong todo id", func(m *ScopeCeilingManifest) { m.TodoID = "NEXT-002" }, "todo_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := deepCopy(base)
			tc.mutate(&m)
			violations := m.Validate()
			if len(violations) == 0 {
				t.Fatalf("expected a violation for %q, got none", tc.name)
			}
			found := false
			for _, v := range violations {
				if strings.Contains(v.String(), tc.wantHit) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a violation containing %q, got %v", tc.wantHit, violations)
			}
		})
	}
}
