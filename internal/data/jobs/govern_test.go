package jobs_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// job004Fixture is one RUNNING run with a CLAIMED partition carrying two
// checkpoints and one PENDING partition. It reuses the JOB-001 helpers in
// jobs_test.go.
type job004Fixture struct {
	db       *pgtest.DB
	conn     *pgxadapter.Conn
	tenant   uuid.UUID
	run      jobs.JobRun
	claimed  jobs.JobPartition
	pending  jobs.JobPartition
	checkpts int
}

func newJob004Fixture(t *testing.T, key string) job004Fixture {
	t.Helper()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, key)
	conn := appConn(t, db)
	def := publish(t, ctx, conn, tenant, newDefinition(tenant, "job.gov."+key, 1))
	runID := uuid.New()
	declareRun(t, ctx, conn, tenant, def, runID)
	var begun jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		begun, err = (jobs.RunStore{}).Begin(ctx, tx, tenant, runID, 1, fixedInstant.Add(time.Minute))
		return err
	})
	claimedID := uuid.New()
	var created jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID:     tenant,
			PartitionID:  claimedID,
			RunID:        runID,
			PartitionKey: "part-gov-a",
			CreatedAt:    fixedInstant,
		})
		return err
	})
	var claimed jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		claimed, err = (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, claimedID, created.Version, "worker-a", fixedInstant.Add(2*time.Minute))
		return err
	})
	for _, seq := range []uint64{1, 2} {
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, jobs.JobCheckpoint{
				TenantID:         tenant,
				PartitionID:      claimedID,
				Sequence:         seq,
				StateDigest:      digestOf(key + "/gov-state"),
				PartitionVersion: claimed.Version,
				TakenAt:          fixedInstant.Add(time.Duration(seq+10) * time.Minute),
			})
			return err
		})
	}
	pendingID := uuid.New()
	var pending jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		pending, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID:     tenant,
			PartitionID:  pendingID,
			RunID:        runID,
			PartitionKey: "part-gov-b",
			CreatedAt:    fixedInstant,
		})
		return err
	})
	return job004Fixture{db: db, conn: conn, tenant: tenant, run: begun, claimed: claimed, pending: pending, checkpts: 2}
}

// checkpointRows counts a partition's checkpoints inside the tenant scope.
func checkpointRows(t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenant, partition uuid.UUID) int {
	t.Helper()
	var list []jobs.JobCheckpoint
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		list, err = (jobs.CheckpointStore{}).List(ctx, tx, tenant, partition)
		return err
	})
	return len(list)
}

func partitionState(t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenant, partition uuid.UUID) string {
	t.Helper()
	var loaded jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		loaded, err = (jobs.PartitionStore{}).Load(ctx, tx, tenant, partition)
		return err
	})
	return loaded.State
}

