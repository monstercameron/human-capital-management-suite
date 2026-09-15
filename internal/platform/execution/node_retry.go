package execution

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// The served driver's node retry policy (WF-RUN-006). Every compiled
// BackoffRef the shipped workflows declare resolves here to a capped
// exponential backoff with bounded deterministic jitter and an instance
// deadline; retries park on durable RETRY_BACKOFF timers that the timer
// scheduler fires and the driver's ResumeTimer resumes. The values are
// composition constants until a published policy registry resolves them.
const (
	observationRetryBackoffRef = "policy.retry.observation.bounded/v1"
	effectRetryBackoffRef      = "policy.retry.effect.bounded/v1"
	nodeRetryBudgetVersion     = "workflow-node-retry/v1"
	nodeRetryDependency        = "workflow.node"
)

// composedNodeRetry builds the served policy over one process-local budget
// provisioner: every decision for one instance node spends one shared
// allowance of the node's compiled retries.
func composedNodeRetry() *execute.NodeRetryPolicy {
	return &execute.NodeRetryPolicy{
		Backoffs: map[string]execute.RetryBackoff{
			observationRetryBackoffRef: {BaseDelay: 30 * time.Second, MaxDelay: 5 * time.Minute, JitterFraction: 0.2, Deadline: 24 * time.Hour},
			effectRetryBackoffRef:      {BaseDelay: time.Minute, MaxDelay: 15 * time.Minute, JitterFraction: 0.2, Deadline: 24 * time.Hour},
		},
		Retryable: []runtime.FailureKind{runtime.FailureTransient, runtime.FailureTimeout, runtime.FailureThrottled, runtime.FailureUnavailable},
		Classify:  execute.ClassifyStableErrorClass,
		Budget:    execute.ProvisionedRetryBudget(admission.NewProvisioner(), nodeRetryDependency, nodeRetryBudgetVersion),
		Timers:    timer.RetryTimers{},
	}
}
