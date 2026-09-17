package jobs_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// TestTodo_JOB_004_Race proves governed operations converge under
// concurrency: one canceller wins each run, and pause/resume/dispatch
// checks stay mutually consistent on every goroutine.
func TestTodo_JOB_004_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newJob004Fixture(t, "job004-race")
	gov := jobs.NewGovernor()

	const racers = 8
	var wg sync.WaitGroup
	conns := make([]*pgxadapter.Conn, racers)
	for i := range conns {
		conns[i] = appConn(t, fx.db)
	}
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = inTenantTxErr(conns[i], fx.tenant, func(tx dbport.Tx) error {
				_, _, err := gov.CancelRun(ctx, tx, fx.tenant, fx.run.RunID, fx.run.Version, fixedInstant.Add(time.Hour))
				return err
			})
		}(i)
	}
	wg.Wait()
	won := 0
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, jobs.ErrVersionConflict) || errors.Is(err, jobs.ErrIllegalTransition):
			// A lost CAS or an already-terminal run: both are the
			// allowed loser outcomes, never a partial cancel.
		default:
			t.Fatalf("canceller %d err = %v, want nil, ErrVersionConflict or ErrIllegalTransition", i, err)
		}
	}
	if won != 1 {
		t.Fatalf("cancel winners = %d, want exactly 1", won)
	}
	var terminal jobs.JobRun
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		terminal, err = (jobs.RunStore{}).Load(ctx, tx, fx.tenant, fx.run.RunID)
		return err
	})
	if terminal.State != jobs.RunCancelled {
		t.Fatalf("run after raced cancel = %s, want CANCELLED", terminal.State)
	}
	if got := partitionState(t, ctx, fx.conn, fx.tenant, fx.pending.PartitionID); got != jobs.PartitionCancelled {
		t.Fatalf("pending partition after raced cancel = %s, want CANCELLED", got)
	}

	// Pause/resume/dispatch hammering stays mutually consistent.
	fx2 := newJob004Fixture(t, "job004-race-gate")
	const gaters = 16
	gateConns := make([]*pgxadapter.Conn, gaters)
	for i := range gateConns {
		gateConns[i] = appConn(t, fx2.db)
	}
	for i := 0; i < gaters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = inTenantTxErr(gateConns[i], fx2.tenant, func(tx dbport.Tx) error {
				return gov.PauseRun(ctx, tx, fx2.tenant, fx2.run.RunID)
			})
			var gated error
			_ = inTenantTxErr(gateConns[i], fx2.tenant, func(tx dbport.Tx) error {
				gated = gov.CheckDispatchable(ctx, tx, fx2.tenant, fx2.run.RunID)
				return nil
			})
			if gated != nil && !errors.Is(gated, jobs.ErrPaused) {
				t.Errorf("gater %d dispatch err = %v, want nil or ErrPaused", i, gated)
			}
			_ = inTenantTxErr(gateConns[i], fx2.tenant, func(tx dbport.Tx) error {
				return gov.ResumeRun(ctx, tx, fx2.tenant, fx2.run.RunID)
			})
		}(i)
	}
	wg.Wait()
	paused := gov.IsPaused(fx2.tenant, fx2.run.RunID)
	var gated error
	inTenantTx(t, fx2.conn, fx2.tenant, func(tx dbport.Tx) error {
		gated = gov.CheckDispatchable(ctx, tx, fx2.tenant, fx2.run.RunID)
		return nil
	})
	if paused != errors.Is(gated, jobs.ErrPaused) {
		t.Fatalf("paused = %v but dispatch err = %v: gate inconsistent", paused, gated)
	}
}

