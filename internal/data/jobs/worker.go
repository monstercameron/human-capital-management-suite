package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// ItemProcessor applies one work item and returns the digest of the durable
// result represented by its checkpoint. The effect and checkpoint commit in
// the same tenant transaction, so a process crash cannot commit one without
// the other.
type ItemProcessor func(context.Context, Executor, WorkItem) (string, error)

// PartitionExecution describes one resumable partition pass.
type PartitionExecution struct {
	TenantID    uuid.UUID
	PartitionID uuid.UUID
	Holder      string
	LeaseTTL    time.Duration
	Now         func() time.Time
	Items       []WorkItem
	Process     ItemProcessor
	Leases      *LeaseManager
}

// ExecutePartition claims or recovers one partition, resumes after its last
// durable checkpoint, and completes it. A prior claim is recoverable only
// after LeaseTTL has elapsed. Recovery advances partition_version, which
// rejects writes from the abandoned generation. Each processor effect and
// checkpoint share one transaction.
func ExecutePartition(ctx context.Context, db dbport.Beginner, in PartitionExecution) (ExecutionResult, error) {
	if db == nil || in.TenantID == uuid.Nil || in.PartitionID == uuid.Nil || !validIdentifier(in.Holder) || in.LeaseTTL <= 0 || in.Process == nil || in.Leases == nil {
		return ExecutionResult{}, invalid("partition_execution", "database, tenant, partition, holder, positive lease, processor and fence are required")
	}
	now := in.Now
	if now == nil {
		now = time.Now
	}
	instant := now().UTC()
	if instant.IsZero() {
		return ExecutionResult{}, invalid("now", "timestamp is unset")
	}
	partition, err := loadAndClaim(ctx, db, in, instant)
	if err != nil {
		return ExecutionResult{}, err
	}
	if partition.State == PartitionCompleted {
		return ExecutionResult{}, nil
	}
	lease, err := in.Leases.Acquire(in.TenantID, in.PartitionID, in.Holder)
	if err != nil {
		return ExecutionResult{}, err
	}
	defer func() { _ = in.Leases.Release(lease) }()
	watermark, hasWatermark, err := loadWatermark(ctx, db, in.TenantID, in.PartitionID)
	if err != nil {
		return ExecutionResult{}, err
	}
	plan, err := PlanResume(in.TenantID, in.PartitionID, in.Items, watermark, hasWatermark)
	if err != nil {
		return ExecutionResult{}, err
	}
	result := ExecutionResult{Watermark: plan.Watermark}
	for _, item := range plan.Pending {
		var digest string
		err := withTenantTx(ctx, db, in.TenantID, func(tx dbport.Tx) error {
			var processErr error
			digest, processErr = in.Process(ctx, tx, item)
			if processErr != nil {
				return processErr
			}
			if len(digest) != 64 {
				return invalid("state_digest", "a processor returns a lowercase SHA-256 digest")
			}
			for _, r := range digest {
				if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
					return invalid("state_digest", "a processor returns a lowercase SHA-256 digest")
				}
			}
			_, err := in.Leases.Checkpoint(ctx, tx, lease, JobCheckpoint{
				TenantID: in.TenantID, PartitionID: in.PartitionID, Sequence: item.Index + 1,
				StateDigest: digest, PartitionVersion: partition.Version, TakenAt: now().UTC(),
			})
			return err
		})
		if err != nil {
			return result, fmt.Errorf("jobs: process partition item %d: %w", item.Index, err)
		}
		result.Processed = append(result.Processed, item.Index)
		result.Watermark = item.Index + 1
	}
	err = withTenantTx(ctx, db, in.TenantID, func(tx dbport.Tx) error {
		_, err := (PartitionStore{}).Complete(ctx, tx, in.TenantID, in.PartitionID, partition.Version, now().UTC())
		return err
	})
	if err != nil {
		return result, fmt.Errorf("jobs: complete partition %s: %w", in.PartitionID, err)
	}
	return result, nil
}

func loadAndClaim(ctx context.Context, db dbport.Beginner, in PartitionExecution, at time.Time) (JobPartition, error) {
	var out JobPartition
	err := withTenantTx(ctx, db, in.TenantID, func(tx dbport.Tx) error {
		part, err := (PartitionStore{}).Load(ctx, tx, in.TenantID, in.PartitionID)
		if err != nil {
			return err
		}
		switch part.State {
		case PartitionPending:
			out, err = (PartitionStore{}).ClaimPartition(ctx, tx, in.TenantID, in.PartitionID, part.Version, in.Holder, at)
		case PartitionClaimed:
			if part.ClaimedBy == in.Holder && at.Before(part.ClaimedAt.Add(in.LeaseTTL)) {
				out = part
				return nil
			}
			if at.Before(part.ClaimedAt.Add(in.LeaseTTL)) {
				return fmt.Errorf("%w: partition %s held by %q", ErrLeaseHeld, part.PartitionID, part.ClaimedBy)
			}
			out, err = (PartitionStore{}).ReclaimExpiredPartition(ctx, tx, in.TenantID, in.PartitionID, part.Version, part.ClaimedAt.Add(in.LeaseTTL), in.Holder, at)
		case PartitionCompleted:
			out = part
			return nil
		default:
			return fmt.Errorf("%w: partition %s is %s", ErrIllegalTransition, part.PartitionID, part.State)
		}
		return err
	})
	if err != nil {
		return JobPartition{}, err
	}
	return out, nil
}

func loadWatermark(ctx context.Context, db dbport.Beginner, tenant, partition uuid.UUID) (uint64, bool, error) {
	var watermark uint64
	var present bool
	err := withTenantTx(ctx, db, tenant, func(tx dbport.Tx) error {
		var err error
		watermark, present, err = LoadWatermark(ctx, tx, tenant, partition)
		return err
	})
	return watermark, present, err
}

func withTenantTx(ctx context.Context, db dbport.Beginner, tenant uuid.UUID, fn func(dbport.Tx) error) error {
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
