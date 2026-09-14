package pilotblueprint

import (
	"strings"
	"testing"
)

// TestTodo_CUSTOMER_001_Property is CUSTOMER-001's PROPERTY test. It proves
// the GREEN invariant - every RED element Validate checks for actually
// causes a violation naming the exact broken field when removed, and a
// structurally complete blueprint with none of those defects validates
// clean - holds across the whole schema, not just the one checked-in file.
func TestTodo_CUSTOMER_001_Property(t *testing.T) {
	base := validFixture()
	if v := base.Validate(); len(v) != 0 {
		t.Fatalf("validFixture() must validate clean, got: %v", v)
	}

	cases := []struct {
		name    string
		mutate  func(*Blueprint)
		wantHit string
	}{
		{"wrong todo id", func(b *Blueprint) { b.TodoID = "SELECT-001" }, "todo_id"},
		{"missing template version", func(b *Blueprint) { b.TemplateVersion = "" }, "template_version"},
		{"missing provider topology ref", func(b *Blueprint) { b.ProviderTopologyRef = "" }, "provider_topology_ref"},
		{"missing jurisdiction profile ref", func(b *Blueprint) { b.JurisdictionProfileRef = "" }, "jurisdiction_profile_ref"},

		{"a workstream is dropped entirely", func(b *Blueprint) {
			b.Workstreams = b.Workstreams[1:]
		}, "workstreams"},
		{"two workstreams are swapped, breaking discovery-through-hypercare order", func(b *Blueprint) {
			b.Workstreams[0], b.Workstreams[1] = b.Workstreams[1], b.Workstreams[0]
		}, "discovery-through-hypercare order"},
		{"a workstream kind is duplicated", func(b *Blueprint) {
			b.Workstreams[1].Kind = b.Workstreams[0].Kind
		}, "workstreams[1].kind"},
		{"a sequence number is wrong", func(b *Blueprint) { b.Workstreams[2].Sequence = 99 }, "workstreams[2].sequence"},

		{"a workstream's customer owner is dropped", func(b *Blueprint) {
			b.Workstreams[0].Owners = b.Workstreams[0].Owners[1:]
		}, "missing a CUSTOMER owner"},
		{"a workstream's owner role title is erased", func(b *Blueprint) {
			b.Workstreams[0].Owners[0].RoleTitle = ""
		}, "owners[0].role_title"},
		{"a workstream names two accountable owners", func(b *Blueprint) {
			b.Workstreams[0].Owners[0].RACIRole = Accountable
			b.Workstreams[0].Owners[1].RACIRole = Accountable
		}, "exactly one ACCOUNTABLE owner"},
		{"a workstream names zero accountable owners", func(b *Blueprint) {
			for i := range b.Workstreams[0].Owners {
				b.Workstreams[0].Owners[i].RACIRole = Consulted
			}
		}, "exactly one ACCOUNTABLE owner"},
		{"an owner names an unknown party", func(b *Blueprint) {
			b.Workstreams[0].Owners[0].Party = "VENDOR"
		}, "owners[0].party"},

		{"a workstream's prerequisites are removed", func(b *Blueprint) {
			b.Workstreams[0].Prerequisites = nil
		}, "prerequisites"},
		{"a prerequisite's evidence ref is erased", func(b *Blueprint) {
			b.Workstreams[0].Prerequisites[0].EvidenceRef = ""
		}, "prerequisites[0].evidence_ref"},
		{"a prerequisite's gate action is unknown", func(b *Blueprint) {
			b.Workstreams[0].Prerequisites[0].Gate.Action = "MAYBE"
		}, "prerequisites[0].gate.action"},

		{"input artifacts are removed", func(b *Blueprint) { b.Workstreams[0].InputArtifacts = nil }, "input_artifacts"},
		{"output artifacts are removed", func(b *Blueprint) { b.Workstreams[0].OutputArtifacts = nil }, "output_artifacts"},

		{"due offset is non-positive", func(b *Blueprint) { b.Workstreams[0].Timing.DueOffsetDays = 0 }, "timing.due_offset_days"},
		{"expiry offset is non-positive", func(b *Blueprint) { b.Workstreams[0].Timing.ExpiryOffsetDays = 0 }, "timing.expiry_offset_days"},
		{"expiry offset is earlier than due offset", func(b *Blueprint) {
			b.Workstreams[0].Timing.DueOffsetDays = 30
			b.Workstreams[0].Timing.ExpiryOffsetDays = 5
		}, "timing.expiry_offset_days"},

		{"acceptance oracle is erased", func(b *Blueprint) { b.Workstreams[0].AcceptanceOracle = "" }, "acceptance_oracle"},
		{"data-processing boundary is erased", func(b *Blueprint) { b.Workstreams[0].DataProcessingBoundary = "" }, "data_processing_boundary"},
		{"escalation is erased", func(b *Blueprint) { b.Workstreams[0].Escalation = "" }, "escalation"},
		{"fallback is erased", func(b *Blueprint) { b.Workstreams[0].Fallback = "" }, "fallback"},

		{"no signature", func(b *Blueprint) { b.Signature = nil }, "signature"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := deepCopy(base)
			tc.mutate(&b)
			violations := b.Validate()
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
