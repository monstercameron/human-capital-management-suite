package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// job003Fixture publishes one definition, declares and begins one run, and
// creates plus claims one partition, returning the claimed partition. It
// reuses the JOB-001 helpers in jobs_test.go.
type job003Fixture struct {
	db        *pgtest.DB
	conn      *pgxadapter.Conn
	tenant    uuid.UUID
	run       jobs.JobRun
	partition jobs.JobPartition
}

func newJob003Fixture(t *testing.T, key string) job003Fixture {
	t.Helper()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, key)
	conn := appConn(t, db)
	def := publish(t, ctx, conn, tenant, newDefinition(tenant, "job.lease."+key, 1))
	runID := uuid.New()
	declareRun(t, ctx, conn, tenant, def, runID)
	var begun jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		begun, err = (jobs.RunStore{}).Begin(ctx, tx, tenant, runID, 1, fixedInstant.Add(time.Minute))
		return err
	})
	partitionID := uuid.New()
	var created jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID:     tenant,
			PartitionID:  partitionID,
			RunID:        runID,
			PartitionKey: "part-0001",
			CreatedAt:    fixedInstant,
		})
		return err
	})
	var claimed jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		claimed, err = (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, partitionID, created.Version, "worker-a", fixedInstant.Add(2*time.Minute))
		return err
	})
	return job003Fixture{db: db, conn: conn, tenant: tenant, run: begun, partition: claimed}
}

// fencedCheckpoint writes one checkpoint sequence through the lease fence.
func fencedCheckpoint(t *testing.T, ctx context.Context, fx job003Fixture, mgr *jobs.LeaseManager, lease jobs.Lease, seq uint64) jobs.JobCheckpoint {
	t.Helper()
	var out jobs.JobCheckpoint
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		out, err = mgr.Checkpoint(ctx, tx, lease, jobs.JobCheckpoint{
			TenantID:         fx.tenant,
			PartitionID:      fx.partition.PartitionID,
			Sequence:         seq,
			StateDigest:      digestOf(fmt.Sprintf("lease-state-%d", seq)),
			PartitionVersion: fx.partition.Version,
			TakenAt:          fixedInstant.Add(time.Duration(seq+10) * time.Minute),
		})
		return err
	})
	return out
}

func checkpointCount(t *testing.T, ctx context.Context, fx job003Fixture) int {
	t.Helper()
	var list []jobs.JobCheckpoint
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		list, err = (jobs.CheckpointStore{}).List(ctx, tx, fx.tenant, fx.partition.PartitionID)
		return err
	})
	return len(list)
}

