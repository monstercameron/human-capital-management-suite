package timeclockstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrMissingPunchSchema indicates that the durable request projection needed
// by the governed workflow has not been installed. Returning this error is
// deliberate: a partial legacy row must never be presented as an authoritative
// request with fabricated session or workflow lineage.
var ErrMissingPunchSchema = errors.New("timeclockstore: missing-punch workflow projection schema is not installed")

var _ clockservice.MissingPunchSessionReader = Adapter{}
var _ clockservice.MissingPunchObservationReader = Adapter{}
var _ clockservice.MissingPunchReviewReader = Adapter{}

// GetMissingPunchSession loads the exact tenant-scoped session and derives
// exception facts from its persisted payload. It does not infer a missing-out
// exception from the session status or from a client-supplied timestamp.
func (a Adapter) GetMissingPunchSession(ctx context.Context, tenant, id string) (clockservice.MissingPunchSession, error) {
	if a.Store == nil {
		return clockservice.MissingPunchSession{}, clockservice.ErrUnavailable
	}
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(id) == "" {
		return clockservice.MissingPunchSession{}, timestore.ErrInvalid
	}
	var result clockservice.MissingPunchSession
	err := a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var payload []byte
		var status string
		if err := tx.QueryRow(ctx, `SELECT tenant_id,id,worker_ref,status,revision,payload FROM time_session WHERE tenant_id=$1 AND id=$2`, tenant, id).
			Scan(&result.TenantID, &result.ID, &result.WorkerRef, &status, &result.Revision, &payload); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return timestore.ErrNotFound
			}
			return err
		}
		result.Status = status
		_, result.PeriodClosed, _ = sessionMissingOutFacts(payload)
		var missingOutEvents int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM time_session_event WHERE tenant_id=$1 AND session_id=$2 AND (kind='MISSING_OUT' OR payload->>'terminal'='TIME_SESSION_MISSING_OUT')`, tenant, id).Scan(&missingOutEvents); err != nil {
			return err
		}
		result.MissingOut = missingOutEvents > 0
		var instanceID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT instance_id FROM time_workflow_session_run WHERE tenant_id=$1 AND session_id=$2`, tenant, id).Scan(&instanceID); err == nil {
			result.OriginalWorkflowInstanceRef = instanceID.String()
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		return nil
	})
	if err != nil {
		return clockservice.MissingPunchSession{}, err
	}
	return result, nil
}

