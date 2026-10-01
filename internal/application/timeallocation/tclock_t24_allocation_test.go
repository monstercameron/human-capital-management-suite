package timeallocation

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type tclockT24Authorizer struct {
	denyOrder string
	mu        sync.Mutex
	calls     []WorkOrderAuthorization
}

func (a *tclockT24Authorizer) AuthorizeWorkOrder(_ context.Context, in WorkOrderAuthorization) error {
	a.mu.Lock()
	a.calls = append(a.calls, in)
	a.mu.Unlock()
	if in.WorkOrderID == a.denyOrder {
		return errors.New("member is not authorized for this work order")
	}
	return nil
}

type tclockT24PayrollVerifier struct {
	decision AuthorityDecision
	calls    int
}

func (v *tclockT24PayrollVerifier) VerifyPayrollAuthority(_ context.Context, got AuthorityDecision) error {
	v.calls++
	if got.DecisionID != v.decision.DecisionID || got.DecisionDigest != v.decision.DecisionDigest {
		return errors.New("authority decision does not match server record")
	}
	return nil
}

func tclockT24Request(revision uint64, first, second string) AllocationRequest {
	return AllocationRequest{
		TenantID: "tenant-a", ActorID: "supervisor-1", TimecardID: "tc-1", ApprovedRevision: revision, IdempotencyKey: "allocate-" + first,
		Intervals: []ApprovedInterval{{ID: "interval-1", TimecardID: "tc-1", ApprovedRevision: revision, WorkerID: "worker-1", SourceRef: "timecard/tc-1/line-1", Minutes: 90, Shares: []WorkOrderShare{{ProjectID: "project-a", WorkOrderID: first, LineID: "labor", Minutes: 45}, {ProjectID: "project-a", WorkOrderID: second, LineID: "labor", Minutes: 45}}}},
	}
}

func TestTodo_FTIME_005(t *testing.T) {
	ledger := NewLedger()
	auth := &tclockT24Authorizer{}
	req := tclockT24Request(7, "wo-1", "wo-2")
	got, err := ledger.Allocate(context.Background(), auth, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || got.SourceMinutes != 90 || got.AllocatedMinutes != 90 || len(got.Lines) != 2 || len(got.Deltas) != 2 {
		t.Fatalf("allocation = %+v, want one exact effective revision", got)
	}
	if got.Lines[0].SourceRef == "" || got.Lines[0].TimecardID != "tc-1" || got.Lines[0].ApprovedRevision != 7 {
		t.Fatalf("source link was not retained: %+v", got.Lines[0])
	}
	replayed, err := ledger.Allocate(context.Background(), auth, req)
	if err != nil || replayed.Revision != got.Revision || replayed.Digest != got.Digest {
		t.Fatalf("idempotent replay = %+v, %v; want original revision", replayed, err)
	}
	conflicting := req
	conflicting.Intervals = append([]ApprovedInterval(nil), req.Intervals...)
	conflicting.Intervals[0].Shares = append([]WorkOrderShare(nil), req.Intervals[0].Shares...)
	conflicting.Intervals[0].Shares[0].Minutes = 44
	conflicting.Intervals[0].Shares[1].Minutes = 46
	if _, err := ledger.Allocate(context.Background(), auth, conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed idempotency command = %v, want ErrIdempotencyConflict", err)
	}
	report, err := ledger.Reconcile("tenant-a", "tc-1")
	if err != nil || !report.Balanced || report.BillingDraft.Lines[0].Minutes != 45 || report.BillingDraft.Lines[1].Minutes != 45 {
		t.Fatalf("reconciliation = %+v, %v", report, err)
	}

	corrected := tclockT24Request(8, "wo-1", "wo-3")
	corrected.Intervals[0].Shares[0].Minutes = 30
	corrected.Intervals[0].Shares[1].Minutes = 60
	corrected.IdempotencyKey = "allocate-correction"
	correctedResult, err := ledger.Allocate(context.Background(), auth, corrected)
	if err != nil {
		t.Fatal(err)
	}
	if correctedResult.PreviousRevision != 1 || correctedResult.Revision != 2 || correctedResult.AllocatedMinutes != 90 {
		t.Fatalf("corrected revision = %+v", correctedResult)
	}
	if deltaMinutes(correctedResult.Deltas, "wo-1") != -15 || deltaMinutes(correctedResult.Deltas, "wo-2") != -45 || deltaMinutes(correctedResult.Deltas, "wo-3") != 60 {
		t.Fatalf("correction deltas = %+v", correctedResult.Deltas)
	}
	report, err = ledger.Reconcile("tenant-a", "tc-1")
	if err != nil || report.BillingDraft.Lines[0].Minutes != 30 || report.BillingDraft.Lines[len(report.BillingDraft.Lines)-1].Minutes != 60 {
		t.Fatalf("corrected billing draft = %+v, %v", report.BillingDraft, err)
	}
}

func deltaMinutes(deltas []AllocationDelta, workOrder string) int64 {
	var total int64
	for _, delta := range deltas {
		if delta.WorkOrderID == workOrder {
			total += delta.Minutes
		}
	}
	return total
}

func TestTodo_FTIME_005_Integration(t *testing.T) {
	ledger := NewLedger()
	got, err := ledger.Allocate(context.Background(), &tclockT24Authorizer{}, tclockT24Request(4, "wo-1", "wo-2"))
	if err != nil {
		t.Fatal(err)
	}
	decision := AuthorityDecision{TenantID: "tenant-a", DecisionID: "authority-4", TimecardID: "tc-1", ApprovedRevision: 4, Outcome: AuthorityAccepted, DecidedBy: "payroll-lead", DecisionDigest: "sha256:authority-4"}
	verifier := &tclockT24PayrollVerifier{decision: decision}
	handoff, err := ledger.ExportPayroll(context.Background(), verifier, HandoffRequest{TenantID: "tenant-a", ActorID: "payroll-operator", TimecardID: "tc-1", ApprovedRevision: 4, AllocationRevision: got.Revision, IdempotencyKey: "payroll-1", Authority: decision})
	if err != nil {
		t.Fatal(err)
	}
	if verifier.calls != 1 || handoff.Receipt.Status != ReceiptAccepted || handoff.Receipt.AuthorityDecisionID != decision.DecisionID || handoff.Export.Digest == "" || len(handoff.Export.Lines) != 2 {
		t.Fatalf("handoff = %+v, verifier calls=%d", handoff, verifier.calls)
	}
	if handoff.Export.Lines[0].Minutes+handoff.Export.Lines[1].Minutes != 90 {
		t.Fatalf("payroll export did not conserve approved time: %+v", handoff.Export.Lines)
	}
}

func TestTodo_FTIME_005_Security(t *testing.T) {
	ledger := NewLedger()
	auth := &tclockT24Authorizer{denyOrder: "wo-denied"}
	if _, err := ledger.Allocate(context.Background(), auth, tclockT24Request(1, "wo-allowed", "wo-denied")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("denied project member allocation = %v, want ErrUnauthorized", err)
	}
	if _, err := ledger.Current("tenant-a", "tc-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("denied allocation persisted = %v", err)
	}
	if _, err := ledger.ExportPayroll(context.Background(), &tclockT24PayrollVerifier{}, HandoffRequest{}); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unbound payroll export = %v", err)
	}
	if _, err := ledger.Allocate(context.Background(), auth, AllocationRequest{TenantID: "tenant-a", ActorID: "supervisor-1", TimecardID: "tc-1", ApprovedRevision: 1, IdempotencyKey: "bad", Intervals: []ApprovedInterval{{ID: "same", TimecardID: "tc-1", ApprovedRevision: 1, WorkerID: "worker-1", SourceRef: "src", Minutes: 60, Shares: []WorkOrderShare{{ProjectID: "project-a", WorkOrderID: "wo-1", LineID: "labor", Minutes: 60}, {ProjectID: "project-a", WorkOrderID: "wo-2", LineID: "labor", Minutes: 1}}}}}); !errors.Is(err, ErrUnbalanced) {
		t.Fatalf("over-allocated approved interval = %v, want ErrUnbalanced", err)
	}
}

