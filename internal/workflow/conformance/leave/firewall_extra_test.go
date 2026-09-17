package leave

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func firewallFaultEngines() []EngineClaim {
	return []EngineClaim{
		{Engine: "leave.eligibility", Verdict: VerdictKeepDomainLocal},
		{Engine: "leave.replan.partial", Verdict: VerdictInsufficientEvidence},
	}
}

func firewallViolationRule(t *testing.T, err error) string {
	t.Helper()
	var firewall *FirewallError
	if !errors.As(err, &firewall) {
		t.Fatalf("err=%v, want *FirewallError", err)
	}
	if len(firewall.Violations) == 0 {
		t.Fatal("firewall error names no violation")
	}
	return firewall.Violations[0].Rule
}

// TestTodo_NEXT_007_Fault proves execution-shaped faults fail closed: an
// execute-mode capability and a write effect never pass the firewall.
func TestTodo_NEXT_007_Fault(t *testing.T) {
	executing := mutateNode(t, firewallBase(t), "read_employment_and_authority", func(node *workflow.Node) {
		node.Capability.OperationMode = workflow.ModeExecute
	})
	_, err := CheckGraph(executing, firewallFaultEngines())
	if err == nil {
		t.Fatal("execute-mode capability passed the firewall")
	}
	if rule := firewallViolationRule(t, err); rule != RuleSimulateOnly {
		t.Fatalf("rule=%q, want %q", rule, RuleSimulateOnly)
	}

	writing := mutateNode(t, firewallBase(t), "read_employment_and_authority", func(node *workflow.Node) {
		node.Capability.EffectBinding = "writes:employment"
	})
	_, err = CheckGraph(writing, firewallFaultEngines())
	if err == nil {
		t.Fatal("write-bound capability passed the firewall")
	}
	if rule := firewallViolationRule(t, err); rule != RuleNoWrites {
		t.Fatalf("rule=%q, want %q", rule, RuleNoWrites)
	}
}

// TestTodo_NEXT_007_Conformance proves the verdict is deterministic and
// complete: identical closures seal identical reports and every verdict is
// counted.
func TestTodo_NEXT_007_Conformance(t *testing.T) {
	first, err := CheckGraph(firewallBase(t), firewallFaultEngines())
	if err != nil {
		t.Fatal(err)
	}
	second, err := CheckGraph(firewallBase(t), firewallFaultEngines())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("firewall verdict is not deterministic")
	}
	if first.VerdictCount != len(firewallFaultEngines()) {
		t.Fatalf("verdicts=%d, want %d", first.VerdictCount, len(firewallFaultEngines()))
	}
	if first.NodeCount != len(firewallBase(t).Nodes) {
		t.Fatalf("nodes=%d, want %d", first.NodeCount, len(firewallBase(t).Nodes))
	}
}

// TestTodo_NEXT_007_Mutation proves mutated verdicts fail closed: an
// evidence-free EXTRACT and an undeclared verdict never publish.
func TestTodo_NEXT_007_Mutation(t *testing.T) {
	_, err := CheckGraph(firewallBase(t), []EngineClaim{
		{Engine: "leave.eligibility", Verdict: VerdictExtract},
	})
	if err == nil {
		t.Fatal("evidence-free EXTRACT published")
	}
	if rule := firewallViolationRule(t, err); rule != RuleEngineVerdictClosed {
		t.Fatalf("rule=%q, want %q", rule, RuleEngineVerdictClosed)
	}

	_, err = CheckGraph(firewallBase(t), []EngineClaim{
		{Engine: "leave.eligibility", Verdict: EngineVerdict("SOME_DAY")},
	})
	if err == nil {
		t.Fatal("undeclared verdict published")
	}
	if rule := firewallViolationRule(t, err); rule != RuleEngineVerdictClosed {
		t.Fatalf("rule=%q, want %q", rule, RuleEngineVerdictClosed)
	}
}
