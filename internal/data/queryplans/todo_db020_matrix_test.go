package queryplans_test

import (
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/queryplans"
)

// TestTodo_DB_020_Property checks that the catalogue never declares a query
// without enough information to make its bounded, tenant-aware proof specific.
func TestTodo_DB_020_Property(t *testing.T) {
	t.Parallel()
	entries := queryplans.Catalog()
	if len(entries) == 0 {
		t.Fatal("critical query catalogue is empty")
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.Name == "" || entry.Table == "" || entry.Owner == "" {
			t.Errorf("incomplete critical query entry: %+v", entry)
		}
		if seen[entry.Name] {
			t.Errorf("duplicate critical query name %q", entry.Name)
		}
		seen[entry.Name] = true
		if entry.RowThreshold <= 0 {
			t.Errorf("%s has no representative row threshold", entry.Name)
		}
		if len(entry.ExpectedIndexSubstrings) == 0 {
			t.Errorf("%s has no expected tenant-aware index", entry.Name)
		}
	}
}

// TestTodo_DB_020_Mutation verifies that changing an otherwise passing plan
// to a sequential scan or an unrelated index makes the plan proof fail.
func TestTodo_DB_020_Mutation(t *testing.T) {
	t.Parallel()
	entry := queryplans.Entry{Table: "journey_worker", ExpectedIndexSubstrings: []string{"journey_worker_recorded"}}
	for _, tc := range []struct {
		name      string
		json      string
		wantSeq   bool
		wantIndex bool
	}{
		{"baseline", indexScanPlanJSON, false, true},
		{"sequential scan mutation", seqScanPlanJSON, true, false},
		{"wrong index mutation", `[{"Plan":{"Node Type":"Index Scan","Relation Name":"journey_worker","Index Name":"worker_unscoped_idx"}}]`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := queryplans.ParseExplainJSON(tc.json)
			if err != nil {
				t.Fatalf("parse plan: %v", err)
			}
			if got := plan.HasSeqScanOn(entry.Table); got != tc.wantSeq {
				t.Errorf("HasSeqScanOn(%s) = %v, want %v", entry.Table, got, tc.wantSeq)
			}
			if got := plan.UsesIndexContaining(entry.ExpectedIndexSubstrings...); got != tc.wantIndex {
				t.Errorf("UsesIndexContaining = %v, want %v", got, tc.wantIndex)
			}
		})
	}
}

// TestTodo_DB_020_Race exercises the immutable plan parser and shape walker
// concurrently so shared query-plan analysis remains safe under parallel
// request diagnostics.
func TestTodo_DB_020_Race(t *testing.T) {
	t.Parallel()
	const workers, iterations = 16, 50
	var wg sync.WaitGroup
	errCh := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < iterations; n++ {
				plan, err := queryplans.ParseExplainJSON(partitionedAppendPlanJSON)
				if err != nil {
					errCh <- err.Error()
					return
				}
				if !plan.HasSeqScanOn("ledger_event_p1") || plan.HasSeqScanOn("ledger_event_p0") {
					errCh <- "partition scan walk returned an incorrect result"
					return
				}
				shape := plan.Shape().Normalized()
				if shape.NodeType == "" {
					errCh <- "normalized plan shape is empty"
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// BenchmarkTodo_DB_020 measures the CPU cost of decoding and walking a captured
// representative partitioned plan, the pure analysis performed after EXPLAIN.
func BenchmarkTodo_DB_020(b *testing.B) {
	for i := 0; i < b.N; i++ {
		plan, err := queryplans.ParseExplainJSON(partitionedAppendPlanJSON)
		if err != nil {
			b.Fatal(err)
		}
		if !plan.HasSeqScanOn("ledger_event_p1") {
			b.Fatal("expected partition scan not found")
		}
		_ = plan.Shape().Normalized()
	}
}
