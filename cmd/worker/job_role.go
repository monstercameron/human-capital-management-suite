// BatchJobRunner composes the batch job substrate into the worker role: it
// admits a job definition through the durable admission scheduler,
// publishes it, and drives one run to completion (start, partition claim,
// checkpoint, partition completion, run completion) against the live
// database. Every state transition threads the compare-and-swap version
// the stores return, so a concurrent worker can never silently overwrite
// this runner's claim.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// batchPool is the narrow database seam the runner needs: transactions only.
// Both *pgxadapter.Pool and *pgxadapter.Conn satisfy it.
type batchPool interface {
	Begin(ctx context.Context) (dbport.Tx, error)
}

// BatchJobRequest is one composed batch execution: the definition to admit
// and the run shape to drive. Partitions counts the deterministic
// partitions (p-0..p-N) the run creates and completes.
type BatchJobRequest struct {
	TenantID   uuid.UUID
	JobID      string
	Version    uint64
	Partitions int
	Holder     string
	DeclaredBy string
}

// BatchJobReport is one completed batch execution accounted: the admitted
// definition, the admission receipt, the completed run, and every completed
// partition with its durable checkpoint.
type BatchJobReport struct {
	Definition  jobs.JobDefinition
	Receipt     jobs.AdmissionReceipt
	Run         jobs.JobRun
	Partitions  []jobs.JobPartition
	Checkpoints []jobs.JobCheckpoint
}

// batchJobRunner is the worker's composed batch substrate client. The lease
// manager is executor-local by design (see jobs.LeaseManager): the durable
// resume watermark lives in job_checkpoint, which this runner writes on
// every partition, so a replacement holder resumes from the stored
// watermark rather than from this process's memory.
type batchJobRunner struct {
	pool      batchPool
	holder    string
	clock     func() time.Time
	admission *jobs.Scheduler
	leases    *jobs.LeaseManager
}

// batchJobRoleFor composes the batch runner the worker role serves.
func batchJobRoleFor(pool batchPool, holder string, clock func() time.Time) (*batchJobRunner, error) {
	if pool == nil {
		return nil, errors.New("worker: batch role needs a database pool")
	}
	if holder == "" {
		return nil, errors.New("worker: batch role needs a holder identity")
	}
	if clock == nil {
		return nil, errors.New("worker: batch role needs a clock")
	}
	admissionScheduler, err := jobs.NewScheduler(jobs.SchedulerPolicy{
		CellID: "cell-worker", CellCapacity: 16, ReservedP0: 2,
		TenantLimit: 4, RetryAllowance: 3, QuotaVersion: "v1",
	})
	if err != nil {
		return nil, fmt.Errorf("worker: batch admission scheduler: %w", err)
	}
	leases, err := jobs.NewLeaseManager(time.Hour, clock)
	if err != nil {
		return nil, fmt.Errorf("worker: batch lease manager: %w", err)
	}
	return &batchJobRunner{pool: pool, holder: holder, clock: clock, admission: admissionScheduler, leases: leases}, nil
}

