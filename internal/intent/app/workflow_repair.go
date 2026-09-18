package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/repairrecord"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	operationrepair "github.com/monstercameron/human-capital-management-suite/internal/operations/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// RepairEffectPort, RepairObservationPort and RepairReconciliationPort are the
// three external-system seams a RepairPlan execution needs, re-exported so a
// composition root names them without importing the workflow engine.
//
// None of them has a production adapter in this repository yet: the
// connectivity plane (internal/connectivity) is read-only, so nothing here can
// drive a corrective payroll or IAM mutation, read back its result, or compare
// it against intended and canonical values. Rather than compose a fake, a cell
// that supplies none of them gets the refusing defaults below, and a repair
// submitted to it is denied with a reason that says exactly what is missing.
type (
	RepairEffectPort         = execute.RepairEffectPort
	RepairObservationPort    = execute.RepairObservationPort
	RepairReconciliationPort = execute.RepairReconciliationPort
)

// composeWorkflowRepair builds WF-RUN-016's governed RepairPlan execution over
// the execution database.
//
// It is the same governed door composeWorkflowControl builds for the four
// workflow controls: the operator's authority is their current JIT grant in
// the durable trust store (a grant narrowed to the repair plan through
// workflowcontrol.RepairApprovalField carries the second approval DATABASE_REPAIR
// demands), the revalidation is dry-run first so its simulation is sealed into
// the receipt, and the gateway journals the receipt. What it adds is the
// durable idempotency record: the claim, the accepted redrive and the settled
// verdict are appended to workflow_repair_execution_record (migrations/00311)
// in their own committed transactions, so a cell that restarts between the
// corrective effect and its reconciliation never drives that effect again.
//
// It returns a nil controller, never an error, when the cell has no execution
// database or tenant mapping -- the same contract composeWorkflowControl has.
//
// The authority resolver is returned alongside the controller so a surface
// that only wants to say whether a viewer could open this door asks the
// identical resolver the door itself consults (UXLIVE-006). A second,
// separately built resolver would be a second opinion about authority, which
// is the one thing a governed door must not have.
func composeWorkflowRepair(
	db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time,
	recorder WorkflowRecorder, journal operator.Journal, effect RepairEffectPort,
	observation RepairObservationPort, reconciliation RepairReconciliationPort,
) (*workflowcontrol.RepairController, workflowcontrol.AuthorityResolver, error) {
	if db == nil || tenantUUID == nil {
		return nil, nil, nil
	}
	ids := func(t values.TenantId) (uuid.UUID, error) {
		id := tenantUUID(t)
		if id == uuid.Nil {
			return uuid.Nil, fmt.Errorf("%w: tenant %s has no storage identity", workflowcontrol.ErrInvalidCommand, t)
		}
		return id, nil
	}
	if effect == nil {
		effect = unavailableRepairEffect{}
	}
	if observation == nil {
		observation = unavailableRepairObservation{}
	}
	if reconciliation == nil {
		reconciliation = unverifiableRepair{}
	}
	executor, err := execute.NewRepairExecutor(execute.RepairExecutionOptions{
		// The admission fence is derived from the immutable plan, so it is the
		// same value on every cell and after every restart; what makes the
		// "never redrive twice" promise durable is the record below, not this
		// in-process fence table.
		Admission: operationrepair.NewMemoryStore(),
		Effect:    effect, Observation: observation, Reconciliation: reconciliation,
		Records: PostgresRepairRecords(db),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("app: compose repair executor: %w", err)
	}
	if journal == nil {
		// The same in-process default composeWorkflowControl takes. It is the
		// receipt journal, not the repair's idempotency: a restart forgets the
		// receipts either way, and what it must not forget -- that the
		// corrective effect already ran -- is the durable record above. A
		// composition holding a durable operator journal should pass it here
		// and to the controls, so both doors share one receipt chronology.
		journal = operator.NewMemoryJournal()
	}
	authority := workflowcontrol.JITAuthority{Grants: truststore.New(db), TenantIDs: ids, Clock: now}
	controller, err := workflowcontrol.NewRepairController(journal, executor, authority, now,
		workflowcontrol.WithRepairPreflightSimulation(), workflowcontrol.WithRepairRecorder(recorder))
	if err != nil {
		return nil, nil, fmt.Errorf("app: compose workflow repair: %w", err)
	}
	return controller, authority, nil
}

// repairRecords is the PostgreSQL-backed [execute.RepairIdempotencyStore].
//
// Each call runs in its own committed transaction on purpose. The claim has to
// be durable before the corrective effect is invoked; if it shared the
// caller's transaction it would vanish with a rollback, and the next attempt
// would redrive an external mutation the provider may already hold.
type repairRecords struct{ db dbport.Beginner }

// PostgresRepairRecords adapts the execution database to the workflow engine's
// repair idempotency port.
func PostgresRepairRecords(db dbport.Beginner) execute.RepairIdempotencyStore {
	return repairRecords{db: db}
}

// LoadRepairRecords implements [execute.RepairIdempotencyStore].
func (r repairRecords) LoadRepairRecords(ctx context.Context, tenantID uuid.UUID, fenceKey string) ([]execute.RepairRecord, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("app: begin repair record read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, fmt.Errorf("app: scope repair record read: %w", err)
	}
	stored, err := repairrecord.Load(ctx, tx, tenantID, fenceKey)
	if err != nil {
		return nil, err
	}
	out := make([]execute.RepairRecord, 0, len(stored))
	for _, row := range stored {
		out = append(out, execute.RepairRecord{
			TenantID: row.TenantID, FenceKey: row.FenceKey, Stage: execute.RepairRecordStage(row.Stage),
			FenceID: row.FenceID, PlanDigest: row.PlanDigest, OriginalSemanticKey: row.OriginalSemanticKey,
			FailedEffectKey: row.FailedEffectKey, Status: execute.RepairStatus(row.Status),
			Executed: row.Executed, ConsistencyState: row.ConsistencyState,
			EffectRef: row.EffectRef, EffectResultRef: row.EffectResultRef,
			ObservationState: row.ObservationState, ObservationDigest: row.ObservationDigest,
			ObservationComplete: row.ObservationComplete, ReconciliationStatus: row.ReconciliationStatus,
			ReconciliationRoute: row.ReconciliationRoute, RecordedAt: row.RecordedAt,
		})
	}
	return out, nil
}

// AppendRepairRecord implements [execute.RepairIdempotencyStore]. The write
// commits on its own: nothing the caller does afterwards may erase the fact
// that this attempt reached the effect boundary.
func (r repairRecords) AppendRepairRecord(ctx context.Context, record execute.RepairRecord) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("app: begin repair record write: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := tenancy.WithTenant(ctx, tx, record.TenantID); err != nil {
		return false, fmt.Errorf("app: scope repair record write: %w", err)
	}
	claimed, err := repairrecord.Append(ctx, tx, repairrecord.Record{
		TenantID: record.TenantID, FenceKey: record.FenceKey, Stage: repairrecord.Stage(record.Stage),
		FenceID: record.FenceID, PlanDigest: record.PlanDigest, OriginalSemanticKey: record.OriginalSemanticKey,
		FailedEffectKey: record.FailedEffectKey, Status: string(record.Status),
		Executed: record.Executed, ConsistencyState: record.ConsistencyState,
		EffectRef: record.EffectRef, EffectResultRef: record.EffectResultRef,
		ObservationState: record.ObservationState, ObservationDigest: record.ObservationDigest,
		ObservationComplete: record.ObservationComplete, ReconciliationStatus: record.ReconciliationStatus,
		ReconciliationRoute: record.ReconciliationRoute, RecordedAt: record.RecordedAt,
	})
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("app: commit repair record: %w", err)
	}
	committed = true
	return claimed, nil
}

