package execute

// Poison-node quarantine on the served path (WF-RUN-007). A node whose retry
// budget is exhausted with no declared failure route used to be refused
// NO_FAILURE_ROUTE: the advance transaction rolled back, the last attempt left
// no durable trace and the instance stayed RUNNING with nothing to run. With
// [Options.PoisonWork] configured the driver instead admits the exhausted work
// through [runtime.Admit], files the sealed [runtime.QuarantinedWork] record,
// marks the attempt FAILED and routes the instance to the BLOCKED,
// REPAIR_REQUIRED or QUARANTINED status the admission decided -- all inside the
// advance transaction, so the record and the route commit together or not at
// all. The record is an operations projection; the instance status carries the
// workflow state and is never a success.
//
// This is unrelated to version quarantine (internal/workflow/quarantine),
// which refuses starts on a bad workflow version.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ErrPoisonWorkQuarantined reports that an exhausted node was durably quarantined
// and its instance routed away from RUNNING. Read the record with
// errors.As(err, *[PoisonWorkError]).
var ErrPoisonWorkQuarantined = errors.New("workflow execute: exhausted node quarantined")

// PoisonWorkStore files one sealed record through the advance
// transaction. [runtime.QuarantineStore] satisfies it.
type PoisonWorkStore interface {
	File(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID, work runtime.QuarantinedWork, recordedAt time.Time) (runtime.QuarantinedWork, error)
}

// PoisonWorkPolicy is the served driver's poison-work policy.
type PoisonWorkPolicy struct {
	// Store files the record. Required.
	Store PoisonWorkStore
	// Owner is who is accountable for quarantined work: BLOCKED and
	// QUARANTINED records require one. Required.
	Owner string
	// SLA is how long the owner has to act; retained on every record.
	SLA time.Duration
	// RepairRoute is the governed repair a caller-mapped BUDGET_EXHAUSTED
	// failure waits for (REPAIR_REQUIRED). Empty treats such a failure as
	// ordinary attempt exhaustion (BLOCKED).
	RepairRoute string
}

func (p *PoisonWorkPolicy) validate() error {
	if p == nil {
		return nil
	}
	if p.Store == nil || strings.TrimSpace(p.Owner) == "" || p.SLA < 0 {
		return invalid("PoisonWork requires a Store, an Owner and a non-negative SLA")
	}
	return nil
}

// PoisonWorkError carries the durable record and the status the instance was
// routed to. It unwraps to [ErrPoisonWorkQuarantined].
type PoisonWorkError struct {
	Work   runtime.QuarantinedWork
	Status runtime.InstanceStatus
	cause  error
}

func (e *PoisonWorkError) Error() string {
	return fmt.Sprintf("%v: node %s after %d attempts routed the instance %s (%s; next action %s): %v",
		ErrPoisonWorkQuarantined, e.Work.NodeID, e.Work.Attempts, e.Status, e.Work.LastError, e.Work.NextAction, e.cause)
}

// Unwrap exposes the sentinel only: the frontier refusal the quarantine
// replaced is described in the message, never re-classified as the outcome.
func (e *PoisonWorkError) Unwrap() error { return ErrPoisonWorkQuarantined }

// poisonable reports whether an advancement refusal is the exhausted-retry
// refusal this policy owns.
func (d *Driver) poisonable(err error) bool {
	return err != nil && d.opts.PoisonWork != nil && frontier.CodeOf(err) == frontier.CodeNoFailureRoute
}

// poisonOutcome is the advance span outcome for a poison-work filing: a
// committed quarantine is a denial of progress, a failed filing a failure.
func poisonOutcome(err error) string {
	if errors.Is(err, ErrPoisonWorkQuarantined) {
		return OutcomeDenied
	}
	return OutcomeFailure
}

// refuseSettledAttempt stops a re-run from executing a node whose latest
// attempt a poison-work quarantine already settled FAILED: the handler never
// runs again for it, so quarantined work is not silently retried.
func refuseSettledAttempt(latest runtime.NodeExecution) error {
	if latest.Status != runtime.NodeFailed {
		return nil
	}
	return fmt.Errorf("%w: node %s attempt %d is settled FAILED and does not run again",
		ErrPoisonWorkQuarantined, latest.NodeID, latest.Attempt)
}

