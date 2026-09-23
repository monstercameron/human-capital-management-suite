package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-RUN-010: CancelIntent on an intent whose execution is bound to a live
// workflow instance is the governed workflow cancellation, not an intent-state
// flip. A served promotion's intent does not itself move to EXECUTING while
// its workflow runs (ExecuteIntent consumes only terminal results), so the
// bound instance is found by the intent's correlation whenever the kernel
// would otherwise cancel the intent cleanly. The instance is decided and
// acted on durably by internal/workflow/cancellation first, and the intent's
// disposition is derived from that recorded decision:
//
//	CANCELLED             -> the kernel cancels the intent (point CLEAN)
//	CANNOT_CANCEL         -> TOO_LATE; the intent is untouched
//	COMPENSATION_REQUIRED -> CANCELLATION_PENDING; the intent is untouched and
//	                         the instance is CANCELLING until the recorded
//	                         compensation obligation is discharged
//	REPAIR_REQUIRED       -> REPAIR_REQUIRED: through the kernel (point
//	                         PARTIAL_EFFECT, repair ref = the decision) for an
//	                         EXECUTING intent, otherwise recorded as evidence
//	                         with the intent untouched
//
// An intent with no bound instance keeps the kernel's own answer.

// BoundCancellation asks for the governed cancellation of the workflow
// instance an intent's execution is bound to.
type BoundCancellation struct {
	Tenant        values.TenantId
	IntentID      string
	CorrelationID string
	Reason        string
	RequestedBy   string
	RecordedAt    time.Time
}

// BoundCancellationVerdict is the recorded governed decision for that
// instance. Bound is false when no workflow instance carries the intent's
// correlation.
type BoundCancellationVerdict struct {
	Bound      bool
	InstanceID uuid.UUID
	DecisionID uuid.UUID
	Decision   workflow.CancellationDecision
	// TerminalStatus is set instead of Decision when the instance had already
	// ended before this request with no cancellation decision of its own.
	TerminalStatus runtime.InstanceStatus
}

// WorkflowCancellation is CancelIntent's governed-cancellation port.
type WorkflowCancellation interface {
	CancelBound(ctx context.Context, req BoundCancellation) (BoundCancellationVerdict, error)
}

// AdmissionRelease frees everything a cleanly cancelled intent held: the
// admission reservation (PROMOUX-002) and the proposal's compensation-pool
// budget hold (WF-RUN-034). It is the one release capability every cancel
// path shares (WF-REV-004).
type AdmissionRelease func(ctx context.Context, tenant values.TenantId, intentID string, at time.Time) error

// releasePromotionAdmission frees a cleanly cancelled promotion's admission
// window and budget hold in one tenant-scoped transaction. Both releases are
// idempotent, so a replayed cancellation or a second path reaching the same
// intent frees the hold exactly once: the repeat call writes nothing. An
// intent that never held anything releases nothing and reports success.
func releasePromotionAdmission(ctx context.Context, db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, tenant values.TenantId, intentID string, at time.Time) error {
	id, err := uuid.Parse(intentID)
	if err != nil {
		return fmt.Errorf("app: release admission: intent id: %w", err)
	}
	tenantID := tenantUUID(tenant)
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if err := promotionguard.Release(ctx, tx, tenantID, id, at.UTC()); err != nil {
		return err
	}
	// WF-REV-004: a promotion cancelled through CancelIntent rather than the
	// journey screen kept its compensation budget reserved, because only the
	// journey's best-effort release freed the hold. The kernel path frees it
	// here, through the same capability every cancel path shares.
	if _, err := promotionbudget.ReleaseForIntent(ctx, tx, tenantID, id, at.UTC()); err != nil {
		return fmt.Errorf("app: release the cancelled promotion's budget hold: %w", err)
	}
	return tx.Commit(ctx)
}

// governedDisposition maps a recorded verdict onto CancelIntent's kernel
// input. When decided is non-empty the disposition is final without asking
// the kernel and the intent is not mutated; otherwise point and repairRef are
// what the kernel decides with.
func (v BoundCancellationVerdict) governedDisposition(executing bool) (point intent.CancellationPoint, repairRef string, decided intent.CancellationDisposition) {
	repair := func(ref string) (intent.CancellationPoint, string, intent.CancellationDisposition) {
		if executing {
			return intent.CancellationPointPartialEffect, ref, ""
		}
		return intent.CancellationPointUnknown, "", intent.DispositionRepairRequired
	}
	switch v.Decision {
	case workflow.Cancelled:
		return intent.CancellationPointClean, "", ""
	case workflow.RepairRequired:
		return repair("workflow-cancellation:" + v.DecisionID.String())
	case workflow.CannotCancel:
		return intent.CancellationPointUnknown, "", intent.DispositionTooLate
	case workflow.CompensationRequired:
		return intent.CancellationPointMidFlight, "", intent.DispositionCancellationPending
	}
	switch v.TerminalStatus {
	case runtime.InstanceCancelled:
		return intent.CancellationPointClean, "", ""
	case runtime.InstanceCompleted:
		return intent.CancellationPointUnknown, "", intent.DispositionTooLate
	case runtime.InstanceRepairRequired:
		return repair("workflow-instance:" + v.InstanceID.String())
	}
	// A quarantined or superseded instance cannot be decided here.
	return intent.CancellationPointUnknown, "", intent.DispositionCancellationPending
}

