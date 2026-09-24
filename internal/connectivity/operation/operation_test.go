package operation_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

var testNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func newJournal() *operation.MemoryJournal {
	return operation.NewMemoryJournal(func() time.Time { return testNow })
}

func request(id uuid.UUID, resource string, sequence uint64, class operation.OrderingClass) operation.PlanRequest {
	return operation.PlanRequest{
		OperationID: id, TenantID: "tenant-promotion", ConnectionID: "payroll-connection", ConnectorVersion: "payroll.v1",
		BusinessTransactionID: "business-promotion-1", WorkflowInstanceID: "workflow-promotion-1", SemanticOperation: "payroll.sync",
		Direction: "OUTBOUND", Criticality: "P1", ExternalResourceKey: resource, OrderingClass: class, ResourceSequence: sequence,
		ExpectedExternalVersion: "v1", SourceAuthorityDecisionRef: "authority.payroll", AuthorityPolicyFingerprint: "authz-v1", WriterFenceEpoch: 1,
		CanonicalInputRef: "input/promotion-1", CanonicalInputDigest: "sha256:canonical", MappingProfileVersion: "payroll-map-v1",
		MappedPayloadRef: "payload/promotion-1", MappedPayloadDigest: "sha256:mapped", MappedPayload: []byte(`{"worker":"w-1","amount":"100.00"}`),
		Classification: "CONFIDENTIAL", Purpose: "PAYROLL_SYNC", DestinationRef: "payroll.example", CredentialRef: "secretref://payroll/production",
		IdempotencyKey: "promotion-1:" + resource + ":" + string(rune('0'+sequence)), ObservationRequirement: operation.ObservationBySemanticIdentity,
		CreatedAt: testNow, DeadlineAt: testNow.Add(time.Hour),
	}
}

func planned(t *testing.T, j *operation.MemoryJournal, id uuid.UUID, resource string, sequence uint64, class operation.OrderingClass) operation.Operation {
	t.Helper()
	op, err := j.Plan(context.Background(), request(id, resource, sequence, class))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return op
}

func confirmed(op operation.Operation) operation.Revalidation {
	return operation.Revalidation{OperationID: op.OperationID, Confirmed: true, PlanDigest: "plan-v1", CurrentPlanDigest: "plan-v1", Explanation: "exact authority, mapping and writer fence confirmed"}
}

func queued(t *testing.T, j *operation.MemoryJournal, id uuid.UUID) {
	t.Helper()
	if _, err := j.Queue(context.Background(), "tenant-promotion", id); err != nil {
		t.Fatalf("Queue: %v", err)
	}
}

func TestTodo_INTG_011(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	op := planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	if op.State != operation.StatePlanned || op.OperationID != id || len(op.Attempts) != 0 {
		t.Fatalf("planned journal = %+v", op)
	}
	got, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalInputDigest == "" || got.MappedPayloadDigest == "" || got.IdempotencyKey == "" || got.AuthorityPolicyFingerprint == "" {
		t.Fatalf("journal lost dispatch metadata: %+v", got)
	}
}

func TestTodo_INTG_011_Golden(t *testing.T) {
	op := request(uuid.MustParse("00000000-0000-0000-0000-000000000011"), "worker:w-1", 1, operation.OrderingStrict)
	got, err := newJournal().Plan(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(operation.Explain(got), "state=PLANNED") || strings.Contains(operation.Explain(got), "100.00") {
		t.Fatalf("unsafe explanation: %s", operation.Explain(got))
	}
}

func TestTodo_INTG_011_Integration(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:integration", 1, operation.OrderingStrict)
	rows, err := j.List(context.Background(), "tenant-promotion")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].OperationID != id || rows[0].ExternalResourceKey != "worker:integration" {
		t.Fatalf("listed operation=%+v", rows)
	}
}
func TestTodo_INTG_011_Fault(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	if _, err := j.Get(context.Background(), "tenant-promotion", uuid.New()); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("missing row error = %v", err)
	}
}
func TestTodo_INTG_011_Security(t *testing.T) {
	j := newJournal()
	_, err := j.Plan(context.Background(), request(uuid.New(), "worker:w-1", 1, operation.OrderingStrict))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := j.List(context.Background(), "other-tenant")
	if err != nil || len(rows) != 0 {
		t.Fatalf("cross-tenant rows = %v, err=%v", rows, err)
	}
}
func TestTodo_INTG_011_Mutation(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	op := planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	op.MappedPayload[0] = 'X'
	got, _ := j.Get(context.Background(), "tenant-promotion", id)
	if string(got.MappedPayload) == string(op.MappedPayload) {
		t.Fatal("journal exposed mutable payload")
	}
}
func TestTodo_INTG_011_Race(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = j.Get(context.Background(), "tenant-promotion", id)
			_, _ = j.List(context.Background(), "tenant-promotion")
		}()
	}
	wg.Wait()
}
func FuzzTodo_INTG_011(f *testing.F) {
	f.Add("worker:w-1")
	f.Fuzz(func(t *testing.T, resource string) {
		_ = operation.Explain(operation.Operation{ExternalResourceKey: resource})
	})
}

