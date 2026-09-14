package execute

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	operationrepair "github.com/monstercameron/human-capital-management-suite/internal/operations/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// RepairExecutionMode is the workflow mode used by a RepairPlan. It is kept
// separate from normal EXECUTE so a repair cannot accidentally re-enter the
// parent business transaction.
const RepairExecutionMode workflow.ExecutionMode = workflow.ModeRepair

type RepairStatus string

const (
	RepairCompleted          RepairStatus = "COMPLETED"
	RepairNoLongerRequired   RepairStatus = "NO_LONGER_REQUIRED"
	RepairReplanRequired     RepairStatus = "REPLAN_REQUIRED"
	RepairReapprovalRequired RepairStatus = "REAPPROVAL_REQUIRED"
	RepairBlocked            RepairStatus = "BLOCKED"
	RepairUnknown            RepairStatus = "UNKNOWN"
	RepairFailed             RepairStatus = "FAILED"
	RepairReconciliationWait RepairStatus = "RECONCILIATION_REQUIRED"
)

// RepairExecutionRequest is the immutable input to one repair-mode run.
type RepairExecutionRequest struct {
	Plan    operationrepair.RepairPlan
	Current operationrepair.CurrentEvidence
	Now     time.Time
	Actor   string
}

type RepairApprovalRequest struct {
	Plan       operationrepair.RepairPlan
	Simulation operationrepair.Simulation
	Actor      string
	At         time.Time
}

type RepairApprovalResult struct {
	Approved bool
	Digest   string
}

type RepairApprovalPort interface {
	ApproveRepair(context.Context, RepairApprovalRequest) (RepairApprovalResult, error)
}

type RepairEffectRequest struct {
	Plan                operationrepair.RepairPlan
	Step                operationrepair.Step
	Fence               operationrepair.Fence
	OriginalSemanticKey string
	Mode                workflow.ExecutionMode
	Actor               string
}

type RepairEffectResult struct {
	EffectKey string
	EffectRef string
	Accepted  bool
	ResultRef string
}

type RepairEffectPort interface {
	ExecuteRepairEffect(context.Context, RepairEffectRequest) (RepairEffectResult, error)
}

type RepairObservation struct {
	Observed bool
	Complete bool
	Digest   string
	State    string
}

type RepairObservationPort interface {
	ObserveRepair(context.Context, operationrepair.RepairPlan, RepairEffectResult) (RepairObservation, error)
}

type RepairReconciliationRequest struct {
	Plan        operationrepair.RepairPlan
	Effect      RepairEffectResult
	Observation RepairObservation
	Fence       operationrepair.Fence
	EvaluatedAt time.Time
}

type RepairReconciliationPort interface {
	ReconcileRepair(context.Context, RepairReconciliationRequest) (reconcile.CompletionDecision, error)
}

// RepairEvidence is a business-evidence summary, not a software log. Each
// stage is linked to the repair fence and the original effect identity.
type RepairEvidence struct {
	Stage      string
	PlanDigest string
	FenceID    string
	EffectKey  string
	Detail     string
	RecordedAt time.Time
}

type RepairEvidencePort interface {
	RecordRepairEvidence(context.Context, RepairEvidence) error
}

type RepairExecutionOptions struct {
	Admission      operationrepair.Admitter
	Approval       RepairApprovalPort
	Effect         RepairEffectPort
	Observation    RepairObservationPort
	Reconciliation RepairReconciliationPort
	Evidence       RepairEvidencePort
}

// RepairExecutionResult is returned for every typed revalidation outcome.
// Corrective execution is true only after the failed effect was redriven and
// RECON-002 returned a consistent terminal decision.
type RepairExecutionResult struct {
	Mode                workflow.ExecutionMode
	Status              RepairStatus
	PlanDigest          string
	OriginalSemanticKey string
	FailedEffectKey     string
	Fence               operationrepair.Fence
	Executed            bool
	ConsistencyState    string
	Observation         RepairObservation
	Reconciliation      reconcile.CompletionDecision
	Evidence            []RepairEvidence
}

