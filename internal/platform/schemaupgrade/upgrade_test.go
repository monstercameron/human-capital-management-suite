package schemaupgrade

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func testPlan() Plan {
	return Plan{
		ID:                        "upgrade-people-v2",
		FromVersion:               1,
		ToVersion:                 2,
		Compatibility:             CompatibilityFull,
		SourceDigest:              strings.Repeat("1", 64),
		TargetDigest:              strings.Repeat("2", 64),
		RollbackBoundary:          RollbackBeforeContract,
		RequiredAdoptionWatermark: 3,
		BackfillBatchSize:         2,
		Binaries: []Binary{
			{Name: "old", Reads: []int{1, 2}, Writes: []int{1}},
			{Name: "new", Reads: []int{1, 2}, Writes: []int{1, 2}},
		},
	}
}

func testRows() []Row {
	return []Row{{Key: "a", Digest: "da"}, {Key: "b", Digest: "db"}, {Key: "c", Digest: "dc"}}
}

func completeUpgrade(t *testing.T) State {
	t.Helper()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	state, err := New(testPlan(), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Expand(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for !state.Checkpoint.Completed {
		if _, err := state.ResumeBackfill(testRows(), 2, now.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := state.CompareShadow(testRows(), testRows(), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	state.SetConsumerWatermark(3)
	if err := state.Cutover(3, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTodo_DB_021(t *testing.T) {
	state := completeUpgrade(t)
	if err := state.Contract(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if state.Phase != PhaseContracted || len(state.History) != 6 {
		t.Fatalf("phase=%s history=%d, want CONTRACTED and six append-only events", state.Phase, len(state.History))
	}
}

func TestTodo_DB_021_Golden(t *testing.T) {
	state := completeUpgrade(t)
	if got, want := state.Checkpoint.Cursor, "c"; got != want {
		t.Fatalf("checkpoint cursor=%q, want %q", got, want)
	}
	if state.Checkpoint.SourceDigest != state.Checkpoint.CopiedDigest || state.Shadow.SourceDigest != state.Shadow.TargetDigest {
		t.Fatal("completed upgrade did not retain equal source, copied and shadow digests")
	}
}

func TestTodo_DB_021_Race(t *testing.T) {
	state := completeUpgrade(t)
	const readers = 16
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			copy := state.Snapshot()
			copy.History[0].Detail = "tampered"
			copy.Plan.Binaries[0].Reads[0] = 99
			if state.History[0].Detail == "tampered" || state.Plan.Binaries[0].Reads[0] == 99 {
				t.Errorf("snapshot %d shares mutable state", i)
			}
		}(i)
	}
	wg.Wait()
}

func TestTodo_DB_021_Integration(t *testing.T) {
	state := completeUpgrade(t)
	if err := state.Contract(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := state.Rollback(time.Now().UTC()); !errors.Is(err, ErrNotReversible) {
		t.Fatalf("rollback after before-contract boundary error=%v, want not reversible", err)
	}
	if state.Phase != PhaseContracted || state.History[len(state.History)-1].Phase != PhaseContracted {
		t.Fatal("rejected rollback mutated lifecycle history")
	}
}

func TestTodo_DB_021_Mutation(t *testing.T) {
	state, err := New(testPlan(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Expand(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	rows := testRows()
	if _, err := state.ResumeBackfill(rows, 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	rows[0].Digest = "changed"
	if _, err := state.ResumeBackfill(rows, 1, time.Now().UTC()); !errors.Is(err, ErrCheckpointMismatch) {
		t.Fatalf("ResumeBackfill error=%v, want checkpoint mismatch", err)
	}
}

func TestTodo_DB_021_Fault(t *testing.T) {
	state := completeUpgrade(t)
	state.SetConsumerWatermark(0)
	if err := state.Contract(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := state.Rollback(time.Now().UTC()); !errors.Is(err, ErrNotReversible) {
		t.Fatalf("rollback after before-contract boundary error=%v, want not reversible", err)
	}

	lagging, err := New(testPlan(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := lagging.Expand(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for !lagging.Checkpoint.Completed {
		if _, err := lagging.ResumeBackfill(testRows(), 10, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := lagging.CompareShadow(testRows(), []Row{{Key: "a", Digest: "wrong"}}, time.Now().UTC()); !errors.Is(err, ErrShadowMismatch) {
		t.Fatalf("CompareShadow error=%v, want shadow mismatch", err)
	}
}

func TestTodo_TOOL_020(t *testing.T) {
	state := completeUpgrade(t)
	if state.Phase != PhaseCutover || !state.Shadow.Exact || !state.Checkpoint.Completed {
		t.Fatalf("rolling upgrade phase=%s shadow=%v completed=%v", state.Phase, state.Shadow.Exact, state.Checkpoint.Completed)
	}
	if err := state.Contract(time.Date(2026, 9, 2, 12, 6, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if state.Phase != PhaseContracted || len(state.History) != 6 {
		t.Fatalf("phase=%s history=%d, want contracted with append-only history", state.Phase, len(state.History))
	}
}

func TestTodo_TOOL_020_Golden(t *testing.T) {
	state := completeUpgrade(t)
	base := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	want := []Event{
		{Phase: PhasePlanned, At: base, Detail: "plan accepted"},
		{Phase: PhaseExpanded, At: base.Add(time.Minute), Detail: "old and new representations admitted"},
		{Phase: PhaseBackfilled, At: base.Add(2 * time.Minute), Detail: "backfill complete"},
		{Phase: PhaseShadowed, At: base.Add(3 * time.Minute), Detail: "shadow comparison exact"},
		{Phase: PhaseCutover, At: base.Add(4 * time.Minute), Watermark: 3, Detail: "new representation serving"},
	}
	if !reflect.DeepEqual(state.History, want) {
		t.Fatalf("history=%#v, want %#v", state.History, want)
	}
}

func TestTodo_TOOL_020_Conformance(t *testing.T) {
	state := completeUpgrade(t)
	if err := state.Contract(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := state.Rollback(time.Now().UTC()); !errors.Is(err, ErrNotReversible) {
		t.Fatalf("rollback crossed irreversible contract boundary: %v", err)
	}
}

func TestTodo_TOOL_020_Fault(t *testing.T) {
	state := completeUpgrade(t)
	if err := state.Contract(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	before := len(state.History)
	if err := state.Rollback(time.Now().UTC()); !errors.Is(err, ErrNotReversible) {
		t.Fatalf("rollback error=%v, want not reversible", err)
	}
	if len(state.History) != before || state.Phase != PhaseContracted {
		t.Fatal("rejected rollback mutated durable history or phase")
	}
}

func TestRollbackBoundaryIsClosedAndRejectionPreservesState(t *testing.T) {
	plan := testPlan()
	plan.RollbackBoundary = RollbackBoundary("AFTER_CONTRACT")
	if _, err := New(plan, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("New error=%v, want invalid plan", err)
	}

	state := completeUpgrade(t)
	state.Plan.RollbackBoundary = RollbackBoundary("AFTER_CONTRACT") // corrupted persisted input
	before := state.Snapshot()
	if err := state.Rollback(time.Date(2026, 9, 2, 12, 5, 0, 0, time.UTC)); !errors.Is(err, ErrNotReversible) {
		t.Fatalf("Rollback error=%v, want not reversible", err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatalf("rejected rollback mutated state: got %#v want %#v", state, before)
	}
}
