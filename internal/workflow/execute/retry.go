package execute

// One immediate-caller retry policy on the served path (WF-RUN-006).
//
// With [Options.NodeRetry] configured, every failed node that carries a
// compiled retry policy is routed by exactly one decision: [runtime.Decide],
// fed the node's compiled attempt cap, the backoff its BackoffRef resolves to,
// a jitter seeded from the tenant, instance, node and attempt (so a replay
// computes the same instant), the instance deadline and the propagated retry
// budget. The driver then acts on that exact decision inside the advance
// transaction:
//
//   - RETRY_NOW (only when the resolved backoff admits zero delay): the next
//     attempt is materialized READY in the same transaction and the drain
//     runs it, still bounded by the attempt cap, the budget and MaxSteps.
//   - RETRY_AFTER: a durable RETRY_BACKOFF timer is scheduled at the exact
//     backoff instant and the run parks. [Driver.ResumeTimer] on that fired
//     timer materializes the next attempt under the instance version
//     compare-and-set, so concurrent resumes create exactly one attempt.
//   - TERMINAL: the failure is marked [frontier.NodeOutcome.RetryTerminal] so
//     the frontier takes the compiled failure route (an OBSERVE node's retry
//     exhaustion route) at once; with no route the refusal reaches the
//     poison-work path (poison.go). Nonretryable, DO_NOT_RETRY, deadline and
//     budget exhaustion never retry.
//
// Only this layer retries a node. The step runner and the capability gateway
// report one attempt's outcome and never loop; the START closure's
// serializable retry (transaction/commit.RetryClosure) retries a database
// abort of the start commit, before any node runs, and is not a node retry.
// Domain and provider error mapping stays with the caller: the driver reads
// only the stable error class a step reported, through
// [NodeRetryPolicy.Classify].

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ErrRetryBackoffPending reports a READY drain that reached a node whose
// failed attempt is waiting on its RETRY_BACKOFF timer. The attempt is
// materialized only by that timer's resume (or by a RETRY_NOW decision), so a
// re-run never skips the backoff.
var ErrRetryBackoffPending = errors.New("workflow execute: node retry is waiting on its backoff timer")

// TimerKindRetryBackoff is the durable timer kind a RETRY_AFTER decision
// parks on. It matches migration 00026's own timer_kind value.
const TimerKindRetryBackoff = "RETRY_BACKOFF"

// Retry decisions the driver acts on.
const (
	RetryDecisionNow      = "RETRY_NOW"
	RetryDecisionAfter    = "RETRY_AFTER"
	RetryDecisionTerminal = "TERMINAL"
)

// retryLayer names the one layer that retries a workflow node.
const retryLayer = "workflow.execute.node"

// RetryClassification is a step's stable error class mapped onto the retry
// policy's failure vocabulary.
type RetryClassification struct {
	Kind       runtime.FailureKind
	DoNotRetry bool
}

// ClassifyStableErrorClass maps the stable error-class vocabulary a step
// reports: the runtime failure kinds retry as themselves, DO_NOT_RETRY is the
// caller's explicit refusal, and every other class is PERMANENT. It performs
// no provider or domain mapping; a class it does not know fails closed.
func ClassifyStableErrorClass(errorClass string) RetryClassification {
	class := strings.ToUpper(strings.TrimSpace(errorClass))
	switch runtime.FailureKind(class) {
	case runtime.FailureTransient, runtime.FailureTimeout, runtime.FailureThrottled, runtime.FailureUnavailable:
		return RetryClassification{Kind: runtime.FailureKind(class)}
	}
	if class == runtime.ReasonDoNotRetry {
		return RetryClassification{Kind: runtime.FailurePermanent, DoNotRetry: true}
	}
	return RetryClassification{Kind: runtime.FailurePermanent}
}

