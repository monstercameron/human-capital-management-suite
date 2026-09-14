package recovery

import (
	"testing"
	"time"
)

func TestTodo_RECOVERY_004(t *testing.T) {
	scenario := mustGameDayScenario(t, LostDatabase)
	report := mustExecuteGameDay(t, GameDayInput{
		Scenario:    scenario,
		ObservedRPO: 30 * time.Second,
		ObservedRTO: 4 * time.Minute,
		Evidence: GameDayEvidence{
			Incident:   "INC-004-db",
			Advisory:   "ADV-004-db",
			Repair:     "REPAIR-004-db",
			PostReview: "REVIEW-004-db",
		},
	})
	if report.Status != GameDayPass {
		t.Fatalf("status=%v findings=%+v", report.Status, report.Findings)
	}
	if report.Digest == "" {
		t.Fatal("game-day report must carry a digest")
	}
	// Missing RTO blows the budget deterministically: a failed gate, not
	// a silent pass.
	missed := mustExecuteGameDay(t, GameDayInput{
		Scenario:    scenario,
		ObservedRPO: 30 * time.Second,
		ObservedRTO: 30 * time.Minute,
		Evidence: GameDayEvidence{
			Incident:   "INC-004-db",
			Advisory:   "ADV-004-db",
			Repair:     "REPAIR-004-db",
			PostReview: "REVIEW-004-db",
		},
	})
	if missed.Status != GameDayFailedGate {
		t.Fatalf("over-budget RTO passed: %+v", missed)
	}
}
