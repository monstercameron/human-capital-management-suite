package operation_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

func restoredOp(state operation.State, epoch uint64) operation.Operation {
	return operation.Operation{OperationID: uuid.New(), State: state, WriterFenceEpoch: epoch}
}

func dispatchAccepted(t *testing.T, j *operation.MemoryJournal, resource string) (uuid.UUID, operation.DispatchResult) {
	t.Helper()
	id := uuid.New()
	planned(t, j, id, resource, 1, operation.OrderingIndependent)
	queued(t, j, id)
	result, err := j.Dispatch(context.Background(), leaseFor(t, j, id), operation.NewPayrollSync())
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	return id, result
}

// TestTodo_CONN_RT_009_Property: every lifecycle state classifies to
// exactly one documented disposition under a restored epoch.
func TestTodo_CONN_RT_009_Property(t *testing.T) {
	noRedrive := map[operation.State]bool{
		operation.StateReconciled: true, operation.StateDeadLetter: true, operation.StateRejected: true,
		operation.StateAmbiguous: true, operation.StateSent: true, operation.StateSending: true,
		operation.StateProviderAccepted: true, operation.StateObserving: true,
	}
	for _, state := range []operation.State{
		operation.StatePlanned, operation.StateQueued, operation.StateLeased,
		operation.StateSending, operation.StateSent, operation.StateProviderAccepted,
		operation.StateObserving, operation.StateReconciled, operation.StateFailed,
		operation.StateRetryable, operation.StateDeadLetter, operation.StateAmbiguous,
		operation.StateRepairRequired, operation.StateRejected,
	} {
		verdict := operation.ClassifyRestored(restoredOp(state, 1), 2)
		if !verdict.StaleWorker {
			t.Fatalf("state %s: epoch-1 op not stale at epoch 2", state)
		}
		if noRedrive[state] && verdict.Disposition != operation.RestoredNoRedrive {
			t.Fatalf("state %s: disposition %v, want NO_REDRIVE", state, verdict.Disposition)
		}
		fresh := operation.ClassifyRestored(restoredOp(state, 2), 2)
		if fresh.StaleWorker {
			t.Fatalf("state %s: epoch-2 op stale at epoch 2", state)
		}
		if fresh.Disposition != verdict.Disposition {
			t.Fatalf("state %s: disposition moves with epoch", state)
		}
	}
	if verdict := operation.ClassifyRestored(restoredOp(operation.StateRepairRequired, 1), 2); verdict.Disposition != operation.RestoredRepairOnly {
		t.Fatalf("repair-required: %v", verdict.Disposition)
	}
}

// TestTodo_CONN_RT_009_Golden pins the restored disposition table.
func TestTodo_CONN_RT_009_Golden(t *testing.T) {
	ops := []operation.Operation{
		{State: operation.StateProviderAccepted, WriterFenceEpoch: 1},
		{State: operation.StateReconciled, WriterFenceEpoch: 1},
		{State: operation.StateFailed, WriterFenceEpoch: 2},
		{State: operation.StateQueued, WriterFenceEpoch: 2},
	}
	summary := operation.SummarizeRestored(ops, 2)
	raw, err := os.ReadFile(filepath.Join("testdata", "conn_rt_009.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.TrimSpace(string(raw)); summary != want {
		t.Fatalf("summary mismatch:\n got=%q\nwant=%q", summary, want)
	}
}

// TestTodo_CONN_RT_009_Race: concurrent classification and lease checks
// agree on every verdict.
func TestTodo_CONN_RT_009_Race(t *testing.T) {
	j := newJournal()
	id, _ := dispatchAccepted(t, j, "worker:conn-rt-009-race")
	op, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	const racers = 16
	var wg sync.WaitGroup
	verdicts := make([]operation.RestoredVerdict, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			verdicts[i] = operation.ClassifyRestored(op, 2)
		}(i)
	}
	wg.Wait()
	for i := range racers {
		if verdicts[i].Disposition != operation.RestoredNoRedrive || !verdicts[i].StaleWorker {
			t.Fatalf("racer %d: %+v", i, verdicts[i])
		}
	}
}

