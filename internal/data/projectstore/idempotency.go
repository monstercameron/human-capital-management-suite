package projectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var ErrIdempotencyConflict = errors.New("project idempotency key fingerprint conflict")

func mutationFingerprint(value any) (string, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

// claimIdempotency returns the prior result for an exact retry. A newly
// claimed row is committed atomically with the mutation and its event records.
func claimIdempotency(ctx context.Context, tx dbport.Tx, tenantID, actorID, operation, clientKey, fingerprint string) (bool, json.RawMessage, error) {
	var claimed int
	err := tx.QueryRow(ctx, `INSERT INTO project_idempotency(tenant_id,actor_id,operation,client_key,fingerprint) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING 1`, tenantID, actorID, operation, clientKey, fingerprint).Scan(&claimed)
	if err == nil {
		return false, nil, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return false, nil, err
	}
	var priorFingerprint string
	var result json.RawMessage
	if err := tx.QueryRow(ctx, `SELECT fingerprint,result_json FROM project_idempotency WHERE tenant_id=$1 AND actor_id=$2 AND operation=$3 AND client_key=$4`, tenantID, actorID, operation, clientKey).Scan(&priorFingerprint, &result); err != nil {
		return false, nil, err
	}
	if priorFingerprint != fingerprint {
		return false, nil, ErrIdempotencyConflict
	}
	return true, result, nil
}

func finishIdempotency(ctx context.Context, tx dbport.Tx, tenantID, actorID, operation, clientKey string, result any) error {
	value, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE project_idempotency SET result_json=$1::jsonb WHERE tenant_id=$2 AND actor_id=$3 AND operation=$4 AND client_key=$5`, value, tenantID, actorID, operation, clientKey)
	return err
}
