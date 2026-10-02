package application

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
)

// TestTodo_AGENT_041_RunFacts: the owner's run row carries what the run
// records say about spend, the wait before the run started and the sources the
// answer cites, as a dollar amount, a duration and a count; a run whose records
// say nothing carries none of them, so the page shows a dash.
func TestTodo_AGENT_041_RunFacts(t *testing.T) {
	started := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	full := projectAgentControlRun(AgentOwnerRunProjection{
		View:      ownerops.TaskView{TaskID: "run-1", State: "COMPLETED", SpendMicros: 4200},
		StartedAt: started, UpdatedAt: started.Add(9 * time.Second),
		QueueLag: 2500 * time.Millisecond, CitationCount: 3,
	}, AgentControlIdentity{Name: "Policy Helper"})
	if full.Spend != "$0.0042" || full.QueueLag != "2.5s" || len(full.Citations) != 3 {
		t.Fatalf("spend=%q queue lag=%q citations=%v", full.Spend, full.QueueLag, full.Citations)
	}
	if big := projectAgentControlRun(AgentOwnerRunProjection{View: ownerops.TaskView{TaskID: "run-2", State: "COMPLETED", SpendMicros: 1_234_567}, StartedAt: started, UpdatedAt: started}, AgentControlIdentity{}); big.Spend != "$1.23" {
		t.Fatalf("a dollar of spend reads %q", big.Spend)
	}
	empty := projectAgentControlRun(AgentOwnerRunProjection{View: ownerops.TaskView{TaskID: "run-3", State: "COMPLETED"}, StartedAt: started, UpdatedAt: started}, AgentControlIdentity{})
	if empty.Spend != "" || empty.QueueLag != "" || len(empty.Citations) != 0 {
		t.Fatalf("a run with no records shows spend=%q queue lag=%q citations=%v", empty.Spend, empty.QueueLag, empty.Citations)
	}
	// A task's sources are the distinct ones its steps read, counted once each.
	entries := []agentrun.LedgerEntry{
		{Kind: "STEP_RESULT", SourceID: "doc-a", SourceIDs: []string{"doc-a", "doc-b"}},
		{Kind: "STEP_RESULT", SourceID: "doc-b"},
		{Kind: "USER_GOAL"},
	}
	if got := agentTaskSourceCount(entries); got != 2 {
		t.Fatalf("task sources = %d, want 2", got)
	}
}