// TestTodo_JOB_004_Fault proves safe-point refusals: terminal runs cannot
// pause or cancel twice, healthy runs cannot redrive, and cancel or
// redrive mid-flight preserves checkpoints, attempts and partition
// lineage.
func TestTodo_JOB_004_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newJob004Fixture(t, "job004-fault")
	gov := jobs.NewGovernor()

	var completed jobs.JobRun
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		completed, err = (jobs.RunStore{}).Complete(ctx, tx, fx.tenant, fx.run.RunID, fx.run.Version, fixedInstant.Add(time.Hour))
		return err
	})
	err := inTenantTxErr(fx.conn, fx.tenant, func(tx dbport.Tx) error {
		return gov.PauseRun(ctx, tx, fx.tenant, fx.run.RunID)
	})
	if !errors.Is(err, jobs.ErrIllegalTransition) {
		t.Fatalf("pause terminal run err = %v, want ErrIllegalTransition", err)
	}
	err = inTenantTxErr(fx.conn, fx.tenant, func(tx dbport.Tx) error {
		_, _, err := gov.CancelRun(ctx, tx, fx.tenant, fx.run.RunID, completed.Version, fixedInstant.Add(2*time.Hour))
		return err
	})
	if !errors.Is(err, jobs.ErrIllegalTransition) {
		t.Fatalf("cancel terminal run err = %v, want ErrIllegalTransition", err)
	}
	err = inTenantTxErr(fx.conn, fx.tenant, func(tx dbport.Tx) error {
		_, err := gov.RedriveRun(ctx, tx, fx.tenant, fx.run.RunID, completed.Version, fixedInstant.Add(3*time.Hour))
		return err
	})
	if !errors.Is(err, jobs.ErrIllegalTransition) {
		t.Fatalf("redrive completed run err = %v, want ErrIllegalTransition", err)
	}

	// Cancel mid-flight settles open partitions and keeps evidence.
	fx2 := newJob004Fixture(t, "job004-fault-cancel")
	var cancelled jobs.JobRun
	var settled int
	inTenantTx(t, fx2.conn, fx2.tenant, func(tx dbport.Tx) error {
		var err error
		cancelled, settled, err = gov.CancelRun(ctx, tx, fx2.tenant, fx2.run.RunID, fx2.run.Version, fixedInstant.Add(time.Hour))
		return err
	})
	if settled != 2 || cancelled.Attempt != 1 {
		t.Fatalf("mid-flight cancel settled %d attempt %d, want 2 and lineage attempt 1", settled, cancelled.Attempt)
	}
	if n := checkpointRows(t, ctx, fx2.conn, fx2.tenant, fx2.claimed.PartitionID); n != fx2.checkpts {
		t.Fatalf("checkpoints after mid-flight cancel = %d, want %d", n, fx2.checkpts)
	}
	err = inTenantTxErr(fx2.conn, fx2.tenant, func(tx dbport.Tx) error {
		_, _, err := gov.CancelRun(ctx, tx, fx2.tenant, fx2.run.RunID, cancelled.Version, fixedInstant.Add(2*time.Hour))
		return err
	})
	if !errors.Is(err, jobs.ErrIllegalTransition) {
		t.Fatalf("second cancel err = %v, want ErrIllegalTransition", err)
	}

	// Redrive after a crash keeps the prior attempt's checkpoints and
	// leaves settled partitions untouched.
	fx3 := newJob004Fixture(t, "job004-fault-redrive")
	var failed jobs.JobRun
	inTenantTx(t, fx3.conn, fx3.tenant, func(tx dbport.Tx) error {
		var err error
		failed, err = (jobs.RunStore{}).Fail(ctx, tx, fx3.tenant, fx3.run.RunID, fx3.run.Version, fixedInstant.Add(2*time.Hour), "effect failed")
		return err
	})
	var redriven jobs.JobRun
	inTenantTx(t, fx3.conn, fx3.tenant, func(tx dbport.Tx) error {
		var err error
		redriven, err = gov.RedriveRun(ctx, tx, fx3.tenant, fx3.run.RunID, failed.Version, fixedInstant.Add(3*time.Hour))
		return err
	})
	if redriven.Attempt != 2 || redriven.State != jobs.RunDeclared {
		t.Fatalf("redriven = %+v, want DECLARED attempt 2", redriven)
	}
	if n := checkpointRows(t, ctx, fx3.conn, fx3.tenant, fx3.claimed.PartitionID); n != fx3.checkpts {
		t.Fatalf("checkpoints after redrive = %d, want %d", n, fx3.checkpts)
	}
	var report jobs.HousekeepingReport
	inTenantTx(t, fx3.conn, fx3.tenant, func(tx dbport.Tx) error {
		var err error
		report, err = gov.HousekeepingScan(ctx, tx, fx3.tenant, fx3.run.RunID)
		return err
	})
	if len(report.Holds) == 0 {
		t.Fatalf("holds after redrive = %v, want open partitions named as repair obligations", report.Holds)
	}
	if report.DeletedRows != 0 {
		t.Fatalf("redrive housekeeping deleted %d rows, want 0", report.DeletedRows)
	}
}