func TestTodo_INTG_012(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "worker-1", At: testNow, Duration: time.Minute, Revalidate: func(op operation.Operation) operation.Revalidation { return confirmed(op) }})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Token == uuid.Nil || lease.FenceToken != 1 {
		t.Fatalf("lease = %+v", lease)
	}
}
func TestTodo_INTG_012_Integration(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:lease", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	got, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != operation.StateLeased || got.FenceToken != lease.FenceToken {
		t.Fatalf("lease was not recorded: operation=%+v lease=%+v", got, lease)
	}
}
func TestTodo_INTG_012_Fault(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	_, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "worker-1", At: testNow, Duration: time.Minute, Revalidate: func(op operation.Operation) operation.Revalidation {
		return operation.Revalidation{OperationID: op.OperationID, Requirement: "BLOCK", Explanation: "writer fence is stale"}
	}})
	if !errors.Is(err, operation.ErrRevalidationBlocked) {
		t.Fatalf("lease error = %v", err)
	}
	op, _ := j.Get(context.Background(), "tenant-promotion", id)
	if op.State != operation.StateRejected {
		t.Fatalf("blocked operation = %s", op.State)
	}
}
func TestTodo_INTG_012_Security(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	_, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "worker-1", At: testNow, Duration: time.Minute, Revalidate: func(op operation.Operation) operation.Revalidation {
		return operation.Revalidation{OperationID: op.OperationID, Confirmed: true, PlanDigest: "old", CurrentPlanDigest: "new"}
	}})
	if !errors.Is(err, operation.ErrRevalidationBlocked) {
		t.Fatalf("plan mismatch = %v", err)
	}
}
func TestTodo_INTG_012_Mutation(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:lease-mutation", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	lease.FenceToken++
	if _, err := j.Dispatch(context.Background(), lease, operation.NewPayrollSync()); !errors.Is(err, operation.ErrLeaseExpired) {
		t.Fatalf("modified fence token dispatch error=%v", err)
	}
	got, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != operation.StateLeased {
		t.Fatalf("invalid lease changed operation to %s", got.State)
	}
}
func TestTodo_INTG_012_Race(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:lease-race", 1, operation.OrderingStrict)
	queued(t, j, id)
	start := make(chan struct{})
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: fmt.Sprintf("worker-%d", i), At: testNow, Duration: time.Minute, Revalidate: confirmed})
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, operation.ErrLeaseFenced) {
			t.Fatalf("unexpected lease contention error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful leases=%d, want 1", successes)
	}
}
func FuzzTodo_INTG_012(f *testing.F) {
	f.Add("BLOCK")
	f.Fuzz(func(t *testing.T, reason string) {
		_ = operation.Explain(operation.Operation{ResponseClass: operation.ResponseClass(reason)})
	})
}

