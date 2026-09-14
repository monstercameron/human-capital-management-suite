package readiness

import (
	"testing"
	"time"
)

func TestPilotCutoverDrillMeetsStopGoRollbackRPOAndReconciliationContracts(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	drill := mustCutoverDrill(t, at)
	report := mustExecuteCutover(t, CutoverInput{
		Drill: drill, At: at,
		ObservedRPO: 20 * time.Second, ObservedRTO: 3 * time.Minute,
	})
	if report.Decision != CutoverGoLive {
		t.Fatalf("decision=%v findings=%+v", report.Decision, report.Findings)
	}
	if report.Reconciliation.Applied != 3 || report.Reconciliation.Total != 3 {
		t.Fatalf("reconciliation: %+v", report.Reconciliation)
	}
	if report.Digest == "" {
		t.Fatal("cutover report must carry a digest")
	}
	// An ambiguous delta blocks go-live: the drill rolls back instead of
	// guessing.
	ambiguous := mustCutoverDrill(t, at)
	ambiguous.Deltas[1].Digest = ""
	held := mustExecuteCutover(t, CutoverInput{
		Drill: ambiguous, At: at,
		ObservedRPO: 20 * time.Second, ObservedRTO: 3 * time.Minute,
	})
	if held.Decision != CutoverRollback {
		t.Fatalf("ambiguous delta decision=%v", held.Decision)
	}
}