// TestTodo_JOB_004_Security proves tenant isolation: no governed
// operation reaches across tenants, pause state never leaks, and a
// housekeeping report names only its own tenant's lineage.
func TestTodo_JOB_004_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newJob004Fixture(t, "job004-sec-a")
	other := insertTenant(t, fx.db, "job004-sec-b")
	otherConn := appConn(t, fx.db)
	gov := jobs.NewGovernor()

	try := func(name string, fn func(tx dbport.Tx) error) error {
		t.Helper()
		return inTenantTxErr(otherConn, other, fn)
	}
	if err := try("pause", func(tx dbport.Tx) error {
		return gov.PauseRun(ctx, tx, other, fx.run.RunID)
	}); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("cross-tenant pause err = %v, want ErrNotFound", err)
	}
	if err := try("cancel", func(tx dbport.Tx) error {
		_, _, err := gov.CancelRun(ctx, tx, other, fx.run.RunID, fx.run.Version, fixedInstant.Add(time.Hour))
		return err
	}); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("cross-tenant cancel err = %v, want ErrNotFound", err)
	}
	if err := try("redrive", func(tx dbport.Tx) error {
		_, err := gov.RedriveRun(ctx, tx, other, fx.run.RunID, fx.run.Version, fixedInstant.Add(time.Hour))
		return err
	}); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("cross-tenant redrive err = %v, want ErrNotFound", err)
	}
	if err := try("reap", func(tx dbport.Tx) error {
		_, err := gov.HousekeepingScan(ctx, tx, other, fx.run.RunID)
		return err
	}); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("cross-tenant reap err = %v, want ErrNotFound", err)
	}
	if gov.IsPaused(other, fx.run.RunID) {
		t.Fatal("cross-tenant pause attempt left pause state behind")
	}

	// The victim run is untouched and still dispatchable in its tenant.
	var victim jobs.JobRun
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		victim, err = (jobs.RunStore{}).Load(ctx, tx, fx.tenant, fx.run.RunID)
		return err
	})
	if victim.State != jobs.RunRunning || victim.Version != fx.run.Version {
		t.Fatalf("victim run = %+v, want untouched RUNNING version %d", victim, fx.run.Version)
	}
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		if err := gov.CheckDispatchable(ctx, tx, fx.tenant, fx.run.RunID); err != nil {
			t.Errorf("victim dispatch err = %v, want nil", err)
		}
		return nil
	})

	// Pausing in the owning tenant does not gate the same run id seen
	// from the other tenant: it stays not-found there, never paused.
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		return gov.PauseRun(ctx, tx, fx.tenant, fx.run.RunID)
	})
	var gated error
	_ = inTenantTxErr(otherConn, other, func(tx dbport.Tx) error {
		gated = gov.CheckDispatchable(ctx, tx, other, fx.run.RunID)
		return nil
	})
	if !errors.Is(gated, jobs.ErrNotFound) {
		t.Fatalf("cross-tenant dispatch err = %v, want ErrNotFound, never ErrPaused", gated)
	}

	// Each tenant's report names only its own lineage.
	otherRun := uuid.New()
	otherDef := publish(t, ctx, otherConn, other, newDefinition(other, "job.gov.job004-sec-b", 1))
	inTenantTx(t, otherConn, other, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID: other, RunID: otherRun, JobID: otherDef.JobID, JobVersion: otherDef.Version,
			DeclaredBy: "workload:jobs-lane-test", DeclaredAt: fixedInstant,
		})
		return err
	})
	inTenantTx(t, otherConn, other, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: other, PartitionID: uuid.New(), RunID: otherRun,
			PartitionKey: "part-other", CreatedAt: fixedInstant,
		})
		return err
	})
	var reportA, reportB jobs.HousekeepingReport
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		reportA, err = gov.HousekeepingScan(ctx, tx, fx.tenant, fx.run.RunID)
		return err
	})
	inTenantTx(t, otherConn, other, func(tx dbport.Tx) error {
		var err error
		reportB, err = gov.HousekeepingScan(ctx, tx, other, otherRun)
		return err
	})
	for _, hold := range reportA.Holds {
		if hold == "part-other" {
			t.Fatalf("tenant A report leaks tenant B hold: %+v", reportA)
		}
	}
	for _, hold := range reportB.Holds {
		if hold == "part-gov-a" || hold == "part-gov-b" {
			t.Fatalf("tenant B report leaks tenant A hold: %+v", reportB)
		}
	}
	if reportA.TenantID != fx.tenant || reportB.TenantID != other {
		t.Fatalf("report tenants = %s/%s, want scoped owners", reportA.TenantID, reportB.TenantID)
	}
}
