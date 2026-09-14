package demand

import (
	"strings"
	"testing"
)

func mustExplain(t *testing.T, partition DemandPartition, requirement CoverageRequirement, assignments []SupplyAssignment) GapExplanation {
	t.Helper()
	explanation, err := ExplainGaps(partition, requirement, assignments)
	if err != nil {
		t.Fatal(err)
	}
	return explanation
}

// TestTodo_DEMAND_004: every gap traces to its signals, provenance,
// skill dimension and supply attributions — without crediting one
// supply twice across overlapping demand and without citing workers.
func TestTodo_DEMAND_004(t *testing.T) {
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
	gap := explanation.Gaps[0]
	if gap.Outcome != CellShort || gap.SignalID != "signal-1" {
		t.Fatalf("gap=%+v", gap)
	}
	if gap.Shortfall.String() != "1 FTE" || gap.Attributed.String() != "1 FTE" || gap.Required.String() != "2 FTE" {
		t.Fatalf("quantities=%+v", gap)
	}
	if gap.SourceRef == "" || gap.Scenario == "" || gap.Target == "" {
		t.Fatalf("gap cites no provenance: %+v", gap)
	}
	rendered := string(explanation.Canonical())
	if strings.Contains(rendered, "worker-1") {
		t.Fatal("explanation leaks worker identity")
	}
	// Seeded defect: a partition for another requirement must reject
	// with DEMAND_004_REJECTED — never explain across mismatched truth.
	other, err := NewCoverageRequirement("coverage-2", signal.Scenario, signal.Version, []DemandSignal{signal})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExplainGaps(partition, other, nil)
	rejected, ok := AsGapRejected(err)
	if !ok || rejected.Code != GapRejectedCode {
		t.Fatalf("mismatch error = %v", err)
	}
	if rejected.Field == "" || rejected.State == "" || rejected.Version == 0 {
		t.Fatalf("rejection names no field/state/version: %+v", rejected)
	}
	if err := explanation.Validate(); err != nil {
		t.Fatal(err)
	}
}
