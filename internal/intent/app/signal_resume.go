package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ErrParkedResumeStale reports that the node a parked resume was dispatched
// for is no longer on the instance's frontier: an earlier resume already
// consumed the wait. A dispatcher settles such work instead of retrying it
// forever. An instance that is merely not RUNNING or WAITING (paused,
// quarantined) is a plain refusal, so its work is retried once the
// intervention ends rather than dropped.
var ErrParkedResumeStale = errors.New("app: parked resume is stale")

// ErrSignalResumeUnsupported reports an executor that cannot resume a node
// from a matched signal.
var ErrSignalResumeUnsupported = errors.New("app: executor does not resume signals")

// ExecutionSignalResumeRequest resumes one parked SIGNAL node from the matched
// receipt internal/data/signals committed. It names the receipt; it never
// carries the signal payload.
type ExecutionSignalResumeRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	SignalID                uuid.UUID
	SubscriptionID          uuid.UUID
	RecordedAt              time.Time
}

// SignalResumeExecutor is the optional extension of [ProposalExecutor] a
// signal-capable driver adapter implements. It is a separate interface so
// every executor that predates WF-RUN-005 keeps satisfying ProposalExecutor.
type SignalResumeExecutor interface {
	ResumeSignal(ctx context.Context, req ExecutionSignalResumeRequest) (ExecutionResult, error)
}

// ResumeMatchedSignal reconstructs the same approved StartRequest used by
// ExecuteIntent, verifies the node is still waiting, and resumes it through
// the executor's durable signal path from the named receipt. The receipt's
// own drift check (it must be an ACCEPTED match for this instance's SIGNAL
// node) runs inside the driver's advancement transaction.
func (c *Cell) ResumeMatchedSignal(ctx context.Context, instanceID, nodeID string, attempt int, signalID, subscriptionID string) (ExecutionResult, error) {
	parsedSignal, err := uuid.Parse(signalID)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: signal resume signal id: %w", err)
	}
	parsedSubscription, err := uuid.Parse(subscriptionID)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: signal resume subscription id: %w", err)
	}
	if c != nil && c.Service != nil && c.Service.executor != nil {
		if _, ok := c.Service.executor.(SignalResumeExecutor); !ok {
			return ExecutionResult{}, ErrSignalResumeUnsupported
		}
	}
	prepared, err := c.prepareParkedResume(ctx, "signal", instanceID, nodeID, attempt,
		func(context.Context, dbport.Tx, uuid.UUID, uuid.UUID) (string, error) {
			return "signal:" + parsedSignal.String(), nil
		})
	if err != nil {
		return ExecutionResult{}, err
	}
	executor := c.Service.executor.(SignalResumeExecutor)
	result, err := executor.ResumeSignal(ctx, ExecutionSignalResumeRequest{
		Start: prepared.start, InstanceID: prepared.instanceID,
		ExpectedInstanceVersion: prepared.instance.InstanceVersion,
		SignalID:                parsedSignal, SubscriptionID: parsedSubscription,
	})
	if err != nil {
		return ExecutionResult{}, err
	}
	return c.consumeParkedResume(ctx, "signal", prepared, result)
}