// RepairExecutor implements the diagnose -> simulate -> approve -> execute
// -> observe -> verify sequence. Diagnose is represented by the immutable
// plan supplied by the operations owner; this executor never reruns or
// mutates the parent business transaction.
type RepairExecutor struct {
	opts      RepairExecutionOptions
	mu        sync.Mutex
	completed map[string]RepairExecutionResult
}

func NewRepairExecutor(opts RepairExecutionOptions) (*RepairExecutor, error) {
	if opts.Admission == nil {
		return nil, fmt.Errorf("workflow execute: repair admission is required")
	}
	if opts.Effect == nil || opts.Observation == nil || opts.Reconciliation == nil {
		return nil, fmt.Errorf("workflow execute: repair effect, observation and reconciliation ports are required")
	}
	return &RepairExecutor{opts: opts, completed: make(map[string]RepairExecutionResult)}, nil
}

func (e *RepairExecutor) Execute(ctx context.Context, req RepairExecutionRequest) (ret0 RepairExecutionResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execute.repair", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if ctx == nil {
		return RepairExecutionResult{}, fmt.Errorf("workflow execute: repair context is nil")
	}
	if req.Now.IsZero() {
		return RepairExecutionResult{}, fmt.Errorf("workflow execute: repair evaluation time is required")
	}
	result := RepairExecutionResult{
		Mode: RepairExecutionMode, PlanDigest: req.Plan.Digest,
		OriginalSemanticKey: req.Plan.OriginalSemanticKey, FailedEffectKey: req.Plan.FailedEffectKey,
		ConsistencyState: "DEGRADED",
	}
	addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "DIAGNOSED", PlanDigest: req.Plan.Digest, EffectKey: req.Plan.FailedEffectKey, Detail: "immutable repair plan accepted", RecordedAt: req.Now})

	simulation, err := operationrepair.Simulate(operationrepair.RevalidationRequest{Plan: req.Plan, Current: req.Current, Now: req.Now, Actor: req.Actor})
	if err != nil {
		return result, err
	}
	addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "SIMULATED", PlanDigest: req.Plan.Digest, EffectKey: req.Plan.FailedEffectKey, Detail: simulation.Reason, RecordedAt: req.Now})
	if simulation.Status != operationrepair.StatusReady {
		result.Status = repairStatus(simulation.Status)
		if simulation.Status == operationrepair.StatusNoLongerRequired {
			result.ConsistencyState = "CONSISTENT"
		}
		return result, nil
	}

	if req.Plan.RequiresApproval {
		if e.opts.Approval == nil {
			result.Status = RepairReapprovalRequired
			return result, nil
		}
		approval, approvalErr := e.opts.Approval.ApproveRepair(ctx, RepairApprovalRequest{Plan: req.Plan, Simulation: simulation, Actor: req.Actor, At: req.Now})
		if approvalErr != nil {
			return result, approvalErr
		}
		if !approval.Approved || approval.Digest != req.Plan.ApprovalDigest {
			result.Status = RepairReapprovalRequired
			return result, nil
		}
		addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "APPROVED", PlanDigest: req.Plan.Digest, EffectKey: req.Plan.FailedEffectKey, Detail: "repair approval is bound to the plan digest", RecordedAt: req.Now})
	}

	// This is deliberately after simulation and approval: the last read before
	// the effect is the current-truth revalidation and its own repair fence.
	admission, err := e.opts.Admission.RevalidateAndFence(ctx, operationrepair.RevalidationRequest{Plan: req.Plan, Current: req.Current, Now: req.Now, Actor: req.Actor})
	if err != nil {
		return result, err
	}
	if admission.Status != operationrepair.StatusReady {
		result.Status = repairStatus(admission.Status)
		return result, nil
	}
	result.Fence = admission.Fence
	e.mu.Lock()
	prior, replay := e.completed[admission.Fence.FenceKey]
	e.mu.Unlock()
	if replay {
		return prior, nil
	}
	addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "REVALIDATED", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: req.Plan.FailedEffectKey, Detail: admission.Reason, RecordedAt: req.Now})

	step, ok := failedStep(req.Plan)
	if !ok {
		return result, fmt.Errorf("workflow execute: failed effect %q is not in repair plan", req.Plan.FailedEffectKey)
	}
	effect, err := e.opts.Effect.ExecuteRepairEffect(ctx, RepairEffectRequest{Plan: req.Plan, Step: step, Fence: admission.Fence, OriginalSemanticKey: req.Plan.OriginalSemanticKey, Mode: RepairExecutionMode, Actor: req.Actor})
	if err != nil {
		result.Status = RepairFailed
		return result, err
	}
	if effect.EffectKey != req.Plan.FailedEffectKey || !effect.Accepted {
		result.Status = RepairFailed
		return result, fmt.Errorf("workflow execute: repair effect was not accepted")
	}
	result.Executed = true
	addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "EXECUTED", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: effect.EffectKey, Detail: "only the failed effect was redriven", RecordedAt: req.Now})

	observation, err := e.opts.Observation.ObserveRepair(ctx, req.Plan, effect)
	if err != nil {
		result.Status = RepairReconciliationWait
		return result, err
	}
	result.Observation = observation
	addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "OBSERVED", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: effect.EffectKey, Detail: observation.State, RecordedAt: req.Now})

	decision, err := e.opts.Reconciliation.ReconcileRepair(ctx, RepairReconciliationRequest{Plan: req.Plan, Effect: effect, Observation: observation, Fence: admission.Fence, EvaluatedAt: req.Now})
	if err != nil {
		result.Status = RepairReconciliationWait
		return result, err
	}
	result.Reconciliation = decision
	if decision.Status != reconcile.CompletionPass || decision.Route != reconcile.RouteConsistent {
		result.Status = RepairReconciliationWait
		return result, nil
	}
	result.Status = RepairCompleted
	result.ConsistencyState = "CONSISTENT"
	addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "VERIFIED", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: effect.EffectKey, Detail: "RECON-002 passed with CONSISTENT", RecordedAt: req.Now})
	e.mu.Lock()
	e.completed[admission.Fence.FenceKey] = result
	e.mu.Unlock()
	return result, nil
}