func TestTodo_TX_010(t *testing.T) {
	j := newJournal()
	first := uuid.New()
	second := uuid.New()
	planned(t, j, first, "worker:w-1", 1, operation.OrderingStrict)
	planned(t, j, second, "worker:w-1", 2, operation.OrderingStrict)
	queued(t, j, first)
	queued(t, j, second)
	_, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: second, WorkerID: "worker-2", At: testNow, Duration: time.Minute, Revalidate: confirmed})
	if !errors.Is(err, operation.ErrCausalBlocked) {
		t.Fatalf("overtaking lease = %v", err)
	}
	firstLease, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: first, WorkerID: "worker-1", At: testNow, Duration: time.Minute, Revalidate: confirmed})
	if err != nil {
		t.Fatal(err)
	}
	writer := operation.NewPayrollSync()
	if _, err := j.Dispatch(context.Background(), firstLease, writer); err != nil {
		t.Fatal(err)
	}
	_, err = j.RecordObservation(context.Background(), operation.Observation{TenantID: "tenant-promotion", ObservationID: uuid.New(), OperationID: first, ExternalResourceKey: "worker:w-1", Verdict: operation.ObservationApplied, ObservedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: second, WorkerID: "worker-2", At: testNow, Duration: time.Minute, Revalidate: confirmed}); err != nil {
		t.Fatalf("second operation did not progress after predecessor: %v", err)
	}
}
func TestTodo_TX_010_Race(t *testing.T) {
	j := newJournal()
	a := uuid.New()
	b := uuid.New()
	planned(t, j, a, "worker:a", 1, operation.OrderingStrict)
	planned(t, j, b, "worker:b", 1, operation.OrderingStrict)
	queued(t, j, a)
	queued(t, j, b)
	var wg sync.WaitGroup
	for _, id := range []uuid.UUID{a, b} {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			_, _ = j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "worker", At: testNow, Duration: time.Minute, Revalidate: confirmed})
		}(id)
	}
	wg.Wait()
}
func TestTodo_TX_010_Mutation(t *testing.T) {
	j := newJournal()
	a, b := uuid.New(), uuid.New()
	planned(t, j, a, "worker:ordered", 1, operation.OrderingStrict)
	planned(t, j, b, "worker:ordered", 2, operation.OrderingStrict)
	queued(t, j, a)
	queued(t, j, b)
	if _, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: b, WorkerID: "worker-b", At: testNow, Duration: time.Minute, Revalidate: confirmed}); !errors.Is(err, operation.ErrCausalBlocked) {
		t.Fatalf("sequence mutation bypassed predecessor: %v", err)
	}
}