// TestTodo_JOB_003 proves fenced leases and resumable partition execution.
// A live lease checkpoints; a superseded (stale) worker is fenced off before
// any write; after a crash the durable checkpoint watermark resumes with zero
// skipped or duplicated logical items and an explicit incomplete tail.
func TestTodo_JOB_003(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newJob003Fixture(t, "job003-primary")

	now := fixedInstant
	mgr, err := jobs.NewLeaseManager(5*time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLeaseManager: %v", err)
	}
	leaseA, err := mgr.Acquire(fx.tenant, fx.partition.PartitionID, "worker-a")
	if err != nil {
		t.Fatalf("Acquire worker-a: %v", err)
	}
	if leaseA.Token != 1 {
		t.Fatalf("first lease token = %d, want 1", leaseA.Token)
	}
	if _, err := mgr.Acquire(fx.tenant, fx.partition.PartitionID, "worker-b"); !errors.Is(err, jobs.ErrLeaseHeld) {
		t.Fatalf("contending acquire err = %v, want ErrLeaseHeld", err)
	}

	got := fencedCheckpoint(t, ctx, fx, mgr, leaseA, 1)
	if got.Sequence != 1 {
		t.Fatalf("fenced checkpoint sequence = %d, want 1", got.Sequence)
	}

	// The lease lapses and worker-b fences worker-a out.
	now = now.Add(6 * time.Minute)
	leaseB, err := mgr.Acquire(fx.tenant, fx.partition.PartitionID, "worker-b")
	if err != nil {
		t.Fatalf("Acquire worker-b after lapse: %v", err)
	}
	if leaseB.Token != leaseA.Token+1 {
		t.Fatalf("fencing token = %d, want %d", leaseB.Token, leaseA.Token+1)
	}
	before := checkpointCount(t, ctx, fx)
	err = inTenantTxErr(fx.conn, fx.tenant, func(tx dbport.Tx) error {
		_, err := mgr.Checkpoint(ctx, tx, leaseA, jobs.JobCheckpoint{
			TenantID:         fx.tenant,
			PartitionID:      fx.partition.PartitionID,
			Sequence:         2,
			StateDigest:      digestOf("stale-write"),
			PartitionVersion: fx.partition.Version,
			TakenAt:          now,
		})
		return err
	})
	if !errors.Is(err, jobs.ErrStaleFence) {
		t.Fatalf("stale checkpoint err = %v, want ErrStaleFence", err)
	}
	if after := checkpointCount(t, ctx, fx); after != before {
		t.Fatalf("stale write persisted: checkpoints %d -> %d, want no write", before, after)
	}

	fencedCheckpoint(t, ctx, fx, mgr, leaseB, 2)
	fencedCheckpoint(t, ctx, fx, mgr, leaseB, 3)

	var watermark uint64
	var ok bool
	inTenantTx(t, fx.conn, fx.tenant, func(tx dbport.Tx) error {
		var err error
		watermark, ok, err = jobs.LoadWatermark(ctx, tx, fx.tenant, fx.partition.PartitionID)
		return err
	})
	if !ok || watermark != 3 {
		t.Fatalf("LoadWatermark = (%d, %v), want (3, true)", watermark, ok)
	}

	var items []jobs.WorkItem
	for i := uint64(0); i < 8; i++ {
		items = append(items, jobs.WorkItem{Index: i, Key: fmt.Sprintf("item-%d", i)})
	}
	plan, err := jobs.PlanResume(fx.tenant, fx.partition.PartitionID, items, watermark, ok)
	if err != nil {
		t.Fatalf("PlanResume: %v", err)
	}
	if len(plan.Pending) != 5 || plan.Pending[0].Index != 3 {
		t.Fatalf("resume pending starts at %+v, want 5 items from index 3", plan.Pending)
	}
	var applied []uint64
	res, err := jobs.ExecuteFromWatermark(plan, func(it jobs.WorkItem) error {
		applied = append(applied, it.Index)
		return nil
	})
	if err != nil {
		t.Fatalf("ExecuteFromWatermark: %v", err)
	}
	if len(res.Incomplete) != 0 {
		t.Fatalf("incomplete = %+v, want empty on success", res.Incomplete)
	}
	seen := map[uint64]int{}
	for _, idx := range applied {
		if idx < 3 {
			t.Fatalf("re-applied durable item %d: duplicate effect", idx)
		}
		seen[idx]++
	}
	if len(applied) != 5 {
		t.Fatalf("applied %d items %v, want exactly [3 4 5 6 7]", len(applied), applied)
	}
	for idx, n := range seen {
		if n != 1 {
			t.Fatalf("item %d applied %d times, want exactly once", idx, n)
		}
	}

	// A mid-partition failure names its explicit incomplete tail.
	plan2, err := jobs.PlanResume(fx.tenant, fx.partition.PartitionID, items, watermark, ok)
	if err != nil {
		t.Fatalf("PlanResume retry: %v", err)
	}
	res2, err := jobs.ExecuteFromWatermark(plan2, func(it jobs.WorkItem) error {
		if it.Index == 5 {
			return errors.New("effect failed at item-5")
		}
		return nil
	})
	if !errors.Is(err, jobs.ErrItemFailed) {
		t.Fatalf("failed execution err = %v, want ErrItemFailed", err)
	}
	if len(res2.Incomplete) != 3 || res2.Incomplete[0].Key != "item-5" || res2.Incomplete[2].Key != "item-7" {
		t.Fatalf("incomplete tail = %+v, want [item-5 item-6 item-7]", res2.Incomplete)
	}
	if res2.Watermark != 5 {
		t.Fatalf("failure watermark = %d, want 5", res2.Watermark)
	}
}