// filePoisonWork files the exhausted node's record, fails its attempt and
// routes the instance inside tx, then commits tx.
func (d *Driver) filePoisonWork(
	ctx context.Context, tx dbport.Tx, run runContext, outcome frontier.NodeOutcome, attempt int, at time.Time,
	retry *runtime.RetryRoute, cause error,
) error {
	policy := d.opts.PoisonWork
	tenantID, instanceID := run.start.TenantID, run.instanceID
	node, _ := run.selection.Plan.Node(outcome.NodeID)
	spec := poisonSpec(*policy, run.selection.WorkflowID, node, outcome, attempt)
	keepRetryReason(&spec, *policy, retry)
	spec.IdempotencyKey = "wfq:" + runtime.NodeExecutionID(tenantID, instanceID, outcome.NodeID, attempt).String()
	work, err := runtime.Admit(spec)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("workflow execute: admit quarantined work for %s: %w", outcome.NodeID, err))
	}
	filed, err := policy.Store.File(ctx, tx, tenantID, instanceID, work, at)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("workflow execute: file quarantined work for %s: %w", outcome.NodeID, err))
	}
	status := runtime.InstanceStatus(filed.Route)
	if err := failPoisonAttempt(ctx, tx, tenantID, instanceID, outcome, attempt, status, at); err != nil {
		return errors.Join(cause, fmt.Errorf("workflow execute: route quarantined instance: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.Join(cause, fmt.Errorf("workflow execute: commit quarantine of %s: %w", outcome.NodeID, err))
	}
	return &PoisonWorkError{Work: filed, Status: status, cause: cause}
}

// poisonSpec maps the exhausted node onto runtime.Admit's terminal route:
// an ambiguous error class waits under quarantine for reconciliation, a
// caller-mapped terminal reason is kept, a declared retry budget is attempt
// exhaustion, and anything else is non-retryable.
func poisonSpec(policy PoisonWorkPolicy, workflowID string, node workflow.CompiledNode, outcome frontier.NodeOutcome, attempt int) runtime.QuarantineSpec {
	class := strings.TrimSpace(outcome.ErrorClass)
	if class == "" {
		class = "NO_OUTCOME"
	}
	reason := runtime.ReasonNonretryable
	switch {
	case class == runtime.ReasonBudgetExhausted && policy.RepairRoute != "":
		reason = runtime.ReasonBudgetExhausted
	case class == runtime.ReasonDoNotRetry || class == runtime.ReasonDeadlineExceeded || class == runtime.ReasonNonretryable:
		reason = class
	case node.Retry != nil && node.Retry.MaxAttempts > 1:
		reason = runtime.ReasonAttemptsExhausted
	}
	ambiguous := strings.Contains(class, "AMBIGUOUS") || strings.HasPrefix(class, "UNKNOWN")
	spec := runtime.QuarantineSpec{
		NodeID: outcome.NodeID, WorkflowID: workflowID, Attempts: attempt, LastError: class,
		Ambiguous: ambiguous, Owner: policy.Owner, SLA: policy.SLA,
		Terminal: runtime.RetryRoute{Decision: runtime.RouteTerminal, Reason: reason, RepairRoute: policy.RepairRoute},
	}
	if ambiguous {
		spec.NextAction = "reconcile-outcome:" + outcome.NodeID
	}
	return spec
}

// keepRetryReason carries the retry policy's own terminal reason (WF-RUN-006)
// into the filing instead of re-deriving it from the error class. A budget
// exhaustion keeps its repair route; without one it is attempt exhaustion.
func keepRetryReason(spec *runtime.QuarantineSpec, policy PoisonWorkPolicy, retry *runtime.RetryRoute) {
	if retry == nil || retry.Reason == "" {
		return
	}
	reason, repair := retry.Reason, policy.RepairRoute
	if reason == runtime.ReasonBudgetExhausted {
		if repair == "" {
			repair = retry.RepairRoute
		}
		if repair == "" {
			reason = runtime.ReasonAttemptsExhausted
		}
	}
	spec.Terminal.Reason, spec.Terminal.RepairRoute = reason, repair
}

// failPoisonAttempt records the exhausted attempt as FAILED with its error class
// and moves the instance to status, keeping its frontier and dimensions.
func failPoisonAttempt(
	ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID,
	outcome frontier.NodeOutcome, attempt int, status runtime.InstanceStatus, at time.Time,
) error {
	store := runtime.Store{}
	current, err := store.LoadInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return err
	}
	execution, err := store.LoadNodeExecution(ctx, ex, tenantID, instanceID, outcome.NodeID, attempt)
	if err != nil {
		return err
	}
	version := current.InstanceVersion
	// A settled FAILED attempt never reaches here: the frontier refuses it
	// NODE_NOT_ACTIVE before it could be refused NO_FAILURE_ROUTE.
	path := []runtime.NodeStatus{runtime.NodeFailed}
	if execution.Status == runtime.NodeReady {
		path = []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}
	}
	for i, hop := range path {
		t := runtime.NodeTransition{
			TenantID: tenantID, InstanceID: instanceID, NodeID: outcome.NodeID, Attempt: attempt,
			ExpectedInstanceVersion: version, Status: hop,
		}
		if i == len(path)-1 {
			completed := at
			t.ErrorClass, t.CompletedAt = outcome.ErrorClass, &completed
		}
		if _, version, err = store.RecordNodeTransition(ctx, ex, t); err != nil {
			return err
		}
	}
	_, err = store.RecordInstanceState(ctx, ex, runtime.InstanceTransition{
		TenantID: tenantID, InstanceID: instanceID, ExpectedVersion: version,
		Status:               status,
		CurrentNodeIDs:       current.CurrentNodeIDs,
		VariableRevisionHead: current.VariableRevisionHead,
		EffectiveContextRef:  current.EffectiveContextRef,
		LastCheckpointRef:    current.LastCheckpointRef,
		CompletionDimensions: current.CompletionDimensions,
	})
	return err
}
