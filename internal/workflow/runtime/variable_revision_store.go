package runtime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

const variableRevisionColumns = `tenant_id, instance_id, revision, variable_name, schema_ref,
	variable_value::text, value_digest, writer_node_id, writer_ref, reason, causation_id,
	previous_revision, prior_digest, revision_digest, written_at`

// AppendVariableRevision is the variable write path over
// migrations/00295_workflow_variable_revision.sql. In the caller's
// transaction it
//
//  1. advances workflow_instance.variable_revision_head from rev.Revision-1
//     to rev.Revision and bumps instance_version, both under the
//     expectedInstanceVersion compare-and-set;
//  2. inserts the append-only revision row; and
//  3. refreshes the workflow_variable current-value projection, fenced on the
//     variable's previous revision.
//
// A stale instance version is [CodeStaleInstance]; a revision that does not
// extend the stored head, or names a previous revision the projection does
// not hold, is [CodeStaleInstance] as well, because both mean another writer
// got there first. It returns the stored revision and the new instance version.
func (s Store) AppendVariableRevision(
	ctx context.Context, ex Executor, rev VariableRevision, expectedInstanceVersion int64,
) (ret0 VariableRevision, ret1 int64, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.append_variable_revision", rev)
	defer func() { observe.DoneWith(obsOp, retErr, ret0, ret1) }()
	id := rev.InstanceID.String()
	if err := rev.Validate(); err != nil {
		return VariableRevision{}, 0, err
	}
	if expectedInstanceVersion < 1 {
		return VariableRevision{}, 0, refuse(CodeInvalidRecord, id, rev.WriterNodeID,
			"expected instance version must be at least 1")
	}

	var version int64
	err := ex.QueryRow(ctx, `
		UPDATE workflow_instance
		SET instance_version = instance_version + 1, variable_revision_head = $4
		WHERE tenant_id = $1 AND instance_id = $2 AND instance_version = $3
			AND variable_revision_head = $4 - 1
		RETURNING instance_version`,
		rev.TenantID, rev.InstanceID, expectedInstanceVersion, rev.Revision).Scan(&version)
	if err != nil {
		if !errors.Is(err, dbport.ErrNoRows) {
			return VariableRevision{}, 0, wrap(CodeStorageFailed, id, rev.WriterNodeID, err, "advance variable revision head")
		}
		current, loadErr := s.LoadInstance(ctx, ex, rev.TenantID, rev.InstanceID)
		if loadErr != nil {
			return VariableRevision{}, 0, loadErr
		}
		if current.InstanceVersion != expectedInstanceVersion {
			return VariableRevision{}, 0, staleError(rev.InstanceID, rev.WriterNodeID, expectedInstanceVersion, current.InstanceVersion)
		}
		return VariableRevision{}, 0, refuse(CodeStaleInstance, id, rev.WriterNodeID,
			"variable revision %d does not extend stored head %d", rev.Revision, current.VariableRevisionHead)
	}

	if _, err := ex.Exec(ctx, `
		INSERT INTO workflow_variable_revision (
			tenant_id, instance_id, revision, variable_name, schema_ref, variable_value,
			value_digest, writer_node_id, writer_ref, reason, causation_id,
			previous_revision, prior_digest, revision_digest, written_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		rev.TenantID, rev.InstanceID, rev.Revision, rev.VariableName, rev.SchemaRef, string(rev.Value),
		rev.ValueDigest, rev.WriterNodeID, rev.WriterRef, rev.Reason, rev.CausationID,
		rev.PreviousRevision, rev.PriorDigest, rev.RevisionDigest, rev.WrittenAt.UTC()); err != nil {
		return VariableRevision{}, 0, wrap(CodeStorageFailed, id, rev.WriterNodeID, err,
			"insert variable revision %d", rev.Revision)
	}

	var affected int64
	if rev.PreviousRevision == 0 {
		affected, err = ex.Exec(ctx, `
			INSERT INTO workflow_variable (
				tenant_id, instance_id, variable_name, schema_ref, variable_value,
				written_at_revision, variable_version, written_at)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6, 1, $7)
			ON CONFLICT DO NOTHING`,
			rev.TenantID, rev.InstanceID, rev.VariableName, rev.SchemaRef, string(rev.Value),
			rev.Revision, rev.WrittenAt.UTC())
	} else {
		affected, err = ex.Exec(ctx, `
			UPDATE workflow_variable
			SET schema_ref = $4, variable_value = $5::jsonb, written_at_revision = $6,
				variable_version = variable_version + 1, written_at = $7
			WHERE tenant_id = $1 AND instance_id = $2 AND variable_name = $3 AND written_at_revision = $8`,
			rev.TenantID, rev.InstanceID, rev.VariableName, rev.SchemaRef, string(rev.Value),
			rev.Revision, rev.WrittenAt.UTC(), rev.PreviousRevision)
	}
	if err != nil {
		return VariableRevision{}, 0, wrap(CodeStorageFailed, id, rev.WriterNodeID, err,
			"project variable %s", rev.VariableName)
	}
	if affected == 0 {
		return VariableRevision{}, 0, refuse(CodeStaleInstance, id, rev.WriterNodeID,
			"variable %s is not at previous revision %d", rev.VariableName, rev.PreviousRevision)
	}
	return cloneRevision(rev), version, nil
}

// LoadVariableRevisions reads an instance's whole revision history and
// rebuilds it as a [VariableRevisionLog], so a gap, a reorder or an edited
// row is refused rather than returned.
func (Store) LoadVariableRevisions(
	ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID,
) (ret0 *VariableRevisionLog, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.load_variable_revisions", instanceID)
	defer func() { observe.DoneWith(obsOp, retErr) }()
	rows, err := ex.Query(ctx, `SELECT `+variableRevisionColumns+`
		FROM workflow_variable_revision
		WHERE tenant_id = $1 AND instance_id = $2
		ORDER BY revision`, tenantID, instanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "read variable revisions")
	}
	defer rows.Close()
	var stored []VariableRevision
	for rows.Next() {
		var (
			r     VariableRevision
			value string
		)
		if err := rows.Scan(&r.TenantID, &r.InstanceID, &r.Revision, &r.VariableName, &r.SchemaRef,
			&value, &r.ValueDigest, &r.WriterNodeID, &r.WriterRef, &r.Reason, &r.CausationID,
			&r.PreviousRevision, &r.PriorDigest, &r.RevisionDigest, &r.WrittenAt); err != nil {
			return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "scan variable revision")
		}
		canonical, err := canonicalVariableValue(json.RawMessage(value))
		if err != nil {
			return nil, wrap(CodeInvalidRecord, instanceID.String(), r.WriterNodeID, err, "stored variable value")
		}
		r.Value = canonical
		r.WrittenAt = r.WrittenAt.UTC()
		stored = append(stored, r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), "", err, "read variable revisions")
	}
	return NewVariableRevisionLog(tenantID, instanceID, stored)
}