// RetryBackoff is what a compiled node's BackoffRef resolves to.
type RetryBackoff struct {
	// BaseDelay is the first retry's delay; each later retry doubles it.
	BaseDelay time.Duration
	// MaxDelay caps every delay, jitter included.
	MaxDelay time.Duration
	// JitterFraction in [0, 1] shortens a delay by up to that share, by an
	// amount derived from the attempt identity ([RetryJitter]).
	JitterFraction float64
	// Deadline bounds the whole instance, measured from its creation: a
	// retry whose backoff would end past it is terminal. Zero is no deadline.
	Deadline time.Duration
	// Immediate admits zero backoff: a retry runs in the same drain
	// (RETRY_NOW) instead of parking on a timer.
	Immediate bool
}

// RetryBudgetRequest identifies the logical operation whose shared retry
// budget one node failure spends.
type RetryBudgetRequest struct {
	TenantID    uuid.UUID
	InstanceID  uuid.UUID
	WorkflowID  string
	NodeID      string
	MaxAttempts int
	Retryable   []runtime.FailureKind
}

// RetryBudgetFunc returns the propagated budget a node's retries spend. Every
// layer handed the same reference draws from the same tokens.
type RetryBudgetFunc func(ctx context.Context, req RetryBudgetRequest) (runtime.BudgetRef, error)

// RetryTimerRequest asks for the durable RETRY_BACKOFF timer that wakes
// Attempt of NodeID at FiresAt.
type RetryTimerRequest struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Attempt    int
	Key        string
	FiresAt    time.Time
	CreatedAt  time.Time
	Causal     *runtime.CausalMetadata
}

// RetryTimerFactory writes the RETRY_BACKOFF timer inside the advance
// transaction. internal/workflow/timer.RetryTimers implements it.
type RetryTimerFactory interface {
	ScheduleRetry(ctx context.Context, ex runtime.Executor, req RetryTimerRequest) (TimerHandle, error)
}

// NodeRetryPolicy configures the served node retry decision.
type NodeRetryPolicy struct {
	// Backoffs resolves every compiled BackoffRef. A failed node whose
	// reference is absent is refused rather than retried on a guess.
	Backoffs map[string]RetryBackoff
	// Retryable is the failure-kind set the policy may retry.
	Retryable []runtime.FailureKind
	// Classify maps a step's error class. Nil is [ClassifyStableErrorClass].
	Classify func(errorClass string) RetryClassification
	// Budget supplies the propagated retry budget. Required.
	Budget RetryBudgetFunc
	// Timers writes RETRY_BACKOFF timers. Required.
	Timers RetryTimerFactory
}

func (p *NodeRetryPolicy) validate() error {
	if p == nil {
		return nil
	}
	if p.Budget == nil || p.Timers == nil || len(p.Retryable) == 0 || len(p.Backoffs) == 0 {
		return invalid("NodeRetry requires Backoffs, Retryable, Budget and Timers")
	}
	for ref, b := range p.Backoffs {
		if strings.TrimSpace(ref) == "" || b.BaseDelay <= 0 || b.MaxDelay < b.BaseDelay || b.Deadline < 0 ||
			b.JitterFraction < 0 || b.JitterFraction > 1 {
			return invalid("NodeRetry backoff %q needs 0 < BaseDelay <= MaxDelay, a non-negative deadline and jitter in [0,1]", ref)
		}
	}
	return nil
}

func (p *NodeRetryPolicy) classify(errorClass string) RetryClassification {
	if p.Classify != nil {
		return p.Classify(errorClass)
	}
	return ClassifyStableErrorClass(errorClass)
}

// RetryBackoffKey is the durable timer key of the RETRY_BACKOFF timer that
// wakes attempt of a node. workflow_timer is unique per (instance, node, key).
func RetryBackoffKey(attempt int) string {
	return retryBackoffKeyPrefix + strconv.Itoa(attempt)
}

const retryBackoffKeyPrefix = "retry-backoff|attempt:"

