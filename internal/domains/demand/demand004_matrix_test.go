package demand

import (
	"strings"
	"sync"
	"testing"
)

func twinSignal(t *testing.T, id string) DemandSignal {
	t.Helper()
	signal := validSignal(t)
	signal.SignalID = id
	return signal
}

func demand004Fixture(t *testing.T) (CoverageRequirement, []SupplyAssignment) {
	t.Helper()
	signals := []DemandSignal{twinSignal(t, "signal-1"), twinSignal(t, "signal-2")}
	requirement, err := NewCoverageRequirement("coverage-1", "base", "v1", signals)
	if err != nil {
		t.Fatal(err)
	}
	assignments := []SupplyAssignment{
		{Supply: validSupply(t, "supply-1", "1"), WorkerRef: "worker-PII-1", Qualified: true, Available: true},
	}
	return requirement, assignments
}

// TestTodo_DEMAND_004_Property: gap-shape invariants hold for every
// outcome and shared supply is never credited twice.
func TestTodo_DEMAND_004_Property(t *testing.T) {
	requirement, assignments := demand004Fixture(t)
	partition := mustPartition(t, requirement, assignments, "snapshot-7")
	explanation := mustExplain(t, partition, requirement, assignments)
	if len(explanation.Gaps) != 2 {
		t.Fatalf("gaps=%d", len(explanation.Gaps))
	}
	credited, err := zeroLike(validSignal(t).Quantity)
	if err != nil {
		t.Fatal(err)
	}
	supplied, err := zeroLike(validSignal(t).Quantity)
	if err != nil {
		t.Fatal(err)
	}
	supplied, err = supplied.Add(assignments[0].Supply.Quantity)
	if err != nil {
		t.Fatal(err)
	}
	for _, gap := range explanation.Gaps {
		recomposed, err := gap.Attributed.Add(gap.Shortfall)
		if err != nil || recomposed.Value().Cmp(gap.Required.Value()) != 0 {
			t.Fatalf("gap %s recomposes to %v, want %v", gap.SignalID, recomposed, gap.Required)
		}
		credited, err = credited.Add(gap.Attributed)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Overlapping demand shares one unit of supply: total credit can
	// never exceed what exists, however many gaps cite it.
	if credited.Value().Cmp(supplied.Value()) > 0 {
		t.Fatalf("double-counted supply: credited=%v supplied=%v", credited, supplied)
	}
	// Deterministic replay.
	again := mustExplain(t, partition, requirement, assignments)
	if again.CanonicalDigest != explanation.CanonicalDigest {
		t.Fatal("identical explanation inputs replayed to different digests")
	}
}

// TestTodo_DEMAND_004_Race: concurrent explanations replay
// identically over shared inputs.
func TestTodo_DEMAND_004_Race(t *testing.T) {
	requirement, assignments := demand004Fixture(t)
	partition := mustPartition(t, requirement, assignments, "snapshot-7")
	const workers = 8
	digests := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			explanation, err := ExplainGaps(partition, requirement, assignments)
			if err != nil {
				t.Error(err)
				return
			}
			digests[n] = explanation.CanonicalDigest
		}(i)
	}
	wg.Wait()
	for i := 1; i < workers; i++ {
		if digests[i] != digests[0] || digests[0] == "" {
			t.Fatal("concurrent explanations diverged")
		}
	}
}

// TestTodo_DEMAND_004_Security: worker identities never reach the
// explanation, and explaining persists nothing.
func TestTodo_DEMAND_004_Security(t *testing.T) {
	requirement, assignments := demand004Fixture(t)
	partition := mustPartition(t, requirement, assignments, "snapshot-7")
	beforeRequirement := requirement.Canonical()
	beforePartition := partition.Digest
	explanation := mustExplain(t, partition, requirement, assignments)
	rendered := string(explanation.Canonical())
	for _, worker := range []string{"worker-PII-1"} {
		if strings.Contains(rendered, worker) {
			t.Fatalf("explanation leaks %q", worker)
		}
	}
	if string(requirement.Canonical()) != string(beforeRequirement) || partition.Digest != beforePartition {
		t.Fatal("explanation mutated its inputs")
	}
	if len(assignments) != 1 || assignments[0].WorkerRef != "worker-PII-1" {
		t.Fatal("explanation mutated its assignments")
	}
}

// TestTodo_DEMAND_004_Mutation: malformed and edge inputs resolve on
// the documented side.
func TestTodo_DEMAND_004_Mutation(t *testing.T) {
	signal := validSignal(t)
	requirement, err := NewCoverageRequirement("coverage-1", signal.Scenario, signal.Version, []DemandSignal{signal})
	if err != nil {
		t.Fatal(err)
	}
	// Unknown-confidence signals explain with a reason and zero
	// attributions — never fabricated credit.
	mystery := validSignal(t)
	mystery.ConfidenceClass = ConfidenceUnknown
	mysteryReq, err := NewCoverageRequirement("coverage-2", mystery.Scenario, mystery.Version, []DemandSignal{mystery})
	if err != nil {
		t.Fatal(err)
	}
	supply := validSupply(t, "supply-1", "2")
	mysteryPartition := mustPartition(t, mysteryReq, []SupplyAssignment{
		{Supply: supply, WorkerRef: "w", Qualified: true, Available: true},
	}, "snapshot-7")
	mysteryExplanation := mustExplain(t, mysteryPartition, mysteryReq, []SupplyAssignment{
		{Supply: supply, WorkerRef: "w", Qualified: true, Available: true},
	})
	if len(mysteryExplanation.Gaps) != 1 || len(mysteryExplanation.Gaps[0].Attributions) != 0 {
		t.Fatalf("unknown gap=%+v", mysteryExplanation.Gaps)
	}
	if mysteryExplanation.Gaps[0].Reason == "" {
		t.Fatal("unknown gap carries no reason")
	}
	// A partition cell for a foreign signal rejects.
	forged := mustPartition(t, requirement, nil, "snapshot-7")
	forged.Cells = append(forged.Cells, CoverageCell{SignalID: "ghost", Outcome: CellShort})
	if _, err := ExplainGaps(forged, requirement, nil); err == nil {
		t.Fatal("foreign signal cell explained")
	} else if _, ok := AsGapRejected(err); !ok {
		t.Fatalf("foreign signal error = %v", err)
	}
	// Empty assignments on a shortfall explain zero credit, not error.
	bare := mustPartition(t, requirement, nil, "snapshot-7")
	bareExplanation := mustExplain(t, bare, requirement, nil)
	if len(bareExplanation.Gaps) != 1 {
		t.Fatalf("bare gaps=%d", len(bareExplanation.Gaps))
	}
	if bareExplanation.Gaps[0].Attributed.String() != "0 FTE" || bareExplanation.Gaps[0].Shortfall.String() != "2 FTE" {
		t.Fatalf("bare gap=%+v", bareExplanation.Gaps[0])
	}
}
