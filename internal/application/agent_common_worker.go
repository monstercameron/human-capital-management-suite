package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/resources"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

var ErrCommonAgentWorker = errors.New("application: common agent execution unavailable")

// CommonAgentOutput names an actually persisted, validated source-owner artifact.
// Delivery retains this same reference; a receipt cannot replace the output.
type CommonAgentOutput struct{ Ref, Digest string }

// CommonAgentExecutionSource owns the native bounded input and protected output.
// Deliver must be idempotent by admission.ID and reload its current authority.
type CommonAgentExecutionSource interface {
	BuildCommonAgentModelWork(context.Context, agentrun.Record, runstate.Run) (AgentModelExecutorRequest, error)
	ValidateAndPersistCommonAgentOutput(context.Context, agentrun.Record, runstate.Run, agentmodel.ModelResult) (CommonAgentOutput, error)
	DeliverCommonAgentOutput(context.Context, agentrun.Record, runstate.Run, CommonAgentOutput) error
}

type CommonAgentModelExecutor interface {
	Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error)
}

// CommonAgentContinuationSource reloads an applied capability's retained
// result. Recovery cannot implicitly repeat a previously applied effect.
type CommonAgentContinuationSource interface {
	BuildCommonAgentContinuation(context.Context, agentrun.Record, runstate.Run) (AgentModelExecutorRequest, error)
}

// CommonAgentToolSource permits only source-owned pinned capabilities. An error
// after BeginEffect is ambiguous until ReconcileCommonAgentEffect observes it.
type CommonAgentToolSource interface {
	ExecuteCommonAgentTool(context.Context, agentrun.Record, runstate.Run, agentmodel.ToolProposal, string) ([]byte, CommonAgentOutput, error)
	ReconcileCommonAgentEffect(context.Context, agentrun.Record, runstate.Run, runstate.Effect) (runstate.EffectStatus, CommonAgentOutput, error)
}

type CommonAgentWorkerConfig struct {
	Runtime   *CommonAgentRuntime
	Model     CommonAgentModelExecutor
	Sources   map[agentrun.SourceKind]CommonAgentExecutionSource
	Resources *AgentResourceRuntime
	WorkerID  string
	LeaseTTL  time.Duration
	Now       func() time.Time
}

type CommonAgentWorker struct{ cfg CommonAgentWorkerConfig }

func NewCommonAgentWorker(cfg CommonAgentWorkerConfig) (*CommonAgentWorker, error) {
	if cfg.Runtime == nil || cfg.Resources == nil || isNilPersonaOutputPort(cfg.Model) || len(cfg.Sources) == 0 || !required(cfg.WorkerID) || cfg.LeaseTTL <= 0 || cfg.Now == nil {
		return nil, ErrCommonAgentWorker
	}
	sources := make(map[agentrun.SourceKind]CommonAgentExecutionSource, len(cfg.Sources))
	for kind, source := range cfg.Sources {
		if isNilPersonaOutputPort(source) || (kind != agentrun.SourceSchedule && kind != agentrun.SourceWorkflow && kind != agentrun.SourceEvent && kind != agentrun.SourceAPI) {
			return nil, ErrCommonAgentWorker
		}
		sources[kind] = source
	}
	cfg.Sources = sources
	return &CommonAgentWorker{cfg: cfg}, nil
}

