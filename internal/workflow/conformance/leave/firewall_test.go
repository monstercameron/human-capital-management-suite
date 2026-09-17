package leave

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/leavereturn"
)

func firewallBase(t *testing.T) workflow.Definition {
	t.Helper()
	return leavereturn.ReferenceDefinition()
}

func cloneDefinition(def workflow.Definition) workflow.Definition {
	out := def
	out.Nodes = append([]workflow.Node(nil), def.Nodes...)
	out.DeclaredModes = append([]workflow.ExecutionMode(nil), def.DeclaredModes...)
	return out
}

func mutateNode(t *testing.T, def workflow.Definition, id string, mutate func(*workflow.Node)) workflow.Definition {
	t.Helper()
	out := cloneDefinition(def)
	for i := range out.Nodes {
		if out.Nodes[i].ID == id {
			mutate(&out.Nodes[i])
			return out
		}
	}
	t.Fatalf("node %q not found", id)
	return out
}

// TestLeaveConformanceGraphHasNoGateABOrPhase2ImplementationDependency is
// NEXT-007: the CONF-004 leave-and-return closure must stay a hypothetical
// conformance fixture. The real reference graph passes every firewall rule,
// and every hostile mutation — a structural implementation node, a runtime or
// provider capability, an execute mode, a write effect, an UNKNOWN default,
// an unpinned legal context or an evidence-free engine extraction — fails
// with its exact rule.
func TestLeaveConformanceGraphHasNoGateABOrPhase2ImplementationDependency(t *testing.T) {
	base := firewallBase(t)
	report, err := CheckGraph(base, []EngineClaim{
		{Engine: "leave.eligibility", Verdict: VerdictKeepDomainLocal},
		{Engine: "leave.replan.partial", Verdict: VerdictInsufficientEvidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.WorkflowID != leavereturn.WorkflowID || report.Version != leavereturn.Version {
		t.Fatalf("report identity = %q v%d", report.WorkflowID, report.Version)
	}
	if report.NodeCount != len(base.Nodes) || report.NodeCount == 0 {
		t.Fatalf("node count = %d", report.NodeCount)
	}
	for _, id := range report.CapabilityIDs {
		if !isContractDouble(id) {
			t.Fatalf("non-double capability admitted: %q", id)
		}
	}
	if report.Digest == "" {
		t.Fatal("report carries no digest")
	}

	cases := []struct {
		name string
		rule string
		def  func(t *testing.T) workflow.Definition
		want []EngineClaim
	}{
		{
			name: "join reaches implementation",
			rule: RuleNoStructuralImplementation,
			def: func(t *testing.T) workflow.Definition {
				out := cloneDefinition(firewallBase(t))
				out.Nodes = append(out.Nodes, workflow.Node{ID: "join_impl", Type: workflow.StepJoin})
				return out
			},
		},
		{
			name: "subworkflow reaches implementation",
			rule: RuleNoStructuralImplementation,
			def: func(t *testing.T) workflow.Definition {
				out := cloneDefinition(firewallBase(t))
				out.Nodes = append(out.Nodes, workflow.Node{ID: "sub_impl", Type: workflow.StepSubworkflow})
				return out
			},
		},
		{
			name: "provider capability is not a contract double",
			rule: RuleContractDoublesOnly,
			def: func(t *testing.T) workflow.Definition {
				return mutateNode(t, firewallBase(t), leavereturn.NodeReadEmploymentAuthority, func(n *workflow.Node) {
					n.Capability.ID = "hcmnext.provider.sms.send"
				})
			},
		},
		{
			name: "execute mode escapes simulation",
			rule: RuleSimulateOnly,
			def: func(t *testing.T) workflow.Definition {
				return mutateNode(t, firewallBase(t), leavereturn.NodeReadEmploymentAuthority, func(n *workflow.Node) {
					n.Capability.OperationMode = workflow.ModeExecute
				})
			},
		},
		{
			name: "definition claiming execute",
			rule: RuleSimulateOnly,
			def: func(t *testing.T) workflow.Definition {
				out := cloneDefinition(firewallBase(t))
				out.DeclaredModes = []workflow.ExecutionMode{workflow.ModeSimulate, workflow.ModeExecute}
				return out
			},
		},
		{
			name: "write effect mutates state",
			rule: RuleNoWrites,
			def: func(t *testing.T) workflow.Definition {
				return mutateNode(t, firewallBase(t), leavereturn.NodeReadEmploymentAuthority, func(n *workflow.Node) {
					n.DeclaredEffect = capability.EffectExternalMutation
				})
			},
		},
		{
			name: "mutation binding mutates state",
			rule: RuleNoWrites,
			def: func(t *testing.T) workflow.Definition {
				return mutateNode(t, firewallBase(t), leavereturn.NodeReadEmploymentAuthority, func(n *workflow.Node) {
					n.Capability.IdempotencyKeyMapping = "worker_id"
				})
			},
		},
		{
			name: "unknown default",
			rule: RuleNoUnknownDefault,
			def: func(t *testing.T) workflow.Definition {
				return mutateNode(t, firewallBase(t), leavereturn.NodeEvidenceReviewDecision, func(n *workflow.Node) {
					n.Decision.DefaultRoute = "UNKNOWN"
				})
			},
		},
		{
			name: "unpinned legal context claims real jurisdiction",
			rule: RulePinnedJurisdiction,
			def: func(t *testing.T) workflow.Definition {
				return mutateNode(t, firewallBase(t), leavereturn.NodeResolveLeavePrograms, func(n *workflow.Node) {
					for i := range n.RequiredContext {
						n.RequiredContext[i].Pinned = false
						n.RequiredContext[i].RequiredWatermarks = nil
					}
				})
			},
		},
		{
			name: "engine extraction without counterexamples",
			rule: RuleEngineVerdictClosed,
			def:  func(t *testing.T) workflow.Definition { return firewallBase(t) },
			want: []EngineClaim{{Engine: "leave.eligibility", Verdict: VerdictExtract}},
		},
		{
			name: "unknown engine verdict",
			rule: RuleEngineVerdictClosed,
			def:  func(t *testing.T) workflow.Definition { return firewallBase(t) },
			want: []EngineClaim{{Engine: "leave.eligibility", Verdict: "SHIP_IT"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CheckGraph(tc.def(t), tc.want)
			var firewallErr *FirewallError
			if !errors.As(err, &firewallErr) {
				t.Fatalf("error = %v, want *FirewallError", err)
			}
			found := false
			for _, violation := range firewallErr.Violations {
				if violation.Rule == tc.rule {
					found = true
				}
			}
			if !found {
				t.Fatalf("violations = %+v, want rule %q", firewallErr.Violations, tc.rule)
			}
		})
	}
}
