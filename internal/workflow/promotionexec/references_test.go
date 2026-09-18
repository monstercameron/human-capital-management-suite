package promotionexec

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Unit 3 (spec-compliance plan): the promotion graph must resolve against
// exactly the registries it needs -- no more, no less. It declares no
// per-node resolver, timeout-policy or compensation references: both writes
// are correctly IRREVERSIBLE by declaration (the core commit is never unwound
// and a produced hold release cannot itself be un-released), timeouts arrive
// only as WAIT specs, and every capability is statically bound. This test
// pins that steady state, so a future node that needs a published reference
// cannot slip in without a resolver to check it against (the compiler refuses
// such a node with REFERENCE_RESOLVER_REQUIRED; see WF-COMP-007).

func TestPromotionPlanNeedsNoReferenceRegistry(t *testing.T) {
	projections := map[string]func() (*workflow.CompiledWorkflow, error){
		"EXECUTE":  func() (*workflow.CompiledWorkflow, error) { return Compile() },
		"SIMULATE": func() (*workflow.CompiledWorkflow, error) { return CompileSimulation() },
	}
	for name, compile := range projections {
		plan, err := compile()
		if err != nil {
			t.Fatalf("%s: Compile: %v", name, err)
		}
		if len(plan.References) != 0 {
			t.Fatalf("%s: plan pins %d references, want none: every target is statically bound", name, len(plan.References))
		}
		for _, node := range plan.Nodes {
			if node.ResolverRef != nil || node.TimeoutPolicy != nil || node.CompensationRef != nil {
				t.Fatalf("%s: node %s declares resolver=%v timeout=%v compensation=%v, want none without a published registry",
					name, node.ID, node.ResolverRef, node.TimeoutPolicy, node.CompensationRef)
			}
		}
	}
}
