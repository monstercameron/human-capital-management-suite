package application

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// PostgresClockWorkflowEvidenceReader reads runtime evidence using a
// tenant-scoped transaction. Effect references are the authoritative link
// between the workflow commit node and the exact observation/session; a
// matching instance or plan alone is insufficient.
type PostgresClockWorkflowEvidenceReader struct {
	DB dbport.Beginner
}

// LoadClockWorkflowNodeEvidence returns the latest successful commit_punch
// attempt linked to observationID, or found=false when no such proof exists.
func (r PostgresClockWorkflowEvidenceReader) LoadPunchNodeEvidence(ctx context.Context, tenantID, instanceID uuid.UUID, observationID string) (clockservice.PunchNodeEvidence, bool, error) {
	if r.DB == nil || tenantID == uuid.Nil || instanceID == uuid.Nil || strings.TrimSpace(observationID) == "" {
		return clockservice.PunchNodeEvidence{}, false, errors.New("clock workflow evidence: tenant, instance and observation are required")
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return clockservice.PunchNodeEvidence{}, false, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return clockservice.PunchNodeEvidence{}, false, err
	}
	const query = `
		SELECT n.node_id, n.attempt, n.status, COALESCE(n.output_artifact_ref, ''),
		       COALESCE(n.trace_id, ''), n.effect_refs, i.compiled_plan_hash,
		       i.instance_version
		FROM workflow_node_execution n
		JOIN workflow_instance i ON i.tenant_id = n.tenant_id AND i.instance_id = n.instance_id
		WHERE n.tenant_id = $1 AND n.instance_id = $2 AND n.node_id = 'commit_punch'
		  AND n.status = 'SUCCEEDED'
		  AND ('time_observation:' || $3) = ANY(n.effect_refs)
		ORDER BY n.attempt DESC
		LIMIT 1`
	var evidence clockservice.PunchNodeEvidence
	var refs []string
	if err := tx.QueryRow(ctx, query, tenantID, instanceID, observationID).Scan(
		&evidence.NodeID, &evidence.Attempt, &evidence.CompletedState,
		&evidence.OutputDigest, &evidence.TraceID, &refs, &evidence.PlanDigest,
		&evidence.InstanceVersion,
	); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return clockservice.PunchNodeEvidence{}, false, nil
		}
		return clockservice.PunchNodeEvidence{}, false, err
	}
	evidence.TenantID = tenantID
	evidence.InstanceID = instanceID
	evidence.ObservationID = observationID
	for _, ref := range refs {
		if strings.HasPrefix(ref, "time_session:") {
			evidence.SessionID = strings.TrimPrefix(ref, "time_session:")
			break
		}
	}
	return evidence, true, nil
}

var _ clockservice.PunchNodeEvidenceReader = PostgresClockWorkflowEvidenceReader{}
