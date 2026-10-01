package timestore

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Credential kinds a worker can be identified by at a shared device.
const (
	CredentialPIN   = "PIN"
	CredentialBadge = "BADGE"
	CredentialQR    = "QR"
)

// Credential states.
const (
	CredentialActive  = "ACTIVE"
	CredentialRevoked = "REVOKED"
)

// Credential history events.
const (
	CredentialEventIssued  = "ISSUED"
	CredentialEventRotated = "ROTATED"
	CredentialEventRevoked = "REVOKED"
)

// pinIterations is deliberately large enough to make offline PIN guessing
// slow without pulling in a KDF dependency this module does not already
// vendor; it is HMAC-SHA256 chained, salted per credential.
const pinIterations = 50000

// hashPIN derives a salted verifier for a PIN. It never returns the salt
// derived from the PIN itself: salt is always caller-supplied random bytes.
func hashPIN(pin string, salt []byte) []byte {
	sum := []byte(pin)
	for i := 0; i < pinIterations; i++ {
		mac := hmac.New(sha256.New, salt)
		mac.Write(sum)
		sum = mac.Sum(nil)
	}
	return sum
}

func newSalt() ([]byte, error) {
	salt := make([]byte, 16)
	_, err := rand.Read(salt)
	return salt, err
}

// Credential is the current state of one worker's identification method at
// shared devices. There is at most one row per (tenant, worker, kind); a
// rotation replaces the verifier in place and bumps Revision, it does not
// create a second row.
type Credential struct {
	TenantID, ID, WorkerID, Kind, ExternalID, State string
	VerifierHash, VerifierSalt                      []byte
	Revision                                        int64
	IssuedAt                                        time.Time
	RotatedAt, RevokedAt                            *time.Time
	UpdatedAt                                       time.Time
}

const credentialColumns = `tenant_id,id,worker_id,kind,verifier_hash,verifier_salt,external_id,state,revision,issued_at,rotated_at,revoked_at,updated_at`

