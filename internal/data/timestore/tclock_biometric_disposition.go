package timestore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// BiometricDisposition is the durable state of a consent withdrawal and its
// crypto-erasure saga. It contains no biometric bytes or key material.
type BiometricDisposition struct {
	TenantID, ID, ConsentID, WorkerID, TemplateCustodyRef, State string
	IdempotencyKey, LastError                                    string
	ClaimID                                                      *string
	Revision                                                     int64
	TombstonedAt, KeyDestroyedAt, CompletedAt                    *time.Time
}

func recordBiometricDispositionEvent(ctx context.Context, tx dbport.Tx, tenant, dispositionID, event, actorID, reason string, claimID *string) error {
	_, err := tx.Exec(ctx, `INSERT INTO time_biometric_disposition_event(tenant_id,id,disposition_id,event,actor_id,reason,claim_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, uuid.NewString(), dispositionID, event, actorID, reason, claimID)
	return err
}

const biometricDispositionColumns = `tenant_id,id,consent_id,worker_id,template_custody_ref,state,idempotency_key,claim_id,tombstoned_at,key_destroyed_at,completed_at,last_error,revision`

func scanBiometricDisposition(row dbport.Row) (BiometricDisposition, error) {
	var d BiometricDisposition
	if err := row.Scan(&d.TenantID, &d.ID, &d.ConsentID, &d.WorkerID, &d.TemplateCustodyRef, &d.State, &d.IdempotencyKey, &d.ClaimID, &d.TombstonedAt, &d.KeyDestroyedAt, &d.CompletedAt, &d.LastError, &d.Revision); errors.Is(err, dbport.ErrNoRows) {
		return BiometricDisposition{}, ErrNotFound
	} else {
		return d, err
	}
}

// WithdrawBiometricConsent revokes identification and creates one durable
// tombstone. Repeating the same idempotency key returns the original row.
func (s *Store) WithdrawBiometricConsent(ctx context.Context, tenant, consentID, workerID, actorID, reason, idempotencyKey string, expectedRevision int64) (BiometricDisposition, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(consentID) == "" || strings.TrimSpace(workerID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(idempotencyKey) == "" || expectedRevision <= 0 {
		return BiometricDisposition{}, ErrInvalid
	}
	var out BiometricDisposition
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if existing, err := scanBiometricDisposition(tx.QueryRow(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND idempotency_key=$2`, tenant, idempotencyKey)); err == nil {
			if existing.ConsentID != consentID || existing.WorkerID != workerID {
				return ErrIdempotencyConflict
			}
			out = existing
			return nil
		}
		consent, err := scanConsent(tx.QueryRow(ctx, `SELECT `+biometricConsentColumns+` FROM time_biometric_consent WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, consentID))
		if err != nil {
			return err
		}
		if consent.WorkerID != workerID || consent.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if consent.State == ConsentDestroyed {
			return ErrInvalid
		}
		if consent.State == ConsentGranted {
			next := expectedRevision + 1
			if tag, err := tx.Exec(ctx, `UPDATE time_biometric_consent SET state='REVOKED',revision=$1,updated_at=now() WHERE tenant_id=$2 AND id=$3 AND revision=$4`, next, tenant, consentID, expectedRevision); err != nil || tag != 1 {
				if err != nil {
					return err
				}
				return ErrRevisionConflict
			}
			if err := recordConsentEvent(ctx, tx, tenant, consentID, ConsentRevoked, reason, actorID, next); err != nil {
				return err
			}
		}
		id := uuid.NewString()
		if _, err := tx.Exec(ctx, `INSERT INTO time_biometric_disposition(tenant_id,id,consent_id,worker_id,template_custody_ref,idempotency_key) VALUES($1,$2,$3,$4,$5,$6)`, tenant, id, consentID, workerID, consent.TemplateCustodyRef, idempotencyKey); err != nil {
			return err
		}
		if err := recordBiometricDispositionEvent(ctx, tx, tenant, id, "WITHDRAWN", actorID, reason, nil); err != nil {
			return err
		}
		var scanErr error
		out, scanErr = scanBiometricDisposition(tx.QueryRow(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return scanErr
	})
	return out, err
}

// GetBiometricDisposition returns a tenant-bound saga row.
func (s *Store) GetBiometricDisposition(ctx context.Context, tenant, id string) (BiometricDisposition, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(id) == "" {
		return BiometricDisposition{}, ErrInvalid
	}
	var out BiometricDisposition
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		out, err = scanBiometricDisposition(tx.QueryRow(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	return out, err
}

// ClaimBiometricTombstone atomically records the tombstone before custody
// work. A crash after this call is recoverable by ReconcileBiometricDisposition.
func (s *Store) ClaimBiometricTombstone(ctx context.Context, tenant, id, actorID string, now time.Time) (BiometricDisposition, error) {
	if tenant == "" || id == "" || actorID == "" || now.IsZero() {
		return BiometricDisposition{}, ErrInvalid
	}
	var out BiometricDisposition
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		d, err := scanBiometricDisposition(tx.QueryRow(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id))
		if err != nil {
			return err
		}
		if d.State == "COMPLETED" || d.State == "KEY_DESTROYED" || d.State == "TOMBSTONED" || d.State == "KEY_DESTROYING" {
			out = d
			return nil
		}
		if d.State == "HOLD_BLOCKED" && d.ClaimID != nil {
			if _, err = tx.Exec(ctx, `UPDATE time_biometric_disposition SET state='TOMBSTONED',last_error='',revision=revision+1,updated_at=$1 WHERE tenant_id=$2 AND id=$3`, now, tenant, id); err != nil {
				return err
			}
			out, err = scanBiometricDisposition(tx.QueryRow(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND id=$2`, tenant, id))
			return err
		}
		claim := uuid.NewString()
		if _, err = tx.Exec(ctx, `INSERT INTO time_biometric_key_destroy_claim(tenant_id,id,disposition_id,custody_ref) VALUES($1,$2,$3,$4)`, tenant, claim, id, d.TemplateCustodyRef); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE time_biometric_disposition SET state='TOMBSTONED',claim_id=$1,tombstoned_at=$2,revision=revision+1,updated_at=$2 WHERE tenant_id=$3 AND id=$4`, claim, now, tenant, id); err != nil {
			return err
		}
		if err = recordBiometricDispositionEvent(ctx, tx, tenant, id, "TOMBSTONED", actorID, "durable tombstone claimed", &claim); err != nil {
			return err
		}
		out, err = scanBiometricDisposition(tx.QueryRow(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	return out, err
}

// MarkBiometricKeyDestroyed records successful custody destruction exactly
// once. It is safe to call after a retry of an already completed claim.
func (s *Store) MarkBiometricKeyDestroyed(ctx context.Context, tenant, id, actorID string, now time.Time) (BiometricDisposition, error) {
	if tenant == "" || id == "" || actorID == "" || now.IsZero() {
		return BiometricDisposition{}, ErrInvalid
	}
	var out BiometricDisposition
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		d, err := scanBiometricDisposition(tx.QueryRow(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id))
		if err != nil {
			return err
		}
		if d.State == "COMPLETED" || d.State == "KEY_DESTROYED" {
			out = d
			return nil
		}
		if d.State != "TOMBSTONED" && d.State != "KEY_DESTROYING" {
			return ErrInvalid
		}
		if _, err = tx.Exec(ctx, `UPDATE time_biometric_key_destroy_claim SET state='COMPLETED',completed_at=$1 WHERE tenant_id=$2 AND disposition_id=$3 AND state='CLAIMED'`, now, tenant, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE time_biometric_disposition SET state='COMPLETED',key_destroyed_at=$1,completed_at=$1,revision=revision+1,updated_at=$1 WHERE tenant_id=$2 AND id=$3`, now, tenant, id); err != nil {
			return err
		}
		if err = recordBiometricDispositionEvent(ctx, tx, tenant, id, "KEY_DESTROYED", actorID, "custody acknowledged key destruction", d.ClaimID); err != nil {
			return err
		}
		out, err = scanBiometricDisposition(tx.QueryRow(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	return out, err
}

// MarkBiometricHoldBlocked records a hold observed during either preflight or
// the final recheck. A later reconciler may retry after the hold is released.
func (s *Store) MarkBiometricHoldBlocked(ctx context.Context, tenant, id, actorID, reason string, now time.Time) (BiometricDisposition, error) {
	if tenant == "" || id == "" || actorID == "" || strings.TrimSpace(reason) == "" || now.IsZero() {
		return BiometricDisposition{}, ErrInvalid
	}
	var out BiometricDisposition
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE time_biometric_disposition SET state='HOLD_BLOCKED',last_error=$1,revision=revision+1,updated_at=$2 WHERE tenant_id=$3 AND id=$4 AND state IN ('TOMBSTONE_PENDING','TOMBSTONED','KEY_DESTROYING')`, reason, now, tenant, id); err != nil {
			return err
		}
		if err := recordBiometricDispositionEvent(ctx, tx, tenant, id, "HOLD_BLOCKED", actorID, reason, nil); err != nil {
			return err
		}
		var err error
		out, err = scanBiometricDisposition(tx.QueryRow(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	return out, err
}

// ReconcileBiometricDisposition returns pending tombstones for crash recovery.
func (s *Store) ReconcileBiometricDisposition(ctx context.Context, tenant string, limit int) ([]BiometricDisposition, error) {
	if tenant == "" || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]BiometricDisposition, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+biometricDispositionColumns+` FROM time_biometric_disposition WHERE tenant_id=$1 AND state IN ('TOMBSTONE_PENDING','TOMBSTONED','KEY_DESTROYING','KEY_DESTROYED','HOLD_BLOCKED') ORDER BY updated_at LIMIT $2`, tenant, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanBiometricDisposition(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}
