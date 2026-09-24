package app

import (
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

// TestTodo_REV_070_01_Summary proves the listing summary carries
// the row lifecycle tokens: a terminated contractor row projects
// to a terminated contractor summary, and the active default
// passes through untouched rather than invented.
func TestTodo_REV_070_01_Summary(t *testing.T) {
	row := workforce.WorkerRow{
		WorkerID:        uuid.New(),
		LifecycleStatus: "TERMINATED", WorkerType: "CONTRACTOR",
	}
	row.WorkerKey = row.WorkerID.String()
	summary := createdWorkerSummary(row)
	if summary.LifecycleStatus != "TERMINATED" {
		t.Fatalf("summary lifecycle = %q, want TERMINATED", summary.LifecycleStatus)
	}
	if summary.WorkerType != "CONTRACTOR" {
		t.Fatalf("summary worker type = %q, want CONTRACTOR", summary.WorkerType)
	}
	if summary.WorkerRef != "worker-"+row.WorkerID.String()[:8] {
		t.Fatalf("summary ref = %q, want a read-time opaque fallback", summary.WorkerRef)
	}

	activeID := uuid.New()
	active := workforce.WorkerRow{
		WorkerKey: activeID.String(), WorkerID: activeID,
		LifecycleStatus: "ACTIVE", WorkerType: "EMPLOYEE",
	}
	activeSummary := createdWorkerSummary(active)
	if activeSummary.LifecycleStatus != "ACTIVE" || activeSummary.WorkerType != "EMPLOYEE" {
		t.Fatalf("active summary = %+v", activeSummary)
	}
}
