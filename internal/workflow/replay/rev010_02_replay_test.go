package replay

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_REV_010_02_Replay proves a decision recorded against a
// tenant-published threshold table refuses explicitly instead of
// recomputing against the reference table: the pinned tenant version has
// no available binding, so replay reports the table unavailable rather
// than silently evaluating different thresholds. Recording tenant
// revisions in attempts (so replay can rebuild the exact tenant table)
// is follow-up work outside this issue.
func TestTodo_REV_010_02_Replay(t *testing.T) {
	plan := promotionPlan(t)
	rec := promotionRecord(t, plan)
	art, _ := rec.NodeInput(workflow.PromotionNodeRaiseThreshold, 1)
	decision, _ := plan.Node(workflow.PromotionNodeRaiseThreshold)
	cand := RulesDecisionCandidate{Bindings: []RuleTableBinding{PromotionThresholdBinding()}}

	tenant, err := rules.PromotionApprovalThresholdTableForTenant(
		rules.TenantThresholdScope{Tenant: "tenant-a"},
		rules.TenantThresholdResolution{FinanceText: "5.0000", ExecutiveText: "15.0000", Revision: 7, DefinitionVersion: "tenant-params/v7", Found: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	tenantDigest, err := tenant.Digest()
	if err != nil {
		t.Fatal(err)
	}
	pinned := art.Clone()
	for i := range pinned.Versions {
		if pinned.Versions[i].Kind == runtime.PinnedVersionRuleTable {
			pinned.Versions[i].Version = tenant.Version
			pinned.Versions[i].Digest = tenantDigest
		}
	}
	_, err = cand.Recompute(context.Background(), CandidateRequest{Node: decision, Attempt: 1, Inputs: pinned})
	if CodeOf(err) != CodeArtifactUnavailable || !strings.Contains(err.Error(), "tenant-params/v7") {
		t.Fatalf("tenant pin: code = %q (%v), want explicit unavailability citing the tenant version", CodeOf(err), err)
	}
}
