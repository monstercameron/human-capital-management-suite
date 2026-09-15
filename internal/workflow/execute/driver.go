package execute

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Status is the bounded driver's outcome.
type Status string

const (
	StatusParked   Status = "PARKED"
	StatusComplete Status = "COMPLETE"
	StatusResolved Status = "RESOLVED"
)

// Options are the ports and explicit policies a Driver needs. MaxSteps bounds
// one synchronous call; zero uses a conservative plan-sized bound.
type Options struct {
	DB        Beginner
	Steps     StepRunner
	WorkItems WorkItemFactory
	Terminal  TerminalWriter
	Repair    RepairRequester
	Guard     idempotency.Store
	Retention idempotency.RetentionPolicy
	Clock     func() time.Time
	MaxSteps  int
	Advance   AdvanceFunc
	// Instrumentation is OBS-023's span/log port. Nil means
	// [NoopInstrumentation]: no spans, no log lines, every composition that
	// predates OBS-023 keeps running unchanged.
	Instrumentation Instrumentation
	// Recorder is the workflow-engine telemetry recorder
	// (internal/workflow/observe) attached to every call's context that does
	// not already carry one, so the runtime, lease, timer and step operations
	// the driver reaches each emit a span and a log line. Nil attaches none.
	Recorder observe.Recorder
	// Workload is WF-RUN-021's admission gate applied to every start this
	// driver runs whose request does not carry its own. Nil admits every start,
	// exactly as before WF-RUN-021.
	Workload *runtime.WorkloadGate
	// Evidence is OBS-024's execution-evidence port. Nil means
	// [NoopExecutionEvidence].
	Evidence ExecutionEvidence
	// Items loads the durable WorkItem a Resume advances from. Required only
	// once a caller actually calls Resume (WF-RUN-028); Execute never reads
	// it.
	Items WorkItemReader
	// Currency revalidates the pinned proposal, its approval and its control
	// snapshots before every advancement this driver attempts, including the
	// one that reaches a terminal write (WF-RUN-029). Nil runs no currency
	// check at all, exactly reproducing this driver's pre-WF-RUN-029
	// behavior.
	Currency *CurrencyGuard
	// Timers creates the durable timer a TIMER_REQUIRED continuation
	// describes (WF-RUN-004). Nil leaves a TIMER_REQUIRED continuation
	// unsupported, exactly as it was before that ticket.
	Timers TimerFactory
	// TimerReader loads the durable timer a [Driver.ResumeTimer] advances
	// from. Required only once a caller actually calls ResumeTimer.
	TimerReader TimerReader
	// ConflictFence is the application-composed durable conflict adapter used
	// only when a prepared transaction plan carries a registered intent. It is
	// intentionally a port: workflow execution must not construct a data
	// adapter, and legacy plans remain unfenced when this is nil.
	ConflictFence transactioncommit.ConflictFence
	// Fence and FenceVerifier make every advancement this driver performs a
	// fenced one (WF-RUN-002): the fence is verified against the durable
	// lease before the advancement reads anything, so a worker whose lease was
	// taken over completes no node, writes no state and dispatches no effect.
	// They are set together or not at all; Fence.At is stamped per call from
	// the instant the driver is already using, so a caller never has to keep
	// it current.
	//
	// Configuring a fence takes the advancement through
	// [runtime.AdvanceFenced] and therefore bypasses Advance: the two seams
	// are alternatives, and a test that wants an injected AdvanceFunc leaves
	// the fence unset.
	Fence         *runtime.Fence
	FenceVerifier runtime.FenceVerifier
	// StartRetry opts into a bounded serializable retry of the complete start
	// closure. Nil preserves the historical single transaction behavior.
	StartRetry *transactioncommit.RetryOptions
	// StartRetryFor selects a request-scoped retry policy without storing
	// tenant or operation state on the Driver. It is evaluated once per
	// Execute call and its returned options are copied locally.
	StartRetryFor func(context.Context, StartRetryIdentity) (*transactioncommit.RetryOptions, error)
}

// StartRetryIdentity is the complete immutable identity needed to select a
// persisted retry budget. Proposal content, mutable slices and runtime ports
// deliberately do not cross this policy boundary.
type StartRetryIdentity struct {
	TenantID            uuid.UUID
	StartIdempotencyKey string
}

