package projectstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// MigrateTaskTx writes one planner-validated task migration inside the
// workflow publication transaction. The caller must hold the project row
// lock and must have validated the migration against this task's current
// revision. Event payloads contain status and field IDs only, never field
// values.
func MigrateTaskTx(ctx context.Context, tx dbport.Tx, tenantID, projectID, taskID string, expectedRevision int64, statusID string, fields json.RawMessage, configVersion int64, actorID string, changedFieldIDs []string) error {
	if tx == nil || tenantID == "" || projectID == "" || taskID == "" || expectedRevision <= 0 || statusID == "" || len(fields) == 0 || configVersion <= 0 || actorID == "" {
		return ErrInvalidRecord
	}
	var priorStatus string
	if err := tx.QueryRow(ctx, `SELECT status_id FROM project_task WHERE tenant_id=$1 AND project_id=$2 AND id=$3 AND revision=$4 AND archived=false FOR UPDATE`, tenantID, projectID, taskID, expectedRevision).Scan(&priorStatus); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrRevisionConflict
		}
		return err
	}
	var nextRevision int64
	err := tx.QueryRow(ctx, `UPDATE project_task SET status_id=$1,fields_json=$2::jsonb,revision=revision+1,updated_at=now()
		WHERE tenant_id=$3 AND project_id=$4 AND id=$5 AND revision=$6 AND archived=false
		RETURNING revision`, statusID, fields, tenantID, projectID, taskID, expectedRevision).Scan(&nextRevision)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrRevisionConflict
		}
		return err
	}
	return appendProjectEvent(ctx, tx, tenantID, projectID, taskID, actorID, "HUMAN", "task.workflow_migrated", expectedRevision, nextRevision, configVersion, map[string]any{
		"fromStatus": priorStatus,
		"toStatus":   statusID,
		"fieldIDs":   append([]string(nil), changedFieldIDs...),
	})
}
