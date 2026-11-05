package rules_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestTenantLatticeMatchesWorkflow pins the rules tenant lattice to the
// workflow lattice: the two must agree on every dominance pair, because
// rules cannot import workflow and carries its own copy.
func TestTenantLatticeMatchesWorkflow(t *testing.T) {
	ruleLabels := []rules.TenantDataClassification{rules.TenantPublic, rules.TenantInternal, rules.TenantConfidential, rules.TenantRestricted}
	flowLabels := []workflow.DataClassification{workflow.DataPublic, workflow.DataInternal, workflow.DataConfidential, workflow.DataRestricted}
	for i, r := range ruleLabels {
		for j, s := range ruleLabels {
			if got, want := r.Dominates(s), flowLabels[i].Dominates(flowLabels[j]); got != want {
				t.Fatalf("dominance %d/%d disagrees: rules=%v workflow=%v", i, j, got, want)
			}
		}
		if _, err := rules.ParseTenantDataClassification(string(flowLabels[i])); err != nil {
			t.Fatalf("workflow label %q rejected: %v", flowLabels[i], err)
		}
	}
	if _, err := rules.ParseTenantDataClassification("CONFIDENTIAL_HR"); err == nil {
		t.Fatal("qualified label accepted")
	}
}
