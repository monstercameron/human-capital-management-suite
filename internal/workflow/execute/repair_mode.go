package execute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

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
	// RepairSeparationRequired is what a plan's own author gets when they
	// submit it for execution and separation of duties applies. It is not a
	// failure of the repair; it is a statement that a different person has to
	// run it.
	RepairSeparationRequired RepairStatus = "SEPARATION_OF_DUTIES_REQUIRED"
	// RepairIndeterminate reports that a previous attempt under this fence
	// reached the corrective effect and never recorded what came of it -- the
	// shape a crash between the effect and its acknowledgement leaves behind.
	// The effect is never repeated on that evidence: the finding is diagnosed
	// again and a new plan issued.
	RepairIndeterminate RepairStatus = "INDETERMINATE"
)

// RepairExecutionRequest is the immutable input to one repair-mode run.
type RepairExecutionRequest struct {
	// TenantID scopes the durable idempotency record. A repair without one is
	// refused: an unscoped record is either invisible under row-level security
	// or, worse, shared.
	TenantID uuid.UUID
	Plan     operationrepair.RepairPlan
	Current  operationrepair.CurrentEvidence
	Now      time.Time
	// Actor is the operator executing the repair.
	Actor string
	// Author is who authored or approved the plan. Where the plan requires
	// approval, an Author equal to Actor -- or an unrecorded Author, which
	// cannot be proven distinct -- refuses the execution.
	Author string
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
	// Records is the durable idempotency record. It is required: without it a
	// restart between the corrective effect and its reconciliation would
	// redrive an external mutation the provider already accepted.
	Records RepairIdempotencyStore
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
//
// It also never writes runtime instance state. REPAIR_REQUIRED has no outgoing
// edge in internal/workflow/runtime's instance state machine -- "a repair is a
// new instance, not a resurrected one" -- so neither a failed repair nor a
// completed one clears it here. What a repair produces is evidence, a
// consistency verdict and the durable record below.
type RepairExecutor struct {
	opts RepairExecutionOptions
}

func NewRepairExecutor(opts RepairExecutionOptions) (*RepairExecutor, error) {
	if opts.Admission == nil {
		return nil, fmt.Errorf("workflow execute: repair admission is required")
	}
	if opts.Effect == nil || opts.Observation == nil || opts.Reconciliation == nil {
		return nil, fmt.Errorf("workflow execute: repair effect, observation and reconciliation ports are required")
	}
	if opts.Records == nil {
		return nil, fmt.Errorf("workflow execute: a durable repair idempotency record is required")
	}
	return &RepairExecutor{opts: opts}, nil
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
	if req.TenantID == uuid.Nil {
		return RepairExecutionResult{}, fmt.Errorf("workflow execute: repair tenant is required")
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
		// Separation of duties: a plan that needs approval is not one its own
		// author may execute. An unrecorded author is refused too -- it cannot
		// be proven distinct, and failing open here would make the rule
		// optional for anyone who omits a field.
		if strings.TrimSpace(req.Author) == "" || strings.EqualFold(strings.TrimSpace(req.Author), strings.TrimSpace(req.Actor)) {
			result.Status = RepairSeparationRequired
			addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "SEPARATION_REFUSED", PlanDigest: req.Plan.Digest, EffectKey: req.Plan.FailedEffectKey, Detail: "the repair author may not execute their own approved plan", RecordedAt: req.Now})
			return result, nil
		}
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

	// The durable record, not this process's memory, decides whether the
	// corrective effect has already run. Everything below reads it before the
	// effect port is reachable.
	records, err := e.opts.Records.LoadRepairRecords(ctx, req.TenantID, admission.Fence.FenceKey)
	if err != nil {
		return result, fmt.Errorf("workflow execute: load repair record: %w", err)
	}
	if settled, ok := findRepairStage(records, RepairStageSettled); ok {
		replayed := result
		applyRepairRecord(&replayed, settled)
		addEvidence(&replayed, e.opts.Evidence, ctx, RepairEvidence{Stage: "REPLAYED", PlanDigest: req.Plan.Digest, FenceID: settled.FenceID, EffectKey: settled.FailedEffectKey, Detail: "this repair fence is already settled durably", RecordedAt: req.Now})
		return replayed, nil
	}
	executedRecord, resuming := findRepairStage(records, RepairStageExecuted)
	if _, claimed := findRepairStage(records, RepairStageClaimed); claimed && !resuming {
		// An earlier attempt reached the effect boundary and never recorded
		// what came of it. Re-running is the one thing that must not happen:
		// the provider may already hold the mutation.
		result.Status, result.ConsistencyState = RepairIndeterminate, "UNKNOWN"
		addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "INDETERMINATE", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: req.Plan.FailedEffectKey, Detail: "a prior attempt claimed this fence and recorded no outcome; diagnose again rather than redrive", RecordedAt: req.Now})
		return result, nil
	}
	addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "REVALIDATED", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: req.Plan.FailedEffectKey, Detail: admission.Reason, RecordedAt: req.Now})

	step, ok := failedStep(req.Plan)
	if !ok {
		return result, fmt.Errorf("workflow execute: failed effect %q is not in repair plan", req.Plan.FailedEffectKey)
	}

	var effect RepairEffectResult
	if resuming {
		// The effect was accepted before; only observe and verify are left.
		effect = executedRecord.effectOf()
		result.Executed = true
		addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "EXECUTED", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: effect.EffectKey, Detail: "durably recorded redrive resumed without calling the provider again", RecordedAt: req.Now})
	} else {
		claimed, claimErr := e.opts.Records.AppendRepairRecord(ctx, repairRecordOf(req, admission.Fence, RepairStageClaimed, RepairUnknown, "UNKNOWN", false, RepairEffectResult{}, RepairObservation{}, reconcile.CompletionDecision{}))
		if claimErr != nil {
			return result, fmt.Errorf("workflow execute: claim repair fence: %w", claimErr)
		}
		if !claimed {
			// Another attempt won the claim between the read above and this
			// write. Nothing was redriven here, and nothing may be.
			result.Status, result.ConsistencyState = RepairBlocked, "UNKNOWN"
			addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "CLAIM_REFUSED", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: req.Plan.FailedEffectKey, Detail: "a concurrent attempt holds this repair fence", RecordedAt: req.Now})
			return result, nil
		}
		effect, err = e.opts.Effect.ExecuteRepairEffect(ctx, RepairEffectRequest{Plan: req.Plan, Step: step, Fence: admission.Fence, OriginalSemanticKey: req.Plan.OriginalSemanticKey, Mode: RepairExecutionMode, Actor: req.Actor})
		if err != nil {
			// The claim stands deliberately. Whether the provider accepted the
			// mutation before failing is unknown, so the next attempt is told
			// INDETERMINATE rather than allowed to repeat it.
			result.Status = RepairFailed
			return result, err
		}
		if effect.EffectKey != req.Plan.FailedEffectKey || !effect.Accepted {
			result.Status = RepairFailed
			return result, fmt.Errorf("workflow execute: repair effect was not accepted")
		}
		result.Executed = true
		if _, err := e.opts.Records.AppendRepairRecord(ctx, repairRecordOf(req, admission.Fence, RepairStageExecuted, RepairReconciliationWait, "DEGRADED", true, effect, RepairObservation{}, reconcile.CompletionDecision{})); err != nil {
			return result, fmt.Errorf("workflow execute: record repair redrive: %w", err)
		}
		addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "EXECUTED", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: effect.EffectKey, Detail: "only the failed effect was redriven", RecordedAt: req.Now})
	}

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
	if _, err := e.opts.Records.AppendRepairRecord(ctx, repairRecordOf(req, admission.Fence, RepairStageSettled, RepairCompleted, "CONSISTENT", true, effect, observation, decision)); err != nil {
		return result, fmt.Errorf("workflow execute: settle repair record: %w", err)
	}
	addEvidence(&result, e.opts.Evidence, ctx, RepairEvidence{Stage: "VERIFIED", PlanDigest: req.Plan.Digest, FenceID: admission.Fence.FenceID, EffectKey: effect.EffectKey, Detail: "RECON-002 passed with CONSISTENT", RecordedAt: req.Now})
	return result, nil
}