// Driver synchronously runs the READY frontier of one newly started workflow.
type Driver struct {
	opts    Options
	advance AdvanceFunc
}

// New validates immutable driver wiring. StepRunner is required because every
// supported execution starts with READY work; the other ports are checked when
// their corresponding continuation is actually reached.
func New(opts Options) (*Driver, error) {
	if opts.DB == nil {
		return nil, invalid("database Beginner is required")
	}
	if opts.Steps == nil {
		return nil, invalid("StepRunner is required")
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Now().UTC() }
	}
	if opts.Instrumentation == nil {
		opts.Instrumentation = NoopInstrumentation{}
	}
	if opts.Evidence == nil {
		opts.Evidence = NoopExecutionEvidence{}
	}
	if (opts.Fence == nil) != (opts.FenceVerifier == nil) {
		return nil, invalid("a lease fence and its verifier are configured together or not at all")
	}
	if opts.StartRetry != nil && opts.StartRetryFor != nil {
		return nil, invalid("StartRetry and StartRetryFor are mutually exclusive")
	}
	advance := opts.Advance
	if advance == nil {
		advance = runtime.Advance
	}
	return &Driver{opts: opts, advance: advance}, nil
}

// observed attaches the driver's recorder to ctx unless the caller already
// attached one.
func (d *Driver) observed(ctx context.Context) context.Context {
	if d == nil || observe.RecorderFrom(ctx) != nil {
		return ctx
	}
	return observe.WithRecorder(ctx, d.opts.Recorder)
}

// ExecuteRequest starts from an already-materialized immutable proposal. The
// embedded StartRequest carries the policy resolver, exact version store and
// all proposal-alignment assertions runtime.Start validates.
type ExecuteRequest struct {
	Start runtime.StartRequest
}

// Result is either COMPLETE or PARKED on the WorkItems returned here. Every
// receipt is from a committed transaction.
type Result struct {
	Status Status
	Start  runtime.StartReceipt
	// StartResolution is populated only when an ambiguous START commit is
	// resolved by an explicit read-only transaction. Such a result is never
	// drained or projected as COMPLETE.
	StartResolution *runtime.StartResolution
	Advances        []runtime.AdvanceReceipt
	WorkItems       []workitem.WorkItem
	// Timers are the durable timers this result's advancements created. An
	// instance parked on one is waiting for a caller to settle it, not for a
	// person.
	Timers          []TimerHandle
	InstanceVersion int64
	Frontier        []string
	// EvidenceIDs are the OBS-024 execution-evidence ids recorded while
	// producing this result (APPROVAL_COMPLETED/TASK_SUBMITTED on a
	// [Driver.Resume], TERMINAL_WRITTEN on any call that reaches COMPLETE),
	// in recording order.
	EvidenceIDs []string
}