// GetMissingPunchObservation loads immutable original punch evidence by its
// tenant-scoped identity. The time schema only stores accepted observations,
// so Accepted is true after the row is found.
func (a Adapter) GetMissingPunchObservation(ctx context.Context, tenant, id string) (clockservice.MissingPunchObservation, error) {
	if a.Store == nil {
		return clockservice.MissingPunchObservation{}, clockservice.ErrUnavailable
	}
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(id) == "" {
		return clockservice.MissingPunchObservation{}, timestore.ErrInvalid
	}
	var row struct {
		ID, TenantID, WorkerRef, DeviceRef, EventType, Timezone, Digest string
		OccurredAt, ReceivedAt                                          time.Time
		Payload                                                         []byte
	}
	var result clockservice.MissingPunchObservation
	err := a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,worker_ref,device_ref,event_type,timezone,digest,occurred_at,received_at,payload FROM time_observation WHERE tenant_id=$1 AND id=$2`, tenant, id).
			Scan(&row.ID, &row.TenantID, &row.WorkerRef, &row.DeviceRef, &row.EventType, &row.Timezone, &row.Digest, &row.OccurredAt, &row.ReceivedAt, &row.Payload); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return timestore.ErrNotFound
			}
			return err
		}
		result = clockservice.MissingPunchObservation{ID: row.ID, TenantID: row.TenantID, Observation: clockdomain.TimeObservation{
			Accepted: true, Tenant: values.TenantId(row.TenantID), EventType: clockdomain.EventType(row.EventType), DeviceRef: row.DeviceRef,
			WorkerRef: row.WorkerRef, OccurredAt: row.OccurredAt.UTC(), RecordedAt: row.ReceivedAt.UTC(), Timezone: row.Timezone, Digest: row.Digest,
		}}
		return nil
	})
	if err != nil {
		return clockservice.MissingPunchObservation{}, err
	}
	return result, nil
}

// GetMissingPunchRequest reads the bound request projection and its latest
// immutable workflow version. Legacy missed_punch_request rows are ignored.
func (a Adapter) GetMissingPunchRequest(ctx context.Context, tenant, id string) (clockservice.MissingPunchRecord, error) {
	if a.Store == nil {
		return clockservice.MissingPunchRecord{}, clockservice.ErrUnavailable
	}
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(id) == "" {
		return clockservice.MissingPunchRecord{}, timestore.ErrInvalid
	}
	var record clockservice.MissingPunchRecord
	err := a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var originalWorkflow, workflowID string
		var workflowAttempt int
		var workflowVersion int64
		var expectedSessionRevision int64
		var updatedAt time.Time
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,worker_ref,session_id,original_observation_id,claimed_out_at,reason,requested_by,status,request_revision,expected_session_revision,period_closed,original_workflow_instance_ref,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version,created_at,updated_at FROM missed_punch_workflow_request WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&record.ID, &record.TenantID, &record.WorkerRef, &record.SessionID, &record.OriginalObservationID, &record.ClaimedOutAt, &record.Reason, &record.RequestedBy, &record.Decision, &record.Revision, &expectedSessionRevision, &record.PeriodClosed, &originalWorkflow, &workflowID, &record.WorkflowTraceID, &record.WorkflowNodeID, &record.WorkflowPlanDigest, &workflowAttempt, &workflowVersion, &record.CreatedAt, &updatedAt); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return timestore.ErrNotFound
			}
			return err
		}
		record.DecidedAt = updatedAt
		record.WorkflowInstanceID, record.OriginalWorkflowInstanceRef = workflowID, originalWorkflow
		record.ExpectedSessionRevision = uint64(expectedSessionRevision)
		record.WorkflowAttempt, record.WorkflowInstanceVersion = workflowAttempt, workflowVersion
		var action, latestActor, reopen string
		if err := tx.QueryRow(ctx, `SELECT action,actor_ref,reopen_ref,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version,created_at FROM missed_punch_workflow_version WHERE tenant_id=$1 AND request_id=$2 ORDER BY revision DESC LIMIT 1`, tenant, id).Scan(&action, &latestActor, &reopen, &record.WorkflowInstanceID, &record.WorkflowTraceID, &record.WorkflowNodeID, &record.WorkflowPlanDigest, &record.WorkflowAttempt, &record.WorkflowInstanceVersion, &record.DecidedAt); err != nil {
			return err
		}
		if action != "REQUEST" {
			record.DecidedBy, record.ReopenRef = latestActor, reopen
		}
		var proofInstance uuid.UUID
		var proofAt time.Time
		if err := tx.QueryRow(ctx, `SELECT workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version,completed_at FROM missed_punch_workflow_execution_proof WHERE tenant_id=$1 AND request_id=$2 ORDER BY proof_revision DESC LIMIT 1`, tenant, id).Scan(&proofInstance, &record.WorkflowTraceID, &record.WorkflowNodeID, &record.WorkflowPlanDigest, &record.WorkflowAttempt, &record.WorkflowInstanceVersion, &proofAt); err == nil {
			record.WorkflowInstanceID = proofInstance.String()
			record.DecidedAt = proofAt
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		return nil
	})
	if err != nil {
		return clockservice.MissingPunchRecord{}, err
	}
	return record, nil
}

type persistedSessionException struct {
	Kind string `json:"kind"`
}

type persistedSessionPayload struct {
	OpenExceptions []persistedSessionException `json:"open_exceptions"`
	PeriodClosed   bool                        `json:"period_closed"`
	WorkflowRef    string                      `json:"original_workflow_instance_ref"`
}

func sessionMissingOutFacts(payload []byte) (bool, bool, string) {
	var state persistedSessionPayload
	if json.Unmarshal(payload, &state) != nil {
		return false, false, ""
	}
	missing := false
	for _, exception := range state.OpenExceptions {
		if exception.Kind == "MISSING_OUT" {
			missing = true
			break
		}
	}
	return missing, state.PeriodClosed, state.WorkflowRef
}
