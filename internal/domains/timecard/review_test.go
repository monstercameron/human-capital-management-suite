package timecard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

var updateGolden = os.Getenv("TIMECARD_UPDATE_GOLDEN") == "1"

func punch(id, worker string, at time.Time, event clock.EventType) clock.TimeObservation {
	return clock.TimeObservation{
		Accepted: true, WorkerRef: worker, EventType: event, OccurredAt: at, RecordedAt: at,
		Timezone: "America/Chicago", Digest: "sha256:" + id,
	}
}

func fixturePunches(worker string, day time.Time) []clock.TimeObservation {
	return []clock.TimeObservation{
		punch("in-1", worker, day.Add(9*time.Hour), clock.EventClockIn),
		punch("out-1", worker, day.Add(17*time.Hour), clock.EventClockOut),
	}
}

func compliantComparison() attendance.Result {
	return attendance.Result{Outcome: attendance.Compliant, Context: attendance.EffectiveContext{
		EffectiveAt: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), KnownAt: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC),
		Timezone: "America/Chicago", Calendar: "us-standard",
	}}
}

// TestTodo_FTIME_004 is the PRIMARY test: a review pairs punches into
// complete intervals, an incomplete pair is reported open rather than as a
// silent zero-hour interval, and a reasoned correction appends to history
// instead of overwriting it.
func TestTodo_FTIME_004(t *testing.T) {
	day := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	punches := fixturePunches("worker-1", day)

	view, err := BuildReview("worker-1", "kiosk-1", "America/Chicago", punches, nil, compliantComparison(), nil)
	if err != nil {
		t.Fatalf("BuildReview: %v", err)
	}
	if len(view.PairedIntervals) != 1 || !view.PairedIntervals[0].Complete {
		t.Fatalf("PairedIntervals = %+v, want one complete pair", view.PairedIntervals)
	}
	if view.PairedIntervals[0].Minutes != 8*60 {
		t.Fatalf("Minutes = %d, want %d", view.PairedIntervals[0].Minutes, 8*60)
	}

	// RED: a missing OUT punch must never silently become zero hours -- it
	// must be reported open.
	openPunches := []clock.TimeObservation{punch("in-2", "worker-1", day.Add(9*time.Hour), clock.EventClockIn)}
	view2, err := BuildReview("worker-1", "kiosk-1", "America/Chicago", openPunches, nil, compliantComparison(), nil)
	if err != nil {
		t.Fatalf("BuildReview open: %v", err)
	}
	if len(view2.PairedIntervals) != 1 || view2.PairedIntervals[0].Complete {
		t.Fatalf("PairedIntervals = %+v, want one incomplete pair", view2.PairedIntervals)
	}
	line := LineFromPairedInterval("open-line", view2.PairedIntervals[0])
	if !line.Open || line.Minutes != 0 {
		t.Fatalf("open line = %+v, want Open=true and Minutes=0 by construction", line)
	}
	if err := line.Validate(); err != nil {
		t.Fatalf("an open line with zero minutes must validate: %v", err)
	}

	// A reasoned correction appends to the correction history; the original
	// entry is never removed.
	original := clock.TimeObservation{Accepted: true, Digest: "sha256:in-1", OccurredAt: day.Add(9 * time.Hour)}
	corrected, err := clock.CorrectObservation(clock.CorrectionRequest{
		Original: original, Reason: "worker forgot to clock in on time", ReasonRef: "reason-1",
		AttestationRef: "att-1", ApprovalRef: "appr-1", CorrectedOccurredAt: day.Add(9*time.Hour - 10*time.Minute),
		Impacts: []string{"timecard"}, Now: day.Add(18 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CorrectObservation: %v", err)
	}
	history, err := AppendCorrection(nil, corrected)
	if err != nil {
		t.Fatalf("AppendCorrection: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	history2, err := AppendCorrection(history, corrected)
	if err != nil {
		t.Fatalf("AppendCorrection second: %v", err)
	}
	if len(history2) != 2 || len(history) != 1 {
		t.Fatalf("history must grow by append, not mutate the prior slice: got %d and %d", len(history2), len(history))
	}
	if history2[0].OriginalDigest != history[0].OriginalDigest {
		t.Fatalf("the first correction's evidence changed after a later append")
	}

	// RED: approval must be blocked by an unresolved exception.
	comparison := compliantComparison()
	comparison.Outcome = attendance.Exception
	comparison.Exceptions = []attendance.Finding{{Kind: attendance.MissingException, ShiftID: "s1"}}
	view3, err := BuildReview("worker-1", "kiosk-1", "America/Chicago", punches, nil, comparison, nil)
	if err != nil {
		t.Fatalf("BuildReview with exception: %v", err)
	}
	open := OpenExceptions(view3, nil)
	if len(open) != 1 {
		t.Fatalf("OpenExceptions = %v, want 1 unresolved finding", open)
	}
	resolved := map[string]bool{exceptionKey(view3.Exceptions[0]): true}
	if got := OpenExceptions(view3, resolved); len(got) != 0 {
		t.Fatalf("OpenExceptions after resolution = %v, want none", got)
	}
}

// TestTodo_FTIME_004_Golden pins the exact shape of a review built from a
// fixed fixture. Run with TIMECARD_UPDATE_GOLDEN=1 to refresh it after a
// deliberate change.
func TestTodo_FTIME_004_Golden(t *testing.T) {
	day := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	view, err := BuildReview("worker-1", "kiosk-1", "America/Chicago", fixturePunches("worker-1", day), nil, compliantComparison(), nil)
	if err != nil {
		t.Fatalf("BuildReview: %v", err)
	}
	got := fmt.Sprintf("worker=%s source=%s tz=%s pairs=%d minutes=%d complete=%t outcome=%s",
		view.WorkerID, view.Source, view.Timezone, len(view.PairedIntervals), view.PairedIntervals[0].Minutes,
		view.PairedIntervals[0].Complete, view.ScheduleComparison.Outcome)

	path := filepath.Join("testdata", "ftime_004_review_golden.txt")
	if updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile golden fixture: %v", err)
	}
	if got != string(want) {
		t.Fatalf("review golden mismatch:\n got:  %s\n want: %s", got, string(want))
	}
}

// TestTodo_FTIME_004_Security proves a review never mixes another worker's
// punches into the pinned worker's view.
func TestTodo_FTIME_004_Security(t *testing.T) {
	day := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	mixed := append(fixturePunches("worker-1", day), punch("in-other", "worker-2", day.Add(9*time.Hour), clock.EventClockIn))
	if _, err := BuildReview("worker-1", "kiosk-1", "America/Chicago", mixed, nil, compliantComparison(), nil); !errors.Is(err, ErrReviewRejected) {
		t.Fatalf("BuildReview with a cross-worker punch: got %v, want ErrReviewRejected", err)
	}
}

// TestTodo_FTIME_004_Race proves PairPunches and BuildReview are safe to
// call concurrently on the same immutable evidence and always agree.
func TestTodo_FTIME_004_Race(t *testing.T) {
	day := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	punches := fixturePunches("worker-1", day)
	comparison := compliantComparison()

	var wg sync.WaitGroup
	results := make([]ReviewView, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			view, err := BuildReview("worker-1", "kiosk-1", "America/Chicago", punches, nil, comparison, nil)
			if err != nil {
				t.Errorf("goroutine %d: BuildReview: %v", i, err)
				return
			}
			results[i] = view
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if len(r.PairedIntervals) != 1 || r.PairedIntervals[0].Minutes != 8*60 {
			t.Fatalf("goroutine %d produced %+v, results diverged under concurrency", i, r.PairedIntervals)
		}
	}
}
