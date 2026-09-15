package syncjob

import (
	"strings"
	"testing"
)

// FuzzTodo_INTG_019 is INTG-019's oracle. It holds two invariants over
// arbitrary single items: every input lands in exactly one bucket with
// counts that add up, and replaying the returned cursor over the same item
// mints no write (the REFACTOR invariant: an unchanged fingerprint never
// mints a write).
func FuzzTodo_INTG_019(f *testing.F) {
	f.Add("job-1", "hcm", "a", "fp-a1", "partner", "")
	f.Add("job-1", "hcm", "b", "fp-b1", "hcm", "")
	f.Add("job-1", "hcm", "c", "fp-c1", "partner", "boom")
	f.Fuzz(func(t *testing.T, jobID, local, id, fingerprint, origin, failure string) {
		if strings.TrimSpace(jobID) == "" || strings.TrimSpace(local) == "" || strings.TrimSpace(id) == "" {
			t.Skip("empty identity is a caller error, not a fuzz case")
		}
		job := Job{ID: jobID, Mode: ModeIncremental, LocalSystem: local}
		items := []SourceItem{{ID: id, Fingerprint: fingerprint, Origin: origin, Err: failure}}
		batch, err := PlanBatch(job, Cursor{JobID: jobID}, items)
		if err != nil {
			t.Fatalf("PlanBatch: %v", err)
		}
		trimmed := strings.TrimSpace(id)
		landed := 0
		for _, w := range batch.Writes {
			if w.ID == trimmed {
				landed++
			}
		}
		for _, s := range batch.Skips {
			if s.ID == trimmed {
				landed++
			}
		}
		for _, fl := range batch.Failures {
			if fl.ID == trimmed {
				landed++
			}
		}
		if landed != 1 {
			t.Fatalf("item %q landed in %d buckets: %+v", id, landed, batch)
		}
		if got := int64(len(batch.Writes) + len(batch.Skips) + len(batch.Failures)); batch.Cursor.Processed != got {
			t.Fatalf("counts do not add up: %+v", batch.Cursor)
		}
		if failure != "" && len(batch.Failures) != 1 {
			t.Fatalf("failed item not recorded: %+v", batch)
		}
		if failure == "" && origin == local && (len(batch.Skips) != 1 || batch.Skips[0].Reason != ReasonLoopPrevented) {
			t.Fatalf("local echo not loop-suppressed: %+v", batch)
		}
		replay, err := PlanBatch(job, batch.Cursor, items)
		if err != nil {
			t.Fatalf("replay PlanBatch: %v", err)
		}
		if failure == "" && len(replay.Writes) != 0 {
			t.Fatalf("unchanged replay minted writes: %+v", replay.Writes)
		}
	})
}
