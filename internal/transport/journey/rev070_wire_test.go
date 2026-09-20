package journey_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	journey "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// TestTodo_REV_070_01_Wire proves the wire round trip preserves
// the lifecycle tokens: a terminated contractor summary renders
// onto the wire and back unchanged, and an unreported pair stays
// unreported rather than invented.
func TestTodo_REV_070_01_Wire(t *testing.T) {
	in := workspace.WorkerSummary{
		WorkerRef: "gone", WorkerID: "id-gone",
		LifecycleStatus: "TERMINATED", WorkerType: "CONTRACTOR",
	}
	back := journey.WorkerRoundTripForTest(in)
	if back.LifecycleStatus != "TERMINATED" {
		t.Fatalf("wire lifecycle = %q, want TERMINATED", back.LifecycleStatus)
	}
	if back.WorkerType != "CONTRACTOR" {
		t.Fatalf("wire worker type = %q, want CONTRACTOR", back.WorkerType)
	}

	active := workspace.WorkerSummary{
		WorkerRef: "staying", WorkerID: "id-staying",
		LifecycleStatus: "ACTIVE", WorkerType: "EMPLOYEE",
	}
	activeBack := journey.WorkerRoundTripForTest(active)
	if activeBack.LifecycleStatus != "ACTIVE" || activeBack.WorkerType != "EMPLOYEE" {
		t.Fatalf("active wire round trip = %+v", activeBack)
	}

	var bare workspace.WorkerSummary
	bareBack := journey.WorkerRoundTripForTest(bare)
	if bareBack.LifecycleStatus != "" || bareBack.WorkerType != "" {
		t.Fatalf("bare wire round trip invents %+v", bareBack)
	}
}
