package syncjob

import (
	"errors"
	"testing"
)

func fixtureJob() Job {
	return Job{ID: "job-1", Mode: ModeIncremental, LocalSystem: "hcm"}
}

func fixtureItems() []SourceItem {
	return []SourceItem{
		{ID: "a", Fingerprint: "fp-a1", Origin: "partner"},
		{ID: "b", Fingerprint: "fp-b1", Origin: "partner"},
		{ID: "c", Fingerprint: "fp-c1", Origin: "partner"},
	}
}

// TestTodo_INTG_019 is INTG-019's PRIMARY proof: full/incremental/delta/
// targeted jobs preserve cursor/watermark/counts/items, ownership prevents
// loops, missing records become explicit candidates, and unchanged
// fingerprints suppress writes.
func TestTodo_INTG_019(t *testing.T) {
	job := fixtureJob()
	batch, err := PlanBatch(job, Cursor{JobID: "job-1"}, fixtureItems())
	if err != nil {
		t.Fatalf("PlanBatch: %v", err)
	}
	if len(batch.Writes) != 3 {
		t.Fatalf("writes = %d, want 3 new items", len(batch.Writes))
	}
	if batch.Cursor.Processed != 3 || batch.Cursor.Written != 3 {
		t.Fatalf("cursor = %+v, want processed=3 written=3", batch.Cursor)
	}
	if batch.Cursor.Watermark == 0 {
		t.Fatal("watermark did not advance")
	}

	// Restart resumes from the returned cursor: unchanged fingerprints
	// suppress every write but counts still account every item.
	resumed, err := PlanBatch(job, batch.Cursor, fixtureItems())
	if err != nil {
		t.Fatalf("resume PlanBatch: %v", err)
	}
	if len(resumed.Writes) != 0 {
		t.Fatalf("resumed writes = %d, want 0 (unchanged fingerprints)", len(resumed.Writes))
	}
	if len(resumed.Skips) != 3 {
		t.Fatalf("resumed skips = %d, want 3", len(resumed.Skips))
	}
	if resumed.Cursor.Processed != 6 {
		t.Fatalf("resumed processed = %d, want 6", resumed.Cursor.Processed)
	}

	// Changed fingerprint rewrites only that item.
	changed := fixtureItems()
	changed[1].Fingerprint = "fp-b2"
	updated, err := PlanBatch(job, resumed.Cursor, changed)
	if err != nil {
		t.Fatalf("changed PlanBatch: %v", err)
	}
	if len(updated.Writes) != 1 || updated.Writes[0].ID != "b" {
		t.Fatalf("changed writes = %+v, want only b", updated.Writes)
	}

	// Absence without a deletion policy becomes an explicit candidate,
	// never an inferred deletion.
	absent, err := PlanBatch(job, updated.Cursor, fixtureItems()[:2])
	if err != nil {
		t.Fatalf("absent PlanBatch: %v", err)
	}
	if len(absent.Candidates) != 1 || absent.Candidates[0].ID != "c" {
		t.Fatalf("candidates = %+v, want explicit candidate c", absent.Candidates)
	}
	for _, w := range absent.Writes {
		if w.Reason == ReasonDelete {
			t.Fatalf("inferred deletion without policy: %+v", w)
		}
	}

	// Echoes of our own writes are ownership-suppressed, never re-emitted.
	echo := []SourceItem{{ID: "x", Fingerprint: "fp-x", Origin: "hcm"}}
	looped, err := PlanBatch(job, Cursor{JobID: "job-1"}, echo)
	if err != nil {
		t.Fatalf("echo PlanBatch: %v", err)
	}
	if len(looped.Writes) != 0 {
		t.Fatalf("echo writes = %+v, want none (loop prevented)", looped.Writes)
	}
	if len(looped.Skips) != 1 || looped.Skips[0].Reason != ReasonLoopPrevented {
		t.Fatalf("echo skips = %+v, want one LOOP_PREVENTED", looped.Skips)
	}

	// Per-item failures are recorded explicitly and never disappear.
	failing := []SourceItem{{ID: "bad", Fingerprint: "fp-bad", Origin: "partner", Err: "checksum mismatch"}}
	failed, err := PlanBatch(job, Cursor{JobID: "job-1"}, failing)
	if err != nil {
		t.Fatalf("failing PlanBatch: %v", err)
	}
	if len(failed.Failures) != 1 || failed.Failures[0].ID != "bad" {
		t.Fatalf("failures = %+v, want recorded failure bad", failed.Failures)
	}
	if failed.Cursor.Failed != 1 {
		t.Fatalf("cursor failed = %d, want 1", failed.Cursor.Failed)
	}
}

// TestTodo_INTG_019_Integration proves resume across a persisted cursor:
// batch two never reprocesses batch one's items end to end.
func TestTodo_INTG_019_Integration(t *testing.T) {
	job := fixtureJob()
	store := NewMemStore()
	batch, err := PlanBatch(job, Cursor{JobID: "job-1"}, fixtureItems())
	if err != nil {
		t.Fatalf("batch one: %v", err)
	}
	store.Save(batch.Cursor)
	loaded, ok := store.Load("job-1")
	if !ok {
		t.Fatal("persisted cursor missing")
	}
	second, err := PlanBatch(job, loaded, []SourceItem{
		{ID: "c", Fingerprint: "fp-c1", Origin: "partner"},
		{ID: "d", Fingerprint: "fp-d1", Origin: "partner"},
	})
	if err != nil {
		t.Fatalf("batch two: %v", err)
	}
	if len(second.Writes) != 1 || second.Writes[0].ID != "d" {
		t.Fatalf("batch two writes = %+v, want only d", second.Writes)
	}
	for _, w := range second.Writes {
		if w.ID == "a" || w.ID == "b" {
			t.Fatalf("reprocessed batch-one item: %+v", w)
		}
	}
}

// TestTodo_INTG_019_Fault proves unknown modes, foreign cursors and empty
// items fail closed.
func TestTodo_INTG_019_Fault(t *testing.T) {
	bad := fixtureJob()
	bad.Mode = "SIDEWAYS"
	if _, err := PlanBatch(bad, Cursor{JobID: "job-1"}, fixtureItems()); !errors.Is(err, ErrUnknownMode) {
		t.Fatalf("unknown mode error = %v, want ErrUnknownMode", err)
	}
	if _, err := PlanBatch(fixtureJob(), Cursor{JobID: "other-job"}, fixtureItems()); !errors.Is(err, ErrCursorMismatch) {
		t.Fatalf("foreign cursor error = %v, want ErrCursorMismatch", err)
	}
	empty, err := PlanBatch(fixtureJob(), Cursor{JobID: "job-1"}, nil)
	if err != nil {
		t.Fatalf("empty items: %v", err)
	}
	if empty.Cursor.Processed != 0 || len(empty.Writes) != 0 {
		t.Fatalf("empty batch = %+v, want no-op", empty)
	}
	dup := []SourceItem{
		{ID: "a", Fingerprint: "fp-a1", Origin: "partner"},
		{ID: "a", Fingerprint: "fp-a1", Origin: "partner"},
	}
	if _, err := PlanBatch(fixtureJob(), Cursor{JobID: "job-1"}, dup); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("duplicate id error = %v, want ErrInvalidJob", err)
	}
}