// unavailableRepairEffect is what a cell with no corrective connector adapter
// composes. It refuses rather than pretending a redrive happened: a repair
// that reports success it did not perform is worse than one that cannot run.
type unavailableRepairEffect struct{}

// ErrRepairEffectUnavailable reports that this cell composes no adapter able
// to redrive a failed external effect.
var ErrRepairEffectUnavailable = fmt.Errorf("app: this cell composes no corrective effect adapter; a repair cannot redrive an external system from here")

func (unavailableRepairEffect) ExecuteRepairEffect(context.Context, execute.RepairEffectRequest) (execute.RepairEffectResult, error) {
	return execute.RepairEffectResult{}, ErrRepairEffectUnavailable
}

// unavailableRepairObservation is the matching read-back seam.
type unavailableRepairObservation struct{}

func (unavailableRepairObservation) ObserveRepair(context.Context, operationrepair.RepairPlan, execute.RepairEffectResult) (execute.RepairObservation, error) {
	return execute.RepairObservation{Observed: false, State: "UNOBSERVED"}, nil
}

// unverifiableRepair is the conservative default verifier. It never returns
// PASS: closing a repair means declaring external consistency restored, and
// this cell holds no comparison of observed against intended and canonical
// values to declare it from. A repair therefore stays open -- which is the
// correct answer -- until a cell composes a real comparer.
type unverifiableRepair struct{}

func (unverifiableRepair) ReconcileRepair(_ context.Context, req execute.RepairReconciliationRequest) (reconcile.CompletionDecision, error) {
	reason := "this cell composes no repair comparer, so external consistency cannot be verified"
	if !req.Observation.Observed {
		reason = "the corrective effect was not observed"
	}
	return reconcile.CompletionDecision{
		Status: reconcile.CompletionUnknown, Terminal: false, Route: reconcile.RouteDegraded,
		NextAction: reconcile.ActionObserve, Reason: reason,
	}, nil
}
