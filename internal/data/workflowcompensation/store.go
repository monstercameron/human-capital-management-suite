// Package workflowcompensation persists workflow compensation events and
// operation state in the caller's governing transaction.
package workflowcompensation

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type Key struct {
	Tenant                             uuid.UUID
	Capability, Effect, IdempotencyKey string
}
type Operation struct {
	RequestDigest, State string
	Payload              []byte
}

func validKey(k Key) bool {
	return k.Tenant != uuid.Nil && strings.TrimSpace(k.Capability) != "" && strings.TrimSpace(k.Effect) != "" && strings.TrimSpace(k.IdempotencyKey) != ""
}

// Reserve is protected by the primary key and returns the committed or
// transaction-local operation when another attempt already owns it.
func Reserve(ctx context.Context, tx dbport.Tx, key Key, digest string) (Operation, bool, error) {
	if !validKey(key) || strings.TrimSpace(digest) == "" {
		return Operation{}, false, errors.New("workflow compensation: incomplete operation identity")
	}
	var out Operation
	err := tx.QueryRow(ctx, `INSERT INTO workflow_compensation_operation
		(tenant_id,capability_id,effect_ref,idempotency_key,request_digest,state,payload)
		VALUES($1,$2,$3,$4,$5,'RESERVED','{}'::jsonb)
		ON CONFLICT(tenant_id,capability_id,effect_ref,idempotency_key) DO NOTHING
		RETURNING request_digest,state,payload`, key.Tenant, key.Capability, key.Effect, key.IdempotencyKey, digest).Scan(&out.RequestDigest, &out.State, &out.Payload)
	if err == nil {
		return out, true, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return Operation{}, false, err
	}
	err = tx.QueryRow(ctx, `SELECT request_digest,state,payload FROM workflow_compensation_operation
		WHERE tenant_id=$1 AND capability_id=$2 AND effect_ref=$3 AND idempotency_key=$4 FOR UPDATE`,
		key.Tenant, key.Capability, key.Effect, key.IdempotencyKey).Scan(&out.RequestDigest, &out.State, &out.Payload)
	return out, false, err
}

func Update(ctx context.Context, tx dbport.Tx, key Key, digest, state string, payload []byte) error {
	if !validKey(key) || strings.TrimSpace(digest) == "" || len(payload) == 0 {
		return errors.New("workflow compensation: incomplete operation update")
	}
	if state != "RESERVED" && state != "EFFECT_RECORDED" && state != "COMPLETED" {
		return errors.New("workflow compensation: invalid operation state")
	}
	count, err := tx.Exec(ctx, `UPDATE workflow_compensation_operation SET state=$1,payload=$2::jsonb,updated_at=now()
		WHERE tenant_id=$3 AND capability_id=$4 AND effect_ref=$5 AND idempotency_key=$6 AND request_digest=$7
		AND ((state='RESERVED' AND $1 IN ('RESERVED','EFFECT_RECORDED','COMPLETED'))
		OR (state='EFFECT_RECORDED' AND $1 IN ('EFFECT_RECORDED','COMPLETED')))`, state, payload, key.Tenant, key.Capability, key.Effect, key.IdempotencyKey, digest)
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("workflow compensation: operation update lost its reservation")
	}
	return nil
}

// Append inserts an immutable event. An identical retry is accepted; reusing
// the content-derived reference for different bytes is rejected.
func Append(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, ref, digest string, payload []byte) error {
	if tenant == uuid.Nil || strings.TrimSpace(ref) == "" || strings.TrimSpace(digest) == "" || len(payload) == 0 {
		return errors.New("workflow compensation: incomplete event identity")
	}
	count, err := tx.Exec(ctx, `INSERT INTO workflow_compensation_event(tenant_id,event_ref,event_digest,payload)
		VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(tenant_id,event_ref) DO NOTHING`, tenant, ref, digest, payload)
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	var stored string
	err = tx.QueryRow(ctx, `SELECT event_digest FROM workflow_compensation_event WHERE tenant_id=$1 AND event_ref=$2`, tenant, ref).Scan(&stored)
	if err != nil {
		return err
	}
	if stored != digest {
		return errors.New("workflow compensation: event reference digest conflict")
	}
	return nil
}
