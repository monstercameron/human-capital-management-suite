package timeclock

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_WTIME_005(t *testing.T) {
	def := PeriodTimecardDefinition(DefaultParams())
	if def.WorkflowID != PeriodWorkflowID || def.StartNodeID != NodeAwaitPeriodTrigger {
		t.Fatalf("period identity = %q/%q, start %q", def.WorkflowID, def.IntentType, def.StartNodeID)
	}

	nodes := make(map[string]workflow.Node, len(def.Nodes))
	for _, node := range def.Nodes {
		nodes[node.ID] = node
	}
	for _, want := range []struct {
		id    string
		type_ workflow.StepType
	}{
		{NodeFoldPeriod, workflow.StepTransform},
		{NodeAttestTimecard, workflow.StepTask},
		{NodeApproveTimecard, workflow.StepApproval},
		{NodeDispatchApprovedTime, workflow.StepCapability},
		{NodeAwaitDestinationAcceptance, workflow.StepSignal},
		{NodeAwaitPeriodReopen, workflow.StepSignal},
		{NodeEndPeriodClosed, workflow.StepEnd},
	} {
		if got, ok := nodes[want.id]; !ok || got.Type != want.type_ {
			t.Fatalf("node %q = %#v, want type %s", want.id, got, want.type_)
		}
	}
	if nodes[NodeFoldPeriod].Metadata[MetadataEngineGap] != GapReducingJoin {
		t.Fatalf("fold metadata = %#v, want reducing-join gap", nodes[NodeFoldPeriod].Metadata)
	}
	if nodes[NodeAwaitDestinationAcceptance].Signal.EventType != EventTypeDestinationAccepted {
		t.Fatalf("destination signal = %#v", nodes[NodeAwaitDestinationAcceptance].Signal)
	}
	if nodes[NodeAwaitPeriodReopen].Signal.EventType != EventTypePeriodReopen {
		t.Fatalf("reopen signal = %#v", nodes[NodeAwaitPeriodReopen].Signal)
	}
}

func TestTodo_WTIME_005_Golden(t *testing.T) {
	def := PeriodTimecardDefinition(DefaultParams())
	want := map[string]string{
		NodeAwaitPeriodTrigger:         NodeCollectPeriodInputs,
		NodeCollectPeriodInputs:        NodeFoldPeriod,
		NodeFoldPeriod:                 NodeComputePeriodPremiums,
		NodeComputePeriodPremiums:      NodeAttestTimecard,
		NodeAttestTimecard:             NodePeriodReady,
		NodeSelectPeriodDestination:    NodeApproveTimecard,
		NodeApproveTimecard:            NodeLockPeriodTimecard,
		NodeLockPeriodTimecard:         NodeDispatchApprovedTime,
		NodeDispatchApprovedTime:       NodeAwaitDestinationAcceptance,
		NodeAwaitDestinationAcceptance: NodeObserveDestination,
		NodeObserveDestination:         NodeAwaitPeriodReopen,
		NodeAwaitPeriodReopen:          NodeClassifyPeriodReopen,
	}
	for from, to := range want {
		if !periodEdgeExists(def.Edges, from, to) {
			t.Fatalf("missing golden edge %s -> %s", from, to)
		}
	}
	if !periodEdgeExists(def.Edges, NodeClassifyPeriodReopen, NodeRefoldPeriodReopen) {
		t.Fatal("late session does not re-enter the fold")
	}
	if !periodEdgeExists(def.Edges, NodeClassifyPeriodReopen, NodeEndPeriodReopenRejected) {
		t.Fatal("payroll-locked reopen has no governed rejection")
	}
}

func TestTodo_WTIME_005_Compiles(t *testing.T) {
	plan, err := CompileDefinition(PeriodTimecardDefinition(DefaultParams()))
	if err != nil {
		t.Fatalf("period definition does not compile: %v", err)
	}
	if plan.WorkflowID != PeriodWorkflowID || len(plan.Terminals) != 5 {
		t.Fatalf("compiled period plan = %q with %d terminals", plan.WorkflowID, len(plan.Terminals))
	}
}

func TestTodo_WTIME_005_Property(t *testing.T) {
	def := PeriodTimecardDefinition(DefaultParams())
	ends := 0
	for _, node := range def.Nodes {
		if node.Type == workflow.StepEnd {
			ends++
		}
	}
	if ends != 5 {
		t.Fatalf("terminal count = %d, want 5", ends)
	}
	if len(def.Limits.DeclaredCycles) != 1 || def.Limits.DeclaredCycles[0].EntryNodeID != NodeFoldPeriod {
		t.Fatalf("period cycle declaration = %#v", def.Limits.DeclaredCycles)
	}
	if len(def.ApprovalRequirements) != 1 || def.ApprovalRequirements[0].ID != ApprovalPeriodTimecard {
		t.Fatalf("approval requirements = %#v", def.ApprovalRequirements)
	}
}

func TestTodo_WTIME_005_RegistryComposition(t *testing.T) {
	params := DefaultParams()
	if params.TenantScope == "" || params.OrganizationScope == "" {
		t.Fatal("default time parameters must carry tenant and organization scope")
	}
	if len(Manifests()) == 0 || len(RuleClasses()) == 0 || len(FormClasses()) == 0 {
		t.Fatal("time registry is empty")
	}
	for _, plan := range Plans() {
		if plan.Definition == nil || plan.Definition(params).WorkflowID != plan.WorkflowID {
			t.Fatalf("plan %q has no matching definition", plan.TemplateID)
		}
	}
}

func periodEdgeExists(edges []workflow.Edge, from, to string) bool {
	for _, edge := range edges {
		if edge.From == from && edge.To == to {
			return true
		}
	}
	return false
}
