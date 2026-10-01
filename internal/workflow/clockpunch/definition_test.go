package clockpunch

import (
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"testing"
)

func TestTodo_WTIME004_HandAuthoredClockInOutCompiles(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatal(err)
	}
	if plan.WorkflowID != WorkflowID || plan.StartNodeID != NodeCommitPunch {
		t.Fatalf("unexpected clock graph: %+v", plan)
	}
	if len(Definition().Nodes) != 7 || len(Definition().Edges) != 11 {
		t.Fatal("clock-in/out routes changed")
	}
}

func TestTodo_WTIME004_ClockInAndOutTraverseSharedEngine(t *testing.T) {
	plan, err := Compile()
	if err != nil {
		t.Fatal(err)
	}
	state, err := frontier.Seed(plan, "clock-session")
	if err != nil {
		t.Fatal(err)
	}
	step := func(outcome frontier.NodeOutcome) frontier.Transition {
		t.Helper()
		result, err := frontier.Advance(plan, state, outcome)
		if err != nil {
			t.Fatal(err)
		}
		if result.Next.PlanDigest != plan.Digest() {
			t.Fatal("plan pin drifted")
		}
		state = result.Next
		return result
	}
	opened := step(frontier.NodeOutcome{NodeID: NodeCommitPunch, Outcome: workflow.OutcomeSucceeded, OutputDigest: "clock-in-evidence"})
	if len(opened.Frontier) != 1 || opened.Frontier[0] != NodeAwaitClockOut {
		t.Fatalf("clock-in frontier=%v", opened.Frontier)
	}
	parked := step(frontier.NodeOutcome{NodeID: NodeAwaitClockOut, Await: frontier.AwaitSignal, AwaitRef: "hcmnext.events.time.clock_out"})
	if len(parked.Intents) != 1 || parked.Intents[0].Kind != frontier.IntentSignalSubscriptionRequired {
		t.Fatalf("clock-out wait=%+v", parked.Intents)
	}
	resumed := step(frontier.NodeOutcome{NodeID: NodeAwaitClockOut, Outcome: workflow.OutcomeSucceeded})
	if len(resumed.Frontier) != 1 || resumed.Frontier[0] != NodeCommitClockOut {
		t.Fatalf("clock-out signal frontier=%v", resumed.Frontier)
	}
	closed := step(frontier.NodeOutcome{NodeID: NodeCommitClockOut, Outcome: workflow.OutcomeSucceeded, OutputDigest: "clock-out-evidence"})
	if len(closed.Frontier) != 1 || closed.Frontier[0] != NodeClosed {
		t.Fatalf("clock-out frontier=%v", closed.Frontier)
	}
	terminal := step(frontier.NodeOutcome{NodeID: NodeClosed})
	if terminal.Terminal.TerminalCode != "TIME_SESSION_CLOSED" || terminal.Terminal.Lifecycle.ExecutionState != "COMMITTED" || len(terminal.Frontier) != 0 {
		t.Fatalf("terminal=%+v", terminal)
	}
}

func TestTodo_WTIME004_MissingOutAndCommitFailureDoNotCloseSuccessfully(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failed bool
		want   string
	}{{"missing out", false, "TIME_SESSION_MISSING_OUT"}, {"commit failure", true, "TIME_SESSION_REPAIR_REQUIRED"}} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := Compile()
			if err != nil {
				t.Fatal(err)
			}
			state, err := frontier.Seed(plan, "clock-session")
			if err != nil {
				t.Fatal(err)
			}
			outcome := frontier.NodeOutcome{NodeID: NodeCommitPunch, Outcome: workflow.OutcomeSucceeded}
			if tc.failed {
				outcome = frontier.NodeOutcome{NodeID: NodeCommitPunch, Failed: true, ErrorClass: "STORE_UNAVAILABLE"}
			}
			tr, err := frontier.Advance(plan, state, outcome)
			if err != nil {
				t.Fatal(err)
			}
			state = tr.Next
			terminalNode := NodeRepair
			if !tc.failed {
				tr, err = frontier.Advance(plan, state, frontier.NodeOutcome{NodeID: NodeAwaitClockOut, Outcome: workflow.Outcome("TIMED_OUT")})
				if err != nil {
					t.Fatal(err)
				}
				state = tr.Next
				terminalNode = NodeMissingOut
			}
			tr, err = frontier.Advance(plan, state, frontier.NodeOutcome{NodeID: terminalNode})
			if err != nil {
				t.Fatal(err)
			}
			if tr.Terminal.TerminalCode != tc.want {
				t.Fatalf("unexpected terminal=%+v", tr.Terminal)
			}
		})
	}
}
