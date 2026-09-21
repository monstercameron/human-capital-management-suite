package app

import (
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func align029Record(status runtime.InstanceStatus, frontier []string, nodes map[string]runtime.NodeStatus, items ...workitem.WorkItem) journeyRecord {
	inst := &runtime.Instance{InstanceID: uuid.New(), RuntimeStatus: status, CurrentNodeIDs: frontier}
	rec := journeyRecord{instance: inst, items: items}
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rec.nodes = append(rec.nodes, runtime.NodeExecution{NodeID: id, Attempt: 1, Status: nodes[id]})
	}
	return rec
}

// TestTodo_ALIGN_029 proves the product stage of a promotion journey is
// projected from durable workflow state only: no instance is PROPOSED or
// BLOCKED by whether a proposal revision exists, the frontier node names the
// in-flight stage, and a reached terminal decides the finished stage.
func TestTodo_ALIGN_029(t *testing.T) {
	for name, tc := range map[string]struct {
		revision string
		record   journeyRecord
		want     workspace.JourneyStage
	}{
		"no instance, simulated":   {"rev-1", journeyRecord{}, workspace.JourneyStageProposed},
		"no instance, no plan":     {"", journeyRecord{}, workspace.JourneyStageBlocked},
		"finance approval":         {"rev-1", align029Record(runtime.InstanceWaiting, []string{promotionexec.NodeApproveFinance}, nil), journeyStageFinanceApproval},
		"manager approval":         {"rev-1", align029Record(runtime.InstanceWaiting, []string{promotionexec.NodeApproveManager}, nil), journeyStageManagerApproval},
		"waiting effective date":   {"rev-1", align029Record(runtime.InstanceWaiting, []string{promotionexec.NodeWaitEffectiveDate}, nil), journeyStageWaitingEffective},
		"revalidating":             {"rev-1", align029Record(runtime.InstanceRunning, []string{promotionexec.NodeRevalidate}, nil), journeyStageRevalidation},
		"executing":                {"rev-1", align029Record(runtime.InstanceRunning, []string{promotionexec.NodeExecutePromotion}, nil), journeyStageExecuted},
		"observing effects":        {"rev-1", align029Record(runtime.InstanceRunning, []string{promotionexec.NodeObservePayroll}, nil), journeyStageObservingEffects},
		"recorded":                 {"rev-1", align029Record(runtime.InstanceCompleted, nil, map[string]runtime.NodeStatus{promotionexec.NodeEndComplete: runtime.NodeSucceeded}), journeyStageRecorded},
		"rejected":                 {"rev-1", align029Record(runtime.InstanceCompleted, nil, map[string]runtime.NodeStatus{promotionexec.NodeEndRejected: runtime.NodeSucceeded}), workspace.JourneyStageRejected},
		"repair":                   {"rev-1", align029Record(runtime.InstanceCompleted, nil, map[string]runtime.NodeStatus{promotionexec.NodeEndRepairPlan: runtime.NodeSucceeded}), journeyStageRepairRequired},
		"cancelled":                {"rev-1", align029Record(runtime.InstanceCompleted, nil, map[string]runtime.NodeStatus{promotionexec.NodeEndCancelled: runtime.NodeSucceeded}), workspace.JourneyStageFailed},
		"open approval work item":  {"rev-1", align029Record(runtime.InstanceWaiting, []string{"unmapped"}, nil, workitem.WorkItem{Kind: workitem.KindApproval, Status: workitem.StatusRouted, NodeID: promotionexec.NodeApproveManager}), journeyStageManagerApproval},
		"running without frontier": {"rev-1", align029Record(runtime.InstanceRunning, nil, nil), workspace.JourneyStageAwaitingApproval},
		"blocked runtime":          {"rev-1", align029Record(runtime.InstanceBlocked, nil, nil), workspace.JourneyStageFailed},
	} {
		if got := deriveJourneyStage(tc.revision, tc.record); got != tc.want {
			t.Errorf("%s: stage = %s, want %s", name, got, tc.want)
		}
	}
}

