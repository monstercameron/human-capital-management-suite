package projectstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
)

var ErrInvalidProjectTransition = errors.New("invalid project lifecycle transition")

// UpdateProjectSettings commits name/timezone, project revision, event/outbox,
// and the idempotency result in one tenant transaction.
func (s *Store) UpdateProjectSettings(ctx context.Context, tenantID, projectID, name, timezone string, expectedRevision int64, actorID, origin, key string) (ProjectRecord, error) {
	name = strings.TrimSpace(name)
	if tenantID == "" || projectID == "" || name == "" || expectedRevision <= 0 || actorID == "" || key == "" || !validOrigin(origin) {
		return ProjectRecord{}, ErrInvalidRecord
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return ProjectRecord{}, ErrInvalidRecord
	}
	fingerprint, err := mutationFingerprint(struct {
		TenantID, ProjectID, Name, Timezone string
		ExpectedRevision                    int64
		Actor, Origin                       string
	}{tenantID, projectID, name, timezone, expectedRevision, actorID, origin})
	if err != nil {
		return ProjectRecord{}, err
	}
	var out ProjectRecord
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		replay, receipt, err := claimIdempotency(ctx, tx, tenantID, actorID, "project.settings", key, fingerprint)
		if err != nil {
			return err
		}
		if replay {
			return json.Unmarshal(receipt, &out)
		}
		prior, err := scanProject(tx.QueryRow(ctx, `SELECT id,tenant_id,owner_id,name,project_timezone,lifecycle,revision,created_at,updated_at FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID))
		if err != nil {
			return mapNotFound(err)
		}
		updated, err := domainProject(prior).UpdateSettings(name, timezone, uint64(expectedRevision))
		if err != nil {
			if errors.Is(err, project.ErrRevisionConflict) {
				return ErrRevisionConflict
			}
			return ErrInvalidRecord
		}
		out, err = scanProject(tx.QueryRow(ctx, `UPDATE project SET name=$1,project_timezone=$2,revision=revision+1,updated_at=now() WHERE tenant_id=$3 AND id=$4 AND revision=$5 RETURNING id,tenant_id,owner_id,name,project_timezone,lifecycle,revision,created_at,updated_at`, updated.Name, updated.Timezone, tenantID, projectID, expectedRevision))
		if err != nil {
			return mapNotFound(err)
		}
		if err := appendProjectEvent(ctx, tx, tenantID, projectID, projectID, actorID, origin, "project.settings_updated", expectedRevision, out.Revision, 0, map[string]any{"name": out.Name, "timezone": out.Timezone}); err != nil {
			return err
		}
		return finishIdempotency(ctx, tx, tenantID, actorID, "project.settings", key, out)
	})
	if err != nil {
		return ProjectRecord{}, err
	}
	return out, nil
}

// TransitionProject archives or restores a project as an audited project
// revision. Only the recorded owner can perform these lifecycle transitions.
func (s *Store) TransitionProject(ctx context.Context, tenantID, projectID string, target project.Lifecycle, expectedRevision int64, actorID, origin, key string) (ProjectRecord, error) {
	if tenantID == "" || projectID == "" || expectedRevision <= 0 || actorID == "" || key == "" || !validOrigin(origin) || (target != project.LifecycleArchived && target != project.LifecycleActive) {
		return ProjectRecord{}, ErrInvalidRecord
	}
	operation, event := "project.restore", "project.restored"
	if target == project.LifecycleArchived {
		operation, event = "project.archive", "project.archived"
	}
	fingerprint, err := mutationFingerprint(struct {
		TenantID, ProjectID string
		Target              project.Lifecycle
		ExpectedRevision    int64
		Actor, Origin       string
	}{tenantID, projectID, target, expectedRevision, actorID, origin})
	if err != nil {
		return ProjectRecord{}, err
	}
	var out ProjectRecord
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		prior, err := scanProject(tx.QueryRow(ctx, `SELECT id,tenant_id,owner_id,name,project_timezone,lifecycle,revision,created_at,updated_at FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID))
		if err != nil {
			return mapNotFound(err)
		}
		if actorID != prior.OwnerID {
			return project.ErrNotAuthorized
		}
		replay, receipt, err := claimIdempotency(ctx, tx, tenantID, actorID, operation, key, fingerprint)
		if err != nil {
			return err
		}
		if replay {
			return json.Unmarshal(receipt, &out)
		}
		updated, err := domainProject(prior).TransitionProject(target, uint64(expectedRevision), actorID, project.RoleOwner, "")
		if err != nil {
			if errors.Is(err, project.ErrRevisionConflict) {
				return ErrRevisionConflict
			}
			return ErrInvalidProjectTransition
		}
		out, err = scanProject(tx.QueryRow(ctx, `UPDATE project SET lifecycle=$1,revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND id=$3 AND revision=$4 RETURNING id,tenant_id,owner_id,name,project_timezone,lifecycle,revision,created_at,updated_at`, string(updated.State), tenantID, projectID, expectedRevision))
		if err != nil {
			return mapNotFound(err)
		}
		if err := appendProjectEvent(ctx, tx, tenantID, projectID, projectID, actorID, origin, event, expectedRevision, out.Revision, 0, map[string]any{"lifecycle": out.Lifecycle}); err != nil {
			return err
		}
		return finishIdempotency(ctx, tx, tenantID, actorID, operation, key, out)
	})
	if err != nil {
		return ProjectRecord{}, err
	}
	return out, nil
}

func domainProject(r ProjectRecord) project.Project {
	return project.Project{ID: project.ProjectID(r.ID), TenantID: r.TenantID, OwnerID: r.OwnerID, Name: r.Name, Timezone: r.Timezone, State: project.Lifecycle(r.Lifecycle), Revision: uint64(r.Revision)}
}

func scanProject(row interface{ Scan(...any) error }) (ProjectRecord, error) {
	var value ProjectRecord
	err := row.Scan(&value.ID, &value.TenantID, &value.OwnerID, &value.Name, &value.Timezone, &value.Lifecycle, &value.Revision, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return ProjectRecord{}, err
	}
	return value, nil
}