func scanCredential(row dbport.Row) (Credential, error) {
	var c Credential
	err := row.Scan(&c.TenantID, &c.ID, &c.WorkerID, &c.Kind, &c.VerifierHash, &c.VerifierSalt, &c.ExternalID, &c.State, &c.Revision, &c.IssuedAt, &c.RotatedAt, &c.RevokedAt, &c.UpdatedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	return c, err
}

func recordCredentialEvent(ctx context.Context, tx dbport.Tx, tenant, credentialID, event, actorID string, revision int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO time_worker_credential_history(tenant_id,id,credential_id,revision,event,actor_id) VALUES($1,$2,$3,$4,$5,$6)`,
		tenant, uuid.NewString(), credentialID, revision, event, actorID)
	return err
}

// IssuePINCredential creates or replaces a worker's PIN credential. A PIN
// changes in place: at most one PIN row exists per worker, so a reissue is
// a rotation, recorded as such in history.
func (s *Store) IssuePINCredential(ctx context.Context, tenant, workerID, pin, actorID string) (Credential, error) {
	if tenant == "" || workerID == "" || pin == "" || actorID == "" {
		return Credential{}, ErrInvalid
	}
	salt, err := newSalt()
	if err != nil {
		return Credential{}, err
	}
	hash := hashPIN(pin, salt)
	return s.upsertCredential(ctx, tenant, workerID, CredentialPIN, hash, salt, "", actorID)
}

// IssueBadgeCredential assigns a badge identifier to a worker. Badge IDs
// are not secrets the way a PIN is, so they are stored as-is (no hash).
func (s *Store) IssueBadgeCredential(ctx context.Context, tenant, workerID, badgeID, actorID string) (Credential, error) {
	if badgeID == "" {
		return Credential{}, ErrInvalid
	}
	return s.upsertCredential(ctx, tenant, workerID, CredentialBadge, nil, nil, badgeID, actorID)
}

// IssueQRCredential assigns a QR key ID to a worker.
func (s *Store) IssueQRCredential(ctx context.Context, tenant, workerID, qrKeyID, actorID string) (Credential, error) {
	if qrKeyID == "" {
		return Credential{}, ErrInvalid
	}
	return s.upsertCredential(ctx, tenant, workerID, CredentialQR, nil, nil, qrKeyID, actorID)
}

func (s *Store) upsertCredential(ctx context.Context, tenant, workerID, kind string, hash, salt []byte, externalID, actorID string) (Credential, error) {
	var out Credential
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		existing, err := scanCredential(tx.QueryRow(ctx, `SELECT `+credentialColumns+` FROM time_worker_credential WHERE tenant_id=$1 AND worker_id=$2 AND kind=$3 FOR UPDATE`, tenant, workerID, kind))
		if errors.Is(err, ErrNotFound) {
			id := uuid.NewString()
			if _, err = tx.Exec(ctx, `INSERT INTO time_worker_credential(tenant_id,id,worker_id,kind,verifier_hash,verifier_salt,external_id,state,revision) VALUES($1,$2,$3,$4,$5,$6,$7,'ACTIVE',1)`,
				tenant, id, workerID, kind, hash, salt, externalID); err != nil {
				return err
			}
			if err = recordCredentialEvent(ctx, tx, tenant, id, CredentialEventIssued, actorID, 1); err != nil {
				return err
			}
			out, err = scanCredential(tx.QueryRow(ctx, `SELECT `+credentialColumns+` FROM time_worker_credential WHERE tenant_id=$1 AND id=$2`, tenant, id))
			return err
		}
		if err != nil {
			return err
		}
		newRevision := existing.Revision + 1
		tag, err := tx.Exec(ctx, `UPDATE time_worker_credential SET verifier_hash=$1,verifier_salt=$2,external_id=$3,state='ACTIVE',revision=$4,rotated_at=now(),revoked_at=NULL,updated_at=now() WHERE tenant_id=$5 AND id=$6 AND revision=$7`,
			hash, salt, externalID, newRevision, tenant, existing.ID, existing.Revision)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		if err = recordCredentialEvent(ctx, tx, tenant, existing.ID, CredentialEventRotated, actorID, newRevision); err != nil {
			return err
		}
		out, err = scanCredential(tx.QueryRow(ctx, `SELECT `+credentialColumns+` FROM time_worker_credential WHERE tenant_id=$1 AND id=$2`, tenant, existing.ID))
		return err
	})
	if err != nil {
		return Credential{}, err
	}
	return out, nil
}

// RevokeCredential deactivates a worker's credential of the given kind.
func (s *Store) RevokeCredential(ctx context.Context, tenant, workerID, kind, actorID string, expectedRevision int64) (Credential, error) {
	if tenant == "" || workerID == "" || actorID == "" || expectedRevision <= 0 {
		return Credential{}, ErrInvalid
	}
	var out Credential
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		existing, err := scanCredential(tx.QueryRow(ctx, `SELECT `+credentialColumns+` FROM time_worker_credential WHERE tenant_id=$1 AND worker_id=$2 AND kind=$3 FOR UPDATE`, tenant, workerID, kind))
		if err != nil {
			return err
		}
		if existing.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		tag, err := tx.Exec(ctx, `UPDATE time_worker_credential SET state='REVOKED',revision=$1,revoked_at=now(),updated_at=now() WHERE tenant_id=$2 AND id=$3 AND revision=$4`,
			expectedRevision+1, tenant, existing.ID, expectedRevision)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		if err = recordCredentialEvent(ctx, tx, tenant, existing.ID, CredentialEventRevoked, actorID, expectedRevision+1); err != nil {
			return err
		}
		out, err = scanCredential(tx.QueryRow(ctx, `SELECT `+credentialColumns+` FROM time_worker_credential WHERE tenant_id=$1 AND id=$2`, tenant, existing.ID))
		return err
	})
	if err != nil {
		return Credential{}, err
	}
	return out, nil
}

// VerifyPIN reports whether pin matches the worker's active PIN credential,
// using a constant-time comparison so a mismatch's timing does not leak
// how many leading bytes matched.
func (s *Store) VerifyPIN(ctx context.Context, tenant, workerID, pin string) (bool, error) {
	if tenant == "" || workerID == "" || pin == "" {
		return false, ErrInvalid
	}
	var ok bool
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		c, err := scanCredential(tx.QueryRow(ctx, `SELECT `+credentialColumns+` FROM time_worker_credential WHERE tenant_id=$1 AND worker_id=$2 AND kind='PIN'`, tenant, workerID))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if c.State != CredentialActive {
			return nil
		}
		candidate := hashPIN(pin, c.VerifierSalt)
		ok = subtle.ConstantTimeCompare(candidate, c.VerifierHash) == 1
		return nil
	})
	return ok, err
}

// RecordFailedDeviceAttempt increments a device's failed-attempt counter
// and returns the counter after the increment, locking the device out from
// lockUntil when the threshold is reached.
func (s *Store) RecordFailedDeviceAttempt(ctx context.Context, tenant, deviceID string, threshold int, lockUntil time.Time) (int, error) {
	return s.recordFailedAttempt(ctx, tenant, "time_device_lockout", "device_id", deviceID, threshold, lockUntil)
}

// RecordFailedWorkerAttempt increments a worker's failed-attempt counter,
// independent of which device the attempt happened at.
func (s *Store) RecordFailedWorkerAttempt(ctx context.Context, tenant, workerID string, threshold int, lockUntil time.Time) (int, error) {
	return s.recordFailedAttempt(ctx, tenant, "time_worker_lockout", "worker_id", workerID, threshold, lockUntil)
}

func (s *Store) recordFailedAttempt(ctx context.Context, tenant, table, keyColumn, key string, threshold int, lockUntil time.Time) (int, error) {
	if tenant == "" || key == "" || threshold <= 0 {
		return 0, ErrInvalid
	}
	var count int
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		// One statement: insert the first failure, or atomically bump an
		// existing counter, and either way lock out once the threshold is
		// crossed by this very increment.
		err := tx.QueryRow(ctx, `INSERT INTO `+table+`(tenant_id,`+keyColumn+`,failed_count,locked_until,updated_at) VALUES($1,$2,1,NULL,now())
			ON CONFLICT (tenant_id,`+keyColumn+`) DO UPDATE SET failed_count=`+table+`.failed_count+1,updated_at=now()
			RETURNING failed_count`, tenant, key).Scan(&count)
		if err != nil {
			return err
		}
		if count >= threshold {
			_, err = tx.Exec(ctx, `UPDATE `+table+` SET locked_until=$1 WHERE tenant_id=$2 AND `+keyColumn+`=$3`, lockUntil, tenant, key)
		}
		return err
	})
	return count, err
}

// ResetDeviceAttempts clears a device's failed-attempt counter and lockout,
// typically after a successful identification.
func (s *Store) ResetDeviceAttempts(ctx context.Context, tenant, deviceID string) error {
	return s.resetAttempts(ctx, tenant, "time_device_lockout", "device_id", deviceID)
}

// ResetWorkerAttempts clears a worker's failed-attempt counter and lockout.
func (s *Store) ResetWorkerAttempts(ctx context.Context, tenant, workerID string) error {
	return s.resetAttempts(ctx, tenant, "time_worker_lockout", "worker_id", workerID)
}

func (s *Store) resetAttempts(ctx context.Context, tenant, table, keyColumn, key string) error {
	if tenant == "" || key == "" {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE `+table+` SET failed_count=0,locked_until=NULL,updated_at=now() WHERE tenant_id=$1 AND `+keyColumn+`=$2`, tenant, key)
		return err
	})
}

