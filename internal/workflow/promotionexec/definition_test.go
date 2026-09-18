package promotionexec

import (
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// The digest moved with WF-RUN-037: it now pins execute_promotion as the
// AUTHORITATIVE_CORE (the only change; with the role cleared the plan still
// digests to 186dcb387bc3f6576b88935be0435839fb96afd055e1e69a1da0c6fa74d60674).
const promotionExecutePlanDigest = "655535f1e484991a79562a292eb21374381b1e757e115a3ec53eb25ce61679d7"

func TestPromotionExecuteDefinitionCompiles(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if plan.WorkflowID != WorkflowID || plan.Version != Version {
		t.Fatalf("identity = %s/%d, want %s/%d", plan.WorkflowID, plan.Version, WorkflowID, Version)
	}
	if plan.Phase != workflow.PhaseP1B || plan.TerminalProfile != workflow.TerminalProfileExecute {
		t.Fatalf("phase/profile = %s/%s, want P1B/EXECUTE", plan.Phase, plan.TerminalProfile)
	}
	execute, ok := plan.Node(NodeExecutePromotion)
	if !ok || execute.EffectClass != capability.EffectInternalMutation || execute.Capability == nil || execute.Capability.OperationMode != workflow.ModeExecute {
		t.Fatalf("execute node = %+v, want governed EXECUTE internal mutation", execute)
	}
	if execute.EffectRole != workflow.RoleAuthoritativeCore || len(plan.NodesWithRole(workflow.RoleAuthoritativeCore)) != 1 {
		t.Fatalf("execute node role = %q, cores %v; want the one AUTHORITATIVE_CORE", execute.EffectRole, plan.NodesWithRole(workflow.RoleAuthoritativeCore))
	}
	waitNode, ok := plan.Node(NodeWaitEffectiveDate)
	if !ok || !waitNode.SafePoint {
		t.Fatalf("wait node = %+v, want compiler-placed safe point", waitNode)
	}
	if !execute.SafePoint {
		t.Fatalf("execute node = %+v, want compiler-placed safe point before core commit", execute)
	}
	if plan.Effects.ZeroEffect {
		t.Fatal("EXECUTE plan must carry the one governed core commit effect")
	}
}

func TestTodo_PROMO_EXEC_DEF_Golden(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if promotionExecutePlanDigest == "" {
		t.Fatalf("compiled plan digest = %q; pin this value in the golden", plan.Digest())
	}
	if got := plan.Digest(); got != promotionExecutePlanDigest {
		t.Fatalf("plan digest = %q, want %q", got, promotionExecutePlanDigest)
	}
	wantOrder := NodeOrder()
	if !reflect.DeepEqual(plan.Reachability.Order, wantOrder) {
		t.Fatalf("reachability order = %v, want %v", plan.Reachability.Order, wantOrder)
	}
}

func TestTodo_PROMO_EXEC_DEF_Conformance(t *testing.T) {
	def := Definition()
	wantTypes := map[string]workflow.StepType{
		NodeSnapshotWorker:        workflow.StepCapability,
		NodeSimulateCompensation:  workflow.StepCapability,
		NodeEvaluateBand:          workflow.StepCapability,
		NodeRaiseThreshold:        workflow.StepDecision,
		NodeApproveFinance:        workflow.StepApproval,
		NodeApproveManager:        workflow.StepApproval,
		NodeWaitEffectiveDate:     workflow.StepWait,
		NodeRevalidate:            workflow.StepCapability,
		NodeStillValid:            workflow.StepDecision,
		NodeReapproval:            workflow.StepTask,
		NodeExecutePromotion:      workflow.StepCapability,
		NodeCompensateHold:        workflow.StepCompensate,
		NodeAcknowledgeRelease:    workflow.StepSignal,
		NodeObservePayroll:        workflow.StepObserve,
		NodeObserveAccess:         workflow.StepObserve,
		NodeObserveReconciliation: workflow.StepObserve,
		NodeEndComplete:           workflow.StepEnd,
		NodeEndRepairPlan:         workflow.StepEnd,
		NodeEndRejected:           workflow.StepEnd,
		NodeEndInvalidated:        workflow.StepEnd,
		NodeEndExpired:            workflow.StepEnd,
		NodeEndCancelled:          workflow.StepEnd,
		NodeEndBlocked:            workflow.StepEnd,
	}
	gotTypes := map[string]workflow.StepType{}
	for _, node := range def.Nodes {
		gotTypes[node.ID] = node.Type
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("node types = %v, want %v", gotTypes, wantTypes)
	}
	if got := def.ApprovalRequirements; len(got) != 2 || got[0].ID != ApprovalFinance || got[1].ID != ApprovalManager {
		t.Fatalf("approval requirements = %+v, want finance and manager", got)
	}
	expectedEdges := [][3]string{
		{NodeRaiseThreshold, NodeApproveFinance, "ABOVE_THRESHOLD"},
		{NodeRaiseThreshold, NodeApproveManager, "WITHIN_THRESHOLD"},
		{NodeApproveFinance, NodeApproveManager, "APPROVED"},
		{NodeApproveManager, NodeWaitEffectiveDate, "APPROVED"},
		{NodeWaitEffectiveDate, NodeRevalidate, "FIRED"},
		{NodeStillValid, NodeExecutePromotion, "VALID"},
		{NodeStillValid, NodeReapproval, "REAPPROVAL_REQUIRED"},
		{NodeStillValid, NodeEndBlocked, "BLOCKED"},
		{NodeReapproval, NodeApproveManager, "REAPPROVED"},
		{NodeReapproval, NodeEndCancelled, "WITHDRAWN"},
		{NodeReapproval, NodeEndInvalidated, "INVALIDATED"},
		{NodeExecutePromotion, NodeObservePayroll, "SUCCEEDED"},
		{NodeObservePayroll, NodeObserveAccess, "PASS"},
		{NodeObservePayroll, NodeCompensateHold, "FAIL"},
		{NodeObserveAccess, NodeObserveReconciliation, "PASS"},
		{NodeObserveAccess, NodeCompensateHold, "PARTIAL"},
		{NodeCompensateHold, NodeEndRepairPlan, "COMPENSATED"},
		{NodeObserveReconciliation, NodeAcknowledgeRelease, "CONSISTENT"},
		{NodeAcknowledgeRelease, NodeEndComplete, "SUCCEEDED"},
		{NodeAcknowledgeRelease, NodeEndRepairPlan, "TIMED_OUT"},
		{NodeObserveReconciliation, NodeEndRepairPlan, "DEGRADED"},
	}
	for _, want := range expectedEdges {
		found := false
		for _, edge := range def.Edges {
			if edge.From == want[0] && edge.To == want[1] && edge.RouteKey == want[2] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing documented edge %s -> %s [%s]", want[0], want[1], want[2])
		}
	}
	for _, node := range def.Nodes {
		if node.ID == NodeWaitEffectiveDate && node.Wait == nil {
			t.Error("WAIT node has no wait binding")
		}
		if node.ID == NodeObservePayroll || node.ID == NodeObserveAccess || node.ID == NodeObserveReconciliation {
			if node.Observe == nil || node.Observe.MaxAgeSeconds == 0 || node.Retry == nil || node.Retry.MaxAttempts == 0 {
				t.Errorf("observation node %q lacks expected observation or retry budget", node.ID)
			}
		}
	}
	for _, node := range def.Nodes {
		if node.ID == NodeEndBlocked {
			if node.End == nil || node.End.RuntimeStatus != workflow.RuntimeCompleted {
				t.Fatalf("blocked terminal runtime status = %+v, want COMPLETED", node.End)
			}
			if got := node.End.CompletionMapping["ExecutionState"]; got != "BLOCKED" {
				t.Fatalf("blocked terminal execution state = %q, want BLOCKED", got)
			}
			if got := node.End.CompletionMapping["BusinessState"]; got != "NOT_ACHIEVED" {
				t.Fatalf("blocked terminal business state = %q, want NOT_ACHIEVED", got)
			}
		}
		if node.ID != NodeEndComplete && node.ID != NodeEndRepairPlan && node.End != nil && node.End.CommitReceiptRef != "" {
			t.Fatalf("non-fact terminal %s carries commit receipt %q", node.ID, node.End.CommitReceiptRef)
		}
	}
}

func TestTodo_PROMO_EXEC_DEF_Mutation(t *testing.T) {
	base, err := Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	withoutSafePoint := Definition()
	for i := range withoutSafePoint.Nodes {
		if withoutSafePoint.Nodes[i].ID == NodeExecutePromotion || withoutSafePoint.Nodes[i].ID == NodeWaitEffectiveDate {
			withoutSafePoint.Nodes[i].SafePointRequested = false
		}
	}
	mutated, err := Compile(withoutSafePoint)
	if err != nil {
		t.Fatalf("Compile(mutated safe point): %v", err)
	}
	if mutated.Digest() == base.Digest() {
		t.Fatal("removing compiler safe-point requests did not change the plan digest")
	}
	withoutFinance := Definition()
	withoutFinance.ApprovalRequirements = []workflow.ApprovalRequirement{{ID: ApprovalManager, ResolverExpression: "CurrentManagerOf(worker)", Scope: organizationScope, Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"}}
	if _, err := Compile(withoutFinance); err == nil {
		t.Fatal("removing the finance approval requirement must fail compilation")
	}
}

func TestTodo_PROMO_EXEC_DEF_Simulation(t *testing.T) {
	plan, err := CompileSimulation()
	if err != nil {
		t.Fatalf("CompileSimulation: %v", err)
	}
	if !plan.Effects.ZeroEffect || len(plan.Effects.EffectKeys) != 0 {
		t.Fatalf("simulation effects = %+v, want zero effects", plan.Effects)
	}
	for _, node := range plan.Nodes {
		if node.EffectClass.IsWrite() {
			t.Fatalf("simulation node %q carries write effect %s", node.ID, node.EffectClass)
		}
	}
}

func TestTodo_PROMO_EXEC_DEF_ApprovalRequirements(t *testing.T) {
	when := time.Date(2026, 9, 5, 12, 0, 0, 123, time.UTC)
	finance, err := CompileFinanceApprovalRequirement("principal:finance", when)
	if err != nil {
		t.Fatalf("CompileFinanceApprovalRequirement: %v", err)
	}
	manager, err := CompileManagerApprovalRequirement("principal:manager", when)
	if err != nil {
		t.Fatalf("CompileManagerApprovalRequirement: %v", err)
	}
	if finance.RequirementID != ApprovalFinance || manager.RequirementID != ApprovalManager || finance.Digest() == manager.Digest() {
		t.Fatalf("approval identities/digests = %s/%s, %s/%s", finance.RequirementID, finance.Digest(), manager.RequirementID, manager.Digest())
	}
	if finance.Deadline.DecideBy.Time().Nanosecond() != 0 || manager.Deadline.Expiry.Time().Nanosecond() != 0 {
		t.Fatal("approval deadlines must be truncated to whole seconds")
	}
	if _, err := CompileFinanceApprovalRequirement("", when); err == nil {
		t.Fatal("empty finance approver must be refused")
	}
	if _, err := CompileManagerApprovalRequirement("principal:manager", time.Time{}); err == nil {
		t.Fatal("zero manager deadline must be refused")
	}
}
