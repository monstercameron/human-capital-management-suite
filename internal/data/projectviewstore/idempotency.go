package projectviewstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
)

func fingerprint(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func claim(ctx context.Context, tx dbport.Tx, tenantID, actorID, key, digest string) (bool, json.RawMessage, error) {
	var inserted int
	err := tx.QueryRow(ctx, `INSERT INTO project_idempotency(tenant_id,actor_id,operation,client_key,fingerprint) VALUES($1,$2,'project.view.save',$3,$4) ON CONFLICT DO NOTHING RETURNING 1`, tenantID, actorID, key, digest).Scan(&inserted)
	if err == nil {
		return false, nil, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return false, nil, err
	}
	var previous string
	var result json.RawMessage
	if err := tx.QueryRow(ctx, `SELECT fingerprint,result_json FROM project_idempotency WHERE tenant_id=$1 AND actor_id=$2 AND operation='project.view.save' AND client_key=$3`, tenantID, actorID, key).Scan(&previous, &result); err != nil {
		return false, nil, err
	}
	if previous != digest {
		return false, nil, projectstore.ErrIdempotencyConflict
	}
	return true, result, nil
}

func finish(ctx context.Context, tx dbport.Tx, tenantID, actorID, key string, result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE project_idempotency SET result_json=$1::jsonb WHERE tenant_id=$2 AND actor_id=$3 AND operation='project.view.save' AND client_key=$4`, data, tenantID, actorID, key)
	return err
}
