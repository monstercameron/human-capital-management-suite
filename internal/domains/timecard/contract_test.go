package timecard

import (
	"testing"
	"time"
)

func TestVersionAndExplain(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version() = %d, want 1", Version())
	}
	tc := mustNewTimecard(t)
	now := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)
	tc, err := SetLines(tc, []Line{sessionLine("l1", 120)}, now, "worker-1")
	if err != nil {
		t.Fatalf("SetLines: %v", err)
	}
	exp := Explain(tc)
	if exp.TotalMinutes != 120 || exp.State != Draft || exp.WorkerAttested || exp.Approved {
		t.Fatalf("Explain() = %+v, unexpected", exp)
	}
	if got := tc.Explain(); got != exp {
		t.Fatalf("Timecard.Explain() and Explain() disagree: %+v vs %+v", got, exp)
	}
}
