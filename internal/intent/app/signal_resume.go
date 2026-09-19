package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
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

// ExecutionSignalTimeoutResumeRequest resumes one parked SIGNAL node from
// the EXPIRED wait the sweeper committed. It names the wait; it never
// carries a signal payload, because an expiry has none.
type ExecutionSignalTimeoutResumeRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	SubscriptionID          uuid.UUID
	RecordedAt              time.Time
}

// SignalTimeoutResumeExecutor is the optional extension of
// [ProposalExecutor] a timeout-capable driver adapter implements. It is a
// separate interface so every executor that predates the expiry sweeper
// keeps satisfying ProposalExecutor.
type SignalTimeoutResumeExecutor interface {
	ResumeSignalTimeout(ctx context.Context, req ExecutionSignalTimeoutResumeRequest) (ExecutionResult, error)
}

// ErrSignalTimeoutResumeUnsupported reports an executor that cannot resume a
// node from an expired wait.
var ErrSignalTimeoutResumeUnsupported = errors.New("app: executor does not resume signal timeouts")

// ResumeExpiredSignal reconstructs the same approved StartRequest used by
// ExecuteIntent, verifies the node is still waiting, and resumes it through
// the executor's durable timeout path from the named EXPIRED wait. The
// wait's own drift check (it must be EXPIRED with its timeout continuation
// for this instance's SIGNAL node) runs inside the driver's advancement
// transaction.
func (c *Cell) ResumeExpiredSignal(ctx context.Context, instanceID, nodeID string, attempt int, subscriptionID string) (ExecutionResult, error) {
	parsedSubscription, err := uuid.Parse(subscriptionID)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("app: signal timeout resume subscription id: %w", err)
	}
	if c != nil && c.Service != nil && c.Service.executor != nil {
		if _, ok := c.Service.executor.(SignalTimeoutResumeExecutor); !ok {
			return ExecutionResult{}, ErrSignalTimeoutResumeUnsupported
		}
	}
	prepared, err := c.prepareParkedResume(ctx, "signal-timeout", instanceID, nodeID, attempt,
		func(ctx context.Context, tx dbport.Tx, tenantID, instanceID uuid.UUID) (string, error) {
			sub, loadErr := (signals.Store{}).LoadExpiredSubscription(ctx, tx, tenantID, parsedSubscription)
			if loadErr != nil {
				return "", loadErr
			}
			if sub.InstanceID != instanceID || sub.NodeID != nodeID || sub.NodeAttempt != attempt {
				return "", fmt.Errorf("app: signal-timeout resume wait %s is not the parked %s attempt %d",
					parsedSubscription, nodeID, attempt)
			}
			return signals.ExpiryContinuationRef(parsedSubscription), nil
		})
	if err != nil {
		return ExecutionResult{}, err
	}
	executor := c.Service.executor.(SignalTimeoutResumeExecutor)
	result, err := executor.ResumeSignalTimeout(ctx, ExecutionSignalTimeoutResumeRequest{
		Start: prepared.start, InstanceID: prepared.instanceID,
		ExpectedInstanceVersion: prepared.instance.InstanceVersion,
		SubscriptionID:          parsedSubscription,
	})
	if err != nil {
		return ExecutionResult{}, err
	}
	return c.consumeParkedResume(ctx, "signal-timeout", prepared, result)
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
