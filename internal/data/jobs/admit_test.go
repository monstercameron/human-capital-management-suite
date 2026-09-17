package jobs_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

func jobScheduler(t *testing.T) (*jobs.Scheduler, uuid.UUID) {
	t.Helper()
	scheduler, err := jobs.NewScheduler(jobs.SchedulerPolicy{CellID: "cell-a", CellCapacity: 10, ReservedP0: 4, TenantLimit: 100, RetryAllowance: 3, QuotaVersion: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	digest := strings.Repeat("a", 64)
	for _, job := range []string{"payroll-run", "review-batch"} {
		if err := scheduler.Register(tenant, job, digest); err != nil {
			t.Fatal(err)
		}
	}
	return scheduler, tenant
}

func admitRequest(tenant uuid.UUID, job string, priority jobs.Priority, cost int, operation string) jobs.AdmissionRequest {
	return jobs.AdmissionRequest{TenantID: tenant, JobID: job, DefinitionDigest: strings.Repeat("a", 64), Priority: priority, EstimatedCost: cost, OperationID: operation}
}

// TestTodo_JOB_002 proves priority/quota/cost admission: deterministic
// ADMIT|QUEUE|DEFER|DEGRADE|REJECT preserves P0 capacity under a P4 flood
// and every receipt records its budget evidence.
func TestTodo_JOB_002(t *testing.T) {
	scheduler, tenant := jobScheduler(t)
	flood, err := scheduler.Admit(admitRequest(tenant, "payroll-run", jobs.PriorityP4, 6, "op-flood"))
	if err != nil || flood.Outcome != admission.Admit {
		t.Fatalf("flood head = %+v err=%v", flood, err)
	}
	if flood.BudgetID == "" || flood.EvidenceDigest == "" || flood.DecisionID == "" {
		t.Fatalf("admit receipt lacks budget evidence: %+v", flood)
	}
	shed, err := scheduler.Admit(admitRequest(tenant, "payroll-run", jobs.PriorityP4, 1, "op-shed"))
	if err != nil || shed.Outcome != admission.Reject {
		t.Fatalf("P4 over capacity = %+v err=%v", shed, err)
	}
	// P0 capacity survived the flood: the critical run still admits.
	critical, err := scheduler.Admit(admitRequest(tenant, "payroll-run", jobs.PriorityP0, 2, "op-critical"))
	if err != nil || critical.Outcome != admission.Admit {
		t.Fatalf("P0 after flood = %+v err=%v", critical, err)
	}
	queued, err := scheduler.Admit(admitRequest(tenant, "review-batch", jobs.PriorityP2, 2, "op-queued"))
	if err != nil || queued.Outcome != admission.Queue {
		t.Fatalf("P2 under pressure = %+v err=%v", queued, err)
	}
	deferred, err := scheduler.Admit(admitRequest(tenant, "review-batch", jobs.PriorityP3, 2, "op-deferred"))
	if err != nil || deferred.Outcome != admission.Defer {
		t.Fatalf("P3 under pressure = %+v err=%v", deferred, err)
	}
	// The same sequence replays deterministically on a fresh scheduler.
	replay, replayTenant := jobScheduler(t)
	_ = replayTenant
	for i, want := range []admission.Outcome{admission.Admit, admission.Reject, admission.Admit, admission.Queue, admission.Defer} {
		var receipt jobs.AdmissionReceipt
		switch i {
		case 0:
			receipt, err = replay.Admit(admitRequest(replayTenant, "payroll-run", jobs.PriorityP4, 6, "op-flood"))
		case 1:
			receipt, err = replay.Admit(admitRequest(replayTenant, "payroll-run", jobs.PriorityP4, 1, "op-shed"))
		case 2:
			receipt, err = replay.Admit(admitRequest(replayTenant, "payroll-run", jobs.PriorityP0, 2, "op-critical"))
		case 3:
			receipt, err = replay.Admit(admitRequest(replayTenant, "review-batch", jobs.PriorityP2, 2, "op-queued"))
		case 4:
			receipt, err = replay.Admit(admitRequest(replayTenant, "review-batch", jobs.PriorityP3, 2, "op-deferred"))
		}
		if err != nil || receipt.Outcome != want {
			t.Fatalf("replay %d = %+v err=%v, want %s", i, receipt, err, want)
		}
	}
	ledger := scheduler.Ledger()
	if len(ledger) != 5 {
		t.Fatalf("ledger holds %d receipts, want 5", len(ledger))
	}
	seen := map[string]struct{}{}
	for _, receipt := range ledger {
		if receipt.EvidenceDigest == "" || receipt.BudgetID == "" {
			t.Fatalf("receipt lacks budget evidence: %+v", receipt)
		}
		if _, ok := seen[receipt.EvidenceDigest]; ok {
			t.Fatalf("duplicate evidence digest: %+v", receipt)
		}
		seen[receipt.EvidenceDigest] = struct{}{}
	}
}

func TestTodo_JOB_002_Race(t *testing.T) {
	scheduler, err := jobs.NewScheduler(jobs.SchedulerPolicy{CellID: "cell-race", CellCapacity: 64, TenantLimit: 64, RetryAllowance: 3, QuotaVersion: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	if err := scheduler.Register(tenant, "race-job", strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	const racers = 32
	var wg sync.WaitGroup
	receipts := make([]jobs.AdmissionReceipt, racers)
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			receipt, err := scheduler.Admit(jobs.AdmissionRequest{TenantID: tenant, JobID: "race-job", DefinitionDigest: strings.Repeat("b", 64), Priority: jobs.PriorityP4, EstimatedCost: 1, OperationID: "op-race"})
			receipts[i], errs[i] = receipt, err
		}(i)
	}
	wg.Wait()
	admitted := 0
	for i := 0; i < racers; i++ {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if receipts[i].Outcome == admission.Admit {
			admitted++
		}
	}
	// One shared retry budget serves the whole race: every receipt names
	// the same budget while carrying its own evidence digest.
	for i := 0; i < racers; i++ {
		if receipts[i].BudgetID != receipts[0].BudgetID {
			t.Fatalf("racer %d budget %s diverges from %s", i, receipts[i].BudgetID, receipts[0].BudgetID)
		}
	}
	if admitted != racers {
		t.Fatalf("admitted %d of %d within capacity", admitted, racers)
	}
	if got := scheduler.CellUsed(); got != racers {
		t.Fatalf("cell used = %d, want %d", got, racers)
	}
	if len(scheduler.Ledger()) != racers {
		t.Fatalf("ledger holds %d receipts, want %d", len(scheduler.Ledger()), racers)
	}
}

func TestTodo_JOB_002_Fault(t *testing.T) {
	scheduler, tenant := jobScheduler(t)
	unknown := admitRequest(tenant, "never-published", jobs.PriorityP0, 1, "op-unknown")
	if _, err := scheduler.Admit(unknown); !errors.Is(err, jobs.ErrUnknownJob) {
		t.Fatalf("unknown job error = %v", err)
	}
	forged := admitRequest(tenant, "payroll-run", jobs.PriorityP0, 1, "op-forged")
	forged.DefinitionDigest = strings.Repeat("f", 64)
	if _, err := scheduler.Admit(forged); !errors.Is(err, jobs.ErrUnknownJob) {
		t.Fatalf("digest mismatch error = %v", err)
	}
	zeroCost := admitRequest(tenant, "payroll-run", jobs.PriorityP0, 0, "op-zero")
	if _, err := scheduler.Admit(zeroCost); !errors.Is(err, jobs.ErrAdmissionInvalid) {
		t.Fatalf("zero cost error = %v", err)
	}
	negative := admitRequest(tenant, "payroll-run", jobs.PriorityP0, -2, "op-negative")
	if _, err := scheduler.Admit(negative); !errors.Is(err, jobs.ErrAdmissionInvalid) {
		t.Fatalf("negative cost error = %v", err)
	}
	blankJob := admitRequest(tenant, "  ", jobs.PriorityP0, 1, "op-blank")
	if _, err := scheduler.Admit(blankJob); !errors.Is(err, jobs.ErrAdmissionInvalid) {
		t.Fatalf("blank job error = %v", err)
	}
	noTenant := admitRequest(uuid.Nil, "payroll-run", jobs.PriorityP0, 1, "op-notenant")
	if _, err := scheduler.Admit(noTenant); !errors.Is(err, jobs.ErrAdmissionInvalid) {
		t.Fatalf("nil tenant error = %v", err)
	}
	badPriority := admitRequest(tenant, "payroll-run", jobs.Priority("P9"), 1, "op-badprio")
	if _, err := scheduler.Admit(badPriority); !errors.Is(err, jobs.ErrAdmissionInvalid) {
		t.Fatalf("bad priority error = %v", err)
	}
	// A full cell defers even P0: the critical run waits instead of
	// overrunning capacity.
	full, fullTenant := jobScheduler(t)
	if _, err := full.Admit(admitRequest(fullTenant, "payroll-run", jobs.PriorityP4, 6, "op-fill-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := full.Admit(admitRequest(fullTenant, "payroll-run", jobs.PriorityP0, 4, "op-fill-2")); err != nil {
		t.Fatal(err)
	}
	over, err := full.Admit(admitRequest(fullTenant, "payroll-run", jobs.PriorityP0, 1, "op-over"))
	if err != nil || over.Outcome != admission.Defer {
		t.Fatalf("P0 over absolute capacity = %+v err=%v", over, err)
	}
	if _, err := jobs.NewScheduler(jobs.SchedulerPolicy{}); !errors.Is(err, jobs.ErrAdmissionInvalid) {
		t.Fatalf("empty policy error = %v", err)
	}
}

func BenchmarkTodo_JOB_002(b *testing.B) {
	scheduler, err := jobs.NewScheduler(jobs.SchedulerPolicy{CellID: "cell-bench", CellCapacity: 1 << 30, TenantLimit: 1 << 30, RetryAllowance: 3, QuotaVersion: "v1"})
	if err != nil {
		b.Fatal(err)
	}
	tenant := uuid.New()
	if err := scheduler.Register(tenant, "bench-job", strings.Repeat("c", 64)); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scheduler.Admit(jobs.AdmissionRequest{TenantID: tenant, JobID: "bench-job", DefinitionDigest: strings.Repeat("c", 64), Priority: jobs.PriorityP2, EstimatedCost: 1, OperationID: "op-bench"})
		if err != nil {
			b.Fatal(err)
		}
	}
}
