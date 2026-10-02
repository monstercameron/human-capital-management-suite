package application

import (
	"testing"
	"time"
)

func TestTodo_AGENTUX_073_RunDuration(t *testing.T) {
	started := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)
	deadline := started.Add(2 * time.Minute)
	cases := []struct {
		name     string
		updated  time.Time
		deadline time.Time
		state    string
		want     time.Duration
	}{
		{"a failed run closed long after its deadline ran to the deadline", started.Add(45 * time.Minute), deadline, "FAILED", 2 * time.Minute},
		{"a failed run closed before its deadline ran until it was closed", started.Add(20 * time.Second), deadline, "FAILED", 20 * time.Second},
		{"an expired run is measured the same way", started.Add(time.Hour), deadline, "expired", 2 * time.Minute},
		{"a completed run keeps its own end", started.Add(5 * time.Minute), deadline, "COMPLETED", 5 * time.Minute},
		{"a run with no deadline keeps its own end", started.Add(45 * time.Minute), time.Time{}, "FAILED", 45 * time.Minute},
		{"a deadline before the start is ignored", started.Add(45 * time.Minute), started.Add(-time.Minute), "FAILED", 45 * time.Minute},
		{"a row changed before it started has no duration", started.Add(-time.Second), deadline, "FAILED", 0},
	}
	for _, tc := range cases {
		if got := agentUX073RunWorkedFor(started, tc.updated, tc.deadline, tc.state); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
	row := projectAgentControlRun(AgentOwnerRunProjection{StartedAt: started, UpdatedAt: started.Add(45 * time.Minute), Deadline: deadline}, AgentControlIdentity{Name: "Assistant"})
	if row.Duration != "45m0s" {
		t.Fatalf("a run with no terminal state keeps its measured duration, got %s", row.Duration)
	}
	projection := AgentOwnerRunProjection{StartedAt: started, UpdatedAt: started.Add(45 * time.Minute), Deadline: deadline}
	projection.View.State = "FAILED"
	if row := projectAgentControlRun(projection, AgentControlIdentity{Name: "Assistant"}); row.Duration != "2m0s" {
		t.Fatalf("the owner's row shows the sweep time as the run's duration: %s", row.Duration)
	}
}