// Execute resolves the workflow once, starts it atomically, then drains its
// READY continuations. It returns as soon as human work is durably created.
func (d *Driver) Execute(ctx context.Context, req ExecuteRequest) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.execute", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.Start.Resolver == nil {
		return Result{}, invalid("StartRequest has no WorkflowResolver")
	}
	var selection runtime.WorkflowSelection
	var err error
	startReq := req.Start
	var started runtime.StartReceipt
	startRetry := d.opts.StartRetry
	if d.opts.StartRetryFor != nil {
		identity := StartRetryIdentity{TenantID: startReq.TenantID, StartIdempotencyKey: startReq.StartIdempotencyKey}
		if identity.TenantID == uuid.Nil || strings.TrimSpace(identity.StartIdempotencyKey) == "" {
			return Result{}, invalid("StartRetryFor requires tenant and start idempotency identity")
		}
		startRetry, err = d.opts.StartRetryFor(ctx, identity)
		if err != nil {
			return Result{}, fmt.Errorf("workflow execute: select start retry policy: %w", err)
		}
		if startRetry == nil {
			return Result{}, invalid("StartRetryFor returned no retry policy")
		}
		if startRetry.MaxAttempts < 0 || startRetry.BaseDelay < 0 || startRetry.MaxDelay < 0 {
			return Result{}, invalid("StartRetryFor returned invalid retry bounds")
		}
		if startRetry.Admit == nil {
			return Result{}, invalid("StartRetryFor returned retry policy without admission")
		}
		if startRetry.Prepare != nil || startRetry.ResolveAmbiguous != nil {
			return Result{}, invalid("StartRetryFor returned transaction-coordinator callbacks")
		}
		copy := *startRetry
		startRetry = &copy
	}
	if startRetry == nil {
		selection, err = req.Start.Resolver.ResolveWorkflow(ctx, req.Start)
		if err != nil {
			return Result{}, fmt.Errorf("workflow execute: resolve workflow: %w", err)
		}
		if selection.Plan == nil || selection.WorkflowID == "" {
			return Result{}, invalid("WorkflowResolver returned no workflow id or plan")
		}
		startReq.Resolver = fixedResolver{selection: selection}
		started, err = d.startOnce(ctx, startReq, false, nil, nil)
		if err != nil {
			return Result{}, err
		}
	} else {
		transactionalResolver, ok := req.Start.Resolver.(TransactionalWorkflowResolver)
		if !ok {
			return Result{}, invalid("serializable start requires a transaction-bound WorkflowResolver")
		}
		// Observe the resolver used inside every transaction so the plan that
		// won the authoritative retry is the one used to drain READY work.
		err := transactioncommit.RetryClosure(ctx, *startRetry, func(ctx context.Context) error {
			var err error
			started, err = d.startOnce(ctx, startReq, true, transactionalResolver, &selection)
			return err
		})
		if err != nil {
			if errors.Is(err, transactioncommit.ErrCommitAmbiguous) {
				resolved, resolveErr := d.resolveAmbiguousStart(ctx, startReq, selection)
				if resolveErr == nil {
					return resolved, nil
				}
				return Result{}, fmt.Errorf("%w: start outcome resolution: %v", transactioncommit.ErrCommitAmbiguous, resolveErr)
			}
			return Result{}, err
		}
	}

	result := Result{
		Start: started, InstanceVersion: started.InstanceVersion,
		Frontier: append([]string(nil), started.Frontier...),
	}
	ready := append([]string(nil), started.Frontier...)
	return d.drainReady(ctx, runContext{
		start: startReq, selection: selection, instanceID: started.InstanceID,
		traceID: d.opts.Instrumentation.TraceID(ctx),
	}, result, ready)
}

func (d *Driver) resolveAmbiguousStart(ctx context.Context, req runtime.StartRequest, selection runtime.WorkflowSelection) (Result, error) {
	db, ok := d.opts.DB.(ReadOnlyBeginner)
	if !ok {
		return Result{}, fmt.Errorf("read-only START outcome resolver is unavailable")
	}
	tx, err := db.BeginReadOnly(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := tenancy.WithTenant(ctx, tx, req.TenantID); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return Result{}, fmt.Errorf("%v; rollback read-only START resolution: %w", err, rollbackErr)
		}
		return Result{}, err
	}
	resolution, err := runtime.ResolveStartOutcome(ctx, tx, req, selection)
	if err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return Result{}, fmt.Errorf("%v; rollback read-only START resolution: %w", err, rollbackErr)
		}
		return Result{}, err
	}
	if err := tx.Rollback(ctx); err != nil {
		return Result{}, fmt.Errorf("rollback read-only START resolution: %w", err)
	}
	return Result{
		Status:          StatusResolved,
		StartResolution: &resolution,
		InstanceVersion: resolution.Instance.InstanceVersion,
		Frontier:        append([]string(nil), resolution.Instance.CurrentNodeIDs...),
	}, nil
}

type transactionalResolver struct {
	inner     TransactionalWorkflowResolver
	tx        dbport.Tx
	selection *runtime.WorkflowSelection
}

func (r transactionalResolver) ResolveWorkflow(ctx context.Context, req runtime.StartRequest) (runtime.WorkflowSelection, error) {
	selection, err := r.inner.ResolveWorkflowInTx(ctx, r.tx, req)
	if err == nil {
		*r.selection = selection
	}
	return selection, err
}

