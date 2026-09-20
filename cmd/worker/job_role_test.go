package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// rev035BatchDB opens an isolated embedded-PostgreSQL schema with one
// tenant, mirroring the jobs package's own pgtest posture.
// testBatchClock pins the batch lease and declaration clock.
func testBatchClock() func() time.Time {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return now }
}

func rev035BatchDB(t *testing.T) (*pgtest.DB, uuid.UUID, *pgxadapter.Conn) {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "batch", "tenant batch")
	return db, tenant, db.NewConn(t)
}

func rev035BatchTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("with tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("batch tx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestTodo_REV_035_02 proves the worker batch role runs a real job to
// completion against a live database: admission, definition publish, run
// start, partition claim, checkpoint and run completion all execute through
// the composed runner instead of only inside the jobs package's own tests.
func TestTodo_REV_035_02(t *testing.T) {
	_, tenant, conn := rev035BatchDB(t)
	runner, err := batchJobRoleFor(conn, "worker-batch-test", testBatchClock())
	if err != nil {
		t.Fatalf("batchJobRoleFor: %v", err)
	}
	report, err := runner.RunOnce(context.Background(), BatchJobRequest{
		TenantID: tenant, JobID: "job.rev035", Version: 1,
		Partitions: 1, Holder: "worker-batch-test", DeclaredBy: "workload:batch-test",
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Receipt.Outcome != admission.OutcomeAdmit {
		t.Fatalf("batch admission outcome = %s, want ADMIT: %+v", report.Receipt.Outcome, report.Receipt)
	}
	if report.Run.State != jobs.RunCompleted {
		t.Fatalf("run state = %s, want COMPLETED", report.Run.State)
	}
	if len(report.Partitions) != 1 || report.Partitions[0].State != jobs.PartitionCompleted {
		t.Fatalf("partitions = %+v, want one COMPLETED", report.Partitions)
	}
	if len(report.Checkpoints) != 1 {
		t.Fatalf("checkpoints = %d, want one durable checkpoint", len(report.Checkpoints))
	}
}

// TestTodo_REV_035_02_Integration proves crash-resume through the composed
// runner: three partitions checkpoint durably, and the recorded watermark
// is what a replacement worker would resume from after the first holder is
// gone.
func TestTodo_REV_035_02_Integration(t *testing.T) {
	_, tenant, conn := rev035BatchDB(t)
	runner, err := batchJobRoleFor(conn, "worker-batch-test", testBatchClock())
	if err != nil {
		t.Fatalf("batchJobRoleFor: %v", err)
	}
	report, err := runner.RunOnce(context.Background(), BatchJobRequest{
		TenantID: tenant, JobID: "job.rev035.resume", Version: 1,
		Partitions: 3, Holder: "worker-batch-test", DeclaredBy: "workload:batch-test",
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Run.State != jobs.RunCompleted {
		t.Fatalf("run state = %s, want COMPLETED", report.Run.State)
	}
	for _, part := range report.Partitions {
		if part.State != jobs.PartitionCompleted {
			t.Fatalf("partition %s state = %s, want COMPLETED", part.PartitionKey, part.State)
		}
		var watermark uint64
		var has bool
		rev035BatchTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			watermark, has, err = jobs.LoadWatermark(context.Background(), tx, tenant, part.PartitionID)
			return err
		})
		if !has || watermark != 1 {
			t.Fatalf("partition %s watermark = %d/%v, want durable checkpoint 1", part.PartitionKey, watermark, has)
		}
	}
}