func retryBackoffAttempt(key string) (int, bool) {
	rest, ok := strings.CutPrefix(key, retryBackoffKeyPrefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n > 1
}

// RetryJitter returns the deterministic jitter for one retry: the capped
// delay shortened by fraction times a value in [0, 1) derived from the
// tenant, instance, node and upcoming attempt. The same identity always
// yields the same instant, so a replayed decision schedules the same timer.
// The result is truncated to whole milliseconds.
func RetryJitter(tenantID, instanceID uuid.UUID, nodeID string, fraction float64) func(attempt int, delay time.Duration) time.Duration {
	return func(attempt int, delay time.Duration) time.Duration {
		sum := sha256.Sum256([]byte(tenantID.String() + "|" + instanceID.String() + "|" + nodeID + "|" + strconv.Itoa(attempt)))
		unit := float64(binary.BigEndian.Uint64(sum[:8])>>11) / float64(uint64(1)<<53)
		// Whole milliseconds: the durable timer stores the instant at database
		// precision, so the decided instant and the stored one are identical.
		return (delay - time.Duration(float64(delay)*fraction*unit)).Truncate(time.Millisecond)
	}
}

// ProvisionedRetryBudget returns a [RetryBudgetFunc] that provisions one
// budget per instance node on provisioner, allowing the node's compiled
// retries (MaxAttempts-1). Re-provisioning returns the existing budget, so
// every decision and every nested layer for that node spends the same tokens.
func ProvisionedRetryBudget(provisioner *admission.Provisioner, dependency, version string) RetryBudgetFunc {
	return func(_ context.Context, req RetryBudgetRequest) (runtime.BudgetRef, error) {
		if provisioner == nil {
			return runtime.BudgetRef{}, invalid("retry budget has no provisioner")
		}
		classes := make([]admission.FailureClass, 0, len(req.Retryable))
		for _, kind := range req.Retryable {
			if class, ok := admissionClass(kind); ok {
				classes = append(classes, class)
			}
		}
		allowed := req.MaxAttempts - 1
		if allowed < 0 {
			allowed = 0
		}
		operation := "workflow-node:" + req.InstanceID.String() + "/" + req.NodeID
		budget, err := provisioner.Provision(admission.ProvisionSpec{
			TenantID: req.TenantID.String(), Service: "workflow", Dependency: dependency,
			LogicalOperationID: operation, OperationKind: "workflow.node.retry",
			Allowed: allowed, Retryable: classes, Version: version,
		})
		if err != nil {
			return runtime.BudgetRef{}, fmt.Errorf("workflow execute: provision retry budget for %s: %w", req.NodeID, err)
		}
		return runtime.BudgetRef{
			Provisioner: provisioner, BudgetID: budget.ID, TenantID: req.TenantID.String(),
			LogicalOperationID: operation, OperationKind: "workflow.node.retry", Dependency: dependency,
		}, nil
	}
}

func admissionClass(kind runtime.FailureKind) (admission.FailureClass, bool) {
	switch kind {
	case runtime.FailureTransient:
		return admission.FailureTransient, true
	case runtime.FailureTimeout:
		return admission.FailureTimeout, true
	case runtime.FailureThrottled:
		return admission.FailureThrottled, true
	case runtime.FailureUnavailable:
		return admission.FailureUnavailable, true
	default:
		return "", false
	}
}

// nodeRetryDecision is the exact decision for one failed attempt.
type nodeRetryDecision struct {
	kind    string
	route   runtime.RetryRoute
	node    workflow.CompiledNode
	firesAt time.Time
}

// decideRetry routes one outcome. Without NodeRetry, or for a success or a
// node with no compiled retry policy, it keeps the pre-WF-RUN-006 routing.
func (d *Driver) decideRetry(
	ctx context.Context, ex runtime.Executor, run runContext, outcome frontier.NodeOutcome, attempt int, at time.Time,
) (frontier.NodeOutcome, *nodeRetryDecision, error) {
	plan := run.selection.Plan
	node, ok := plan.Node(outcome.NodeID)
	if !ok {
		return outcome, nil, nil
	}
	policy := d.opts.NodeRetry
	if policy == nil || !outcome.Failed || node.Retry == nil {
		return exhaustedObservationRoute(plan, node, outcome, attempt), nil, nil
	}
	spec, ok := policy.Backoffs[node.Retry.BackoffRef]
	if !ok {
		return outcome, nil, invalid("node %s retry backoff %q is not resolved by NodeRetry", node.ID, node.Retry.BackoffRef)
	}
	tenantID, instanceID := run.start.TenantID, run.instanceID
	inst, err := (runtime.Store{}).LoadInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return outcome, nil, err
	}
	elapsed := at.Sub(inst.CreatedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	budget, err := policy.Budget(ctx, RetryBudgetRequest{
		TenantID: tenantID, InstanceID: instanceID, WorkflowID: run.selection.WorkflowID, NodeID: node.ID,
		MaxAttempts: int(node.Retry.MaxAttempts), Retryable: append([]runtime.FailureKind(nil), policy.Retryable...),
	})
	if err != nil {
		return outcome, nil, fmt.Errorf("workflow execute: retry budget for %s: %w", node.ID, err)
	}
	jitter := RetryJitter(tenantID, instanceID, node.ID, spec.JitterFraction)
	if spec.Immediate {
		jitter = func(int, time.Duration) time.Duration { return 0 }
	}
	class := policy.classify(outcome.ErrorClass)
	route, err := runtime.Decide(runtime.RetryPolicy{
		MaxAttempts: int(node.Retry.MaxAttempts), BaseDelay: spec.BaseDelay, MaxDelay: spec.MaxDelay,
		Deadline: spec.Deadline, Retryable: policy.Retryable, Jitter: jitter,
	}, budget, runtime.RetryAttempt{
		N: attempt, Failure: class.Kind, DoNotRetry: class.DoNotRetry, Elapsed: elapsed,
		AttemptID: runtime.NodeExecutionID(tenantID, instanceID, node.ID, attempt).String(), Layer: retryLayer,
	})
	if err != nil {
		return outcome, nil, fmt.Errorf("workflow execute: retry decision for %s: %w", node.ID, err)
	}
	decision := &nodeRetryDecision{route: route, node: node}
	if route.Decision == runtime.RouteRetry {
		decision.kind = RetryDecisionAfter
		decision.firesAt = at.Add(route.NextDelay)
		if route.NextDelay == 0 && spec.Immediate {
			decision.kind = RetryDecisionNow
		}
		return outcome, decision, nil
	}
	decision.kind = RetryDecisionTerminal
	outcome.RetryTerminal = true
	if node.Type == workflow.StepObserve {
		// An OBSERVE node declares its terminal route as its retry
		// exhaustion route; a terminal decision is exhaustion of retrying.
		if routed := exhaustedObservationRoute(plan, node, outcome, int(node.Retry.MaxAttempts)); !routed.Failed {
			routed.RetryTerminal = false
			outcome = routed
		}
	}
	return outcome, decision, nil
}

// applyRetry acts on a RETRY decision inside the advance transaction, after
// the frontier recorded the failed attempt RETRYING.
func (d *Driver) applyRetry(
	ctx context.Context, ex runtime.Executor, run runContext, advanced *runtime.AdvanceReceipt, decision *nodeRetryDecision, at time.Time,
) ([]TimerHandle, error) {
	if decision == nil || decision.kind == RetryDecisionTerminal {
		return nil, nil
	}
	node := decision.node
	if advanced.CompletedState != string(runtime.NodeRetrying) {
		return nil, invalid("retry policy decided %s for node %s but the frontier settled the attempt %s",
			decision.kind, node.ID, advanced.CompletedState)
	}
	tenantID, instanceID := run.start.TenantID, run.instanceID
	next := decision.route.NextAttempt
	if decision.kind == RetryDecisionNow {
		created := runtime.NewNodeExecution(tenantID, instanceID, node.ID, next, node.Type, runtime.NodeReady)
		_, version, err := (runtime.Store{}).RecordNodeExecution(ctx, ex, created, advanced.NewInstanceVersion)
		if err != nil {
			return nil, fmt.Errorf("workflow execute: materialize immediate retry %d for %s: %w", next, node.ID, err)
		}
		advanced.NewInstanceVersion = version
		return nil, nil
	}
	var causal *runtime.CausalMetadata
	for _, rec := range advanced.Continuations {
		if rec.Kind == frontier.IntentReady && rec.TargetNodeID == node.ID {
			causal = rec.Causal
		}
	}
	handle, err := d.opts.NodeRetry.Timers.ScheduleRetry(ctx, ex, RetryTimerRequest{
		TenantID: tenantID, InstanceID: instanceID, NodeID: node.ID, Attempt: next,
		Key: RetryBackoffKey(next), FiresAt: decision.firesAt, CreatedAt: at, Causal: causal,
	})
	if err != nil {
		return nil, fmt.Errorf("workflow execute: schedule retry backoff for %s: %w", node.ID, err)
	}
	if handle.NodeID != "" && handle.NodeID != node.ID {
		return nil, invalid("RetryTimerFactory returned a timer for node %s while scheduling %s", handle.NodeID, node.ID)
	}
	if !handle.FiresAt.Equal(decision.firesAt) {
		return nil, invalid("RetryTimerFactory promised %s for node %s, the decision is %s", handle.FiresAt, node.ID, decision.firesAt)
	}
	handle.NodeID, handle.RetryBackoff = node.ID, true
	return []TimerHandle{handle}, nil
}

// poisonRoute is the terminal decision a poison-work filing keeps, or nil.
func (n *nodeRetryDecision) poisonRoute() *runtime.RetryRoute {
	if n == nil || n.kind != RetryDecisionTerminal {
		return nil
	}
	route := n.route
	return &route
}

// retryParked reports whether this advancement parked nodeID on a
// RETRY_BACKOFF timer rather than leaving it READY for this drain.
func retryParked(timers []TimerHandle, nodeID string) bool {
	for _, t := range timers {
		if t.RetryBackoff && t.NodeID == nodeID {
			return true
		}
	}
	return false
}

// currentRetryAttempt is prepareReadyAttempt under NodeRetry: the attempt to
// run is the latest durable one, which only a retry decision or a backoff
// resume materializes. A latest attempt still RETRYING is waiting on its
// backoff and is refused.
func (d *Driver) currentRetryAttempt(ctx context.Context, run runContext, node workflow.CompiledNode) (int, error) {
	var latest runtime.NodeExecution
	if err := d.inTenantTx(ctx, run.start.TenantID, func(ex runtime.Executor) error {
		rows, err := (runtime.Store{}).LoadNodeExecutions(ctx, ex, run.start.TenantID, run.instanceID)
		latest = latestExecution(rows, node.ID)
		return err
	}); err != nil {
		return 0, err
	}
	if latest.Attempt == 0 {
		return 0, invalid("ready node %s has no durable execution", node.ID)
	}
	if err := refuseSettledAttempt(latest); err != nil {
		return 0, err
	}
	if latest.Status == runtime.NodeRetrying {
		return 0, fmt.Errorf("%w: node %s attempt %d", ErrRetryBackoffPending, node.ID, latest.Attempt)
	}
	return latest.Attempt, nil
}

func latestExecution(rows []runtime.NodeExecution, nodeID string) runtime.NodeExecution {
	latest := runtime.NodeExecution{}
	for _, row := range rows {
		if row.NodeID == nodeID && row.Attempt > latest.Attempt {
			latest = row
		}
	}
	return latest
}

// resumeRetryBackoff handles [Driver.ResumeTimer] for a RETRY_BACKOFF timer.
// It reports handled=false for every other timer kind, which ResumeTimer
// advances as a WAIT exactly as before. In one transaction it checks the
// fired row against the pinned plan and the durable attempt, and records the
// woken attempt READY under the instance version compare-and-set; concurrent
// resumes of the same timer therefore create exactly one attempt, and a
// resume after the attempt exists is refused ([ErrTimerDrift]).
func (d *Driver) resumeRetryBackoff(ctx context.Context, run runContext, req ResumeTimerRequest, at time.Time) (Result, bool, error) {
	tenantID := req.Start.TenantID
	handled := false
	var nodeID string
	var version int64
	var frontierIDs []string
	err := d.inTenantTx(ctx, tenantID, func(ex runtime.Executor) error {
		row, err := d.opts.TimerReader.LoadTimer(ctx, ex, tenantID, req.TimerID)
		if err != nil {
			return err
		}
		if row.Kind != TimerKindRetryBackoff {
			return nil
		}
		handled = true
		if d.opts.NodeRetry == nil {
			return invalid("RETRY_BACKOFF timer %s resumed on a driver without NodeRetry", row.TimerID)
		}
		node, attempt, err := checkRetryTimer(req, run.selection.Plan, row)
		if err != nil {
			return err
		}
		if fence := d.currentFence(ctx); fence != nil {
			stamped := *fence
			stamped.At = at
			if err := d.verifyFence(ctx, ex, tenantID, stamped); err != nil {
				return err
			}
		}
		store := runtime.Store{}
		inst, err := store.LoadInstance(ctx, ex, tenantID, req.InstanceID)
		if err != nil {
			return err
		}
		if inst.RuntimeStatus != runtime.InstanceRunning || inst.InstanceVersion != req.ExpectedInstanceVersion {
			return timerDrift("retry timer %s resumes instance %s at version %d, but it is %s at version %d",
				row.TimerID, req.InstanceID, req.ExpectedInstanceVersion, inst.RuntimeStatus, inst.InstanceVersion)
		}
		rows, err := store.LoadNodeExecutions(ctx, ex, tenantID, req.InstanceID)
		if err != nil {
			return err
		}
		latest := latestExecution(rows, node.ID)
		nodeID, version, frontierIDs = node.ID, inst.InstanceVersion, inst.CurrentNodeIDs
		switch {
		case latest.Status == runtime.NodeRetrying && latest.Attempt == attempt-1:
			created := runtime.NewNodeExecution(tenantID, req.InstanceID, node.ID, attempt, node.Type, runtime.NodeReady)
			_, version, err = store.RecordNodeExecution(ctx, ex, created, inst.InstanceVersion)
			if err != nil {
				return fmt.Errorf("workflow execute: materialize retry attempt %d for %s: %w", attempt, node.ID, err)
			}
			return nil
		case latest.Status == runtime.NodeReady && latest.Attempt == attempt && d.currentFence(ctx) != nil:
			// A fenced resume that crashed after materializing and before
			// running the attempt; the lease excludes a concurrent runner.
			return nil
		default:
			return timerDrift("retry timer %s wakes %s attempt %d, but its latest attempt is %d %s",
				row.TimerID, node.ID, attempt, latest.Attempt, latest.Status)
		}
	})
	if err != nil || !handled {
		return Result{}, handled, err
	}
	result := Result{InstanceVersion: version, Frontier: append([]string(nil), frontierIDs...)}
	result, err = d.drainReady(ctx, run, result, []string{nodeID})
	return result, true, err
}

// checkRetryTimer compares a loaded RETRY_BACKOFF row against the request and
// the pinned plan, returning the node and the attempt it wakes.
func checkRetryTimer(req ResumeTimerRequest, plan *workflow.CompiledWorkflow, row FiredTimer) (workflow.CompiledNode, int, error) {
	if row.TimerID != req.TimerID || row.InstanceID != req.InstanceID {
		return workflow.CompiledNode{}, 0, timerDrift("retry timer %s of instance %s is not the requested %s of %s",
			row.TimerID, row.InstanceID, req.TimerID, req.InstanceID)
	}
	if row.State != TimerStateFired {
		return workflow.CompiledNode{}, 0, timerDrift("retry timer %s is %s, not %s", row.TimerID, row.State, TimerStateFired)
	}
	node, ok := plan.Node(row.NodeID)
	if !ok || node.Retry == nil {
		return workflow.CompiledNode{}, 0, timerDrift("retry timer %s names node %s, which declares no retry policy in the pinned plan", row.TimerID, row.NodeID)
	}
	if req.Outcome.NodeID != "" && req.Outcome.NodeID != row.NodeID {
		return workflow.CompiledNode{}, 0, timerDrift("resume names node %s but retry timer %s wakes %s", req.Outcome.NodeID, row.TimerID, row.NodeID)
	}
	attempt, ok := retryBackoffAttempt(row.Key)
	if !ok || attempt > int(node.Retry.MaxAttempts) {
		return workflow.CompiledNode{}, 0, timerDrift("retry timer %s key %q names no admissible attempt of %s", row.TimerID, row.Key, row.NodeID)
	}
	return node, attempt, nil
}
