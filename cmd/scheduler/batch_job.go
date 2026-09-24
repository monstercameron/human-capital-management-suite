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
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const (
	ledgerJobID      = "hcmnext.ledger.invariant.scan"
	ledgerJobTarget  = "hcmnext.jobs.ledger_invariant_scan"
	ledgerJobLease   = 5 * time.Minute
	ledgerJobVersion = uint64(1)
)

const ledgerJobBody = `{"operation":"ledger.invariant.scan","version":1}`

type batchJobDB interface{ dbport.Beginner }

type batchJobProcessor func(context.Context, jobs.Executor, uuid.UUID, jobs.WorkItem) (string, error)

func runBatchJobWorkload(ctx context.Context, db batchJobDB, tenant uuid.UUID, identity string, interval time.Duration, clock func() time.Time, logger bootstrap.Logger) error {
	return runBatchJobWorkloadWithProcessor(ctx, db, tenant, identity, interval, clock, logger, processLedgerInvariantItem)
}

func runBatchJobWorkloadWithProcessor(ctx context.Context, db batchJobDB, tenant uuid.UUID, identity string, interval time.Duration, clock func() time.Time, logger bootstrap.Logger, process batchJobProcessor) error {
	if db == nil || tenant == uuid.Nil || identity == "" || interval <= 0 || clock == nil || logger == nil {
		return fmt.Errorf("batch job worker requires database, tenant, identity, positive interval, clock and logger")
	}
	if process == nil {
		return fmt.Errorf("batch job worker requires a processor")
	}
	if err := runLedgerInvariantJob(ctx, db, tenant, identity, interval, clock, process); err != nil {
		logger.Error("jobs.ledger_invariant.failed", "tenant_id", tenant.String(), "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := runLedgerInvariantJob(ctx, db, tenant, identity, interval, clock, process); err != nil {
				logger.Error("jobs.ledger_invariant.failed", "tenant_id", tenant.String(), "error", err)
			}
		}
	}
}

// runLedgerInvariantJob admits and executes the scheduler's concrete ledger
// invariant job. Its run and partition identities are stable within one
// cadence window, which lets another process reopen the same run after a
// crash. Each stream scan result is committed with its checkpoint.
func runLedgerInvariantJob(ctx context.Context, db batchJobDB, tenant uuid.UUID, identity string, interval time.Duration, clock func() time.Time, process batchJobProcessor) error {
	now := clock().UTC()
	if now.IsZero() {
		return fmt.Errorf("scheduler job clock returned an unset time")
	}
	definition := jobs.JobDefinition{
		TenantID: tenant, JobID: ledgerJobID, Version: ledgerJobVersion,
		DefinitionDigest: digestHex([]byte(ledgerJobBody)), TriggerDigest: digestHex([]byte("hcmnext.schedule.ledger-invariant.v1")),
		TargetDefinitionRef: ledgerJobTarget, TargetDefinitionVersion: 1,
		Body: []byte(ledgerJobBody), PublishedBy: "role:scheduler", PublishedAt: now,
	}
	if err := ensureLedgerJobDefinition(ctx, db, definition); err != nil {
		return err
	}
	streams, err := loadLedgerStreams(ctx, db, tenant)
	if err != nil {
		return err
	}
	workKeys := streams
	if len(workKeys) == 0 {
		workKeys = []string{"__empty__"}
	}
	admitter, err := jobs.NewScheduler(jobs.SchedulerPolicy{
		CellID: "cell-local", CellCapacity: 10000, ReservedP0: 1000,
		TenantLimit: 10000, RetryAllowance: 3, QuotaVersion: "scheduler-batch-v1",
	})
	if err != nil {
		return err
	}
	if err := admitter.Register(tenant, definition.JobID, definition.DefinitionDigest); err != nil {
		return err
	}
	bucket := now.UnixNano() / interval.Nanoseconds()
	runID := uuid.NewSHA1(tenant, []byte(fmt.Sprintf("%s/%d", ledgerJobID, bucket)))
	receipt, err := admitter.Admit(jobs.AdmissionRequest{
		TenantID: tenant, JobID: definition.JobID, DefinitionDigest: definition.DefinitionDigest,
		Priority: jobs.PriorityP1, EstimatedCost: max(1, len(workKeys)), OperationID: runID.String(),
	})
	if err != nil {
		return err
	}
	if receipt.Outcome != admission.Admit {
		return fmt.Errorf("batch job admission outcome %s: %s", receipt.Outcome, receipt.Reason)
	}
	run, err := startOrLoadLedgerRun(ctx, db, tenant, runID, now)
	if err != nil {
		return err
	}
	runID = run.RunID
	if run.State == jobs.RunCompleted {
		return nil
	}
	if run.State == jobs.RunDeclared {
		run, err = beginLedgerRun(ctx, db, tenant, runID, run.Version, now)
		if err != nil {
			return err
		}
	} else if run.State != jobs.RunRunning {
		return fmt.Errorf("ledger job run %s is %s and cannot be resumed", runID, run.State)
	}
	leases, err := jobs.NewLeaseManager(ledgerJobLease, clock)
	if err != nil {
		return err
	}
	for _, streamKey := range workKeys {
		partitionID := uuid.NewSHA1(runID, []byte("partition/"+streamKey))
		partition, err := createOrLoadLedgerPartition(ctx, db, tenant, runID, partitionID, streamKey, now)
		if err != nil {
			return err
		}
		if partition.State == jobs.PartitionCompleted {
			continue
		}
		items := []jobs.WorkItem{{Index: 0, Key: streamKey}}
		if streamKey == "__empty__" {
			items[0].Key = "no-ledger-streams"
		}
		_, err = jobs.ExecutePartition(ctx, db, jobs.PartitionExecution{
			TenantID: tenant, PartitionID: partitionID, Holder: identity,
			LeaseTTL: ledgerJobLease, Now: clock, Items: items, Leases: leases,
			Process: func(ctx context.Context, ex jobs.Executor, item jobs.WorkItem) (string, error) {
				return process(ctx, ex, tenant, item)
			},
		})
		if err != nil {
			return err
		}
	}
	return completeLedgerRun(ctx, db, tenant, runID, now)
}

