package jobs_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/drain"
)

// REV-017-03, drain half: a real admission DEGRADE never called drain Begin,
// so leases never fenced. The Scheduler owns an optional Drainer (nil
// disables fencing); a DEGRADE/REJECT decision raises its fence so leases
// actually fence and drain.

func drainScheduler(t *testing.T) (*jobs.Scheduler, uuid.UUID, *drain.Drainer) {
	t.Helper()
	scheduler, err := jobs.NewScheduler(jobs.SchedulerPolicy{CellID: "cell-a", CellCapacity: 10, ReservedP0: 4, TenantLimit: 100, RetryAllowance: 3, QuotaVersion: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	fencer := drain.NewDrainer()
	scheduler.Drainer = fencer
	tenant := uuid.New()
	digest := strings.Repeat("a", 64)
	for _, job := range []string{"payroll-run", "review-batch"} {
		if err := scheduler.Register(tenant, job, digest); err != nil {
			t.Fatal(err)
		}
	}
	return scheduler, tenant, fencer
}

// TestTodo_REV_017_03_Drain is the REV-017-03 primary test for the drain
// half: a DEGRADE decision on the live batch Scheduler raises the OPS-006
// fence (new leases refused), and the fenced workload drains exactly: the
// safe-pointed in-flight lease completes and Finish partitions it safe.
func TestTodo_REV_017_03_Drain(t *testing.T) {
	scheduler, tenant, fencer := drainScheduler(t)
	if err := fencer.Acquire("partition-p0", "effect-ledger-append"); err != nil {
		t.Fatalf("acquire in-flight lease: %v", err)
	}
	flood, err := scheduler.Admit(admitRequest(tenant, "payroll-run", jobs.PriorityP4, 6, "op-flood"))
	if err != nil || flood.Outcome != admission.Admit {
		t.Fatalf("flood head = %+v err=%v", flood, err)
	}
	degraded, err := scheduler.Admit(admitRequest(tenant, "review-batch", jobs.PriorityP1, 5, "op-degraded"))
	if err != nil || degraded.Outcome != admission.Degrade {
		t.Fatalf("P1 under pressure = %+v err=%v, want DEGRADE", degraded, err)
	}
	if err := fencer.Acquire("partition-new", "effect-x"); !errors.Is(err, drain.ErrDrainFenced) {
		t.Fatalf("post-DEGRADE acquire = %v, want ErrDrainFenced: leases must fence", err)
	}
	if err := fencer.Heartbeat("partition-p0", 1, drain.RegionSafe); err != nil {
		t.Fatalf("heartbeat to safe: %v", err)
	}
	if err := fencer.Complete("partition-p0", 1); err != nil {
		t.Fatalf("complete safe lease: %v", err)
	}
	report, err := fencer.Finish()
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if report.Fence != 1 || len(report.Safe) != 0 || len(report.Ambiguous) != 0 {
		t.Fatalf("drain report = %+v, want fence 1 with the completed safe lease retired", report)
	}
}

// TestTodo_REV_017_03_Drain_Fault proves the fence is steady under sustained
// overload: REJECT fences too, a second DEGRADE keeps the fence without
// failing the decision, and a scheduler without a Drainer keeps the old
// behavior (decisions stand, nothing fenced, no nil panic).
func TestTodo_REV_017_03_Drain_Fault(t *testing.T) {
	scheduler, tenant, fencer := drainScheduler(t)
	if _, err := scheduler.Admit(admitRequest(tenant, "payroll-run", jobs.PriorityP4, 6, "op-flood")); err != nil {
		t.Fatal(err)
	}
	rejected, err := scheduler.Admit(admitRequest(tenant, "payroll-run", jobs.PriorityP4, 1, "op-shed"))
	if err != nil || rejected.Outcome != admission.Reject {
		t.Fatalf("P4 over capacity = %+v err=%v, want REJECT", rejected, err)
	}
	if err := fencer.Acquire("partition-new", "effect-x"); !errors.Is(err, drain.ErrDrainFenced) {
		t.Fatalf("post-REJECT acquire = %v, want ErrDrainFenced", err)
	}
	again, err := scheduler.Admit(admitRequest(tenant, "review-batch", jobs.PriorityP1, 5, "op-degraded-again"))
	if err != nil || again.Outcome != admission.Degrade {
		t.Fatalf("second DEGRADE = %+v err=%v, want the decision to stand under a raised fence", again, err)
	}
	if err := fencer.Acquire("partition-newer", "effect-y"); !errors.Is(err, drain.ErrDrainFenced) {
		t.Fatalf("fence did not hold = %v", err)
	}

	plain, plainTenant := jobScheduler(t)
	shed, err := plain.Admit(admitRequest(plainTenant, "payroll-run", jobs.PriorityP4, 6, "op-plain-flood"))
	if err != nil || shed.Outcome != admission.Admit {
		t.Fatalf("plain flood = %+v err=%v", shed, err)
	}
	degraded, err := plain.Admit(admitRequest(plainTenant, "review-batch", jobs.PriorityP1, 5, "op-plain-degraded"))
	if err != nil || degraded.Outcome != admission.Degrade {
		t.Fatalf("plain P1 under pressure = %+v err=%v, want DEGRADE with no drainer attached", degraded, err)
	}
}

// TestTodo_REV_017_03_Drain_Race proves concurrent admission under pressure
// fences exactly once: every decision stands, no error escapes, and the
// fence holds afterward. The Drainer and the Scheduler are both
// mutex-guarded, so the race detector must stay silent.
func TestTodo_REV_017_03_Drain_Race(t *testing.T) {
	scheduler, tenant, fencer := drainScheduler(t)
	const racers = 8
	var wg sync.WaitGroup
	outcomes := make([]admission.Outcome, racers)
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			receipt, err := scheduler.Admit(admitRequest(tenant, "payroll-run", jobs.PriorityP4, 6, fmt.Sprintf("op-race-%d", i)))
			if err == nil {
				outcomes[i] = receipt.Outcome
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	var admits, rejects int
	for i := 0; i < racers; i++ {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		switch outcomes[i] {
		case admission.Admit:
			admits++
		case admission.Reject:
			rejects++
		default:
			t.Fatalf("racer %d outcome = %s, want ADMIT or REJECT", i, outcomes[i])
		}
	}
	if admits != 1 || rejects != racers-1 {
		t.Fatalf("admits = %d rejects = %d, want exactly 1 admit and %d rejects", admits, rejects, racers-1)
	}
	if err := fencer.Acquire("partition-race", "effect-x"); !errors.Is(err, drain.ErrDrainFenced) {
		t.Fatalf("post-race acquire = %v, want ErrDrainFenced", err)
	}
}
