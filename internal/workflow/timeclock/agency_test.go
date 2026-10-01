package timeclock

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_WTIME_012_DefinitionUsesAgencyCapabilities(t *testing.T) {
	def := AgencyVMSDefinition(DefaultParams())
	compiled, err := CompileDefinition(def)
	if err != nil {
		t.Fatalf("compile agency definition: %v", err)
	}
	if compiled.WorkflowID != AgencyWorkflowID || compiled.Version != AgencyVersion {
		t.Fatalf("compiled identity = %s/%d", compiled.WorkflowID, compiled.Version)
	}
	if def.WorkflowID != AgencyWorkflowID || def.StartNodeID != NodeAgencyOpenTimesheet {
		t.Fatalf("identity = %s/%s", def.WorkflowID, def.StartNodeID)
	}
	seen := map[string]workflow.Node{}
	for _, node := range def.Nodes {
		seen[node.ID] = node
	}
	for _, id := range []string{NodeAgencyOpenTimesheet, NodeAgencyExport, NodeAgencyObserve, NodeAgencyEndClosed, NodeAgencyEndAmbiguous} {
		if _, ok := seen[id]; !ok {
			t.Fatalf("missing node %q", id)
		}
	}
	if seen[NodeAgencyExport].Capability == nil || seen[NodeAgencyExport].Capability.ID != CapAgencyExportToVMS {
		t.Fatalf("export capability = %#v", seen[NodeAgencyExport].Capability)
	}
	if seen[NodeAgencyObserve].Capability == nil || seen[NodeAgencyObserve].Capability.ID != CapAgencyObserveVMS {
		t.Fatalf("observe capability = %#v", seen[NodeAgencyObserve].Capability)
	}
	for _, node := range def.Nodes {
		if node.Capability != nil && node.Capability.ID == CapDispatchApprovedTime {
			t.Fatal("agency workflow must not use the employee period dispatch capability")
		}
	}
}

func TestTodo_WTIME_012_DefinitionRoutesGovernedFailures(t *testing.T) {
	def := AgencyVMSDefinition(DefaultParams())
	has := func(from, to, route string) bool {
		for _, edge := range def.Edges {
			if edge.From == from && edge.To == to && edge.RouteKey == route {
				return true
			}
		}
		return false
	}
	cases := []struct {
		name, from, to, route string
	}{
		{"closed period reopens", NodeAgencyValidate, NodeAgencyEndReopen, RouteAgencyPeriodClosed},
		{"missing submission", NodeAgencyAwaitSubmission, NodeAgencyEndMissing, "TIMED_OUT"},
		{"rejected delivery", NodeAgencyObserve, NodeAgencyEndRejected, string(workflow.OutcomeFail)},
		{"accepted delivery closes", NodeAgencyObserve, NodeAgencyEndClosed, string(workflow.OutcomePass)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !has(tc.from, tc.to, tc.route) {
				t.Fatalf("missing edge %s -> %s (%s)", tc.from, tc.to, tc.route)
			}
		})
	}
	if len(def.ApprovalRequirements) != 1 || def.ApprovalRequirements[0].ID != ApprovalAgencyTimesheet ||
		def.ApprovalRequirements[0].ResolverExpression != "HostManagerFor(assignment)" {
		t.Fatalf("approval requirement = %#v", def.ApprovalRequirements)
	}
}
