package execution

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

var (
	_ app.SignalResumeExecutor        = executeDriverAdapter{}
	_ app.SignalTimeoutResumeExecutor = executeDriverAdapter{}
)

// ResumeSignalTimeout adapts app's timeout resume request onto the driver's
// durable timeout path. Only the expired wait's identity crosses: the driver
// reloads the committed expiry itself and always advances TIMED_OUT.
func (a executeDriverAdapter) ResumeSignalTimeout(ctx context.Context, req app.ExecutionSignalTimeoutResumeRequest) (app.ExecutionResult, error) {
	result, err := a.driver.ResumeSignalTimeout(ctx, execute.ResumeSignalTimeoutRequest{
		Start: req.Start, InstanceID: req.InstanceID,
		ExpectedInstanceVersion: req.ExpectedInstanceVersion,
		SubscriptionID:          req.SubscriptionID, RecordedAt: req.RecordedAt,
	})
	if err != nil {
		return app.ExecutionResult{}, err
	}
	return adaptExecutionResult(result, req.InstanceID.String()), nil
}

// ResumeSignal adapts app's signal resume request onto the driver's durable
// signal path (WF-RUN-005). Only the receipt's identity crosses: the driver
// reloads the matched receipt itself.
func (a executeDriverAdapter) ResumeSignal(ctx context.Context, req app.ExecutionSignalResumeRequest) (app.ExecutionResult, error) {
	result, err := a.driver.ResumeSignal(ctx, execute.ResumeSignalRequest{
		Start: req.Start, InstanceID: req.InstanceID,
		ExpectedInstanceVersion: req.ExpectedInstanceVersion,
		SignalID:                req.SignalID, SubscriptionID: req.SubscriptionID, RecordedAt: req.RecordedAt,
	})
	if err != nil {
		return app.ExecutionResult{}, err
	}
	return adaptExecutionResult(result, req.InstanceID.String()), nil
}