// TestTodo_ALIGN_029_Property proves terminal precedence and status honesty:
// whatever frontier an instance reports, a SUCCEEDED terminal decides the
// stage, and a terminal present only as SKIPPED never does.
func TestTodo_ALIGN_029_Property(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	terminals := map[string]workspace.JourneyStage{
		promotionexec.NodeEndComplete:   journeyStageRecorded,
		promotionexec.NodeEndRejected:   workspace.JourneyStageRejected,
		promotionexec.NodeEndRepairPlan: journeyStageRepairRequired,
		promotionexec.NodeEndBlocked:    workspace.JourneyStageBlocked,
	}
	for _, node := range plan.Nodes {
		for terminal, want := range terminals {
			reached := align029Record(runtime.InstanceCompleted, []string{node.ID}, map[string]runtime.NodeStatus{terminal: runtime.NodeSucceeded})
			if got := deriveJourneyStage("rev", reached); got != want {
				t.Fatalf("frontier %s with %s reached = %s, want %s", node.ID, terminal, got, want)
			}
			skipped := align029Record(runtime.InstanceWaiting, []string{promotionexec.NodeApproveFinance}, map[string]runtime.NodeStatus{terminal: runtime.NodeSkipped})
			if got := deriveJourneyStage("rev", skipped); got != journeyStageFinanceApproval {
				t.Fatalf("a SKIPPED %s decided the stage: %s", terminal, got)
			}
		}
	}
}

// TestTodo_ALIGN_029_Golden pins the node-to-stage projection for every node
// of the compiled promotion plan.
func TestTodo_ALIGN_029_Golden(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, node := range plan.Nodes {
		stage, _ := journeyStageForNode(node.ID)
		lines = append(lines, node.ID+"="+string(stage))
	}
	sort.Strings(lines)
	want := strings.Join([]string{
		"acknowledge_release=AWAITING_ACKNOWLEDGEMENT",
		"approve_finance=FINANCE_APPROVAL", "approve_manager=MANAGER_APPROVAL",
		// Promotion 1.1.0: the provider-confirmation waits sit between the
		// commit and its observations.
		"await_access_confirmation=OBSERVING_EFFECTS", "await_payroll_confirmation=OBSERVING_EFFECTS",
		"compensate_budget_hold=REPAIR_REQUIRED",
		"end_blocked=BLOCKED", "end_cancelled=FAILED",
		"end_complete=RECORDED", "end_expired=FAILED", "end_invalidated=FAILED", "end_rejected=REJECTED",
		"end_repair_plan=REPAIR_REQUIRED", "evaluate_band=PROPOSED", "execute_promotion=EXECUTED",
		"observe_access=OBSERVING_EFFECTS", "observe_payroll=OBSERVING_EFFECTS", "observe_reconciliation=OBSERVING_EFFECTS",
		"raise_threshold=PROPOSED", "reapproval_task=REAPPROVAL", "revalidate=REVALIDATION", "simulate_compensation=PROPOSED",
		"snapshot_worker=PROPOSED", "still_valid=REVALIDATION", "wait_effective_date=WAITING_EFFECTIVE_DATE",
	}, "\n")
	if got := strings.Join(lines, "\n"); got != want {
		t.Fatalf("node projection =\n%s\nwant\n%s", got, want)
	}
}

// TestTodo_ALIGN_029_Conformance proves the projection is total over the
// compiled plan: a node added to the promotion workflow without a product
// stage fails here instead of rendering an unknown state.
func TestTodo_ALIGN_029_Conformance(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range plan.Nodes {
		if stage, ok := journeyStageForNode(node.ID); !ok || stage == "" {
			t.Errorf("compiled node %s has no product stage", node.ID)
		}
	}
	if _, ok := journeyStageForNode("not_a_node"); ok {
		t.Error("an unknown node was given a stage")
	}
}

// TestTodo_ALIGN_029_Fault proves degraded durable state never renders as a
// healthy stage: a closed work item does not hold the journey in approval, an
// instance in any non-live runtime status without a terminal is FAILED, and a
// live one without a recognizable frontier is AWAITING_APPROVAL.
func TestTodo_ALIGN_029_Fault(t *testing.T) {
	closed := workitem.WorkItem{Kind: workitem.KindApproval, Status: workitem.StatusCompleted, NodeID: promotionexec.NodeApproveFinance}
	if got := deriveJourneyStage("rev", align029Record(runtime.InstanceRunning, nil, nil, closed)); got != workspace.JourneyStageAwaitingApproval {
		t.Errorf("closed item on a running instance = %s", got)
	}
	for _, status := range []runtime.InstanceStatus{runtime.InstancePaused, runtime.InstanceRepairRequired, runtime.InstanceQuarantined, runtime.InstanceCancelled, runtime.InstanceBlocked} {
		if got := deriveJourneyStage("rev", align029Record(status, nil, nil)); got != workspace.JourneyStageFailed {
			t.Errorf("%s without a terminal = %s, want FAILED", status, got)
		}
	}
	for _, status := range []runtime.InstanceStatus{runtime.InstanceCreated, runtime.InstanceRunning, runtime.InstanceWaiting} {
		if got := deriveJourneyStage("rev", align029Record(status, []string{"unmapped"}, nil)); got != workspace.JourneyStageAwaitingApproval {
			t.Errorf("%s with an unmapped frontier = %s", status, got)
		}
	}
}
