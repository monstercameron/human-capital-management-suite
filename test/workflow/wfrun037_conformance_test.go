package workflow_test

import (
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/benefits"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/hrcase"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/learning"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/managerchange"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/mobility"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/talent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/termination"
	timeconf "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/time"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/transfer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

type registryFunc func() (*capability.Registry, error)

func compileWith(registry registryFunc, compile func(workflow.CapabilityResolver) (*workflow.CompiledWorkflow, error)) func() (*workflow.CompiledWorkflow, error) {
	return func() (*workflow.CompiledWorkflow, error) {
		r, err := registry()
		if err != nil {
			return nil, err
		}
		return compile(r)
	}
}

// TestTodo_WF_RUN_037_Conformance compiles every published workflow
// definition in the repository -- the executable promotion plan and its
// simulation projection, the approval prototype and every reference
// conformance workflow -- and proves each one satisfies the WF-RUN-037
// classification: every write effect carries a role its effect class admits,
// no read carries one, every dependent node has a failure route, the summary
// agrees with the nodes, and the promotion commit is the one authoritative
// core.
func TestTodo_WF_RUN_037_Conformance(t *testing.T) {
	definitions := []struct {
		name    string
		compile func() (*workflow.CompiledWorkflow, error)
	}{
		{"promotionexec.execute", func() (*workflow.CompiledWorkflow, error) { return promotionexec.Compile() }},
		{"promotionexec.simulate", func() (*workflow.CompiledWorkflow, error) { return promotionexec.CompileSimulation() }},
		{"prototype.approval", prototype.CompileApproval},
		{"conformance.benefits", compileWith(benefits.GoldenEnvironment().Registry, benefits.Compile)},
		{"conformance.hrcase", compileWith(hrcase.GoldenEnvironment().Registry, hrcase.Compile)},
		{"conformance.learning", compileWith(learning.GoldenEnvironment().Registry, learning.Compile)},
		{"conformance.managerchange", compileWith(managerchange.NewEnvironment().Registry, func(r workflow.CapabilityResolver) (*workflow.CompiledWorkflow, error) {
			return managerchange.Compile(r)
		})},
		{"conformance.mobility", compileWith(mobility.GoldenEnvironment().Registry, mobility.Compile)},
		{"conformance.payroll", compileWith(payroll.GoldenEnvironment().Registry, payroll.Compile)},
		{"conformance.talent", compileWith(talent.GoldenEnvironment().Registry, talent.Compile)},
		{"conformance.termination", compileWith(termination.GoldenEnvironment().Registry, termination.Compile)},
		{"conformance.time", compileWith(timeconf.GoldenEnvironment().Registry, timeconf.Compile)},
		{"conformance.transfer", compileWith(transfer.GoldenEnvironment().Registry, transfer.Compile)},
	}
	writes := 0
	for _, def := range definitions {
		t.Run(def.name, func(t *testing.T) {
			plan, err := def.compile()
			if err != nil {
				t.Fatalf("published definition no longer compiles: %v", err)
			}
			if err := plan.Verify(); err != nil {
				t.Fatalf("plan does not verify: %v", err)
			}
			byRole := map[string][]string{}
			for _, n := range plan.Nodes {
				if !n.EffectClass.IsWrite() {
					if n.EffectRole != "" {
						t.Fatalf("node %s is %s but carries role %s", n.ID, n.EffectClass, n.EffectRole)
					}
					continue
				}
				writes++
				admitted := false
				for _, r := range workflow.AdmittedEffectRoles(n.EffectClass) {
					admitted = admitted || r == n.EffectRole
				}
				if !admitted {
					t.Fatalf("node %s is %s with role %q, admitted %v", n.ID, n.EffectClass, n.EffectRole, workflow.AdmittedEffectRoles(n.EffectClass))
				}
				if n.EffectRole != workflow.RoleAuthoritativeCore && n.FailureRoute == "" {
					t.Fatalf("dependent node %s (%s) has no failure route", n.ID, n.EffectRole)
				}
				byRole[string(n.EffectRole)] = append(byRole[string(n.EffectRole)], n.ID)
			}
			for k := range byRole {
				sort.Strings(byRole[k])
			}
			if len(byRole) != len(plan.Effects.NodesByRole) {
				t.Fatalf("NodesByRole %v disagrees with nodes %v", plan.Effects.NodesByRole, byRole)
			}
			for k, ids := range byRole {
				got := plan.Effects.NodesByRole[k]
				if len(got) != len(ids) {
					t.Fatalf("NodesByRole[%s] = %v, nodes say %v", k, got, ids)
				}
				for i := range ids {
					if got[i] != ids[i] {
						t.Fatalf("NodesByRole[%s] = %v, nodes say %v", k, got, ids)
					}
				}
			}
			if def.name == "promotionexec.execute" {
				cores := plan.NodesWithRole(workflow.RoleAuthoritativeCore)
				if len(cores) != 1 || cores[0] != promotionexec.NodeExecutePromotion || len(plan.Effects.NodesByRole) != 1 {
					t.Fatalf("promotion roles = %v, want %s as the only classified write, the AUTHORITATIVE_CORE", plan.Effects.NodesByRole, promotionexec.NodeExecutePromotion)
				}
			}
		})
	}
	if writes == 0 {
		t.Fatal("no published definition carries a write effect; the conformance sweep proves nothing")
	}
}