func (d *Driver) startOnce(ctx context.Context, req runtime.StartRequest, serializable bool, resolver TransactionalWorkflowResolver, selection *runtime.WorkflowSelection) (runtime.StartReceipt, error) {
	var tx dbport.Tx
	var err error
	if serializable {
		db, ok := d.opts.DB.(SerializableBeginner)
		if !ok {
			return runtime.StartReceipt{}, fmt.Errorf("workflow execute: serializable start requires BeginSerializable")
		}
		tx, err = db.BeginSerializable(ctx)
	} else {
		tx, err = d.opts.DB.Begin(ctx)
	}
	if err != nil {
		return runtime.StartReceipt{}, fmt.Errorf("workflow execute: begin start: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if serializable {
		req.Resolver = transactionalResolver{inner: resolver, tx: tx, selection: selection}
	}
	if err := tenancy.WithTenant(ctx, tx, req.TenantID); err != nil {
		return runtime.StartReceipt{}, err
	}
	if req.Workload == nil && d.opts.Workload != nil {
		gate := *d.opts.Workload
		req.Workload = &gate
	}
	started, err := runtime.Start(ctx, tx, req)
	if err != nil {
		return runtime.StartReceipt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		if !serializable {
			return runtime.StartReceipt{}, fmt.Errorf("workflow execute: commit start: %w", err)
		}
		// SQLSTATE 40001/40P01 is a definite abort and may be retried;
		// every other commit failure is unresolved and must not be replayed.
		var s interface{ SQLState() string }
		if errors.As(err, &s) && (s.SQLState() == "40001" || s.SQLState() == "40P01") {
			return runtime.StartReceipt{}, err
		}
		return runtime.StartReceipt{}, fmt.Errorf("%w: workflow execute: commit start: %v", transactioncommit.ErrCommitAmbiguous, err)
	}
	return started, nil
}

type runContext struct {
	start      runtime.StartRequest
	selection  runtime.WorkflowSelection
	instanceID uuid.UUID
	// traceID is the ambient trace id read off the call's own incoming span
	// context once, at Execute/Resume entry (OBS-023), and threaded onto
	// every StepRequest and runtime.AdvanceRequest this run produces.
	traceID string
	// timerID is set only on a timer resume (OBS-013): the durable timer
	// whose stored causal identity links the advancement's resume span.
	// It is empty on every other path, which advances unlinked.
	timerID uuid.UUID
}

func (d *Driver) drainReady(ctx context.Context, run runContext, result Result, ready []string) (Result, error) {
	sort.Strings(ready)
	max := d.opts.MaxSteps
	if max <= 0 {
		max = 4*len(run.selection.Plan.Nodes) + 8
	}

	for steps := 0; len(ready) > 0; steps++ {
		if steps >= max {
			return Result{}, fmt.Errorf("%w: READY loop exceeded %d steps", ErrNoProgress, max)
		}
		nodeID := ready[0]
		ready = ready[1:]
		node, ok := run.selection.Plan.Node(nodeID)
		if !ok {
			return Result{}, invalid("frontier names node %s absent from resolved plan", nodeID)
		}
		attempt, err := d.prepareReadyAttempt(ctx, run, &result, node)
		if err != nil {
			return Result{}, err
		}
		at := d.opts.Clock().UTC()
		req := StepRequest{
			TenantID: run.start.TenantID, InstanceID: run.instanceID,
			InstanceVersion: result.InstanceVersion, Attempt: attempt,
			Node: node, Plan: run.selection.Plan, Proposal: run.start.Proposal,
			CorrelationID: run.start.CorrelationID, RecordedAt: at,
			TraceID: run.traceID,
		}
		inputs, err := d.stepInputs(ctx, run, req, attempt)
		if err != nil {
			return Result{}, err
		}

		advanced, created, evidenceIDs, timers, err := d.advanceOnce(ctx, run, result.InstanceVersion, at, attempt, inputs)
		if err != nil {
			return Result{}, err
		}
		result.Advances = append(result.Advances, advanced)
		result.WorkItems = append(result.WorkItems, created...)
		result.Timers = append(result.Timers, timers...)
		result.EvidenceIDs = append(result.EvidenceIDs, evidenceIDs...)
		result.InstanceVersion = advanced.NewInstanceVersion
		result.Frontier = append([]string(nil), advanced.Frontier...)
		if advanced.Complete {
			result.Status = StatusComplete
			return result, nil
		}

		parked := false
		for _, rec := range advanced.Continuations {
			switch rec.Kind {
			case frontier.IntentReady:
				ready = append(ready, rec.TargetNodeID)
			case frontier.IntentWorkItemRequired, frontier.IntentTimerRequired:
				// A durable timer parks this driver exactly as human work
				// does: it has nothing left to run, and a caller with its own
				// clock reading decides when the instance moves again.
				parked = true
			}
		}
		sort.Strings(ready)
		if parked {
			result.Status = StatusParked
			return result, nil
		}
	}

	return Result{}, fmt.Errorf("%w: instance %s is not terminal and has no READY continuation or WorkItem", ErrNoProgress, run.instanceID)
}

// prepareReadyAttempt closes the retry gap between frontier's READY intent
// and a driver's next invocation. A failed attempt is persisted as RETRYING;
// the READY continuation is the instruction to materialize the next durable
// attempt before the handler runs. Without that materialization the driver
// would repeatedly execute attempt 1, so frontier retry accounting would
// never reach the node's retry budget (PROMO-009 fault path).
func (d *Driver) prepareReadyAttempt(
	ctx context.Context, run runContext, result *Result, node workflow.CompiledNode,
) (int, error) {
	if node.Type != workflow.StepObserve || node.Retry == nil || node.Retry.MaxAttempts <= 1 {
		return 1, nil
	}
	tx, err := d.opts.DB.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("workflow execute: begin retry preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, run.start.TenantID); err != nil {
		return 0, err
	}
	rows, err := (runtime.Store{}).LoadNodeExecutions(ctx, tx, run.start.TenantID, run.instanceID)
	if err != nil {
		return 0, err
	}
	latest := runtime.NodeExecution{}
	for _, row := range rows {
		if row.NodeID == node.ID && row.Attempt > latest.Attempt {
			latest = row
		}
	}
	if latest.Attempt == 0 {
		return 0, invalid("ready node %s has no durable execution", node.ID)
	}
	attempt := latest.Attempt
	if latest.Status == runtime.NodeRetrying {
		attempt++
		created := runtime.NewNodeExecution(run.start.TenantID, run.instanceID, node.ID, attempt, node.Type, runtime.NodeReady)
		_, nextVersion, err := (runtime.Store{}).RecordNodeExecution(ctx, tx, created, result.InstanceVersion)
		if err != nil {
			return 0, fmt.Errorf("workflow execute: prepare retry attempt %d for %s: %w", attempt, node.ID, err)
		}
		result.InstanceVersion = nextVersion
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("workflow execute: commit retry preparation for %s: %w", node.ID, err)
	}
	return attempt, nil
}

// stepInputs runs one READY node and returns the advancement inputs for it.
// A node the configured runner does not claim through
// [TransactionalStepRunner] runs here, before the advance transaction opens,
// and its outcome is fed in as a constant. A claimed node runs inside the
// advance transaction instead, so its domain write and the node's recorded
// outcome commit together or not at all.
func (d *Driver) stepInputs(ctx context.Context, run runContext, req StepRequest, attempt int) (advanceInputsFunc, error) {
	nodeID := req.Node.ID
	check := func(outcome frontier.NodeOutcome) (frontier.NodeOutcome, error) {
		if outcome.NodeID == "" {
			outcome.NodeID = nodeID
		} else if outcome.NodeID != nodeID {
			return frontier.NodeOutcome{}, invalid("StepRunner returned outcome for %s while running %s", outcome.NodeID, nodeID)
		}
		return outcome, nil
	}
	span := func(stepCtx context.Context) (context.Context, Span) {
		return d.opts.Instrumentation.StartNodeSpan(stepCtx, SpanAttributes{
			InstanceID: run.instanceID.String(), NodeID: nodeID, Attempt: attempt,
		})
	}
	end := func(s Span, outcome frontier.NodeOutcome, err error) {
		switch {
		case err != nil:
			s.End(OutcomeFailure, err)
		case outcome.Failed:
			s.End(OutcomeFailure, nil)
		default:
			s.End(OutcomeSuccess, nil)
		}
	}

	if tr, ok := d.opts.Steps.(TransactionalStepRunner); ok && tr.RunsInTransaction(req.Node) {
		return func(txCtx context.Context, ex runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error) {
			nodeCtx, nodeSpan := span(txCtx)
			outcome, refs, err := tr.RunInTx(nodeCtx, ex, req)
			if err == nil {
				outcome, err = check(outcome)
			}
			end(nodeSpan, outcome, err)
			if err != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil, fmt.Errorf("workflow execute: run node %s in transaction: %w", nodeID, err)
			}
			return outcome, refs, nil, nil
		}, nil
	}

	nodeCtx, nodeSpan := span(ctx)
	outcome, refs, err := d.opts.Steps.Run(nodeCtx, req)
	if err != nil {
		end(nodeSpan, outcome, err)
		return nil, fmt.Errorf("workflow execute: run node %s: %w", nodeID, err)
	}
	outcome, err = check(outcome)
	end(nodeSpan, outcome, err)
	if err != nil {
		return nil, err
	}
	return func(context.Context, runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error) {
		return outcome, refs, nil, nil
	}, nil
}

