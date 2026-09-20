package workitem

import (
	"context"

	"github.com/google/uuid"
)

// Batched reads for list surfaces (REV-090-02). They answer ListForInstance
// and LoadTransitions for a whole page in one statement each, with the same
// tenant scoping, the same per-key ordering and the same silence about rows
// the caller may not see.

// ListForInstances reads every work item of every listed workflow instance,
// keyed by instance id. Each slice keeps ListForInstance's order (node id,
// then creation time).
func (Store) ListForInstances(ctx context.Context, ex Executor, tenantID uuid.UUID, instanceIDs []uuid.UUID) (map[uuid.UUID][]WorkItem, error) {
	out := make(map[uuid.UUID][]WorkItem, len(instanceIDs))
	if len(instanceIDs) == 0 {
		return out, nil
	}
	rows, err := ex.Query(ctx,
		`SELECT `+workItemColumns+` FROM work_item
		 WHERE tenant_id = $1 AND workflow_instance_id = ANY($2::text[]::uuid[])
		 ORDER BY workflow_instance_id, node_id, created_at`,
		tenantID, batchIDStrings(instanceIDs))
	if err != nil {
		return nil, wrap(CodeStorageFailed, "", err, "list work items for instances")
	}
	defer rows.Close()
	for rows.Next() {
		item, scanErr := scanWorkItem(rows)
		if scanErr != nil {
			return nil, wrap(CodeStorageFailed, "", scanErr, "scan work item")
		}
		out[item.WorkflowInstanceID] = append(out[item.WorkflowInstanceID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, "", err, "iterate work items")
	}
	return out, nil
}

// LoadTransitionsForItems reads every recorded transition of every listed
// work item, keyed by work item id, each slice ordered by item_version as
// LoadTransitions orders it.
func (Store) LoadTransitionsForItems(ctx context.Context, ex Executor, tenantID uuid.UUID, workItemIDs []uuid.UUID) (map[uuid.UUID][]TransitionRecord, error) {
	out := make(map[uuid.UUID][]TransitionRecord, len(workItemIDs))
	if len(workItemIDs) == 0 {
		return out, nil
	}
	rows, err := ex.Query(ctx,
		`SELECT `+transitionColumns+` FROM work_item_transition
		 WHERE tenant_id = $1 AND work_item_id = ANY($2::text[]::uuid[])
		 ORDER BY work_item_id, item_version`,
		tenantID, batchIDStrings(workItemIDs))
	if err != nil {
		return nil, wrap(CodeStorageFailed, "", err, "list work item transitions")
	}
	defer rows.Close()
	for rows.Next() {
		t, scanErr := scanTransition(rows)
		if scanErr != nil {
			return nil, wrap(CodeStorageFailed, "", scanErr, "scan work item transition")
		}
		out[t.WorkItemID] = append(out[t.WorkItemID], t)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, "", err, "iterate work item transitions")
	}
	return out, nil
}

func batchIDStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