func processLedgerInvariantItem(ctx context.Context, ex jobs.Executor, tenant uuid.UUID, item jobs.WorkItem) (string, error) {
	if item.Key == "no-ledger-streams" {
		return digestHex([]byte(item.Key)), nil
	}
	report, err := scanLedgerKeys(ctx, ex, tenant, []string{item.Key})
	if err != nil {
		return "", err
	}
	if report.Quality != "HEALTHY" {
		return "", fmt.Errorf("ledger stream %s quality is %s", item.Key, report.Quality)
	}
	return digestHex([]byte(report.Digest)), nil
}

func ensureLedgerJobDefinition(ctx context.Context, db batchJobDB, definition jobs.JobDefinition) error {
	return withBatchTenantTx(ctx, db, definition.TenantID, func(tx dbport.Tx) error {
		_, err := (jobs.DefinitionStore{}).Publish(ctx, tx, definition)
		if err == nil {
			return nil
		}
		if !errors.Is(err, jobs.ErrDuplicate) {
			return err
		}
		stored, err := (jobs.DefinitionStore{}).Load(ctx, tx, definition.TenantID, definition.JobID, definition.Version)
		if err != nil {
			return err
		}
		if stored.DefinitionDigest != definition.DefinitionDigest || stored.TriggerDigest != definition.TriggerDigest || stored.TargetDefinitionRef != definition.TargetDefinitionRef || string(stored.Body) != ledgerJobBody {
			return fmt.Errorf("published ledger job definition does not match this worker")
		}
		return nil
	})
}

func loadLedgerStreams(ctx context.Context, db batchJobDB, tenant uuid.UUID) ([]string, error) {
	var keys []string
	err := withBatchTenantTx(ctx, db, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT stream_key FROM ledger_stream ORDER BY stream_key`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return err
			}
			keys = append(keys, key)
		}
		return rows.Err()
	})
	return keys, err
}

func startOrLoadLedgerRun(ctx context.Context, db batchJobDB, tenant, runID uuid.UUID, now time.Time) (jobs.JobRun, error) {
	var run jobs.JobRun
	err := withBatchTenantTx(ctx, db, tenant, func(tx dbport.Tx) error {
		open, err := (jobs.RunStore{}).LoadOpenByJob(ctx, tx, tenant, ledgerJobID)
		if err == nil {
			run = open
			return nil
		}
		if !errors.Is(err, jobs.ErrNotFound) {
			return err
		}
		run, err = (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{TenantID: tenant, RunID: runID, JobID: ledgerJobID, JobVersion: ledgerJobVersion, DeclaredBy: "role:scheduler", DeclaredAt: now})
		if errors.Is(err, jobs.ErrDuplicate) {
			run, err = (jobs.RunStore{}).Load(ctx, tx, tenant, runID)
		}
		return err
	})
	return run, err
}

func beginLedgerRun(ctx context.Context, db batchJobDB, tenant, runID uuid.UUID, version uint64, now time.Time) (jobs.JobRun, error) {
	var run jobs.JobRun
	err := withBatchTenantTx(ctx, db, tenant, func(tx dbport.Tx) error {
		var err error
		run, err = (jobs.RunStore{}).Begin(ctx, tx, tenant, runID, version, now)
		return err
	})
	return run, err
}

func createOrLoadLedgerPartition(ctx context.Context, db batchJobDB, tenant, runID, partitionID uuid.UUID, streamKey string, now time.Time) (jobs.JobPartition, error) {
	partitionKey := "stream-" + uuid.NewSHA1(runID, []byte(streamKey)).String()
	if streamKey == "__empty__" {
		partitionKey = "tenant-empty"
	}
	var partition jobs.JobPartition
	err := withBatchTenantTx(ctx, db, tenant, func(tx dbport.Tx) error {
		var err error
		partition, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{TenantID: tenant, PartitionID: partitionID, RunID: runID, PartitionKey: partitionKey, CreatedAt: now})
		if errors.Is(err, jobs.ErrDuplicate) {
			partition, err = (jobs.PartitionStore{}).Load(ctx, tx, tenant, partitionID)
		}
		return err
	})
	return partition, err
}

func completeLedgerRun(ctx context.Context, db batchJobDB, tenant, runID uuid.UUID, now time.Time) error {
	return withBatchTenantTx(ctx, db, tenant, func(tx dbport.Tx) error {
		run, err := (jobs.RunStore{}).Load(ctx, tx, tenant, runID)
		if err != nil {
			return err
		}
		if run.State == jobs.RunCompleted {
			return nil
		}
		_, err = (jobs.RunStore{}).Complete(ctx, tx, tenant, runID, run.Version, now)
		return err
	})
}

func withBatchTenantTx(ctx context.Context, db batchJobDB, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
