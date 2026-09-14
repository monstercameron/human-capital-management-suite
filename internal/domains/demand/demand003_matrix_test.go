package demand

import (
	"strings"
	"testing"
)

// TestTodo_DEMAND_003_Property: every assignment edge maps to exactly
// one documented cell outcome.
func TestTodo_DEMAND_003_Property(t *testing.T) {
	signal := validSignal(t)
	requirement, err := NewCoverageRequirement("coverage-1", signal.Scenario, signal.Version, []DemandSignal{signal})
	if err != nil {
		t.Fatal(err)
	}
	supply := validSupply(t, "supply-1", "2")
	otherScope := supply
	otherScope.Location = "loc-bos"
	cases := []struct {
		name        string
		assignments []SupplyAssignment
		want        CellOutcome
	}{
		{"no assignments", nil, CellShort},
		{"unqualified worker", []SupplyAssignment{{Supply: supply, WorkerRef: "w-1", Available: true}}, CellShort},
		{"unavailable worker", []SupplyAssignment{{Supply: supply, WorkerRef: "w-1", Qualified: true}}, CellShort},
		{"scope mismatch", []SupplyAssignment{{Supply: otherScope, WorkerRef: "w-1", Qualified: true, Available: true}}, CellShort},
		{"exact cover", []SupplyAssignment{{Supply: supply, WorkerRef: "w-1", Qualified: true, Available: true}}, CellCovered},
		{"under cover", []SupplyAssignment{{Supply: validSupply(t, "supply-2", "1"), WorkerRef: "w-1", Qualified: true, Available: true}}, CellShort},
		{"over cover", []SupplyAssignment{
			{Supply: supply, WorkerRef: "w-1", Qualified: true, Available: true},
			{Supply: validSupply(t, "supply-2", "1"), WorkerRef: "w-2", Qualified: true, Available: true},
		}, CellExcess},
	}
	for _, tc := range cases {
		got := mustPartition(t, requirement, tc.assignments, "snapshot-7")
		if len(got.Cells) != 1 || got.Cells[0].Outcome != tc.want {
			t.Fatalf("%s: outcome=%v", tc.name, got.Cells)
		}
	}
}

// TestTodo_DEMAND_003_Golden: the partition is deterministic and its
// digest distinguishes source snapshots.
func TestTodo_DEMAND_003_Golden(t *testing.T) {
	signal := validSignal(t)
	requirement, err := NewCoverageRequirement("coverage-1", signal.Scenario, signal.Version, []DemandSignal{signal})
	if err != nil {
		t.Fatal(err)
	}
	assignments := []SupplyAssignment{
		{Supply: validSupply(t, "supply-1", "2"), WorkerRef: "worker-1", Qualified: true, Available: true},
	}
	first := mustPartition(t, requirement, assignments, "snapshot-7")
	second := mustPartition(t, requirement, assignments, "snapshot-7")
	if first.Digest != second.Digest {
		t.Fatal("partition digest is not deterministic")
	}
	if !strings.HasPrefix(first.Digest, "sha256:") {
		t.Fatalf("digest=%q", first.Digest)
	}
	other := mustPartition(t, requirement, assignments, "snapshot-8")
	if other.Digest == first.Digest {
		t.Fatal("partition digest ignores source snapshot")
	}
	if first.Cells[0].Interval == "" || first.Cells[0].Dimension == "" || first.Cells[0].Required == "" {
		t.Fatalf("cell missing partition keys: %+v", first.Cells[0])
	}
}
