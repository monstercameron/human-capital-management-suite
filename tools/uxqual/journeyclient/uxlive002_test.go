package journeyclient

import (
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UXLIVE-002's RED was measured on the running server. Journey
// 01a0b189-e04c-7265-8926-909531dd6530 closed BLOCKED after both approvals
// succeeded and revalidation refused -- System diagnostics records
// approve_finance SUCCEEDED, approve_manager SUCCEEDED, still_valid
// SUCCEEDED, execute_promotion SKIPPED, end_blocked SUCCEEDED -- and the
// history below the stepper said both reviews completed. The stepper said
// "Proposal - Did not complete", "Finance review - Not started", "Manager
// review - Not started".
//
// The cause was stepStates: a pure stage->shape table. BLOCKED is reachable
// from any point in the run, so the stage alone cannot say where the run
// got to. The states now come from the run's own recorded history, and the
// table only supplies the shape for stages where that is unambiguous.

func uxlive002At(minute int) *timestamppb.Timestamp {
	return timestamppb.New(time.Date(2026, 9, 17, 22, minute, 0, 0, time.UTC))
}

func uxlive002Event(kind, title, ref string, minute int) *journeyv1.TimelineEvent {
	return &journeyv1.TimelineEvent{Kind: kind, Title: title, Ref: ref, At: uxlive002At(minute)}
}

// uxlive002BothApprovalsDone is the timeline of a run that started its
// workflow and completed both approvals.
func uxlive002BothApprovalsDone() []*journeyv1.TimelineEvent {
	return []*journeyv1.TimelineEvent{
		uxlive002Event(eventIntentCreated, "", "", 40),
		uxlive002Event(eventInstanceStarted, "", "", 41),
		uxlive002Event(eventWorkItem, "Finance review assigned", "wi-finance", 41),
		uxlive002Event(eventWorkItem, "Finance review completed", "wi-finance", 42),
		uxlive002Event(eventWorkItem, "Manager review assigned", "wi-manager", 42),
		uxlive002Event(eventWorkItem, "Manager review completed", "wi-manager", 43),
		uxlive002Event(eventLedgerRecorded, "", "", 43),
	}
}

func uxlive002States(t *testing.T, stage string, events []*journeyv1.TimelineEvent) [5]string {
	t.Helper()
	steps := stepsLocale("en-US", stage, events, "2026-07-01")
	if len(steps) != 5 {
		t.Fatalf("stepper has %d steps, want 5", len(steps))
	}
	var states [5]string
	for i, step := range steps {
		states[i] = step.State
	}
	return states
}

// TestTodo_UXLIVE_002 is the primary red/green test: a blocked run that
// completed both approvals must not report them as never started.
func TestTodo_UXLIVE_002(t *testing.T) {
	states := uxlive002States(t, stageBlocked, uxlive002BothApprovalsDone())

	for i, label := range []string{"proposal", "finance review", "manager review"} {
		if states[i] != stepDone {
			t.Fatalf("%s = %q, want %q: the run's own history records it completed", label, states[i], stepDone)
		}
	}
	if states[3] != stepFailed {
		t.Fatalf("effective date = %q, want %q: that is where this run stopped", states[3], stepFailed)
	}
	if states[4] != stepUpcoming {
		t.Fatalf("recorded = %q, want %q", states[4], stepUpcoming)
	}
}

// TestTodo_UXLIVE_002_Property is the invariant behind the fix: whatever the
// stage, no step the run's history proves completed may be shown as
// unstarted, and an issue stage marks exactly one failure.
func TestTodo_UXLIVE_002_Property(t *testing.T) {
	stages := []string{
		stageProposed, stageBlocked, stageAwaitingApproval, stageFinanceApproval,
		stageManagerApproval, stageReapproval, stageWaitingEffective, stageRevalidation,
		stageExecuted, stageObservingEffects, stageCompleted, stageRecorded,
		stageRejected, stageFailed, stageRepairRequired,
	}
	timelines := map[string][]*journeyv1.TimelineEvent{
		"empty":          nil,
		"created only":   {uxlive002Event(eventIntentCreated, "", "", 40)},
		"started":        {uxlive002Event(eventIntentCreated, "", "", 40), uxlive002Event(eventInstanceStarted, "", "", 41)},
		"one approval":   {uxlive002Event(eventInstanceStarted, "", "", 41), uxlive002Event(eventWorkItem, "COMPLETED", "wi-finance", 42)},
		"both approvals": uxlive002BothApprovalsDone(),
		"bare transition vocabulary": {
			uxlive002Event(eventInstanceStarted, "", "", 41),
			uxlive002Event(eventWorkItem, "COMPLETED", "wi-finance", 42),
			uxlive002Event(eventWorkItem, "COMPLETED", "wi-manager", 43),
		},
		"finance cancelled": {
			uxlive002Event(eventInstanceStarted, "", "", 41),
			uxlive002Event(eventWorkItem, "Finance review assigned", "wi-finance", 41),
			uxlive002Event(eventWorkItem, "Finance review cancelled", "wi-finance", 42),
			uxlive002Event(eventLedgerRecorded, "", "", 42),
		},
	}

	for _, stage := range stages {
		for name, events := range timelines {
			states := uxlive002States(t, stage, events)
			done := completedSteps(stage, events)
			failures := 0
			for i, state := range states {
				if done[i] && state != stepDone {
					t.Fatalf("%s/%s: step %d is %q although the history records it completed", stage, name, i, state)
				}
				if state == stepFailed {
					failures++
				}
			}
			if failures > 1 {
				t.Fatalf("%s/%s: stepper marks %d failures, a run stops once", stage, name, failures)
			}
			if stoppedStage(stage) && failures != 1 {
				t.Fatalf("%s/%s: an issue stage marks %d failures, want exactly 1", stage, name, failures)
			}
		}
	}
}

// TestTodo_UXLIVE_002_Golden pins the states per stage for the two
// timelines the live audit actually found, so the blocked-after-approvals
// shape cannot silently revert to the stage table's guess.
func TestTodo_UXLIVE_002_Golden(t *testing.T) {
	cases := []struct {
		stage string
		want  [5]string
	}{
		{stage: stageBlocked, want: [5]string{stepDone, stepDone, stepDone, stepFailed, stepUpcoming}},
		{stage: stageRepairRequired, want: [5]string{stepDone, stepDone, stepDone, stepFailed, stepUpcoming}},
		{stage: stageRecorded, want: [5]string{stepDone, stepDone, stepDone, stepDone, stepDone}},
		{stage: stageCompleted, want: [5]string{stepDone, stepDone, stepDone, stepDone, stepDone}},
	}
	for _, c := range cases {
		if got := uxlive002States(t, c.stage, uxlive002BothApprovalsDone()); got != c.want {
			t.Fatalf("%s states = %v, want %v", c.stage, got, c.want)
		}
	}

	// A proposal that never started its workflow still fails at the
	// proposal, which is what the stage table used to assume for every
	// blocked run.
	neverStarted := []*journeyv1.TimelineEvent{uxlive002Event(eventIntentCreated, "", "", 40)}
	want := [5]string{stepFailed, stepUpcoming, stepUpcoming, stepUpcoming, stepUpcoming}
	if got := uxlive002States(t, stageBlocked, neverStarted); got != want {
		t.Fatalf("blocked proposal states = %v, want %v", got, want)
	}
}

// TestTodo_UXLIVE_002_Security keeps the stepper honest about approvals: an
// approval the history records as refused is never presented as completed,
// and a step that did not happen carries no borrowed timestamp.
//
// The stage table still supplies the shape for a terminal stage whose
// timeline this projection did not receive -- the stage is durable engine
// state, and denying a recorded promotion would be the opposite lie -- so
// this asserts what the evidence contradicts, not what it merely omits.
func TestTodo_UXLIVE_002_Security(t *testing.T) {
	cancelled := []*journeyv1.TimelineEvent{
		uxlive002Event(eventIntentCreated, "", "", 40),
		uxlive002Event(eventInstanceStarted, "", "", 41),
		uxlive002Event(eventWorkItem, "Finance review assigned", "wi-finance", 41),
		uxlive002Event(eventWorkItem, "Finance review cancelled", "wi-finance", 42),
		uxlive002Event(eventLedgerRecorded, "", "", 42),
	}
	for _, stage := range []string{stageFailed, stageBlocked, stageRejected} {
		states := uxlive002States(t, stage, cancelled)
		if states[1] == stepDone {
			t.Fatalf("%s: a cancelled finance review is presented as completed: %v", stage, states)
		}
		if states[4] == stepDone {
			t.Fatalf("%s: a terminal refusal is presented as a recorded promotion: %v", stage, states)
		}
	}

	steps := stepsLocale("en-US", stageBlocked, uxlive002BothApprovalsDone(), "2026-07-01")
	if steps[4].State == stepDone {
		t.Fatalf("a blocked run reports its promotion recorded: %q", steps[4].State)
	}
	if steps[4].At != "" {
		t.Fatalf("an unstarted final step carries the time %q from another step's event", steps[4].At)
	}
}
