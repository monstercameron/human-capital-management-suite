package promotionexec

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Unit 1 (spec-compliance plan): the executable promotion graph was missing
// its COMPENSATE primitive. The compensate node performs the bounded
// automatic correction (budget-hold release under the original idempotency
// identity) when a downstream observation reports known-bad state, so the
// RepairPlan starts from an unencumbered position instead of a held one.
//
// The reference workflow's acknowledgement gate (SIGNAL) is intentionally
// out of scope here: no production subscriber, reader or intake exists, and
// landing the node without them would turn served completions into errors or
// indefinite parks. It ships with the signal-intake unit.

func TestPromotionCompensateNodeBindsCorrectiveCapability(t *testing.T) {
	def := Definition()
	var found *workflow.Node
	for i := range def.Nodes {
		if def.Nodes[i].ID == NodeCompensateHold {
			found = &def.Nodes[i]
		}
	}
	if found == nil {
		t.Fatalf("no %q node in the promotion definition", NodeCompensateHold)
	}
	if found.Type != workflow.StepCompensate {
		t.Fatalf("compensate node type = %s, want COMPENSATE", found.Type)
	}
	if found.Capability == nil || found.Capability.ID != capReleaseHold {
		t.Fatalf("compensate node binds %+v, want capability %s", found.Capability, capReleaseHold)
	}
	if found.DeclaredEffect != capability.EffectInternalMutation {
		t.Fatalf("compensate declared effect = %s, want INTERNAL_MUTATION for the local hold release", found.DeclaredEffect)
	}
	if found.EffectRole != workflow.RoleDownstreamEffect {
		t.Fatalf("compensate effect role = %q, want DOWNSTREAM_EFFECT: the core commit is never rolled back", found.EffectRole)
	}
	if found.Capability.IdempotencyKeyMapping == "" {
		t.Fatalf("compensate node names no idempotency key mapping; redrive must reuse the original identity")
	}
	if found.FailureRoute != NodeEndRepairPlan {
		t.Fatalf("compensate failure route = %q, want %q", found.FailureRoute, NodeEndRepairPlan)
	}
}

func TestPromotionCompensateEdges(t *testing.T) {
	def := Definition()
	edges := map[[3]string]bool{}
	for _, edge := range def.Edges {
		edges[[3]string{edge.From, edge.To, edge.RouteKey}] = true
	}
	for _, want := range [][3]string{
		// Known-bad downstream observations attempt the bounded correction;
		// indeterminate ones go straight to governed repair without
		// auto-action.
		{NodeObservePayroll, NodeCompensateHold, "FAIL"},
		{NodeObservePayroll, NodeCompensateHold, "PARTIAL"},
		{NodeObserveAccess, NodeCompensateHold, "FAIL"},
		{NodeObserveAccess, NodeCompensateHold, "PARTIAL"},
		{NodeCompensateHold, NodeEndRepairPlan, "COMPENSATED"},
		{NodeCompensateHold, NodeEndRepairPlan, "PARTIAL"},
		{NodeCompensateHold, NodeEndRepairPlan, "FAILED"},
		{NodeCompensateHold, NodeEndRepairPlan, "REPAIR_REQUIRED"},
	} {
		if !edges[want] {
			t.Errorf("missing edge %s -> %s [%s]", want[0], want[1], want[2])
		}
	}
}

// TestPromotionSimulateProjectionRetainsWrites pins the shape WF-TEST-001
// derives: the SIMULATE projection keeps the authored writes and the
// authoritative core (no relabelling overlays); the recording simulator
// captures them instead of executing them. (It replaces
// TestPromotionSimulateProjectionDowngradesCompensate, whose downgraded
// projection the WF-TEST-001 refactor deleted.)
func TestPromotionSimulateProjectionRetainsWrites(t *testing.T) {
	plan, err := CompileSimulation()
	if err != nil {
		t.Fatalf("CompileSimulation: %v", err)
	}
	hold, ok := plan.Node(NodeCompensateHold)
	if !ok {
		t.Fatalf("simulate plan carries no compensate node")
	}
	if !hold.EffectClass.IsWrite() || hold.EffectRole != workflow.RoleDownstreamEffect {
		t.Fatalf("simulate compensate = %+v, want the retained downstream write", hold)
	}
	if plan.Effects.ZeroEffect {
		t.Fatalf("simulate effects = %+v, want the retained writes", plan.Effects)
	}
	core := plan.NodesWithRole(workflow.RoleAuthoritativeCore)
	if len(core) != 1 || core[0] != NodeExecutePromotion {
		t.Fatalf("simulate cores = %v, want exactly the retained execute_promotion", core)
	}
}