func failedStep(plan operationrepair.RepairPlan) (operationrepair.Step, bool) {
	for _, step := range plan.Steps {
		if step.EffectKey == plan.FailedEffectKey {
			return step, true
		}
	}
	return operationrepair.Step{}, false
}

func repairStatus(status operationrepair.Status) RepairStatus {
	switch status {
	case operationrepair.StatusNoLongerRequired:
		return RepairNoLongerRequired
	case operationrepair.StatusReplanRequired:
		return RepairReplanRequired
	case operationrepair.StatusReapprovalRequired:
		return RepairReapprovalRequired
	case operationrepair.StatusBlocked:
		return RepairBlocked
	default:
		return RepairUnknown
	}
}

func addEvidence(result *RepairExecutionResult, sink RepairEvidencePort, ctx context.Context, evidence RepairEvidence) {
	result.Evidence = append(result.Evidence, evidence)
	if sink != nil {
		// Evidence stays on the result either way; a failed durable write is
		// not fatal to the repair but must be visible as its own failed
		// operation rather than silently discarded.
		_, op := observe.Begin(ctx, "workflow.repair.record_evidence")
		_ = observe.Done(op, sink.RecordRepairEvidence(ctx, evidence))
	}
}

// Explain returns a telemetry-safe repair execution summary.
func ExplainRepair(result RepairExecutionResult) string {
	return fmt.Sprintf("workflow repair mode=%s status=%s plan=%s effect=%s executed=%t consistency=%s evidence=%d", result.Mode, result.Status, result.PlanDigest, result.FailedEffectKey, result.Executed, strings.TrimSpace(string(result.Reconciliation.Route)), len(result.Evidence))
}