// advanceInputsFunc produces the [frontier.NodeOutcome] and
// [runtime.GovernanceRefs] one [Driver.advanceOnce] call feeds to
// [Driver.advance], from inside the same transaction advanceOnce opens,
// plus the stored causal identity the resume span links back with
// (OBS-013), or nil when there is none to link. [drainReady] supplies a
// trivial constant closure over what [StepRunner.Run] already computed
// outside the transaction; [Driver.Resume] supplies one that loads the
// durable WorkItem through [WorkItemReader] and derives the outcome from
// that stored row (WF-RUN-028); [Driver.ResumeTimer] supplies one that
// loads the durable timer and returns its stored causal identity, which
// never governs the advancement it links.
type advanceInputsFunc func(context.Context, runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error)

func (d *Driver) advanceOnce(
	ctx context.Context,
	run runContext,
	expectedVersion int64,
	at time.Time,
	attempt int,
	inputs advanceInputsFunc,
) (runtime.AdvanceReceipt, []workitem.WorkItem, []string, []TimerHandle, error) {
	// The advancement's own node id is not known until inputs(...) runs
	// inside the transaction below (Resume derives it from the durable
	// WorkItem it loads there), so the OBS-023 advance span opens with only
	// the instance attribute and gains node_id once outcome is known.
	advCtx, advSpan := d.opts.Instrumentation.StartAdvanceSpan(ctx, SpanAttributes{
		InstanceID: run.instanceID.String(),
	})

	tx, err := d.opts.DB.Begin(advCtx)
	if err != nil {
		advSpan.End(OutcomeFailure, err)
		return runtime.AdvanceReceipt{}, nil, nil, nil, fmt.Errorf("workflow execute: begin advance: %w", err)
	}
	defer func() { _ = tx.Rollback(advCtx) }()
	if err := tenancy.WithTenant(advCtx, tx, run.start.TenantID); err != nil {
		advSpan.End(OutcomeFailure, err)
		return runtime.AdvanceReceipt{}, nil, nil, nil, err
	}

	outcome, refs, causal, err := inputs(advCtx, tx)
	if err != nil {
		advSpan.End(OutcomeFailure, err)
		return runtime.AdvanceReceipt{}, nil, nil, nil, err
	}
	// OBS-013: when the inputs came from a drift-checked durable timer row
	// carrying stored causal identity, open the resume span that links this
	// advancement back to the parked trace. The span is a child of the
	// advancement span and wraps the remainder of this call; a nil causal
	// (every non-timer path) or an instrumentation without the extension
	// keeps the historical unlinked behavior exactly.
	resumeOutcome := OutcomeSuccess
	if causal != nil {
		if starter, ok := d.opts.Instrumentation.(ResumeSpanStarter); ok {
			var resumeSpan Span
			advCtx, resumeSpan = starter.StartResumeSpan(advCtx, ResumeSpanRequest{
				InstanceID: run.instanceID.String(),
				NodeID:     outcome.NodeID,
				Attempt:    attempt,
				TimerID:    run.timerID.String(),
				Causal:     causal,
				At:         at,
			})
			defer func() {
				if err != nil {
					resumeOutcome = OutcomeFailure
				}
				resumeSpan.End(resumeOutcome, err)
			}()
		}
	}
	// A node the plan routed back to (a re-approval returning to its gate)
	// is on a later attempt than the caller can know before the outcome is
	// loaded, so the advancement addresses the highest recorded attempt of
	// the outcome's node rather than the caller's default. Only a plan that
	// declares a cycle can re-enter a node, so only such a plan pays the
	// lookup; every other plan's node is on attempt 1 by construction.
	if len(run.selection.Plan.Limits.DeclaredCycles) > 0 {
		executions, err := (runtime.Store{}).LoadNodeExecutions(advCtx, tx, run.start.TenantID, run.instanceID)
		if err != nil {
			advSpan.End(OutcomeFailure, err)
			return runtime.AdvanceReceipt{}, nil, nil, nil, err
		}
		if open := highestAttempt(executions, outcome.NodeID); open > attempt {
			attempt = open
		}
	}
	if node, ok := run.selection.Plan.Node(outcome.NodeID); ok {
		outcome = exhaustedObservationRoute(run.selection.Plan, node, outcome, attempt)
	}

	if d.opts.Currency != nil {
		verdict, cerr := d.opts.Currency.Check(advCtx, tx, CurrencyCheckRequest{
			TenantID: run.start.TenantID, InstanceID: run.instanceID,
			Proposal: run.start.Proposal, CheckedAt: at,
		})
		if cerr != nil {
			advSpan.End(OutcomeFailure, cerr)
			return runtime.AdvanceReceipt{}, nil, nil, nil, cerr
		}
		if verdict.Blocked {
			if err := blockInstance(advCtx, tx, run.start.TenantID, run.instanceID, verdict); err != nil {
				advSpan.End(OutcomeFailure, err)
				return runtime.AdvanceReceipt{}, nil, nil, nil, err
			}
			if err := tx.Commit(advCtx); err != nil {
				advSpan.End(OutcomeFailure, err)
				return runtime.AdvanceReceipt{}, nil, nil, nil, fmt.Errorf("workflow execute: commit currency block: %w", err)
			}
			blockedErr := fmt.Errorf("%w: %s (%s)",
				ErrCurrencyBlocked, verdict.Reason, strings.Join(verdict.Explanation, "; "))
			advSpan.End(OutcomeDenied, blockedErr)
			return runtime.AdvanceReceipt{}, nil, nil, nil, blockedErr
		}
	}

	sink := &continuationSink{
		tx: tx, durable: runtime.ContinuationStore{},
		factory: d.opts.WorkItems, timers: d.opts.Timers, terminal: d.opts.Terminal,
		repair: d.opts.Repair,
		plan:   run.selection.Plan,
		guard:  d.opts.Guard, policy: d.opts.Retention,
		workflowID: run.selection.WorkflowID, planDigest: run.selection.Plan.Digest(),
		proposal: run.start.Proposal, cellID: run.start.CellID,
		correlationID: run.start.CorrelationID,
		startKey:      run.start.StartIdempotencyKey,
		subjectRefs:   append([]string(nil), run.start.BusinessSubjectRefs...),
		// WF-RUN-030: the END node's own outcome, so a COMPLETE intent's
		// terminal write never has to discard it.
		endNodeID: outcome.NodeID, endOutputDigest: outcome.OutputDigest,
		// OBS-023/OBS-024: the terminal write this sink may perform opens
		// its own span and records its own evidence entry.
		instrumentation: d.opts.Instrumentation, evidence: d.opts.Evidence,
	}
	advReq := runtime.AdvanceRequest{
		TenantID: run.start.TenantID, InstanceID: run.instanceID,
		ExpectedInstanceVersion: expectedVersion, Attempt: attempt,
		Plan: run.selection.Plan, Outcome: outcome, Refs: refs,
		RecordedAt: at, Sink: sink, TraceID: run.traceID,
	}
	if causalSpan, ok := advSpan.(CausalSpan); ok {
		nodeExecutionID := runtime.NodeExecutionID(run.start.TenantID, run.instanceID, outcome.NodeID, attempt).String()
		advReq.Causal = causalSpan.CausalMetadata(CausalIdentity{
			CorrelationID: run.start.CorrelationID, CausationID: nodeExecutionID,
			LogicalOperationID: run.instanceID.String(), AttemptID: nodeExecutionID,
			ExpiresAt: at.Add(24 * time.Hour),
		})
	}
	var advanced runtime.AdvanceReceipt
	if d.opts.Fence != nil {
		// WF-RUN-002: the fence is checked against the durable lease before
		// the advancement reads any workflow state, so a superseded holder
		// never reaches the sink and therefore never dispatches an effect.
		fence := *d.opts.Fence
		fence.At = at
		advanced, err = runtime.AdvanceFenced(advCtx, tx, runtime.FencedAdvanceRequest{
			Fence: fence, Verifier: d.opts.FenceVerifier, Request: advReq,
		})
		if err != nil && runtime.CodeOf(err) == runtime.CodeFenceRefused {
			// Both %w: the caller classifies this as ErrFenceRefused and can
			// still read the verifier's own LEASE_LOST or FENCE_STALE off the
			// same error.
			err = fmt.Errorf("%w: %w", ErrFenceRefused, err)
		}
	} else {
		advanced, err = d.advance(advCtx, tx, advReq)
	}
	if err != nil {
		advSpan.End(OutcomeFailure, err)
		return runtime.AdvanceReceipt{}, nil, nil, nil, err
	}
	if commitErr := tx.Commit(advCtx); commitErr != nil {
		advSpan.End(OutcomeFailure, commitErr)
		// The commit error binds to the if scope, so publish it through
		// the function-scoped err the deferred resume-span End reads;
		// otherwise the span would close FAILURE with a nil error.
		resumeOutcome = OutcomeFailure
		err = fmt.Errorf("workflow execute: commit advance of %s: %w", outcome.NodeID, commitErr)
		return runtime.AdvanceReceipt{}, nil, nil, nil, err
	}
	advOutcome := OutcomeSuccess
	if !advanced.Complete && len(advanced.Continuations) > 0 {
		for _, rec := range advanced.Continuations {
			if rec.Kind == frontier.IntentWorkItemRequired || rec.Kind == frontier.IntentTimerRequired {
				advOutcome = OutcomeParked
				break
			}
		}
	}
	resumeOutcome = advOutcome
	advSpan.End(advOutcome, nil)
	return advanced,
		append([]workitem.WorkItem(nil), sink.created...),
		append([]string(nil), sink.evidenceIDs...),
		append([]TimerHandle(nil), sink.timersCreated...),
		nil
}

