package projection

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// BarrierStatus is the caller-visible disposition of a read request.
type BarrierStatus string

const (
	BarrierReady       BarrierStatus = "READY"
	BarrierStale       BarrierStatus = "STALE"
	BarrierDegraded    BarrierStatus = "DEGRADED"
	BarrierUnavailable BarrierStatus = "UNAVAILABLE"
	BarrierRebuilding  BarrierStatus = "REBUILDING"
	BarrierTimeout     BarrierStatus = "TIMEOUT"
)

// ReadRequirement declares the minimum source position a caller is allowed
// to observe. Deadline is required; a barrier never waits forever.
type ReadRequirement struct {
	Tenant          uuid.UUID
	ProjectionName  string
	StreamKey       string
	MinimumSequence int64
	Deadline        time.Time
}

// BarrierResult explains the heads used to authorize a read.
type BarrierResult struct {
	Status     BarrierStatus
	Checkpoint Checkpoint
	SourceHead int64
	RetryAfter time.Duration
}

// BarrierError is a typed refusal. Current and SourceHead make a retry or
// operator diagnosis possible without parsing an error string.
type BarrierError struct {
	Status     BarrierStatus
	Required   int64
	Current    int64
	SourceHead int64
	RetryAfter time.Duration
	Cause      error
}

func (e BarrierError) Error() string {
	return fmt.Sprintf("projection: read barrier %s: required %d, current %d, source head %d", e.Status, e.Required, e.Current, e.SourceHead)
}

func (e BarrierError) Unwrap() error { return e.Cause }

// Check makes one consistent observation of the source head and projection
// checkpoint. It never polls business tables and never treats a missing
// watermark as current.
func Check(ctx context.Context, q dbport.Querier, req ReadRequirement) (BarrierResult, error) {
	if req.Tenant == uuid.Nil || req.ProjectionName == "" || req.StreamKey == "" || req.MinimumSequence < 0 || req.Deadline.IsZero() {
		return BarrierResult{}, fmt.Errorf("projection: read barrier: invalid requirement")
	}
	var sourceHead int64
	if err := q.QueryRow(ctx, `SELECT head_sequence FROM stream_head WHERE tenant_id=$1 AND stream_key=$2`, req.Tenant, req.StreamKey).Scan(&sourceHead); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			status := statusOrTimeout(BarrierUnavailable, req.Deadline)
			return BarrierResult{Status: status}, BarrierError{Status: status, Required: req.MinimumSequence, Cause: err}
		}
		return BarrierResult{}, fmt.Errorf("projection: read source head: %w", err)
	}
	cp, err := Read(ctx, q, req.Tenant, req.ProjectionName, req.StreamKey)
	if err != nil {
		var missing ErrCheckpointNotFound
		if errors.As(err, &missing) {
			status := statusOrTimeout(BarrierUnavailable, req.Deadline)
			return BarrierResult{Status: status, SourceHead: sourceHead}, BarrierError{Status: status, Required: req.MinimumSequence, SourceHead: sourceHead, Cause: err}
		}
		return BarrierResult{}, fmt.Errorf("projection: read checkpoint: %w", err)
	}
	result := BarrierResult{Status: BarrierReady, Checkpoint: cp, SourceHead: sourceHead}
	if cp.Status == "REBUILDING" {
		result.Status = BarrierRebuilding
	} else if cp.Status != StatusCurrent {
		result.Status = BarrierDegraded
	} else if sourceHead < req.MinimumSequence {
		result.Status = BarrierUnavailable
	} else if cp.LastAppliedSequence < req.MinimumSequence {
		result.Status = BarrierStale
	}
	if !time.Now().UTC().Before(req.Deadline.UTC()) {
		result.Status = BarrierTimeout
	}
	if result.Status == BarrierReady {
		return result, nil
	}
	err = BarrierError{Status: result.Status, Required: req.MinimumSequence, Current: cp.LastAppliedSequence, SourceHead: sourceHead, RetryAfter: 100 * time.Millisecond}
	return result, err
}

// statusOrTimeout reports TIMEOUT once the requirement's deadline has passed,
// and the given status otherwise.
func statusOrTimeout(status BarrierStatus, deadline time.Time) BarrierStatus {
	if !time.Now().UTC().Before(deadline.UTC()) {
		return BarrierTimeout
	}
	return status
}

// Await retries Check until the requirement is met, the deadline expires, or
// the context is cancelled. Callers that want a single non-blocking decision
// should use Check.
func Await(ctx context.Context, q dbport.Querier, req ReadRequirement) (BarrierResult, error) {
	for {
		result, err := Check(ctx, q, req)
		if err == nil {
			return result, nil
		}
		var barrierErr BarrierError
		if !errors.As(err, &barrierErr) {
			return result, err
		}
		if barrierErr.Status == BarrierUnavailable || barrierErr.Status == BarrierDegraded || barrierErr.Status == BarrierRebuilding {
			if !time.Now().UTC().Before(req.Deadline.UTC()) {
				barrierErr.Status = BarrierTimeout
				result.Status = BarrierTimeout
				return result, barrierErr
			}
		}
		if barrierErr.Status == BarrierTimeout {
			return result, barrierErr
		}
		delay := barrierErr.RetryAfter
		if delay <= 0 {
			delay = 10 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// SetStatus changes only the operational serving state; the watermark and
// digest remain untouched while a rebuild or degraded repair is in progress.
func SetStatus(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, projectionName, streamKey, status string) error {
	if status != StatusCurrent && status != "LAGGING" && status != "STALE" && status != "DISAGREEING" && status != "REBUILDING" {
		return fmt.Errorf("projection: unsupported status %q", status)
	}
	affected, err := tx.Exec(ctx, `UPDATE projection_checkpoint SET status=$4, updated_at=now() WHERE tenant_id=$1 AND projection_name=$2 AND stream_key=$3`, tenant, projectionName, streamKey, status)
	if err != nil {
		return fmt.Errorf("projection: set status: %w", err)
	}
	if affected != 1 {
		return ErrCheckpointNotFound{ProjectionName: projectionName, StreamKey: streamKey}
	}
	return nil
}