// repairRecordOf builds one durable record from the request, the repair fence
// and whatever of the effect, observation and reconciliation exists at that
// stage. Only identities, states and digests travel; no business payload does.
func repairRecordOf(
	req RepairExecutionRequest, fence operationrepair.Fence, stage RepairRecordStage,
	status RepairStatus, consistency string, executed bool,
	effect RepairEffectResult, observation RepairObservation, decision reconcile.CompletionDecision,
) RepairRecord {
	return RepairRecord{
		TenantID: req.TenantID, FenceKey: fence.FenceKey, Stage: stage, FenceID: fence.FenceID,
		PlanDigest: req.Plan.Digest, OriginalSemanticKey: req.Plan.OriginalSemanticKey,
		FailedEffectKey: req.Plan.FailedEffectKey, Status: status, Executed: executed,
		ConsistencyState: consistency, EffectRef: effect.EffectRef, EffectResultRef: effect.ResultRef,
		ObservationState: observation.State, ObservationDigest: observation.Digest,
		ObservationComplete: observation.Complete, ReconciliationStatus: string(decision.Status),
		ReconciliationRoute: string(decision.Route), RecordedAt: req.Now.UTC(),
	}
}

// applyRepairRecord restores onto result exactly what a settled record pinned,
// so a replay answers with the original decision rather than re-deriving one.
func applyRepairRecord(result *RepairExecutionResult, record RepairRecord) {
	result.Status = record.Status
	result.Executed = record.Executed
	result.ConsistencyState = record.ConsistencyState
	result.Observation = record.observationOf()
	result.Reconciliation = reconcile.CompletionDecision{
		Status:   reconcile.CompletionStatus(record.ReconciliationStatus),
		Terminal: record.ReconciliationStatus != "",
		Route:    reconcile.Route(record.ReconciliationRoute),
		Reason:   "replayed from the durable repair execution record",
	}
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