// Execute reconstructs one accepted run from durable records. A retained
// validation checkpoint resumes idempotent delivery without another model call.
func (w *CommonAgentWorker) Execute(ctx context.Context, tenant, id string) (runstate.Run, error) {
	if w == nil || ctx == nil {
		return runstate.Run{}, ErrCommonAgentWorker
	}
	record, err := w.cfg.Runtime.GetAdmission(ctx, tenant, id)
	if err != nil || record.Decision != agentrun.DecisionAccepted || record.Request.Persona != nil {
		return runstate.Run{}, errors.Join(ErrCommonAgentWorker, err)
	}
	source := w.cfg.Sources[record.Request.Source.Kind]
	if source == nil {
		return runstate.Run{}, ErrCommonAgentWorker
	}
	run, err := w.cfg.Runtime.GetRun(ctx, tenant, id)
	if err != nil {
		return run, err
	}
	if commonAgentWorkerTerminal(run.State) {
		return run, nil
	}
	state, err := w.cfg.Runtime.ExecutionService(ctx, tenant)
	if err != nil {
		return run, err
	}
	if !run.Deadline.After(w.cfg.Now().UTC()) || (run.State == runstate.StateRunning && run.Lease != nil && !run.Lease.Until.After(w.cfg.Now().UTC())) {
		run, err = w.cfg.Runtime.Recover(ctx, tenant, id)
		if err != nil || commonAgentWorkerTerminal(run.State) {
			return run, err
		}
	}
	if run.State == runstate.StateReconciling {
		run, err = w.reconcile(ctx, record, run, state, source)
		if err != nil || run.State != runstate.StateReady {
			return run, err
		}
	}
	if run.State != runstate.StateReady {
		return run, runstate.ErrLease
	}
	run, err = w.cfg.Runtime.Claim(ctx, tenant, id, w.cfg.WorkerID, minDuration(w.cfg.LeaseTTL, run.Deadline.Sub(w.cfg.Now().UTC())))
	if err != nil {
		current, readErr := w.cfg.Runtime.GetRun(ctx, tenant, id)
		return current, errors.Join(err, readErr)
	}
	// Model and owner calls cannot outlive the fenced worker lease.
	workCtx, cancel := context.WithDeadline(ctx, minTime(run.Deadline, run.Lease.Until))
	defer cancel()
	resource, err := w.cfg.Resources.Acquire(workCtx, AgentResourceIdentity{TenantID: tenant, UserID: commonAgentExecutionPrincipal(record.Request), TaskID: id, Lane: resources.LaneAutonomous}, "")
	if err != nil {
		return run, err
	}
	defer resource.Release()
	workCtx = resource.Context(workCtx)
	if output, ok := commonAgentValidatedOutput(run); ok {
		return w.deliver(workCtx, record, run, state, source, output)
	}
	var request AgentModelExecutorRequest
	if len(run.Effects) > 0 {
		continuation, ok := source.(CommonAgentContinuationSource)
		if !ok {
			return w.fail(ctx, run, state, "TOOL_CONTINUATION_UNAVAILABLE", ErrCommonAgentWorker)
		}
		request, err = continuation.BuildCommonAgentContinuation(workCtx, record, run)
	} else {
		request, err = source.BuildCommonAgentModelWork(workCtx, record, run)
	}
	if err != nil {
		return w.fail(ctx, run, state, "CONTEXT_UNAVAILABLE", err)
	}
	if err = commonAgentWorkerRequest(record, run, request); err != nil {
		return w.fail(ctx, run, state, "MODEL_BINDING_INVALID", err)
	}
	request = commonAgentBindModelStep(request, run, 1)
	requestDigest, err := commonAgentWorkerDigest(request.Model)
	if err != nil {
		return w.fail(ctx, run, state, "MODEL_BINDING_INVALID", err)
	}
	run, err = state.Checkpoint(workCtx, run.ID, w.cfg.WorkerID, run.Fence, run.Version, runstate.PhaseContext, 1, record.Request.Context.SnapshotID, record.Request.Context.Digest, w.cfg.Now().UTC())
	if err != nil {
		return run, err
	}
	// The pre-call checkpoint rechecks authority before any inference side effect.
	run, err = state.Checkpoint(workCtx, run.ID, w.cfg.WorkerID, run.Fence, run.Version, runstate.PhaseModelCall, 1, request.StepID, requestDigest, w.cfg.Now().UTC())
	if err != nil {
		return run, err
	}
	modelCtx := withCommonAgentModelEvidence(workCtx, record, run, request)
	result, err := w.cfg.Model.Execute(modelCtx, request)
	if err != nil {
		return w.fail(ctx, run, state, "MODEL_UNAVAILABLE", err)
	}
	if result.Result.Finish == agentmodel.FinishToolCalls {
		run, request, result, err = w.tool(workCtx, record, run, state, source, request, result)
		if err != nil {
			return run, err
		}
	}
	if result.Result.Failure != nil || result.Result.Refusal != nil || result.Result.Finish != agentmodel.FinishComplete || (strings.TrimSpace(result.Result.Text) == "" && !json.Valid(result.Result.Structured)) || len(result.Result.ToolProposals) != 0 {
		return w.fail(ctx, run, state, "MODEL_REFUSED_OR_INCOMPLETE", agentmodel.ErrInvalidModelResult)
	}
	resultDigest, err := commonAgentWorkerDigest(result.Result)
	if err != nil {
		return w.fail(ctx, run, state, "MODEL_RESULT_INVALID", err)
	}
	run, err = state.Checkpoint(workCtx, run.ID, w.cfg.WorkerID, run.Fence, run.Version, runstate.PhaseModelCall, 2, request.StepID, resultDigest, w.cfg.Now().UTC())
	if err != nil {
		return run, err
	}
	if err = w.cfg.Runtime.Recheck(workCtx, tenant, id); err != nil {
		return w.fail(ctx, run, state, "OUTPUT_AUTHORITY_CHANGED", err)
	}
	output, err := source.ValidateAndPersistCommonAgentOutput(workCtx, record, run, result.Result)
	if err != nil || !commonAgentOutputValid(output) {
		return w.fail(ctx, run, state, "OUTPUT_REJECTED", errors.Join(ErrCommonAgentWorker, err))
	}
	run, err = state.Checkpoint(workCtx, run.ID, w.cfg.WorkerID, run.Fence, run.Version, runstate.PhaseValidation, 1, output.Ref, output.Digest, w.cfg.Now().UTC())
	if err != nil {
		return run, err
	}
	return w.deliver(workCtx, record, run, state, source, output)
}

