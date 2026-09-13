package pilotblueprint

import (
	"strings"
	"testing"
)

// TestTodo_CUSTOMER_001_Mutation is CUSTOMER-001's named MUTATION test. Each
// case mutates one field of a structurally valid blueprint fixture and
// proves two things together, matching this repository's existing
// `_Mutation` convention (tools/planning/pilotjurisdiction's
// TestTodo_SELECT_001_Mutation, tools/planning/pilotprovider's
// TestTodo_SELECT_002_Mutation): the resulting CanonicalDigest moves away
// from the original (the mutation is byte-detectable, not silently
// absorbed), and Validate names the exact condition the mutation broke (a
// finding is traceable, not merely "something is wrong").
func TestTodo_CUSTOMER_001_Mutation(t *testing.T) {
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
		mutate  func(*Blueprint)
		wantHit string
	}{
		{
			name:    "the legal-review workstream's kind is silently swapped for another stage",
			mutate:  func(b *Blueprint) { b.Workstreams[5].Kind = WorkstreamTesting },
			wantHit: "discovery-through-hypercare order",
		},
		{
			name:    "a workstream's escalation path is silently erased",
			mutate:  func(b *Blueprint) { b.Workstreams[0].Escalation = "" },
			wantHit: "workstreams[0].escalation: missing",
		},
		{
			name:    "a workstream's fallback is silently erased",
			mutate:  func(b *Blueprint) { b.Workstreams[0].Fallback = "" },
			wantHit: "workstreams[0].fallback: missing",
		},
		{
			name:    "a workstream's data-processing boundary is silently erased",
			mutate:  func(b *Blueprint) { b.Workstreams[0].DataProcessingBoundary = "" },
			wantHit: "workstreams[0].data_processing_boundary: missing",
		},
		{
			name: "a workstream's provider owner is quietly reassigned to customer, leaving no provider owner",
			mutate: func(b *Blueprint) {
				for i := range b.Workstreams[0].Owners {
					if b.Workstreams[0].Owners[i].Party == PartyProvider {
						b.Workstreams[0].Owners[i].Party = PartyCustomer
					}
				}
			},
			wantHit: "missing a PROVIDER owner",
		},
		{
			name:    "the provider topology reference is silently unpinned",
			mutate:  func(b *Blueprint) { b.ProviderTopologyRef = "" },
			wantHit: "provider_topology_ref: missing",
		},
		{
			name: "a workstream quietly loses its accountable owner, leaving none",
			mutate: func(b *Blueprint) {
				for i := range b.Workstreams[0].Owners {
					b.Workstreams[0].Owners[i].RACIRole = Consulted
				}
			},
			wantHit: "exactly one ACCOUNTABLE owner",
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
