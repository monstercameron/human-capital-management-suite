package schemaupgrade

import (
	"reflect"
	"testing"
	"time"
)

// TestTodo_TOOL_020_Recovery proves the rolling-upgrade recovery contract:
// rollback restores service without rewriting durable history, replaying a
// completed rollback has no duplicate effect, and a persisted journal
// (Snapshot) resumes to the same completed upgrade.
func TestTodo_TOOL_020_Recovery(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	// Rollback restores service with one append-only event.
	state := completeUpgrade(t)
	rolledBackAt := now.Add(5 * time.Minute)
	if err := state.Rollback(rolledBackAt); err != nil {
		t.Fatalf("Rollback error=%v, want nil", err)
	}
	if state.Phase != PhaseRolledBack {
		t.Fatalf("phase=%s, want ROLLED_BACK", state.Phase)
	}
	if len(state.History) != 6 {
		t.Fatalf("history=%d, want 6 append-only events", len(state.History))
	}
	last := state.History[len(state.History)-1]
	if last.Phase != PhaseRolledBack || !last.At.Equal(rolledBackAt) {
		t.Fatalf("last event=%#v, want ROLLED_BACK at %v", last, rolledBackAt)
	}

	// Replaying the completed rollback is idempotent: no error, no new
	// event, no phase change, no duplicate effect.
	before := state.Snapshot()
	if err := state.Rollback(now.Add(6 * time.Minute)); err != nil {
		t.Fatalf("replayed Rollback error=%v, want nil idempotent", err)
	}
	if state.Phase != PhaseRolledBack {
		t.Fatalf("phase=%s after replay, want ROLLED_BACK", state.Phase)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatalf("replayed rollback mutated state: got %#v want %#v", state, before)
	}

	// A persisted journal resumes after a restart to the same upgrade.
	restarted, err := New(testPlan(), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Expand(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.ResumeBackfill(testRows(), 1, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	journal := restarted.Snapshot() // durable journal written by the adapter
	resumed := journal              // rehydrated after a restart
	for !resumed.Checkpoint.Completed {
		if _, err := resumed.ResumeBackfill(testRows(), 2, now.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	// Replaying a completed backfill returns the same checkpoint without
	// advancing history or mutating progress (no duplicate effect).
	historyLen := len(resumed.History)
	checkpoint, err := resumed.ResumeBackfill(testRows(), 2, now.Add(7*time.Minute))
	if err != nil {
		t.Fatalf("replayed ResumeBackfill error=%v, want nil", err)
	}
	if !checkpoint.Completed || checkpoint.Cursor != "c" {
		t.Fatalf("replayed checkpoint=%+v, want completed with cursor c", checkpoint)
	}
	if len(resumed.History) != historyLen {
		t.Fatal("replayed backfill appended duplicate history")
	}
	if _, err := resumed.CompareShadow(testRows(), testRows(), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	resumed.SetConsumerWatermark(3)
	if err := resumed.Cutover(3, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if resumed.Phase != PhaseCutover || !resumed.Checkpoint.Completed || !resumed.Shadow.Exact {
		t.Fatalf("resumed upgrade phase=%s completed=%v shadow=%v",
			resumed.Phase, resumed.Checkpoint.Completed, resumed.Shadow.Exact)
	}
}
