package threatregister

import (
	"strings"
	"testing"
)

// TestTodo_THREAT_001_Property is THREAT-001's PROPERTY test. It proves the
// GREEN invariant - every RED element Validate checks for actually causes a
// violation naming the exact broken field when removed, and a structurally
// complete fixture with none of those defects validates clean - holds
// across the whole schema, not just the one checked-in file. It also proves
// attack-class and test-kind coverage are driven off [AllAttackClasses] and
// [AllTestKinds] rather than a hardcoded list: it iterates the taxonomy
// itself to build each "drop one class" case, so adding a tenth attack
// class to the taxonomy would automatically gain a case here without any
// edit to this test.
func TestTodo_THREAT_001_Property(t *testing.T) {
	base := validFixture()
	if v := base.Validate(); len(v) != 0 {
		t.Fatalf("validFixture() must validate clean, got: %v", v)
	}

	cases := []struct {
		name    string
		mutate  func(*Register)
		wantHit string
	}{
		{"wrong todo id", func(r *Register) { r.TodoID = "THREAT-002" }, "todo_id"},
		{"missing signed date", func(r *Register) { r.SignedDate = "" }, "signed_date"},
		{"no slices", func(r *Register) { r.Slices = nil }, "slices"},

		{"no actors", func(r *Register) { r.Slices[0].Actors = nil }, "actors"},
		{"no assets", func(r *Register) { r.Slices[0].Assets = nil }, "assets"},
		{"no trust boundaries", func(r *Register) { r.Slices[0].TrustBoundaries = nil }, "trust_boundaries"},
		{"no entry points", func(r *Register) { r.Slices[0].EntryPoints = nil }, "entry_points"},
		{"no edges", func(r *Register) { r.Slices[0].Edges = nil }, "edges"},
		{"no threats", func(r *Register) { r.Slices[0].Threats = nil }, "threats"},
		{"no mitigations", func(r *Register) { r.Slices[0].Mitigations = nil }, "mitigations"},

		{"asset missing data class", func(r *Register) { r.Slices[0].Assets[0].DataClass = "MADE_UP" }, "data_class"},
		{"edge references unknown actor", func(r *Register) { r.Slices[0].Edges[0].Actor = "NOPE" }, "references unknown actor"},
		{"edge references unknown asset", func(r *Register) { r.Slices[0].Edges[0].Asset = "NOPE" }, "references unknown asset"},

		{"threat unknown attack class", func(r *Register) { r.Slices[0].Threats[0].AttackClass = "MADE_UP" }, "attack_class"},
		{"threat missing scenario", func(r *Register) { r.Slices[0].Threats[0].Scenario = "" }, "scenario"},
		{"threat unknown severity", func(r *Register) { r.Slices[0].Threats[0].Severity = "MADE_UP" }, "severity"},
		{"threat missing detection", func(r *Register) { r.Slices[0].Threats[0].Detection = "" }, "detection"},
		{"threat missing recovery", func(r *Register) { r.Slices[0].Threats[0].Recovery = "" }, "recovery"},
		{"threat missing owner", func(r *Register) { r.Slices[0].Threats[0].Owner = "" }, "owner"},
		{"threat no consuming edges", func(r *Register) { r.Slices[0].Threats[0].ConsumingEdges = nil }, "consuming_edges"},
		{"threat references unknown edge", func(r *Register) { r.Slices[0].Threats[0].ConsumingEdges = []string{"NOPE"} }, "references unknown edge"},
		{"threat no tests", func(r *Register) { r.Slices[0].Threats[0].Tests = nil }, "tests"},
		{"threat test unknown kind", func(r *Register) { r.Slices[0].Threats[0].Tests[0].Kind = "MADE_UP" }, "kind"},
		{"threat test missing name", func(r *Register) { r.Slices[0].Threats[0].Tests[0].Name = "" }, "name"},
		{"threat references unknown mitigation", func(r *Register) { r.Slices[0].Threats[0].Mitigations = []string{"NOPE"} }, "references unknown mitigation"},
		{"threat has neither mitigation nor residual risk", func(r *Register) { r.Slices[0].Threats[0].Mitigations = nil }, "no mitigation and no residual risk"},

		{"mitigation missing description", func(r *Register) { r.Slices[0].Mitigations[0].Description = "" }, "description"},
		{"mitigation drops a consuming edge it is still named by", func(r *Register) {
			r.Slices[0].Mitigations[0].ConsumingEdges = r.Slices[0].Mitigations[0].ConsumingEdges[:1]
		}, "does not retain consuming edge"},

		{"no signature", func(r *Register) { r.Signature = nil }, "signature"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := deepCopy(base)
			tc.mutate(&r)
			violations := r.Validate()
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

// TestTodo_THREAT_001_Property_AttackClassCoverageIsDrivenByTheTaxonomy
// proves attack-class total coverage is checked against [AllAttackClasses]
// itself, not a second hardcoded list: for every class in the taxonomy, it
// removes the one threat carrying that class from a valid fixture and
// asserts Validate reports it missing by name - a test written against a
// literal list of nine strings would not automatically extend if the
// taxonomy grew a tenth class, but this one does because it iterates
// AllAttackClasses().
func TestTodo_THREAT_001_Property_AttackClassCoverageIsDrivenByTheTaxonomy(t *testing.T) {
	for _, class := range AllAttackClasses() {
		t.Run(string(class), func(t *testing.T) {
			r := deepCopy(validFixture())
			s := &r.Slices[0]
			var kept []Threat
			for _, th := range s.Threats {
				if th.AttackClass != string(class) {
					kept = append(kept, th)
				}
			}
			if len(kept) != len(s.Threats)-1 {
				t.Fatalf("fixture did not carry exactly one threat for class %s", class)
			}
			s.Threats = kept

			violations := r.Validate()
			found := false
			for _, v := range violations {
				if strings.Contains(v.String(), "ignores attack class "+string(class)) {
					found = true
				}
			}
			if !found {
				t.Errorf("removing the only threat covering %s did not produce an 'ignores attack class' violation naming it: %v", class, violations)
			}
		})
	}
}

// TestTodo_THREAT_001_Property_TestKindCoverageIsDrivenByTheTaxonomy is the
// same proof for [AllTestKinds]: removing every test of one kind from the
// fixture must produce a violation naming that kind, for each kind in the
// taxonomy.
func TestTodo_THREAT_001_Property_TestKindCoverageIsDrivenByTheTaxonomy(t *testing.T) {
	for _, kind := range AllTestKinds() {
		t.Run(kind, func(t *testing.T) {
			r := deepCopy(validFixture())
			s := &r.Slices[0]
			for i := range s.Threats {
				var kept []ThreatTest
				for _, tst := range s.Threats[i].Tests {
					if tst.Kind != kind {
						kept = append(kept, tst)
					}
				}
				s.Threats[i].Tests = kept
			}

			violations := r.Validate()
			found := false
			for _, v := range violations {
				if strings.Contains(v.String(), "no test of kind "+kind) {
					found = true
				}
			}
			if !found {
				t.Errorf("removing every test of kind %s did not produce a violation naming it: %v", kind, violations)
			}
		})
	}
}
