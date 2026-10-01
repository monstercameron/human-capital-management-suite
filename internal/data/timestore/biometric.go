package timestore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Biometric consent states.
const (
	ConsentGranted   = "GRANTED"
	ConsentRevoked   = "REVOKED"
	ConsentDestroyed = "DESTROYED"
)

// BiometricConsent binds one worker's consent to bio identification to a
// specific notice version and a destruction schedule. TemplateCustodyRef
// points at wherever the template itself is kept (on-device, or a
// separately keyed store); this row never holds template bytes, and no
// column here could hold them by accident.
type BiometricConsent struct {
	TenantID, ID, WorkerID, NoticeVersion, LegalBasis, Jurisdiction, TemplateCustodyRef, State string
	Revision                                                                                   int64
	DestructionAt, ConsentedAt, UpdatedAt                                                      time.Time
	DestroyedAt                                                                                *time.Time
}

const biometricConsentColumns = `tenant_id,id,worker_id,notice_version,legal_basis,jurisdiction,template_custody_ref,destruction_at,state,revision,consented_at,updated_at,destroyed_at`

func scanConsent(row dbport.Row) (BiometricConsent, error) {
	var c BiometricConsent
	err := row.Scan(&c.TenantID, &c.ID, &c.WorkerID, &c.NoticeVersion, &c.LegalBasis, &c.Jurisdiction, &c.TemplateCustodyRef, &c.DestructionAt, &c.State, &c.Revision, &c.ConsentedAt, &c.UpdatedAt, &c.DestroyedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return BiometricConsent{}, ErrNotFound
	}
	return c, err
}

func recordConsentEvent(ctx context.Context, tx dbport.Tx, tenant, consentID, event, reason, actorID string, revision int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO time_biometric_consent_history(tenant_id,id,consent_id,revision,event,reason,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		tenant, uuid.NewString(), consentID, revision, event, reason, actorID)
	return err
}

// RecordBiometricConsent stores a worker's consent to a specific notice
// version, with the legal basis, jurisdiction, template custody reference
// and destruction schedule that notice promised. Callers (TCLOCK-006's
// policy layer) are responsible for refusing to call this until a policy
// naming those facts exists; this store only records what it is given.
func (s *Store) RecordBiometricConsent(ctx context.Context, tenant, workerID, noticeVersion, legalBasis, jurisdiction, templateCustodyRef, actorID string, destructionAt time.Time) (BiometricConsent, error) {
	if tenant == "" || workerID == "" || noticeVersion == "" || legalBasis == "" || jurisdiction == "" || templateCustodyRef == "" || actorID == "" || destructionAt.IsZero() {
		return BiometricConsent{}, ErrInvalid
	}
	id := uuid.NewString()
	var out BiometricConsent
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO time_biometric_consent(tenant_id,id,worker_id,notice_version,legal_basis,jurisdiction,template_custody_ref,destruction_at,state,revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'GRANTED',1)`,
			tenant, id, workerID, noticeVersion, legalBasis, jurisdiction, templateCustodyRef, destructionAt); err != nil {
			return err
		}
		if err := recordConsentEvent(ctx, tx, tenant, id, ConsentGranted, "", actorID, 1); err != nil {
			return err
		}
		var err error
		out, err = scanConsent(tx.QueryRow(ctx, `SELECT `+biometricConsentColumns+` FROM time_biometric_consent WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	if err != nil {
		return BiometricConsent{}, err
	}
	return out, nil
}

// GetBiometricConsent returns one consent record by ID.
func (s *Store) GetBiometricConsent(ctx context.Context, tenant, id string) (BiometricConsent, error) {
	if tenant == "" || id == "" {
		return BiometricConsent{}, ErrInvalid
	}
	var out BiometricConsent
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		out, err = scanConsent(tx.QueryRow(ctx, `SELECT `+biometricConsentColumns+` FROM time_biometric_consent WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	return out, err
}

// RevokeBiometricConsent withdraws consent. The template still exists in
// its custody store until DestroyBiometricConsent runs; this only stops
// future biometric identification for the worker.
func (s *Store) RevokeBiometricConsent(ctx context.Context, tenant, id, reason, actorID string, expectedRevision int64) (BiometricConsent, error) {
	return s.transitionConsent(ctx, tenant, id, reason, actorID, ConsentRevoked, "REVOKED", expectedRevision)
}

// DestroyBiometricConsent records that the template referenced by
// TemplateCustodyRef has been destroyed, at the earlier of purpose end or
// the statutory limit. This row is the evidence; actually erasing the
// template at its custody location is the caller's responsibility.
func (s *Store) DestroyBiometricConsent(ctx context.Context, tenant, id, reason, actorID string, expectedRevision int64) (BiometricConsent, error) {
	return s.transitionConsent(ctx, tenant, id, reason, actorID, ConsentDestroyed, "DESTROYED", expectedRevision)
}

func (s *Store) transitionConsent(ctx context.Context, tenant, id, reason, actorID, event, newState string, expectedRevision int64) (BiometricConsent, error) {
	if tenant == "" || id == "" || actorID == "" || expectedRevision <= 0 {
		return BiometricConsent{}, ErrInvalid
	}
	var out BiometricConsent
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		existing, err := scanConsent(tx.QueryRow(ctx, `SELECT `+biometricConsentColumns+` FROM time_biometric_consent WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id))
		if err != nil {
			return err
		}
		if existing.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		newRevision := expectedRevision + 1
		var destroyedSet string
		if newState == ConsentDestroyed {
			destroyedSet = ",destroyed_at=now()"
		}
		tag, err := tx.Exec(ctx, `UPDATE time_biometric_consent SET state=$1,revision=$2,updated_at=now()`+destroyedSet+` WHERE tenant_id=$3 AND id=$4 AND revision=$5`,
			newState, newRevision, tenant, id, expectedRevision)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		if err = recordConsentEvent(ctx, tx, tenant, id, event, reason, actorID, newRevision); err != nil {
			return err
		}
		out, err = scanConsent(tx.QueryRow(ctx, `SELECT `+biometricConsentColumns+` FROM time_biometric_consent WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	if err != nil {
		return BiometricConsent{}, err
	}
	return out, nil
}

// DueForDestruction returns consents whose destruction schedule has passed
// and that have not yet been destroyed, for the retention sweep that
// enforces "destruction at the earlier of purpose end or the statutory
// limit" (TCLOCK-006).
func (s *Store) DueForDestruction(ctx context.Context, tenant string, asOf time.Time, limit int) ([]BiometricConsent, error) {
	if tenant == "" || asOf.IsZero() || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]BiometricConsent, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+biometricConsentColumns+` FROM time_biometric_consent WHERE tenant_id=$1 AND state<>'DESTROYED' AND destruction_at<=$2 ORDER BY destruction_at LIMIT $3`, tenant, asOf, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanConsent(rows)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}
