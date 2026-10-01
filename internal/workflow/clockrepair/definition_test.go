package clockrepair

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

func TestTodo_TCLOCK_011_CompileContract(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatal(err)
	}
	if plan.WorkflowID != WorkflowID || plan.StartNodeID != NodeCommitRequest {
		t.Fatalf("identity = %s/%s", plan.WorkflowID, plan.StartNodeID)
	}
	if len(Definition().Inputs) != 6 || len(plan.Nodes) != 12 {
		t.Fatalf("compiled shape inputs=%d nodes=%d", len(Definition().Inputs), len(plan.Nodes))
	}
	if len(Definition().ApprovalRequirements) != 1 || !Definition().ApprovalRequirements[0].SeparationOfDuties {
		t.Fatal("supervisor approval must require segregation of duties")
	}
	correction, ok := plan.Node(NodeCorrection)
	if !ok || correction.Capability == nil || correction.Capability.ID != CapabilityCorrection {
		t.Fatal("correction capability is not bound")
	}
}

func TestTodo_TCLOCK_011_ApprovedCorrectionPath(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatal(err)
	}
	state, err := frontier.Seed(plan, "repair-instance")
	if err != nil {
		t.Fatal(err)
	}
	state = step(t, plan, state, NodeCommitRequest, workflow.OutcomeSucceeded)
	state = step(t, plan, state, NodeValidate, workflow.OutcomeSucceeded)
	state = step(t, plan, state, NodeEligibility, workflow.Outcome(RouteEligible))
	state = step(t, plan, state, NodeApproval, workflow.Outcome("APPROVED"))
	state = step(t, plan, state, NodeCorrection, workflow.OutcomeSucceeded)
	tr := finish(t, plan, state, NodeApproved)
	if tr.Terminal.TerminalCode != "TIME_PUNCH_CORRECTED" || !tr.Complete {
		t.Fatalf("terminal = %+v", tr.Terminal)
	}
}

func TestTodo_TCLOCK_011_RefusalConflictAndGovernedReopenPaths(t *testing.T) {
	for _, tc := range []struct {
		name, route, terminal string
	}{
		{"ineligible", RouteIneligible, NodeRejected},
		{"conflict", RouteConflict, NodeConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := Compile()
			if err != nil {
				t.Fatal(err)
			}
			state, err := frontier.Seed(plan, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			state = step(t, plan, state, NodeCommitRequest, workflow.OutcomeSucceeded)
			state = step(t, plan, state, NodeValidate, workflow.OutcomeSucceeded)
			state = step(t, plan, state, NodeEligibility, workflow.Outcome(tc.route))
			tr := finish(t, plan, state, tc.terminal)
			if tr.Terminal.NodeID != tc.terminal {
				t.Fatalf("terminal node = %s", tr.Terminal.NodeID)
			}
		})
	}

	plan, err := Compile()
	if err != nil {
		t.Fatal(err)
	}
	state, err := frontier.Seed(plan, "closed")
	if err != nil {
		t.Fatal(err)
	}
	state = step(t, plan, state, NodeCommitRequest, workflow.OutcomeSucceeded)
	state = step(t, plan, state, NodeValidate, workflow.OutcomeSucceeded)
	state = step(t, plan, state, NodeEligibility, workflow.Outcome(RouteClosedPeriod))
	state = step(t, plan, state, NodeReopen, workflow.OutcomeSucceeded)
	tr := finish(t, plan, state, NodeClosedPeriod)
	if tr.Terminal.TerminalCode != "TIME_PUNCH_CLOSED_PERIOD_REOPEN_REQUIRED" {
		t.Fatalf("closed period terminal = %+v", tr.Terminal)
	}
}

func step(t *testing.T, plan *workflow.CompiledWorkflow, state frontier.InstanceState, node string, outcome workflow.Outcome) frontier.InstanceState {
	t.Helper()
	tr, err := frontier.Advance(plan, state, frontier.NodeOutcome{NodeID: node, Outcome: outcome})
	if err != nil {
		t.Fatalf("advance %s: %v", node, err)
	}
	return tr.Next
}

func finish(t *testing.T, plan *workflow.CompiledWorkflow, state frontier.InstanceState, node string) frontier.Transition {
	t.Helper()
	tr, err := frontier.Advance(plan, state, frontier.NodeOutcome{NodeID: node})
	if err != nil {
		t.Fatalf("finish %s: %v", node, err)
	}
	return tr
}