// LockoutState is the current failed-attempt counter and lockout deadline
// for a device or a worker.
type LockoutState struct {
	FailedCount int
	LockedUntil *time.Time
}

// DeviceLockout returns a device's current lockout state. An unknown device
// (no recorded failures yet) returns a zero LockoutState, not ErrNotFound.
func (s *Store) DeviceLockout(ctx context.Context, tenant, deviceID string) (LockoutState, error) {
	return s.lockoutState(ctx, tenant, "time_device_lockout", "device_id", deviceID)
}

// WorkerLockout returns a worker's current lockout state.
func (s *Store) WorkerLockout(ctx context.Context, tenant, workerID string) (LockoutState, error) {
	return s.lockoutState(ctx, tenant, "time_worker_lockout", "worker_id", workerID)
}

func (s *Store) lockoutState(ctx context.Context, tenant, table, keyColumn, key string) (LockoutState, error) {
	if tenant == "" || key == "" {
		return LockoutState{}, ErrInvalid
	}
	var out LockoutState
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT failed_count,locked_until FROM `+table+` WHERE tenant_id=$1 AND `+keyColumn+`=$2`, tenant, key).Scan(&out.FailedCount, &out.LockedUntil)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		return err
	})
	return out, err
}

// SupervisorOverride is one append-only record of a supervisor authorizing
// a punch identification that would otherwise be refused.
type SupervisorOverride struct {
	ID, TenantID, DeviceID, WorkerID, SupervisorCredentialRef, Reason string
	CreatedAt                                                         time.Time
}

// RecordSupervisorOverride stores evidence of a supervisor override. The
// supervisor's own credential reference and a reason are required; there
// is no path that lets an override be recorded anonymously.
func (s *Store) RecordSupervisorOverride(ctx context.Context, tenant, deviceID, workerID, supervisorCredentialRef, reason string) (SupervisorOverride, error) {
	if tenant == "" || deviceID == "" || workerID == "" || supervisorCredentialRef == "" || reason == "" {
		return SupervisorOverride{}, ErrInvalid
	}
	out := SupervisorOverride{ID: uuid.NewString(), TenantID: tenant, DeviceID: deviceID, WorkerID: workerID, SupervisorCredentialRef: supervisorCredentialRef, Reason: reason}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO time_supervisor_override(tenant_id,id,device_id,worker_id,supervisor_credential_ref,reason) VALUES($1,$2,$3,$4,$5,$6) RETURNING created_at`,
			tenant, out.ID, deviceID, workerID, supervisorCredentialRef, reason).Scan(&out.CreatedAt)
	})
	if err != nil {
		return SupervisorOverride{}, err
	}
	return out, nil
}