func TestTodo_FTIME_005_Race(t *testing.T) {
	ledger := NewLedger()
	auth := &tclockT24Authorizer{}
	req := tclockT24Request(2, "wo-1", "wo-2")
	results := make(chan AllocationRevision, 16)
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := ledger.Allocate(context.Background(), auth, req)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	var digest string
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent allocation failed: %v", err)
		}
	}
	for result := range results {
		if result.Revision != 1 {
			t.Fatalf("concurrent replay revision = %d, want 1", result.Revision)
		}
		if digest == "" {
			digest = result.Digest
		} else if result.Digest != digest {
			t.Fatalf("concurrent replay digest changed: %q vs %q", result.Digest, digest)
		}
	}
	current, err := ledger.Current("tenant-a", "tc-1")
	if err != nil || current.Revision != 1 || current.AllocatedMinutes != 90 {
		t.Fatalf("current after concurrent replay = %+v, %v", current, err)
	}
}

func TestTodo_FTIME_005_PayrollRejectedReceipt(t *testing.T) {
	ledger := NewLedger()
	revision, err := ledger.Allocate(context.Background(), &tclockT24Authorizer{}, tclockT24Request(5, "wo-1", "wo-2"))
	if err != nil {
		t.Fatal(err)
	}
	decision := AuthorityDecision{TenantID: "tenant-a", DecisionID: "authority-reject", TimecardID: "tc-1", ApprovedRevision: 5, Outcome: AuthorityRejected, DecidedBy: "payroll-lead", DecisionDigest: "sha256:reject", Reason: "approval was withdrawn"}
	result, err := ledger.ExportPayroll(context.Background(), &tclockT24PayrollVerifier{decision: decision}, HandoffRequest{TenantID: "tenant-a", ActorID: "payroll-operator", TimecardID: "tc-1", ApprovedRevision: 5, AllocationRevision: revision.Revision, IdempotencyKey: "reject-1", Authority: decision})
	if !errors.Is(err, ErrPayrollRejected) || result.Receipt.Status != ReceiptRejected || result.Receipt.Reason == "" || len(result.Export.Lines) != 0 {
		t.Fatalf("rejected handoff = %+v, %v", result, err)
	}
}
