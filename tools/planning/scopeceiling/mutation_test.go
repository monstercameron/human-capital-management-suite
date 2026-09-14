package scopeceiling

import (
	"strings"
	"testing"
)

// TestTodo_PHASE_001_Mutation is PHASE-001's named MUTATION test. Each case
// mutates one field of a structurally valid ceiling fixture and proves two
// things together, matching the repo's existing `_Mutation` convention
// (e.g. tools/planning/designclosure's TestTodo_CLOSE_001_Mutation): the
// resulting CanonicalDigest moves away from the original (so the mutation
// is byte-detectable, not silently absorbed), and Validate names the exact
// condition the mutation broke (so a finding is traceable, not merely
// "something is wrong").
func TestTodo_PHASE_001_Mutation(t *testing.T) {
	original := validFixture()
	if v := original.Validate(); len(v) != 0 {
		t.Fatalf("fixture must start valid, got: %v", v)
	}
	originalDigest, err := original.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest(original): %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*ScopeCeilingManifest)
		wantHit string
	}{
		{
			name:    "an intent's rationale is silently erased",
			mutate:  func(m *ScopeCeilingManifest) { m.Intents[0].Rationale = "" },
			wantHit: "intents[0].rationale: missing include/defer/reject rationale",
		},
		{
			name: "a capability loses its owner",
			mutate: func(m *ScopeCeilingManifest) {
				m.Capabilities[0].OwnerDomain = ""
			},
			wantHit: "capabilities[0].owner_domain: capability without owner",
		},
		{
			name: "a workflow loses its vertical slice",
			mutate: func(m *ScopeCeilingManifest) {
				m.Workflows[0].VerticalSlice = ""
			},
			wantHit: "workflow without vertical slice",
		},
		{
			name: "an endpoint loses its disposition",
			mutate: func(m *ScopeCeilingManifest) {
				m.Endpoints[0].EndpointDisposition = ""
			},
			wantHit: "endpoint without disposition",
		},
		{
			name: "a rejected effect names a concrete provider",
			mutate: func(m *ScopeCeilingManifest) {
				m.Effects[0].ProviderDependency = "SAP SuccessFactors, edition Employee Central"
			},
			wantHit: "hidden provider dependency",
		},
		{
			name: "the topology selection slot is quietly filled",
			mutate: func(m *ScopeCeilingManifest) {
				for i := range m.SelectionSlots {
					if m.SelectionSlots[i].Name == "topology" {
						m.SelectionSlots[i].Filled = true
					}
				}
			},
			wantHit: "must never carry a filled selection slot",
		},
		{
			name: "the jurisdiction selection slot is quietly given a value",
			mutate: func(m *ScopeCeilingManifest) {
				for i := range m.SelectionSlots {
					if m.SelectionSlots[i].Name == "jurisdiction" {
						m.SelectionSlots[i].Value = "US-CA"
					}
				}
			},
			wantHit: "must never carry a selected value",
		},
		{
			name: "a native payroll intent is added and marked INCLUDE",
			mutate: func(m *ScopeCeilingManifest) {
				m.Intents = append(m.Intents, IntentItem{
					ID: "hcmnext.payroll.run_payroll/v1", OwnerDomain: "PAYROLL",
					Gate: GateP1B, Disposition: Include, Rationale: "smuggled in",
				})
			},
			wantHit: "future native payroll/WFM/talent ownership must not be INCLUDE",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := deepCopy(original)
			tc.mutate(&mutated)

			violations := mutated.Validate()
			if len(violations) == 0 {
				t.Fatalf("mutation %q produced no violation - Validate silently absorbed it", tc.name)
			}
			found := false
			for _, v := range violations {
				if strings.Contains(v.String(), tc.wantHit) {
					found = true
				}
			}
			if !found {
				t.Errorf("mutation %q: expected a violation naming %q, got %v", tc.name, tc.wantHit, violations)
			}

			mutatedDigest, err := mutated.CanonicalDigest()
			if err != nil {
				t.Fatalf("CanonicalDigest(mutated): %v", err)
			}
			if mutatedDigest == originalDigest {
				t.Errorf("mutation %q: CanonicalDigest did not move - the mutation is not byte-detectable", tc.name)
			}
		})
	}
}
