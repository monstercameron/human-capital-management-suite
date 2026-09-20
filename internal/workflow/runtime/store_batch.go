package runtime

import (
	"context"

	"github.com/google/uuid"
)

// Batched reads for list surfaces (REV-090-02). A page that shows N journeys
// must not issue N LoadInstance and N LoadNodeExecutions round trips; these
// two methods answer the same questions for a whole page in one statement
// each. They are read-only, tenant-scoped exactly like their single-instance
// counterparts, and return nothing (not a refusal) for an identity that is
// absent or belongs to another tenant, so a batch can never disclose that an
// instance it may not see exists.

// LoadInstances reads every listed instance in one statement, keyed by
// instance id. Missing identities are simply absent from the map.
func (Store) LoadInstances(ctx context.Context, ex Executor, tenantID uuid.UUID, instanceIDs []uuid.UUID) (map[uuid.UUID]Instance, error) {
	out := make(map[uuid.UUID]Instance, len(instanceIDs))
	if len(instanceIDs) == 0 {
		return out, nil
	}
	rows, err := ex.Query(ctx,
		`SELECT `+instanceColumns+` FROM workflow_instance
		 WHERE tenant_id = $1 AND instance_id = ANY($2::text[]::uuid[])`,
		tenantID, uuidStrings(instanceIDs))
	if err != nil {
		return nil, wrap(CodeStorageFailed, "", "", err, "read workflow instances")
	}
	defer rows.Close()
	for rows.Next() {
		inst, scanErr := scanInstance(rows)
		if scanErr != nil {
			return nil, wrap(CodeStorageFailed, "", "", scanErr, "scan workflow instance")
		}
		out[inst.InstanceID] = inst
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, "", "", err, "iterate workflow instances")
	}
	return out, nil
}

// LoadNodeExecutionsForInstances reads every recorded attempt of every listed
// instance in one statement. Each instance's slice keeps LoadNodeExecutions'
// order (node id, then attempt), so a projection over the batch is
// byte-identical to one over the single-instance read.
func (Store) LoadNodeExecutionsForInstances(ctx context.Context, ex Executor, tenantID uuid.UUID, instanceIDs []uuid.UUID) (map[uuid.UUID][]NodeExecution, error) {
	out := make(map[uuid.UUID][]NodeExecution, len(instanceIDs))
	if len(instanceIDs) == 0 {
		return out, nil
	}
	rows, err := ex.Query(ctx, `SELECT `+nodeColumns+` FROM workflow_node_execution
		WHERE tenant_id = $1 AND instance_id = ANY($2::text[]::uuid[])
		ORDER BY instance_id, node_id, attempt`,
		tenantID, uuidStrings(instanceIDs))
	if err != nil {
		return nil, wrap(CodeStorageFailed, "", "", err, "read node executions")
	}
	defer rows.Close()
	for rows.Next() {
		n, scanErr := scanNodeExecution(rows)
		if scanErr != nil {
			return nil, wrap(CodeStorageFailed, "", "", scanErr, "scan node execution")
		}
		out[n.InstanceID] = append(out[n.InstanceID], n)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, "", "", err, "iterate node executions")
	}
	return out, nil
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
