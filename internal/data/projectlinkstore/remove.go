package projectlinkstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
)

// Remove tombstones a task link and advances the task revision. Its activity,
// outbox record and idempotency receipt commit with the tombstone in one tenant
// transaction. The reference identifier is intentionally absent from the
// removal activity payload.
func (s *Store) Remove(ctx context.Context, tenantID, projectID, taskID, linkID, actorID, idempotencyKey string, expectedTaskRevision int64) (int64, error) {
	if s == nil || s.projects == nil || !validID(tenantID) || !validID(projectID) || !validID(taskID) || !validID(linkID) || !validID(actorID) || !validID(idempotencyKey) || expectedTaskRevision <= 0 {
		return 0, ErrInvalidRecord
	}
	fingerprint, err := removeFingerprint(tenantID, projectID, taskID, linkID, actorID, expectedTaskRevision)
	if err != nil {
		return 0, err
	}
	var resultingRevision int64
	err = s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		replay, prior, err := claimLinkCommand(ctx, tx, tenantID, actorID, "task.link.remove", idempotencyKey, fingerprint)
		if err != nil {
			return err
		}
		if replay {
			var result struct {
				TaskRevision int64 `json:"taskRevision"`
			}
			if err := json.Unmarshal(prior, &result); err != nil || result.TaskRevision <= 0 {
				return ErrInvalidRecord
			}
			resultingRevision = result.TaskRevision
			return nil
		}

		var currentRevision int64
		var archived bool
		var lifecycle string
		err = tx.QueryRow(ctx, `SELECT t.revision,t.archived,p.lifecycle FROM project_task t JOIN project p ON p.tenant_id=t.tenant_id AND p.id=t.project_id WHERE t.tenant_id=$1 AND t.project_id=$2 AND t.id=$3 FOR UPDATE OF t,p`, tenantID, projectID, taskID).Scan(&currentRevision, &archived, &lifecycle)
		if errors.Is(err, dbport.ErrNoRows) {
			return projectstore.ErrNotFound
		}
		if err != nil {
			return err
		}
		if currentRevision != expectedTaskRevision {
			return projectstore.ErrRevisionConflict
		}
		if archived || lifecycle != "ACTIVE" {
			return projectstore.ErrRevisionConflict
		}
		var liveLink string
		err = tx.QueryRow(ctx, `SELECT id FROM project_task_link WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND id=$4 AND removed_at IS NULL FOR UPDATE`, tenantID, projectID, taskID, linkID).Scan(&liveLink)
		if errors.Is(err, dbport.ErrNoRows) {
			return projectstore.ErrNotFound
		}
		if err != nil {
			return err
		}

		if err = tx.QueryRow(ctx, `UPDATE project_task SET revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND project_id=$2 AND id=$3 AND revision=$4 RETURNING revision`, tenantID, projectID, taskID, expectedTaskRevision).Scan(&resultingRevision); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return projectstore.ErrRevisionConflict
			}
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE project_task_link SET removed_at=now(),removed_by=$1,removed_task_revision=$2 WHERE tenant_id=$3 AND project_id=$4 AND task_id=$5 AND id=$6 AND removed_at IS NULL`, actorID, resultingRevision, tenantID, projectID, taskID, linkID); err != nil {
			return err
		}
		if err = appendTaskLinkEvent(ctx, tx, tenantID, projectID, taskID, actorID, "task.link_removed", expectedTaskRevision, resultingRevision, map[string]string{"linkId": linkID, "operation": "removed"}); err != nil {
			return err
		}
		return finishLinkCommand(ctx, tx, tenantID, actorID, "task.link.remove", idempotencyKey, map[string]int64{"taskRevision": resultingRevision})
	})
	if err != nil {
		return 0, err
	}
	return resultingRevision, nil
}

func removeFingerprint(tenantID, projectID, taskID, linkID, actorID string, revision int64) (string, error) {
	value, err := json.Marshal(struct {
		TenantID, ProjectID, TaskID, LinkID, ActorID string
		ExpectedTaskRevision                         int64
	}{tenantID, projectID, taskID, linkID, actorID, revision})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:]), nil
}