func leaseFor(t *testing.T, j *operation.MemoryJournal, id uuid.UUID) operation.Lease {
	t.Helper()
	lease, err := j.Lease(context.Background(), operation.LeaseRequest{TenantID: "tenant-promotion", OperationID: id, WorkerID: "worker-1", At: testNow, Duration: time.Minute, Revalidate: confirmed})
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestTodo_INTG_013(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	result, err := j.Dispatch(context.Background(), lease, writer)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ProviderCall || !result.ObservationRequired || len(writer.Calls()) != 1 {
		t.Fatalf("dispatch = %+v calls=%d", result, len(writer.Calls()))
	}
	if result.Attempt.ProviderResult != operation.ResponseSuccess || result.Operation.State != operation.StateProviderAccepted {
		t.Fatalf("dispatch outcome = %+v", result.Operation)
	}
	if len(writer.Calls()[0].Payload) == 0 || writer.Calls()[0].IdempotencyKey == "" {
		t.Fatal("provider did not receive stable governed request")
	}
}
func TestTodo_INTG_013_Integration(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:dispatch-integration", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	result, err := j.Dispatch(context.Background(), lease, writer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation.State != operation.StateProviderAccepted || result.Attempt.ProviderResult != operation.ResponseSuccess || result.Attempt.ProviderRequestID == "" {
		t.Fatalf("dispatch evidence incomplete: %+v", result)
	}
}
func TestTodo_INTG_013_Fault(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	writer.TimeoutAfterSend = true
	result, err := j.Dispatch(context.Background(), lease, writer)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if result.Operation.State != operation.StateAmbiguous {
		t.Fatalf("timeout state = %s", result.Operation.State)
	}
}
func TestTodo_INTG_013_Recovery(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:dispatch-recovery", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	writer.TimeoutAfterSend = true
	first, err := j.Dispatch(context.Background(), lease, writer)
	if err == nil || first.Operation.State != operation.StateAmbiguous {
		t.Fatalf("ambiguous send result=%+v err=%v", first, err)
	}
	resolved, err := j.RecordObservation(context.Background(), operation.Observation{TenantID: "tenant-promotion", ObservationID: uuid.New(), OperationID: id, ExternalResourceKey: "worker:dispatch-recovery", Verdict: operation.ObservationApplied, ObservedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != operation.StateReconciled || len(writer.Calls()) != 1 {
		t.Fatalf("recovery=%+v calls=%d", resolved, len(writer.Calls()))
	}
}
func TestTodo_INTG_013_Mutation(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:payload-copy", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	if _, err := j.Dispatch(context.Background(), lease, writer); err != nil {
		t.Fatal(err)
	}
	calls := writer.Calls()
	calls[0].Payload[0] = 'X'
	if string(writer.Calls()[0].Payload) == string(calls[0].Payload) {
		t.Fatal("writer exposed mutable call payload")
	}
}
func TestTodo_INTG_013_Race(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:dispatch-race", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	start := make(chan struct{})
	var wg sync.WaitGroup
	type dispatchResult struct {
		providerCall bool
		err          error
	}
	results := make(chan dispatchResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := j.Dispatch(context.Background(), lease, writer)
			results <- dispatchResult{providerCall: result.ProviderCall, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, already := 0, 0
	for result := range results {
		if result.providerCall {
			success++
		}
		if errors.Is(result.err, operation.ErrLeaseRequired) || (result.err == nil && !result.providerCall) {
			already++
		} else if result.err != nil {
			t.Fatalf("unexpected concurrent dispatch error: %v", result.err)
		}
	}
	if success != 1 || already != 1 || len(writer.Calls()) != 1 {
		t.Fatalf("dispatch successes=%d rejected=%d calls=%d", success, already, len(writer.Calls()))
	}
}
func FuzzTodo_INTG_013(f *testing.F) {
	f.Add("payroll")
	f.Fuzz(func(t *testing.T, key string) { _ = operation.Explain(operation.Operation{IdempotencyKey: key}) })
}

func TestTodo_INTG_014(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	writer.TimeoutAfterSend = true
	_, _ = j.Dispatch(context.Background(), lease, writer)
	before := len(writer.Calls())
	op, _ := j.Get(context.Background(), "tenant-promotion", id)
	if op.State != operation.StateAmbiguous || op.ResponseClass != operation.ResponseAmbiguous || len(op.Attempts) != 1 {
		t.Fatalf("ambiguous journal = %+v", op)
	}
	resolved, err := j.RecordObservation(context.Background(), operation.Observation{TenantID: "tenant-promotion", ObservationID: uuid.New(), OperationID: id, ExternalResourceKey: "worker:w-1", Verdict: operation.ObservationApplied, ObservedDigest: "sha256:observed", ObservedAt: testNow})
	if err != nil || resolved.State != operation.StateReconciled || resolved.CompletionState != "COMPLETE" {
		t.Fatalf("resolution = %+v, err=%v", resolved, err)
	}
	if len(writer.Calls()) != before {
		t.Fatal("observation caused a blind provider retry")
	}
}
func TestTodo_INTG_014_Integration(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:observe-integration", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	writer.TimeoutAfterSend = true
	if _, err := j.Dispatch(context.Background(), lease, writer); err == nil {
		t.Fatal("expected ambiguous provider outcome")
	}
	result, err := j.RecordObservation(context.Background(), operation.Observation{TenantID: "tenant-promotion", ObservationID: uuid.New(), OperationID: id, ExternalResourceKey: "worker:observe-integration", Verdict: operation.ObservationApplied, ObservedDigest: "sha256:observed", ObservedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if result.CompletionState != "COMPLETE" || result.ResponseClass != operation.ResponseSuccess {
		t.Fatalf("observation did not complete operation: %+v", result)
	}
}
func TestTodo_INTG_014_Fault(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	writer := operation.NewPayrollSync()
	writer.TimeoutAfterSend = true
	_, _ = j.Dispatch(context.Background(), lease, writer)
	op, err := j.RecordObservation(context.Background(), operation.Observation{TenantID: "tenant-promotion", ObservationID: uuid.New(), OperationID: id, ExternalResourceKey: "worker:w-1", Verdict: operation.ObservationUnknown, ObservedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if op.State != operation.StateRepairRequired || op.ResponseClass != operation.ResponseUnknown {
		t.Fatalf("unknown resolution = %+v", op)
	}
}
func FuzzTodo_INTG_014(f *testing.F) {
	f.Add("UNKNOWN")
	f.Fuzz(func(t *testing.T, verdict string) {
		_ = operation.Explain(operation.Operation{ResponseClass: operation.ResponseClass(verdict)})
	})
}

func TestTodo_INTG_016(t *testing.T) {
	j := newJournal()
	failedID := uuid.New()
	siblingID := uuid.New()
	planned(t, j, failedID, "worker:w-1", 1, operation.OrderingStrict)
	planned(t, j, siblingID, "worker:w-2", 1, operation.OrderingStrict)
	queued(t, j, failedID)
	queued(t, j, siblingID)
	failedLease := leaseFor(t, j, failedID)
	writer := operation.NewPayrollSync()
	writer.TimeoutAfterSend = false
	writerErrWriter := &failWriter{}
	_, err := j.Dispatch(context.Background(), failedLease, writerErrWriter)
	if err == nil {
		t.Fatal("failed operation unexpectedly succeeded")
	}
	preview, err := j.PreviewRedrive(context.Background(), operation.RedriveRequest{TenantID: "tenant-promotion", OperationID: failedID, CurrentMappingProfileVersion: "payroll-map-v1", CurrentMappedPayloadDigest: "sha256:mapped", CurrentAuthorityPolicyFingerprint: "authz-v1", CurrentWriterFenceEpoch: 1})
	if err != nil || !preview.Compatible {
		t.Fatalf("preview = %+v err=%v", preview, err)
	}
	redriven, err := j.Redrive(context.Background(), operation.RedriveRequest{TenantID: "tenant-promotion", OperationID: failedID, ActorRef: "operator:payroll", At: testNow, CurrentMappingProfileVersion: "payroll-map-v1", CurrentMappedPayloadDigest: "sha256:mapped", CurrentAuthorityPolicyFingerprint: "authz-v1", CurrentWriterFenceEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	if redriven.OperationID != failedID || redriven.RedriveCount != 1 || redriven.State != operation.StateQueued {
		t.Fatalf("redriven = %+v", redriven)
	}
	sibling, _ := j.Get(context.Background(), "tenant-promotion", siblingID)
	if sibling.RedriveCount != 0 || sibling.State != operation.StateQueued {
		t.Fatalf("sibling changed = %+v", sibling)
	}
}
func TestTodo_INTG_016_Integration(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:redrive-integration", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	if _, err := j.Dispatch(context.Background(), lease, &failWriter{}); err == nil {
		t.Fatal("expected provider failure")
	}
	preview, err := j.PreviewRedrive(context.Background(), operation.RedriveRequest{TenantID: "tenant-promotion", OperationID: id, CurrentMappingProfileVersion: "payroll-map-v1", CurrentMappedPayloadDigest: "sha256:mapped", CurrentAuthorityPolicyFingerprint: "authz-v1", CurrentWriterFenceEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Compatible || preview.OperationID != id {
		t.Fatalf("redrive preview=%+v", preview)
	}
}
func TestTodo_INTG_016_Fault(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:w-1", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	_, _ = j.Dispatch(context.Background(), lease, &failWriter{})
	_, err := j.Redrive(context.Background(), operation.RedriveRequest{TenantID: "tenant-promotion", OperationID: id, ActorRef: "operator:payroll", CurrentMappingProfileVersion: "changed", CurrentMappedPayloadDigest: "sha256:new", CurrentAuthorityPolicyFingerprint: "authz-v2", CurrentWriterFenceEpoch: 2})
	if !errors.Is(err, operation.ErrRedriveApprovalRequired) {
		t.Fatalf("unapproved material redrive = %v", err)
	}
}
func TestTodo_INTG_016_Security(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:redrive-security", 1, operation.OrderingStrict)
	queued(t, j, id)
	if rows, err := j.List(context.Background(), "another-tenant"); err != nil || len(rows) != 0 {
		t.Fatalf("other tenant listed operation: rows=%+v err=%v", rows, err)
	}
}
func TestTodo_INTG_016_Mutation(t *testing.T) {
	j := newJournal()
	id := uuid.New()
	planned(t, j, id, "worker:redrive-mutation", 1, operation.OrderingStrict)
	queued(t, j, id)
	lease := leaseFor(t, j, id)
	if _, err := j.Dispatch(context.Background(), lease, &failWriter{}); err == nil {
		t.Fatal("expected provider failure")
	}
	if _, err := j.Redrive(context.Background(), operation.RedriveRequest{TenantID: "tenant-promotion", OperationID: id, ActorRef: "operator:payroll", At: testNow, CurrentMappingProfileVersion: "changed", CurrentMappedPayloadDigest: "sha256:changed", CurrentAuthorityPolicyFingerprint: "changed", CurrentWriterFenceEpoch: 2}); !errors.Is(err, operation.ErrRedriveApprovalRequired) {
		t.Fatalf("material redrive without approval error=%v", err)
	}
	got, err := j.Get(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	if got.RedriveCount != 0 || got.State != operation.StateFailed {
		t.Fatalf("rejected redrive mutated operation: %+v", got)
	}
}
func FuzzTodo_INTG_016(f *testing.F) {
	f.Add("mapping")
	f.Fuzz(func(t *testing.T, change string) {
		_ = operation.Explain(operation.Operation{MappingProfileVersion: change})
	})
}

type failWriter struct{}

func (*failWriter) Write(context.Context, operation.WriteRequest) (operation.WriteResponse, error) {
	return operation.WriteResponse{}, errors.New("provider validation failure")
}
