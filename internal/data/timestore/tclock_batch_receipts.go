package timestore

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// WorkflowReceiptContext is the time-store half of replay provenance. Core
// workflow execution evidence is read by its own database adapter.
type WorkflowReceiptContext struct {
	TenantID       string
	DeviceID       string
	DeviceSequence int64
	ObservationID  string
	SessionID      string
	InstanceID     uuid.UUID
	WorkflowID     string
	PlanDigest     string
	StartKey       string
	EventType      string
}

// LookupWorkflowReceiptContext loads durable time-store correlations for an
// accepted receipt. Missing or ambiguous correlations fail closed.
func (s *Store) LookupWorkflowReceiptContext(ctx context.Context, tenant, device string, sequence int64) (WorkflowReceiptContext, bool, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(device) == "" || sequence <= 0 {
		return WorkflowReceiptContext{}, false, ErrInvalid
	}
	var proof WorkflowReceiptContext
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		const query = `
			SELECT r.tenant_id, r.device_id, r.device_sequence, r.observation_id,
			       o.event_type, e.session_id, wr.instance_id, wr.workflow_id,
			       wr.plan_digest, wr.start_key, count(*) OVER ()
			  FROM time_receipt r
			  JOIN time_observation o ON o.tenant_id=r.tenant_id AND o.id=r.observation_id
			  JOIN time_session_event e ON e.tenant_id=o.tenant_id AND e.idempotency_key=o.idempotency_key
			  JOIN time_workflow_session_run wr ON wr.tenant_id=e.tenant_id AND wr.session_id=e.session_id
			 WHERE r.tenant_id=$1 AND r.device_id=$2 AND r.device_sequence=$3
			   AND r.status='ACCEPTED' AND r.observation_id IS NOT NULL`
		var matches int64
		if err := tx.QueryRow(ctx, query, tenant, device, sequence).Scan(
			&proof.TenantID, &proof.DeviceID, &proof.DeviceSequence, &proof.ObservationID,
			&proof.EventType, &proof.SessionID, &proof.InstanceID, &proof.WorkflowID,
			&proof.PlanDigest, &proof.StartKey, &matches,
		); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return nil
			}
			return err
		}
		if matches != 1 || proof.InstanceID == uuid.Nil || proof.SessionID == "" || proof.WorkflowID == "" || proof.PlanDigest == "" || proof.StartKey == "" {
			return errWorkflowReceiptContextAbsent
		}
		return nil
	})
	if errors.Is(err, errWorkflowReceiptContextAbsent) {
		return WorkflowReceiptContext{}, false, nil
	}
	if err != nil {
		return WorkflowReceiptContext{}, false, err
	}
	if proof.InstanceID == uuid.Nil {
		return WorkflowReceiptContext{}, false, nil
	}
	return proof, true, nil
}

var errWorkflowReceiptContextAbsent = errors.New("workflow receipt context absent")
