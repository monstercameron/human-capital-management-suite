package runtime

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/workload"
)

// WF-RUN-021 refusal codes. A start refused with either writes nothing, so
// the business intent that asked for it stays durable and visible to the
// caller's retry or queue policy.
const (
	// CodeOverloaded: the start asks for more than its limits allow on its
	// own. Retrying the same request will not succeed.
	CodeOverloaded = "OVERLOADED"
	// CodeAdmissionDeferred: the start would fit, but the tenant is at its
	// concurrency limit. The same request may be retried once work finishes.
	CodeAdmissionDeferred = "ADMISSION_DEFERRED"
)

// WorkloadGate is the optional WF-RUN-021 admission check a [StartRequest]
// carries. When present, [Start] resolves limits from Snapshot for the
// start's tenant, workflow and Criticality, and refuses before writing the
// instance when the start's demand exceeds them.
type WorkloadGate struct {
	Snapshot    workload.ControlSnapshot
	Criticality string
	// PayloadBytes is the size of the start's input snapshot.
	PayloadBytes int
	// Observe, when set, receives every verdict -- admitted or refused -- so a
	// composition can record it on its telemetry without this package
	// depending on an exporter.
	Observe func(ctx context.Context, tenantID, workflowID string, verdict workload.Verdict)
}

// liveInstanceStatuses are the statuses that occupy a concurrency slot.
var liveInstanceStatuses = []string{
	string(InstanceCreated), string(InstanceRunning), string(InstanceWaiting),
	string(InstancePauseRequested), string(InstancePaused), string(InstanceCancelling), string(InstanceBlocked),
}

// admitWorkload runs the gate inside the start's own transaction. It takes a
// transaction-scoped advisory lock on (tenant, workflow) before counting, so
// two concurrent starts cannot both observe a free slot and both insert: the
// second waits for the first to commit or roll back and then counts again.
// The instance this start would create or replay is excluded from the count,
// so an idempotent retry of an already-admitted start is never deferred by
// its own row.
func admitWorkload(ctx context.Context, tx Executor, req StartRequest, plan *workflow.CompiledWorkflow, workflowID string, instanceID uuid.UUID) error {
	gate := req.Workload
	if gate == nil {
		return nil
	}
	resolved, err := gate.Snapshot.Resolve(req.TenantID.String(), workflowID, gate.Criticality)
	if err != nil {
		return wrap(CodeInvalidRecord, instanceID.String(), "", err, "resolve workload limits")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 21021))`, req.TenantID.String()+"|"+workflowID); err != nil {
		return wrap(CodeStorageFailed, instanceID.String(), "", err, "serialize workload admission")
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM workflow_instance
		WHERE tenant_id = $1 AND workflow_id = $2 AND runtime_status = ANY($3) AND instance_id <> $4`,
		req.TenantID, workflowID, liveInstanceStatuses, instanceID).Scan(&active); err != nil {
		return wrap(CodeStorageFailed, instanceID.String(), "", err, "count live instances for workload admission")
	}
	demand, err := workload.DemandFor(plan, gate.PayloadBytes, active)
	if err != nil {
		return wrap(CodeInvalidRecord, instanceID.String(), "", err, "derive workload demand")
	}
	verdict := workload.Evaluate(resolved, demand)
	if gate.Observe != nil {
		gate.Observe(ctx, req.TenantID.String(), workflowID, verdict)
	}
	switch verdict.Outcome {
	case workload.OutcomeOverloaded:
		return refuse(CodeOverloaded, instanceID.String(), "", "%s (limits %s from %s)", verdict.Reason(), verdict.SnapshotVersion, verdict.Source)
	case workload.OutcomeAdmissionDeferred:
		return refuse(CodeAdmissionDeferred, instanceID.String(), "", "%s (limits %s from %s)", verdict.Reason(), verdict.SnapshotVersion, verdict.Source)
	}
	return nil
}
