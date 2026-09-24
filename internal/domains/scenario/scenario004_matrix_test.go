package scenario

import (
	"errors"
	"sync"
	"testing"
)

// TestTodo_SCENARIO_004_Property: comparison is deterministic and
// direction-symmetric — self-comparison is all UNCHANGED, digest is
// stable across runs, assumption order never matters, and swapping
// sides swaps ADDED with REMOVED.
func TestTodo_SCENARIO_004_Property(t *testing.T) {
	base := compareBase(t)
	child, err := base.Fork(
		compareAssumption("headcount.target", "14", ValueDecimal, "HEAD", "plan:change-1"),
	)
	if err != nil {
		t.Fatal(err)
	}
	self, err := Compare(base, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, diff := range self.Diffs {
		if diff.Partition != PartitionUnchanged {
			t.Fatalf("self diff = %+v", diff)
		}
	}
	forward, err := Compare(base, child)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Compare(base, child)
	if err != nil {
		t.Fatal(err)
	}
	if forward.CanonicalDigest != again.CanonicalDigest {
		t.Fatal("identical comparisons produced different digests")
	}
	if string(forward.Canonical()) != string(again.Canonical()) {
		t.Fatal("identical comparisons produced different canonical bytes")
	}
	// Assumption order never matters: rebuild the child with the same
	// assumptions in reverse order and expect the same digest.
	reversed := make([]Assumption, len(child.Assumptions))
	copy(reversed, child.Assumptions)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	shuffled, err := NewScenarioRevision(ScenarioRevision{
		ScenarioID: "scenario-1", Revision: 2, ParentRevision: 1, ParentDigest: base.CanonicalDigest,
		Owner: "workforce-planning", Scope: "north-america", Horizon: scenarioHorizon(t),
		BaselineSnapshotRef: "snapshot:2026-01", Author: "planner-1",
		AuthorityDisclaimer: "simulation only; does not mutate authoritative facts", Lifecycle: LifecycleDraft,
		Assumptions: reversed,
	})
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := Compare(base, shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.CanonicalDigest != forward.CanonicalDigest {
		t.Fatal("assumption order changed the comparison digest")
	}
	// Swapping sides swaps ADDED with REMOVED and keeps CHANGED.
	backward, err := Compare(child, base)
	if err != nil {
		t.Fatal(err)
	}
	fwd, bwd := map[string]DiffPartition{}, map[string]DiffPartition{}
	for _, diff := range forward.Diffs {
		fwd[diff.Key] = diff.Partition
	}
	for _, diff := range backward.Diffs {
		bwd[diff.Key] = diff.Partition
	}
	for key, partition := range fwd {
		switch partition {
		case PartitionAdded:
			if bwd[key] != PartitionRemoved {
				t.Fatalf("key %q is ADDED forward but %q backward", key, bwd[key])
			}
		case PartitionRemoved:
			if bwd[key] != PartitionAdded {
				t.Fatalf("key %q is REMOVED forward but %q backward", key, bwd[key])
			}
		default:
			if bwd[key] != partition {
				t.Fatalf("key %q is %q forward but %q backward", key, partition, bwd[key])
			}
		}
	}
	// A changed unit is a CHANGED partition, never silently unchanged.
	renamed, err := base.Fork(
		compareAssumption("headcount.target", "12", ValueDecimal, "FTE", "forecast:2026"),
	)
	if err != nil {
		t.Fatal(err)
	}
	unitDiff, err := Compare(base, renamed)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, diff := range unitDiff.Diffs {
		if diff.Key == "headcount.target" {
			seen = true
			if diff.Partition != PartitionChanged {
				t.Fatalf("unit change diff = %+v", diff)
			}
		}
	}
	if !seen {
		t.Fatal("unit change produced no diff")
	}
	// Invalid revisions never compare: the typed contract error is for
	// comparable-shape mismatches, not malformed input.
	broken := base
	broken.Assumptions = nil
	broken.CanonicalDigest = ""
	if _, err := Compare(broken, base); !errors.Is(err, ErrInvalidScenario) {
		t.Fatalf("invalid revision error = %v", err)
	}
}

// TestTodo_SCENARIO_004_Race: concurrent comparisons publish into shared
// result state and always agree.
func TestTodo_SCENARIO_004_Race(t *testing.T) {
	base := compareBase(t)
	child, err := base.Fork(
		compareAssumption("headcount.target", "14", ValueDecimal, "HEAD", "plan:change-1"),
	)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Compare(base, child)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	results := make(map[int]Comparison, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got, err := Compare(base, child)
			if err != nil {
				t.Errorf("comparison %d: %v", i, err)
				return
			}
			if got.CanonicalDigest != want.CanonicalDigest {
				t.Errorf("comparison %d diverged", i)
				return
			}
			if err := got.Validate(); err != nil {
				t.Errorf("comparison %d invalid: %v", i, err)
				return
			}
			mu.Lock()
			results[i] = got
			mu.Unlock()
		}(i)
	}
	close(start)
	wg.Wait()
	if len(results) != workers {
		t.Fatalf("stored results = %d, want %d", len(results), workers)
	}
	for i, got := range results {
		if got.CanonicalDigest != want.CanonicalDigest {
			t.Fatalf("stored comparison %d diverged", i)
		}
	}
}
