package clockservice

import (
	"context"

	"github.com/google/uuid"
)

// WorkflowPunchResult is the durable result of the synchronous first workflow
// node. Committed is true only after the authoritative time transaction has
// committed and the runtime has returned a durable instance identity.
type WorkflowPunchResult struct {
	PunchResult
	InstanceID      uuid.UUID
	WorkflowID      string
	PlanDigest      string
	StartKey        string
	TraceID         string
	NodeID          string
	Attempt         int
	InstanceVersion int64
	Committed       bool
}

// WorkflowPunchExecutor is the synchronous workflow boundary for accepted
// clock-ins. Implementations invoke the real runtime driver; they must not
// implement a second workflow engine or report acceptance from an outbox-only
// side channel.
type WorkflowPunchExecutor interface {
	ExecutePunch(context.Context, string, PunchWork) (WorkflowPunchResult, error)
}
