package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	executionscheduler "github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
)

// matchedSignalResumer resumes one parked SIGNAL node from a matched receipt;
// in serve it is app.Cell.ResumeMatchedSignal.
type matchedSignalResumer func(ctx context.Context, instanceID, nodeID string, attempt int, signalID, subscriptionID string) (app.ExecutionResult, error)

// signalResumer adapts the cell's matched-signal resume to the scheduler's
// signal dispatcher (WF-RUN-005). Like [timerDispatcher] it refuses a row for
// any tenant but the one this serve composition was configured for. A resume
// whose node an earlier resume already consumed settles COMPLETED: the wait
// was satisfied, and retrying it would loop forever.
func signalResumer(tenantID string, tenant string, resume matchedSignalResumer) executionscheduler.SignalResumer {
	return executionscheduler.SignalResumerFunc(func(ctx context.Context, work executionscheduler.SignalWork) (executionscheduler.Disposition, error) {
		if work.Row.TenantID.String() != tenantID {
			return executionscheduler.DispositionAbandoned, nil
		}
		result, err := resume(app.WithResumeTenant(ctx, tenant), work.Row.InstanceID.String(), work.Row.NodeID, work.Row.Attempt,
			work.Receipt.SignalID.String(), work.Receipt.SubscriptionID.String())
		if errors.Is(err, app.ErrParkedResumeStale) {
			return executionscheduler.DispositionCompleted, nil
		}
		return timerDispatchDisposition(result, err), err
	})
}
