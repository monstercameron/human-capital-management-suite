package threatregister

import (
	"strings"
	"testing"
)

// TestTodo_THREAT_001_Mutation is THREAT-001's named MUTATION test. Each
// case mutates one field of a structurally valid register fixture and
// proves two things together, matching this repository's existing
// `_Mutation` convention (tools/planning/pilotprovider's
// TestTodo_SELECT_002_Mutation, tools/planning/pilotjurisdiction's
// TestTodo_SELECT_001_Mutation): the resulting CanonicalDigest moves away
// from the original (the mutation is byte-detectable, not silently
// absorbed), and Validate names the exact condition the mutation broke.
func TestTodo_THREAT_001_Mutation(t *testing.T) {
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
		mutate  func(*Register)
		wantHit string
	}{
		{
			name:    "a threat's attack class is silently swapped for one already covered",
			mutate:  func(r *Register) { r.Slices[0].Threats[0].AttackClass = r.Slices[0].Threats[1].AttackClass },
			wantHit: "ignores attack class",
		},
		{
			name:    "a threat's owner is silently erased",
			mutate:  func(r *Register) { r.Slices[0].Threats[0].Owner = "" },
			wantHit: "owner",
		},
		{
			name:    "a threat's detection is silently erased",
			mutate:  func(r *Register) { r.Slices[0].Threats[0].Detection = "" },
			wantHit: "detection",
		},
		{
			name:    "a threat's recovery is silently erased",
			mutate:  func(r *Register) { r.Slices[0].Threats[0].Recovery = "" },
			wantHit: "recovery",
		},
		{
			name: "a threat is quietly detached from its edge and mitigation",
			mutate: func(r *Register) {
				r.Slices[0].Threats[0].ConsumingEdges = nil
				r.Slices[0].Threats[0].Mitigations = nil
			},
			wantHit: "consuming_edges",
		},
		{
			name: "the shared mitigation's consuming edges are quietly truncated",
			mutate: func(r *Register) {
				m := &r.Slices[0].Mitigations[0]
				m.ConsumingEdges = m.ConsumingEdges[:len(m.ConsumingEdges)-1]
			},
			wantHit: "does not retain consuming edge",
		},
		{
			name:    "the todo id is silently swapped",
			mutate:  func(r *Register) { r.TodoID = "THREAT-999" },
			wantHit: "todo_id",
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