// exhaustedObservationRoute binds the observe-specific retry contract to the
// frontier's ordinary outcome routing. The frontier failure branch predates
// Observe.RetryExhaustionRoute and only knows FailureRoute; promotion OBSERVE
// nodes intentionally declare their repair target through the observe field.
// Once the durable attempt reaches its budget, route through the explicit edge
// to that target so the handler cannot loop or fail with NO_FAILURE_ROUTE.
func exhaustedObservationRoute(
	plan *workflow.CompiledWorkflow,
	node workflow.CompiledNode,
	outcome frontier.NodeOutcome,
	attempt int,
) frontier.NodeOutcome {
	if !outcome.Failed || node.Type != workflow.StepObserve || node.Observe == nil || node.Retry == nil ||
		node.Observe.RetryExhaustionRoute == "" || attempt < int(node.Retry.MaxAttempts) {
		return outcome
	}
	for _, edge := range plan.Edges {
		if edge.From == node.ID && edge.To == node.Observe.RetryExhaustionRoute {
			outcome.Failed = false
			outcome.Outcome = workflow.Outcome(edge.RouteKey)
			return outcome
		}
	}
	return outcome
}

type fixedResolver struct{ selection runtime.WorkflowSelection }

func (r fixedResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return r.selection, nil
}

var _ runtime.WorkflowResolver = fixedResolver{}
