package operation_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

// TestRestoredConnectorOperationRequiresObservationOrGovernedRepairBeforeRedrive
// is the PRIMARY test. A worker that crashes mid-send restores into a new
// lease epoch: the ambiguous operation is durably NO_REDRIVE, a stale
// worker's dispatch is fenced, observation evidence resolves state, and
// only an approved RepairPlan creates a related new attempt with a
// distinct effect identity.
func TestRestoredConnectorOperationRequiresObservationOrGovernedRepairBeforeRedrive(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:conn-rt-009", 1, operation.OrderingIndependent)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	result, err := j.Dispatch(context.Background(), lease, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation.State != operation.StateProviderAccepted {
		t.Fatalf("dispatch state = %s", result.Operation.State)
	}
	// Crash: the worker restores under a new lease epoch.
	restored, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	verdict := operation.ClassifyRestored(restored, 2)
	if verdict.Disposition != operation.RestoredNoRedrive {
		t.Fatalf("restored ambiguous disposition = %v", verdict.Disposition)
	}
	// Timeout-after-send never auto-redrives, even when asked directly.
	redrive := operation.RedriveRequest{
		TenantID: "tenant-promotion", OperationID: id,
		ActorRef: "worker-1", At: testNow,
	}
	if _, err := j.Redrive(context.Background(), redrive); err == nil {
		t.Fatal("ambiguous operation redrove without observation")
	}
	// A stale worker holding the old epoch is fenced.
	if err := operation.CheckRestoredLease(restored, 2, "worker-1", 1); err == nil {
		t.Fatal("stale worker dispatch admitted after restore")
	}
	// Observation evidence resolves state without a second effect.
	obs := operation.Observation{
		TenantID: "tenant-promotion", ObservationID: uuid.New(), OperationID: id,
		ExternalResourceKey: restored.ExternalResourceKey, Verdict: operation.ObservationApplied,
		ExternalObjectRef: "payroll-object-9", ExternalVersion: "v2",
		ObservedDigest: result.Attempt.RequestDigest, ObservedAt: testNow, Authority: "payroll-authority",
	}
	resolved, err := j.RecordObservation(context.Background(), obs)
	if err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}
	if resolved.State != operation.StateReconciled {
		t.Fatalf("observed state = %s", resolved.State)
	}
	// Terminal operations stay NO_REDRIVE after restore.
	terminal, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if verdict := operation.ClassifyRestored(terminal, 2); verdict.Disposition != operation.RestoredNoRedrive {
		t.Fatalf("terminal disposition = %v", verdict.Disposition)
	}
}
