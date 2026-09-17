package leave

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/leavereturn"
)

func firewallEngines() []EngineClaim {
	return []EngineClaim{
		{Engine: "leave.eligibility", Verdict: VerdictKeepDomainLocal},
		{Engine: "leave.replan.partial", Verdict: VerdictInsufficientEvidence},
	}
}

// TestTodo_NEXT_007_Property: the firewall report is deterministic —
// repeated runs agree, node order never matters, and verdict gating is
// exact: EXTRACT passes only with a counterexample outside leave.
func TestTodo_NEXT_007_Property(t *testing.T) {
	base := leavereturn.ReferenceDefinition()
	first, err := CheckGraph(base, firewallEngines())
	if err != nil {
		t.Fatal(err)
	}
	second, err := CheckGraph(base, firewallEngines())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("identical closures produced different digests")
	}
	reordered := cloneDefinition(base)
	for i, j := 0, len(reordered.Nodes)-1; i < j; i, j = i+1, j-1 {
		reordered.Nodes[i], reordered.Nodes[j] = reordered.Nodes[j], reordered.Nodes[i]
	}
	shuffled, err := CheckGraph(reordered, firewallEngines())
	if err != nil {
		t.Fatal(err)
	}
	if shuffled.Digest != first.Digest {
		t.Fatal("node order changed the firewall digest")
	}
	// An EXTRACT with a genuine cross-domain counterexample passes, while
	// one citing only leave does not.
	extract, err := CheckGraph(base, []EngineClaim{{
		Engine: "leave.eligibility", Verdict: VerdictExtract,
		CounterexampleDomains: []string{"leave", "termination"},
	}})
	if err != nil {
		t.Fatalf("counterexample-backed EXTRACT rejected: %v", err)
	}
	if extract.VerdictCount != 1 {
		t.Fatalf("verdict count = %d", extract.VerdictCount)
	}
	_, err = CheckGraph(base, []EngineClaim{{
		Engine: "leave.eligibility", Verdict: VerdictExtract,
		CounterexampleDomains: []string{"leave"},
	}})
	var firewallErr *FirewallError
	if !errors.As(err, &firewallErr) {
		t.Fatalf("leave-only EXTRACT error = %v", err)
	}
	// Capabilities deduplicate: invoking one double twice still reports it
	// once, keeping the report a set rather than a log.
	doubled := mutateNode(t, base, leavereturn.NodeResolveReadiness, func(n *workflow.Node) {
		n.Capability.ID = leavereturn.CapReadEmploymentAuthority
	})
	deduped, err := CheckGraph(doubled, firewallEngines())
	if err != nil {
		t.Fatal(err)
	}
	if len(deduped.CapabilityIDs) != len(first.CapabilityIDs)-1 {
		t.Fatalf("capability ids = %v", deduped.CapabilityIDs)
	}
}

// TestTodo_NEXT_007_Golden: the vetted CONF-004 closure shape is pinned.
// Drift in the reference workflow fails here until re-vetted by hand.
func TestTodo_NEXT_007_Golden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "graph.golden"))
	if err != nil {
		t.Fatal(err)
	}
	golden := string(raw)
	for _, want := range []string{
		"workflow_id: hcmnext.workflows.leave_and_return",
		"version: 1",
		"node_count: 14",
		"verdict_count: 2",
		"hcmnext.conformance.leavereturn.observe_benefits_continuation",
		"hcmnext.conformance.leavereturn.read_employment_and_authority",
		"hcmnext.conformance.leavereturn.resolve_leave_programs",
		"hcmnext.conformance.leavereturn.resolve_return_readiness",
	} {
		if !strings.Contains(golden, want) {
			t.Fatalf("golden is missing %q", want)
		}
	}
	report, err := CheckGraph(leavereturn.ReferenceDefinition(), firewallEngines())
	if err != nil {
		t.Fatal(err)
	}
	if report.NodeCount != 14 || len(report.CapabilityIDs) != 4 || report.VerdictCount != 2 {
		t.Fatalf("report drifted from golden: %+v", report)
	}
	if !strings.Contains(golden, "digest: "+report.Digest) {
		t.Fatalf("report digest %q is not the vetted golden digest", report.Digest)
	}
}

// TestTodo_NEXT_007_Security: a hostile closure combining every escape —
// execute mode, a provider capability and a write effect — is rejected
// with every applicable rule, and a zero definition fails closed instead
// of panicking.
func TestTodo_NEXT_007_Security(t *testing.T) {
	hostile := cloneDefinition(leavereturn.ReferenceDefinition())
	hostile.DeclaredModes = []workflow.ExecutionMode{workflow.ModeExecute}
	hostile.TerminalProfile = workflow.TerminalProfileExecute
	for i := range hostile.Nodes {
		if hostile.Nodes[i].ID == leavereturn.NodeReadEmploymentAuthority {
			hostile.Nodes[i].Capability.ID = "hcmnext.provider.legal.delivery"
			hostile.Nodes[i].Capability.OperationMode = workflow.ModeExecute
			hostile.Nodes[i].DeclaredEffect = capability.EffectIrreversibleExternalMutation
		}
	}
	_, err := CheckGraph(hostile, []EngineClaim{{Engine: "x", Verdict: "EXECUTE_ANYWAY"}})
	var firewallErr *FirewallError
	if !errors.As(err, &firewallErr) {
		t.Fatalf("error = %v, want *FirewallError", err)
	}
	seen := make(map[string]bool, len(firewallErr.Violations))
	for _, violation := range firewallErr.Violations {
		seen[violation.Rule] = true
	}
	for _, rule := range []string{RuleSimulateOnly, RuleContractDoublesOnly, RuleNoWrites, RuleEngineVerdictClosed} {
		if !seen[rule] {
			t.Fatalf("hostile closure escaped rule %q: %+v", rule, firewallErr.Violations)
		}
	}
	var zeroErr *FirewallError
	if _, err := CheckGraph(workflow.Definition{}, nil); !errors.As(err, &zeroErr) {
		t.Fatalf("zero definition error = %v", err)
	}
	if len(zeroErr.Violations) == 0 {
		t.Fatal("zero definition produced no violations")
	}
}