// TestTodo_JOB_004 proves governed safe-point operations: pause gates
// dispatch without touching rows, cancel settles the run plus every open
// partition through the CAS stores, redrive opens a new attempt on the
// same run identity, and housekeeping reports lineage and holds while
// deleting nothing.
func TestTodo_JOB_004(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newJob004Fixture(t, "job004-primary")
	gov := jobs.NewGovernor()

	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		return gov.PauseRun(ctx, tx, fx.tenant, fx.run.RunID)
	})
	var gated error
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		gated = gov.CheckDispatchable(ctx, tx, fx.tenant, fx.run.RunID)
		return nil
	})
	if !errors.Is(gated, jobs.ErrPaused) {
		t.Fatalf("dispatch while paused err = %v, want ErrPaused", gated)
	}
	still := partitionState(t, ctx, fx.conn, fx.tenant, fx.claimed.PartitionID)
	if still != jobs.PartitionClaimed {
		t.Fatalf("pause moved partition to %s, want no row change", still)
	}
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		return gov.ResumeRun(ctx, tx, fx.tenant, fx.run.RunID)
	})
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		gated = gov.CheckDispatchable(ctx, tx, fx.tenant, fx.run.RunID)
		return nil
	})
	if gated != nil {
		t.Fatalf("dispatch after resume err = %v, want nil", gated)
	}

	var cancelled jobs.JobRun
	var settled int
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		cancelled, settled, err = gov.CancelRun(ctx, tx, fx.tenant, fx.run.RunID, fx.run.Version, fixedInstant.Add(time.Hour))
		return err
	})
	if cancelled.State != jobs.RunCancelled {
		t.Fatalf("cancelled run state = %s, want CANCELLED", cancelled.State)
	}
	if settled != 2 {
		t.Fatalf("cancelled partitions = %d, want 2", settled)
	}
	if got := partitionState(t, ctx, fx.conn, fx.tenant, fx.claimed.PartitionID); got != jobs.PartitionCancelled {
		t.Fatalf("claimed partition = %s, want CANCELLED", got)
	}
	if got := partitionState(t, ctx, fx.conn, fx.tenant, fx.pending.PartitionID); got != jobs.PartitionCancelled {
		t.Fatalf("pending partition = %s, want CANCELLED", got)
	}
	if n := checkpointRows(t, ctx, fx.conn, fx.tenant, fx.claimed.PartitionID); n != fx.checkpts {
		t.Fatalf("checkpoints after cancel = %d, want lineage preserved at %d", n, fx.checkpts)
	}
	var reloaded jobs.JobRun
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		reloaded, err = (jobs.RunStore{}).Load(ctx, tx, fx.tenant, fx.run.RunID)
		return err
	})
	if reloaded.Attempt != 1 {
		t.Fatalf("cancelled run attempt = %d, want lineage kept at 1", reloaded.Attempt)
	}

	// A failed run redrives onto a new attempt of the same identity with
	// its evidence intact.
	fx2 := newJob004Fixture(t, "job004-redrive")
	var failed jobs.JobRun
	inTenantTx(t, fx2.conn, fx2.tenant, func(tx dbport.Tx) error {
		var err error
		failed, err = (jobs.RunStore{}).Fail(ctx, tx, fx2.tenant, fx2.run.RunID, fx2.run.Version, fixedInstant.Add(2*time.Hour), "worker lost")
		return err
	})
	var redriven jobs.JobRun
	inTenantTx(t, fx2.conn, fx2.tenant, func(tx dbport.Tx) error {
		var err error
		redriven, err = gov.RedriveRun(ctx, tx, fx2.tenant, fx2.run.RunID, failed.Version, fixedInstant.Add(3*time.Hour))
		return err
	})
	if redriven.State != jobs.RunDeclared || redriven.Attempt != failed.Attempt+1 {
		t.Fatalf("redriven run = %+v, want DECLARED attempt %d", redriven, failed.Attempt+1)
	}
	if redriven.RunID != fx2.run.RunID {
		t.Fatalf("redrive forked identity %s, want same run %s", redriven.RunID, fx2.run.RunID)
	}
	if n := checkpointRows(t, ctx, fx2.conn, fx2.tenant, fx2.claimed.PartitionID); n != fx2.checkpts {
		t.Fatalf("checkpoints after redrive = %d, want %d preserved", n, fx2.checkpts)
	}

	// Housekeeping scans lineage and holds and deletes nothing.
	var before, after jobs.HousekeepingReport
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		before, err = gov.HousekeepingScan(ctx, tx, fx.tenant, fx.run.RunID)
		return err
	})
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		after, err = gov.HousekeepingScan(ctx, tx, fx.tenant, fx.run.RunID)
		return err
	})
	if before.RunState != jobs.RunCancelled || before.PartitionsTotal != 2 || before.CheckpointsTotal != fx.checkpts {
		t.Fatalf("housekeeping report = %+v, want cancelled run with 2 partitions and %d checkpoints", before, fx.checkpts)
	}
	if before.PartitionsOpen != 0 {
		t.Fatalf("open partitions after cancel = %d, want 0", before.PartitionsOpen)
	}
	if before.DeletedRows != 0 || after.DeletedRows != 0 {
		t.Fatalf("housekeeping deleted rows %+v/%+v, want zero destructive effect", before, after)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("scan is not read-only: %+v vs %+v", before, after)
	}
	if n := checkpointRows(t, ctx, fx.conn, fx.tenant, fx.claimed.PartitionID); n != fx.checkpts {
		t.Fatalf("checkpoints after scan = %d, want %d", n, fx.checkpts)
	}
}