// executionWorkflowCancellation is the production [WorkflowCancellation]
// over the execution database and the promotion plans this cell runs.
type executionWorkflowCancellation struct {
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
	plans      workflowcontrol.PlanSet
}

// CancelBound implements [WorkflowCancellation].
func (w executionWorkflowCancellation) CancelBound(ctx context.Context, req BoundCancellation) (BoundCancellationVerdict, error) {
	tenantID := w.tenantUUID(req.Tenant)
	if tenantID == uuid.Nil {
		return BoundCancellationVerdict{}, fmt.Errorf("app: cancel bound workflow: tenant %s has no storage identity", req.Tenant)
	}
	if req.CorrelationID == "" {
		return BoundCancellationVerdict{}, nil
	}
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return BoundCancellationVerdict{}, fmt.Errorf("app: begin bound workflow cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return BoundCancellationVerdict{}, err
	}
	// The root instance of the execution: a child carries the same
	// correlation but is decided through its parent.
	var instanceID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT i.instance_id FROM workflow_instance i
		WHERE i.tenant_id = $1 AND i.correlation_id = $2
		  AND NOT EXISTS (SELECT 1 FROM workflow_child_link l
		                  WHERE l.tenant_id = i.tenant_id AND l.child_instance_id = i.instance_id)
		ORDER BY i.created_at DESC, i.instance_id DESC
		LIMIT 1`, tenantID, req.CorrelationID).Scan(&instanceID)
	if errors.Is(err, dbport.ErrNoRows) {
		return BoundCancellationVerdict{}, nil
	}
	if err != nil {
		return BoundCancellationVerdict{}, fmt.Errorf("app: find the workflow bound to intent %s: %w", req.IntentID, err)
	}
	inst, err := (runtime.Store{}).LoadInstance(ctx, tx, tenantID, instanceID)
	if err != nil {
		return BoundCancellationVerdict{}, err
	}
	plan, err := w.plans.ResolvePlan(ctx, tx, inst)
	if err != nil {
		return BoundCancellationVerdict{}, err
	}
	out, err := cancellation.Decide(ctx, tx, cancellation.Request{
		TenantID: tenantID, InstanceID: instanceID, Plan: plan, Plans: w.plans,
		Reason: req.Reason, RequestedBy: req.RequestedBy, RecordedAt: req.RecordedAt,
	})
	verdict := BoundCancellationVerdict{Bound: true, InstanceID: instanceID}
	if errors.Is(err, cancellation.ErrTerminal) {
		verdict.TerminalStatus = inst.RuntimeStatus
		return verdict, nil
	}
	if err != nil {
		return BoundCancellationVerdict{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BoundCancellationVerdict{}, fmt.Errorf("app: commit bound workflow cancellation: %w", err)
	}
	verdict.Decision, verdict.DecisionID = out.Decision, out.DecisionID
	return verdict, nil
}

// composeWorkflowCancellation builds CancelIntent's governed cancellation and
// admission release over the execution database. Both are nil when the cell
// has no execution database or tenant mapping.
func composeWorkflowCancellation(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) (WorkflowCancellation, AdmissionRelease, error) {
	if db == nil || tenantUUID == nil {
		return nil, nil, nil
	}
	plan, err := promotionexec.Compile()
	if err != nil {
		return nil, nil, fmt.Errorf("app: compile promotion plan for cancellation: %w", err)
	}
	simulation, err := promotionexec.CompileSimulation()
	if err != nil {
		return nil, nil, fmt.Errorf("app: compile promotion simulation plan for cancellation: %w", err)
	}
	// Instances pinned to the frozen 1.0.0 plan stay cancellable and
	// controllable after 1.1.0 is activated.
	frozen, err := promotionexec.CompileV1_0()
	if err != nil {
		return nil, nil, fmt.Errorf("app: compile the frozen promotion plan for cancellation: %w", err)
	}
	frozenSimulation, err := promotionexec.CompileSimulationV1_0()
	if err != nil {
		return nil, nil, fmt.Errorf("app: compile the frozen promotion simulation plan for cancellation: %w", err)
	}
	release := func(ctx context.Context, tenant values.TenantId, intentID string, at time.Time) error {
		return releasePromotionAdmission(ctx, db, tenantUUID, tenant, intentID, at)
	}
	return executionWorkflowCancellation{db: db, tenantUUID: tenantUUID, plans: workflowcontrol.NewPlanSet(plan, simulation, frozen, frozenSimulation)}, release, nil
}
