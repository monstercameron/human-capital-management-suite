package demand

import (
	"testing"
)

func demand006Gap(t *testing.T) SignalGap {
	t.Helper()
	signal := validSignal(t)
	requirement, err := NewCoverageRequirement("coverage-1", signal.Scenario, signal.Version, []DemandSignal{signal})
	if err != nil {
		t.Fatal(err)
	}
	supply := validSupply(t, "supply-1", "1")
	partition := mustPartition(t, requirement, []SupplyAssignment{
		{Supply: supply, WorkerRef: "worker-1", Qualified: true, Available: true},
	}, "snapshot-7")
	explanation := mustExplain(t, partition, requirement, []SupplyAssignment{
		{Supply: supply, WorkerRef: "worker-1", Qualified: true, Available: true},
	})
	if len(explanation.Gaps) != 1 {
		t.Fatalf("gaps=%d", len(explanation.Gaps))
	}
	return explanation.Gaps[0]
}

// TestTodo_DEMAND_006: a coverage shortfall proposes a governed,
// human-approved operational intent — a description, never a mutation
// of schedules or headcount.
func TestTodo_DEMAND_006(t *testing.T) {
	gap := demand006Gap(t)
	proposal, err := ProposeIntent(gap, IntentParams{
		IntentName:   "close-nyc-nursing-shortfall",
		Scope:        "loc-nyc",
		Snapshot:     "snapshot-7",
		ApprovalRole: "workforce-planner",
		Quorum:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Seeded defect: the proposal must stay descriptive. It names an
	// intent with scope, risk, snapshot and a human approval gate —
	// and carries no schedule or headcount mutation whatsoever.
	if proposal.IntentName == "" || proposal.Scope == "" || proposal.Risk == "" ||
		proposal.Snapshot == "" || proposal.ApprovalRole == "" || proposal.Quorum < 1 {
		t.Fatalf("proposal is not governed: %+v", proposal)
	}
	if proposal.Kind != IntentCloseShortfall || proposal.SignalID != "signal-1" {
		t.Fatalf("proposal=%+v", proposal)
	}
	if proposal.Shortfall.String() != "1 FTE" {
		t.Fatalf("shortfall=%v", proposal.Shortfall)
	}
	if proposal.CostKnown {
		t.Fatal("uncosted proposal claims a cost")
	}
	if proposal.CanonicalDigest == "" {
		t.Fatal("proposal is not digested")
	}
	if err := proposal.Validate(); err != nil {
		t.Fatal(err)
	}
}
