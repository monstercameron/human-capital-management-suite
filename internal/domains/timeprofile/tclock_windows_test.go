package timeprofile

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/worktimerules"
)

func TestTodo_WTIME_007_TimeProfilePinnedWindow(t *testing.T) {
	asOf := time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)
	ledger := WorkingTimeLedger{Entries: []WorkingTimeInterval{
		{ID: "w1", TenantID: "tenant-a", WorkerID: "worker-a", Kind: WorkingTimeKindWork, Start: asOf.Add(-9 * time.Hour), End: asOf.Add(-time.Hour), Source: "punch"},
		{ID: "rest", TenantID: "tenant-a", WorkerID: "worker-a", Kind: WorkingTimeKindRest, Start: asOf.Add(-24 * time.Hour), End: asOf.Add(-16 * time.Hour), Source: "schedule"},
	}}
	got, err := ObserveWorkingTimeWindows(ledger, WorkingTimeWindowRequest{TenantID: "tenant-a", WorkerID: "worker-a", AsOf: asOf, ReferencePeriod: 14 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("ObserveWorkingTimeWindows: %v", err)
	}
	if got.DayMinutes != 8*60 || got.WeekMinutes != 8*60 || got.Revision == "" {
		t.Fatalf("window = %+v, want 480 minutes and a revision", got)
	}
	if !got.HasLastRestEnd || !got.LastRestEnd.Equal(asOf.Add(-16*time.Hour)) {
		t.Fatalf("last rest = %v/%t", got.LastRestEnd, got.HasLastRestEnd)
	}
	if err := CheckWorkingTimeRevision(ledger, "tenant-a", "worker-a", got.Revision); err != nil {
		t.Fatalf("pinned revision did not validate: %v", err)
	}
}

func TestTodo_WTIME_007_TimeProfileRecoveryAndIsolation(t *testing.T) {
	asOf := time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)
	ledger := WorkingTimeLedger{Entries: []WorkingTimeInterval{
		{ID: "a", TenantID: "tenant-a", WorkerID: "worker-a", Kind: worktimerules.KindWork, Start: asOf.Add(-2 * time.Hour), End: asOf.Add(-time.Hour), Source: "punch"},
		{ID: "foreign", TenantID: "tenant-b", WorkerID: "worker-a", Kind: worktimerules.KindWork, Start: asOf.Add(-24 * time.Hour), End: asOf, Source: "punch"},
	}}
	before, err := ObserveWorkingTimeWindows(ledger, WorkingTimeWindowRequest{TenantID: "tenant-a", WorkerID: "worker-a", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	corrected := WorkingTimeLedger{Entries: append(append([]WorkingTimeInterval(nil), ledger.Entries...), WorkingTimeInterval{ID: "correction", TenantID: "tenant-a", WorkerID: "worker-a", Kind: worktimerules.KindWork, Start: asOf.Add(-4 * time.Hour), End: asOf.Add(-3 * time.Hour), Source: "correction"})}
	after, err := ObserveWorkingTimeWindows(corrected, WorkingTimeWindowRequest{TenantID: "tenant-a", WorkerID: "worker-a", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision == before.Revision || after.DayMinutes != before.DayMinutes+60 {
		t.Fatalf("corrected window = %+v, before = %+v", after, before)
	}
	if err := CheckWorkingTimeRevision(corrected, "tenant-a", "worker-a", before.Revision); !errors.Is(err, worktimerules.ErrRevisionMismatch) {
		t.Fatalf("stale revision error = %v, want ErrRevisionMismatch", err)
	}
}
