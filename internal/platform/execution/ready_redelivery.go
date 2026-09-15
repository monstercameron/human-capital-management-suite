package execution

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

var _ app.ReadyRedeliverer = executeDriverAdapter{}

// RedeliverReady implements app.ReadyRedeliverer over the served driver: it
// drains the READY frontier of an instance whose driver died mid-drain
// (WF-RUN-003), under the fence the recovery sweep carries on ctx.
func (a executeDriverAdapter) RedeliverReady(ctx context.Context, req app.ExecutionRedeliveryRequest) (app.ExecutionResult, error) {
	result, err := a.driver.RedeliverReady(ctx, execute.RedeliverRequest{
		Start: req.Start, InstanceID: req.InstanceID, ExpectedInstanceVersion: req.ExpectedInstanceVersion,
	})
	if err != nil {
		return app.ExecutionResult{}, err
	}
	return adaptExecutionResult(result, req.InstanceID.String()), nil
}
