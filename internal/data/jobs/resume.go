package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// ErrItemFailed reports a resumable execution that stopped at a logical
// item whose effect failed. The returned [ExecutionResult] still names
// exactly what was applied and the explicit incomplete tail, so the next
// attempt resumes from the same durable watermark.
var ErrItemFailed = errors.New("jobs: partition item failed")

// WorkItem is one logical item of a partition's input: a dense,
// zero-based position plus the caller-interpreted key of the unit of
// work. The executor never interprets Key; it only guarantees each index
// is applied at most once per execution.
type WorkItem struct {
	Index uint64
	Key   string
}

// ResumePlan is the executable remainder of a partition: every input item
// at or past the durable watermark, in index order. Watermark counts
// applied items: a checkpoint sequence N means items 0..N-1 are durable,
// so execution restarts at N. Without a watermark the whole input
// replays.
type ResumePlan struct {
	TenantID    uuid.UUID
	PartitionID uuid.UUID
	Watermark   uint64
	Pending     []WorkItem
}

// PlanResume derives the executable remainder of items from a durable
// watermark. hasWatermark must come from [LoadWatermark]: false replays
// the whole input. Duplicate indexes would fork the exactly-once
// guarantee, so they are refused before any plan exists.
func PlanResume(tenant, partition uuid.UUID, items []WorkItem, watermark uint64, hasWatermark bool) (ResumePlan, error) {
	if tenant == uuid.Nil {
		return ResumePlan{}, invalid("tenant_id", "a resume is tenant scoped")
	}
	if partition == uuid.Nil {
		return ResumePlan{}, invalid("partition_id", "a resume names its partition")
	}
	seen := map[uint64]struct{}{}
	for _, item := range items {
		if _, dup := seen[item.Index]; dup {
			return ResumePlan{}, invalid("items", "duplicate logical index forks exactly-once execution")
		}
		seen[item.Index] = struct{}{}
	}
	start := uint64(0)
	if hasWatermark {
		start = watermark
	}
	pending := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if item.Index >= start {
			pending = append(pending, item)
		}
	}
	slices.SortFunc(pending, func(a, b WorkItem) int {
		switch {
		case a.Index < b.Index:
			return -1
		case a.Index > b.Index:
			return 1
		default:
			return 0
		}
	})
	return ResumePlan{TenantID: tenant, PartitionID: partition, Watermark: start, Pending: pending}, nil
}

// LoadWatermark reads the durable resume point of a partition: the
// highest checkpoint sequence, which counts applied items. A partition
// with no checkpoints reports hasWatermark false rather than an error,
// so a cold start replays from zero. A genuine read failure propagates;
// a missing checkpoint row is not one.
func LoadWatermark(ctx context.Context, ex Executor, tenant, partition uuid.UUID) (watermark uint64, hasWatermark bool, err error) {
	latest, err := (CheckpointStore{}).Latest(ctx, ex, tenant, partition)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("jobs: load resume watermark of %s: %w", partition, err)
	}
	return latest.Sequence, true, nil
}

// ExecutionResult is the explicit outcome of one execution pass: the
// indexes applied in order, the next index to apply, and the tail that
// was not applied. On success Incomplete is empty; on failure it holds
// the failed item plus every item after it, so nothing is silently
// skipped.
type ExecutionResult struct {
	Processed  []uint64
	Watermark  uint64
	Incomplete []WorkItem
}

// ExecuteFromWatermark applies a resume plan's pending items in index
// order through apply, stopping at the first failure. Each pending index
// is applied at most once: success reports the full pass with an empty
// incomplete tail, failure reports [ErrItemFailed] with the applied
// prefix and the explicit incomplete remainder.
func ExecuteFromWatermark(plan ResumePlan, apply func(WorkItem) error) (ExecutionResult, error) {
	if apply == nil {
		return ExecutionResult{}, invalid("apply", "execution needs an effect")
	}
	result := ExecutionResult{Watermark: plan.Watermark}
	for i, item := range plan.Pending {
		if err := apply(item); err != nil {
			result.Incomplete = append([]WorkItem{}, plan.Pending[i:]...)
			return result, fmt.Errorf("%w: item %d (%q): %v", ErrItemFailed, item.Index, item.Key, err)
		}
		result.Processed = append(result.Processed, item.Index)
		result.Watermark = item.Index + 1
	}
	return result, nil
}