func batchDigest(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// inTenantTx runs fn inside one tenant-scoped transaction.
func (r *batchJobRunner) inTenantTx(ctx context.Context, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("worker: batch begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("worker: batch tenant scope: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// RunOnce admits the requested definition and drives one run with exactly
// req.Partitions partitions to completion, checkpointing every partition.
func (r *batchJobRunner) RunOnce(ctx context.Context, req BatchJobRequest) (BatchJobReport, error) {
	var report BatchJobReport
	if req.TenantID == uuid.Nil || req.JobID == "" || req.Partitions < 1 {
		return report, errors.New("worker: batch request needs a tenant, a job id and at least one partition")
	}
	holder := req.Holder
	if holder == "" {
		holder = r.holder
	}
	now := r.clock()
	definition := jobs.JobDefinition{
		TenantID:                req.TenantID,
		JobID:                   req.JobID,
		Version:                 req.Version,
		DefinitionDigest:        batchDigest(req.JobID + "/definition"),
		TriggerDigest:           batchDigest(req.JobID + "/trigger"),
		TargetDefinitionRef:     "hcmnext.jobs.worker_batch",
		TargetDefinitionVersion: 1,
		Body:                    []byte(`{"partitions":1}`),
		PublishedBy:             req.DeclaredBy,
		PublishedAt:             now,
	}
	if err := r.admission.Register(req.TenantID, req.JobID, definition.DefinitionDigest); err != nil {
		return report, fmt.Errorf("worker: batch register %s: %w", req.JobID, err)
	}
	receipt, err := r.admission.Admit(jobs.AdmissionRequest{
		TenantID:         req.TenantID,
		JobID:            req.JobID,
		DefinitionDigest: definition.DefinitionDigest,
		Priority:         jobs.PriorityP2,
		EstimatedCost:    1,
		OperationID:      "batch-" + uuid.NewString(),
	})
	if err != nil {
		return report, fmt.Errorf("worker: batch admit %s: %w", req.JobID, err)
	}
	if receipt.Outcome != admission.OutcomeAdmit {
		return report, fmt.Errorf("worker: batch %s not admitted: %s (%s)", req.JobID, receipt.Outcome, receipt.Reason)
	}
	report.Receipt = receipt
	if err := r.inTenantTx(ctx, req.TenantID, func(tx dbport.Tx) error {
		published, err := (jobs.DefinitionStore{}).Publish(ctx, tx, definition)
		if err != nil {
			return fmt.Errorf("worker: batch publish %s: %w", req.JobID, err)
		}
		report.Definition = published
		return nil
	}); err != nil {
		return report, err
	}
	runID := uuid.New()
	var run jobs.JobRun
	if err := r.inTenantTx(ctx, req.TenantID, func(tx dbport.Tx) error {
		started, err := (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID:   req.TenantID,
			RunID:      runID,
			JobID:      req.JobID,
			JobVersion: req.Version,
			DeclaredBy: req.DeclaredBy,
			DeclaredAt: now,
		})
		if err != nil {
			return fmt.Errorf("worker: batch start run: %w", err)
		}
		begun, err := (jobs.RunStore{}).Begin(ctx, tx, req.TenantID, runID, started.Version, now)
		if err != nil {
			return fmt.Errorf("worker: batch begin run: %w", err)
		}
		run = begun
		return nil
	}); err != nil {
		return report, err
	}
	for i := 0; i < req.Partitions; i++ {
		partition, checkpoint, err := r.runPartition(ctx, req.TenantID, runID, fmt.Sprintf("p-%d", i), holder, now)
		if err != nil {
			return report, err
		}
		report.Partitions = append(report.Partitions, partition)
		report.Checkpoints = append(report.Checkpoints, checkpoint)
	}
	if err := r.inTenantTx(ctx, req.TenantID, func(tx dbport.Tx) error {
		completed, err := (jobs.RunStore{}).Complete(ctx, tx, req.TenantID, runID, run.Version, now)
		if err != nil {
			return fmt.Errorf("worker: batch complete run: %w", err)
		}
		loaded, err := (jobs.RunStore{}).Load(ctx, tx, req.TenantID, runID)
		if err != nil {
			return fmt.Errorf("worker: batch load run: %w", err)
		}
		completed = loaded
		report.Run = completed
		return nil
	}); err != nil {
		return report, err
	}
	return report, nil
}

// runPartition creates, claims, checkpoints and completes one partition,
// returning the completed partition and its durable checkpoint.
func (r *batchJobRunner) runPartition(ctx context.Context, tenant, runID uuid.UUID, key, holder string, now time.Time) (jobs.JobPartition, jobs.JobCheckpoint, error) {
	var partition jobs.JobPartition
	var checkpoint jobs.JobCheckpoint
	if err := r.inTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		created, err := (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID:     tenant,
			PartitionID:  uuid.New(),
			RunID:        runID,
			PartitionKey: key,
			CreatedAt:    now,
		})
		if err != nil {
			return fmt.Errorf("worker: batch create partition %s: %w", key, err)
		}
		claimed, err := (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, created.PartitionID, created.Version, holder, now)
		if err != nil {
			return fmt.Errorf("worker: batch claim partition %s: %w", key, err)
		}
		lease, err := r.leases.Acquire(tenant, claimed.PartitionID, holder)
		if err != nil {
			return fmt.Errorf("worker: batch acquire partition lease %s: %w", key, err)
		}
		written, err := r.leases.Checkpoint(ctx, tx, lease, jobs.JobCheckpoint{
			TenantID:         tenant,
			PartitionID:      claimed.PartitionID,
			Sequence:         1,
			StateDigest:      batchDigest("state/" + key),
			PartitionVersion: claimed.Version,
			TakenAt:          now,
		})
		if err != nil {
			return fmt.Errorf("worker: batch checkpoint partition %s: %w", key, err)
		}
		done, err := (jobs.PartitionStore{}).Complete(ctx, tx, tenant, claimed.PartitionID, claimed.Version, now)
		if err != nil {
			return fmt.Errorf("worker: batch complete partition %s: %w", key, err)
		}
		partition = done
		checkpoint = written
		return nil
	}); err != nil {
		return partition, checkpoint, err
	}
	return partition, checkpoint, nil
}
