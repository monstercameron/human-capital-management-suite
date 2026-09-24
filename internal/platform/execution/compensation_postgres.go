package execution

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowcompensation"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/compensate"
)

// postgresCompensationStore adapts the workflow port to PostgreSQL. All
// methods require the caller's transaction from dbport context.
type postgresCompensationStore struct{}

func compensationTx(ctx context.Context) (dbport.Tx, error) {
	tx, ok := dbport.TxFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("platform execution: compensation requires its governing transaction")
	}
	return tx, nil
}
func compensationKey(k compensate.OperationKey) (workflowcompensation.Key, error) {
	tenant, err := uuid.Parse(k.TenantID)
	if err != nil {
		return workflowcompensation.Key{}, err
	}
	return workflowcompensation.Key{Tenant: tenant, Capability: k.CapabilityRef, Effect: k.TargetEffectRef, IdempotencyKey: k.IdempotencyKey}, nil
}
func (postgresCompensationStore) Reserve(ctx context.Context, k compensate.OperationKey, digest string) (compensate.OperationRecord, bool, error) {
	tx, err := compensationTx(ctx)
	if err != nil {
		return compensate.OperationRecord{}, false, err
	}
	key, err := compensationKey(k)
	if err != nil {
		return compensate.OperationRecord{}, false, err
	}
	stored, created, err := workflowcompensation.Reserve(ctx, tx, key, digest)
	if err != nil {
		return compensate.OperationRecord{}, false, err
	}
	var rec compensate.OperationRecord
	if !created {
		if err = json.Unmarshal(stored.Payload, &rec); err != nil {
			return rec, false, fmt.Errorf("platform execution: decode durable compensation operation: %w", err)
		}
		if rec.Key != k || rec.RequestDigest != stored.RequestDigest {
			return rec, false, fmt.Errorf("platform execution: durable compensation operation binding mismatch")
		}
	}
	if created {
		rec = compensate.OperationRecord{Key: k, RequestDigest: digest, State: compensate.OperationReserved}
		if err = saveCompensationOperation(ctx, tx, key, digest, rec); err != nil {
			return rec, false, err
		}
	}
	return rec, created, nil
}
func (postgresCompensationStore) RecordEffect(ctx context.Context, k compensate.OperationKey, digest string, receipt compensate.CapabilityReceipt) error {
	return postgresCompensationStore{}.update(ctx, k, digest, func(r *compensate.OperationRecord) { r.State = compensate.OperationEffectRecorded; r.Receipt = receipt })
}
func (postgresCompensationStore) Complete(ctx context.Context, k compensate.OperationKey, digest string, result compensate.Result) error {
	return postgresCompensationStore{}.update(ctx, k, digest, func(r *compensate.OperationRecord) { r.State = compensate.OperationCompleted; r.Result = result })
}
func (postgresCompensationStore) update(ctx context.Context, k compensate.OperationKey, digest string, mutate func(*compensate.OperationRecord)) error {
	tx, err := compensationTx(ctx)
	if err != nil {
		return err
	}
	key, err := compensationKey(k)
	if err != nil {
		return err
	}
	var payload []byte
	if err = tx.QueryRow(ctx, `SELECT payload FROM workflow_compensation_operation WHERE tenant_id=$1 AND capability_id=$2 AND effect_ref=$3 AND idempotency_key=$4 AND request_digest=$5 FOR UPDATE`, key.Tenant, key.Capability, key.Effect, key.IdempotencyKey, digest).Scan(&payload); err != nil {
		return err
	}
	var rec compensate.OperationRecord
	if err = json.Unmarshal(payload, &rec); err != nil {
		return err
	}
	if rec.Key != k || rec.RequestDigest != digest {
		return fmt.Errorf("platform execution: compensation operation binding mismatch")
	}
	mutate(&rec)
	return saveCompensationOperation(ctx, tx, key, digest, rec)
}
func saveCompensationOperation(ctx context.Context, tx dbport.Tx, key workflowcompensation.Key, digest string, rec compensate.OperationRecord) error {
	payload, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return workflowcompensation.Update(ctx, tx, key, digest, string(rec.State), payload)
}
func (postgresCompensationStore) AppendCompensation(ctx context.Context, e compensate.Event) (string, error) {
	tx, err := compensationTx(ctx)
	if err != nil {
		return "", err
	}
	tenant, err := uuid.Parse(e.Request.TenantID)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	if err = workflowcompensation.Append(ctx, tx, tenant, e.EventRef, e.Digest, payload); err != nil {
		return "", err
	}
	return e.EventRef, nil
}

func (postgresCompensationStore) CloseCompensated(ctx context.Context, scope idempotency.Scope, eventRef string) error {
	tx, err := compensationTx(ctx)
	if err != nil {
		return err
	}
	_, err = (idempotency.PostgresStore{}).CloseCompensated(ctx, tx, scope, eventRef)
	return err
}
