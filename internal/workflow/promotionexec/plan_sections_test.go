package promotionexec

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Unit 2 (spec-compliance plan): the published promotion plan must pin every
// section the runtime depends on, in one digest, with compiler-placed safe
// points before every write. This inventory proves the whole surface at once
// so a future graph change cannot silently drop a section the digest used to
// pin.

func TestPromotionCompiledPlanPinsEverySection(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := plan.Verify(); err != nil {
		t.Fatalf("plan does not verify against its own digest: %v", err)
	}
	if plan.CompilerVersion == "" {
		t.Fatalf("plan carries no compiler version; the fingerprint pins nothing")
	}
	if len(plan.Reachability.Order) != len(plan.Nodes) {
		t.Fatalf("reachability covers %d of %d nodes", len(plan.Reachability.Order), len(plan.Nodes))
	}
	if len(plan.Nodes) > int(plan.Limits.MaxNodes) {
		t.Fatalf("%d nodes exceed the declared max %d", len(plan.Nodes), plan.Limits.MaxNodes)
	}
	for _, node := range plan.Nodes {
		if plan.Reachability.Depth[node.ID] > plan.Limits.MaxDepth {
			t.Fatalf("node %s depth %d exceeds max %d", node.ID, plan.Reachability.Depth[node.ID], plan.Limits.MaxDepth)
		}
	}
	cores := plan.NodesWithRole(workflow.RoleAuthoritativeCore)
	downstream := plan.NodesWithRole(workflow.RoleDownstreamEffect)
	if len(cores) != 1 || cores[0] != NodeExecutePromotion {
		t.Fatalf("cores = %v, want exactly the authoritative commit", cores)
	}
	if len(downstream) != 1 || downstream[0] != NodeCompensateHold {
		t.Fatalf("downstream = %v, want exactly the bounded correction", downstream)
	}
	// Every write-effect node carries a compiler-placed safe point, so an
	// operator can pause before either mutation. Terminals carry their own.
	for _, id := range []string{NodeExecutePromotion, NodeCompensateHold} {
		node, ok := plan.Node(id)
		if !ok || !node.SafePoint {
			t.Fatalf("node %s safe point = %v, want a compiler-placed pause before the write", id, ok && node.SafePoint)
		}
	}
	// The correction's governance survives compilation: bound approvals and
	// the pre-effect revalidation boundary the served path re-checks.
	compensate, _ := plan.Node(NodeCompensateHold)
	if len(compensate.Governance.ApprovalRequirements) != 2 {
		t.Fatalf("compensate approvals = %v, want the finance and manager binding", compensate.Governance.ApprovalRequirements)
	}
	if boundary, ok := plan.Governance.RevalidationPoints[NodeCompensateHold]; !ok || boundary != workflow.RevalidatePreEffect {
		t.Fatalf("compensate revalidation = %q, want PRE_EFFECT", boundary)
	}
	// The repair terminal the correction routes to still carries its
	// RepairPlan reference; the plan's closure policy did not lose it.
	repair, ok := plan.Node(NodeEndRepairPlan)
	if !ok || repair.Terminal == nil || len(repair.Terminal.RepairRefs) == 0 {
		t.Fatalf("repair terminal = %+v, want the bounded RepairPlan reference", repair)
	}
	if plan.FailurePolicyRef == "" || plan.CancellationPolicyRef == "" ||
		plan.RetentionPolicyRef == "" || plan.MigrationPolicyRef == "" {
		t.Fatalf("plan policies = %q/%q/%q/%q, want all four pinned",
			plan.FailurePolicyRef, plan.CancellationPolicyRef, plan.RetentionPolicyRef, plan.MigrationPolicyRef)
	}
	// No PARALLEL node and no external mutation: the absent concurrency
	// section is the documented steady state, not a missing analysis.
	if plan.Concurrency != nil {
		t.Fatalf("concurrency section = %+v, want nil for a sequential internal-mutation plan", plan.Concurrency)
	}
}
