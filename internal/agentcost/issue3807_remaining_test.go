package agentcost

import "testing"

// The allowance left today is the tightest of the agent's limits, as a whole
// percent, and is unknown when the agent has no limit.
func TestIssue3807_RemainingIsTheTightestLimit(t *testing.T) {
	f := newFixture(t)
	if _, known := f.gate.Remaining(policy("general")); known {
		t.Fatal("no limit set, yet the allowance was reported as known")
	}
	if err := f.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 10, MaxSpendMicros: 2_000_000}); err != nil {
		t.Fatal(err)
	}
	if err := f.gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", ConversationID: "general", MaxRuns: 4}); err != nil {
		t.Fatal(err)
	}
	if pct, known := f.gate.Remaining(policy("general")); !known || pct != 100 {
		t.Fatalf("fresh day = %d, %v; want 100, true", pct, known)
	}
	f.gate.Record(policy("general"), 100_000, "olive")
	// 1 of 4 runs in the conversation (75 left) is tighter than 1 of 10 (90).
	if pct, _ := f.gate.Remaining(policy("general")); pct != 75 {
		t.Fatalf("remaining = %d, want 75", pct)
	}
	// Another conversation counts only the agent-wide limit: 90 left.
	if pct, _ := f.gate.Remaining(policy("random")); pct != 90 {
		t.Fatalf("other conversation remaining = %d, want 90", pct)
	}
	// Another tenant's agent of the same name shares nothing.
	other := policy("general")
	other.TenantID = "t2"
	if _, known := f.gate.Remaining(other); known {
		t.Fatal("another tenant's allowance was visible")
	}
	for i := 0; i < 12; i++ {
		f.gate.Record(policy("general"), 500_000, "olive")
	}
	if pct, _ := f.gate.Remaining(policy("general")); pct != 0 {
		t.Fatalf("exhausted remaining = %d, want 0", pct)
	}
}