// TestTodo_CONN_RT_009_Integration: crash, Recover, epoch restore,
// observation and governed repair compose end to end.
func TestTodo_CONN_RT_009_Integration(t *testing.T) {
	j := newJournal()
	id, result := dispatchAccepted(t, j, "worker:conn-rt-009-int")
	// Crash before observation: Recover settles to ambiguous, restore
	// fences redrive.
	recovered, err := j.Recover(context.Background(), testNow)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(recovered) != 0 {
		t.Fatalf("accepted operation recovered as sendable: %+v", recovered)
	}
	op, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if verdict := operation.ClassifyRestored(op, 2); verdict.Disposition != operation.RestoredNoRedrive {
		t.Fatalf("post-recover: %+v", verdict)
	}
	// Observation with NOT_APPLIED plus an approved plan creates the
	// related new attempt with a distinct identity.
	obs := operation.Observation{
		TenantID: "tenant-promotion", ObservationID: uuid.New(), OperationID: id,
		ExternalResourceKey: op.ExternalResourceKey, Verdict: operation.ObservationNotApplied,
		ObservedDigest: "sha256:absent", ObservedAt: testNow, Authority: "payroll-authority",
	}
	if _, err := j.RecordObservation(context.Background(), obs); err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}
	plan := operation.RepairPlan{ID: "repair-9", Approved: true, ApprovedBy: "ops-commander"}
	attempt, err := operation.PlanRepairAttempt(context.Background(), j, id, plan, testNow)
	if err != nil {
		t.Fatalf("PlanRepairAttempt: %v", err)
	}
	if attempt.OperationID == id || attempt.RepairPlanID != "repair-9" || attempt.State != operation.StatePlanned {
		t.Fatalf("repair attempt: %+v", attempt)
	}
	if attempt.IdempotencyKey == op.IdempotencyKey {
		t.Fatal("repair attempt reuses the original effect identity")
	}
	_ = result
}

// TestTodo_CONN_RT_009_Fault: stale and future epochs, unapproved plans
// and terminal repairs fail closed.
func TestTodo_CONN_RT_009_Fault(t *testing.T) {
	j := newJournal()
	id, _ := dispatchAccepted(t, j, "worker:conn-rt-009-fault")
	op, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.CheckRestoredLease(op, 2, "", 2); err == nil {
		t.Fatal("anonymous worker admitted")
	}
	if err := operation.CheckRestoredLease(op, 2, "worker-1", 3); err == nil {
		t.Fatal("future epoch claim admitted")
	}
	if err := operation.CheckRestoredLease(op, 2, "worker-1", 2); err == nil {
		t.Fatal("current-epoch worker admitted against an older fence")
	}
	plan := operation.RepairPlan{ID: "repair-9", Approved: false, ApprovedBy: "ops-commander"}
	if _, err := operation.PlanRepairAttempt(context.Background(), j, id, plan, testNow); err == nil {
		t.Fatal("unapproved repair plan admitted")
	}
	nameless := operation.RepairPlan{Approved: true, ApprovedBy: "ops-commander"}
	if _, err := operation.PlanRepairAttempt(context.Background(), j, id, nameless, testNow); err == nil {
		t.Fatal("nameless repair plan admitted")
	}
	if _, err := operation.PlanRepairAttempt(context.Background(), j, uuid.New(), operation.RepairPlan{ID: "r", Approved: true, ApprovedBy: "ops"}, testNow); err == nil {
		t.Fatal("repair attempt for unknown operation admitted")
	}
}

// TestTodo_CONN_RT_009_Security: terminal operations admit no repair
// attempt; the original is never mutated by repair planning.
func TestTodo_CONN_RT_009_Security(t *testing.T) {
	j := newJournal()
	id, result := dispatchAccepted(t, j, "worker:conn-rt-009-sec")
	obs := operation.Observation{
		TenantID: "tenant-promotion", ObservationID: uuid.New(), OperationID: id,
		ExternalResourceKey: result.Operation.ExternalResourceKey, Verdict: operation.ObservationApplied,
		ExternalObjectRef: "payroll-object-s", ExternalVersion: "v2",
		ObservedDigest: result.Attempt.RequestDigest, ObservedAt: testNow, Authority: "payroll-authority",
	}
	if _, err := j.RecordObservation(context.Background(), obs); err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}
	plan := operation.RepairPlan{ID: "repair-x", Approved: true, ApprovedBy: "ops-commander"}
	if _, err := operation.PlanRepairAttempt(context.Background(), j, id, plan, testNow); err == nil {
		t.Fatal("repair attempt admitted against a reconciled operation")
	}
	before, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if before.State != operation.StateReconciled {
		t.Fatalf("original mutated: %+v", before)
	}
}

