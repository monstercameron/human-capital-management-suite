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

// AddRevisioned inserts a typed task reference while advancing the exact task
// revision and appending task activity/outbox evidence in the same transaction.
func (s *Store) AddRevisioned(ctx context.Context, link LinkRecord, actorID, idempotencyKey string, expectedTaskRevision int64) (int64, error) {
	if s == nil || s.projects == nil || !validRecord(link) || !validID(actorID) || !validID(idempotencyKey) || expectedTaskRevision <= 0 {
		return 0, ErrInvalidRecord
	}
	fingerprint, err := addFingerprint(link, actorID, expectedTaskRevision)
	if err != nil {
		return 0, err
	}
	var resultingRevision int64
	err = s.projects.RunTenantTx(ctx, link.TenantID, func(tx dbport.Tx) error {
		replay, prior, err := claimLinkCommand(ctx, tx, link.TenantID, actorID, "task.link.add", idempotencyKey, fingerprint)
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
		err = tx.QueryRow(ctx, `SELECT lifecycle FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, link.TenantID, link.ProjectID).Scan(&lifecycle)
		if errors.Is(err, dbport.ErrNoRows) {
			return projectstore.ErrNotFound
		}
		if err != nil {
			return err
		}
		if lifecycle != "ACTIVE" {
			return projectstore.ErrRevisionConflict
		}
		err = tx.QueryRow(ctx, `SELECT revision,archived FROM project_task WHERE tenant_id=$1 AND project_id=$2 AND id=$3 FOR UPDATE`, link.TenantID, link.ProjectID, link.TaskID).Scan(&currentRevision, &archived)
		if errors.Is(err, dbport.ErrNoRows) {
			return projectstore.ErrNotFound
		}
		if err != nil {
			return err
		}
		if currentRevision != expectedTaskRevision || archived {
			return projectstore.ErrRevisionConflict
		}
		if err := AddTx(ctx, tx, link); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `UPDATE project_task SET revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND project_id=$2 AND id=$3 AND revision=$4 RETURNING revision`, link.TenantID, link.ProjectID, link.TaskID, expectedTaskRevision).Scan(&resultingRevision); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return projectstore.ErrRevisionConflict
			}
			return err
		}
		if err := appendTaskLinkEvent(ctx, tx, link.TenantID, link.ProjectID, link.TaskID, actorID, "task.link_added", expectedTaskRevision, resultingRevision, map[string]string{"linkId": link.ID, "operation": "added"}); err != nil {
			return err
		}
		return finishLinkCommand(ctx, tx, link.TenantID, actorID, "task.link.add", idempotencyKey, map[string]int64{"taskRevision": resultingRevision})
	})
	if err != nil {
		return 0, err
	}
	return resultingRevision, nil
}

func addFingerprint(link LinkRecord, actorID string, revision int64) (string, error) {
	value, err := json.Marshal(struct {
		Link                 LinkRecord
		ActorID              string
		ExpectedTaskRevision int64
	}{link, actorID, revision})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:]), nil
}

func claimLinkCommand(ctx context.Context, tx dbport.Tx, tenantID, actorID, operation, key, fingerprint string) (bool, json.RawMessage, error) {
	var claimed int
	err := tx.QueryRow(ctx, `INSERT INTO project_idempotency(tenant_id,actor_id,operation,client_key,fingerprint) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING 1`, tenantID, actorID, operation, key, fingerprint).Scan(&claimed)
	if err == nil {
		return false, nil, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return false, nil, err
	}
	var priorFingerprint string
	var result json.RawMessage
	if err := tx.QueryRow(ctx, `SELECT fingerprint,result_json FROM project_idempotency WHERE tenant_id=$1 AND actor_id=$2 AND operation=$3 AND client_key=$4`, tenantID, actorID, operation, key).Scan(&priorFingerprint, &result); err != nil {
		return false, nil, err
	}
	if priorFingerprint != fingerprint {
		return false, nil, projectstore.ErrIdempotencyConflict
	}
	return true, result, nil
}

func finishLinkCommand(ctx context.Context, tx dbport.Tx, tenantID, actorID, operation, key string, result any) error {
	value, err := json.Marshal(result)
	if err != nil {
		return err
	}
	updated, err := tx.Exec(ctx, `UPDATE project_idempotency SET result_json=$1::jsonb WHERE tenant_id=$2 AND actor_id=$3 AND operation=$4 AND client_key=$5`, value, tenantID, actorID, operation, key)
	if err != nil {
		return err
	}
	if updated != 1 {
		return errors.New("project link idempotency receipt missing")
	}
	return nil
}
