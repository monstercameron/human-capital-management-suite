package demand

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_DEMAND_003(t *testing.T) {
	signal := validSignal(t)
	requirement, err := NewCoverageRequirement("coverage-1", signal.Scenario, signal.Version, []DemandSignal{signal})
	if err != nil {
		t.Fatal(err)
	}
	supply := validSupply(t, "supply-1", "2")
	partition := mustPartition(t, requirement, []SupplyAssignment{
		{Supply: supply, WorkerRef: "worker-1", Qualified: true, Available: true},
	}, "snapshot-7")
	if len(partition.Cells) != 1 {
		t.Fatalf("cells=%d", len(partition.Cells))
	}
	if partition.Cells[0].Outcome != CellCovered {
		t.Fatalf("outcome=%v", partition.Cells[0].Outcome)
	}
	if partition.Snapshot != "snapshot-7" || partition.Digest == "" {
		t.Fatalf("partition: %+v", partition)
	}
	// Seeded defect: an unqualified worker cannot satisfy the
	// requirement — the cell shorts instead of covering.
	short := mustPartition(t, requirement, []SupplyAssignment{
		{Supply: supply, WorkerRef: "worker-1", Qualified: false, Available: true},
	}, "snapshot-7")
	if short.Cells[0].Outcome != CellShort {
		t.Fatalf("unqualified outcome=%v", short.Cells[0].Outcome)
	}
	excess := mustPartition(t, requirement, []SupplyAssignment{
		{Supply: supply, WorkerRef: "worker-1", Qualified: true, Available: true},
		{Supply: validSupply(t, "supply-2", "2"), WorkerRef: "worker-2", Qualified: true, Available: true},
	}, "snapshot-7")
	if excess.Cells[0].Outcome != CellExcess {
		t.Fatalf("excess outcome=%v", excess.Cells[0].Outcome)
	}
	unknown := validSignal(t)
	unknown.ConfidenceClass = ConfidenceUnknown
	unknownReq, err := NewCoverageRequirement("coverage-2", unknown.Scenario, unknown.Version, []DemandSignal{unknown})
	if err != nil {
		t.Fatal(err)
	}
	mystery := mustPartition(t, unknownReq, []SupplyAssignment{
		{Supply: supply, WorkerRef: "worker-1", Qualified: true, Available: true},
	}, "snapshot-7")
	if mystery.Cells[0].Outcome != CellUnknown {
		t.Fatalf("unknown-confidence outcome=%v", mystery.Cells[0].Outcome)
	}
	if excess.Digest == partition.Digest {
		t.Fatal("partition digest must distinguish cells")
	}
}

func mustPartition(t *testing.T, requirement CoverageRequirement, assignments []SupplyAssignment, snapshot string) DemandPartition {
	t.Helper()
	partition, err := PartitionDemand(requirement, assignments, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return partition
}

func validSupply(t *testing.T, ref, amount string) SupplyReference {
	t.Helper()
	start, err := values.NewLocalDate(2026, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2026, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := values.NewQuantity(amount, "FTE", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return SupplyReference{Ref: ref, Location: "loc-nyc", Skill: "nursing", Work: interval, Quantity: quantity}
}
