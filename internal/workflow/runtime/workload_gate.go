package runtime

import (
	"context"
	"encoding/json"

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

// effectiveGate fills the admission inputs a composition did not set from the
// start itself: criticality is the pinned plan's declared risk class, and the
// payload is the canonical size of the bound proposal revision (the start's
// input snapshot). A composition-supplied value always wins. Without this a
// shared gate built once at composition carries no criticality and a zero
// payload, so criticality-scoped rules never match and payload limits are
// inert.
func effectiveGate(gate WorkloadGate, req StartRequest, plan *workflow.CompiledWorkflow) WorkloadGate {
	if gate.Criticality == "" && plan != nil {
		gate.Criticality = plan.RiskClass
	}
	if gate.PayloadBytes == 0 {
		// A revision that cannot be canonically encoded (an unset instant, for
		// example) is not measurable; it is refused by Start's own proposal
		// and approval checks, so admission must not pre-empt that typed
		// refusal with an encoding error. It is admitted on its other
		// dimensions and never reaches a step.
		if body, err := json.Marshal(req.Proposal.Revision); err == nil {
			gate.PayloadBytes = len(body)
		}
	}
	return gate
}

// admitWorkload runs the gate inside the start's own transaction. It takes a
// transaction-scoped advisory lock on (tenant, workflow) before counting, so
// two concurrent starts cannot both observe a free slot and both insert: the
// second waits for the first to commit or roll back and then counts again.
// The instance this start would create or replay is excluded from the count,
// so an idempotent retry of an already-admitted start is never deferred by
// its own row.
func admitWorkload(ctx context.Context, tx Executor, req StartRequest, plan *workflow.CompiledWorkflow, workflowID string, instanceID uuid.UUID) error {
	if req.Workload == nil {
		return nil
	}
	gate := effectiveGate(*req.Workload, req, plan)
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
