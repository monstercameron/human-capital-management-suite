package timer

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// RetryTimers is the production [execute.RetryTimerFactory] (WF-RUN-006): it
// writes the durable RETRY_BACKOFF timer a retry decision parks a failed node
// on, inside the advance transaction. Firing it is this package's ordinary
// [Scheduler.Fire]; the enqueued ready work names the RETRYING attempt and the
// driver's ResumeTimer materializes the attempt the key names.
type RetryTimers struct {
	Scheduler Scheduler
}

var _ execute.RetryTimerFactory = RetryTimers{}

// RetryTimerID is the derived identity of the RETRY_BACKOFF timer that wakes
// attempt of one node, so a replayed decision addresses the promise it made.
func RetryTimerID(tenantID, instanceID uuid.UUID, nodeID string, attempt int) uuid.UUID {
	return uuid.NewSHA1(timerNamespace, []byte(tenantID.String()+"|"+instanceID.String()+"|"+nodeID+"|"+execute.RetryBackoffKey(attempt)))
}

// ScheduleRetry records the RETRY_BACKOFF promise. A replay of the same
// attempt returns the existing row with Replay set; a replay that disagrees
// about the instant is refused rather than moved.
func (r RetryTimers) ScheduleRetry(ctx context.Context, ex runtime.Executor, req execute.RetryTimerRequest) (ret0 execute.TimerHandle, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.timer.schedule_retry", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	loc := location{instanceID: req.InstanceID, nodeID: req.NodeID}
	switch {
	case req.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.NodeID == "":
		return execute.TimerHandle{}, invalid(loc, "a retry timer names its tenant, instance and node")
	case req.Attempt < 2 || req.Key != execute.RetryBackoffKey(req.Attempt):
		return execute.TimerHandle{}, invalid(loc, "a retry timer wakes a later attempt under its derived key")
	case req.FiresAt.IsZero() || req.CreatedAt.IsZero():
		return execute.TimerHandle{}, invalid(loc, "a retry timer carries the decided instant and the caller's clock reading")
	}
	id := RetryTimerID(req.TenantID, req.InstanceID, req.NodeID, req.Attempt)
	loc.timerID = id
	err := r.Scheduler.timers.Set(ctx, ex, runtimestate.Timer{
		TenantID: req.TenantID, TimerID: id, InstanceID: req.InstanceID, NodeID: req.NodeID,
		Key: req.Key, Kind: runtimestate.TimerRetryBackoff, FiresAt: req.FiresAt.UTC(), CreatedAt: req.CreatedAt.UTC(),
		Causal: causalToRow(req.Causal),
	})
	switch {
	case err == nil:
		return execute.TimerHandle{TimerID: id, NodeID: req.NodeID, Key: req.Key, FiresAt: req.FiresAt.UTC(), RetryBackoff: true}, nil
	case errors.Is(err, runtimestate.ErrDuplicate):
		existing, loadErr := r.Scheduler.Load(ctx, ex, req.TenantID, id)
		if loadErr != nil {
			return execute.TimerHandle{}, loadErr
		}
		if !existing.FiresAt.Equal(req.FiresAt) || existing.Kind != runtimestate.TimerRetryBackoff {
			return execute.TimerHandle{}, refuse(CodeInvalid, ErrInvalid, loc,
				"retry timer already promises %s at %s; a replay decided %s", existing.Kind, existing.FiresAt, req.FiresAt)
		}
		return execute.TimerHandle{TimerID: id, NodeID: req.NodeID, Key: existing.Key, FiresAt: existing.FiresAt, Replay: true, RetryBackoff: true}, nil
	case errors.Is(err, runtimestate.ErrInvalid):
		return execute.TimerHandle{}, wrapErr(CodeInvalid, ErrInvalid, loc, err, "the durable store refused the retry timer row")
	default:
		return execute.TimerHandle{}, wrapErr(CodeStorageFailed, ErrStorage, loc, err, "write the retry timer row")
	}
}
