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
		WorkerKey: "gone", WorkerID: uuid.New(),
		LifecycleStatus: "TERMINATED", WorkerType: "CONTRACTOR",
	}
	summary := createdWorkerSummary(row)
	if summary.LifecycleStatus != "TERMINATED" {
		t.Fatalf("summary lifecycle = %q, want TERMINATED", summary.LifecycleStatus)
	}
	if summary.WorkerType != "CONTRACTOR" {
		t.Fatalf("summary worker type = %q, want CONTRACTOR", summary.WorkerType)
	}
	if summary.WorkerRef != "gone" {
		t.Fatalf("summary ref = %q, want gone", summary.WorkerRef)
	}

	active := workforce.WorkerRow{
		WorkerKey: "staying", WorkerID: uuid.New(),
		LifecycleStatus: "ACTIVE", WorkerType: "EMPLOYEE",
	}
	activeSummary := createdWorkerSummary(active)
	if activeSummary.LifecycleStatus != "ACTIVE" || activeSummary.WorkerType != "EMPLOYEE" {
		t.Fatalf("active summary = %+v", activeSummary)
	}
}
