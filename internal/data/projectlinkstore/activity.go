package projectlinkstore

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
)

func appendTaskLinkEvent(ctx context.Context, tx dbport.Tx, tenantID, projectID, taskID, actorID, eventType string, priorRevision, newRevision int64, value map[string]string) error {
	var taskSequence int64
	if err := tx.QueryRow(ctx, `UPDATE project_task SET activity_sequence=GREATEST(activity_sequence,
 (SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id=$3 AND event_type LIKE 'task.%' AND task_sequence=0) +
 (SELECT count(*) FROM project_task_comment_activity WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND task_sequence=0)) + 1
 WHERE tenant_id=$1 AND project_id=$2 AND id=$3 RETURNING activity_sequence`, tenantID, projectID, taskID).Scan(&taskSequence); err != nil {
		return err
	}
	event, err := projectstore.AppendOutboxEventTx(ctx, tx, projectstore.AppendOutboxEvent{TenantID: tenantID, ProjectID: projectID, AggregateID: taskID, EventType: eventType, SourceRevision: newRevision, Classification: "INTERNAL", Value: value})
	if err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO project_activity(tenant_id,id,project_id,aggregate_id,actor_id,origin,event_type,prior_revision,new_revision,config_version,classification,payload,task_sequence) VALUES($1,$2,$3,$4,$5,'HUMAN',$6,$7,$8,0,'INTERNAL',$9::jsonb,$10)`, tenantID, event.EventID, projectID, taskID, actorID, eventType, priorRevision, newRevision, payload, taskSequence)
	return err
}