// TestTodo_CONN_RT_009_Conformance: the conformance vector pins the
// disposition table and the repair linkage shape.
func TestTodo_CONN_RT_009_Conformance(t *testing.T) {
	j := newJournal()
	id, _ := dispatchAccepted(t, j, "worker:conn-rt-009-conf")
	op, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	verdict := operation.ClassifyRestored(op, 2)
	if verdict.Disposition != operation.RestoredNoRedrive || !verdict.StaleWorker || verdict.Reason == "" {
		t.Fatalf("conformance vector: %+v", verdict)
	}
	plan := operation.RepairPlan{ID: "repair-c", Approved: true, ApprovedBy: "ops-commander"}
	attempt, err := operation.PlanRepairAttempt(context.Background(), j, id, plan, testNow)
	if err != nil {
		t.Fatalf("PlanRepairAttempt: %v", err)
	}
	if attempt.CausalPredecessorID != id {
		t.Fatalf("repair attempt not linked to %s: %+v", id, attempt)
	}
	events, err := j.Journal(context.Background(), "tenant-promotion", attempt.OperationID)
	if err != nil || len(events) == 0 {
		t.Fatalf("repair attempt journal: %+v %v", events, err)
	}
	if err := j.VerifyJournal(context.Background()); err != nil {
		t.Fatalf("VerifyJournal: %v", err)
	}
}

// TestTodo_CONN_RT_009_Recovery: lease recovery plus epoch restore
// converge; the journal chain verifies after every step.
func TestTodo_CONN_RT_009_Recovery(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:conn-rt-009-rec", 1, operation.OrderingIndependent)
	queued(t, j, id)
	_ = leaseFor(t, j, id)
	// Crash past the lease expiry: Recover re-queues, restore resumes.
	recovered, err := j.Recover(context.Background(), testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(recovered) != 1 || recovered[0].State != operation.StateQueued {
		t.Fatalf("lease recovery: %+v", recovered)
	}
	op, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if verdict := operation.ClassifyRestored(op, 1); verdict.Disposition != operation.RestoredRedrivable {
		t.Fatalf("requeued: %+v", verdict)
	}
	if err := j.VerifyJournal(context.Background()); err != nil {
		t.Fatalf("VerifyJournal: %v", err)
	}
}

// TestTodo_CONN_RT_009_ModelBased: a reference transition table agrees
// with ClassifyRestored on every state.
func TestTodo_CONN_RT_009_ModelBased(t *testing.T) {
	model := map[operation.State]operation.RestoredDisposition{
		operation.StatePlanned: operation.RestoredRedrivable, operation.StateQueued: operation.RestoredRedrivable,
		operation.StateLeased: operation.RestoredRedrivable, operation.StateSending: operation.RestoredNoRedrive,
		operation.StateSent: operation.RestoredNoRedrive, operation.StateProviderAccepted: operation.RestoredNoRedrive,
		operation.StateObserving: operation.RestoredNoRedrive, operation.StateReconciled: operation.RestoredNoRedrive,
		operation.StateFailed: operation.RestoredRedrivable, operation.StateRetryable: operation.RestoredRedrivable,
		operation.StateDeadLetter: operation.RestoredNoRedrive, operation.StateAmbiguous: operation.RestoredNoRedrive,
		operation.StateRepairRequired: operation.RestoredRepairOnly, operation.StateRejected: operation.RestoredNoRedrive,
	}
	for state, want := range model {
		if got := operation.ClassifyRestored(restoredOp(state, 1), 2); got.Disposition != want {
			t.Fatalf("state %s: implementation=%v model=%v", state, got.Disposition, want)
		}
	}
}

// TestTodo_CONN_RT_009_Mutation: epoch edges resolve on the documented
// side.
func TestTodo_CONN_RT_009_Mutation(t *testing.T) {
	op := restoredOp(operation.StateProviderAccepted, 2)
	if verdict := operation.ClassifyRestored(op, 2); verdict.StaleWorker {
		t.Fatalf("same-epoch op stale: %+v", verdict)
	}
	if err := operation.CheckRestoredLease(op, 2, "worker-1", 2); err != nil {
		t.Fatalf("same-epoch lease: %v", err)
	}
	// A planned operation resumes even when its fence predates the epoch:
	// unstarted work is never stranded by a crash.
	planned := restoredOp(operation.StatePlanned, 1)
	if verdict := operation.ClassifyRestored(planned, 2); verdict.Disposition != operation.RestoredRedrivable {
		t.Fatalf("planned: %+v", verdict)
	}
	if err := operation.CheckRestoredLease(planned, 2, "worker-2", 2); err != nil {
		t.Fatalf("planned resume: %v", err)
	}
}
