package productui

import (
	"reflect"
	"testing"
)

func workflowPathFixture() WorkflowDraftView {
	return WorkflowDraftView{
		StartNodeID: "snapshot",
		Nodes: []WorkflowDraftNode{
			{ID: "snapshot", StepType: "CAPABILITY", Outcomes: []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED"}, {RouteKey: "REJECTED"}}},
			{ID: "threshold", StepType: "DECISION", Outcomes: []WorkflowDraftOutcome{{RouteKey: "ABOVE"}, {RouteKey: "WITHIN"}}},
			{ID: "finance", StepType: "APPROVAL", Outcomes: []WorkflowDraftOutcome{{RouteKey: "APPROVED"}, {RouteKey: "REJECTED"}}},
			{ID: "manager", StepType: "APPROVAL", Outcomes: []WorkflowDraftOutcome{{RouteKey: "APPROVED"}, {RouteKey: "REJECTED"}, {RouteKey: "EXPIRED"}}},
			{ID: "redo", StepType: "TASK", Outcomes: []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED"}}},
			{ID: "orphan", StepType: "TASK", Outcomes: []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED"}}, Bindings: []WorkflowDraftBinding{{TargetPath: "worker_id"}}},
			{ID: "end_complete", StepType: "END"},
			{ID: "end_rejected", StepType: "END"},
		},
		Edges: []WorkflowDraftEdge{
			{FromID: "snapshot", ToID: "threshold", RouteKey: "SUCCEEDED"},
			{FromID: "snapshot", ToID: "end_rejected", RouteKey: "REJECTED"},
			{FromID: "threshold", ToID: "finance", RouteKey: "ABOVE"},
			{FromID: "threshold", ToID: "manager", RouteKey: "WITHIN"},
			{FromID: "finance", ToID: "manager", RouteKey: "APPROVED"},
			{FromID: "finance", ToID: "end_rejected", RouteKey: "REJECTED"},
			{FromID: "manager", ToID: "end_complete", RouteKey: "APPROVED"},
			{FromID: "manager", ToID: "redo", RouteKey: "REJECTED"},
			{FromID: "redo", ToID: "manager", RouteKey: "SUCCEEDED"},
		},
	}
}

// TestWorkflowPathOrdersByLongestRouteAndSeparatesWhatIsNotConnected pins the
// defect the path replaced: a step nothing leads to must never be ranked
// beside the start step.
func TestWorkflowPathOrdersByLongestRouteAndSeparatesWhatIsNotConnected(t *testing.T) {
	path := buildWorkflowPath(workflowPathFixture(), "")
	var order []string
	for _, step := range path.Steps {
		order = append(order, step.Node.ID)
	}
	if want := []string{"snapshot", "threshold", "finance", "manager", "redo"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("path order = %v, want %v", order, want)
	}
	if len(path.Loose) != 1 || path.Loose[0].Node.ID != "orphan" {
		t.Fatalf("loose steps = %+v, want only orphan", path.Loose)
	}
	// The key lists exits worst first, the same order a step's chips use.
	if len(path.Exits) != 2 || path.Exits[0].Label != "Rejected" || path.Exits[1].Label != "Complete" {
		t.Fatalf("exits = %+v", path.Exits)
	}
	if path.Exits[1].Tone != "positive" || path.Exits[0].Tone != "danger" || path.Exits[0].Routes != 2 {
		t.Fatalf("exit tone/count = %+v", path.Exits)
	}
}

func TestWorkflowPathSeparatesContinuationsExitsAndJumps(t *testing.T) {
	path := buildWorkflowPath(workflowPathFixture(), "manager")
	threshold, manager, redo := path.Steps[1], path.Steps[3], path.Steps[4]
	if threshold.Continues != "ABOVE" || len(threshold.Jumps) != 1 || threshold.Jumps[0].ToID != "manager" || threshold.Jumps[0].Back {
		t.Fatalf("decision = %+v", threshold)
	}
	if manager.Continues != "REJECTED" || len(manager.Exits) != 1 || manager.Exits[0].ExitID != "end_complete" {
		t.Fatalf("manager = %+v", manager)
	}
	if !reflect.DeepEqual(manager.Open, []string{"EXPIRED"}) {
		t.Fatalf("manager open outcomes = %v, want EXPIRED", manager.Open)
	}
	if len(redo.Jumps) != 1 || !redo.Jumps[0].Back {
		t.Fatalf("loop back was not recognised: %+v", redo.Jumps)
	}
	if path.Lanes == 0 {
		t.Fatal("jumps were given no rail lane")
	}
	// A step a route lands on says where the route came from.
	if len(manager.Arrivals) != 2 || manager.Arrivals[0].From.ID != "threshold" || manager.Arrivals[1].From.ID != "redo" || !manager.Arrivals[1].Back {
		t.Fatalf("manager arrivals = %+v, want the skip from threshold and the loop from redo", manager.Arrivals)
	}
	if threshold.ContinuesTo.ID != "finance" {
		t.Fatalf("decision continues to %q, want finance", threshold.ContinuesTo.ID)
	}
	// The skip and the loop both touch the manager row; both must be drawn,
	// and both are highlighted because manager is selected.
	turns := 0
	for _, cell := range manager.Rail {
		if cell.Turn {
			turns++
			if !cell.Active {
				t.Fatalf("a lane meeting the selected step is not active: %+v", cell)
			}
		}
	}
	if turns != 2 {
		t.Fatalf("manager row meets %d lanes, want 2: %+v", turns, manager.Rail)
	}
	for lane := 0; lane < path.Lanes; lane++ {
		if path.Steps[0].Rail[lane].used() {
			t.Fatalf("a lane crosses the start row: %+v", path.Steps[0].Rail)
		}
	}
}

func TestWorkflowPathProblemsNameWhatIsLeftToFinish(t *testing.T) {
	problems := workflowPathProblems(buildWorkflowPath(workflowPathFixture(), ""), nil)
	keys := map[string]int{}
	for _, problem := range problems {
		keys[problem.Key]++
	}
	// The manager's unset "Expired" is an exception result: it is one grouped
	// thing to finish, not a line of its own. The orphan's unset "Succeeded"
	// is a usual result and is named.
	if keys["workflow_editor.problem_loose"] != 1 || keys["workflow_editor.problem_unbound"] != 1 || keys["workflow_editor.problem_open"] != 1 || keys["workflow_editor.problem_exceptions"] != 1 {
		t.Fatalf("problems = %+v", problems)
	}
	empty := workflowPathProblems(buildWorkflowPath(WorkflowDraftView{StartNodeID: "a", Nodes: []WorkflowDraftNode{{ID: "a", StepType: "TASK"}}}, ""), nil)
	if len(empty) != 1 || empty[0].Key != "workflow_editor.problem_no_exit" {
		t.Fatalf("a draft with no exit was not flagged: %+v", empty)
	}
}
