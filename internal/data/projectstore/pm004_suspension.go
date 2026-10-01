package projectstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

// SuspendProject records an operator-authorized incident suspension. Reads
// remain available, while existing task/configuration commands reject writes
// against the non-active project state.
func (s *Store) SuspendProject(ctx context.Context, tenantID, projectID string, expectedRevision int64, actorID, origin, reason, key string) (ProjectRecord, error) {
	reason = strings.TrimSpace(reason)
	if tenantID == "" || projectID == "" || expectedRevision <= 0 || actorID == "" || reason == "" || key == "" || !validOrigin(origin) {
		return ProjectRecord{}, ErrInvalidRecord
	}
	fingerprint, err := mutationFingerprint(struct {
		TenantID, ProjectID, Actor, Origin, Reason string
		ExpectedRevision                           int64
	}{tenantID, projectID, actorID, origin, reason, expectedRevision})
	if err != nil {
		return ProjectRecord{}, err
	}
	var out ProjectRecord
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		prior, err := scanProject(tx.QueryRow(ctx, `SELECT id,tenant_id,owner_id,name,project_timezone,lifecycle,revision,created_at,updated_at FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID))
		if err != nil {
			return mapNotFound(err)
		}
		var operator bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project_membership WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3 AND role='MANAGER' AND state='ACTIVE')`, tenantID, projectID, actorID).Scan(&operator); err != nil {
			return err
		}
		if !operator {
			return project.ErrNotAuthorized
		}
		replay, receipt, err := claimIdempotency(ctx, tx, tenantID, actorID, "project.suspend", key, fingerprint)
		if err != nil {
			return err
		}
		if replay {
			return json.Unmarshal(receipt, &out)
		}
		updated, err := domainProject(prior).TransitionProject(project.LifecycleSuspended, uint64(expectedRevision), actorID, project.RoleOperator, reason)
		if err != nil {
			if errors.Is(err, project.ErrRevisionConflict) {
				return ErrRevisionConflict
			}
			if errors.Is(err, project.ErrNotAuthorized) {
				return err
			}
			return ErrInvalidProjectTransition
		}
		out, err = scanProject(tx.QueryRow(ctx, `UPDATE project SET lifecycle=$1,revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND id=$3 AND revision=$4 RETURNING id,tenant_id,owner_id,name,project_timezone,lifecycle,revision,created_at,updated_at`, string(updated.State), tenantID, projectID, expectedRevision))
		if err != nil {
			return mapNotFound(err)
		}
		if err := appendProjectEvent(ctx, tx, tenantID, projectID, projectID, actorID, origin, "project.suspended", expectedRevision, out.Revision, 0, map[string]any{"lifecycle": out.Lifecycle, "reason": reason}); err != nil {
			return err
		}
		return finishIdempotency(ctx, tx, tenantID, actorID, "project.suspend", key, out)
	})
	if err != nil {
		return ProjectRecord{}, err
	}
	return out, nil
}