func (w *CommonAgentWorker) deliver(ctx context.Context, record agentrun.Record, run runstate.Run, state *runstate.Service, source CommonAgentExecutionSource, output CommonAgentOutput) (runstate.Run, error) {
	if err := w.cfg.Runtime.Recheck(ctx, run.TenantID, run.ID); err != nil {
		return w.fail(ctx, run, state, "DELIVERY_AUTHORITY_CHANGED", err)
	}
	if err := source.DeliverCommonAgentOutput(ctx, record, run, output); err != nil {
		// Leave RUNNING with the validation reference. Recovery retries this exact
		// source-owned artifact and idempotency key, never generates a new answer.
		return run, err
	}
	return state.Checkpoint(ctx, run.ID, w.cfg.WorkerID, run.Fence, run.Version, runstate.PhaseDelivery, 1, output.Ref, output.Digest, w.cfg.Now().UTC())
}

func (w *CommonAgentWorker) fail(ctx context.Context, run runstate.Run, state *runstate.Service, code string, cause error) (runstate.Run, error) {
	failed, err := state.Fail(ctx, run.ID, w.cfg.WorkerID, code, false, run.Fence, run.Version, w.cfg.Now().UTC())
	if err != nil {
		return run, errors.Join(cause, err)
	}
	return failed, cause
}

// Sweep polls durable accepted work, so dispatch does not depend on an in-memory
// queue surviving the server. Independent failures do not starve later runs.
func (w *CommonAgentWorker) Sweep(ctx context.Context, tenant string, source agentrun.SourceKind, limit int) (int, error) {
	if w == nil || ctx == nil || w.cfg.Sources[source] == nil {
		return 0, ErrCommonAgentWorker
	}
	runs, err := w.cfg.Runtime.Pending(ctx, tenant, source, limit)
	if err != nil {
		return 0, err
	}
	completed := 0
	var failures []error
	for _, run := range runs {
		result, err := w.Execute(ctx, tenant, run.ID)
		if err != nil && !errors.Is(err, runstate.ErrLease) && !errors.Is(err, runstate.ErrConflict) {
			failures = append(failures, fmt.Errorf("common agent run %s: %w", run.ID, err))
		}
		if result.State == runstate.StateCompleted {
			completed++
		}
	}
	return completed, errors.Join(failures...)
}

func commonAgentOutputValid(output CommonAgentOutput) bool {
	return required(output.Ref) && personaRunAuthorityDigest(output.Digest)
}

func commonAgentValidatedOutput(run runstate.Run) (CommonAgentOutput, bool) {
	for i := len(run.Checkpoints) - 1; i >= 0; i-- {
		checkpoint := run.Checkpoints[i]
		if checkpoint.Phase == runstate.PhaseValidation {
			output := CommonAgentOutput{Ref: checkpoint.Ref, Digest: checkpoint.Digest}
			return output, commonAgentOutputValid(output)
		}
	}
	return CommonAgentOutput{}, false
}

func commonAgentWorkerTerminal(state runstate.State) bool {
	return state == runstate.StateCompleted || state == runstate.StateFailed || state == runstate.StateCancelled || state == runstate.StateExpired || state == runstate.StateNeedsRepair
}

func commonAgentWorkerRequest(record agentrun.Record, run runstate.Run, request AgentModelExecutorRequest) error {
	if commonAgentCheckExecutionIdentity(record, run) != nil || request.Task.TenantID != run.TenantID || request.Task.TaskID != run.ID || request.Task.AgentID != run.AgentDigest ||
		request.Outbound.Principal != commonAgentExecutionPrincipal(record.Request) || request.Model.Limits.MaxCostMicros <= 0 || uint64(request.Model.Limits.MaxCostMicros) > record.Request.Budget.MaxCostMicros ||
		request.Model.Limits.MaxInputTokens <= 0 || uint64(request.Model.Limits.MaxInputTokens) > record.Request.Budget.MaxInputTokens || request.Model.Limits.MaxOutputTokens <= 0 || uint64(request.Model.Limits.MaxOutputTokens) > record.Request.Budget.MaxOutputTokens ||
		request.Model.Deadline.After(run.Deadline) {
		return ErrCommonAgentWorker
	}
	return validateExecutorRequest(request)
}

func commonAgentCheckExecutionIdentity(record agentrun.Record, run runstate.Run) error {
	if agentrun.ValidateAdmissionRecord(record) != nil || record.Decision != agentrun.DecisionAccepted || (record.Request.Principal.Mode != agentrun.ModeSponsored && record.Request.Principal.Mode != agentrun.ModeOnBehalfOf) || record.Request.Persona != nil || record.Authority.Principal != record.Request.Principal {
		return ErrCommonAgentWorker
	}
	_, err := commonAgentCheckExecution(run, record)
	return err
}

func commonAgentExecutionPrincipal(request agentrun.Request) string {
	if request.Principal.Mode == agentrun.ModeOnBehalfOf {
		return request.Principal.InvokerID
	}
	return request.Principal.AgentPrincipalID
}
